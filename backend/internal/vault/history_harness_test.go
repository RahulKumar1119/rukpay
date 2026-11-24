package vault

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file is the harness of the history tests: store wrappers that log the
// order of every call and can be told to fail, an environment that wires them to
// one MemoryStore, and the model of the History_Snapshot that the property tests
// compare the service with. It changes nothing in harness_test.go.

// opLog is an ordered, concurrency-safe log of the store operations of a test.
type opLog struct {
	mu  sync.Mutex
	ops []string
}

func (l *opLog) add(op string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = append(l.ops, op)
}

func (l *opLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ops...)
}

func (l *opLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = nil
}

// Operation names in an opLog.
const (
	opRead   = "read"
	opCreate = "create"
	opAppend = "append"
)

// logVaultStore wraps a VaultStore. It logs every Create before it runs, counts
// the calls, and fails Create when failCreate is set. Get and UpdateIf pass
// through to the embedded store.
type logVaultStore struct {
	store.VaultStore
	log *opLog

	mu         sync.Mutex
	failCreate error
	creates    int
}

func (l *logVaultStore) Create(ctx context.Context, rec store.VaultRecord) error {
	l.log.add(opCreate)
	l.mu.Lock()
	l.creates++
	fail := l.failCreate
	l.mu.Unlock()
	if fail != nil {
		return fail
	}
	return l.VaultStore.Create(ctx, rec)
}

// histRead is one RecentHistory call.
type histRead struct {
	payer string
	limit int
}

// histAppend is one AppendHistory call.
type histAppend struct {
	payer string
	ev    store.HistoryEvent
}

// logHistory wraps a HistoryStore. It logs and records every call and fails reads
// or appends when readErr or appendErr is set. A failed call never reaches the
// wrapped store.
type logHistory struct {
	inner store.HistoryStore
	log   *opLog

	mu        sync.Mutex
	readErr   error
	appendErr error
	reads     []histRead
	appends   []histAppend
}

func (h *logHistory) RecentHistory(ctx context.Context, payer string, limit int) ([]store.HistoryEvent, error) {
	h.log.add(opRead)
	h.mu.Lock()
	h.reads = append(h.reads, histRead{payer: payer, limit: limit})
	fail := h.readErr
	h.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	return h.inner.RecentHistory(ctx, payer, limit)
}

func (h *logHistory) AppendHistory(ctx context.Context, payer string, ev store.HistoryEvent) error {
	h.log.add(opAppend)
	h.mu.Lock()
	h.appends = append(h.appends, histAppend{payer: payer, ev: ev})
	fail := h.appendErr
	h.mu.Unlock()
	if fail != nil {
		return fail
	}
	return h.inner.AppendHistory(ctx, payer, ev)
}

func (h *logHistory) readCalls() []histRead {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]histRead(nil), h.reads...)
}

func (h *logHistory) appendCalls() []histAppend {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]histAppend(nil), h.appends...)
}

// histEnv is a vault service wired to one MemoryStore through the two logging
// wrappers, with a clock that counts how often the service reads it.
type histEnv struct {
	svc    *Service
	mem    *store.MemoryStore
	vaults *logVaultStore
	hist   *logHistory
	log    *opLog
	clock  *testClock
	obs    *recObserver

	// clockReads counts the calls the service makes to its clock. The store has
	// its own clock, which is not counted.
	clockReads atomic.Int64
}

func newHistEnv(demo bool) *histEnv {
	clock := newTestClock()
	mem := store.NewMemoryStoreWithClock(clock.Now)
	log := &opLog{}
	e := &histEnv{
		mem:    mem,
		vaults: &logVaultStore{VaultStore: mem, log: log},
		hist:   &logHistory{inner: mem, log: log},
		log:    log,
		clock:  clock,
		obs:    &recObserver{},
	}
	counted := func() time.Time {
		e.clockReads.Add(1)
		return clock.Now()
	}
	e.svc = New(e.vaults, counted, demo, testIssuer{}, e.obs)
	e.svc.SetHistory(e.hist)
	return e
}

