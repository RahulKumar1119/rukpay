package risk

import (
	"flag"
	"strconv"
	"testing"

	"pgregory.net/rapid"
)

// determinismMinChecks is the minimum number of generated inputs per property
// run, so the test does not depend on the -rapid.checks command-line flag.
const determinismMinChecks = 1000

// init raises the rapid check count to at least determinismMinChecks. An
// explicit -rapid.checks flag on the command line is parsed later and still
// takes precedence.
func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	current, err := strconv.Atoi(f.Value.String())
	if err != nil || current >= determinismMinChecks {
		return
	}
	// flag.Set only fails on a malformed value, and the value is a plain integer.
	_ = flag.Set("rapid.checks", strconv.Itoa(determinismMinChecks))
}

// determinismSignals generates valid Signals. Each numeric field mixes the exact
// tier boundary values from Requirement 2 with a wide random range, so every
// tier edge is hit often.
func determinismSignals() *rapid.Generator[Signals] {
	amount := rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999, 2000, 9999, 10000, 24999, 25000}),
		rapid.Int64Range(1, 200000),
	)
	age := rapid.OneOf(
		rapid.SampledFrom([]int64{0, 23, 24, 71, 72}),
		rapid.Int64Range(0, 500),
	)
	retries := rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3}),
		rapid.IntRange(0, 20),
	)

	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         amount.Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: age.Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: rapid.Bool().Draw(t, "active_unknown_call"),
			RapidRetryCount:   retries.Draw(t, "rapid_retry_count"),
		}
	})
}

// Feature: behavioral-risk-engine, Property 3: Scoring is deterministic
//
// **Validates: Requirements 2.8**
func TestDeterminismEvaluateIdenticalSignals(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		signals := determinismSignals().Draw(t, "signals")
		other := determinismSignals().Draw(t, "other")

		first, firstErr := Evaluate(signals)
		if firstErr != nil {
			t.Fatalf("Evaluate returned an error for valid signals: %v", firstErr)
		}

		// A call with different signals in between must not change later results,
		// which rules out hidden state carried from one call to the next.
		if _, err := Evaluate(other); err != nil {
			t.Fatalf("Evaluate returned an error for valid signals: %v", err)
		}

		for call := 2; call <= 3; call++ {
			got, err := Evaluate(signals)
			if err != nil {
				t.Fatalf("call %d: Evaluate returned an error for valid signals: %v", call, err)
			}
			if got != first {
				t.Fatalf("call %d: result %+v differs from first result %+v", call, got, first)
			}
		}
	})
}
