package risk

import (
	"flag"
	"math"
	"strconv"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"pgregory.net/rapid"
)

// lockMinChecks is the minimum number of generated inputs per property run
// (Requirement 5.1). rapid defaults to 100, so the flag is raised here instead
// of relying on -rapid.checks being passed on the command line.
const lockMinChecks = 1000

// lockMinScore is the score the lock guarantee promises (Requirement 4.1). It is
// written out here so the test does not depend on the constant in risk.go.
const lockMinScore = 75

func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= lockMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(lockMinChecks)); err != nil {
		panic("risk: raise rapid.checks: " + err.Error())
	}
}

// lockAmountGen draws amounts of 25000 or more: the boundary, values just
// above it, ordinary values and very large values.
func lockAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{25000, 25001, 85000, 1000000, math.MaxInt64}),
		rapid.Int64Range(25000, 100000),
		rapid.Int64Range(25000, math.MaxInt64),
	)
}

// lockAgeGen draws beneficiary ages below 24 hours, including 0 and 23.
func lockAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 23}),
		rapid.Int64Range(0, 23),
	)
}

// lockRetryGen draws retry counts across every retry tier (0, 1, 2, 3 and
// larger).
func lockRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3, math.MaxInt32}),
		rapid.IntRange(0, 10),
		rapid.IntRange(0, math.MaxInt32),
	)
}

// lockSignalsGen draws Signals that satisfy the lock-guarantee precondition:
// amount_inr >= 25000, beneficiary_age_hours < 24 and active_unknown_call
// true. is_late_night and rapid_retry_count are free.
func lockSignalsGen() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         lockAmountGen().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: lockAgeGen().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: true,
			RapidRetryCount:   lockRetryGen().Draw(t, "rapid_retry_count"),
		}
	})
}

// Feature: behavioral-risk-engine, Property 5: Lock guarantee
//
// **Validates: Requirements 4.1, 5.1**
func TestLockGuaranteeHoldsForLargeNewBeneficiaryOnCall(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := lockSignalsGen().Draw(t, "signals")

		res, err := Evaluate(s)
		if err != nil {
			t.Fatalf("Evaluate(%+v) returned an error for valid signals: %v", s, err)
		}
		if res.Score < lockMinScore {
			t.Fatalf("Evaluate(%+v).Score = %d, want at least %d", s, res.Score, lockMinScore)
		}
		if res.State != store.StateLocked15M {
			t.Fatalf("Evaluate(%+v).State = %q, want %q", s, res.State, store.StateLocked15M)
		}
	})
}
