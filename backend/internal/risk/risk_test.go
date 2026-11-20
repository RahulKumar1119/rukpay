package risk

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// unitQuietSignals returns valid Signals that score 0 points on every signal:
// a small amount, a Known_Contact, daytime, no call and no retries. Tier tests
// change one field at a time so the total score equals that field's points.
func unitQuietSignals() Signals {
	return Signals{
		AmountINR:         450,
		BeneficiaryAgeHrs: 72,
	}
}

// TestUnitDemoPresets checks the three demo presets from Requirement 4.3 to 4.5,
// asserting both the score and the state.
func TestUnitDemoPresets(t *testing.T) {
	tests := []struct {
		name      string
		signals   Signals
		wantScore int
		wantState store.VaultState
	}{
		{
			// Requirement 4.3: Known_Contact, daytime, no call, no retries.
			name: "450 rupees to a known contact passes",
			signals: Signals{
				AmountINR:         450,
				BeneficiaryAgeHrs: 720,
			},
			wantScore: 0,
			wantState: store.StateInstantPass,
		},
		{
			// Requirement 4.4: 20 amount points + 25 age points.
			name: "18000 rupees to a new contact needs a reality check",
			signals: Signals{
				AmountINR:         18000,
				BeneficiaryAgeHrs: 2,
			},
			wantScore: 45,
			wantState: store.StateRealityCheck,
		},
		{
			// Requirement 4.5: 35 + 25 + 10 late night + 20 unknown call.
			name: "85000 rupees to a new contact late at night on a call is locked",
			signals: Signals{
				AmountINR:         85000,
				BeneficiaryAgeHrs: 2,
				IsLateNight:       true,
				ActiveUnknownCall: true,
			},
			wantScore: 90,
			wantState: store.StateLocked15M,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Score(tt.signals); got != tt.wantScore {
				t.Errorf("Score() = %d, want %d", got, tt.wantScore)
			}
			res, err := Evaluate(tt.signals)
			if err != nil {
				t.Fatalf("Evaluate() error = %v, want nil", err)
			}
			if res.Score != tt.wantScore {
				t.Errorf("Evaluate().Score = %d, want %d", res.Score, tt.wantScore)
			}
			if res.State != tt.wantState {
				t.Errorf("Evaluate().State = %q, want %q", res.State, tt.wantState)
			}
		})
	}
}

// TestUnitAmountTierBoundaries checks the amount tiers on both sides of each
// boundary (Requirement 2.2).
func TestUnitAmountTierBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		amount     int64
		wantPoints int
	}{
		{"1 rupee is the lowest valid amount", 1, 0},
		{"1999 is below the 2000 tier", 1999, 0},
		{"2000 starts the 10 point tier", 2000, 10},
		{"9999 is the top of the 10 point tier", 9999, 10},
		{"10000 starts the 20 point tier", 10000, 20},
		{"24999 is the top of the 20 point tier", 24999, 20},
		{"25000 starts the 35 point tier", 25000, 35},
		{"large amount stays in the 35 point tier", 1000000, 35},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := amountPoints(tt.amount); got != tt.wantPoints {
				t.Errorf("amountPoints(%d) = %d, want %d", tt.amount, got, tt.wantPoints)
			}
			s := unitQuietSignals()
			s.AmountINR = tt.amount
			if got := Score(s); got != tt.wantPoints {
				t.Errorf("Score(amount=%d) = %d, want %d", tt.amount, got, tt.wantPoints)
			}
		})
	}
}

// TestUnitAgeTierBoundaries checks the beneficiary-age tiers on both sides of
// each boundary (Requirement 2.3).
func TestUnitAgeTierBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		ageHours   int64
		wantPoints int
	}{
		{"0 hours is a brand-new contact", 0, 25},
		{"23 hours is below the 24 hour tier", 23, 25},
		{"24 hours starts the 10 point tier", 24, 10},
		{"71 hours is the top of the 10 point tier", 71, 10},
		{"72 hours is a known contact", 72, 0},
		{"a year old contact scores nothing", 8760, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agePoints(tt.ageHours); got != tt.wantPoints {
				t.Errorf("agePoints(%d) = %d, want %d", tt.ageHours, got, tt.wantPoints)
			}
			s := unitQuietSignals()
			s.BeneficiaryAgeHrs = tt.ageHours
			if got := Score(s); got != tt.wantPoints {
				t.Errorf("Score(age=%d) = %d, want %d", tt.ageHours, got, tt.wantPoints)
			}
		})
	}
}

// TestUnitRetryTierBoundaries checks the rapid-retry tiers (Requirement 2.6).
func TestUnitRetryTierBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		retries    int
		wantPoints int
	}{
		{"0 retries", 0, 0},
		{"1 retry still scores nothing", 1, 0},
		{"2 retries score 5", 2, 5},
		{"3 retries score 10", 3, 10},
		{"many retries stay at 10", 50, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryPoints(tt.retries); got != tt.wantPoints {
				t.Errorf("retryPoints(%d) = %d, want %d", tt.retries, got, tt.wantPoints)
			}
			s := unitQuietSignals()
			s.RapidRetryCount = tt.retries
			if got := Score(s); got != tt.wantPoints {
				t.Errorf("Score(retries=%d) = %d, want %d", tt.retries, got, tt.wantPoints)
			}
		})
	}
}

