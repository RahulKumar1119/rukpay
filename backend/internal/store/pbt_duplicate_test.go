package store

import (
	"context"
	"errors"
	"testing"

	"pgregory.net/rapid"
)

// Feature: behavioral-risk-engine, Property 9: Duplicate Create is rejected and leaves the record unchanged
//
// **Validates: Requirements 7.3, 10.3**
func TestDuplicateCreateIsRejectedAndLeavesRecordUnchanged(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		first := contractRecordGen().Draw(rt, "first")
		second := contractRecordGen().Draw(rt, "second")
		second.ID = first.ID

		contractMustCreate(rt, env.vs, first)

		err := env.vs.Create(context.Background(), second)
		if !errors.Is(err, ErrAlreadyExists) {
			rt.Fatalf("second Create(id %q) = %v, want ErrAlreadyExists", second.ID, err)
		}

		contractWantRecord(rt, env.vs, first.ID, first)
	})
}
