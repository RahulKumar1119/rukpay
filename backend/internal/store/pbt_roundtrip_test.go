package store

import (
	"testing"

	"pgregory.net/rapid"
)

// Feature: behavioral-risk-engine, Property 8: Create then Get round trip
//
// **Validates: Requirements 7.2, 7.4, 9.6**
func TestRoundTripCreateThenGetReturnsTheRecord(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		rec := contractRecordGen().Draw(rt, "record")

		contractMustCreate(rt, env.vs, rec)

		contractWantRecord(rt, env.vs, rec.ID, rec)
	})
}
