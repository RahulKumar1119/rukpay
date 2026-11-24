package risk

import (
	"math"
	"reflect"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// unitNow is the evaluation time of the example tests below.
const unitNow = 1_700_000_000

// quiet are Signals that earn no baseline point, so the Contributions of an
// example are the behavioural ones alone.
var quiet = Signals{AmountINR: 450, BeneficiaryAgeHrs: 720}

// unitEvents returns n identical events that are age seconds old.
func unitEvents(n int, age, amount int64, ref string) []HistoryEvent {
	out := make([]HistoryEvent, n)
	for i := range out {
		out[i] = HistoryEvent{At: unitNow - age, AmountINR: amount, BeneficiaryRef: ref}
	}
	return out
}

// contributionsFor returns the Contributions of a for the given Signal_Ids, in
// the order of the Explanation, with their reasons.
func contributionsFor(a Assessment, signals ...string) []Contribution {
	var out []Contribution
	for _, c := range a.Explanation {
		for _, s := range signals {
			if c.Signal == s {
				out = append(out, c)
			}
		}
	}
	return out
}

// TestUnitBehaviourSentenceTexts pins the exact text of every sentence that this
// group added to the design's table, so a changed word fails here even though
// the scan only checks the wording rules.
func TestUnitBehaviourSentenceTexts(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"sentVelocityLow", sentVelocityLow, "Several payments were made in the last few minutes."},
		{"sentVelocityMid", sentVelocityMid, "Many payments were made in the last few minutes."},
		{"sentVelocityHigh", sentVelocityHigh, "A lot of payments were made in the last few minutes."},
		{"sentDailyLow", sentDailyLow, "There have been quite a few payments in the last day."},
		{"sentDailyHigh", sentDailyHigh, "There have been a lot of payments in the last day."},
		{"sentRepeatLow", sentRepeatLow, "A few payments went to this same recipient in the last hour."},
		{"sentRepeatHigh", sentRepeatHigh, "Many payments went to this same recipient in the last hour."},
		{"sentDeviationMid", sentDeviationMid, "This payment is much larger than your recent ones."},
		{"sentDeviationHigh", sentDeviationHigh, "This payment is far larger than your recent ones."},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestUnitBehaviourConstants pins the caps and windows of the design.
func TestUnitBehaviourConstants(t *testing.T) {
	tests := []struct {
		name      string
		got, want int
	}{
		{"BehaviourCap", BehaviourCap, 25},
		{"VelocityWindow", VelocityWindow, 600},
		{"FrequencyWindow", FrequencyWindow, 86400},
		{"RepeatWindow", RepeatWindow, 3600},
		{"DeviationMinEvents", DeviationMinEvents, 5},
		{"DeviationMinAmount", DeviationMinAmount, 2000},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

// TestUnitWithinWindowEdges checks the window rule on both sides of every edge
// and at the ends of the int64 range.
func TestUnitWithinWindowEdges(t *testing.T) {
	tests := []struct {
		name       string
		at, now, w int64
		wantInside bool
	}{
		{"599 seconds old is inside 600", 1000 - 599, 1000, 600, true},
		{"600 seconds old is outside 600", 1000 - 600, 1000, 600, false},
		{"601 seconds old is outside 600", 1000 - 601, 1000, 600, false},
		{"no age is inside", 1000, 1000, 600, true},
		{"one second old is inside 2", 999, 1000, 2, true},
		{"one second old is outside 1", 999, 1000, 1, false},
		{"a time one second later is inside", 1001, 1000, 600, true},
		{"a time far later is inside", math.MaxInt64, 0, 600, true},
		{"the smallest time is outside for the largest time", math.MinInt64, math.MaxInt64, 600, false},
		{"the smallest time is outside even a huge window", math.MinInt64, math.MaxInt64, math.MaxInt64, false},
		{"the largest time is inside for the smallest time", math.MaxInt64, math.MinInt64, 600, true},
		{"a negative time 599 seconds old is inside", -1000, -1000 + 599, 600, true},
		{"a negative time 600 seconds old is outside", -1000, -1000 + 600, 600, false},
		{"599 seconds before the largest time is inside", math.MaxInt64 - 599, math.MaxInt64, 600, true},
		{"600 seconds before the largest time is outside", math.MaxInt64 - 600, math.MaxInt64, 600, false},
		{"an age of the largest int64 is outside a window of one less", 0, math.MaxInt64, math.MaxInt64, false},
		{"an age one below the largest int64 is inside", 1, math.MaxInt64, math.MaxInt64, true},
		{"a window of 0 holds no past event", 1000, 1000, 0, false},
		{"a negative window holds no past event", 999, 1000, -5, false},
	}
	for _, tt := range tests {
		if got := within(tt.at, tt.now, tt.w); got != tt.wantInside {
			t.Errorf("%s: within(%d, %d, %d) = %v, want %v", tt.name, tt.at, tt.now, tt.w, got, tt.wantInside)
		}
	}
}

// TestUnitWindowTiers checks every tier of velocity, daily_frequency and
// repeat_attempts end to end through Assess, with the points and the exact
// sentence of each.
func TestUnitWindowTiers(t *testing.T) {
	const ref = "r***@okaxis"
	type tier struct {
		count  int
		points int
		reason string
	}

	// Velocity: events 10 seconds old to no recipient, so only velocity and,
	// below 10 events, nothing else reads them.
	velocity := []tier{
		{0, 0, ""}, {1, 0, ""}, {2, 0, ""},
		{3, 5, "Several payments were made in the last few minutes."},
		{4, 5, "Several payments were made in the last few minutes."},
		{5, 10, "Many payments were made in the last few minutes."},
		{6, 10, "Many payments were made in the last few minutes."},
		{7, 10, "Many payments were made in the last few minutes."},
		{8, 15, "A lot of payments were made in the last few minutes."},
		{9, 15, "A lot of payments were made in the last few minutes."},
	}
	// Daily: events 1000 seconds old, inside the day and outside the velocity
	// window.
	daily := []tier{
		{0, 0, ""}, {9, 0, ""},
		{10, 5, "There have been quite a few payments in the last day."},
		{19, 5, "There have been quite a few payments in the last day."},
		{20, 10, "There have been a lot of payments in the last day."},
		{21, 10, "There have been a lot of payments in the last day."},
	}
	// Repeat: events 1000 seconds old to the same recipient.
	repeat := []tier{
		{0, 0, ""}, {1, 0, ""},
		{2, 5, "A few payments went to this same recipient in the last hour."},
		{3, 5, "A few payments went to this same recipient in the last hour."},
		{4, 10, "Many payments went to this same recipient in the last hour."},
		{5, 10, "Many payments went to this same recipient in the last hour."},
	}

	run := func(t *testing.T, signal string, tiers []tier, age int64, eventRef, paymentRef string) {
		for _, tt := range tiers {
			a, err := Assess(Input{Signals: quiet, Now: unitNow, History: unitEvents(tt.count, age, 100, eventRef), BeneficiaryRef: paymentRef})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			got := contributionsFor(a, signal)
			if tt.points == 0 {
				if len(got) != 0 {
					t.Errorf("%s with %d events: got %+v, want none", signal, tt.count, got)
				}
				continue
			}
			want := []Contribution{{Signal: signal, Points: tt.points, Reason: tt.reason}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s with %d events: got %+v, want %+v", signal, tt.count, got, want)
			}
		}
	}
	t.Run("velocity", func(t *testing.T) { run(t, "velocity", velocity, 10, "", "") })
	t.Run("daily_frequency", func(t *testing.T) { run(t, "daily_frequency", daily, 1000, "", "") })
	t.Run("repeat_attempts", func(t *testing.T) { run(t, "repeat_attempts", repeat, 1000, ref, ref) })
}

// TestUnitRepeatAttemptsNeedAnExactNonEmptyRef checks Requirement 10 criteria 3
// and 4: empty refs never match each other, and a ref that differs in case or
// by a space is not the same ref.
func TestUnitRepeatAttemptsNeedAnExactNonEmptyRef(t *testing.T) {
	const ref = "r***@okaxis"
	tests := []struct {
		name       string
		eventRef   string
		paymentRef string
		wantPoints int
	}{
		{"equal refs repeat", ref, ref, 10},
		{"empty refs never match", "", "", 0},
		{"an empty payment ref matches no event", ref, "", 0},
		{"an empty event ref does not match a ref", "", ref, 0},
		{"a different case is a different ref", "R***@OKAXIS", ref, 0},
		{"a trailing space is a different ref", ref + " ", ref, 0},
		{"a leading space is a different ref", " " + ref, ref, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: quiet, Now: unitNow, History: unitEvents(4, 1000, 100, tt.eventRef), BeneficiaryRef: tt.paymentRef})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if got := pointsOf(a.Explanation, "repeat_attempts"); got != tt.wantPoints {
				t.Errorf("repeat_attempts = %d, want %d", got, tt.wantPoints)
			}
		})
	}
}

