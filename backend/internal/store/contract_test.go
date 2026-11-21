package store

import (
	"context"
	"flag"
	"math"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// This file is the shared contract harness. It runs one test body against
// MemoryStore and against DynamoDBStore over fakeDynamoDB, each with a
// controllable clock, so both adapters face identical tests (Requirements
// 11.5, 11.6). The property tests in pbt_*_test.go and the examples in
// dynamodb_test.go build on it.

// contractMinChecks is the minimum number of generated inputs per property run.
// rapid defaults to 100, so the flag is raised here instead of relying on
// -rapid.checks being passed on the command line. A larger explicit value on
// the command line still wins. This one init covers every property test in the
// package.
const contractMinChecks = 1000

func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= contractMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(contractMinChecks)); err != nil {
		panic("store: raise rapid.checks: " + err.Error())
	}
}

// contractTable is the table name the harness gives DynamoDBStore. It differs
// from the production default so a test can tell the two apart.
const contractTable = "RukPayVaultsContract"

// contractEpoch is the fixed starting time of every contract clock. It is a
// whole second, because the unixtime tag stores whole seconds only.
func contractEpoch() time.Time {
	return time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
}

// contractClock is a clock that tests move by hand. It is safe for concurrent
// use.
type contractClock struct {
	mu  sync.Mutex
	now time.Time
}

func newContractClock(start time.Time) *contractClock {
	return &contractClock{now: start}
}

// Now returns the current time of the clock. It matches the signature of the
// now field of both adapters.
func (c *contractClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t.
func (c *contractClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the clock forward by d.
func (c *contractClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// contractEnv is one fresh store with its clock. fake is nil for MemoryStore
// and holds the fake client behind a DynamoDBStore.
type contractEnv struct {
	vs    VaultStore
	clock *contractClock
	fake  *fakeDynamoDB
}

func contractNewMemoryEnv() *contractEnv {
	clock := newContractClock(contractEpoch())
	s := NewMemoryStore()
	s.now = clock.Now
	return &contractEnv{vs: s, clock: clock}
}

func contractNewDynamoEnv() *contractEnv {
	clock := newContractClock(contractEpoch())
	fake := newFakeDynamoDB()
	s := NewDynamoDBStore(fake, contractTable)
	s.now = clock.Now
	return &contractEnv{vs: s, clock: clock, fake: fake}
}

// contractAdapter names one adapter and builds a fresh environment for it.
type contractAdapter struct {
	name   string
	newEnv func() *contractEnv
}

func contractAdapters() []contractAdapter {
	return []contractAdapter{
		{name: "MemoryStore", newEnv: contractNewMemoryEnv},
		{name: "DynamoDBStore", newEnv: contractNewDynamoEnv},
	}
}

// contractRun runs body once per adapter, each in its own subtest with a fresh
// store.
func contractRun(t *testing.T, body func(t *testing.T, env *contractEnv)) {
	t.Helper()
	for _, a := range contractAdapters() {
		a := a
		t.Run(a.name, func(t *testing.T) { body(t, a.newEnv()) })
	}
}

// contractCheck runs prop as a rapid property once per adapter. Every generated
// input gets a fresh store, so one check cannot see another's records.
func contractCheck(t *testing.T, prop func(rt *rapid.T, env *contractEnv)) {
	t.Helper()
	for _, a := range contractAdapters() {
		a := a
		t.Run(a.name, func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) { prop(rt, a.newEnv()) })
		})
	}
}

// contractCheckDynamo is contractCheck for properties that only apply to
// DynamoDBStore. env.fake is never nil in prop.
func contractCheckDynamo(t *testing.T, prop func(rt *rapid.T, env *contractEnv)) {
	t.Helper()
	t.Run("DynamoDBStore", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) { prop(rt, contractNewDynamoEnv()) })
	})
}

// contractStates returns the six vault states.
func contractStates() []VaultState {
	return []VaultState{
		StateInstantPass,
		StateRealityCheck,
		StateLocked15M,
		StateGuardianApproved,
		StateVetoed,
		StateExpired,
	}
}

func contractStateGen() *rapid.Generator[VaultState] {
	return rapid.SampledFrom(contractStates())
}

// contractIDGen draws vault ids made of lower-case letters only. Letters-only
// ids cannot contain a digit string, an uppercase mask or a mask symbol, so
// the error-leak property can look for those values without false matches.
func contractIDGen() *rapid.Generator[string] {
	return rapid.StringMatching(`^[a-z]{6,16}$`)
}

// Valid masked identifier generators, one per format in mask.go.
func contractAccountGen() *rapid.Generator[string] {
	return rapid.StringMatching(`^XXXX-XXXX-[0-9]{4}$`)
}

func contractPhoneGen() *rapid.Generator[string] {
	return rapid.StringMatching(`^\+91-XXXXXX[0-9]{4}$`)
}

func contractUPIGen() *rapid.Generator[string] {
	return rapid.StringMatching(`^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]{1,12}$`)
}

// contractOptional draws either an empty string or a value from g. Empty
// identifier fields are allowed (Requirement 12.4).
func contractOptional(g *rapid.Generator[string]) *rapid.Generator[string] {
	return rapid.OneOf(rapid.Just(""), g)
}

// contractReasonGen draws a plain-language reason: empty or printable text.
func contractReasonGen() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.Just(""),
		rapid.StringMatching(`^[ -~]{1,80}$`),
	)
}

func contractAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 450, 1999, 2000, 9999, 10000, 18000, 24999, 25000, 85000, math.MaxInt32}),
		rapid.Int64Range(1, 10000000),
	)
}

// contractRecordGen draws a valid VaultRecord: masked or empty identifiers,
// whole-second times and an ExpiresAt that lies in the future of
// contractEpoch. Whole seconds matter because the unixtime tag drops
// sub-second precision.
func contractRecordGen() *rapid.Generator[VaultRecord] {
	return rapid.Custom(func(t *rapid.T) VaultRecord {
		epoch := contractEpoch()
		createdAgo := rapid.Int64Range(0, 86400).Draw(t, "created_ago_s")
		expiresIn := rapid.Int64Range(3600, 30*86400).Draw(t, "expires_in_s")

		rec := VaultRecord{
			ID:                 contractIDGen().Draw(t, "id"),
			State:              contractStateGen().Draw(t, "state"),
			RiskScore:          rapid.IntRange(0, 100).Draw(t, "risk_score"),
			PayerAccountMasked: contractOptional(contractAccountGen()).Draw(t, "payer_account_masked"),
			BeneficiaryMasked:  contractOptional(contractAccountGen()).Draw(t, "beneficiary_masked"),
			UPIMasked:          contractOptional(contractUPIGen()).Draw(t, "upi_masked"),
			PhoneMasked:        contractOptional(contractPhoneGen()).Draw(t, "phone_masked"),
			AmountINR:          contractAmountGen().Draw(t, "amount_inr"),
			CreatedAt:          epoch.Add(-time.Duration(createdAgo) * time.Second),
			Version:            rapid.IntRange(0, 100).Draw(t, "version"),
			ExpiresAt:          epoch.Add(time.Duration(expiresIn) * time.Second),
		}
		// A locked vault has a deadline. A zero LockDeadline and TokenExpiry
		// mean "never locked" and must round-trip like any other value. A
		// deadline earlier than the token expiry is a demo fast-forward.
		if rapid.Bool().Draw(t, "locked") {
			tokenExpiry := rec.CreatedAt.Add(15 * time.Minute).Unix()
			rec.TokenExpiry = tokenExpiry
			rec.LockDeadline = rapid.OneOf(
				rapid.Just(tokenExpiry),
				rapid.Int64Range(rec.CreatedAt.Unix(), tokenExpiry),
			).Draw(t, "lock_deadline")
		}
		rec.Settled = rapid.Bool().Draw(t, "settled")
		rec.Reason = contractReasonGen().Draw(t, "reason")
		rec.EvidenceSummary = rapid.OneOf(
			rapid.Just(""),
			rapid.StringMatching(`^[ -~]{1,120}$`),
		).Draw(t, "evidence")
		return rec
	})
}

// contractNormalize puts every time of rec in UTC so that two records that
// describe the same instants compare equal with reflect.DeepEqual. DynamoDBStore
// returns times in the local zone.
func contractNormalize(rec VaultRecord) VaultRecord {
	rec.CreatedAt = rec.CreatedAt.UTC()
	rec.ExpiresAt = rec.ExpiresAt.UTC()
	return rec
}

// contractEqual reports whether a and b hold the same values. It compares every
// field, including fields added later, and compares times by instant.
func contractEqual(a, b VaultRecord) bool {
	return reflect.DeepEqual(contractNormalize(a), contractNormalize(b))
}

// contractMustCreate stores rec and fails the test if that returns an error.
func contractMustCreate(t rapid.TB, vs VaultStore, rec VaultRecord) {
	t.Helper()
	if err := vs.Create(context.Background(), rec); err != nil {
		t.Fatalf("Create(id %q) returned %v, want nil", rec.ID, err)
	}
}

// contractMustGet reads id and fails the test if that returns an error.
func contractMustGet(t rapid.TB, vs VaultStore, id string) VaultRecord {
	t.Helper()
	got, err := vs.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get(id %q) returned %v, want nil", id, err)
	}
	return got
}

// contractWantRecord fails the test unless Get(id) returns want.
func contractWantRecord(t rapid.TB, vs VaultStore, id string, want VaultRecord) {
	t.Helper()
	got := contractMustGet(t, vs, id)
	if !contractEqual(got, want) {
		t.Fatalf("Get(id %q) = %+v, want %+v", id, got, want)
	}
}

// TestContractGeneratorsProduceValidRecords checks the harness itself: every
// generated record passes ValidateMasked, has whole-second times and expires
// after the contract epoch, and both adapters accept it.
func TestContractGeneratorsProduceValidRecords(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		rec := contractRecordGen().Draw(rt, "record")

		if err := ValidateMasked(rec); err != nil {
			rt.Fatalf("generated record fails ValidateMasked: %v (%+v)", err, rec)
		}
		for name, ts := range map[string]time.Time{
			"CreatedAt": rec.CreatedAt,
			"ExpiresAt": rec.ExpiresAt,
		} {
			if ts.Nanosecond() != 0 {
				rt.Fatalf("%s has a sub-second part: %v", name, ts)
			}
		}
		if !rec.ExpiresAt.After(contractEpoch()) {
			rt.Fatalf("ExpiresAt %v is not after the epoch %v", rec.ExpiresAt, contractEpoch())
		}
		contractMustCreate(rt, env.vs, rec)
	})
}
