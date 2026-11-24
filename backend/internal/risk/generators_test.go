package risk

import (
	"math"

	"pgregory.net/rapid"
)

// This file holds the generators that the Assess property tests share. Every
// Assess property draws its input from genInput (or genInvalidInput) and reads
// the answer through the abstractions in this file and in reference_test.go.
// Groups 8 and 12 widen Input with a History_Snapshot, an evaluation time and a
// recipient, and they widen genInput, genInvalidInput, cloneInput and
// baselineOnly here, so the property tests need no edit.

// genAmount draws an amount_inr greater than 0. It mixes every tier boundary,
// the smallest and the largest valid amount, small ordinary amounts and the
// whole valid range.
func genAmount() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999, 2000, 9999, 10000, 24999, 25000, math.MaxInt64}),
		rapid.Int64Range(1, 30000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// genAgeHours draws a beneficiary_age_hours of 0 or more. It mixes every tier
// boundary, the largest age, small ordinary ages and the whole valid range.
func genAgeHours() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 23, 24, 71, 72, 73, math.MaxInt64}),
		rapid.Int64Range(0, 100),
		rapid.Int64Range(0, math.MaxInt64),
	)
}

// genRetries draws a rapid_retry_count of 0 or more. It mixes every tier
// boundary, a very large count, small ordinary counts and the whole valid
// range.
func genRetries() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3, 4, math.MaxInt}),
		rapid.IntRange(0, 10),
		rapid.IntRange(0, math.MaxInt),
	)
}

// genSignals draws valid Signals that reach every tier boundary of the baseline
// rules: amount 1999 and 2000, 9999 and 10000, 24999 and 25000, age 23 and 24
// and 71 and 72, retries 1 and 2 and 3, and the largest amount.
func genSignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         genAmount().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: genAgeHours().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: rapid.Bool().Draw(t, "active_unknown_call"),
			RapidRetryCount:   genRetries().Draw(t, "rapid_retry_count"),
		}
	})
}

// genInput draws a valid Input. It is the one generator that every Assess
// property test calls.
func genInput() *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		return Input{Signals: genSignals().Draw(t, "signals")}
	})
}

// genInvalidSignals draws Signals with at least one invalid field: an amount
// below 1, a negative beneficiary age or a negative retry count. It breaks any
// non-empty subset of the three fields, so the first-failing-field order is
// exercised, and keeps the other fields valid.
func genInvalidSignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		s := genSignals().Draw(t, "signals")
		// Bits 1, 2 and 4 select the amount, the age and the retry count.
		mask := rapid.IntRange(1, 7).Draw(t, "broken_fields")
		if mask&1 != 0 {
			s.AmountINR = rapid.OneOf(
				rapid.SampledFrom([]int64{0, -1, math.MinInt64}),
				rapid.Int64Range(math.MinInt64, 0),
			).Draw(t, "invalid_amount_inr")
		}
		if mask&2 != 0 {
			s.BeneficiaryAgeHrs = rapid.OneOf(
				rapid.SampledFrom([]int64{-1, math.MinInt64}),
				rapid.Int64Range(math.MinInt64, -1),
			).Draw(t, "invalid_beneficiary_age_hours")
		}
		if mask&4 != 0 {
			s.RapidRetryCount = rapid.OneOf(
				rapid.SampledFrom([]int{-1, math.MinInt}),
				rapid.IntRange(math.MinInt, -1),
			).Draw(t, "invalid_rapid_retry_count")
		}
		return s
	})
}

// genInvalidInput draws an Input whose Signals are invalid.
func genInvalidInput() *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		return Input{Signals: genInvalidSignals().Draw(t, "signals")}
	})
}

// cloneInput returns a copy of in that shares no memory with it, so a test can
// check that Assess left its argument unchanged. Input holds only values
// today. A group that adds a slice or a pointer to Input deep copies it here.
func cloneInput(in Input) Input {
	return in
}

// baselineOnly reports whether the scored evaluation has an empty
// History_Snapshot and names no Beneficiary, which are the inputs for which
// Assess must give the score and state that the shipped Evaluate gave. Today
// every Input is such an input. A group that adds history or a recipient to
// Input narrows this.
func baselineOnly(in Input) bool {
	return true
}
