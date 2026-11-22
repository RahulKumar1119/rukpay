package vault

import (
	"context"
	"flag"
	"strconv"
	"sync"
	"time"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file is the shared harness for the vault tests: a fake clock, a fake
// token issuer, a recording observer, a store wrapper that counts writes, and
// helpers that put a vault in any state.

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
		panic("vault: raise rapid.checks: " + err.Error())
	}
}

// harnessEpoch is the fixed starting time of every test clock, a whole second.
func harnessEpoch() time.Time {
	return time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
}

// testClock is a clock that tests move by hand. It is safe for concurrent use.
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

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// AdvanceSeconds moves the clock forward by n seconds.
func (c *testClock) AdvanceSeconds(n int64) { c.Advance(time.Duration(n) * time.Second) }

// testIssuer issues a recognisable token for a vault id and expiry. It holds a
// secret value that must never appear in a View, so tests can look for it.
type testIssuer struct{}

const harnessSecret = "SECRET-SIGNING-KEY-d41d8cd9"

func (testIssuer) Issue(vaultID string, expiry int64) string {
	return "tok:" + vaultID + ":" + strconv.FormatInt(expiry, 10)
}

// recObserver records every View it is told about.
type recObserver struct {
	mu    sync.Mutex
	views []View
}

func (o *recObserver) Publish(v View) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.views = append(o.views, v)
}

func (o *recObserver) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.views)
}

func (o *recObserver) all() []View {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]View(nil), o.views...)
}

// countingStore wraps a store and counts the calls that changed it.
type countingStore struct {
	store.VaultStore

	mu           sync.Mutex
	writes       int // Create or UpdateIf calls that returned nil
	updateIfCall int // all UpdateIf calls, successful or not
	beforeUpdate func(id string, expected store.VaultState)
}

func (c *countingStore) Create(ctx context.Context, rec store.VaultRecord) error {
	err := c.VaultStore.Create(ctx, rec)
	if err == nil {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}
	return err
}

func (c *countingStore) UpdateIf(ctx context.Context, id string, expected store.VaultState, next store.VaultRecord) error {
	c.mu.Lock()
	c.updateIfCall++
	hook := c.beforeUpdate
	c.beforeUpdate = nil // the hook fires once
	c.mu.Unlock()
	if hook != nil {
		hook(id, expected)
	}

	err := c.VaultStore.UpdateIf(ctx, id, expected, next)
	if err == nil {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}
	return err
}

func (c *countingStore) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

// env is one service with its store, clock and observer.
type env struct {
	svc   *Service
	mem   *store.MemoryStore
	cs    *countingStore
	clock *testClock
	obs   *recObserver
}

func newEnv(demo bool) *env {
	clock := newTestClock()
	mem := store.NewMemoryStoreWithClock(clock.Now)
	cs := &countingStore{VaultStore: mem}
	obs := &recObserver{}
	return &env{
		svc:   New(cs, clock.Now, demo, testIssuer{}, obs),
		mem:   mem,
		cs:    cs,
		clock: clock,
		obs:   obs,
	}
}

// Signal presets for the three demo payments.

func signalsInstantPass() risk.Signals {
	return risk.Signals{AmountINR: 450, BeneficiaryAgeHrs: 720}
}

func signalsRealityCheck() risk.Signals {
	return risk.Signals{AmountINR: 18000, BeneficiaryAgeHrs: 2}
}

func signalsLocked() risk.Signals {
	return risk.Signals{AmountINR: 85000, BeneficiaryAgeHrs: 2, IsLateNight: true, ActiveUnknownCall: true}
}

// create evaluates a payment and returns its first View.
func (e *env) create(tb rapid.TB, sig risk.Signals) View {
	tb.Helper()
	return e.createWith(tb, EvaluateRequest{Signals: sig})
}

// createWith evaluates a full request, including masked identifiers.
func (e *env) createWith(tb rapid.TB, req EvaluateRequest) View {
	tb.Helper()
	v, err := e.svc.Evaluate(context.Background(), req)
	if err != nil {
		tb.Fatalf("Evaluate(%+v) = %v, want nil", req, err)
	}
	return v
}

// vaultIn creates a vault and drives it into state, then returns its View. The
// clock ends at most a few seconds after the vault was created, except for
// EXPIRED, which needs the lock to run out.
func (e *env) vaultIn(tb rapid.TB, state store.VaultState) View {
	tb.Helper()
	return e.vaultInWith(tb, state, EvaluateRequest{})
}