// TestUnitFraudPatternExamples checks the worked example of Requirement 11
// criterion 5 and the cases of criterion 8, with the exact reason.
func TestUnitFraudPatternExamples(t *testing.T) {
	const ref = "XXXX-XXXX-4821"
	const reason = "Several small payments to this recipient were followed by a large one."
	signals := func(amount int64) Signals { return Signals{AmountINR: amount, BeneficiaryAgeHrs: 720} }
	// small returns n small payments to the recipient, 3599 seconds old.
	small := func(n int, amount int64) []HistoryEvent { return unitEvents(n, 3599, amount, ref) }

	tests := []struct {
		name    string
		amount  int64
		ref     string
		history []HistoryEvent
		want    bool
	}{
		{"three payments of 1000 then 10000", 10000, ref, small(3, 1000), true},
		{"two small payments are too few", 10000, ref, small(2, 1000), false},
		{"payments of 1001 are not small", 10000, ref, small(3, 1001), false},
		{"two small and one of 1001", 10000, ref, append(small(2, 1000), small(1, 1001)...), false},
		{"a payment of 9999 is not large", 9999, ref, small(3, 1000), false},
		{"payments exactly 3600 seconds old are outside", 10000, ref, unitEvents(3, 3600, 1000, ref), false},
		{"payments to another recipient do not count", 10000, ref, unitEvents(3, 100, 1000, "r***@okaxis"), false},
		{"an empty payment ref earns nothing", 10000, "", unitEvents(3, 100, 1000, ""), false},
		{"other events neither count nor cancel", 10000, ref,
			append(append(small(3, 1000), unitEvents(2, 100, 90000, "r***@okaxis")...), unitEvents(1, 7200, 5, ref)...), true},
		{"100 matching events award it once", 10000, ref, small(100, 1), true},
		{"the largest amount is large", math.MaxInt64, ref, small(3, 1000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: signals(tt.amount), Now: unitNow, History: tt.history, BeneficiaryRef: tt.ref})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			got := contributionsFor(a, "pattern_test_then_large")
			if !tt.want {
				if len(got) != 0 {
					t.Errorf("got %+v, want no pattern Contribution", got)
				}
				return
			}
			want := []Contribution{{Signal: "pattern_test_then_large", Points: 15, Reason: reason}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestUnitCapExamples checks the worked examples of Requirement 1 criteria 5 and
// 6 and Requirement 11 criterion 7, and the two edges where no cap line exists.
func TestUnitCapExamples(t *testing.T) {
	tests := []struct {
		name          string
		signals       Signals
		history       []HistoryEvent
		want          []Contribution
		wantBehaviour int
		wantScore     int
		wantState     store.VaultState
	}{
		{
			// 15 velocity points and 15 deviation points are 30, 5 over the cap.
			name:    "a behavioural sum of 30 gives a behaviour_cap line of -5",
			signals: Signals{AmountINR: 2000, BeneficiaryAgeHrs: 720},
			history: unitEvents(8, 10, 100, ""),
			want: []Contribution{
				{Signal: "amount", Points: 10, Reason: "This payment is bigger than a small everyday purchase."},
				{Signal: "velocity", Points: 15, Reason: "A lot of payments were made in the last few minutes."},
				{Signal: "amount_deviation", Points: 15, Reason: "This payment is far larger than your recent ones."},
				{Signal: "behaviour_cap", Points: -5, Reason: "The extra points from recent activity have an upper limit."},
			},
			wantBehaviour: 25,
			wantScore:     35,
			wantState:     store.StateInstantPass,
		},
		{
			// 15 velocity points and 10 daily points are exactly 25.
			name:    "a behavioural sum of exactly 25 gives no behaviour_cap line",
			signals: quiet,
			history: unitEvents(20, 10, 100, ""),
			want: []Contribution{
				{Signal: "velocity", Points: 15, Reason: "A lot of payments were made in the last few minutes."},
				{Signal: "daily_frequency", Points: 10, Reason: "There have been a lot of payments in the last day."},
			},
			wantBehaviour: 25,
			wantScore:     25,
			wantState:     store.StateInstantPass,
		},
		{
			// A baseline of 90 and 15 velocity points are 105, 5 over the limit.
			name:    "a total of 105 gives a score_cap line of -5",
			signals: highRiskPresetSignals,
			history: unitEvents(8, 10, 50000, ""),
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "velocity", Points: 15, Reason: "A lot of payments were made in the last few minutes."},
				{Signal: "score_cap", Points: -5, Reason: "The total cannot go above its upper limit."},
			},
			wantBehaviour: 15,
			wantScore:     100,
			wantState:     store.StateLocked15M,
		},
		{
			// A baseline of 90 and 10 velocity points are exactly 100.
			name:    "a total of exactly 100 gives no score_cap line",
			signals: highRiskPresetSignals,
			history: unitEvents(5, 10, 50000, ""),
			want: []Contribution{
				{Signal: "amount", Points: 35, Reason: "This is a very large payment."},
				{Signal: "beneficiary_age", Points: 25, Reason: "The recipient was added less than a day ago."},
				{Signal: "late_night", Points: 10, Reason: "The payment is being made late at night."},
				{Signal: "unknown_call", Points: 20, Reason: "A call from someone you do not know is going on."},
				{Signal: "velocity", Points: 10, Reason: "Many payments were made in the last few minutes."},
			},
			wantBehaviour: 10,
			wantScore:     100,
			wantState:     store.StateLocked15M,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{Signals: tt.signals, Now: unitNow, History: tt.history})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(a.Explanation, tt.want) {
				t.Errorf("Explanation = %+v, want %+v", a.Explanation, tt.want)
			}
			if a.Behaviour != tt.wantBehaviour || a.Score != tt.wantScore || a.State != tt.wantState {
				t.Errorf("Behaviour, Score, State = %d, %d, %q, want %d, %d, %q",
					a.Behaviour, a.Score, a.State, tt.wantBehaviour, tt.wantScore, tt.wantState)
			}
		})
	}
}

