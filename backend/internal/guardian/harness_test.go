package guardian

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
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// This file is the shared harness for the guardian tests: a fake clock, a store
// wrapper that counts every call, and helpers that put a vault in any state.

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
		panic("guardian: raise rapid.checks: " + err.Error())
	}
}

// harnessEpoch is the fixed starting time of every test clock, a whole second.
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

// countingStore wraps a store and counts every call that reaches it.
type countingStore struct {
	store.VaultStore

	mu       sync.Mutex
	calls    int // every method call
	writes   int // Create or UpdateIf calls that returned nil
	updateIf int // every UpdateIf call
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
	c.updateIf++
	c.mu.Unlock()
	err := c.VaultStore.UpdateIf(ctx, id, expected, next)
	if err == nil {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}
	return err
}

func (c *countingStore) snapshot() (calls, writes, updateIf int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.writes, c.updateIf
}

// harnessKey is the signing key of every test environment.
var harnessKey = []byte("harness-signing-key-8f3a1c")

// env is a guardian service over a real vault service, store, signer and hub.
type env struct {
	guardian *Service
	vault    *vault.Service
	signer   *Signer
	hub      *Hub
	mem      *store.MemoryStore
	cs       *countingStore
	clock    *testClock
}

func newEnv(demo bool) *env {
	clock := newTestClock()
	mem := store.NewMemoryStoreWithClock(clock.Now)
	cs := &countingStore{VaultStore: mem}
	signer := NewSigner(harnessKey)
	hub := NewHub()
	vs := vault.New(cs, clock.Now, demo, signer, hub)
	return &env{
		guardian: NewService(vs, signer, clock.Now),
		vault:    vs,
		signer:   signer,
		hub:      hub,
		mem:      mem,
		cs:       cs,
		clock:    clock,
	}
}

func signalsInstantPass() risk.Signals {
	return risk.Signals{AmountINR: 450, BeneficiaryAgeHrs: 720}
}

func signalsRealityCheck() risk.Signals {
	return risk.Signals{AmountINR: 18000, BeneficiaryAgeHrs: 2}
}

func signalsLocked() risk.Signals {
	return risk.Signals{AmountINR: 85000, BeneficiaryAgeHrs: 2, IsLateNight: true, ActiveUnknownCall: true}
}

// vaultIn creates a vault and drives it into state, then returns its View.
func (e *env) vaultIn(tb rapid.TB, state store.VaultState) vault.View {
	return e.vaultInWith(tb, state, vault.EvaluateRequest{})
}

// vaultInWith is vaultIn for a request that carries masked identifiers. The
// signals of req are replaced by the preset that leads to state.
func (e *env) vaultInWith(tb rapid.TB, state store.VaultState, req vault.EvaluateRequest) vault.View {
	tb.Helper()
	ctx := context.Background()

	create := func(sig risk.Signals) vault.View {
		req.Signals = sig
		v, err := e.vault.Evaluate(ctx, req)
		if err != nil {
			tb.Fatalf("Evaluate(%+v) = %v, want nil", req, err)
		}
		return v
	}

	var (
		v   vault.View
		err error
	)
	switch state {
	case store.StateInstantPass:
		v = create(signalsInstantPass())
	case store.StateRealityCheck:
		v = create(signalsRealityCheck())
	case store.StateLocked15M:
		v = create(signalsLocked())
	case store.StateGuardianApproved:
		v = create(signalsLocked())
		v, err = e.vault.Approve(ctx, v.ID)
	case store.StateVetoed:
		v = create(signalsLocked())
		v, err = e.vault.Veto(ctx, v.ID)
	case store.StateExpired:
		v = create(signalsLocked())
		e.clock.AdvanceSeconds(vault.LockDuration + 1)
		v, err = e.vault.Get(ctx, v.ID)
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

// longToken is a token for id that stays valid for a day, whatever the vault
// state. It lets a test reach the state checks that sit behind token
// verification.
func (e *env) longToken(id string) string {
	return e.signer.Issue(id, e.clock.Now().Unix()+86400)
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

func nonLockedStates() []store.VaultState {
	var states []store.VaultState
	for _, s := range allStates() {
		if s != store.StateLocked15M {
			states = append(states, s)
		}
	}
	return states
}

// identifiersGen draws a request body with optional masked identifiers. The
// signals are left empty, because vaultInWith chooses them.
func identifiersGen() *rapid.Generator[vault.EvaluateRequest] {
	// A masked value is also text that redact.Text must leave alone. A random
	// UPI handle can spell a word such as "otp", which redact.Text rightly
	// rewrites, so those values are left out of the generator.
	optional := func(expr string) *rapid.Generator[string] {
		plain := rapid.StringMatching(expr).Filter(func(s string) bool { return redact.Text(s) == s })
		return rapid.OneOf(rapid.Just(""), plain)
	}
	return rapid.Custom(func(t *rapid.T) vault.EvaluateRequest {
		return vault.EvaluateRequest{
			PayerAccountMasked: optional(`^XXXX-XXXX-[0-9]{4}$`).Draw(t, "payer_account_masked"),
			BeneficiaryMasked:  optional(`^XXXX-XXXX-[0-9]{4}$`).Draw(t, "beneficiary_masked"),
			UPIMasked:          optional(`^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]{1,12}$`).Draw(t, "upi_masked"),
			PhoneMasked:        optional(`^\+91-XXXXXX[0-9]{4}$`).Draw(t, "phone_masked"),
		}
	})
}

// vaultIDGen draws a vault id in the real format: 32 lower-case hex characters.
func vaultIDGen() *rapid.Generator[string] {
	return rapid.StringMatching(`^[0-9a-f]{32}$`)
}
