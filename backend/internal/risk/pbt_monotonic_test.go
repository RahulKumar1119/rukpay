package risk

import (
	"flag"
	"math"
	"strconv"
	"testing"

	"pgregory.net/rapid"
)

// monotonicMinChecks is the minimum number of generated inputs per property
// run (Requirement 5.4). rapid defaults to 100, so the flag is raised here
// instead of relying on -rapid.checks being passed on the command line.
const monotonicMinChecks = 1000

// init raises the rapid check count to at least monotonicMinChecks. An
// explicit -rapid.checks flag on the command line is parsed later and still
// takes precedence.
func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= monotonicMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(monotonicMinChecks)); err != nil {
		panic("risk: raise rapid.checks: " + err.Error())
	}
}

// monotonicAmountGen mixes every amount tier boundary with ordinary and very
// large valid amounts (amount_inr must be greater than 0).
func monotonicAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999, 2000, 9999, 10000, 24999, 25000, math.MaxInt64}),
		rapid.Int64Range(1, 30000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// monotonicAgeGen mixes every beneficiary-age tier boundary with ordinary and
// very large valid ages (beneficiary_age_hours must be 0 or more).
func monotonicAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 23, 24, 71, 72, math.MaxInt64}),
		rapid.Int64Range(0, 100),
		rapid.Int64Range(0, math.MaxInt64),
	)
}

// monotonicRetryGen mixes every retry tier boundary with ordinary and larger
// valid counts (rapid_retry_count must be 0 or more).
func monotonicRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3, math.MaxInt32}),
		rapid.IntRange(0, 10),
		rapid.IntRange(0, math.MaxInt32),
	)
}

// monotonicBaseSignalsGen draws valid Signals with ActiveUnknownCall set to
// false explicitly. Every other field, including both values of IsLateNight,
// is generated.
func monotonicBaseSignalsGen() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         monotonicAmountGen().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: monotonicAgeGen().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: false,
			RapidRetryCount:   monotonicRetryGen().Draw(t, "rapid_retry_count"),
		}
	})
}

// Feature: behavioral-risk-engine, Property 7: Unknown call never lowers the score
//
// **Validates: Requirements 5.4**
func TestMonotonicUnknownCallNeverLowersScore(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := monotonicBaseSignalsGen().Draw(t, "base")

		// Flip only active_unknown_call; Signals is a value type, so base is
		// left unchanged.
		withCall := base
		withCall.ActiveUnknownCall = true

		baseRes, err := Evaluate(base)
		if err != nil {
			t.Fatalf("Evaluate(%+v) returned an error for valid signals: %v", base, err)
		}
		callRes, err := Evaluate(withCall)
		if err != nil {
			t.Fatalf("Evaluate(%+v) returned an error for valid signals: %v", withCall, err)
		}

		if callRes.Score < baseRes.Score {
			t.Fatalf("score fell from %d to %d when active_unknown_call changed from false to true; base %+v, with call %+v",
				baseRes.Score, callRes.Score, base, withCall)
		}
	})
}
