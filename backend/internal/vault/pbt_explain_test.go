package vault

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file holds the vault half of Property 13. The store half (round trips
// through both adapters) is in store/pbt_explanation_test.go.

// explainSignalsGen draws valid signals across every tier boundary, and about
// one time in three signals that earn no points at all, so that the empty
// Explanation is reached as often as the others.
func explainSignalsGen() *rapid.Generator[risk.Signals] {
	zero := rapid.Custom(func(t *rapid.T) risk.Signals {
		return risk.Signals{
			AmountINR:         rapid.Int64Range(1, 1999).Draw(t, "zero_amount_inr"),
			BeneficiaryAgeHrs: rapid.Int64Range(72, 5000).Draw(t, "zero_beneficiary_age_hours"),
			RapidRetryCount:   rapid.IntRange(0, 1).Draw(t, "zero_rapid_retry_count"),
		}
	})
	return rapid.OneOf(signalsGen(), signalsGen(), zero)
}

// contributionGen draws a Contribution that the engine would never write, so
// that a read which rebuilt the Explanation from anything else would show.
func contributionGen() *rapid.Generator[store.Contribution] {
	return rapid.Custom(func(t *rapid.T) store.Contribution {
		return store.Contribution{
			Signal: rapid.StringN(1, 20, -1).Draw(t, "signal"),
			Points: rapid.IntRange(-30, 40).Draw(t, "points"),
			Reason: rapid.StringN(1, 60, -1).Draw(t, "reason"),
		}
	})
}

// explanationShape names what the record holds after the test has prepared it.
type explanationShape string

const (
	shapeAssessed explanationShape = "assessed" // as Evaluate wrote it
	shapeAbsent   explanationShape = "absent"   // a record from before the feature: nil
	shapeEmpty    explanationShape = "empty"    // present and empty
	shapeCustom   explanationShape = "custom"   // present with generated Contributions
)

// explanationSum adds up the points of list.
func explanationSum(list []store.Contribution) int {
	sum := 0
	for _, c := range list {
		sum += c.Points
	}
	return sum
}

// prepareExplanation puts the record of vault id into shape and returns the
// Explanation and the Risk_Score the record must keep from then on. For the
// assessed shape it leaves the record as Evaluate wrote it. For the other shapes
// it rewrites the stored Explanation, and sets the score to the sum of the
// points so that Requirement 2.8 can be checked on every View. An absent
// Explanation needs no such sum.
func prepareExplanation(rt *rapid.T, e *env, id string, shape explanationShape) ([]store.Contribution, int) {
	rt.Helper()
	rec := e.record(rt, id)
	if shape == shapeAssessed {
		return rec.Explanation, rec.RiskScore
	}

	next := rec
	switch shape {
	case shapeAbsent:
		next.Explanation = nil
	case shapeEmpty:
		next.Explanation = []store.Contribution{}
		next.RiskScore = 0
	case shapeCustom:
		next.Explanation = rapid.SliceOfN(contributionGen(), 1, 8).Draw(rt, "custom_explanation")
		next.RiskScore = explanationSum(next.Explanation)
	default:
		rt.Fatalf("unknown shape %q", shape)
	}
	if err := e.mem.UpdateIf(context.Background(), id, rec.State, next); err != nil {
		rt.Fatalf("rewrite the Explanation: %v", err)
	}
	return next.Explanation, next.RiskScore
}

// explanationKey returns the raw `explanation` member of the JSON form of v and
// whether the key is there at all.
func explanationKey(rt *rapid.T, v View) (json.RawMessage, bool) {
	rt.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		rt.Fatalf("marshal View: %v", err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil {
		rt.Fatalf("View %s is not a JSON object: %v", body, err)
	}
	raw, ok := members["explanation"]
	return raw, ok
}

// checkExplanationView fails the test unless v shows the stored Explanation want
// as Requirement 2 describes. A nil want means the record has none, so the key
// must be absent. An empty want must be written as [], never as null. Otherwise
// the Contributions must equal want in count, order and values, hold exactly the
// three keys signal, points and reason, and add up to the Risk_Score.
func checkExplanationView(rt *rapid.T, where string, v View, want []store.Contribution) {
	rt.Helper()
	raw, present := explanationKey(rt, v)

	if want == nil {
		if present || v.Explanation != nil {
			rt.Fatalf("%s: a record with no Explanation gave the key %s (pointer %v), want the key left out", where, raw, v.Explanation != nil)
		}
		return
	}
	if !present || v.Explanation == nil {
		rt.Fatalf("%s: a record with an Explanation of %d Contributions gave no key", where, len(want))
	}
	if len(want) == 0 && string(raw) != "[]" {
		rt.Fatalf("%s: an empty Explanation is written as %s, want []", where, raw)
	}
	if !reflect.DeepEqual(*v.Explanation, want) {
		rt.Fatalf("%s: View Explanation = %+v, want the stored %+v", where, *v.Explanation, want)
	}

	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		rt.Fatalf("%s: explanation %s is not a list of objects: %v", where, raw, err)
	}
	for i, item := range items {
		keys := make([]string, 0, len(item))
		for k := range item {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if !reflect.DeepEqual(keys, []string{"points", "reason", "signal"}) {
			rt.Fatalf("%s: Contribution %d has the keys %v, want exactly points, reason and signal", where, i, keys)
		}
	}
	var back []store.Contribution
	if err := json.Unmarshal(raw, &back); err != nil || !reflect.DeepEqual(back, want) {
		rt.Fatalf("%s: JSON %s decodes to %+v (%v), want %+v", where, raw, back, err, want)
	}

	if sum := explanationSum(*v.Explanation); sum != v.RiskScore {
		rt.Fatalf("%s: the points add up to %d, but risk_score is %d", where, sum, v.RiskScore)
	}
}

// explainGet reads the View of vault id and fails the test on an error.
func explainGet(tb rapid.TB, e *env, id string) View {
	tb.Helper()
	v, err := e.svc.Get(context.Background(), id)
	if err != nil {
		tb.Fatalf("Get(%s) = %v, want nil", id, err)
	}
	return v
}

// explainOp runs one generated operation and returns the View it produced: the
// result, or the current View that a conflict carries. A View is false for an
// "advance", which touches nothing. Any other error fails the test.
func explainOp(rt *rapid.T, e *env, id string, op seqOp) (View, bool) {
	rt.Helper()
	ctx := context.Background()
	var (
		v   View
		err error
	)
	switch op.kind {
	case "check":
		v, err = e.svc.RealityCheck(ctx, id, op.answers)
	case "approve":
		v, err = e.svc.Approve(ctx, id)
	case "veto":
		v, err = e.svc.Veto(ctx, id)
	case "fast-forward":
		v, err = e.svc.FastForward(ctx, id)
	case "reconfirm":
		v, err = e.svc.Reconfirm(ctx, id)
	case "get":
		v, err = e.svc.Get(ctx, id)
	default:
		return View{}, false
	}
	var conflict *ConflictError
	switch {
	case err == nil:
	case errors.As(err, &conflict):
		v = conflict.Current
	default:
		rt.Fatalf("%s = %v, want success or a *ConflictError", op.kind, err)
	}
	return v, true
}

// Feature: risk-intelligence, Property 13: The Explanation survives storage, state changes and the view
//
// Whatever the record holds (the Explanation Evaluate wrote, none, an empty one,
// or Contributions the engine never writes), a generated run of reality checks,
// approvals, vetoes, fast-forwards, reads, reconfirmations and clock moves
// leaves the stored Explanation and Risk_Score as they were. Every View of the
// vault, whether returned, carried by a conflict, published to the observer or
// read last, shows exactly that Explanation: the key absent for none, [] for an
// empty one, the stored list otherwise, with points that add up to risk_score.
// A custom list can only come back unchanged if the View copies the stored
// Explanation and never rebuilds it.
//
// **Validates: Requirements 2.2, 2.5, 2.7, 2.8**
func TestExplanationSurvivesStateChangesAndViews(t *testing.T) {
	shapes := []explanationShape{shapeAssessed, shapeAbsent, shapeEmpty, shapeCustom}

	rapid.Check(t, func(rt *rapid.T) {
		req := identifiersGen().Draw(rt, "identifiers")
		req.Signals = explainSignalsGen().Draw(rt, "signals")
		shape := rapid.SampledFrom(shapes).Draw(rt, "shape")
		ops := rapid.SliceOfN(seqOpGen(), 1, 30).Draw(rt, "ops")

		e := newEnv(true)
		created := e.createWith(rt, req)
		want, wantScore := prepareExplanation(rt, e, created.ID, shape)
		checkExplanationView(rt, "first read", explainGet(rt, e, created.ID), want)

		// The records expire an hour after the moment they must exist, so the
		// clock may move only so far.
		budget := int64(3000)

		for i, op := range ops {
			if op.kind == "advance" {
				if op.secs > budget {
					op.secs = budget
				}
				budget -= op.secs
				e.clock.AdvanceSeconds(op.secs)
			}
			got, ok := explainOp(rt, e, created.ID, op)

			rec := e.record(rt, created.ID)
			if !reflect.DeepEqual(rec.Explanation, want) {
				rt.Fatalf("op %d %s (shape %s): stored Explanation = %#v, want %#v", i, op.kind, shape, rec.Explanation, want)
			}
			if rec.RiskScore != wantScore {
				rt.Fatalf("op %d %s (shape %s): stored RiskScore = %d, want %d", i, op.kind, shape, rec.RiskScore, wantScore)
			}
			if ok {
				checkExplanationView(rt, "op "+strconv.Itoa(i)+" "+op.kind, got, want)
			}
		}

		final, err := e.svc.Get(context.Background(), created.ID)
		if err != nil {
			rt.Fatalf("final Get = %v, want nil", err)
		}
		checkExplanationView(rt, "final read", final, want)
		for i, published := range e.obs.all() {
			checkExplanationView(rt, "published view "+strconv.Itoa(i), published, want)
		}
	})
}

// Feature: risk-intelligence, Property 13: The Explanation survives storage, state changes and the view
//
// A new vault stores, in the same single write that creates it, the Explanation
// that risk.Assess gives for its signals, never nil and empty only for a score of
// 0. Its first View shows that Explanation. The score, the state and the vault
// reason are those of the shipped engine and of reasonFor, so the Explanation
// changes nothing that was there before. The Explanation holds none of the
// identifiers or the amount of the request, and the same signals give the same
// Explanation with or without identifiers.
//
// **Validates: Requirements 2.1, 2.3, 2.8, 2.10, 3.9, 3.11, 9.3, 23.1**
func TestNewVaultStoresTheAssessedExplanation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req := identifiersGen().Draw(rt, "identifiers")
		req.Signals = explainSignalsGen().Draw(rt, "signals")

		assessed, err := risk.Assess(risk.Input{Signals: req.Signals})
		if err != nil {
			rt.Fatalf("risk.Assess(%+v) = %v, want nil", req.Signals, err)
		}
		shipped, err := risk.Evaluate(req.Signals)
		if err != nil {
			rt.Fatalf("risk.Evaluate(%+v) = %v, want nil", req.Signals, err)
		}

		e := newEnv(false)
		v := e.createWith(rt, req)
		rec := e.record(rt, v.ID)

		if got := e.cs.writeCount(); got != 1 {
			rt.Fatalf("creating a vault made %d store writes, want the one that creates it", got)
		}
		if rec.Explanation == nil {
			rt.Fatalf("a new vault was stored with no Explanation")
		}
		if !reflect.DeepEqual(rec.Explanation, assessed.Explanation) {
			rt.Fatalf("stored Explanation = %+v, want what risk.Assess gave: %+v", rec.Explanation, assessed.Explanation)
		}
		checkExplanationView(rt, "evaluate response", v, rec.Explanation)
		if len(rec.Explanation) == 0 && rec.RiskScore != 0 {
			rt.Fatalf("an empty Explanation with a score of %d", rec.RiskScore)
		}

		// Nothing that existed before changes (Requirements 9.3, 23.1, 3.9, 3.11).
		if v.State != shipped.State || v.RiskScore != shipped.Score || rec.State != shipped.State || rec.RiskScore != shipped.Score {
			rt.Fatalf("state %s score %d (record %s %d), want %s %d from the shipped engine",
				v.State, v.RiskScore, rec.State, rec.RiskScore, shipped.State, shipped.Score)
		}
		if want := reasonFor(shipped.State, req.Signals); v.Reason != want || rec.Reason != want {
			rt.Fatalf("reason = view %q record %q, want the unchanged %q", v.Reason, rec.Reason, want)
		}

		// No identifier and no amount in the Explanation (Requirement 2.10).
		forbidden := []string{req.PayerAccountMasked, req.BeneficiaryMasked, req.UPIMasked, req.PhoneMasked, strconv.FormatInt(req.AmountINR, 10)}
		for _, c := range rec.Explanation {
			for _, text := range []string{c.Signal, c.Reason} {
				for _, f := range forbidden {
					if f != "" && strings.Contains(text, f) {
						rt.Fatalf("Contribution %+v holds %q from the request", c, f)
					}
				}
			}
		}

		// The identifiers play no part in the Explanation.
		bare := e.create(rt, req.Signals)
		if !reflect.DeepEqual(e.record(rt, bare.ID).Explanation, rec.Explanation) {
			rt.Fatalf("the same signals gave a different Explanation without identifiers")
		}
	})
}

