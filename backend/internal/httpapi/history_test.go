package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// Router tests for the payer's history (Requirements 9.9, 12.1, 23.1, 23.6):
// the history is wired in Build and in New, a payment is scored against the ones
// before it, and the three presets keep their states however long the history
// grows. A service built without a history store behaves as it always did.

// The three one-click presets of the simulator, as it sends them. Each carries the
// masked recipient it shows on the payer's phone (frontend/src/lib/presets.ts).

func simPresetSafe() map[string]any {
	return map[string]any{
		"amount_inr": 450, "beneficiary_age_hours": 720,
		"is_late_night": false, "active_unknown_call": false, "rapid_retry_count": 0,
		"upi_masked": "s***@okaxis",
	}
}

func simPresetMedium() map[string]any {
	return map[string]any{
		"amount_inr": 18000, "beneficiary_age_hours": 6,
		"is_late_night": false, "active_unknown_call": false, "rapid_retry_count": 0,
		"beneficiary_masked": "XXXX-XXXX-3307",
	}
}

func simPresetHigh() map[string]any {
	return map[string]any{
		"amount_inr": 85000, "beneficiary_age_hours": 1,
		"is_late_night": true, "active_unknown_call": true, "rapid_retry_count": 0,
		"beneficiary_masked": "XXXX-XXXX-4821",
	}
}

// newHistoryEnv is newTestEnv with the history switched on: the one MemoryStore
// that the counting vault store wraps is also the history store.
func newHistoryEnv(demo bool) *testEnv {
	clock := newTestClock()
	mem := store.NewMemoryStoreWithClock(clock.Now)
	cs := &countingStore{VaultStore: mem}
	logs := &logBuffer{}

	cfg := Config{
		StorageDriver: "memory",
		AllowedOrigin: harnessOrigin,
		Port:          "8080",
		DemoMode:      demo,
		HMACKey:       []byte(harnessKey),
	}
	deps := Deps{Store: cs, History: mem, Clock: clock.Now, Logger: log.New(logs, "", 0)}
	handler, srv := assemble(cfg, deps)
	return &testEnv{handler: handler, server: srv, cfg: cfg, clock: clock, mem: mem, cs: cs, logs: logs}
}

// linesOf returns the Contributions of a View as signal -> points.
func linesOf(tb testing.TB, v vault.View) map[string]int {
	tb.Helper()
	if v.Explanation == nil {
		tb.Fatalf("View %+v has no explanation", v)
	}
	out := make(map[string]int, len(*v.Explanation))
	for _, c := range *v.Explanation {
		out[c.Signal] = c.Points
	}
	return out
}

// historyEvents returns every History_Event stored for the payer.
func historyEvents(tb testing.TB, mem *store.MemoryStore) []store.HistoryEvent {
	tb.Helper()
	events, err := mem.RecentHistory(context.Background(), store.PayerKey, 1<<20)
	if err != nil {
		tb.Fatalf("read the history: %v", err)
	}
	return events
}

// The first twelve identical payments to one recipient, each scored against the
// ones before it. The n-th payment has n-1 events behind it, all at one second,
// so its velocity and repeat lines grow by the tiers of Requirement 10, the daily
// line joins at ten events, and the sum of 30 is cut to 25 by a cap line.
func TestEachPaymentIsScoredAgainstTheOnesBeforeIt(t *testing.T) {
	e := newHistoryEnv(false)

	want := []map[string]int{
		{},
		{},
		{"repeat_attempts": 5},
		{"velocity": 5, "repeat_attempts": 5},
		{"velocity": 5, "repeat_attempts": 10},
		{"velocity": 10, "repeat_attempts": 10},
		{"velocity": 10, "repeat_attempts": 10},
		{"velocity": 10, "repeat_attempts": 10},
		{"velocity": 15, "repeat_attempts": 10},
		{"velocity": 15, "repeat_attempts": 10},
		{"velocity": 15, "daily_frequency": 5, "repeat_attempts": 10, "behaviour_cap": -5},
		{"velocity": 15, "daily_frequency": 5, "repeat_attempts": 10, "behaviour_cap": -5},
	}
	for i, lines := range want {
		res := e.do(t, http.MethodPost, pathEvaluate, simPresetSafe())
		if res.status != http.StatusOK {
			t.Fatalf("payment %d = %d %s, want 200", i+1, res.status, res.body)
		}
		v := res.view(t)

		got := linesOf(t, v)
		wantScore := 0
		for _, points := range lines {
			wantScore += points
		}
		if !reflect.DeepEqual(got, lines) {
			t.Errorf("payment %d: explanation lines %v, want %v", i+1, got, lines)
		}
		if v.RiskScore != wantScore || v.State != store.StateInstantPass || !v.Settled {
			t.Errorf("payment %d: %s with score %d, want INSTANT_PASS with %d", i+1, v.State, v.RiskScore, wantScore)
		}
		if v.Reason == "" || strings.Contains(v.Reason, "several payments") {
			t.Errorf("payment %d: a payment that passes at once has the reason %q, want its usual sentence", i+1, v.Reason)
		}
		// A later read of the vault returns the Explanation stored with it.
		if read := e.get(t, v.ID); !reflect.DeepEqual(linesOf(t, read), lines) {
			t.Errorf("payment %d: the stored Explanation reads back as %v, want %v", i+1, linesOf(t, read), lines)
		}
	}
	if n := len(historyEvents(t, e.mem)); n != len(want) {
		t.Fatalf("the history holds %d events, want %d", n, len(want))
	}
}

