package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// explanationAttr is the persisted attribute name of VaultRecord.Explanation.
const explanationAttr = "explanation"

// explanationSample is a filled Explanation that includes a negative cap line.
func explanationSample() []Contribution {
	return []Contribution{
		{Signal: "amount", Points: 20, Reason: "The amount is larger than usual."},
		{Signal: "velocity", Points: 15, Reason: "Several payments were made close together."},
		{Signal: "behaviour_cap", Points: -5, Reason: "Extra points from behaviour are limited."},
	}
}

// explanationRecord returns a valid record for id that carries expl. The
// times are whole seconds and ExpiresAt lies after the contract epoch.
func explanationRecord(id string, expl []Contribution) VaultRecord {
	epoch := contractEpoch()
	return VaultRecord{
		ID:        id,
		State:     StateLocked15M,
		RiskScore: 90,
		AmountINR: 85000,
		CreatedAt: epoch,
		ExpiresAt: epoch.Add(time.Hour),
		Reason:    "A protective pause is on.",

		Explanation: expl,
	}
}

// explanationCopy copies expl and keeps nil as nil and empty as empty.
func explanationCopy(expl []Contribution) []Contribution {
	if expl == nil {
		return nil
	}
	return append([]Contribution{}, expl...)
}

// explanationSame reports whether a and b are equal, with nil and empty
// counted as different values.
func explanationSame(a, b []Contribution) bool {
	return (a == nil) == (b == nil) && reflect.DeepEqual(a, b)
}

// TestExplanationAttributeEncoding pins how the pinned attributevalue release
// writes and reads the Explanation (Requirements 2.4, 2.5, 23.4). The stores
// rely on three facts: a nil slice is left out, an empty slice is written as
// an empty list, and each Contribution is a map with the keys signal, points
// and reason. If an SDK upgrade changes one of them, this test fails first.
func TestExplanationAttributeEncoding(t *testing.T) {
	t.Run("nil slice is left out", func(t *testing.T) {
		item, err := attributevalue.MarshalMap(explanationRecord("v1", nil))
		if err != nil {
			t.Fatalf("MarshalMap returned %v", err)
		}
		if _, ok := item[explanationAttr]; ok {
			t.Fatalf("a nil Explanation was written as %v, want no attribute", item[explanationAttr])
		}
	})

	t.Run("empty slice is written as an empty list", func(t *testing.T) {
		item, err := attributevalue.MarshalMap(explanationRecord("v1", []Contribution{}))
		if err != nil {
			t.Fatalf("MarshalMap returned %v", err)
		}
		list, ok := item[explanationAttr].(*types.AttributeValueMemberL)
		if !ok {
			t.Fatalf("an empty Explanation was written as %T, want a list", item[explanationAttr])
		}
		if len(list.Value) != 0 {
			t.Fatalf("empty list holds %d items, want 0", len(list.Value))
		}
	})

	t.Run("each Contribution is a map of three keys", func(t *testing.T) {
		item, err := attributevalue.MarshalMap(explanationRecord("v1", explanationSample()))
		if err != nil {
			t.Fatalf("MarshalMap returned %v", err)
		}
		list, ok := item[explanationAttr].(*types.AttributeValueMemberL)
		if !ok || len(list.Value) != len(explanationSample()) {
			t.Fatalf("Explanation was written as %#v, want a list of %d items", item[explanationAttr], len(explanationSample()))
		}
		for i, v := range list.Value {
			m, ok := v.(*types.AttributeValueMemberM)
			if !ok {
				t.Fatalf("item %d is %T, want a map", i, v)
			}
			if len(m.Value) != 3 {
				t.Errorf("item %d has %d keys, want 3", i, len(m.Value))
			}
			if _, ok := m.Value["signal"].(*types.AttributeValueMemberS); !ok {
				t.Errorf("item %d signal is %T, want a string", i, m.Value["signal"])
			}
			if _, ok := m.Value["points"].(*types.AttributeValueMemberN); !ok {
				t.Errorf("item %d points is %T, want a number", i, m.Value["points"])
			}
			if _, ok := m.Value["reason"].(*types.AttributeValueMemberS); !ok {
				t.Errorf("item %d reason is %T, want a string", i, m.Value["reason"])
			}
		}
	})

	t.Run("an item without the attribute reads back nil", func(t *testing.T) {
		item, err := attributevalue.MarshalMap(explanationRecord("v1", nil))
		if err != nil {
			t.Fatalf("MarshalMap returned %v", err)
		}
		var rec VaultRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			t.Fatalf("UnmarshalMap returned %v", err)
		}
		if rec.Explanation != nil {
			t.Fatalf("Explanation = %#v, want nil", rec.Explanation)
		}
	})

	t.Run("an empty list reads back empty and not nil", func(t *testing.T) {
		item, err := attributevalue.MarshalMap(explanationRecord("v1", []Contribution{}))
		if err != nil {
			t.Fatalf("MarshalMap returned %v", err)
		}
		var rec VaultRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			t.Fatalf("UnmarshalMap returned %v", err)
		}
		if rec.Explanation == nil || len(rec.Explanation) != 0 {
			t.Fatalf("Explanation = %#v, want an empty non-nil slice", rec.Explanation)
		}
	})

	t.Run("a filled list reads back equal", func(t *testing.T) {
		item, err := attributevalue.MarshalMap(explanationRecord("v1", explanationSample()))
		if err != nil {
			t.Fatalf("MarshalMap returned %v", err)
		}
		var rec VaultRecord
		if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
			t.Fatalf("UnmarshalMap returned %v", err)
		}
		if !explanationSame(rec.Explanation, explanationSample()) {
			t.Fatalf("Explanation = %#v, want %#v", rec.Explanation, explanationSample())
		}
	})
}

