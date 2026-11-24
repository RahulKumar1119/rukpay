package vault

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// historyScenario names how a generated run seeds the history, so that every
// behavioural signal is reached often and not only by luck.
type historyScenario string

const (
	scenarioRandom    historyScenario = "random"    // any stored events at any age
	scenarioBurst     historyScenario = "burst"     // a quick run of recent payments
	scenarioPattern   historyScenario = "pattern"   // small payments, then a large one
	scenarioDeviation historyScenario = "deviation" // small payments, then a far larger one, with old big ones around
)

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// drawSeeds draws the History_Events stored before the first evaluation of a
// run. It returns them in the order they are written.
func drawSeeds(rt *rapid.T, scenario historyScenario) []store.HistoryEvent {
	epoch := histEpoch()
	var seeds []store.HistoryEvent

	random := rapid.OneOf(rapid.IntRange(0, 10), rapid.IntRange(0, 130)).Draw(rt, "random_seed_count")
	if scenario != scenarioRandom {
		random = rapid.IntRange(0, 3).Draw(rt, "random_seed_count")
	}
	for i := 0; i < random; i++ {
		seeds = append(seeds, eventGen(seedID("r", i)).Draw(rt, fmt.Sprintf("random_seed_%d", i)))
	}

	// extra builds an event that is not left to chance: its expiry is the real
	// one, so the store hides it exactly when the retention has passed.
	extra := func(i int, kind string, at, amount int64, ref string) store.HistoryEvent {
		return store.HistoryEvent{ID: seedID(kind, i), At: at, AmountINR: amount, BeneficiaryRef: ref, ExpiresAt: store.HistoryExpiry(at)}
	}
	switch scenario {
	case scenarioBurst:
		for i, n := 0, rapid.IntRange(3, 10).Draw(rt, "burst_count"); i < n; i++ {
			at := epoch - rapid.Int64Range(0, 590).Draw(rt, "burst_age")
			ref := rapid.SampledFrom(poolRefs()).Draw(rt, "burst_ref")
			seeds = append(seeds, extra(i, "b", at, rapid.Int64Range(1, 5000).Draw(rt, "burst_amount"), ref))
		}
	case scenarioPattern:
		for i, n := 0, rapid.IntRange(3, 5).Draw(rt, "small_count"); i < n; i++ {
			at := epoch - rapid.Int64Range(1, 3500).Draw(rt, "small_age")
			seeds = append(seeds, extra(i, "p", at, rapid.Int64Range(1, 1000).Draw(rt, "small_amount"), poolAccounts[0]))
		}
	case scenarioDeviation:
		for i, n := 0, rapid.IntRange(5, 8).Draw(rt, "recent_count"); i < n; i++ {
			at := epoch - rapid.Int64Range(0, 200000).Draw(rt, "recent_age")
			ref := rapid.SampledFrom(poolRefs()).Draw(rt, "recent_ref")
			seeds = append(seeds, extra(i, "d", at, rapid.Int64Range(1, 1000).Draw(rt, "recent_amount"), ref))
		}
		// Big payments on both sides of the retention. The model says which are
		// kept, and the Explanation shows whether the service agrees: a big
		// payment that is kept hides the deviation, one that is dropped does not.
		for i, n := 0, rapid.IntRange(0, 4).Draw(rt, "big_count"); i < n; i++ {
			age := rapid.SampledFrom([]int64{-5, 0, 604798, 604799, 604800, 604801, 700000}).Draw(rt, "big_age")
			amount := rapid.SampledFrom([]int64{85000, 1_000_000, math.MaxInt64}).Draw(rt, "big_amount")
			ev := extra(i, "g", epoch-age, amount, "")
			ev.ExpiresAt = 0 // the store keeps it, so only the service can drop it
			seeds = append(seeds, ev)
		}
	}
	return seeds
}

