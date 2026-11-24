package store

import (
	"context"
	"testing"

	"pgregory.net/rapid"
)

// explanationSignals are the Signal_Id values the risk engine writes. The store
// accepts any string, and this list only makes the generated lines look real.
var explanationSignals = []string{
	"amount", "beneficiary_age", "late_night", "unknown_call", "rapid_retries",
	"velocity", "daily_frequency", "repeat_attempts", "amount_deviation",
	"pattern_test_then_large", "behaviour_cap", "beneficiary_care", "score_cap",
}

// explanationGen draws an Explanation that is nil, empty or filled with up to
// 14 lines. Points run below zero because the two cap lines are negative.
func explanationGen() *rapid.Generator[[]Contribution] {
	line := rapid.Custom(func(t *rapid.T) Contribution {
		return Contribution{
			Signal: rapid.SampledFrom(explanationSignals).Draw(t, "signal"),
			Points: rapid.IntRange(-35, 100).Draw(t, "points"),
			Reason: rapid.StringMatching(`^[A-Z][a-z ]{0,60}\.$`).Draw(t, "reason"),
		}
	})
	return rapid.OneOf(
		rapid.Just[[]Contribution](nil),
		rapid.Just([]Contribution{}),
		rapid.SliceOfN(line, 1, 14),
	)
}

// Feature: risk-intelligence, Property 13: The Explanation survives storage, state changes and the view
//
// This is the storage half of Property 13. For any Explanation (nil, empty or
// filled), a record written through MemoryStore or through DynamoDBStore over
// the fake reads back with equal Contributions in equal order, nil as nil and
// empty as empty, through Create and through UpdateIf, and no caller can change
// the stored Explanation through a slice it passed in or got back. The vault
// half (state changes and the view) is checked in the vault package.
//
// **Validates: Requirements 2.3, 2.4, 2.5, 23.4**
func TestExplanationSurvivesStorageAndStateChanges(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()

		first := explanationGen().Draw(rt, "first_explanation")
		rec := contractRecordGen().Draw(rt, "record")
		rec.Explanation = explanationCopy(first)
		contractMustCreate(rt, env.vs, rec)

		// A change to the slice given to Create must not reach the store.
		for i := range rec.Explanation {
			rec.Explanation[i].Points++
		}
		got := contractMustGet(rt, env.vs, rec.ID)
		if !explanationSame(got.Explanation, first) {
			rt.Fatalf("after Create, Explanation = %#v, want %#v", got.Explanation, first)
		}

		// A change to the returned slice must not reach the store.
		for i := range got.Explanation {
			got.Explanation[i].Reason = "Changed."
		}
		if again := contractMustGet(rt, env.vs, rec.ID); !explanationSame(again.Explanation, first) {
			rt.Fatalf("a change to a returned slice reached the store: %#v, want %#v", again.Explanation, first)
		}

		// UpdateIf writes the whole item, so the new Explanation replaces the old.
		second := explanationGen().Draw(rt, "second_explanation")
		next := contractMustGet(rt, env.vs, rec.ID)
		next.State = rapid.SampledFrom(contractStates()).Draw(rt, "next_state")
		next.Explanation = explanationCopy(second)
		if err := env.vs.UpdateIf(ctx, rec.ID, rec.State, next); err != nil {
			rt.Fatalf("UpdateIf returned %v", err)
		}
		for i := range next.Explanation {
			next.Explanation[i].Signal = "changed"
		}
		final := contractMustGet(rt, env.vs, rec.ID)
		if !explanationSame(final.Explanation, second) {
			rt.Fatalf("after UpdateIf, Explanation = %#v, want %#v", final.Explanation, second)
		}
		if final.State != next.State || final.RiskScore != next.RiskScore {
			rt.Fatalf("UpdateIf changed fields outside the Explanation: state %v score %d", final.State, final.RiskScore)
		}
	})
}
