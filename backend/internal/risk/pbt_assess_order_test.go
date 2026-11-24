package risk

import (
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 3: Scoring is deterministic and does not touch its inputs
//
// Two inputs that differ only in the order of events with equal times give equal
// Assessments. The test finds every run of events with the same time in a
// generated snapshot, puts each run in a random order, and compares. It fails
// when no generated input had a run whose order actually changed, so it cannot
// pass with nothing to check.
//
// **Validates: Requirements 1.8, 9.5**
func TestAssessIgnoresTheOrderOfEqualTimeEvents(t *testing.T) {
	var reordered int
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		shuffled := cloneInput(in)
		for i := 0; i < len(in.History); {
			j := i + 1
			for j < len(in.History) && in.History[j].At == in.History[i].At {
				j++
			}
			if j-i > 1 {
				run := append([]HistoryEvent(nil), in.History[i:j]...)
				copy(shuffled.History[i:j], rapid.Permutation(run).Draw(t, "run"))
			}
			i = j
		}
		if !reflect.DeepEqual(shuffled.History, in.History) {
			reordered++
		}

		first, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}
		second, err := Assess(shuffled)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", shuffled, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("a different order of equal-time events changed the result:\n%+v\n%+v", first, second)
		}
	})
	if reordered == 0 {
		t.Fatal("no generated snapshot had equal-time events in a new order, so the check passed with nothing to check")
	}
}
