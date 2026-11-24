package httpapi

import (
	"math"
	"net/http"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// invalidAmountGen draws an amount that fails validation: zero or negative.
func invalidAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, -1, math.MinInt64}),
		rapid.Int64Range(-1_000_000, 0),
		rapid.Int64Range(math.MinInt64, 0),
	)
}

// validAmountGen draws a valid amount across the tier boundaries.
func validAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 450, 1999, 2000, 9999, 10000, 24999, 25000, 85000}),
		rapid.Int64Range(1, 500_000),
	)
}

func invalidAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{-1, math.MinInt64}),
		rapid.Int64Range(-100_000, -1),
	)
}

func validAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 23, 24, 71, 72, 720}),
		rapid.Int64Range(0, 5000),
	)
}

func invalidRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{-1, math.MinInt32}),
		rapid.IntRange(-1000, -1),
	)
}

func validRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3}),
		rapid.IntRange(0, 20),
	)
}

// Feature: cooling-off-vault, Property 2: Invalid input never creates a vault
//
// Signals that fail validation give a 400 that names the first invalid field, in
// the order amount, age, retries, and no store call is made.
//
// **Validates: Requirements 1.6, 1.7**
func TestInvalidSignalsNeverCreateAVault(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Choose which fields are invalid. At least one must be.
		badAmount := rapid.Bool().Draw(rt, "bad_amount")
		badAge := rapid.Bool().Draw(rt, "bad_age")
		badRetry := rapid.Bool().Draw(rt, "bad_retry")
		if !badAmount && !badAge && !badRetry {
			badAmount = true
		}

		body := map[string]any{
			"is_late_night":       rapid.Bool().Draw(rt, "late_night"),
			"active_unknown_call": rapid.Bool().Draw(rt, "call"),
		}
		wantField := ""
		pick := func(bad bool, field string, invalid, valid any) {
			if bad {
				body[field] = invalid
				if wantField == "" {
					wantField = field
				}
				return
			}
			body[field] = valid
		}
		pick(badAmount, "amount_inr", invalidAmountGen().Draw(rt, "amount"), validAmountGen().Draw(rt, "amount_ok"))
		pick(badAge, "beneficiary_age_hours", invalidAgeGen().Draw(rt, "age"), validAgeGen().Draw(rt, "age_ok"))
		pick(badRetry, "rapid_retry_count", invalidRetryGen().Draw(rt, "retry"), validRetryGen().Draw(rt, "retry_ok"))

		e := newTestEnv(false)
		res := e.do(rt, http.MethodPost, pathEvaluate, body)

		var got struct {
			Error struct {
				Code  string `json:"code"`
				Field string `json:"field"`
			} `json:"error"`
		}
		res.json(rt, &got)
		if res.status != http.StatusBadRequest || got.Error.Code != "invalid_field" || got.Error.Field != wantField {
			rt.Fatalf("evaluate(%v) = %d %s, want a 400 invalid_field naming %s", body, res.status, res.body, wantField)
		}
		if calls, writes := e.cs.snapshot(); calls != 0 || writes != 0 {
			rt.Fatalf("an invalid request made %d store calls and %d writes", calls, writes)
		}
	})
}

// Feature: cooling-off-vault, Property 2: Invalid input never creates a vault
//
// An identifier that is not masked gives a 400 invalid_identifier, even when the
// signals are also invalid, and no store call is made. The response does not
// echo the identifier.
//
// **Validates: Requirements 1.7**
func TestUnmaskedIdentifiersNeverCreateAVault(t *testing.T) {
	fields := []struct {
		name string
		raw  *rapid.Generator[string]
	}{
		{"payer_account_masked", rapid.StringMatching(`^[0-9]{9,18}$`)},
		{"beneficiary_masked", rapid.StringMatching(`^[0-9]{9,18}$`)},
		{"upi_masked", rapid.StringMatching(`^[a-z]{3,10}@[a-z]{3,8}$`)},
		{"phone_masked", rapid.StringMatching(`^(\+91)?[6-9][0-9]{9}$`)},
	}
	rapid.Check(t, func(rt *rapid.T) {
		f := fields[rapid.IntRange(0, len(fields)-1).Draw(rt, "field")]
		anyText := rapid.String().Filter(func(s string) bool {
			if s == "" {
				return false
			}
			rec := store.VaultRecord{}
			switch f.name {
			case "payer_account_masked":
				rec.PayerAccountMasked = s
			case "beneficiary_masked":
				rec.BeneficiaryMasked = s
			case "upi_masked":
				rec.UPIMasked = s
			default:
				rec.PhoneMasked = s
			}
			return store.ValidateMasked(rec) != nil
		})
		value := rapid.OneOf(f.raw, anyText).Draw(rt, "value")

		body := map[string]any{
			"amount_inr":            validAmountGen().Draw(rt, "amount"),
			"beneficiary_age_hours": validAgeGen().Draw(rt, "age"),
			f.name:                  value,
		}
		if rapid.Bool().Draw(rt, "also_bad_signals") {
			body["amount_inr"] = invalidAmountGen().Draw(rt, "bad_amount")
		}

		e := newTestEnv(false)
		res := e.do(rt, http.MethodPost, pathEvaluate, body)
		if res.status != http.StatusBadRequest || res.errorCode(rt) != "invalid_identifier" {
			rt.Fatalf("evaluate with %s = %q gave %d %s, want a 400 invalid_identifier", f.name, value, res.status, res.body)
		}
		if calls, writes := e.cs.snapshot(); calls != 0 || writes != 0 {
			rt.Fatalf("an unmasked identifier made %d store calls and %d writes", calls, writes)
		}
		if len(value) >= 6 {
			if echoed, found := bodyContains(res, value); found {
				rt.Fatalf("the response echoes the identifier %q", echoed)
			}
		}
	})
}

