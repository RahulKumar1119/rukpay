package risk

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 1: Contributions add up to the score and the caps are exact
//
// The whole Explanation of any valid evaluation equals the independent model of
// reference_behaviour_test.go: the baseline lines, the behavioural lines, a
// behaviour_cap line of 25 minus the behavioural sum only when that sum is above
// 25, and a score_cap line of 100 minus the total only when the capped parts add
// up to more than 100. The same holds for the Behaviour_Points, the Risk_Score
// and the Initial_State, and each reason is the sentence the model holds for its
// Signal_Id and tier. The inputs include behavioural sums below, at and above 25,
// totals at and above 100, and the largest amount as the payment and as an event.
//
// **Validates: Requirements 1.5, 1.6, 1.12, 9.1, 9.2, 10.8, 11.7**
func TestAssessMatchesTheBehaviourReferenceModel(t *testing.T) {
	requireModelPatternTable(t)

	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		want := refAssess(refPatternTable, in)
		var got []refEntry
		for _, c := range a.Explanation {
			got = append(got, refEntry{Signal: c.Signal, Points: c.Points})
		}
		if !equalEntries(got, want.Entries) {
			t.Fatalf("Explanation = %v, reference model = %v (behavioural sum %d)", got, want.Entries, want.BehaviourSum)
		}
		if a.Baseline != want.Baseline || a.Behaviour != want.Behaviour || a.Score != want.Score || a.State != want.State {
			t.Fatalf("Baseline, Behaviour, Score, State = %d, %d, %d, %q, reference model = %d, %d, %d, %q",
				a.Baseline, a.Behaviour, a.Score, a.State, want.Baseline, want.Behaviour, want.Score, want.State)
		}

		// Every reason is the sentence of its Signal_Id and tier (Requirement 3.1).
		if problem := refReasonProblem(a.Explanation, false); problem != "" {
			t.Fatal(problem)
		}

		// The cap numbers, written out. A behavioural sum of exactly 25 or a
		// total of exactly 100 adds no cap line.
		lines := map[string]int{}
		for _, c := range a.Explanation {
			lines[c.Signal] = c.Points
		}
		if _, has := lines["behaviour_cap"]; has != (want.BehaviourSum > 25) {
			t.Fatalf("behaviour_cap line present = %v with a behavioural sum of %d", has, want.BehaviourSum)
		}
		if _, has := lines["score_cap"]; has != (want.Baseline+want.Behaviour > 100) {
			t.Fatalf("score_cap line present = %v with a total of %d", has, want.Baseline+want.Behaviour)
		}
		if a.Behaviour > 25 || a.Score > 100 {
			t.Fatalf("Behaviour = %d and Score = %d, want at most 25 and 100", a.Behaviour, a.Score)
		}
	})
}
