package vault

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file holds Property 17: history never decides whether an evaluation
// succeeds. Each failure path leaves the caller with what it would have had
// without that failure.

// failureGen draws the error a failing store returns: arbitrary text, a context
// error, or one of the store's own sentinels, bare or wrapped.
func failureGen() *rapid.Generator[error] {
	return rapid.OneOf(
		rapid.Map(rapid.StringN(0, 40, -1), func(s string) error { return errors.New(s) }),
		rapid.SampledFrom([]error{
			context.DeadlineExceeded,
			context.Canceled,
			store.ErrNotFound,
			store.ErrAlreadyExists,
			store.ErrConditionFailed,
			store.ErrUnmaskedIdentifier,
			fmt.Errorf("history: %w", store.ErrNotFound),
		}),
	)
}

// normalised returns v and rec with the random vault id, and the guardian token
// that is made from it, taken out, so that two evaluations can be compared.
func normalised(v View, rec store.VaultRecord) (View, store.VaultRecord) {
	v.ID, v.GuardianToken = "", ""
	rec.ID = ""
	return v, rec
}

// drawHistoryCase draws a valid request and a stored history for one run.
func drawHistoryCase(rt *rapid.T) (EvaluateRequest, []store.HistoryEvent) {
	req := pooledIdentifiersGen().Draw(rt, "identifiers")
	req.Signals = explainSignalsGen().Draw(rt, "signals")

	n := rapid.IntRange(0, 20).Draw(rt, "seed_count")
	seeds := make([]store.HistoryEvent, 0, n)
	for i := 0; i < n; i++ {
		seeds = append(seeds, eventGen(seedID("r", i)).Draw(rt, fmt.Sprintf("seed_%d", i)))
	}
	return req, seeds
}

// Feature: risk-intelligence, Property 17: Every failure path gives today's result
//
// A snapshot that cannot be read, for any error, counts as empty. The evaluation
// still creates its vault, with no error, and the vault is exactly the one the
// shipped service creates when it has no history at all: the same state, score,
// Explanation, reason and every other field. The read is tried once, and the
// payment is still recorded afterwards as the one event of the evaluation.
//
// **Validates: Requirements 12.7**
func TestFailedSnapshotReadGivesTheResultOfAnEmptySnapshot(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req, seeds := drawHistoryCase(rt)
		failure := failureGen().Draw(rt, "read_error")

		e := newHistEnv(false)
		for _, ev := range seeds {
			e.seed(rt, ev)
		}
		e.hist.readErr = failure
		control := newEnv(false) // the service as it was before history existed

		res := e.evaluate(req)
		if res.err != nil {
			rt.Fatalf("Evaluate after a failed read = %v, want nil", res.err)
		}
		want := control.createWith(rt, req)

		gotView, gotRec := normalised(res.view, e.record(rt, res.view.ID))
		wantView, wantRec := normalised(want, control.record(rt, want.ID))
		if !reflect.DeepEqual(gotView, wantView) || !reflect.DeepEqual(gotRec, wantRec) {
			rt.Fatalf("after a failed read the vault is %+v, want what no history gives: %+v", gotRec, wantRec)
		}

		if !reflect.DeepEqual(res.ops, []string{opRead, opCreate, opAppend}) {
			rt.Fatalf("store operations %v, want the read, the vault write and the event write", res.ops)
		}
		if appends := e.hist.appendCalls(); len(appends) != 1 || appends[0].ev.AmountINR != req.AmountINR || appends[0].ev.BeneficiaryRef != wantRef(req) {
			rt.Fatalf("event writes %+v, want one for this payment", appends)
		}
	})
}

