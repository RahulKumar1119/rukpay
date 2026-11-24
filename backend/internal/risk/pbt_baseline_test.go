package risk

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 4: The baseline matches the shipped rules, and no history changes nothing
//
// The five baseline Contributions are compared with the reference model of
// reference_test.go. With an empty History_Snapshot and no Beneficiary, the
// score and state are compared with frozenEvaluate, a frozen copy of the
// shipped Evaluate. For every input the score is at least the baseline and the
// state is never lower than the one the baseline alone gives.
//
// **Validates: Requirements 1.9, 9.3, 9.4, 9.12, 23.1**
func TestBaselineMatchesShippedRules(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		// The baseline lines are the lines of the baseline class, in order. A
		// Signal_Id is absent exactly when it earns 0 points.
		var got []refEntry
		for _, c := range a.Explanation {
			if refClass(c.Signal) == refClassBaseline {
				got = append(got, refEntry{Signal: c.Signal, Points: c.Points})
			}
		}
		want := refBaseline(in.Signals)
		if len(got) != len(want) {
			t.Fatalf("baseline lines = %v, reference model = %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("baseline lines = %v, reference model = %v", got, want)
			}
		}

		wantBaseline := refBaselineTotal(want)
		if a.Baseline != wantBaseline {
			t.Fatalf("Baseline = %d, reference model = %d", a.Baseline, wantBaseline)
		}

		// Requirement 9.4: nothing lowers the score below the baseline.
		if a.Score < a.Baseline {
			t.Fatalf("Score = %d is below Baseline = %d", a.Score, a.Baseline)
		}
		// Requirement 9.12: nothing lowers the state below the baseline state.
		baselineState := frozenMapState(a.Baseline)
		if refStateRank(a.State) < refStateRank(baselineState) {
			t.Fatalf("State = %q is lower than the baseline state %q", a.State, baselineState)
		}

		// Requirements 9.3 and 23.1: with no history and no recipient, the
		// shipped Evaluate gives the same score and state.
		if baselineOnly(in) {
			frozen, err := frozenEvaluate(in.Signals)
			if err != nil {
				t.Fatalf("frozenEvaluate(%+v) returned an error for valid signals: %v", in.Signals, err)
			}
			if a.Score != frozen.Score || a.State != frozen.State {
				t.Fatalf("Assess gives score %d and state %q, the shipped Evaluate gives %d and %q",
					a.Score, a.State, frozen.Score, frozen.State)
			}
		}
	})
}
