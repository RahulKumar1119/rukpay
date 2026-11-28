package risk

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// extraCare returns a recipient on extra-care protection.
func extraCare(verified, pressure bool) *Recipient {
	return &Recipient{Protection: store.ProtectionExtraCare, Verified: verified, Pressure: pressure}
}

// TestUnitCareSentenceTexts pins the exact text of the two care sentences of the
// design's table, so a changed word fails here even though the sentence scan
// only checks the wording rules.
func TestUnitCareSentenceTexts(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"sentCareList", sentCareList, "This recipient is on your extra-care list."},
		{"sentCareNew", sentCareNew, "This recipient is new or has not been checked yet."},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
		if v := sentenceViolations(tt.got); len(v) != 0 {
			t.Errorf("%s = %q breaks the wording rules: %v", tt.name, tt.got, v)
		}
	}

	// The sentence scan finds both constants, so it checks them with no edit.
	consts, _ := sentenceFile(t)
	for _, name := range []string{"sentCareList", "sentCareNew"} {
		if _, ok := consts[name]; !ok {
			t.Errorf("the sentence scan of sentences.go does not find %s", name)
		}
	}
}

// TestUnitCareConstants pins the two numbers that other packages read.
func TestUnitCareConstants(t *testing.T) {
	if CareCap != 10 {
		t.Errorf("CareCap = %d, want 10", CareCap)
	}
	if LargePaymentThreshold != 10_000 {
		t.Errorf("LargePaymentThreshold = %d, want 10000", LargePaymentThreshold)
	}
}