// TestUnitMapStateBoundaries checks the state mapping on both sides of the two
// thresholds (Requirements 3.1, 3.2, 3.3).
func TestUnitMapStateBoundaries(t *testing.T) {
	tests := []struct {
		score int
		want  store.VaultState
	}{
		{0, store.StateInstantPass},
		{39, store.StateInstantPass},
		{40, store.StateRealityCheck},
		{74, store.StateRealityCheck},
		{75, store.StateLocked15M},
		{100, store.StateLocked15M},
	}

	for _, tt := range tests {
		t.Run(string(tt.want)+"_at_"+strconv.Itoa(tt.score), func(t *testing.T) {
			if got := MapState(tt.score); got != tt.want {
				t.Errorf("MapState(%d) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

// TestUnitThresholdConstants checks the named thresholds (Requirement 3.4).
func TestUnitThresholdConstants(t *testing.T) {
	if RealityCheckThreshold != 40 {
		t.Errorf("RealityCheckThreshold = %d, want 40", RealityCheckThreshold)
	}
	if LockThreshold != 75 {
		t.Errorf("LockThreshold = %d, want 75", LockThreshold)
	}
	if MaxScore != 100 {
		t.Errorf("MaxScore = %d, want 100", MaxScore)
	}
}

// TestUnitValidateFieldNames checks that each invalid signal is reported under
// its own field name, and that the nearest valid value is accepted
// (Requirements 1.2, 1.3, 1.4).
func TestUnitValidateFieldNames(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(s *Signals)
		wantField string // empty means the signals are valid
	}{
		{"amount of 0", func(s *Signals) { s.AmountINR = 0 }, "amount_inr"},
		{"negative amount", func(s *Signals) { s.AmountINR = -1 }, "amount_inr"},
		{"amount of 1 is valid", func(s *Signals) { s.AmountINR = 1 }, ""},
		{"negative beneficiary age", func(s *Signals) { s.BeneficiaryAgeHrs = -1 }, "beneficiary_age_hours"},
		{"beneficiary age of 0 is valid", func(s *Signals) { s.BeneficiaryAgeHrs = 0 }, ""},
		{"negative retry count", func(s *Signals) { s.RapidRetryCount = -1 }, "rapid_retry_count"},
		{"retry count of 0 is valid", func(s *Signals) { s.RapidRetryCount = 0 }, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := unitQuietSignals()
			tt.mutate(&s)

			err := Validate(s)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}

			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate() = %v, want *ValidationError", err)
			}
			if verr.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", verr.Field, tt.wantField)
			}
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Errorf("Error() = %q, want it to name %q", err.Error(), tt.wantField)
			}
		})
	}
}

// TestUnitValidateFieldOrder checks that the first failing field in the order
// amount, age, retry is the one reported when several fields are invalid.
func TestUnitValidateFieldOrder(t *testing.T) {
	tests := []struct {
		name      string
		signals   Signals
		wantField string
	}{
		{
			name:      "all three invalid reports amount",
			signals:   Signals{AmountINR: 0, BeneficiaryAgeHrs: -1, RapidRetryCount: -1},
			wantField: "amount_inr",
		},
		{
			name:      "amount and age invalid reports amount",
			signals:   Signals{AmountINR: -5, BeneficiaryAgeHrs: -1, RapidRetryCount: 0},
			wantField: "amount_inr",
		},
		{
			name:      "amount and retry invalid reports amount",
			signals:   Signals{AmountINR: 0, BeneficiaryAgeHrs: 5, RapidRetryCount: -2},
			wantField: "amount_inr",
		},
		{
			name:      "age and retry invalid reports age",
			signals:   Signals{AmountINR: 450, BeneficiaryAgeHrs: -1, RapidRetryCount: -1},
			wantField: "beneficiary_age_hours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var verr *ValidationError
			if err := Validate(tt.signals); !errors.As(err, &verr) {
				t.Fatalf("Validate() = %v, want *ValidationError", err)
			}
			if verr.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", verr.Field, tt.wantField)
			}
		})
	}
}

// TestUnitEvaluateReturnsValidationError checks that Evaluate passes the
// validation error through and returns the zero Result.
func TestUnitEvaluateReturnsValidationError(t *testing.T) {
	tests := []struct {
		name      string
		signals   Signals
		wantField string
	}{
		{
			name:      "invalid amount",
			signals:   Signals{AmountINR: 0, BeneficiaryAgeHrs: 72},
			wantField: "amount_inr",
		},
		{
			name:      "invalid age",
			signals:   Signals{AmountINR: 450, BeneficiaryAgeHrs: -1},
			wantField: "beneficiary_age_hours",
		},
		{
			name:      "invalid retry count",
			signals:   Signals{AmountINR: 450, BeneficiaryAgeHrs: 72, RapidRetryCount: -1},
			wantField: "rapid_retry_count",
		},
		{
			name:      "several invalid reports amount first",
			signals:   Signals{AmountINR: -1, BeneficiaryAgeHrs: -1, RapidRetryCount: -1},
			wantField: "amount_inr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Evaluate(tt.signals)

			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Evaluate() error = %v, want *ValidationError", err)
			}
			if verr.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", verr.Field, tt.wantField)
			}
			if res != (Result{}) {
				t.Errorf("Evaluate() result = %+v, want zero Result on error", res)
			}
		})
	}
}