// A payment that is held for a calm moment names the behaviour that led to it.
// Eight small payments in the last minute earn the medium payment 15 velocity
// points, and its amount, far above those eight, 15 more for the deviation. The
// 30 points are cut to 25 by the cap, so it scores 70 and is still held for the
// reality check.
func TestMediumPaymentAfterABurstNamesTheBurst(t *testing.T) {
	e := newHistoryEnv(false)
	for i := 0; i < 8; i++ {
		e.evaluate(t, simPresetSafe())
	}

	v := e.evaluate(t, simPresetMedium())
	if v.State != store.StateRealityCheck {
		t.Fatalf("medium payment = %s, want REALITY_CHECK", v.State)
	}
	want := map[string]int{
		"amount": 20, "beneficiary_age": 25,
		"velocity": 15, "amount_deviation": 15, "behaviour_cap": -5,
	}
	if got := linesOf(t, v); !reflect.DeepEqual(got, want) {
		t.Errorf("explanation lines %v, want %v", got, want)
	}
	if v.RiskScore != 70 {
		t.Errorf("score = %d, want 70", v.RiskScore)
	}
	for _, phrase := range []string{"several payments in a short time", "an amount well above your recent payments"} {
		if !strings.Contains(v.Reason, phrase) {
			t.Errorf("reason %q does not name %q", v.Reason, phrase)
		}
	}
	if strings.Contains(v.Reason, "cap") {
		t.Errorf("reason %q names a cap line, which has no phrase", v.Reason)
	}
}