// Feature: risk-intelligence, Property 13: The Explanation survives storage, state changes and the view
//
// A View never shares memory with the record or with another View. After any
// operation, a caller who rewrites every Contribution of the View it got changes
// nothing: not the stored record, not a View that the observer was sent for the
// same change, and not the next read.
//
// **Validates: Requirements 2.3, 2.7**
func TestExplanationInAViewIsACopy(t *testing.T) {
	scoring := explainSignalsGen().Filter(func(s risk.Signals) bool {
		a, err := risk.Assess(risk.Input{Signals: s})
		return err == nil && len(a.Explanation) > 0
	})

	rapid.Check(t, func(rt *rapid.T) {
		sig := scoring.Draw(rt, "signals")
		ops := rapid.SliceOfN(seqOpGen(), 1, 12).Draw(rt, "ops")

		e := newEnv(true)
		created := e.create(rt, sig)
		want := e.record(rt, created.ID).Explanation

		budget := int64(3000)
		for i, op := range ops {
			if op.kind == "advance" {
				if op.secs > budget {
					op.secs = budget
				}
				budget -= op.secs
				e.clock.AdvanceSeconds(op.secs)
			}
			got, ok := explainOp(rt, e, created.ID, op)
			if !ok {
				continue
			}

			// What the observer was sent so far, copied before the caller acts.
			before := e.obs.all()
			sent := make([][]store.Contribution, len(before))
			for j, pv := range before {
				sent[j] = append([]store.Contribution{}, *pv.Explanation...)
			}

			// The caller rewrites everything it was given, and drops the list.
			list := *got.Explanation
			for j := range list {
				list[j] = store.Contribution{Signal: "changed", Points: 1000 + j, Reason: "changed"}
			}
			*got.Explanation = nil

			if rec := e.record(rt, created.ID); !reflect.DeepEqual(rec.Explanation, want) {
				rt.Fatalf("op %d %s: the stored Explanation changed to %+v", i, op.kind, rec.Explanation)
			}
			for j, pv := range e.obs.all()[:len(before)] {
				if !reflect.DeepEqual(*pv.Explanation, sent[j]) {
					rt.Fatalf("op %d %s: the View sent to the observer (#%d) changed to %+v", i, op.kind, j, *pv.Explanation)
				}
			}
			next, err := e.svc.Get(context.Background(), created.ID)
			if err != nil {
				rt.Fatalf("op %d %s: Get = %v, want nil", i, op.kind, err)
			}
			checkExplanationView(rt, "read after the caller changed its View", next, want)
		}
	})
}

