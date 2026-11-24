package risk

import (
	"errors"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 10: Invalid signals produce the validation error
//
// **Validates: Requirements 1.11, 9.13**
func TestAssessInvalidSignalsGiveTheValidationError(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInvalidInput().Draw(t, "input")

		want := refInvalidField(in.Signals)
		if want == "" {
			t.Fatalf("generator drew valid signals %+v", in.Signals)
		}

		// Validate is the shipped check. Assess must report the same field.
		var fromValidate *ValidationError
		if err := Validate(in.Signals); !errors.As(err, &fromValidate) {
			t.Fatalf("Validate(%+v) = %v, want *ValidationError", in.Signals, err)
		}

		a, err := Assess(in)
		var fromAssess *ValidationError
		if !errors.As(err, &fromAssess) {
			t.Fatalf("Assess(%+v) error = %v, want *ValidationError", in, err)
		}
		if fromAssess.Field != fromValidate.Field || fromAssess.Field != want {
			t.Fatalf("Assess field = %q, Validate field = %q, reference field = %q",
				fromAssess.Field, fromValidate.Field, want)
		}

		// Requirement 1.11: no score and no Explanation, not even an empty one.
		if !reflect.DeepEqual(a, Assessment{}) {
			t.Fatalf("Assess(%+v) returned %+v with an error, want the zero Assessment", in, a)
		}
		if a.Explanation != nil {
			t.Fatalf("Assess(%+v) returned an Explanation with an error", in)
		}
	})
}
