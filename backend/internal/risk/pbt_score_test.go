package risk

import (
	"flag"
	"math"
	"strconv"
	"testing"

	"pgregory.net/rapid"
)

// scoreMinChecks is the minimum number of generated inputs per property run
// (Requirements 5.3, 5.5). rapid defaults to 100, so the flag is raised here
// instead of relying on -rapid.checks being passed on the command line.
const scoreMinChecks = 1000

func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= scoreMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(scoreMinChecks)); err != nil {
		panic("risk: raise rapid.checks: " + err.Error())
	}
}

// scoreTier is one row of a tier table from requirements 2.2, 2.3 and 2.6:
// an input at or above floor earns pts, until a higher floor is reached.
type scoreTier struct {
	floor int64
	pts   int
}

// Tier tables copied from the requirements, not from risk.go. Floors are in
// ascending order.
var (
	// Requirement 2.2: 0 below 2000, 10 for 2000-9999, 20 for 10000-24999,
	// 35 for 25000 or more.
	scoreAmountTiers = []scoreTier{{0, 0}, {2000, 10}, {10000, 20}, {25000, 35}}

	// Requirement 2.3: 25 below 24 hours, 10 for 24-71 hours, 0 for 72 or more.
	scoreAgeTiers = []scoreTier{{0, 25}, {24, 10}, {72, 0}}

	// Requirement 2.6: 0 for 0 or 1 retries, 5 for 2, 10 for 3 or more.
	scoreRetryTiers = []scoreTier{{0, 0}, {2, 5}, {3, 10}}
)

const (
	scoreLateNightPts   = 10 // Requirement 2.4
	scoreUnknownCallPts = 20 // Requirement 2.5
	scoreCap            = 100
)

// scoreTierPoints returns the points of the highest tier whose floor v reaches.
func scoreTierPoints(tiers []scoreTier, v int64) int {
	pts := 0
	for _, t := range tiers {
		if v >= t.floor {
			pts = t.pts
		}
	}
	return pts
}

// scoreReference is the independent reference model for Requirement 2.1: the
// sum of the per-signal points, capped at 100.
func scoreReference(s Signals) int {
	total := scoreTierPoints(scoreAmountTiers, s.AmountINR) +
		scoreTierPoints(scoreAgeTiers, s.BeneficiaryAgeHrs) +
		scoreTierPoints(scoreRetryTiers, int64(s.RapidRetryCount))
	if s.IsLateNight {
		total += scoreLateNightPts
	}
	if s.ActiveUnknownCall {
		total += scoreUnknownCallPts
	}
	if total > scoreCap {
		return scoreCap
	}
	return total
}

// scoreAmountGen mixes every amount tier boundary with ordinary and very large
// valid amounts (amount_inr must be greater than 0).
func scoreAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999, 2000, 9999, 10000, 24999, 25000, math.MaxInt64}),
		rapid.Int64Range(1, 30000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// scoreAgeGen mixes every beneficiary-age tier boundary with ordinary and very
// large valid ages (beneficiary_age_hours must be 0 or more).
func scoreAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 23, 24, 71, 72, math.MaxInt64}),
		rapid.Int64Range(0, 100),
		rapid.Int64Range(0, math.MaxInt64),
	)
}

// scoreRetryGen mixes every retry tier boundary with ordinary and larger valid
// counts (rapid_retry_count must be 0 or more).
func scoreRetryGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3, math.MaxInt32}),
		rapid.IntRange(0, 10),
		rapid.IntRange(0, math.MaxInt32),
	)
}

// scoreSignalsGen draws valid Signals with boundary-heavy numeric fields.
func scoreSignalsGen() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         scoreAmountGen().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: scoreAgeGen().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: rapid.Bool().Draw(t, "active_unknown_call"),
			RapidRetryCount:   scoreRetryGen().Draw(t, "rapid_retry_count"),
		}
	})
}

// Feature: behavioral-risk-engine, Property 2: Score equals the reference model and stays in range
//
// **Validates: Requirements 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 5.3, 5.5**
func TestScoreMatchesReferenceModelAndStaysInRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := scoreSignalsGen().Draw(t, "signals")

		res, err := Evaluate(s)
		if err != nil {
			t.Fatalf("Evaluate(%+v) returned an error for valid signals: %v", s, err)
		}

		want := scoreReference(s)
		if res.Score != want {
			t.Fatalf("Evaluate(%+v).Score = %d, reference model = %d", s, res.Score, want)
		}
		if res.Score < 0 || res.Score > scoreCap {
			t.Fatalf("Evaluate(%+v).Score = %d, want 0..%d", s, res.Score, scoreCap)
		}
	})
}