// vaultInWith is vaultIn for a request that carries masked identifiers. The
// signals of req are replaced by the preset that leads to state.
func (e *env) vaultInWith(tb rapid.TB, state store.VaultState, req EvaluateRequest) View {
	tb.Helper()
	ctx := context.Background()

	var (
		v   View
		err error
	)
	switch state {
	case store.StateInstantPass:
		req.Signals = signalsInstantPass()
		v = e.createWith(tb, req)
	case store.StateRealityCheck:
		req.Signals = signalsRealityCheck()
		v = e.createWith(tb, req)
	case store.StateLocked15M:
		req.Signals = signalsLocked()
		v = e.createWith(tb, req)
	case store.StateGuardianApproved:
		req.Signals = signalsLocked()
		v = e.createWith(tb, req)
		v, err = e.svc.Approve(ctx, v.ID)
	case store.StateVetoed:
		req.Signals = signalsLocked()
		v = e.createWith(tb, req)
		v, err = e.svc.Veto(ctx, v.ID)
	case store.StateExpired:
		req.Signals = signalsLocked()
		v = e.createWith(tb, req)
		e.clock.AdvanceSeconds(LockDuration + 1)
		v, err = e.svc.Get(ctx, v.ID)
	default:
		tb.Fatalf("vaultIn: unknown state %q", state)
	}
	if err != nil {
		tb.Fatalf("vaultIn(%s) = %v, want nil", state, err)
	}
	if v.State != state {
		tb.Fatalf("vaultIn(%s) produced state %s", state, v.State)
	}
	return v
}

// record reads the raw stored record, bypassing lazy expiry.
func (e *env) record(tb rapid.TB, id string) store.VaultRecord {
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

// signalsGen draws valid signals across every tier boundary.
func signalsGen() *rapid.Generator[risk.Signals] {
	return rapid.Custom(func(t *rapid.T) risk.Signals {
		return risk.Signals{
			AmountINR: rapid.OneOf(
				rapid.SampledFrom([]int64{1, 450, 1999, 2000, 9999, 10000, 18000, 24999, 25000, 85000}),
				rapid.Int64Range(1, 200000),
			).Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: rapid.OneOf(
				rapid.SampledFrom([]int64{0, 2, 23, 24, 71, 72, 720}),
				rapid.Int64Range(0, 1000),
			).Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: rapid.Bool().Draw(t, "active_unknown_call"),
			RapidRetryCount: rapid.OneOf(
				rapid.SampledFrom([]int{0, 1, 2, 3}),
				rapid.IntRange(0, 10),
			).Draw(t, "rapid_retry_count"),
		}
	})
}

// answersGen draws one of the eight valid answer combinations.
func answersGen() *rapid.Generator[Answers] {
	yesNo := rapid.SampledFrom([]Answer{AnswerYes, AnswerNo})
	return rapid.Custom(func(t *rapid.T) Answers {
		return Answers{
			Coercion:     yesNo.Draw(t, "coercion"),
			Secrecy:      yesNo.Draw(t, "secrecy"),
			Verification: yesNo.Draw(t, "verification"),
		}
	})
}

// wantLock is the reality-check rule written out again, independently of
// CheckAnswers (Requirements 3.2 to 3.5).
func wantLock(a Answers) bool {
	return a.Coercion == "YES" || a.Secrecy == "YES" || a.Verification == "NO"
}

// identifiersGen draws a request body with optional masked identifiers. The
// signals are left empty, because vaultInWith chooses them.
func identifiersGen() *rapid.Generator[EvaluateRequest] {
	// A masked value is also text that redact.Text must leave alone. A random
	// UPI handle can spell a word such as "otp", which redact.Text rightly
	// rewrites, so those values are left out of the generator.
	optional := func(expr string) *rapid.Generator[string] {
		plain := rapid.StringMatching(expr).Filter(func(s string) bool { return redact.Text(s) == s })
		return rapid.OneOf(rapid.Just(""), plain)
	}
	return rapid.Custom(func(t *rapid.T) EvaluateRequest {
		return EvaluateRequest{
			PayerAccountMasked: optional(`^XXXX-XXXX-[0-9]{4}$`).Draw(t, "payer_account_masked"),
			BeneficiaryMasked:  optional(`^XXXX-XXXX-[0-9]{4}$`).Draw(t, "beneficiary_masked"),
			UPIMasked:          optional(`^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]{1,12}$`).Draw(t, "upi_masked"),
			PhoneMasked:        optional(`^\+91-XXXXXX[0-9]{4}$`).Draw(t, "phone_masked"),
		}
	})
}