// Feature: risk-intelligence, Property 16: The snapshot and the event follow the recording rules
//
// For any stored history and any run of evaluations, each evaluation reads the
// History_Snapshot once, before anything is written, with the payer key and a
// limit of 100. The snapshot it scores with is the newest 100 stored events that
// are within the retention, oldest first, and holds none of the payment being
// scored: the Explanation, score, state and reason equal what risk.Assess gives
// for exactly that snapshot, the evaluation time and the Beneficiary_Ref. Then,
// after the vault is stored and not before, it appends exactly one event with the
// evaluation time, the request amount unchanged, that one Beneficiary_Ref, an id
// of its own, and an expiry of time plus 604,800. The clock is read once. The
// next evaluation sees the event.
//
// The scenarios put events on both sides of every window and of the retention,
// at extreme times, and in numbers above the limit of 100. A big payment that is
// just inside the retention hides the amount deviation, and one just outside
// does not, so the Explanation shows which of them the service kept.
//
// **Validates: Requirements 10.5, 12.1, 12.3, 12.4, 12.5, 12.6, 12.10**
func TestSnapshotAndEventFollowTheRecordingRules(t *testing.T) {
	scenarios := []historyScenario{scenarioRandom, scenarioBurst, scenarioPattern, scenarioDeviation}
	reached := make(map[string]int)

	rapid.Check(t, func(rt *rapid.T) {
		scenario := rapid.SampledFrom(scenarios).Draw(rt, "scenario")
		e := newHistEnv(false)

		seeds := drawSeeds(rt, scenario)
		seen := make(map[string]bool)
		stored := make([]store.HistoryEvent, 0, len(seeds))
		for _, ev := range seeds {
			e.seed(rt, ev)
			stored = append(stored, ev)
			seen[ev.ID] = true
		}

		steps := rapid.IntRange(1, 6).Draw(rt, "evaluations")
		var recorded []store.HistoryEvent
		for i := 0; i < steps; i++ {
			req := pooledIdentifiersGen().Draw(rt, "identifiers")
			req.Signals = explainSignalsGen().Draw(rt, "signals")
			advance := rapid.OneOf(
				rapid.Just(int64(0)),
				rapid.Int64Range(0, 30),
				rapid.SampledFrom([]int64{599, 600, 3599, 3600}),
			).Draw(rt, "advance")
			if i == 0 {
				switch scenario {
				case scenarioPattern:
					req.BeneficiaryMasked = poolAccounts[0]
					req.AmountINR = rapid.SampledFrom([]int64{10000, 18000, 85000}).Draw(rt, "pattern_amount")
					advance = 0
				case scenarioDeviation:
					req.AmountINR = rapid.Int64Range(3000, 200000).Draw(rt, "deviation_amount")
					advance = 0
				}
			}
			e.clock.AdvanceSeconds(advance)

			now := e.clock.Now().Unix()
			ref := wantRef(req)
			snapshot := modelSnapshot(stored, now)
			want, err := risk.Assess(risk.Input{Signals: req.Signals, Now: now, History: snapshot, BeneficiaryRef: ref})
			if err != nil {
				rt.Fatalf("risk.Assess on the model snapshot = %v, want nil", err)
			}

			res := e.evaluate(req)
			if res.err != nil {
				rt.Fatalf("evaluation %d: Evaluate = %v, want nil", i, res.err)
			}

			// The order of the stores' operations: the snapshot, then the vault,
			// then the event. One read, one create, one append.
			if !reflect.DeepEqual(res.ops, []string{opRead, opCreate, opAppend}) {
				rt.Fatalf("evaluation %d: store operations %v, want %v", i, res.ops, []string{opRead, opCreate, opAppend})
			}
			if res.clockReads != 1 {
				rt.Fatalf("evaluation %d: the service read its clock %d times, want once", i, res.clockReads)
			}
			reads := e.hist.readCalls()
			if len(reads) != 1 || reads[0] != (histRead{payer: store.PayerKey, limit: 100}) {
				rt.Fatalf("evaluation %d: history reads %+v, want one read of the payer key with limit 100", i, reads)
			}

			// What was scored is what Assess says for the model snapshot.
			rec := e.record(rt, res.view.ID)
			if !reflect.DeepEqual(rec.Explanation, want.Explanation) || rec.RiskScore != want.Score || rec.State != want.State {
				rt.Fatalf("evaluation %d (%s): stored %d %s %+v, want %d %s %+v for the snapshot %+v",
					i, scenario, rec.RiskScore, rec.State, rec.Explanation, want.Score, want.State, want.Explanation, snapshot)
			}
			if rec.CreatedAt.Unix() != now {
				rt.Fatalf("evaluation %d: CreatedAt %d, want the one clock reading %d", i, rec.CreatedAt.Unix(), now)
			}
			if wantReason := reasonForExplained(want.State, req.Signals, want.Explanation); rec.Reason != wantReason {
				rt.Fatalf("evaluation %d: reason %q, want %q", i, rec.Reason, wantReason)
			}
			if res.view.Explanation == nil || !reflect.DeepEqual(*res.view.Explanation, want.Explanation) {
				rt.Fatalf("evaluation %d: the returned View holds a different Explanation", i)
			}
			for _, c := range want.Explanation {
				key := c.Signal
				if strings.HasPrefix(key, risk.SignalPatternPrefix) {
					key = "pattern"
				}
				reached[key]++
			}

			// The one event of this evaluation.
			appends := e.hist.appendCalls()
			if len(appends) != 1 {
				rt.Fatalf("evaluation %d: %d history appends, want exactly 1", i, len(appends))
			}
			ev := appends[0].ev
			if appends[0].payer != store.PayerKey {
				rt.Fatalf("evaluation %d: event written under %q, want the payer key", i, appends[0].payer)
			}
			if ev.At != now || ev.AmountINR != req.AmountINR || ev.BeneficiaryRef != ref || ev.ExpiresAt != now+604_800 {
				rt.Fatalf("evaluation %d: event %+v, want time %d, amount %d, ref %q and expiry %d", i, ev, now, req.AmountINR, ref, now+604_800)
			}
			if ev.BeneficiaryRef != "" && store.MaskedFormat(ev.BeneficiaryRef) == store.MaskNone {
				rt.Fatalf("evaluation %d: event ref %q is not a masked identifier", i, ev.BeneficiaryRef)
			}
			if !hex32.MatchString(ev.ID) || ev.ID == res.view.ID {
				rt.Fatalf("evaluation %d: event id %q must be 32 hex characters and not the vault id", i, ev.ID)
			}
			if seen[ev.ID] || seen[res.view.ID] {
				rt.Fatalf("evaluation %d: event id %q or vault id %q was used before", i, ev.ID, res.view.ID)
			}
			seen[ev.ID], seen[res.view.ID] = true, true

			// The model learns the event, so the next evaluation must see it.
			stored = append(stored, ev)
			recorded = append(recorded, ev)
		}

		// The store holds each recorded event exactly once, unchanged, and nothing
		// else that the run did not write.
		held := make(map[string]store.HistoryEvent)
		for _, ev := range e.stored(rt) {
			held[ev.ID] = ev
		}
		for _, ev := range recorded {
			if got, ok := held[ev.ID]; !ok || got != ev {
				rt.Fatalf("recorded event %+v is stored as %+v (present %v)", ev, got, ok)
			}
		}
		seedIDs := make(map[string]bool, len(seeds))
		for _, ev := range seeds {
			seedIDs[ev.ID] = true
		}
		extra := 0
		for id := range held {
			if !seedIDs[id] {
				extra++
			}
		}
		if extra != len(recorded) {
			rt.Fatalf("the store holds %d events that no seed explains, want the %d recorded", extra, len(recorded))
		}
	})

	for _, signal := range []string{
		risk.SignalVelocity, risk.SignalDailyFrequency, risk.SignalRepeatAttempts,
		risk.SignalAmountDeviation, "pattern", risk.SignalBehaviourCap,
	} {
		if reached[signal] == 0 {
			t.Errorf("no generated evaluation reached %q, so the property did not check it", signal)
		}
	}
}

