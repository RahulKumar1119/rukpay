package risk

import (
	"math"
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// windowSignals are the three Signal_Ids of Requirement 10.
var windowSignals = []string{"velocity", "daily_frequency", "repeat_attempts"}

// pointsOf returns the points of the Contribution with the given Signal_Id, or 0
// when the Explanation holds none. An absent Contribution counts as 0 points.
func pointsOf(explanation []Contribution, signal string) int {
	for _, c := range explanation {
		if c.Signal == signal {
			return c.Points
		}
	}
	return 0
}

// windowEntries returns the velocity, daily_frequency and repeat_attempts
// Contributions of an Explanation as model entries, in the order they appear.
func windowEntries(explanation []Contribution) []refEntry {
	var out []refEntry
	for _, c := range explanation {
		for _, s := range windowSignals {
			if c.Signal == s {
				out = append(out, refEntry{Signal: c.Signal, Points: c.Points})
			}
		}
	}
	return out
}

// equalEntries reports whether two lists of model entries are equal.
func equalEntries(a, b []refEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Feature: risk-intelligence, Property 6: Window counts and tiers
//
// For any History_Snapshot, evaluation time and Beneficiary_Ref, the velocity,
// daily_frequency and repeat_attempts Contributions equal an independent model
// that counts events Within_Window of 600, 86400 and 3600 seconds with
// arbitrary-precision arithmetic: an event exactly the window old is out, one
// second younger is in, an event in the future is in, and repeat counts only
// non-empty, exactly equal refs. The rapid_retries points do not change with the
// snapshot, and the payment being evaluated is in no count because the counts
// read the snapshot alone.
//
// **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8**
func TestWindowSignalsMatchTheReferenceModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		got := windowEntries(a.Explanation)
		want := refWindowEntries(in)
		if !equalEntries(got, want) {
			t.Fatalf("window Contributions = %v, reference model = %v (counts %+v)", got, want, refWindowCounts(in))
		}
		if problem := refReasonProblem(contributionsFor(a, windowSignals...), false); problem != "" {
			t.Fatal(problem)
		}

		// Requirement 10 criterion 7: the rapid_retries points come from the
		// client-sent count alone, so an empty snapshot gives the same line.
		bare := cloneInput(in)
		bare.History = nil
		b, err := Assess(bare)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", bare, err)
		}
		if g, w := pointsOf(a.Explanation, "rapid_retries"), pointsOf(b.Explanation, "rapid_retries"); g != w {
			t.Fatalf("rapid_retries = %d with a snapshot and %d without", g, w)
		}
	})
}

// Feature: risk-intelligence, Property 6: Window counts and tiers
//
// Each window signal takes its points from its own count alone. Changing every
// amount of the snapshot and every field of the Signals leaves all three
// Contributions as they were, because no window signal reads them. Changing every
// event ref and the ref of the payment leaves velocity and daily_frequency as
// they were, because only repeat_attempts reads refs.
//
// **Validates: Requirements 10.1, 10.2, 10.3, 10.6**
func TestWindowSignalsReadOnlyTheirOwnInputs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}
		base := windowEntries(a.Explanation)

		// Other amounts and other Signals: the same three Contributions.
		other := cloneInput(in)
		other.Signals = genSignals().Draw(t, "other_signals")
		for i := range other.History {
			other.History[i].AmountINR = genEventAmount().Draw(t, "other_amount")
		}
		b, err := Assess(other)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", other, err)
		}
		if got := windowEntries(b.Explanation); !equalEntries(got, base) {
			t.Fatalf("other amounts and signals give %v, want %v", got, base)
		}

		// Other refs: velocity and daily_frequency are the same.
		reref := cloneInput(in)
		reref.BeneficiaryRef = genPaymentRef().Draw(t, "other_ref")
		for i := range reref.History {
			reref.History[i].BeneficiaryRef = drawEventRef(t, reref.BeneficiaryRef)
		}
		c, err := Assess(reref)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", reref, err)
		}
		for _, signal := range []string{"velocity", "daily_frequency"} {
			if g, w := pointsOf(c.Explanation, signal), pointsOf(a.Explanation, signal); g != w {
				t.Fatalf("%s = %d with other refs, want %d", signal, g, w)
			}
		}
	})
}

// Feature: risk-intelligence, Property 6: Window counts and tiers
//
// The points of velocity, daily_frequency and repeat_attempts never fall when
// History_Events are added one at a time to a snapshot with the Signals, the
// evaluation time and the recipient unchanged, so that the count each reads
// rises from 0. A Contribution that is absent counts as 0 points.
//
// **Validates: Requirements 10.6, 10.8, 27.6**
func TestWindowPointsNeverFallAsEventsAreAddedFromAnyHistory(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")

		var last [3]int
		for k := 0; k <= len(in.History); k++ {
			step := cloneInput(in)
			step.History = append([]HistoryEvent(nil), in.History[:k]...)
			a, err := Assess(step)
			if err != nil {
				t.Fatalf("Assess(%+v) returned an error for valid signals: %v", step, err)
			}
			for i, signal := range windowSignals {
				if p := pointsOf(a.Explanation, signal); p < last[i] {
					t.Fatalf("%s fell from %d to %d when event %d of %d was added", signal, last[i], p, k, len(in.History))
				} else {
					last[i] = p
				}
			}
		}
	})
}