// epoch is the clock time of a new environment, in epoch seconds.
func histEpoch() int64 { return harnessEpoch().Unix() }

// seed writes a History_Event straight into the MemoryStore, bypassing the
// service and the wrappers, so a test controls exactly what is stored.
func (e *histEnv) seed(tb rapid.TB, ev store.HistoryEvent) {
	tb.Helper()
	if err := e.mem.AppendHistory(context.Background(), store.PayerKey, ev); err != nil {
		tb.Fatalf("seed event: %v", err)
	}
}

// stored returns every unexpired History_Event of the payer in the MemoryStore,
// oldest first, read with a limit far above the number stored.
func (e *histEnv) stored(tb rapid.TB) []store.HistoryEvent {
	tb.Helper()
	events, err := e.mem.RecentHistory(context.Background(), store.PayerKey, 1<<20)
	if err != nil {
		tb.Fatalf("read stored history: %v", err)
	}
	return events
}

// resetCalls forgets the reads and appends recorded so far.
func (h *logHistory) resetCalls() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reads, h.appends = nil, nil
}

// record reads the stored vault record id, bypassing the service.
func (e *histEnv) record(tb rapid.TB, id string) store.VaultRecord {
	tb.Helper()
	rec, err := e.mem.Get(context.Background(), id)
	if err != nil {
		tb.Fatalf("store Get(%s) = %v, want nil", id, err)
	}
	return rec
}

// evalResult is what one Evaluate call produced and what the stores saw while it
// ran.
type evalResult struct {
	view       View
	err        error
	ops        []string // the store operations, in order
	clockReads int64    // how often the service read its clock
}

// evaluate runs one evaluation with a fresh operation log and clock count.
func (e *histEnv) evaluate(req EvaluateRequest) evalResult {
	e.log.reset()
	e.hist.resetCalls()
	e.clockReads.Store(0)
	v, err := e.svc.Evaluate(context.Background(), req)
	return evalResult{view: v, err: err, ops: e.log.list(), clockReads: e.clockReads.Load()}
}

// wantRef is the Beneficiary_Ref of a request, written out again from
// Requirement 12.5: the first non-empty of beneficiary_masked, upi_masked and
// phone_masked, and never the payer's own account.
func wantRef(req EvaluateRequest) string {
	switch {
	case req.BeneficiaryMasked != "":
		return req.BeneficiaryMasked
	case req.UPIMasked != "":
		return req.UPIMasked
	case req.PhoneMasked != "":
		return req.PhoneMasked
	}
	return ""
}

// retained reports whether an event at time at is within the History_Retention
// of an evaluation at time now, using unlimited-size integers: the event is kept
// when it is later than now, or when now minus at is less than 604,800 seconds
// (Requirement 12.4). It shares no code with the service.
func retained(at, now int64) bool {
	if at > now {
		return true
	}
	age := new(big.Int).Sub(big.NewInt(now), big.NewInt(at))
	return age.Cmp(big.NewInt(604_800)) < 0
}

// modelSnapshot is the History_Snapshot that Requirement 12.4 describes for the
// events stored and an evaluation at time now: the store lists the 100 events
// with the greatest (time, id) pairs, hides the ones past their expiry, and the
// service keeps the ones within the retention, oldest first.
func modelSnapshot(stored []store.HistoryEvent, now int64) []risk.HistoryEvent {
	sorted := append([]store.HistoryEvent(nil), stored...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].At != sorted[j].At {
			return sorted[i].At < sorted[j].At
		}
		return strings.Compare(sorted[i].ID, sorted[j].ID) < 0
	})
	if len(sorted) > store.SnapshotLimit {
		sorted = sorted[len(sorted)-store.SnapshotLimit:]
	}

	out := []risk.HistoryEvent{}
	for _, ev := range sorted {
		if ev.ExpiresAt != 0 && ev.ExpiresAt < now {
			continue // hidden by the store
		}
		if !retained(ev.At, now) {
			continue // older than the retention
		}
		out = append(out, risk.HistoryEvent{At: ev.At, AmountINR: ev.AmountINR, BeneficiaryRef: ev.BeneficiaryRef})
	}
	return out
}