// Feature: risk-intelligence, Property 16: The snapshot and the event follow the recording rules
//
// The retention boundary, one case at a time. Five small recent payments and one
// big payment at a stated age are stored, and a payment of 3,000 rupees is made.
// The big payment hides the deviation when the service keeps it, so the
// Explanation shows whether it was kept: 604,799 seconds old is kept, exactly
// 604,800 is dropped, a time later than now is kept, and so is the extreme
// largest time.
//
// **Validates: Requirements 12.4**
func TestRetentionBoundaryDecidesWhichEventsAreScored(t *testing.T) {
	epoch := histEpoch()
	tests := []struct {
		name      string
		at        int64
		wantKept  bool
		expiresAt int64
		bigAmount int64
	}{
		{"one second inside the retention", epoch - 604_799, true, 0, 85_000},
		{"exactly 604,800 seconds old", epoch - 604_800, false, 0, 85_000},
		{"one second past the retention", epoch - 604_801, false, 0, 85_000},
		{"a time later than the evaluation", epoch + 5, true, 0, 85_000},
		{"the largest time", math.MaxInt64, true, 0, 85_000},
		{"the smallest time", math.MinInt64, false, 0, 85_000},
		{"a time that would overflow a subtraction", math.MinInt64 + 1, false, 0, math.MaxInt64},
		{"stored past its expiry but still listed", epoch - 700_000, false, math.MaxInt64, 85_000},
		{"inside by time and never expiring", epoch - 100, true, 0, 85_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHistEnv(false)
			for i := 0; i < 5; i++ {
				e.seed(t, store.HistoryEvent{ID: seedID("s", i), At: epoch - 1000 - int64(i), AmountINR: 100, ExpiresAt: 0})
			}
			e.seed(t, store.HistoryEvent{ID: seedID("big", 0), At: tt.at, AmountINR: tt.bigAmount, ExpiresAt: tt.expiresAt})

			res := e.evaluate(EvaluateRequest{Signals: risk.Signals{AmountINR: 3000, BeneficiaryAgeHrs: 720}})
			if res.err != nil {
				t.Fatalf("Evaluate = %v, want nil", res.err)
			}
			hasDeviation := false
			for _, c := range *res.view.Explanation {
				if c.Signal == risk.SignalAmountDeviation {
					hasDeviation = true
				}
			}
			// The big payment is the largest amount whenever it is kept, and then
			// 3,000 rupees is less than 3 times it, so no deviation is scored.
			if hasDeviation == tt.wantKept {
				t.Fatalf("the big payment at time %d was kept = %v, but the Explanation %+v shows deviation = %v",
					tt.at, !hasDeviation, *res.view.Explanation, hasDeviation)
			}
		})
	}
}

