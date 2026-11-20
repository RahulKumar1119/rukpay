package risk

import (
	"errors"
	"flag"
	"math"
	"strconv"
	"testing"

	"pgregory.net/rapid"
)

// invalidMinChecks is the minimum number of generated inputs per property run
// (Requirement 5 and the design's testing strategy).
const invalidMinChecks = 1000

// Field names reported by Validate (Requirements 1.2, 1.3, 1.4).
const (
	invalidFieldAmount = "amount_inr"
	invalidFieldAge    = "beneficiary_age_hours"
	invalidFieldRetry  = "rapid_retry_count"
)

// init raises rapid's check count to invalidMinChecks when the flag is lower,
// so the property runs enough inputs without the -rapid.checks CLI flag. A
// value given on the command line is parsed later and still takes effect.
func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	if n, err := strconv.Atoi(f.Value.String()); err == nil && n < invalidMinChecks {
		// Best effort: if the flag cannot be set, rapid keeps its default.
		_ = flag.Set("rapid.checks", strconv.Itoa(invalidMinChecks))
	}
}

// invalidAmountGen draws an amount_inr that violates amount_inr > 0. It mixes
// the boundaries 0 and -1 with the full negative range.
func invalidAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.Just(int64(0)),
		rapid.Just(int64(-1)),
		rapid.Int64Range(-1_000_000, -1),
		rapid.Int64Range(math.MinInt64, -1),
	)
}

// invalidAgeGen draws a beneficiary_age_hours that violates age >= 0.
func invalidAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.Just(int64(-1)),
		rapid.Int64Range(-1_000_000, -1),
		rapid.Int64Range(math.MinInt64, -1),
	)
}

// invalidRetryGen draws a rapid_retry_count that violates retries >= 0.
func invalidRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.Just(-1),
		rapid.IntRange(-1_000_000, -1),
		rapid.IntRange(math.MinInt, -1),
	)
}

// invalidValidAmountGen draws an amount_inr that satisfies amount_inr > 0,
// including the amount tier boundaries.
func invalidValidAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999, 2000, 9999, 10000, 24999, 25000}),
		rapid.Int64Range(1, 10_000_000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// invalidValidAgeGen draws a beneficiary_age_hours that satisfies age >= 0,
// including the age tier boundaries.
func invalidValidAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 23, 24, 71, 72}),
		rapid.Int64Range(0, 10_000),
		rapid.Int64Range(0, math.MaxInt64),
	)
}

// invalidValidRetryGen draws a rapid_retry_count that satisfies retries >= 0,
// including the retry tier boundaries.
func invalidValidRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3}),
		rapid.IntRange(0, 1_000),
		rapid.IntRange(0, math.MaxInt),
	)
}

// Feature: behavioral-risk-engine, Property 1: Invalid signals are rejected with the offending field
//
// Validates: Requirements 1.2, 1.3, 1.4
func TestInvalidSignalsRejectedWithOffendingField(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := Signals{
			AmountINR:         invalidValidAmountGen().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: invalidValidAgeGen().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: rapid.Bool().Draw(t, "active_unknown_call"),
			RapidRetryCount:   invalidValidRetryGen().Draw(t, "rapid_retry_count"),
		}

		// Break exactly one of the three validated fields.
		want := rapid.SampledFrom([]string{
			invalidFieldAmount, invalidFieldAge, invalidFieldRetry,
		}).Draw(t, "invalid_field")
		switch want {
		case invalidFieldAmount:
			s.AmountINR = invalidAmountGen().Draw(t, "invalid_amount_inr")
		case invalidFieldAge:
			s.BeneficiaryAgeHrs = invalidAgeGen().Draw(t, "invalid_beneficiary_age_hours")
		case invalidFieldRetry:
			s.RapidRetryCount = invalidRetryGen().Draw(t, "invalid_rapid_retry_count")
		}

		err := Validate(s)
		if err == nil {
			t.Fatalf("Validate accepted signals with invalid %s", want)
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("Validate returned %T, want *ValidationError", err)
		}
		if ve.Field != want {
			t.Fatalf("ValidationError.Field = %q, want %q", ve.Field, want)
		}
	})
}
