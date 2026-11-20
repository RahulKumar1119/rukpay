package risk

import (
	"flag"
	"fmt"
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// mapStateMinChecks is the minimum number of generated inputs per run
// (Requirement 5 asks for at least 1000 on the PBT suite).
const mapStateMinChecks = 1000

// init raises rapid's default check count to mapStateMinChecks when the
// -rapid.checks flag is registered and set lower, so `go test` runs enough
// inputs without the CLI flag. An explicit -rapid.checks on the command line
// is applied later by flag.Parse and still takes effect.
func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	current, err := strconv.Atoi(f.Value.String())
	if err != nil || current >= mapStateMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(mapStateMinChecks)); err != nil {
		panic(fmt.Sprintf("risk: raise rapid.checks: %v", err))
	}
}

// mapStateScoreGen mixes the exact boundary scores with the full 0 to 100
// range, so the thresholds are hit often instead of by chance.
func mapStateScoreGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 39, 40, 74, 75, 100}),
		rapid.IntRange(0, 100),
	)
}

// **Validates: Requirements 3.1, 3.2, 3.3**
//
// Feature: behavioral-risk-engine, Property 4: State mapping follows the thresholds
func TestMapStateFollowsThresholds(t *testing.T) {
	// The expectations below use the literal numbers 40 and 75 on purpose, so
	// a wrong RealityCheckThreshold or LockThreshold constant is caught.
	rapid.Check(t, func(t *rapid.T) {
		score := mapStateScoreGen().Draw(t, "score")

		got := MapState(score)

		switch {
		case score < 40:
			if got != store.StateInstantPass {
				t.Fatalf("score %d below 40: got %q, want %q", score, got, store.StateInstantPass)
			}
		case score <= 74:
			if got != store.StateRealityCheck {
				t.Fatalf("score %d in 40..74: got %q, want %q", score, got, store.StateRealityCheck)
			}
		default:
			if got != store.StateLocked15M {
				t.Fatalf("score %d at or above 75: got %q, want %q", score, got, store.StateLocked15M)
			}
		}
	})
}
