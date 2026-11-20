package risk

import "github.com/RahulKumar1119/rukpay/backend/internal/store"

// Score thresholds (Requirement 3.4). A score below RealityCheckThreshold maps
// to INSTANT_PASS, a score from RealityCheckThreshold up to LockThreshold-1
// maps to REALITY_CHECK, and a score of LockThreshold or higher maps to
// LOCKED_15M.
const (
	RealityCheckThreshold = 40
	LockThreshold         = 75
	MaxScore              = 100
)

// Points awarded for the boolean signals (Requirements 2.4, 2.5).
const (
	lateNightPoints   = 10
	unknownCallPoints = 20
)

// Signals is the scoring input (Requirement 1.1). It carries no account
// number, UPI ID or phone number (Requirement 12.4).
type Signals struct {
	AmountINR         int64 `json:"amount_inr"`
	BeneficiaryAgeHrs int64 `json:"beneficiary_age_hours"`
	IsLateNight       bool  `json:"is_late_night"`
	ActiveUnknownCall bool  `json:"active_unknown_call"`
	RapidRetryCount   int   `json:"rapid_retry_count"`
}

// ValidationError names the offending signal field and nothing else, so the
// message never carries a signal value.
type ValidationError struct {
	Field string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return "invalid signal: " + e.Field
}

// Result is the output of Evaluate.
type Result struct {
	Score int
	State store.VaultState
}

// Validate checks the signals in the order amount, beneficiary age, retry
// count and returns a *ValidationError for the first failing field
// (Requirements 1.2, 1.3, 1.4).
func Validate(s Signals) error {
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

// amountPoints returns the points for the transfer amount in rupees
// (Requirement 2.2).
func amountPoints(amountINR int64) int {
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

// agePoints returns the points for the beneficiary age in hours
// (Requirement 2.3).
func agePoints(ageHours int64) int {
	switch {
	case ageHours < 24:
		return 25
	case ageHours < 72:
		return 10
	default:
		return 0
	}
}

// retryPoints returns the points for the rapid retry count (Requirement 2.6).
func retryPoints(retries int) int {
	switch {
	case retries >= 3:
		return 10
	case retries == 2:
		return 5
	default:
		return 0
	}
}

// Score returns the sum of the per-signal points, capped at MaxScore
// (Requirement 2.1). It assumes Validate has accepted the signals. It is a
// pure function: the same signals always give the same score (Requirement 2.8).
func Score(s Signals) int {
	total := amountPoints(s.AmountINR) +
		agePoints(s.BeneficiaryAgeHrs) +
		retryPoints(s.RapidRetryCount)
	if s.IsLateNight {
		total += lateNightPoints
	}
	if s.ActiveUnknownCall {
		total += unknownCallPoints
	}
	if total > MaxScore {
		total = MaxScore
	}
	return total
}

// MapState maps a score to its initial vault state (Requirement 3).
func MapState(score int) store.VaultState {
	switch {
	case score >= LockThreshold:
		return store.StateLocked15M
	case score >= RealityCheckThreshold:
		return store.StateRealityCheck
	default:
		return store.StateInstantPass
	}
}

// Evaluate validates the signals, scores them and maps the score to an initial
// state. On a validation failure it returns the zero Result and the error.
func Evaluate(s Signals) (Result, error) {
	if err := Validate(s); err != nil {
		return Result{}, err
	}
	score := Score(s)
	return Result{Score: score, State: MapState(score)}, nil
}