// Feature: risk-intelligence, Property 16: The snapshot and the event follow the recording rules
//
// The Beneficiary_Ref of the evaluation and of its event is the first non-empty
// of beneficiary_masked, upi_masked and phone_masked, in that order, and never
// the payer's own account. The same value is used for scoring: a payment whose
// recipient matches earlier events earns repeat points under that recipient.
//
// **Validates: Requirements 12.5, 12.6**
func TestBeneficiaryRefOrderForScoringAndTheEvent(t *testing.T) {
	const (
		account = "XXXX-XXXX-1111"
		upi     = "r***@okaxis"
		phone   = "+91-XXXXXX3210"
		payer   = "XXXX-XXXX-4821"
	)
	tests := []struct {
		name string
		req  EvaluateRequest
		want string
	}{
		{"all four", EvaluateRequest{PayerAccountMasked: payer, BeneficiaryMasked: account, UPIMasked: upi, PhoneMasked: phone}, account},
		{"upi and phone", EvaluateRequest{PayerAccountMasked: payer, UPIMasked: upi, PhoneMasked: phone}, upi},
		{"phone only", EvaluateRequest{PayerAccountMasked: payer, PhoneMasked: phone}, phone},
		{"the payer's account only", EvaluateRequest{PayerAccountMasked: payer}, ""},
		{"nothing", EvaluateRequest{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHistEnv(false)
			tt.req.Signals = signalsInstantPass()
			// Two earlier payments to the expected recipient make a repeat.
			if tt.want != "" {
				for i := 0; i < 2; i++ {
					e.seed(t, store.HistoryEvent{ID: seedID("s", i), At: histEpoch() - 60 - int64(i), AmountINR: 450, BeneficiaryRef: tt.want, ExpiresAt: 0})
				}
			}
			res := e.evaluate(tt.req)
			if res.err != nil {
				t.Fatalf("Evaluate = %v, want nil", res.err)
			}
			appends := e.hist.appendCalls()
			if len(appends) != 1 || appends[0].ev.BeneficiaryRef != tt.want {
				t.Fatalf("event %+v, want the ref %q", appends, tt.want)
			}
			hasRepeat := false
			for _, c := range *res.view.Explanation {
				hasRepeat = hasRepeat || c.Signal == risk.SignalRepeatAttempts
			}
			if hasRepeat != (tt.want != "") {
				t.Fatalf("repeat points = %v for the ref %q, want %v", hasRepeat, tt.want, tt.want != "")
			}
		})
	}
}