// Whatever the history holds, the three presets stay in their states and in their
// ranges (Requirement 9.9). Forty rounds of the three, in a rotating order and
// with the clock moving a little, put 120 events on the shared history, more than
// the 100 a snapshot reads, and the last round shows the caps at work.
func TestPresetsKeepTheirStatesAfterManyEvaluations(t *testing.T) {
	e := newHistoryEnv(false)
	presets := []struct {
		name      string
		body      func() map[string]any
		state     store.VaultState
		low, high int
	}{
		{"safe", simPresetSafe, store.StateInstantPass, 0, 25},
		{"medium", simPresetMedium, store.StateRealityCheck, 45, 70},
		{"high", simPresetHigh, store.StateLocked15M, 90, 100},
	}

	var last [3]vault.View
	for round := 0; round < 40; round++ {
		for k := 0; k < len(presets); k++ {
			i := (round + k) % len(presets)
			p := presets[i]
			e.clock.AdvanceSeconds(int64(round % 4))

			v := e.evaluate(t, p.body())
			if v.State != p.state || v.RiskScore < p.low || v.RiskScore > p.high {
				t.Fatalf("round %d: the %s preset gave %s with score %d, want %s with %d to %d",
					round, p.name, v.State, v.RiskScore, p.state, p.low, p.high)
			}
			last[i] = v
		}
	}

	// The history was in use: each preset ends at the top of its range, held there
	// by the behaviour cap, and the lock by the score cap.
	for i, wantScore := range []int{25, 70, 100} {
		if last[i].RiskScore != wantScore {
			t.Errorf("the %s preset ended at %d, want %d from the capped behaviour points", presets[i].name, last[i].RiskScore, wantScore)
		}
	}
	if got := linesOf(t, last[1])["behaviour_cap"]; got != -10 {
		t.Errorf("medium behaviour_cap = %d, want -10 (velocity 15, repeat 10 and daily 10 make 35)", got)
	}
	if got := linesOf(t, last[2])["score_cap"]; got != -15 {
		t.Errorf("high score_cap = %d, want -15 (90 and 25 make 115)", got)
	}
	if n := len(historyEvents(t, e.mem)); n != 120 {
		t.Errorf("the history holds %d events, want 120", n)
	}
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// Over the router, with the history switched on, any run of presets in any order,
// with any gaps between them, keeps each preset in its state and its range. The
// presets are sent with and without the recipient they carry, so repeat points
// build up on one recipient in some runs and not in others.
//
// **Validates: Requirements 9.9, 12.1, 23.6**
func TestPresetsKeepTheirStatesOverHTTPInAnyOrder(t *testing.T) {
	presets := []struct {
		name      string
		body      func() map[string]any
		recipient string
		state     store.VaultState
		low, high int
	}{
		{"safe", simPresetSafe, "upi_masked", store.StateInstantPass, 0, 25},
		{"medium", simPresetMedium, "beneficiary_masked", store.StateRealityCheck, 45, 70},
		{"high", simPresetHigh, "beneficiary_masked", store.StateLocked15M, 90, 100},
	}
	rapid.Check(t, func(rt *rapid.T) {
		e := newHistoryEnv(false)
		for i, n := 0, rapid.IntRange(1, 40).Draw(rt, "payments"); i < n; i++ {
			p := rapid.SampledFrom(presets).Draw(rt, "preset")
			body := p.body()
			if !rapid.Bool().Draw(rt, "with_recipient") {
				delete(body, p.recipient)
			}
			e.clock.AdvanceSeconds(rapid.OneOf(rapid.Just(int64(0)), rapid.Int64Range(0, 700)).Draw(rt, "gap"))

			v := e.evaluate(rt, body)
			if v.State != p.state || v.RiskScore < p.low || v.RiskScore > p.high {
				rt.Fatalf("payment %d: the %s preset gave %s with score %d, want %s with %d to %d",
					i, p.name, v.State, v.RiskScore, p.state, p.low, p.high)
			}
		}
	})
}

// A handler built with only a Store, as every existing test builds it, has no
// history: nothing is read or written for it, scores stay at their baseline, and
// the store sees the same calls as before (Requirement 23.1).
func TestNilHistoryKeepsTodaysScoringAndStoreCalls(t *testing.T) {
	e := newTestEnv(false)
	const n = 12
	for i := 0; i < n; i++ {
		v := e.evaluate(t, simPresetSafe())
		if lines := linesOf(t, v); len(lines) != 0 || v.RiskScore != 0 {
			t.Fatalf("payment %d: score %d with lines %v, want the baseline of 0 and no lines", i+1, v.RiskScore, lines)
		}
	}
	calls, writes := e.cs.snapshot()
	if calls != n || writes != n {
		t.Fatalf("the store saw %d calls and %d writes, want %d of each: one Create for each payment", calls, writes, n)
	}
	if events := historyEvents(t, e.mem); len(events) != 0 {
		t.Fatalf("the history holds %d events, want none", len(events))
	}
}

// Switching the history on adds no call to the vault store: the vault store sees
// one Create for each payment, and the history has its own store.
func TestHistoryAddsNoCallToTheVaultStore(t *testing.T) {
	e := newHistoryEnv(false)
	const n = 12
	for i := 0; i < n; i++ {
		e.evaluate(t, simPresetSafe())
	}
	calls, writes := e.cs.snapshot()
	if calls != n || writes != n {
		t.Fatalf("the vault store saw %d calls and %d writes, want %d of each", calls, writes, n)
	}
	if events := historyEvents(t, e.mem); len(events) != n {
		t.Fatalf("the history holds %d events, want %d", len(events), n)
	}
}

// A store that happens to implement HistoryStore is not made the history by
// being passed as the Store: only Deps.History turns history on, so a test that
// passes a MemoryStore keeps today's scoring.
func TestHistoryIsNeverGuessedFromTheStore(t *testing.T) {
	clock := newTestClock()
	mem := store.NewMemoryStoreWithClock(clock.Now)
	handler := New(
		Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)},
		Deps{Store: mem, Clock: clock.Now, Logger: log.New(io.Discard, "", 0)},
	)
	e := &testEnv{handler: handler, clock: clock, mem: mem}

	for i := 0; i < 6; i++ {
		v := e.evaluate(t, simPresetSafe())
		if lines := linesOf(t, v); len(lines) != 0 {
			t.Fatalf("payment %d has the lines %v, want none with no history", i+1, lines)
		}
	}
	if events := historyEvents(t, mem); len(events) != 0 {
		t.Fatalf("a store passed as the Store collected %d events, want none", len(events))
	}
}

