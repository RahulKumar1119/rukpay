package risk

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// wholeNumberJSON matches a JSON number with no fraction and no exponent.
var wholeNumberJSON = regexp.MustCompile(`^-?[0-9]+$`)

// Feature: risk-intelligence, Property 2: Explanation shape, order and uniqueness
//
// **Validates: Requirements 1.2, 1.3, 1.7, 1.10, 2.9**
func TestExplanationShapeOrderAndUniqueness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		// Requirement 1.7: a present list, never nil, even when it is empty.
		if a.Explanation == nil {
			t.Fatalf("Assess(%+v).Explanation is nil, want a present list", in)
		}

		seen := make(map[string]bool, len(a.Explanation))
		lastRank := -1
		for _, c := range a.Explanation {
			rank := refRank(c.Signal)
			if rank < 0 {
				t.Fatalf("unknown Signal_Id %q in %+v", c.Signal, a.Explanation)
			}
			// Requirement 1.3: no Signal_Id twice, in the fixed order. Every
			// pattern Contribution shares one position, so equal ranks are
			// allowed for them alone, and each pattern id still appears once.
			if seen[c.Signal] {
				t.Fatalf("Signal_Id %q appears twice in %+v", c.Signal, a.Explanation)
			}
			seen[c.Signal] = true
			if rank < lastRank || (rank == lastRank && rank != refRankPatternSlot) {
				t.Fatalf("Signal_Id %q is out of order in %+v", c.Signal, a.Explanation)
			}
			lastRank = rank

			// Requirement 1.2: a reason with at least one non-space character.
			if strings.TrimSpace(c.Reason) == "" {
				t.Fatalf("Contribution %+v has a blank reason", c)
			}
		}

		// Requirement 2.9: each item is an object with exactly the keys signal,
		// points and reason, with a string, a whole number and a string.
		data, err := json.Marshal(a.Explanation)
		if err != nil {
			t.Fatalf("marshal explanation: %v", err)
		}
		if len(a.Explanation) == 0 && string(data) != "[]" {
			t.Fatalf("empty explanation is written as %s, want []", data)
		}
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			t.Fatalf("explanation JSON %s is not a list of objects: %v", data, err)
		}
		if len(items) != len(a.Explanation) {
			t.Fatalf("JSON holds %d items, explanation holds %d", len(items), len(a.Explanation))
		}
		for _, item := range items {
			if len(item) != 3 {
				t.Fatalf("item %v has %d keys, want 3", item, len(item))
			}
			signal, okSignal := item["signal"]
			points, okPoints := item["points"]
			reason, okReason := item["reason"]
			if !okSignal || !okPoints || !okReason {
				t.Fatalf("item %v lacks one of signal, points, reason", item)
			}
			if !strings.HasPrefix(string(signal), `"`) || !strings.HasPrefix(string(reason), `"`) {
				t.Fatalf("signal %s and reason %s must be JSON strings", signal, reason)
			}
			if !wholeNumberJSON.Match(points) {
				t.Fatalf("points %s must be a JSON number with no fraction", points)
			}
		}
	})
}