// TestUnitCarePointsTable checks every combination of Requirement 18 criteria 6
// and 7, and a protection level that no requirement defines, which earns nothing.
func TestUnitCarePointsTable(t *testing.T) {
	tests := []struct {
		name string
		r    *Recipient
		want int
	}{
		{"no recipient", nil, 0},
		{"STANDARD", &Recipient{Protection: store.ProtectionStandard}, 0},
		{"STANDARD, verified", &Recipient{Protection: store.ProtectionStandard, Verified: true}, 0},
		{"STANDARD, pressure", &Recipient{Protection: store.ProtectionStandard, Pressure: true}, 0},
		{"STANDARD, verified and pressure", &Recipient{Protection: store.ProtectionStandard, Verified: true, Pressure: true}, 0},
		{"EXTRA_CARE, not verified", extraCare(false, false), 10},
		{"EXTRA_CARE, not verified, pressure", extraCare(false, true), 10},
		{"EXTRA_CARE, verified, pressure", extraCare(true, true), 10},
		{"EXTRA_CARE, verified, no pressure", extraCare(true, false), 5},
		{"an empty protection level", &Recipient{Verified: true}, 0},
		{"an unknown protection level", &Recipient{Protection: "OTHER"}, 0},
		{"a lower case protection level", &Recipient{Protection: "extra_care"}, 0},
	}
	for _, tt := range tests {
		if got := carePoints(tt.r); got != tt.want {
			t.Errorf("%s: carePoints() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// TestUnitAssessCareExamples checks the care line end to end with the exact
// Contributions: the points, the sentence of the tier, the place of the line and
// the score cap that the care points can bring.
func TestUnitAssessCareExamples(t *testing.T) {
	const ref = "XXXX-XXXX-4821"
	careNew := Contribution{Signal: "beneficiary_care", Points: 10, Reason: "This recipient is new or has not been checked yet."}
	careList := Contribution{Signal: "beneficiary_care", Points: 5, Reason: "This recipient is on your extra-care list."}
	fullBaseline := Signals{AmountINR: 25000, BeneficiaryAgeHrs: 23, IsLateNight: true, ActiveUnknownCall: true, RapidRetryCount: 3}
	almostFull := fullBaseline
	almostFull.RapidRetryCount = 2

	tests := []struct {
		name      string
		in        Input
		want      []Contribution
		wantScore int
		wantCare  int
		wantState store.VaultState
	}{
		{
			name:      "a safe payment to an unchecked recipient gets 10 care points and still passes",
			in:        Input{Signals: safePresetSignals, Recipient: extraCare(false, false)},
			want:      []Contribution{careNew},
			wantScore: 10, wantCare: 10, wantState: store.StateInstantPass,
		},
		{
			name:      "a safe payment to a checked extra-care recipient gets 5",
			in:        Input{Signals: safePresetSignals, Recipient: extraCare(true, false)},
			want:      []Contribution{careList},
			wantScore: 5, wantCare: 5, wantState: store.StateInstantPass,
		},
		{
			name:      "a pressure mark gives 10 even for a checked recipient",
			in:        Input{Signals: safePresetSignals, Recipient: extraCare(true, true)},
			want:      []Contribution{careNew},
			wantScore: 10, wantCare: 10, wantState: store.StateInstantPass,
		},
		{
			name:      "a STANDARD recipient adds nothing",
			in:        Input{Signals: safePresetSignals, Recipient: &Recipient{Protection: store.ProtectionStandard, Verified: true}},
			want:      []Contribution{},
			wantScore: 0, wantCare: 0, wantState: store.StateInstantPass,
		},
		{
			name: "the line follows the baseline lines",
			in:   Input{Signals: mediumPresetSignals, Recipient: extraCare(false, false)},
			want: []Contribution{
				{Signal: "amount", Points: 20, Reason: "This is a large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				careNew,
			},
			wantScore: 55, wantCare: 10, wantState: store.StateRealityCheck,
		},
		{
			name: "care points can lift a payment from 65 to the lock threshold of 75",
			in:   Input{Signals: courierCustomsSignals, Recipient: extraCare(false, false)},
			want: []Contribution{
				{Signal: "amount", Points: 20, Reason: "This is a large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				careNew,
			},
			wantScore: 75, wantCare: 10, wantState: store.StateLocked15M,
		},
		{
			name: "care points can lift a payment from 35 to the reality check threshold of 40",
			in:   Input{Signals: Signals{AmountINR: 25000, BeneficiaryAgeHrs: 720}, Recipient: extraCare(true, false)},
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				careList,
			},
			wantScore: 40, wantCare: 5, wantState: store.StateRealityCheck,
		},
		{
			name: "a total of exactly 100 has no score cap line",
			in:   Input{Signals: almostFull, Recipient: extraCare(true, false)},
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "rapid_retries", Points: 5, Reason: "The payment has been tried a couple of times in a row."},
				careList,
			},
			wantScore: 100, wantCare: 5, wantState: store.StateLocked15M,
		},
		{
			name: "a total of 105 has a score cap line of minus 5 after the care line",
			in:   Input{Signals: almostFull, Recipient: extraCare(false, false)},
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "rapid_retries", Points: 5, Reason: "The payment has been tried a couple of times in a row."},
				careNew,
				{Signal: "score_cap", Points: -5, Reason: "The total cannot go above its upper limit."},
			},
			wantScore: 100, wantCare: 10, wantState: store.StateLocked15M,
		},
		{
			name: "a baseline of 100 with care points is cut back to 100",
			in:   Input{Signals: fullBaseline, Recipient: extraCare(false, false)},
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "rapid_retries", Points: 10, Reason: "The payment has been tried several times in a row."},
				careNew,
				{Signal: "score_cap", Points: -10, Reason: "The total cannot go above its upper limit."},
			},
			wantScore: 100, wantCare: 10, wantState: store.StateLocked15M,
		},
		{
			name: "every cap in one Explanation: behaviour_cap, then the care line, then score_cap",
			in: Input{
				Signals: highRiskPresetSignals, Now: unitNow,
				History: unitEvents(20, 10, 1000, ref), BeneficiaryRef: ref,
				Recipient: extraCare(false, true),
			},
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "velocity", Points: 15, Reason: "A lot of payments were made in the last few minutes."},
				{Signal: "daily_frequency", Points: 10, Reason: "There have been a lot of payments in the last day."},
				{Signal: "repeat_attempts", Points: 10, Reason: "Many payments went to this same recipient in the last hour."},
				{Signal: "amount_deviation", Points: 15, Reason: "This payment is far larger than your recent ones."},
				{Signal: "pattern_test_then_large", Points: 15, Reason: "Several small payments to this recipient were followed by a large one."},
				{Signal: "behaviour_cap", Points: -40, Reason: "The extra points from recent activity have an upper limit."},
				careNew,
				{Signal: "score_cap", Points: -25, Reason: "The total cannot go above its upper limit."},
			},
			wantScore: 100, wantCare: 10, wantState: store.StateLocked15M,
		},
		{
			name: "a baseline of 0 with the behaviour cap and 10 care points reaches 35 and still passes",
			in: Input{
				Signals: quiet, Now: unitNow, History: unitEvents(20, 10, 100, ref), BeneficiaryRef: ref,
				Recipient: extraCare(false, false),
			},
			want: []Contribution{
				{Signal: "velocity", Points: 15, Reason: "A lot of payments were made in the last few minutes."},
				{Signal: "daily_frequency", Points: 10, Reason: "There have been a lot of payments in the last day."},
				{Signal: "repeat_attempts", Points: 10, Reason: "Many payments went to this same recipient in the last hour."},
				{Signal: "behaviour_cap", Points: -10, Reason: "The extra points from recent activity have an upper limit."},
				careNew,
			},
			wantScore: 35, wantCare: 10, wantState: store.StateInstantPass,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(tt.in)
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(a.Explanation, tt.want) {
				t.Errorf("Explanation = %+v, want %+v", a.Explanation, tt.want)
			}
			if a.Score != tt.wantScore || a.Care != tt.wantCare || a.State != tt.wantState {
				t.Errorf("Score, Care, State = %d, %d, %q, want %d, %d, %q", a.Score, a.Care, a.State, tt.wantScore, tt.wantCare, tt.wantState)
			}
			if sum := sumPoints(a.Explanation); sum != a.Score {
				t.Errorf("the points add up to %d, Score = %d", sum, a.Score)
			}
		})
	}
}