// presetRefs are the masked recipients that the three one-click presets of the
// simulator send, and the signals the harness uses for each of them.
var presetRefs = []string{"s***@okaxis", "XXXX-XXXX-3307", "XXXX-XXXX-4821"}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// Whatever the payer's recorded history holds, and however many presets have
// been evaluated on top of it, the safe preset is INSTANT_PASS with a score of 0
// to 25, the medium preset REALITY_CHECK with 45 to 70, and the high-risk preset
// LOCKED_15M with 90 to 100. The presets are sent with the recipient they carry in
// the simulator and with none, so that repeat points can build up on one
// recipient or not at all. This is the whole service run on its own recorded
// events, the half of Property 5 that the risk package cannot show.
//
// **Validates: Requirements 9.9, 12.1**
func TestPresetsKeepTheirStatesUnderAnyRecordedHistory(t *testing.T) {
	type preset struct {
		name      string
		signals   risk.Signals
		ref       int // index into presetRefs
		wantMin   int
		wantMax   int
		wantState store.VaultState
	}
	presets := []preset{
		{"safe", signalsInstantPass(), 0, 0, 25, store.StateInstantPass},
		{"medium", signalsRealityCheck(), 1, 45, 70, store.StateRealityCheck},
		{"high", signalsLocked(), 2, 90, 100, store.StateLocked15M},
	}

	rapid.Check(t, func(rt *rapid.T) {
		e := newHistEnv(false)

		// Random stored history, plus events aimed at the preset recipients.
		n := rapid.OneOf(rapid.IntRange(0, 10), rapid.IntRange(0, 130)).Draw(rt, "seed_count")
		for i := 0; i < n; i++ {
			e.seed(rt, eventGen(seedID("r", i)).Draw(rt, fmt.Sprintf("seed_%d", i)))
		}
		for i, m := 0, rapid.IntRange(0, 12).Draw(rt, "aimed_count"); i < m; i++ {
			at := histEpoch() - rapid.Int64Range(0, 4000).Draw(rt, "aimed_age")
			e.seed(rt, store.HistoryEvent{
				ID: seedID("a", i), At: at,
				AmountINR:      rapid.SampledFrom([]int64{100, 450, 1000, 18000, 85000}).Draw(rt, "aimed_amount"),
				BeneficiaryRef: rapid.SampledFrom(presetRefs).Draw(rt, "aimed_ref"),
				ExpiresAt:      store.HistoryExpiry(at),
			})
		}

		runs := rapid.IntRange(1, 40).Draw(rt, "runs")
		for i := 0; i < runs; i++ {
			p := rapid.SampledFrom(presets).Draw(rt, "preset")
			req := EvaluateRequest{Signals: p.signals}
			if rapid.Bool().Draw(rt, "with_recipient") {
				switch p.ref {
				case 0:
					req.UPIMasked = presetRefs[0]
				default:
					req.BeneficiaryMasked = presetRefs[p.ref]
				}
			}
			e.clock.AdvanceSeconds(rapid.Int64Range(0, 5).Draw(rt, "advance"))

			res := e.evaluate(req)
			if res.err != nil {
				rt.Fatalf("%s preset: Evaluate = %v, want nil", p.name, res.err)
			}
			v := res.view
			if v.State != p.wantState || v.RiskScore < p.wantMin || v.RiskScore > p.wantMax {
				rt.Fatalf("%s preset (run %d) gave %s with score %d, want %s with %d to %d",
					p.name, i, v.State, v.RiskScore, p.wantState, p.wantMin, p.wantMax)
			}
		}
	})
}

