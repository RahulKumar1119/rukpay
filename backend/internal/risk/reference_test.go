package risk

import (
	"math"
	"regexp"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file holds the two fixed targets that the Assess property tests compare
// against. Neither shares code, a table or a constant with the risk package:
//
//   - refBaseline is a model written from the tier tables of the
//     behavioral-risk-engine spec, Requirement 2 criteria 2 to 6. It uses
//     inclusive ranges and string literals, not the tier rows or the Signal_Id
//     constants of explain.go.
//   - frozenEvaluate is a frozen copy of Evaluate as shipped before the
//     risk-intelligence feature (Requirement 27.4). Nothing in it may change
//     when the engine changes. That is the point of it.

// refBand is one row of a tier table as the requirement text writes it: an
// inclusive range of values and the points that range earns.
type refBand struct {
	from, to int64
	points   int
}

// Requirement 2 criterion 2: 0 points below 2000, 10 points for 2000 to 9999,
// 20 points for 10000 to 24999, 35 points for 25000 or more.
var refAmountBands = []refBand{
	{math.MinInt64, 1999, 0},
	{2000, 9999, 10},
	{10000, 24999, 20},
	{25000, math.MaxInt64, 35},
}

// Requirement 2 criterion 3: 25 points below 24 hours, 10 points for 24 to 71,
// 0 points for 72 or more.
var refAgeBands = []refBand{
	{math.MinInt64, 23, 25},
	{24, 71, 10},
	{72, math.MaxInt64, 0},
}

// Requirement 2 criterion 6: 0 points for 0 or 1 retries, 5 points for 2,
// 10 points for 3 or more.
var refRetryBands = []refBand{
	{math.MinInt64, 1, 0},
	{2, 2, 5},
	{3, math.MaxInt64, 10},
}

// refBandPoints returns the points of the band that holds v. The bands cover
// every int64, so exactly one matches.
func refBandPoints(bands []refBand, v int64) int {
	for _, b := range bands {
		if v >= b.from && v <= b.to {
			return b.points
		}
	}
	panic("reference bands do not cover every value")
}

// refEntry is one expected baseline Contribution, without its sentence.
type refEntry struct {
	Signal string
	Points int
}

// refBaseline returns the baseline Contributions that the reference model
// expects for valid signals, in the order of Requirement 1 criterion 3, with a
// Signal_Id left out when it earns 0 points.
func refBaseline(s Signals) []refEntry {
	candidates := []refEntry{
		{"amount", refBandPoints(refAmountBands, s.AmountINR)},
		{"beneficiary_age", refBandPoints(refAgeBands, s.BeneficiaryAgeHrs)},
		{"late_night", refBoolPoints(s.IsLateNight, 10)},
		{"unknown_call", refBoolPoints(s.ActiveUnknownCall, 20)},
		{"rapid_retries", refBandPoints(refRetryBands, int64(s.RapidRetryCount))},
	}
	var out []refEntry
	for _, c := range candidates {
		if c.Points > 0 {
			out = append(out, c)
		}
	}
	return out
}

// refBaselineTotal returns the sum of the points of the entries.
func refBaselineTotal(entries []refEntry) int {
	total := 0
	for _, e := range entries {
		total += e.Points
	}
	return total
}

func refBoolPoints(on bool, points int) int {
	if on {
		return points
	}
	return 0
}

// refInvalidField returns the field that Requirement 1 of the
// behavioral-risk-engine spec reports for the signals: the first failing field
// in the order amount, beneficiary age, retry count, or "" for valid signals.
func refInvalidField(s Signals) string {
	switch {
	case s.AmountINR < 1:
		return "amount_inr"
	case s.BeneficiaryAgeHrs < 0:
		return "beneficiary_age_hours"
	case s.RapidRetryCount < 0:
		return "rapid_retry_count"
	}
	return ""
}

// refPatternID matches the Signal_Id of a Fraud_Pattern Contribution: the
// prefix pattern_ and a lower-case snake case id.
var refPatternID = regexp.MustCompile(`^pattern_[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// Signal classes of Requirement 1, as the reference holds them.
const (
	refClassNone         = ""
	refClassBaseline     = "baseline"
	refClassBehaviour    = "behaviour"
	refClassCare         = "care"
	refClassBehaviourCap = "behaviour_cap"
	refClassScoreCap     = "score_cap"
	refRankPatternSlot   = 9
)

// refRank returns the position of a Signal_Id in the order of Requirement 1
// criterion 3, or -1 for an unknown Signal_Id. Every Fraud_Pattern Contribution
// shares one position, because their order is the order of the Pattern_Table.
func refRank(signal string) int {
	switch signal {
	case "amount":
		return 0
	case "beneficiary_age":
		return 1
	case "late_night":
		return 2
	case "unknown_call":
		return 3
	case "rapid_retries":
		return 4
	case "velocity":
		return 5
	case "daily_frequency":
		return 6
	case "repeat_attempts":
		return 7
	case "amount_deviation":
		return 8
	case "behaviour_cap":
		return 10
	case "beneficiary_care":
		return 11
	case "score_cap":
		return 12
	}
	if refPatternID.MatchString(signal) {
		return refRankPatternSlot
	}
	return -1
}

// refClass returns the class of a Signal_Id, or refClassNone when unknown.
func refClass(signal string) string {
	rank := refRank(signal)
	switch {
	case rank >= 0 && rank <= 4:
		return refClassBaseline
	case rank >= 5 && rank <= 9:
		return refClassBehaviour
	case rank == 10:
		return refClassBehaviourCap
	case rank == 11:
		return refClassCare
	case rank == 12:
		return refClassScoreCap
	}
	return refClassNone
}

// refStateRank orders the three Initial_States from least to most friction.
func refStateRank(state store.VaultState) int {
	switch state {
	case store.StateInstantPass:
		return 0
	case store.StateRealityCheck:
		return 1
	case store.StateLocked15M:
		return 2
	}
	return -1
}

// frozenResult is the result of frozenEvaluate.
type frozenResult struct {
	Score int
	State store.VaultState
}

const (
	frozenRealityCheckThreshold = 40
	frozenLockThreshold         = 75
	frozenMaxScore              = 100
	frozenLateNightPoints       = 10
	frozenUnknownCallPoints     = 20
)

// frozenValidate is a frozen copy of Validate as shipped.
func frozenValidate(s Signals) error {
	if s.AmountINR <= 0 {
		return &ValidationError{Field: "amount_inr"}
	}
	if s.BeneficiaryAgeHrs < 0 {
		return &ValidationError{Field: "beneficiary_age_hours"}
	}
	if s.RapidRetryCount < 0 {
		return &ValidationError{Field: "rapid_retry_count"}
	}
	return nil
}

// frozenAmountPoints is a frozen copy of amountPoints as shipped.
func frozenAmountPoints(amountINR int64) int {
	switch {
	case amountINR >= 25000:
		return 35
	case amountINR >= 10000:
		return 20
	case amountINR >= 2000:
		return 10
	default:
		return 0
	}
}

// frozenAgePoints is a frozen copy of agePoints as shipped.
func frozenAgePoints(ageHours int64) int {
	switch {
	case ageHours < 24:
		return 25
	case ageHours < 72:
		return 10
	default:
		return 0
	}
}

// frozenRetryPoints is a frozen copy of retryPoints as shipped.
func frozenRetryPoints(retries int) int {
	switch {
	case retries >= 3:
		return 10
	case retries == 2:
		return 5
	default:
		return 0
	}
}

// frozenScore is a frozen copy of Score as shipped.
func frozenScore(s Signals) int {
	total := frozenAmountPoints(s.AmountINR) +
		frozenAgePoints(s.BeneficiaryAgeHrs) +
		frozenRetryPoints(s.RapidRetryCount)
	if s.IsLateNight {
		total += frozenLateNightPoints
	}
	if s.ActiveUnknownCall {
		total += frozenUnknownCallPoints
	}
	if total > frozenMaxScore {
		total = frozenMaxScore
	}
	return total
}

// frozenMapState is a frozen copy of MapState as shipped.
func frozenMapState(score int) store.VaultState {
	switch {
	case score >= frozenLockThreshold:
		return store.StateLocked15M
	case score >= frozenRealityCheckThreshold:
		return store.StateRealityCheck
	default:
		return store.StateInstantPass
	}
}

// frozenEvaluate is a frozen copy of Evaluate as shipped before the
// risk-intelligence feature. On a validation failure it returns the zero result
// and the error.
func frozenEvaluate(s Signals) (frozenResult, error) {
	if err := frozenValidate(s); err != nil {
		return frozenResult{}, err
	}
	score := frozenScore(s)
	return frozenResult{Score: score, State: frozenMapState(score)}, nil
}
