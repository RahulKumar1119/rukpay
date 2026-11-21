package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Example and edge-case tests for the store adapters and the factory. The
// shared examples run against both adapters through contractRun. Expected
// error texts and expression strings are written out literally so that a change
// to them fails here (Requirements 10.1, 10.2, 12.6).

// examplesRecord returns a valid record shaped like the high-risk demo transfer.
func examplesRecord(id string, state VaultState) VaultRecord {
	epoch := contractEpoch()
	return VaultRecord{
		ID:                 id,
		State:              state,
		RiskScore:          90,
		PayerAccountMasked: "XXXX-XXXX-4821",
		BeneficiaryMasked:  "XXXX-XXXX-1100",
		UPIMasked:          "r***@okaxis",
		PhoneMasked:        "+91-XXXXXX3210",
		AmountINR:          85000,
		CreatedAt:          epoch,
		LockExpiresAt:      epoch.Add(15 * time.Minute),
		Version:            1,
		ExpiresAt:          epoch.Add(24 * time.Hour),
	}
}

// examplesWantError fails the test unless err wraps want and prints as text.
func examplesWantError(t *testing.T, err, want error, text string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want it to wrap %v", err, want)
	}
	if err.Error() != text {
		t.Fatalf("error text = %q, want %q", err.Error(), text)
	}
}

func TestStoreExamplesDuplicateCreate(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		first := examplesRecord("vault-a", StateLocked15M)
		second := examplesRecord("vault-a", StateVetoed)
		second.AmountINR = 1

		if err := env.vs.Create(ctx, first); err != nil {
			t.Fatalf("first Create = %v, want nil", err)
		}
		err := env.vs.Create(ctx, second)
		examplesWantError(t, err, ErrAlreadyExists, "create vault-a: vault already exists")

		got, err := env.vs.Get(ctx, "vault-a")
		if err != nil || !contractEqual(got, first) {
			t.Fatalf("Get = %+v, %v, want the first record", got, err)
		}
	})
}

func TestStoreExamplesMissingID(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()

		_, err := env.vs.Get(ctx, "ghost")
		examplesWantError(t, err, ErrNotFound, "get ghost: vault not found")

		err = env.vs.UpdateIf(ctx, "ghost", StateLocked15M, examplesRecord("ghost", StateVetoed))
		examplesWantError(t, err, ErrNotFound, "update ghost: vault not found")

		// The failed update did not create the record.
		if _, err := env.vs.Get(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get after failed UpdateIf = %v, want ErrNotFound", err)
		}
	})
}

func TestStoreExamplesExpiredGet(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		rec := examplesRecord("vault-a", StateLocked15M)
		if err := env.vs.Create(ctx, rec); err != nil {
			t.Fatalf("Create = %v, want nil", err)
		}

		// Still there at the instant of expiry.
		env.clock.Set(rec.ExpiresAt)
		if _, err := env.vs.Get(ctx, "vault-a"); err != nil {
			t.Fatalf("Get at expires_at = %v, want nil", err)
		}

		// Gone one second later, although nothing deleted the record.
		env.clock.Advance(time.Second)
		_, err := env.vs.Get(ctx, "vault-a")
		examplesWantError(t, err, ErrNotFound, "get vault-a: vault not found")
	})
}

func TestStoreExamplesUpdateIf(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		locked := examplesRecord("vault-a", StateLocked15M)
		if err := env.vs.Create(ctx, locked); err != nil {
			t.Fatalf("Create = %v, want nil", err)
		}

		// A mismatch leaves the record alone.
		approved := examplesRecord("vault-a", StateGuardianApproved)
		approved.Version = 2
		err := env.vs.UpdateIf(ctx, "vault-a", StateVetoed, approved)
		examplesWantError(t, err, ErrConditionFailed, "update vault-a: vault state condition failed")
		got, err := env.vs.Get(ctx, "vault-a")
		if err != nil || !contractEqual(got, locked) {
			t.Fatalf("Get after failed UpdateIf = %+v, %v, want the original", got, err)
		}

		// A match replaces it, and Version is stored as given.
		if err := env.vs.UpdateIf(ctx, "vault-a", StateLocked15M, approved); err != nil {
			t.Fatalf("UpdateIf = %v, want nil", err)
		}
		got, err = env.vs.Get(ctx, "vault-a")
		if err != nil || !contractEqual(got, approved) {
			t.Fatalf("Get after UpdateIf = %+v, %v, want %+v", got, err, approved)
		}

		// Any state pair is accepted, even one that is not a legal transition.
		back := examplesRecord("vault-a", StateInstantPass)
		if err := env.vs.UpdateIf(ctx, "vault-a", StateGuardianApproved, back); err != nil {
			t.Fatalf("UpdateIf to an illegal transition = %v, want nil", err)
		}
	})
}