// A handler built with no dependencies at all gets one MemoryStore that serves as
// the vault store and the history, so it scores against the payer's recent
// payments.
func TestNewWithNoStoreKeepsOneSharedMemoryStoreForHistory(t *testing.T) {
	handler := New(
		Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)},
		Deps{Logger: log.New(io.Discard, "", 0)},
	)
	e := &testEnv{handler: handler}

	var last vault.View
	for i := 0; i < 4; i++ {
		last = e.evaluate(t, simPresetSafe())
	}
	if got := linesOf(t, last)["velocity"]; got != 5 {
		t.Fatalf("the fourth payment has %d velocity points, want 5: it must see the three before it", got)
	}
}

// With no Store and a History given, the History is the one used, and the default
// MemoryStore holds only the vaults.
func TestNewWithNoStoreUsesTheGivenHistory(t *testing.T) {
	history := store.NewMemoryStore()
	handler := New(
		Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)},
		Deps{History: history, Logger: log.New(io.Discard, "", 0)},
	)
	e := &testEnv{handler: handler}
	for i := 0; i < 3; i++ {
		e.evaluate(t, simPresetSafe())
	}
	if n := len(historyEvents(t, history)); n != 3 {
		t.Fatalf("the given history holds %d events, want 3", n)
	}
}

// Build with the memory driver wires one MemoryStore as the history, so the real
// entrypoints score against it.
func TestBuildWithTheMemoryDriverTurnsHistoryOn(t *testing.T) {
	handler, err := Build(context.Background(),
		Config{StorageDriver: "memory", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)})
	if err != nil {
		t.Fatalf("Build = %v, want nil", err)
	}
	e := &testEnv{handler: handler}

	var last vault.View
	for i := 0; i < 4; i++ {
		last = e.evaluate(t, simPresetSafe())
	}
	if got := linesOf(t, last)["velocity"]; got != 5 {
		t.Fatalf("Build: the fourth payment has %d velocity points, want 5", got)
	}
}

// dynamoRecorder is a stand-in for the DynamoDB endpoint. It answers every
// request with an empty success and records which operation was asked and the
// body that was sent.
type dynamoRecorder struct {
	mu     sync.Mutex
	ops    []string
	bodies []string
}

func (d *dynamoRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "DynamoDB_20120810.")
	d.mu.Lock()
	d.ops = append(d.ops, op)
	d.bodies = append(d.bodies, string(body))
	d.mu.Unlock()

	answer := `{}`
	if op == "Query" {
		answer = `{"Items":[],"Count":0,"ScannedCount":0}`
	}
	w.Header().Set("Content-Type", "application/x-amz-json-1.0")
	// The client checks this checksum when it is present, as the real service sends it.
	w.Header().Set("X-Amz-Crc32", strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(answer))), 10))
	_, _ = io.WriteString(w, answer)
}

