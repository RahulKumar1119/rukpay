package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// This file is the shared harness for the httpapi tests: a fake clock, a store
// wrapper that counts calls, a log buffer, and helpers that send requests to the
// real handler.

// harnessMinChecks is the minimum number of generated inputs per property run
// (Requirement 12.1). rapid defaults to 100, so the flag is raised here instead
// of relying on -rapid.checks on the command line. A larger explicit value on
// the command line still wins. This one init covers every property test in the
// package.
const harnessMinChecks = 1000

func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= harnessMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(harnessMinChecks)); err != nil {
		panic("httpapi: raise rapid.checks: " + err.Error())
	}
}

// harnessKey is the signing key of every test environment. Tests look for it in
// responses and logs.
const harnessKey = "harness-signing-key-5c1d9e"

// harnessOrigin is the allowed CORS origin of every test environment.
const harnessOrigin = "http://localhost:5173"

func harnessEpoch() time.Time {
	return time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
}

// testClock is a clock that tests move by hand.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock { return &testClock{now: harnessEpoch()} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) AdvanceSeconds(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Duration(n) * time.Second)
}

// countingStore wraps a store and counts the calls that reach it.
type countingStore struct {
	store.VaultStore

	mu     sync.Mutex
	calls  int // every method call
	writes int // Create or UpdateIf calls that returned nil
}

func (c *countingStore) Create(ctx context.Context, rec store.VaultRecord) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	err := c.VaultStore.Create(ctx, rec)
	if err == nil {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}
	return err
}

func (c *countingStore) Get(ctx context.Context, id string) (store.VaultRecord, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.VaultStore.Get(ctx, id)
}

func (c *countingStore) UpdateIf(ctx context.Context, id string, expected store.VaultState, next store.VaultRecord) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	err := c.VaultStore.UpdateIf(ctx, id, expected, next)
	if err == nil {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}
	return err
}

func (c *countingStore) snapshot() (calls, writes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.writes
}

// logBuffer is a bytes.Buffer that is safe for concurrent writes.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// testEnv is the real handler over a MemoryStore, a fake clock and a log buffer.
type testEnv struct {
	handler http.Handler
	server  *server
	cfg     Config
	clock   *testClock
	mem     *store.MemoryStore
	cs      *countingStore
	logs    *logBuffer
}

// envOption changes the configuration or the dependencies of a test environment.
type envOption func(*Config, *Deps)

func withOnLambda(limit time.Duration) envOption {
	return func(c *Config, d *Deps) {
		c.OnLambda = true
		d.LambdaStreamLimit = limit
	}
}

func withKeepAlive(interval time.Duration) envOption {
	return func(_ *Config, d *Deps) { d.KeepAlive = interval }
}

func withOrigin(origin string) envOption {
	return func(c *Config, _ *Deps) { c.AllowedOrigin = origin }
}

func newTestEnv(demo bool, opts ...envOption) *testEnv {
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
	deps := Deps{Store: cs, Clock: clock.Now, Logger: log.New(logs, "", 0)}
	for _, opt := range opts {
		opt(&cfg, &deps)
	}

	handler, srv := assemble(cfg, deps)
	return &testEnv{handler: handler, server: srv, cfg: cfg, clock: clock, mem: mem, cs: cs, logs: logs}
}

// reply is a recorded response.
type reply struct {
	status int
	header http.Header
	body   []byte
}

func (r reply) json(tb rapid.TB, dst any) {
	tb.Helper()
	if err := json.Unmarshal(r.body, dst); err != nil {
		tb.Fatalf("response body %q is not JSON: %v", r.body, err)
	}
}

func (r reply) view(tb rapid.TB) vault.View {
	tb.Helper()
	var v vault.View
	r.json(tb, &v)
	return v
}

// errorCode returns error.code from an error body.
func (r reply) errorCode(tb rapid.TB) string {
	tb.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	r.json(tb, &body)
	return body.Error.Code
}

// send sends a request with a raw body and returns the response.
func (e *testEnv) send(method, target string, body []byte, header http.Header) reply {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return e.serve(req)
}

func (e *testEnv) serve(req *http.Request) reply {
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return reply{status: res.StatusCode, header: res.Header, body: body}
}

// do sends a request whose body is v encoded as JSON. A nil v sends no body.
func (e *testEnv) do(tb rapid.TB, method, target string, v any) reply {
	tb.Helper()
	var body []byte
	if v != nil {
		var err error
		if body, err = json.Marshal(v); err != nil {
			tb.Fatalf("marshal request: %v", err)
		}
	}
	return e.send(method, target, body, nil)
}