func TestStoreExamplesCancelledContext(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		rec := examplesRecord("vault-a", StateLocked15M)
		live := context.Background()
		if err := env.vs.Create(live, examplesRecord("vault-b", StateLocked15M)); err != nil {
			t.Fatalf("Create = %v, want nil", err)
		}
		putsBefore, getsBefore := 0, 0
		if env.fake != nil {
			putsBefore, getsBefore = env.fake.putCalls, env.fake.getCalls
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := env.vs.Create(ctx, rec); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create with a cancelled context = %v, want context.Canceled", err)
		}
		if _, err := env.vs.Get(ctx, "vault-b"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Get with a cancelled context = %v, want context.Canceled", err)
		}
		err := env.vs.UpdateIf(ctx, "vault-b", StateLocked15M, examplesRecord("vault-b", StateVetoed))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("UpdateIf with a cancelled context = %v, want context.Canceled", err)
		}

		// Nothing happened: the cancelled Create stored nothing, the cancelled
		// UpdateIf changed nothing, and the fake client saw no call.
		if _, err := env.vs.Get(live, "vault-a"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get after cancelled Create = %v, want ErrNotFound", err)
		}
		got, err := env.vs.Get(live, "vault-b")
		if err != nil || got.State != StateLocked15M {
			t.Fatalf("Get after cancelled UpdateIf = %+v, %v, want state LOCKED_15M", got, err)
		}
		if env.fake != nil {
			// The two Gets above each made one client call.
			if env.fake.putCalls != putsBefore || env.fake.getCalls != getsBefore+2 {
				t.Fatalf("client calls after cancelled methods: puts %d (want %d), gets %d (want %d)",
					env.fake.putCalls, putsBefore, env.fake.getCalls, getsBefore+2)
			}
		}
	})
}

func TestDynamoDBStoreConditionExpressions(t *testing.T) {
	env := contractNewDynamoEnv()
	ctx := context.Background()
	rec := examplesRecord("vault-a", StateLocked15M)

	// Create sends a put guarded by attribute_not_exists(PK).
	if err := env.vs.Create(ctx, rec); err != nil {
		t.Fatalf("Create = %v, want nil", err)
	}
	put := env.fake.lastPut
	if put.ConditionExpression == nil || *put.ConditionExpression != "attribute_not_exists(PK)" {
		t.Fatalf("Create ConditionExpression = %v, want %q", put.ConditionExpression, "attribute_not_exists(PK)")
	}
	if len(put.ExpressionAttributeNames) != 0 || len(put.ExpressionAttributeValues) != 0 {
		t.Fatalf("Create sent placeholders: %v %v", put.ExpressionAttributeNames, put.ExpressionAttributeValues)
	}
	if put.TableName == nil || *put.TableName != contractTable {
		t.Fatalf("Create TableName = %v, want %q", put.TableName, contractTable)
	}

	// UpdateIf sends a put guarded by existence and the expected state.
	next := examplesRecord("vault-a", StateGuardianApproved)
	if err := env.vs.UpdateIf(ctx, "vault-a", StateLocked15M, next); err != nil {
		t.Fatalf("UpdateIf = %v, want nil", err)
	}
	put = env.fake.lastPut
	const wantCond = "attribute_exists(PK) AND #s = :expected"
	if put.ConditionExpression == nil || *put.ConditionExpression != wantCond {
		t.Fatalf("UpdateIf ConditionExpression = %v, want %q", put.ConditionExpression, wantCond)
	}
	if want := map[string]string{"#s": "state"}; !reflect.DeepEqual(put.ExpressionAttributeNames, want) {
		t.Fatalf("UpdateIf ExpressionAttributeNames = %v, want %v", put.ExpressionAttributeNames, want)
	}
	want := map[string]types.AttributeValue{":expected": &types.AttributeValueMemberS{Value: "LOCKED_15M"}}
	if !reflect.DeepEqual(put.ExpressionAttributeValues, want) {
		t.Fatalf("UpdateIf ExpressionAttributeValues = %v, want %v", put.ExpressionAttributeValues, want)
	}

	// Get reads the item key with a consistent read.
	if _, err := env.vs.Get(ctx, "vault-a"); err != nil {
		t.Fatalf("Get = %v, want nil", err)
	}
	get := env.fake.lastGet
	if get.ConsistentRead == nil || !*get.ConsistentRead {
		t.Fatalf("Get ConsistentRead = %v, want true", get.ConsistentRead)
	}
	wantKey := map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: "VAULT#vault-a"},
		"SK": &types.AttributeValueMemberS{Value: "META"},
	}
	if !reflect.DeepEqual(get.Key, wantKey) {
		t.Fatalf("Get Key = %v, want %v", get.Key, wantKey)
	}
}