// Pools of masked identifiers for generated requests and generated history. The
// same few values recur, so repeat and pattern signals fire often.
var (
	poolAccounts = []string{"XXXX-XXXX-1111", "XXXX-XXXX-2222"}
	poolUPIs     = []string{"r***@okaxis", "s***@okicici"}
	poolPhones   = []string{"+91-XXXXXX3210", "+91-XXXXXX9999"}
)

// poolRefs lists every Beneficiary_Ref a generated event can carry: none, or one
// of the pooled masked identifiers.
func poolRefs() []string {
	refs := []string{"", ""}
	refs = append(refs, poolAccounts...)
	refs = append(refs, poolUPIs...)
	refs = append(refs, poolPhones...)
	return refs
}

// pooledIdentifiersGen draws a request's identifiers from the pools: each of the
// three recipient fields is empty about half the time, and the payer's own
// account is a value that is in no pool, so it can never match an event.
func pooledIdentifiersGen() *rapid.Generator[EvaluateRequest] {
	pick := func(pool []string) *rapid.Generator[string] {
		return rapid.SampledFrom(append([]string{"", ""}, pool...))
	}
	return rapid.Custom(func(t *rapid.T) EvaluateRequest {
		return EvaluateRequest{
			PayerAccountMasked: rapid.SampledFrom([]string{"", "XXXX-XXXX-4821"}).Draw(t, "payer_account_masked"),
			BeneficiaryMasked:  pick(poolAccounts).Draw(t, "beneficiary_masked"),
			UPIMasked:          pick(poolUPIs).Draw(t, "upi_masked"),
			PhoneMasked:        pick(poolPhones).Draw(t, "phone_masked"),
		}
	})
}

// eventGen draws a stored History_Event with the given id. The time is the test
// epoch minus an age that sits on the boundaries of every window and of the
// retention, or is drawn widely, or is an extreme int64. The expiry is none, the
// real retention expiry, or never, so the store hides some old events and the
// service has to judge the rest by their time.
func eventGen(id string) *rapid.Generator[store.HistoryEvent] {
	epoch := histEpoch()
	return rapid.Custom(func(t *rapid.T) store.HistoryEvent {
		var at int64
		switch rapid.IntRange(0, 9).Draw(t, "time_kind") {
		case 0:
			at = rapid.SampledFrom([]int64{math.MinInt64, math.MinInt64 + 1, math.MaxInt64, math.MaxInt64 - 1}).Draw(t, "extreme_at")
		case 1, 2, 3:
			age := rapid.SampledFrom([]int64{-3600, -1, 0, 1, 598, 599, 600, 601, 3599, 3600, 3601, 86399, 86400, 86401, 604799, 604800, 604801, 700000}).Draw(t, "boundary_age")
			at = epoch - age
		case 4, 5:
			at = epoch - rapid.Int64Range(-100, 4000).Draw(t, "recent_age")
		default:
			at = epoch - rapid.Int64Range(0, 1_000_000).Draw(t, "wide_age")
		}
		amount := rapid.OneOf(
			rapid.SampledFrom([]int64{0, 1, 100, 450, 999, 1000, 1001, 2000, 9999, 10000, 85000, math.MaxInt64}),
			rapid.Int64Range(1, 200000),
		).Draw(t, "amount")
		var expires int64
		switch rapid.IntRange(0, 2).Draw(t, "expiry_kind") {
		case 0:
			expires = 0
		case 1:
			expires = store.HistoryExpiry(at)
		default:
			expires = math.MaxInt64
		}
		return store.HistoryEvent{
			ID:             id,
			At:             at,
			AmountINR:      amount,
			BeneficiaryRef: rapid.SampledFrom(poolRefs()).Draw(t, "beneficiary_ref"),
			ExpiresAt:      expires,
		}
	})
}

// seedID names the i-th seeded event. The random prefix varies the byte-wise
// order of ids at equal times, and the index keeps every id unique.
func seedID(prefix string, i int) string { return fmt.Sprintf("seed-%s-%03d", prefix, i) }