// TestUnitNilRecipientGivesTheResultFromBeforeCare checks that a payment with no
// recipient scores exactly as it did before care points existed, on the three
// preset signal sets and the three story signal sets, and that the two care
// sentences appear nowhere in its Explanation.
func TestUnitNilRecipientGivesTheResultFromBeforeCare(t *testing.T) {
	tests := []struct {
		name      string
		signals   Signals
		wantScore int
	}{
		{"safe preset", safePresetSignals, 0},
		{"medium preset", mediumPresetSignals, 45},
		{"high risk preset", highRiskPresetSignals, 90},
		{"digital arrest", digitalArrestSignals, 90},
		{"courier and customs", courierCustomsSignals, 65},
		{"kyc expiry", kycExpirySignals, 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: tt.signals})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if a.Score != tt.wantScore || a.Baseline != tt.wantScore || a.Care != 0 {
				t.Errorf("Score, Baseline, Care = %d, %d, %d, want %d, %d, 0", a.Score, a.Baseline, a.Care, tt.wantScore, tt.wantScore)
			}
			for _, c := range a.Explanation {
				if c.Signal == SignalBeneficiaryCare || strings.Contains(c.Reason, "extra-care") {
					t.Errorf("a payment with no recipient has the care line %+v", c)
				}
			}
		})
	}
}

// TestUnitAssessDoesNotChangeTheRecipient checks the one thing a recipient
// pointer could go wrong with: the fields and the pointer stay as given.
func TestUnitAssessDoesNotChangeTheRecipient(t *testing.T) {
	for _, r := range []*Recipient{extraCare(false, true), extraCare(true, false), {Protection: store.ProtectionStandard, Pressure: true}} {
		pointer, before := r, *r
		if _, err := Assess(Input{Signals: mediumPresetSignals, Recipient: r}); err != nil {
			t.Fatalf("Assess() error = %v, want nil", err)
		}
		if r != pointer || *r != before {
			t.Errorf("Assess changed the recipient %+v to %+v", before, *r)
		}
	}
}