// A conditional failure on UpdateIf is followed by one read. An empty read means
// the record is missing (Requirement 10.4); a record in another state means the
// condition failed.
func TestDynamoDBStoreUpdateIfFollowUpRead(t *testing.T) {
	ctx := context.Background()

	t.Run("missing record", func(t *testing.T) {
		env := contractNewDynamoEnv()
		err := env.vs.UpdateIf(ctx, "ghost", StateLocked15M, examplesRecord("ghost", StateVetoed))
		examplesWantError(t, err, ErrNotFound, "update ghost: vault not found")
		if env.fake.putCalls != 1 || env.fake.getCalls != 1 {
			t.Fatalf("calls: puts %d, gets %d, want 1 and 1", env.fake.putCalls, env.fake.getCalls)
		}
	})

	t.Run("other state", func(t *testing.T) {
		env := contractNewDynamoEnv()
		if err := env.vs.Create(ctx, examplesRecord("vault-a", StateLocked15M)); err != nil {
			t.Fatalf("Create = %v, want nil", err)
		}
		getsBefore := env.fake.getCalls
		err := env.vs.UpdateIf(ctx, "vault-a", StateVetoed, examplesRecord("vault-a", StateGuardianApproved))
		examplesWantError(t, err, ErrConditionFailed, "update vault-a: vault state condition failed")
		if got := env.fake.getCalls - getsBefore; got != 1 {
			t.Fatalf("follow-up reads = %d, want 1", got)
		}
	})

	t.Run("expired record", func(t *testing.T) {
		env := contractNewDynamoEnv()
		rec := examplesRecord("vault-a", StateLocked15M)
		if err := env.vs.Create(ctx, rec); err != nil {
			t.Fatalf("Create = %v, want nil", err)
		}
		env.clock.Set(rec.ExpiresAt.Add(time.Second))
		err := env.vs.UpdateIf(ctx, "vault-a", StateVetoed, examplesRecord("vault-a", StateGuardianApproved))
		examplesWantError(t, err, ErrNotFound, "update vault-a: vault not found")
	})
}

// Client failures other than a failed condition keep the original error
// reachable and print only the operation, the vault id and a fixed message
// (Requirement 10.5).
func TestDynamoDBStoreClientErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom: amount 85000 payer XXXX-XXXX-4821")
	rec := examplesRecord("vault-a", StateLocked15M)

	t.Run("create", func(t *testing.T) {
		env := contractNewDynamoEnv()
		env.fake.failNext(fakeDynamoDBOpPutItem, boom)
		examplesWantError(t, env.vs.Create(ctx, rec), boom, "create vault-a: dynamodb request failed")
	})

	t.Run("get", func(t *testing.T) {
		env := contractNewDynamoEnv()
		env.fake.failNext(fakeDynamoDBOpGetItem, boom)
		_, err := env.vs.Get(ctx, "vault-a")
		examplesWantError(t, err, boom, "get vault-a: dynamodb request failed")
	})

	t.Run("update put", func(t *testing.T) {
		env := contractNewDynamoEnv()
		env.fake.failNext(fakeDynamoDBOpPutItem, boom)
		err := env.vs.UpdateIf(ctx, "vault-a", StateLocked15M, rec)
		examplesWantError(t, err, boom, "update vault-a: dynamodb request failed")
	})

	t.Run("update follow-up read", func(t *testing.T) {
		env := contractNewDynamoEnv()
		env.fake.failNextConditional(fakeDynamoDBOpPutItem)
		env.fake.failNext(fakeDynamoDBOpGetItem, boom)
		err := env.vs.UpdateIf(ctx, "vault-a", StateLocked15M, rec)
		examplesWantError(t, err, boom, "update vault-a: dynamodb request failed")
	})

	t.Run("decode", func(t *testing.T) {
		env := contractNewDynamoEnv()
		// An item whose risk_score is a string cannot be decoded.
		item := map[string]types.AttributeValue{
			"PK":         &types.AttributeValueMemberS{Value: "VAULT#vault-a"},
			"SK":         &types.AttributeValueMemberS{Value: "META"},
			"risk_score": &types.AttributeValueMemberS{Value: "not a number"},
		}
		if _, err := env.fake.PutItem(ctx, &dynamodb.PutItemInput{Item: item}); err != nil {
			t.Fatalf("seed PutItem = %v, want nil", err)
		}
		_, err := env.vs.Get(ctx, "vault-a")
		if err == nil || err.Error() != "get vault-a: dynamodb item decode failed" {
			t.Fatalf("Get of an undecodable item = %v, want %q", err, "get vault-a: dynamodb item decode failed")
		}
	})
}

