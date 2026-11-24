package risk

import (
	"errors"
	"reflect"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// TestUnitSentenceTexts pins the exact text of every sentence of the design's
// table, so a changed word fails here even though the property tests only check
// the wording rules.
func TestUnitSentenceTexts(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"sentAmountMid", sentAmountMid, "This payment is bigger than a small everyday purchase."},
		{"sentAmountHigh", sentAmountHigh, "This is a large payment."},
		{"sentAmountTop", sentAmountTop, "This is a very large payment."},
		{"sentAgeNew", sentAgeNew, "The recipient was added less than a day ago."},
		{"sentAgeRecent", sentAgeRecent, "The recipient was added in the last few days."},
		{"sentLateNight", sentLateNight, "The payment is being made late at night."},
		{"sentUnknownCall", sentUnknownCall, "A call from someone you do not know is going on."},
		{"sentRetriesMid", sentRetriesMid, "The payment has been tried a couple of times in a row."},
		{"sentRetriesHigh", sentRetriesHigh, "The payment has been tried several times in a row."},
		{"sentBehaviourCap", sentBehaviourCap, "The extra points from recent activity have an upper limit."},
		{"sentScoreCap", sentScoreCap, "The total cannot go above its upper limit."},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestUnitSignalIDs pins the Signal_Id strings, which are part of the response.
func TestUnitSignalIDs(t *testing.T) {
	tests := []struct{ got, want string }{
		{SignalAmount, "amount"},
		{SignalBeneficiaryAge, "beneficiary_age"},
		{SignalLateNight, "late_night"},
		{SignalUnknownCall, "unknown_call"},
		{SignalRapidRetries, "rapid_retries"},
		{SignalVelocity, "velocity"},
		{SignalDailyFrequency, "daily_frequency"},
		{SignalRepeatAttempts, "repeat_attempts"},
		{SignalAmountDeviation, "amount_deviation"},
		{SignalPatternPrefix, "pattern_"},
		{SignalBehaviourCap, "behaviour_cap"},
		{SignalBeneficiaryCare, "beneficiary_care"},
		{SignalScoreCap, "score_cap"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Signal_Id = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestUnitAssessPresets checks the three presets end to end, with the exact
// Contributions in order: the Safe preset has an empty, present list, the Medium
// preset two lines, and the High_Risk preset four.
func TestUnitAssessPresets(t *testing.T) {
	tests := []struct {
		name      string
		signals   Signals
		wantScore int
		wantState store.VaultState
		want      []Contribution
	}{
		{
			name:      "450 rupees to a known contact has nothing to explain",
			signals:   Signals{AmountINR: 450, BeneficiaryAgeHrs: 720},
			wantScore: 0,
			wantState: store.StateInstantPass,
			want:      []Contribution{},
		},
		{
			name:      "18000 rupees to a new contact",
			signals:   Signals{AmountINR: 18000, BeneficiaryAgeHrs: 2},
			wantScore: 45,
			wantState: store.StateRealityCheck,
			want: []Contribution{
				{Signal: "amount", Points: 20, Reason: "This is a large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
			},
		},
		{
			name:      "85000 rupees to a new contact late at night on a call",
			signals:   Signals{AmountINR: 85000, BeneficiaryAgeHrs: 2, IsLateNight: true, ActiveUnknownCall: true},
			wantScore: 90,
			wantState: store.StateLocked15M,
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: tt.signals})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if a.Score != tt.wantScore || a.Baseline != tt.wantScore {
				t.Errorf("Score = %d, Baseline = %d, want both %d", a.Score, a.Baseline, tt.wantScore)
			}
			if a.State != tt.wantState {
				t.Errorf("State = %q, want %q", a.State, tt.wantState)
			}
			if a.Explanation == nil {
				t.Fatal("Explanation is nil, want a present list")
			}
			if !reflect.DeepEqual(a.Explanation, tt.want) {
				t.Errorf("Explanation = %+v, want %+v", a.Explanation, tt.want)
			}
		})
	}
}

// TestUnitAssessTiersAndFullScore checks the lower tiers, which the presets do
// not reach, and the largest possible baseline of exactly 100 with no cap line.
func TestUnitAssessTiersAndFullScore(t *testing.T) {
	tests := []struct {
		name    string
		signals Signals
		want    []Contribution
	}{
		{
			name:    "2000 rupees, a 24 hour old contact and 2 retries",
			signals: Signals{AmountINR: 2000, BeneficiaryAgeHrs: 24, RapidRetryCount: 2},
			want: []Contribution{
				{Signal: "amount", Points: 10, Reason: "This payment is bigger than a small everyday purchase."},
				{Signal: "beneficiary_age", Points: 10, Reason: "The recipient was added in the last few days."},
				{Signal: "rapid_retries", Points: 5, Reason: "The payment has been tried a couple of times in a row."},
			},
		},
		{
			name:    "10000 rupees, a 71 hour old contact and 3 retries",
			signals: Signals{AmountINR: 10000, BeneficiaryAgeHrs: 71, RapidRetryCount: 3},
			want: []Contribution{
				{Signal: "amount", Points: 20, Reason: "This is a large payment."},
				{Signal: "beneficiary_age", Points: 10, Reason: "The recipient was added in the last few days."},
				{Signal: "rapid_retries", Points: 10, Reason: "The payment has been tried several times in a row."},
			},
		},
		{
			name:    "just below every tier",
			signals: Signals{AmountINR: 1999, BeneficiaryAgeHrs: 72, RapidRetryCount: 1},
			want:    []Contribution{},
		},
		{
			name:    "every signal at its top tier adds up to exactly 100",
			signals: Signals{AmountINR: 25000, BeneficiaryAgeHrs: 23, IsLateNight: true, ActiveUnknownCall: true, RapidRetryCount: 3},
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "rapid_retries", Points: 10, Reason: "The payment has been tried several times in a row."},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: tt.signals})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(a.Explanation, tt.want) {
				t.Errorf("Explanation = %+v, want %+v", a.Explanation, tt.want)
			}
		})
	}
}

// TestUnitAssessInvalidSignals checks that Assess returns the validation error
// of Validate and the zero Assessment.
func TestUnitAssessInvalidSignals(t *testing.T) {
	tests := []struct {
		name      string
		signals   Signals
		wantField string
	}{
		{"zero amount", Signals{AmountINR: 0, BeneficiaryAgeHrs: 72}, "amount_inr"},
		{"negative age", Signals{AmountINR: 450, BeneficiaryAgeHrs: -1}, "beneficiary_age_hours"},
		{"negative retries", Signals{AmountINR: 450, BeneficiaryAgeHrs: 72, RapidRetryCount: -1}, "rapid_retry_count"},
		{"several invalid reports the amount", Signals{AmountINR: -1, BeneficiaryAgeHrs: -1, RapidRetryCount: -1}, "amount_inr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: tt.signals})
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Assess() error = %v, want *ValidationError", err)
			}
			if verr.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", verr.Field, tt.wantField)
			}
			if !reflect.DeepEqual(a, Assessment{}) {
				t.Errorf("Assess() = %+v with an error, want the zero Assessment", a)
			}
		})
	}
}
