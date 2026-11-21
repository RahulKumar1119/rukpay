package store

import (
	"context"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"pgregory.net/rapid"
)

// layoutCheckItem fails the test unless the item stored for want.ID has PK
// "VAULT#<id>", SK "META", expires_at as a Number holding the expiry in Unix
// epoch seconds, and the vault fields in their documented attribute types:
// lock_deadline and token_expiry as Numbers, settled as a Bool, reason as a
// String and evidence as a String that is absent when the summary is empty.
func layoutCheckItem(rt *rapid.T, env *contractEnv, want VaultRecord) {
	rt.Helper()
	id := want.ID

	item := env.fake.rawItem(id)
	if item == nil {
		rt.Fatalf("no item stored for id %q", id)
	}

	pk, ok := item["PK"].(*types.AttributeValueMemberS)
	if !ok || pk.Value != "VAULT#"+id {
		rt.Fatalf("PK = %#v, want S %q", item["PK"], "VAULT#"+id)
	}
	sk, ok := item["SK"].(*types.AttributeValueMemberS)
	if !ok || sk.Value != "META" {
		rt.Fatalf("SK = %#v, want S %q", item["SK"], "META")
	}

	numbers := map[string]int64{
		"expires_at":    want.ExpiresAt.Unix(),
		"lock_deadline": want.LockDeadline,
		"token_expiry":  want.TokenExpiry,
	}
	for name, wantValue := range numbers {
		n, ok := item[name].(*types.AttributeValueMemberN)
		if !ok {
			rt.Fatalf("%s = %#v, want a Number", name, item[name])
		}
		if wantText := strconv.FormatInt(wantValue, 10); n.Value != wantText {
			rt.Fatalf("%s = %q, want %q", name, n.Value, wantText)
		}
	}

	settled, ok := item["settled"].(*types.AttributeValueMemberBOOL)
	if !ok || settled.Value != want.Settled {
		rt.Fatalf("settled = %#v, want BOOL %v", item["settled"], want.Settled)
	}
	reason, ok := item["reason"].(*types.AttributeValueMemberS)
	if !ok || reason.Value != want.Reason {
		rt.Fatalf("reason = %#v, want S %q", item["reason"], want.Reason)
	}

	evidence, present := item["evidence"]
	switch {
	case want.EvidenceSummary == "" && present:
		rt.Fatalf("evidence = %#v, want the attribute absent for an empty summary", evidence)
	case want.EvidenceSummary != "":
		s, ok := evidence.(*types.AttributeValueMemberS)
		if !ok || s.Value != want.EvidenceSummary {
			rt.Fatalf("evidence = %#v, want S %q", evidence, want.EvidenceSummary)
		}
	}
}

// Feature: behavioral-risk-engine, Property 13: DynamoDB item layout
//
// DynamoDBStore only: the check reads the item that the fake client holds.
//
// **Validates: Requirements 9.2, 9.3**
func TestDynamoDBItemLayout(t *testing.T) {
	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		rec := contractRecordGen().Draw(rt, "record")
		next := contractRecordGen().Draw(rt, "next")
		next.ID = rec.ID

		contractMustCreate(rt, env.vs, rec)
		layoutCheckItem(rt, env, rec)

		// The layout holds for an item replaced by UpdateIf as well.
		if err := env.vs.UpdateIf(context.Background(), rec.ID, rec.State, next); err != nil {
			rt.Fatalf("UpdateIf = %v, want nil", err)
		}
		layoutCheckItem(rt, env, next)

		if env.fake.itemCount() != 1 {
			rt.Fatalf("itemCount = %d, want 1 item per vault", env.fake.itemCount())
		}
		if got := *env.fake.lastPut.TableName; got != contractTable {
			rt.Fatalf("PutItem table = %q, want %q", got, contractTable)
		}
	})
}
