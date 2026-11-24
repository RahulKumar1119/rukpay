package risk

import (
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 3: Scoring is deterministic and does not touch its inputs
//
// **Validates: Requirements 1.8, 9.5, 9.10**
func TestAssessIsDeterministicAndLeavesItsInputAlone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		other := genInput().Draw(t, "other")
		before := cloneInput(in)

		first, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		// A call with a different input in between must not change a later
		// result, which rules out state kept from one call to the next.
		if _, err := Assess(other); err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", other, err)
		}

		for call := 2; call <= 3; call++ {
			again, err := Assess(in)
			if err != nil {
				t.Fatalf("call %d: Assess returned an error for valid signals: %v", call, err)
			}
			if !reflect.DeepEqual(again, first) {
				t.Fatalf("call %d: %+v differs from the first result %+v", call, again, first)
			}
		}

		if !reflect.DeepEqual(in, before) {
			t.Fatalf("Assess changed its input: now %+v, was %+v", in, before)
		}
	})
}