// Build with the dynamodb driver wires the same store as the history. The DynamoDB
// endpoint is a recorder, so no real service is called. One payment makes one
// Query of the payer's history, then one write of the vault, then one write of
// the event, in that order, and the event item sits under the history key with
// the masked recipient and no other identifier.
func TestBuildWithTheDynamoDBDriverTurnsHistoryOn(t *testing.T) {
	rec := &dynamoRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	t.Setenv("STORAGE_DRIVER", "dynamodb")
	t.Setenv("DYNAMODB_TABLE_NAME", "RukPayVaults")
	t.Setenv("DYNAMODB_ENDPOINT", srv.URL)
	t.Setenv("AWS_REGION", "ap-south-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-access-key")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent/credentials")
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent/config")

	handler, err := Build(context.Background(),
		Config{StorageDriver: "dynamodb", AllowedOrigin: harnessOrigin, HMACKey: []byte(harnessKey)})
	if err != nil {
		t.Fatalf("Build = %v, want nil", err)
	}
	e := &testEnv{handler: handler}
	res := e.do(t, http.MethodPost, pathEvaluate, simPresetSafe())
	if res.status != http.StatusOK {
		t.Fatalf("evaluate = %d %s, want 200", res.status, res.body)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if want := []string{"Query", "PutItem", "PutItem"}; !reflect.DeepEqual(rec.ops, want) {
		t.Fatalf("DynamoDB operations %v, want %v: read the history, store the vault, record the event", rec.ops, want)
	}
	var query struct {
		Expr   string                       `json:"KeyConditionExpression"`
		Values map[string]map[string]string `json:"ExpressionAttributeValues"`
	}
	if err := json.Unmarshal([]byte(rec.bodies[0]), &query); err != nil ||
		query.Expr != "PK = :pk" || query.Values[":pk"]["S"] != "HIST#"+store.PayerKey {
		t.Fatalf("the Query was %s (%v), want one read of the history partition of the payer", rec.bodies[0], err)
	}
	if !strings.Contains(rec.bodies[1], `"VAULT#`) || strings.Contains(rec.bodies[1], `"HIST#`) {
		t.Errorf("the first write %s is not the vault item", rec.bodies[1])
	}
	event := rec.bodies[2]
	for _, want := range []string{`"HIST#demo-payer"`, `"s***@okaxis"`, `"amount_inr":{"N":"450"}`, `"expires_at"`} {
		if !strings.Contains(event, want) {
			t.Errorf("the event item %s lacks %s", event, want)
		}
	}
}

// Every log line of an evaluation is the one line of before, with the route, the
// status and the vault id and nothing from the history: no amount, no recipient,
// no event id.
func TestHistoryAddsNothingToTheLogs(t *testing.T) {
	e := newHistoryEnv(false)
	for i := 0; i < 5; i++ {
		e.evaluate(t, simPresetMedium())
	}

	line := regexp.MustCompile(`^route="POST /api/v1/transactions/evaluate" status=200 vault=[0-9a-f]{32}$`)
	lines := strings.Split(strings.TrimSpace(e.logs.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d log lines, want 5:\n%s", len(lines), e.logs.String())
	}
	for _, l := range lines {
		if !line.MatchString(l) {
			t.Errorf("log line %q is not the plain route, status and vault id", l)
		}
	}
	for _, ev := range historyEvents(t, e.mem) {
		if strings.Contains(e.logs.String(), ev.ID) {
			t.Errorf("the log holds the event id %q", ev.ID)
		}
	}
}

// A rejected request leaves no event, and a request that carries a secret or an
// identifier in a field the API does not read stores none of it: an event holds a
// time, an amount, a masked recipient, its id and its expiry, and nothing else.
func TestRejectedRequestsAndExtraFieldsLeaveNothingInTheHistory(t *testing.T) {
	e := newHistoryEnv(false)

	rejected := []map[string]any{
		{"amount_inr": 0, "beneficiary_age_hours": 720},
		{"amount_inr": 450, "beneficiary_age_hours": -1},
		{"amount_inr": 450, "beneficiary_age_hours": 720, "rapid_retry_count": -1},
		{"amount_inr": 450, "beneficiary_age_hours": 720, "upi_masked": "rahul@okaxis"},
		{"amount_inr": 450, "beneficiary_age_hours": 720, "beneficiary_masked": "123456789012"},
		{"amount_inr": 450, "beneficiary_age_hours": 720, "phone_masked": "9876543210"},
		{"amount_inr": 450, "beneficiary_age_hours": 720, "payer_account_masked": "123456789012"},
	}
	for _, body := range rejected {
		if res := e.do(t, http.MethodPost, pathEvaluate, body); res.status != http.StatusBadRequest {
			t.Fatalf("evaluate %v = %d %s, want 400", body, res.status, res.body)
		}
	}
	if n := len(historyEvents(t, e.mem)); n != 0 {
		t.Fatalf("rejected requests left %d events", n)
	}

	body := simPresetSafe()
	body["otp"] = "482913"
	body["pin"] = "1357"
	body["password"] = "hunter2-secret"
	body["raw_account_number"] = "123456789012"
	body["label"] = "Rahul's brother"
	body["beneficiary_id"] = "not-read-yet"
	if res := e.do(t, http.MethodPost, pathEvaluate, body); res.status != http.StatusOK {
		t.Fatalf("evaluate = %d %s, want 200", res.status, res.body)
	}

	events := historyEvents(t, e.mem)
	if len(events) != 1 {
		t.Fatalf("the history holds %d events, want 1", len(events))
	}
	text := fmt.Sprintf("%+v", events)
	for _, secret := range []string{"482913", "1357", "hunter2", "123456789012", "Rahul", "not-read-yet"} {
		if strings.Contains(text, secret) {
			t.Errorf("the stored event %s holds %q", text, secret)
		}
	}
	if ev := events[0]; ev.BeneficiaryRef != "s***@okaxis" || ev.AmountINR != 450 || ev.ExpiresAt != ev.At+604_800 {
		t.Errorf("event = %+v, want the masked recipient, the amount, and an expiry a week on", ev)
	}
}