// A request with no history store scores and writes as before: it reads nothing,
// records nothing, and its Explanation is what risk.Assess gives for an empty
// snapshot.
func TestServiceWithoutAHistoryStoreReadsAndRecordsNothing(t *testing.T) {
	e := newHistEnv(false)
	e.svc.SetHistory(nil)
	for i := 0; i < 5; i++ {
		e.seed(t, store.HistoryEvent{ID: seedID("s", i), At: histEpoch() - 10, AmountINR: 450, ExpiresAt: 0})
	}

	res := e.evaluate(EvaluateRequest{Signals: signalsRealityCheck()})
	if res.err != nil {
		t.Fatalf("Evaluate = %v, want nil", res.err)
	}
	if !reflect.DeepEqual(res.ops, []string{opCreate}) {
		t.Fatalf("store operations %v, want only the vault write", res.ops)
	}
	want, err := risk.Assess(risk.Input{Signals: signalsRealityCheck()})
	if err != nil {
		t.Fatalf("risk.Assess = %v", err)
	}
	if res.view.RiskScore != want.Score || !reflect.DeepEqual(*res.view.Explanation, want.Explanation) {
		t.Fatalf("score %d and Explanation %+v, want %d and %+v with no history", res.view.RiskScore, *res.view.Explanation, want.Score, want.Explanation)
	}
	if got := e.stored(t); len(got) != 5 {
		t.Fatalf("the history holds %d events, want only the 5 seeded", len(got))
	}
}

// Many evaluations in a row each see the ones before them, through the service
// alone: the n-th identical payment to one recipient has n-1 events behind it.
func TestEarlierEvaluationsAreSeenByLaterOnes(t *testing.T) {
	e := newHistEnv(false)
	req := EvaluateRequest{Signals: signalsInstantPass(), UPIMasked: "r***@okaxis"}

	// The signals that each of the first ten payments earns, by how many events
	// came before it (velocity counts all of them, repeat the same recipient's).
	want := []map[string]int{
		{},
		{},
		{risk.SignalRepeatAttempts: 5},
		{risk.SignalVelocity: 5, risk.SignalRepeatAttempts: 5},
		{risk.SignalVelocity: 5, risk.SignalRepeatAttempts: 10},
		{risk.SignalVelocity: 10, risk.SignalRepeatAttempts: 10},
		{risk.SignalVelocity: 10, risk.SignalRepeatAttempts: 10},
		{risk.SignalVelocity: 10, risk.SignalRepeatAttempts: 10},
		{risk.SignalVelocity: 15, risk.SignalRepeatAttempts: 10},
		{risk.SignalVelocity: 15, risk.SignalRepeatAttempts: 10},
	}
	for i, lines := range want {
		res := e.evaluate(req)
		if res.err != nil {
			t.Fatalf("payment %d: Evaluate = %v", i+1, res.err)
		}
		got := make(map[string]int)
		for _, c := range *res.view.Explanation {
			got[c.Signal] = c.Points
		}
		for signal, points := range lines {
			if got[signal] != points {
				t.Errorf("payment %d: %s = %d points, want %d (Explanation %+v)", i+1, signal, got[signal], points, *res.view.Explanation)
			}
		}
		if len(got) != len(lines) {
			t.Errorf("payment %d: Explanation %+v, want only %v", i+1, *res.view.Explanation, lines)
		}
	}
	if n := len(e.stored(t)); n != len(want) {
		t.Fatalf("the history holds %d events, want %d", n, len(want))
	}
}

// A cancelled context stops the evaluation at the vault write and records nothing.
func TestCancelledContextRecordsNoEvent(t *testing.T) {
	e := newHistEnv(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.svc.Evaluate(ctx, EvaluateRequest{Signals: signalsLocked()}); err == nil {
		t.Fatal("Evaluate with a cancelled context = nil, want an error")
	}
	if got := e.hist.appendCalls(); len(got) != 0 {
		t.Fatalf("a cancelled evaluation appended %+v", got)
	}
	if got := e.stored(t); len(got) != 0 {
		t.Fatalf("the history holds %d events, want none", len(got))
	}
}