// TestUnitEveryBehaviouralLineAndBothCapsInOrder checks the fixed order of
// Requirement 1 criterion 3 with every behavioural signal and both caps present:
// the five baseline lines, velocity, daily_frequency, repeat_attempts,
// amount_deviation, the pattern, behaviour_cap and score_cap.
func TestUnitEveryBehaviouralLineAndBothCapsInOrder(t *testing.T) {
	const ref = "XXXX-XXXX-4821"
	in := Input{
		// A baseline of 90, and an amount 85 times the largest earlier payment.
		Signals: highRiskPresetSignals,
		Now:     unitNow,
		// 20 small payments to the recipient, 10 seconds old.
		History:        unitEvents(20, 10, 1000, ref),
		BeneficiaryRef: ref,
	}
	a, err := Assess(in)
	if err != nil {
		t.Fatalf("Assess() error = %v, want nil", err)
	}
	want := []Contribution{
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
		{Signal: "score_cap", Points: -15, Reason: "The total cannot go above its upper limit."},
	}
	if !reflect.DeepEqual(a.Explanation, want) {
		t.Errorf("Explanation = %+v, want %+v", a.Explanation, want)
	}
	if a.Baseline != 90 || a.Behaviour != 25 || a.Score != 100 || a.State != store.StateLocked15M {
		t.Errorf("Baseline, Behaviour, Score, State = %d, %d, %d, %q, want 90, 25, 100, %q",
			a.Baseline, a.Behaviour, a.Score, a.State, store.StateLocked15M)
	}
}

// TestUnitAssessLeavesTheHistoryAlone checks that scoring changes neither the
// order nor the content of the snapshot, even when it is not sorted.
func TestUnitAssessLeavesTheHistoryAlone(t *testing.T) {
	history := []HistoryEvent{
		{At: unitNow - 5000, AmountINR: 9000, BeneficiaryRef: "b"},
		{At: unitNow - 10, AmountINR: 100, BeneficiaryRef: "a"},
		{At: unitNow + 50, AmountINR: 70, BeneficiaryRef: ""},
		{At: unitNow - 800, AmountINR: 1000, BeneficiaryRef: "a"},
		{At: unitNow - 10, AmountINR: 3, BeneficiaryRef: "a"},
	}
	before := append([]HistoryEvent(nil), history...)
	if _, err := Assess(Input{Signals: Signals{AmountINR: 30000, BeneficiaryAgeHrs: 1}, Now: unitNow, History: history, BeneficiaryRef: "a"}); err != nil {
		t.Fatalf("Assess() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(history, before) {
		t.Errorf("Assess changed the snapshot: now %+v, was %+v", history, before)
	}
}
