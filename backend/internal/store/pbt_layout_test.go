package store

import (
	"context"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"pgregory.net/rapid"
)

// layoutCheckItem fails the test unless the item stored for id has PK
// "VAULT#<id>", SK "META" and expires_at as a Number holding expiresAt in Unix
// epoch seconds.
func layoutCheckItem(rt *rapid.T, env *contractEnv, id string, wantExpiresAtUnix int64) {
	rt.Helper()

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
	exp, ok := item["expires_at"].(*types.AttributeValueMemberN)
	if !ok {
		rt.Fatalf("expires_at = %#v, want a Number", item["expires_at"])
	}
	if want := strconv.FormatInt(wantExpiresAtUnix, 10); exp.Value != want {
		rt.Fatalf("expires_at = %q, want %q", exp.Value, want)
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
		layoutCheckItem(rt, env, rec.ID, rec.ExpiresAt.Unix())

		// The layout holds for an item replaced by UpdateIf as well.
		if err := env.vs.UpdateIf(context.Background(), rec.ID, rec.State, next); err != nil {
			rt.Fatalf("UpdateIf = %v, want nil", err)
		}
		layoutCheckItem(rt, env, rec.ID, next.ExpiresAt.Unix())

		if env.fake.itemCount() != 1 {
			rt.Fatalf("itemCount = %d, want 1 item per vault", env.fake.itemCount())
		}
		if got := *env.fake.lastPut.TableName; got != contractTable {
			rt.Fatalf("PutItem table = %q, want %q", got, contractTable)
		}
	})
}