// answersBody builds the answers object of a reality-check request.
func answersBody(coercion, secrecy, verification string) map[string]any {
	return map[string]any{"coercion": coercion, "secrecy": secrecy, "verification": verification}
}

// bodyShape is one request body for the reality-check route and whether the
// route accepts its shape.
type bodyShape struct {
	name  string
	body  map[string]any
	valid bool // exactly one of answers or reconfirm: true is present
	// kind is what a valid shape asks for: "answers" or "reconfirm".
	kind string
}

func bodyShapes(a map[string]any) []bodyShape {
	return []bodyShape{
		{"answers only", map[string]any{"answers": a}, true, "answers"},
		{"answers and a null reconfirm", map[string]any{"answers": a, "reconfirm": nil}, true, "answers"},
		{"reconfirm only", map[string]any{"reconfirm": true}, true, "reconfirm"},
		{"reconfirm and null answers", map[string]any{"reconfirm": true, "answers": nil}, true, "reconfirm"},
		{"both", map[string]any{"answers": a, "reconfirm": true}, false, ""},
		{"answers and reconfirm false", map[string]any{"answers": a, "reconfirm": false}, false, ""},
		{"neither", map[string]any{}, false, ""},
		{"null answers", map[string]any{"answers": nil}, false, ""},
		{"null reconfirm", map[string]any{"reconfirm": nil}, false, ""},
		{"both null", map[string]any{"answers": nil, "reconfirm": nil}, false, ""},
		{"reconfirm false", map[string]any{"reconfirm": false}, false, ""},
		{"unrelated fields only", map[string]any{"coercion": "YES", "reconfirm_please": true}, false, ""},
	}
}

// Feature: cooling-off-vault, Property 11: Reconfirm payload shape
//
// The reality-check route accepts a body only when exactly one of answers or
// reconfirm: true is present. Any other shape is a 400 and the vault is
// unchanged. A valid shape is never a 400: it is a 200 in the state it applies
// to and a 409 in any other.
//
// **Validates: Requirements 3.7, 4.5**
func TestRealityCheckBodyNeedsExactlyOneOfAnswersOrReconfirm(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		state := stateGen().Draw(rt, "state")
		a := answersBody(
			rapid.SampledFrom([]string{"YES", "NO"}).Draw(rt, "coercion"),
			rapid.SampledFrom([]string{"YES", "NO"}).Draw(rt, "secrecy"),
			rapid.SampledFrom([]string{"YES", "NO"}).Draw(rt, "verification"),
		)
		shapes := bodyShapes(a)
		shape := shapes[rapid.IntRange(0, len(shapes)-1).Draw(rt, "shape")]
		if rapid.Bool().Draw(rt, "extra_field") {
			shape.body["notes"] = "an unrelated field"
		}

		e := newTestEnv(false)
		v := e.vaultIn(rt, state)
		before := e.record(rt, v.ID)
		_, writesBefore := e.cs.snapshot()

		res := e.do(rt, http.MethodPost, pathVaultBase+v.ID+"/reality-check", shape.body)

		if !shape.valid {
			if res.status != http.StatusBadRequest || res.errorCode(rt) != "invalid_request" {
				rt.Fatalf("%s on a %s vault = %d %s, want a 400 invalid_request", shape.name, state, res.status, res.body)
			}
			if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) {
				rt.Fatalf("%s changed the vault: %+v -> %+v", shape.name, before, after)
			}
			if _, writes := e.cs.snapshot(); writes != writesBefore {
				rt.Fatalf("%s wrote to the store", shape.name)
			}
			return
		}

		appliesHere := (shape.kind == "answers" && state == store.StateRealityCheck) ||
			(shape.kind == "reconfirm" && state == store.StateExpired)
		want := http.StatusConflict
		if appliesHere {
			want = http.StatusOK
		}
		if res.status != want {
			rt.Fatalf("%s on a %s vault = %d %s, want %d", shape.name, state, res.status, res.body, want)
		}
		if !appliesHere {
			if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) {
				rt.Fatalf("%s on a %s vault changed it: %+v -> %+v", shape.name, state, before, after)
			}
		}
	})
}