// A payment that earns no points is stored with an empty Explanation, which is
// not the same as no Explanation: the key is present and reads [].
func TestSafePaymentGetsAnEmptyExplanationNotAnAbsentOne(t *testing.T) {
	e := newEnv(false)
	v := e.create(t, signalsInstantPass())

	rec := e.record(t, v.ID)
	if rec.Explanation == nil || len(rec.Explanation) != 0 {
		t.Fatalf("stored Explanation = %#v, want present and empty", rec.Explanation)
	}
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"explanation":[]`) {
		t.Fatalf("View %s does not hold \"explanation\":[]", body)
	}
}

// A record written before the Explanation existed reads back with no key at all,
// neither null nor [], and every other field as before.
func TestRecordWithoutAnExplanationLeavesTheKeyOut(t *testing.T) {
	e := newEnv(false)
	v := e.create(t, signalsLocked())

	rec := e.record(t, v.ID)
	old := rec
	old.Explanation = nil
	if err := e.mem.UpdateIf(context.Background(), v.ID, rec.State, old); err != nil {
		t.Fatalf("rewrite the record: %v", err)
	}

	got := explainGet(t, e, v.ID)
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "explanation") {
		t.Fatalf("View %s holds an explanation key for a record that has none", body)
	}
	want := v
	want.Explanation = nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("View = %+v, want the earlier View without the Explanation: %+v", got, want)
	}
}