// awsTestEnv points the AWS SDK at a fixed region and dummy credentials and at
// config files that do not exist, so NewFromEnv builds a client without reading
// the developer's AWS setup and without any network call.
func awsTestEnv(t *testing.T) {
	t.Helper()
	absent := filepath.Join(t.TempDir(), "absent")
	t.Setenv("AWS_REGION", "ap-south-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", absent)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", absent)
}

func TestNewFromEnvSelectsTheAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("memory", func(t *testing.T) {
		t.Setenv("STORAGE_DRIVER", "memory")
		got, err := NewFromEnv(ctx)
		if err != nil {
			t.Fatalf("NewFromEnv = %v, want nil", err)
		}
		if _, ok := got.(*MemoryStore); !ok {
			t.Fatalf("NewFromEnv returned %T, want *MemoryStore", got)
		}
	})

	t.Run("dynamodb with the default table", func(t *testing.T) {
		awsTestEnv(t)
		t.Setenv("STORAGE_DRIVER", "dynamodb")
		t.Setenv("DYNAMODB_TABLE_NAME", "")
		t.Setenv("DYNAMODB_ENDPOINT", "")
		got, err := NewFromEnv(ctx)
		if err != nil {
			t.Fatalf("NewFromEnv = %v, want nil", err)
		}
		s, ok := got.(*DynamoDBStore)
		if !ok {
			t.Fatalf("NewFromEnv returned %T, want *DynamoDBStore", got)
		}
		if s.table != "RukPayVaults" {
			t.Fatalf("table = %q, want %q", s.table, "RukPayVaults")
		}
		client, ok := s.client.(*dynamodb.Client)
		if !ok {
			t.Fatalf("client is %T, want *dynamodb.Client", s.client)
		}
		if endpoint := client.Options().BaseEndpoint; endpoint != nil {
			t.Fatalf("BaseEndpoint = %q, want nil without DYNAMODB_ENDPOINT", *endpoint)
		}
	})

	t.Run("dynamodb with a table and an endpoint", func(t *testing.T) {
		awsTestEnv(t)
		t.Setenv("STORAGE_DRIVER", "dynamodb")
		t.Setenv("DYNAMODB_TABLE_NAME", "OtherVaults")
		t.Setenv("DYNAMODB_ENDPOINT", "http://localhost:8000")
		got, err := NewFromEnv(ctx)
		if err != nil {
			t.Fatalf("NewFromEnv = %v, want nil", err)
		}
		s, ok := got.(*DynamoDBStore)
		if !ok {
			t.Fatalf("NewFromEnv returned %T, want *DynamoDBStore", got)
		}
		if s.table != "OtherVaults" {
			t.Fatalf("table = %q, want %q", s.table, "OtherVaults")
		}
		client, ok := s.client.(*dynamodb.Client)
		if !ok {
			t.Fatalf("client is %T, want *dynamodb.Client", s.client)
		}
		endpoint := client.Options().BaseEndpoint
		if endpoint == nil || *endpoint != "http://localhost:8000" {
			t.Fatalf("BaseEndpoint = %v, want %q", endpoint, "http://localhost:8000")
		}
	})
}

func TestNewFromEnvRejectsAnUnknownDriver(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		driver string
	}{
		{name: "unknown", driver: "sqlite-secret-value"},
		{name: "wrong case", driver: "Memory"},
		{name: "empty", driver: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("STORAGE_DRIVER", tt.driver)
			got, err := NewFromEnv(ctx)
			if err == nil {
				t.Fatalf("NewFromEnv = %T, nil, want an error", got)
			}
			if got != nil {
				t.Fatalf("NewFromEnv returned %T with an error, want a nil store", got)
			}
			if !strings.Contains(err.Error(), "STORAGE_DRIVER") {
				t.Fatalf("error %q does not name STORAGE_DRIVER", err)
			}
			if tt.driver != "" && strings.Contains(err.Error(), tt.driver) {
				t.Fatalf("error %q echoes the supplied value %q", err, tt.driver)
			}
		})
	}
}