// Feature: risk-intelligence, Property 6: Window counts and tiers
//
// The same holds when every added event is inside all three windows and has the
// ref of the payment, so that the three counts all rise together from 0 up to
// 100. The points equal the model at every step and end at the top tier of each
// signal.
//
// **Validates: Requirements 10.1, 10.2, 10.3, 10.6, 10.8, 27.6**
func TestWindowPointsNeverFallAsTheCountsRiseTo100(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := Input{
			Signals:        genSignals().Draw(t, "signals"),
			Now:            genNow().Draw(t, "now"),
			BeneficiaryRef: rapid.SampledFrom([]string{"r***@okaxis", "XXXX-XXXX-4821", "+91-XXXXXX3210"}).Draw(t, "ref"),
		}

		// Every event is less than 600 seconds old or in the future, and so is
		// inside the three windows. The times are sorted, oldest first.
		events := make([]HistoryEvent, 100)
		for i := range events {
			at := timeBefore(in.Now, rapid.Int64Range(0, 599).Draw(t, "age"))
			if rapid.IntRange(0, 9).Draw(t, "future") == 0 {
				at = timeAfter(in.Now, rapid.Int64Range(1, 100).Draw(t, "future_seconds"))
			}
			events[i] = HistoryEvent{At: at, AmountINR: genEventAmount().Draw(t, "amount"), BeneficiaryRef: in.BeneficiaryRef}
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })

		var last [3]int
		for k := 0; k <= len(events); k++ {
			step := in
			step.History = events[:k]
			a, err := Assess(step)
			if err != nil {
				t.Fatalf("Assess(%+v) returned an error for valid signals: %v", step, err)
			}
			if got, want := windowEntries(a.Explanation), refWindowEntries(step); !equalEntries(got, want) {
				t.Fatalf("with %d events the window Contributions are %v, reference model %v", k, got, want)
			}
			for i, signal := range windowSignals {
				p := pointsOf(a.Explanation, signal)
				if p < last[i] {
					t.Fatalf("%s fell from %d to %d at %d events", signal, last[i], p, k)
				}
				last[i] = p
			}
		}
		if last != [3]int{15, 10, 10} {
			t.Fatalf("with 100 events the points are %v, want the top tiers [15 10 10]", last)
		}
	})
}

// TestWindowSignalsHandleEveryInt64Time checks the window rule at the ends of
// the int64 range, where now minus the event time is larger than int64. An event
// at the smallest time is far outside every window of a time at the largest, and
// an event later than the evaluation time is inside.
func TestWindowSignalsHandleEveryInt64Time(t *testing.T) {
	signals := Signals{AmountINR: 450, BeneficiaryAgeHrs: 720}
	event := func(at int64) HistoryEvent {
		return HistoryEvent{At: at, AmountINR: 100, BeneficiaryRef: "r***@okaxis"}
	}
	many := func(at int64, n int) []HistoryEvent {
		out := make([]HistoryEvent, n)
		for i := range out {
			out[i] = event(at)
		}
		return out
	}

	tests := []struct {
		name    string
		now     int64
		history []HistoryEvent
		want    []refEntry
	}{
		{"events at the smallest time are outside every window of the largest time", math.MaxInt64, many(math.MinInt64, 20), nil},
		{"events at the largest time are inside every window of the smallest time", math.MinInt64, many(math.MaxInt64, 20),
			[]refEntry{{"velocity", 15}, {"daily_frequency", 10}, {"repeat_attempts", 10}}},
		{"events at the evaluation time count", math.MinInt64, many(math.MinInt64, 3), []refEntry{{"velocity", 5}, {"repeat_attempts", 5}}},
		{"599 seconds before the largest time is inside", math.MaxInt64, many(math.MaxInt64-599, 3), []refEntry{{"velocity", 5}, {"repeat_attempts", 5}}},
		{"600 seconds before the largest time is outside the velocity window", math.MaxInt64, many(math.MaxInt64-600, 3), []refEntry{{"repeat_attempts", 5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{Signals: signals, Now: tt.now, History: tt.history, BeneficiaryRef: "r***@okaxis"}
			a, err := Assess(in)
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			if got := windowEntries(a.Explanation); !equalEntries(got, tt.want) {
				t.Errorf("window Contributions = %v, want %v", got, tt.want)
			}
			if got, want := windowEntries(a.Explanation), refWindowEntries(in); !equalEntries(got, want) {
				t.Errorf("window Contributions = %v, reference model = %v", got, want)
			}
		})
	}
}