// Signal presets for the three demo payments, as request bodies.

func presetInstantPass() map[string]any {
	return map[string]any{
		"amount_inr": 450, "beneficiary_age_hours": 720,
		"is_late_night": false, "active_unknown_call": false, "rapid_retry_count": 0,
	}
}

func presetRealityCheck() map[string]any {
	return map[string]any{
		"amount_inr": 18000, "beneficiary_age_hours": 6,
		"is_late_night": false, "active_unknown_call": false, "rapid_retry_count": 0,
	}
}

func presetLocked() map[string]any {
	return map[string]any{
		"amount_inr": 85000, "beneficiary_age_hours": 1,
		"is_late_night": true, "active_unknown_call": true, "rapid_retry_count": 0,
	}
}

// evaluate creates a vault through the API and returns its View.
func (e *testEnv) evaluate(tb rapid.TB, body map[string]any) vault.View {
	tb.Helper()
	res := e.do(tb, http.MethodPost, "/api/v1/transactions/evaluate", body)
	if res.status != http.StatusOK {
		tb.Fatalf("evaluate = %d %s, want 200", res.status, res.body)
	}
	return res.view(tb)
}

// vaultIn creates a vault and drives it into state through the API.
func (e *testEnv) vaultIn(tb rapid.TB, state store.VaultState) vault.View {
	tb.Helper()
	var v vault.View
	switch state {
	case store.StateInstantPass:
		v = e.evaluate(tb, presetInstantPass())
	case store.StateRealityCheck:
		v = e.evaluate(tb, presetRealityCheck())
	case store.StateLocked15M:
		v = e.evaluate(tb, presetLocked())
	case store.StateGuardianApproved:
		v = e.evaluate(tb, presetLocked())
		e.mustAct(tb, v, "APPROVE", true)
		v = e.get(tb, v.ID)
	case store.StateVetoed:
		v = e.evaluate(tb, presetLocked())
		e.mustAct(tb, v, "VETO", false)
		v = e.get(tb, v.ID)
	case store.StateExpired:
		v = e.evaluate(tb, presetLocked())
		e.clock.AdvanceSeconds(vault.LockDuration + 1)
		v = e.get(tb, v.ID)
	default:
		tb.Fatalf("vaultIn: unknown state %q", state)
	}
	if v.State != state {
		tb.Fatalf("vaultIn(%s) produced state %s", state, v.State)
	}
	return v
}

func (e *testEnv) get(tb rapid.TB, id string) vault.View {
	tb.Helper()
	res := e.do(tb, http.MethodGet, "/api/v1/vault/"+id, nil)
	if res.status != http.StatusOK {
		tb.Fatalf("GET vault = %d %s, want 200", res.status, res.body)
	}
	return res.view(tb)
}

// mustAct sends a guardian action with the vault's token and fails unless it is
// accepted.
func (e *testEnv) mustAct(tb rapid.TB, v vault.View, action string, confirmed bool) {
	tb.Helper()
	res := e.do(tb, http.MethodPost, "/api/v1/vault/"+v.ID+"/guardian-action",
		map[string]any{"action": action, "token": v.GuardianToken, "confirmed": confirmed})
	if res.status != http.StatusOK {
		tb.Fatalf("guardian-action %s = %d %s, want 200", action, res.status, res.body)
	}
}

// record reads the raw stored record, bypassing lazy expiry.
func (e *testEnv) record(tb rapid.TB, id string) store.VaultRecord {
	tb.Helper()
	rec, err := e.mem.Get(context.Background(), id)
	if err != nil {
		tb.Fatalf("store Get(%s) = %v, want nil", id, err)
	}
	return rec
}

func allStates() []store.VaultState {
	return []store.VaultState{
		store.StateInstantPass,
		store.StateRealityCheck,
		store.StateLocked15M,
		store.StateGuardianApproved,
		store.StateVetoed,
		store.StateExpired,
	}
}

func stateGen() *rapid.Generator[store.VaultState] {
	return rapid.SampledFrom(allStates())
}

// bodyContains reports whether any of the needles appears in the response.
func bodyContains(r reply, needles ...string) (string, bool) {
	for _, n := range needles {
		if n != "" && strings.Contains(string(r.body), n) {
			return n, true
		}
	}
	return "", false
}
