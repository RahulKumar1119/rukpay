package store

import (
	"context"
	"errors"
	"testing"

	"pgregory.net/rapid"
)

// Feature: behavioral-risk-engine, Property 10: UpdateIf is a compare-and-swap on state
//
// The expected state and the new record's state are drawn from all six states
// independently, so the checked state pairs include transitions that no state
// machine would allow (for example VETOED to INSTANT_PASS). The store must
// accept every pair when the expected state matches (Requirement 7.9).
//
// **Validates: Requirements 7.6, 7.7, 7.9, 10.4**
func TestUpdateIfIsCompareAndSwapOnState(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		stored := contractRecordGen().Draw(rt, "stored")
		next := contractRecordGen().Draw(rt, "next")
		next.ID = stored.ID

		// Bias the expected state toward the stored one so that both the
		// matching and the mismatching branch run often.
		expected := rapid.OneOf(
			contractStateGen(),
			rapid.Just(stored.State),
		).Draw(rt, "expected")

		contractMustCreate(rt, env.vs, stored)

		err := env.vs.UpdateIf(context.Background(), stored.ID, expected, next)

		if expected == stored.State {
			if err != nil {
				rt.Fatalf("UpdateIf(expected %q, stored %q -> next %q) = %v, want nil",
					expected, stored.State, next.State, err)
			}
			contractWantRecord(rt, env.vs, stored.ID, next)
			return
		}
		if !errors.Is(err, ErrConditionFailed) {
			rt.Fatalf("UpdateIf(expected %q, stored %q) = %v, want ErrConditionFailed",
				expected, stored.State, err)
		}
		contractWantRecord(rt, env.vs, stored.ID, stored)
	})
}
