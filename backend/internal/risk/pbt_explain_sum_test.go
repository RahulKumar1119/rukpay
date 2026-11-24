package risk

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: risk-intelligence, Property 1: Contributions add up to the score and the caps are exact
//
// The test reads any Assess output through the Signal_Id classes of
// Requirement 1, so it also covers the cap lines once the behavioural and care
// points exist: a behaviour_cap line only when the behavioural sum is above 25
// and a score_cap line only when the total is above 100.
//
// **Validates: Requirements 1.1, 1.4, 1.5, 1.6, 1.10, 1.12, 2.8, 9.1, 9.2, 11.7**
func TestExplanationAddsUpToScoreAndCapsAreExact(t *testing.T) {
	const (
		behaviourCap = 25  // Requirement 9 criterion 2
		careCap      = 10  // Requirement 9 criterion 2
		scoreLimit   = 100 // Requirement 9 criterion 1
	)

	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		var sum, baseline, behaviour, care int
		var behaviourCapLine, scoreCapLine *Contribution
		for i := range a.Explanation {
			c := a.Explanation[i]
			sum += c.Points
			switch refClass(c.Signal) {
			case refClassBaseline:
				baseline += c.Points
			case refClassBehaviour:
				behaviour += c.Points
			case refClassCare:
				care += c.Points
			case refClassBehaviourCap:
				behaviourCapLine = &a.Explanation[i]
			case refClassScoreCap:
				scoreCapLine = &a.Explanation[i]
			default:
				t.Fatalf("unknown Signal_Id %q in %+v", c.Signal, a.Explanation)
			}

			// Requirement 1.10: ordinary lines add points, cap lines take them away.
			isCap := refClass(c.Signal) == refClassBehaviourCap || refClass(c.Signal) == refClassScoreCap
			if isCap && c.Points >= 0 {
				t.Fatalf("cap line %q has points %d, want below 0", c.Signal, c.Points)
			}
			if !isCap && c.Points <= 0 {
				t.Fatalf("line %q has points %d, want above 0", c.Signal, c.Points)
			}
		}

		// Requirements 1.4 and 2.8: the signed points add up to the score.
		if sum != a.Score {
			t.Fatalf("points add up to %d, Score = %d, explanation %+v", sum, a.Score, a.Explanation)
		}
		if a.Score < 0 || a.Score > scoreLimit {
			t.Fatalf("Score = %d, want 0..%d", a.Score, scoreLimit)
		}
		if a.Baseline != baseline {
			t.Fatalf("Baseline = %d, baseline lines add up to %d", a.Baseline, baseline)
		}
		if care > careCap {
			t.Fatalf("care points = %d, want at most %d", care, careCap)
		}

		// Requirements 1.5 and 1.12: the behaviour cap line exists exactly when
		// the behavioural sum is above 25, and then it brings the sum to 25.
		if behaviour > behaviourCap {
			if behaviourCapLine == nil {
				t.Fatalf("behavioural sum %d is above %d but there is no behaviour_cap line", behaviour, behaviourCap)
			}
			if want := behaviourCap - behaviour; behaviourCapLine.Points != want {
				t.Fatalf("behaviour_cap points = %d, want %d", behaviourCapLine.Points, want)
			}
			behaviour = behaviourCap
		} else if behaviourCapLine != nil {
			t.Fatalf("behaviour_cap line %+v with a behavioural sum of %d", *behaviourCapLine, behaviour)
		}

		// Requirements 1.6, 1.12 and 9.1: the score cap line exists exactly when
		// the capped parts add up to more than 100, and then it brings them to 100.
		total := baseline + behaviour + care
		if total > scoreLimit {
			if scoreCapLine == nil {
				t.Fatalf("total %d is above %d but there is no score_cap line", total, scoreLimit)
			}
			if want := scoreLimit - total; scoreCapLine.Points != want {
				t.Fatalf("score_cap points = %d, want %d", scoreCapLine.Points, want)
			}
			total = scoreLimit
		} else if scoreCapLine != nil {
			t.Fatalf("score_cap line %+v with a total of %d", *scoreCapLine, total)
		}
		if a.Score != total {
			t.Fatalf("Score = %d, want the capped total %d", a.Score, total)
		}
	})
}