// Feature: risk-intelligence, Property 17: Every failure path gives today's result
//
// An event that cannot be written, for any error, changes nothing the caller sees:
// no error, and the same vault, score, state, Explanation and reason as an
// evaluation over the same history whose write succeeds. The write is tried once
// and never repeated. The failed evaluation leaves no event behind, while the
// healthy one leaves exactly one.
//
// **Validates: Requirements 2.11, 12.8**
func TestFailedEventWriteChangesNothingTheCallerSees(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req, seeds := drawHistoryCase(rt)
		failure := failureGen().Draw(rt, "append_error")

		failing, healthy := newHistEnv(false), newHistEnv(false)
		for _, ev := range seeds {
			failing.seed(rt, ev)
			healthy.seed(rt, ev)
		}
		failing.hist.appendErr = failure

		res := failing.evaluate(req)
		if res.err != nil {
			rt.Fatalf("Evaluate after a failed event write = %v, want nil", res.err)
		}
		want := healthy.evaluate(req)
		if want.err != nil {
			rt.Fatalf("healthy Evaluate = %v, want nil", want.err)
		}

		gotView, gotRec := normalised(res.view, failing.record(rt, res.view.ID))
		wantView, wantRec := normalised(want.view, healthy.record(rt, want.view.ID))
		if !reflect.DeepEqual(gotView, wantView) || !reflect.DeepEqual(gotRec, wantRec) {
			rt.Fatalf("after a failed event write the vault is %+v, want %+v", gotRec, wantRec)
		}

		if !reflect.DeepEqual(res.ops, []string{opRead, opCreate, opAppend}) {
			rt.Fatalf("store operations %v, want one read, one vault write and one event write", res.ops)
		}
		if got := failing.hist.appendCalls(); len(got) != 1 {
			rt.Fatalf("%d event writes were tried, want 1 and no retry", len(got))
		}
		if got, wantN := len(failing.stored(rt)), len(healthy.stored(rt))-1; got != wantN {
			rt.Fatalf("the failed evaluation left %d stored events, want %d: the seeded ones and no partial event", got, wantN)
		}
	})
}

// Feature: risk-intelligence, Property 17: Every failure path gives today's result
//
// A vault that cannot be stored returns the store's own error, with no View, and
// records no event. The snapshot was read before the vault write, so the payment
// was scored, but nothing about it is kept: no event write is tried, no change is
// published, and the stored history is as it was.
//
// **Validates: Requirements 2.11, 12.2**
func TestFailedVaultWriteRecordsNoEvent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req, seeds := drawHistoryCase(rt)
		failure := failureGen().Draw(rt, "create_error")

		e := newHistEnv(false)
		for _, ev := range seeds {
			e.seed(rt, ev)
		}
		before := e.stored(rt)
		e.vaults.failCreate = failure

		res := e.evaluate(req)
		if !errors.Is(res.err, failure) {
			rt.Fatalf("Evaluate = %v, want the store's own error %v", res.err, failure)
		}
		if !reflect.DeepEqual(res.view, View{}) {
			rt.Fatalf("a failed evaluation returned the View %+v, want none", res.view)
		}
		if !reflect.DeepEqual(res.ops, []string{opRead, opCreate}) {
			rt.Fatalf("store operations %v, want the read and the failed vault write only", res.ops)
		}
		if got := e.hist.appendCalls(); len(got) != 0 {
			rt.Fatalf("a failed vault write still tried the event writes %+v", got)
		}
		if after := e.stored(rt); !reflect.DeepEqual(after, before) {
			rt.Fatalf("the stored history changed from %+v to %+v", before, after)
		}
		if e.obs.count() != 0 {
			rt.Fatalf("a failed evaluation published %d changes", e.obs.count())
		}
	})
}

// invalidRequestGen draws a request that must be rejected, and returns it with a
// text for the failure: a signal that is out of range, a recipient or payer field
// that is not masked, or both, in which case the identifier is reported first.
func invalidRequestGen() *rapid.Generator[EvaluateRequest] {
	rawValues := []string{
		"123456789012", "rahul@okaxis", "9876543210", "+919876543210", " XXXX-XXXX-4821",
		"XXXX-XXXX-4821\n", "XXXX-XXXX-48211", "xxxx-xxxx-4821", "r**@okaxis", "OTP 123456",
	}
	return rapid.Custom(func(t *rapid.T) EvaluateRequest {
		req := EvaluateRequest{Signals: signalsGen().Draw(t, "signals")}
		kind := rapid.IntRange(0, 2).Draw(t, "kind")

		if kind != 1 { // an invalid signal
			switch rapid.IntRange(0, 2).Draw(t, "bad_signal") {
			case 0:
				req.AmountINR = rapid.SampledFrom([]int64{0, -1, math.MinInt64}).Draw(t, "amount")
			case 1:
				req.BeneficiaryAgeHrs = rapid.SampledFrom([]int64{-1, math.MinInt64}).Draw(t, "age")
			default:
				req.RapidRetryCount = rapid.SampledFrom([]int{-1, math.MinInt}).Draw(t, "retries")
			}
		}
		if kind != 0 { // an unmasked identifier
			value := rapid.OneOf(rapid.SampledFrom(rawValues), rapid.StringN(1, 30, -1)).Draw(t, "raw")
			switch rapid.IntRange(0, 3).Draw(t, "field") {
			case 0:
				req.PayerAccountMasked = value
			case 1:
				req.BeneficiaryMasked = value
			case 2:
				req.UPIMasked = value
			default:
				req.PhoneMasked = value
			}
		}
		return req
	}).Filter(func(req EvaluateRequest) bool {
		// A drawn string can happen to be a masked value, and a drawn signal a
		// valid one. Keep only the requests that really are rejected.
		rec := store.VaultRecord{
			PayerAccountMasked: req.PayerAccountMasked, BeneficiaryMasked: req.BeneficiaryMasked,
			UPIMasked: req.UPIMasked, PhoneMasked: req.PhoneMasked,
		}
		return store.ValidateMasked(rec) != nil || risk.Validate(req.Signals) != nil
	})
}