// TestExplanationRoundTripThroughBothStores writes a nil, an empty and a
// filled Explanation through each adapter and reads each back (Requirements
// 2.4, 2.5, 23.4).
func TestExplanationRoundTripThroughBothStores(t *testing.T) {
	cases := []struct {
		name string
		expl []Contribution
	}{
		{"absent", nil},
		{"present and empty", []Contribution{}},
		{"one line", explanationSample()[:1]},
		{"three lines with a negative cap line", explanationSample()},
	}
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		for i, tc := range cases {
			id := "explanation-roundtrip-" + string(rune('a'+i))
			rec := explanationRecord(id, explanationCopy(tc.expl))
			if err := env.vs.Create(ctx, rec); err != nil {
				t.Fatalf("%s: Create returned %v", tc.name, err)
			}
			got, err := env.vs.Get(ctx, id)
			if err != nil {
				t.Fatalf("%s: Get returned %v", tc.name, err)
			}
			if !explanationSame(got.Explanation, tc.expl) {
				t.Errorf("%s: Explanation = %#v, want %#v", tc.name, got.Explanation, tc.expl)
			}
			if !contractEqual(got, rec) {
				t.Errorf("%s: record = %+v, want %+v", tc.name, got, rec)
			}
		}
	})
}

// TestExplanationUpdateIfReplacesTheWholeItem checks that UpdateIf moves an
// Explanation between absent, present and empty, because it writes the whole
// item (Requirement 2.7).
func TestExplanationUpdateIfReplacesTheWholeItem(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		steps := [][]Contribution{nil, explanationSample(), {}, explanationSample()[:2], nil}

		rec := explanationRecord("explanation-update", explanationCopy(steps[0]))
		if err := env.vs.Create(ctx, rec); err != nil {
			t.Fatalf("Create returned %v", err)
		}
		prev := rec.State
		for i, expl := range steps[1:] {
			next := rec
			next.State = StateVetoed
			if prev == StateVetoed {
				next.State = StateLocked15M
			}
			next.Explanation = explanationCopy(expl)
			if err := env.vs.UpdateIf(ctx, rec.ID, prev, next); err != nil {
				t.Fatalf("step %d: UpdateIf returned %v", i+1, err)
			}
			got, err := env.vs.Get(ctx, rec.ID)
			if err != nil {
				t.Fatalf("step %d: Get returned %v", i+1, err)
			}
			if !explanationSame(got.Explanation, expl) {
				t.Fatalf("step %d: Explanation = %#v, want %#v", i+1, got.Explanation, expl)
			}
			prev = next.State
		}
	})
}