// Feature: risk-intelligence, Property 17: Every failure path gives today's result
//
// A request that fails validation returns the error it always returned: an
// unmasked identifier first, then an invalid signal. It reads nothing and writes
// nothing: no snapshot read, no vault write, no event, no change published, and
// no raw value reaches the history store.
//
// **Validates: Requirements 2.11, 12.2**
func TestInvalidRequestReadsAndRecordsNothing(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req := invalidRequestGen().Draw(rt, "request")
		_, seeds := drawHistoryCase(rt)

		e := newHistEnv(false)
		for _, ev := range seeds {
			e.seed(rt, ev)
		}
		before := e.stored(rt)

		res := e.evaluate(req)

		maskErr := store.ValidateMasked(store.VaultRecord{
			PayerAccountMasked: req.PayerAccountMasked, BeneficiaryMasked: req.BeneficiaryMasked,
			UPIMasked: req.UPIMasked, PhoneMasked: req.PhoneMasked,
		})
		var invalid *risk.ValidationError
		switch {
		case maskErr != nil:
			if !errors.Is(res.err, store.ErrUnmaskedIdentifier) {
				rt.Fatalf("Evaluate = %v, want ErrUnmaskedIdentifier first", res.err)
			}
		case !errors.As(res.err, &invalid):
			rt.Fatalf("Evaluate = %v, want a *risk.ValidationError", res.err)
		case invalid.Field != risk.Validate(req.Signals).(*risk.ValidationError).Field:
			rt.Fatalf("the error names %q, want the field Validate names", invalid.Field)
		}
		if !reflect.DeepEqual(res.view, View{}) {
			rt.Fatalf("a rejected request returned the View %+v", res.view)
		}
		if len(res.ops) != 0 {
			rt.Fatalf("a rejected request touched the stores: %v", res.ops)
		}
		if after := e.stored(rt); !reflect.DeepEqual(after, before) {
			rt.Fatalf("the stored history changed from %+v to %+v", before, after)
		}
		if e.obs.count() != 0 {
			rt.Fatalf("a rejected request published %d changes", e.obs.count())
		}
	})
}

// A failure of the history store and a failure of the vault store at the same
// time give the vault store's error, never a success, and still record nothing.
func TestBothStoresFailingReturnsTheVaultError(t *testing.T) {
	e := newHistEnv(false)
	vaultErr := errors.New("vault store is down")
	e.vaults.failCreate = vaultErr
	e.hist.readErr = errors.New("history store is down")
	e.hist.appendErr = errors.New("history store is down")

	res := e.evaluate(EvaluateRequest{Signals: signalsLocked()})
	if !errors.Is(res.err, vaultErr) {
		t.Fatalf("Evaluate = %v, want %v", res.err, vaultErr)
	}
	if !reflect.DeepEqual(res.ops, []string{opRead, opCreate}) {
		t.Fatalf("store operations %v, want the read and the failed vault write only", res.ops)
	}
}

// The history store never receives a value that is not masked or empty: not an
// identifier of the payer, and not a field the request carries that the type does
// not declare. Every event written during a run has a masked or empty ref.
func TestHistoryStoreOnlyEverReceivesMaskedRefs(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		e := newHistEnv(false)
		for i, n := 0, rapid.IntRange(1, 8).Draw(rt, "evaluations"); i < n; i++ {
			req := pooledIdentifiersGen().Draw(rt, "identifiers")
			req.Signals = explainSignalsGen().Draw(rt, "signals")
			if res := e.evaluate(req); res.err != nil {
				rt.Fatalf("Evaluate = %v, want nil", res.err)
			}
			for _, a := range e.hist.appendCalls() {
				if a.ev.BeneficiaryRef != "" && store.MaskedFormat(a.ev.BeneficiaryRef) == store.MaskNone {
					rt.Fatalf("the history store was given the ref %q, which is not masked", a.ev.BeneficiaryRef)
				}
				if a.ev.BeneficiaryRef == req.PayerAccountMasked && req.PayerAccountMasked != "" && wantRef(req) != req.PayerAccountMasked {
					rt.Fatalf("the payer's own account %q was written as the recipient", a.ev.BeneficiaryRef)
				}
			}
		}
	})
}