// TestExplanationIsCopiedByBothStores checks that no caller can change a
// stored Explanation through a slice it passed in or got back (Requirement
// 8.3, as widened by the new slice field).
func TestExplanationIsCopiedByBothStores(t *testing.T) {
	scribble := func(expl []Contribution) {
		for i := range expl {
			expl[i].Signal = "changed"
			expl[i].Points += 1000
			expl[i].Reason = "Changed."
		}
	}

	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		want := explanationSample()

		// The slice passed to Create.
		passed := explanationCopy(want)
		rec := explanationRecord("explanation-copy", passed)
		if err := env.vs.Create(ctx, rec); err != nil {
			t.Fatalf("Create returned %v", err)
		}
		scribble(passed)
		got, err := env.vs.Get(ctx, rec.ID)
		if err != nil {
			t.Fatalf("Get returned %v", err)
		}
		if !explanationSame(got.Explanation, want) {
			t.Fatalf("a change to the slice given to Create reached the store: %#v", got.Explanation)
		}

		// The slice returned by Get.
		scribble(got.Explanation)
		again, err := env.vs.Get(ctx, rec.ID)
		if err != nil {
			t.Fatalf("second Get returned %v", err)
		}
		if !explanationSame(again.Explanation, want) {
			t.Fatalf("a change to a returned slice reached the store: %#v", again.Explanation)
		}

		// The slice passed to UpdateIf.
		sent := explanationCopy(want)
		next := rec
		next.State = StateVetoed
		next.Explanation = sent
		if err := env.vs.UpdateIf(ctx, rec.ID, rec.State, next); err != nil {
			t.Fatalf("UpdateIf returned %v", err)
		}
		scribble(sent)
		after, err := env.vs.Get(ctx, rec.ID)
		if err != nil {
			t.Fatalf("third Get returned %v", err)
		}
		if !explanationSame(after.Explanation, want) {
			t.Fatalf("a change to the slice given to UpdateIf reached the store: %#v", after.Explanation)
		}
	})
}

// TestDynamoDBStoreWritesTheExplanationAttributeOnlyWhenPresent checks the
// stored item itself: no attribute for an absent Explanation, an empty list for
// an empty one (Requirements 2.4, 2.5, 23.4).
func TestDynamoDBStoreWritesTheExplanationAttributeOnlyWhenPresent(t *testing.T) {
	env := contractNewDynamoEnv()
	ctx := context.Background()

	for _, tc := range []struct {
		id        string
		expl      []Contribution
		wantItems int // -1 means the attribute must be absent
	}{
		{"explanation-item-absent", nil, -1},
		{"explanation-item-empty", []Contribution{}, 0},
		{"explanation-item-filled", explanationSample(), 3},
	} {
		if err := env.vs.Create(ctx, explanationRecord(tc.id, tc.expl)); err != nil {
			t.Fatalf("%s: Create returned %v", tc.id, err)
		}
		item := env.fake.rawItem(tc.id)
		attr, present := item[explanationAttr]
		if tc.wantItems < 0 {
			if present {
				t.Errorf("%s: item has an %q attribute, want none", tc.id, explanationAttr)
			}
			continue
		}
		list, ok := attr.(*types.AttributeValueMemberL)
		if !ok || len(list.Value) != tc.wantItems {
			t.Errorf("%s: %q = %#v, want a list of %d items", tc.id, explanationAttr, attr, tc.wantItems)
		}
	}
}
