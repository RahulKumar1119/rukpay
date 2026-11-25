package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Example tests for the beneficiary stores. Each shared example runs against both
// adapters through contractRun. The randomized checks are in
// pbt_beneficiary_test.go and pbt_beneficiary_race_test.go.

// deleteFaultClient wraps a DynamoDBClient and makes DeleteItem return err
// instead of running, while err is not nil. The fake offers no failure injection
// for DeleteItem on purpose (design note I-11), so delete failures come from this
// wrapper. Every other method goes straight to the wrapped client.
type deleteFaultClient struct {
	DynamoDBClient
	err   error
	calls int
}

func (c *deleteFaultClient) DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.DynamoDBClient.DeleteItem(ctx, in, opts...)
}

// beneExample returns a valid record with every optional part set: a verified
// time, a first payment and a history of two entries. Its label, identifier and
// amount are the values that an error text must never hold. Its ExpiresAt is a
// value that the store must replace.
func beneExample(id string) BeneficiaryRecord {
	epoch := contractEpoch().Unix()
	verified, first := epoch-1800, int64(18_000)
	return BeneficiaryRecord{
		ID:               id,
		Label:            "Sharma Stores",
		IdentifierMasked: "XXXX-XXXX-4821",
		AddedAt:          epoch - 3600,
		VerifiedAt:       &verified,
		Trust:            TrustNew,
		Protection:       ProtectionExtraCare,
		PressureFlag:     true,
		Version:          1,
		PaymentCount:     1,
		FirstPaymentINR:  &first,
		History: []HistoryEntry{
			{At: epoch - 3600, Kind: "ADDED"},
			{At: epoch - 600, Kind: "PAYMENT", AmountINR: 18_000},
		},
		ExpiresAt: 7,
	}
}

// beneExampleSecrets are the values of beneExample that no error text may hold.
var beneExampleSecrets = []string{"Sharma Stores", "XXXX-XXXX-4821", "18000"}

// beneNow returns the clock of env in epoch seconds.
func beneNow(env *contractEnv) int64 { return env.clock.Now().Unix() }

// beneCallCounts are the client calls the fake has seen. They stay zero for
// MemoryStore.
type beneCallCounts struct{ puts, gets, queries, deletes int }

func beneCalls(env *contractEnv) beneCallCounts {
	if env.fake == nil {
		return beneCallCounts{}
	}
	return beneCallCounts{env.fake.putCalls, env.fake.getCalls, env.fake.queryCalls, env.fake.deleteCalls}
}

// beneStoredExample is beneExample as the store must hold it after a write at the
// clock of env.
func beneStoredExample(env *contractEnv, id string) BeneficiaryRecord {
	return beneStored(beneExample(id), beneNow(env))
}

func TestBeneficiaryExamplesCreateGetAndListInIDOrder(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(t, env)

		// Written out of order, under two payers.
		for _, id := range []string{"c", "a", "ab", "b"} {
			beneMustCreate(t, bs, PayerKey, beneExample(id))
		}
		beneMustCreate(t, bs, "other-payer", beneExample("a"))

		want := beneStoredExample(env, "a")
		beneWantGet(t, bs, PayerKey, "a", want)
		if want.ExpiresAt != contractEpoch().Unix()+2_592_000 {
			t.Fatalf("expected ExpiresAt = %d, want the write time plus 30 days", want.ExpiresAt)
		}

		var wantList []BeneficiaryRecord
		for _, id := range []string{"a", "ab", "b", "c"} {
			wantList = append(wantList, beneStoredExample(env, id))
		}
		beneWantList(t, bs, PayerKey, wantList)
		beneWantList(t, bs, "other-payer", []BeneficiaryRecord{want})

		empty, err := bs.ListBeneficiaries(ctx, "nobody")
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatalf("ListBeneficiaries of a payer with none = %#v, %v, want an empty non-nil list", empty, err)
		}
	})
}

func TestBeneficiaryExamplesRoundTripOptionalPartsAndHistory(t *testing.T) {
	zero := int64(0)
	fifty := make([]HistoryEntry, 50)
	for i := range fifty {
		fifty[i] = HistoryEntry{At: int64(i), Kind: "PAYMENT", AmountINR: math.MaxInt64 - int64(i)}
	}
	tests := []struct {
		name   string
		mutate func(rec *BeneficiaryRecord)
	}{
		{"everything set", func(*BeneficiaryRecord) {}},
		{"no verified time and no first payment", func(r *BeneficiaryRecord) { r.VerifiedAt, r.FirstPaymentINR = nil, nil }},
		{"a verified time and a first payment of 0 are not absent", func(r *BeneficiaryRecord) { r.VerifiedAt, r.FirstPaymentINR = &zero, &zero }},
		{"a nil history", func(r *BeneficiaryRecord) { r.History = nil }},
		{"an empty history", func(r *BeneficiaryRecord) { r.History = []HistoryEntry{} }},
		{"fifty entries with the largest amount", func(r *BeneficiaryRecord) { r.History = fifty }},
		{"the smallest and largest times", func(r *BeneficiaryRecord) {
			r.AddedAt = math.MinInt64
			r.History = []HistoryEntry{{At: math.MinInt64, Kind: "ADDED"}, {At: math.MaxInt64, Kind: "PAYMENT", AmountINR: math.MaxInt64}}
		}},
		{"the other trust and protection levels", func(r *BeneficiaryRecord) {
			r.Trust, r.Protection, r.PressureFlag = TrustTrusted, ProtectionStandard, false
		}},
		{"an empty label", func(r *BeneficiaryRecord) { r.Label = "" }},
		{"a version of 0 and a payment count of 0", func(r *BeneficiaryRecord) { r.Version, r.PaymentCount = 0, 0 }},
		{"every identifier format", func(r *BeneficiaryRecord) { r.IdentifierMasked = "r***@okaxis" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contractRun(t, func(t *testing.T, env *contractEnv) {
				rec := beneExample("a")
				tt.mutate(&rec)
				bs := beneficiaryOf(t, env)
				beneMustCreate(t, bs, PayerKey, rec)

				want := beneStored(rec, beneNow(env))
				beneWantGet(t, bs, PayerKey, "a", want)
				beneWantList(t, bs, PayerKey, []BeneficiaryRecord{want})
			})
		})
	}
}

func TestBeneficiaryExamplesDuplicateCreateEvenWhenExpired(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(t, env)
		first := beneExample("a")
		beneMustCreate(t, bs, PayerKey, first)
		want := beneStoredExample(env, "a")

		second := beneExample("a")
		second.Label, second.Version = "Someone Else", 9
		err := bs.CreateBeneficiary(ctx, PayerKey, second)
		if !errors.Is(err, ErrAlreadyExists) || err.Error() != "create_beneficiary a: vault already exists" {
			t.Fatalf("duplicate create = %v, want ErrAlreadyExists with the fixed text", err)
		}
		beneWantGet(t, bs, PayerKey, "a", want)

		// Another payer may use the same id.
		beneMustCreate(t, bs, "other-payer", second)

		// Past the expiry the record is hidden, and its id is still taken.
		env.clock.Set(time.Unix(want.ExpiresAt+1, 0).UTC())
		if _, err := bs.GetBeneficiary(ctx, PayerKey, "a"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get of the expired record = %v, want ErrNotFound", err)
		}
		err = bs.CreateBeneficiary(ctx, PayerKey, second)
		if !errors.Is(err, ErrAlreadyExists) || err.Error() != "create_beneficiary a: vault already exists" {
			t.Fatalf("create over an expired record = %v, want ErrAlreadyExists with the fixed text", err)
		}
		if env.fake != nil {
			stored := env.fake.rawItemAt("BENE#"+PayerKey, "B#a")
			if m := fakeDynamoDBAttrS(stored, "label"); m != "Sharma Stores" {
				t.Fatalf("the expired item's label = %q after a refused create, want it unchanged", m)
			}
		}
	})
}

func TestBeneficiaryExamplesConditionalReplace(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(t, env)
		beneMustCreate(t, bs, PayerKey, beneExample("a"))
		created := beneStoredExample(env, "a")

		next := beneExample("a")
		next.Label, next.Version, next.PaymentCount = "Renamed Stores", 99, 2
		next.History = append(next.History, HistoryEntry{At: 5, Kind: "RENAMED"})

		// A wrong version, one below and one above, changes nothing.
		for _, version := range []int{0, 2, math.MinInt, 99} {
			err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", version, next)
			if !errors.Is(err, ErrConditionFailed) || err.Error() != "replace_beneficiary a: vault state condition failed" {
				t.Fatalf("replace at version %d = %v, want ErrConditionFailed with the fixed text", version, err)
			}
			beneWantGet(t, bs, PayerKey, "a", created)
		}

		// The right version wins, stores version + 1 whatever next carries, and a
		// new expiry from the clock of the write.
		env.clock.Advance(90 * time.Minute)
		if err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", 1, next); err != nil {
			t.Fatalf("replace at the stored version = %v, want nil", err)
		}
		want := beneStored(next, beneNow(env))
		want.Version = 2
		beneWantGet(t, bs, PayerKey, "a", want)
		if want.ExpiresAt != created.ExpiresAt+90*60 {
			t.Fatalf("expiry after a replace = %d, want 30 days after the replace (%d)", want.ExpiresAt, created.ExpiresAt+90*60)
		}

		// The version read before is now stale: a second writer loses.
		err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", 1, beneExample("a"))
		if !errors.Is(err, ErrConditionFailed) {
			t.Fatalf("replace at the stale version = %v, want ErrConditionFailed", err)
		}
		beneWantGet(t, bs, PayerKey, "a", want)

		// Replace keeps working from the new version, again and again.
		for version := 2; version < 6; version++ {
			if err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", version, next); err != nil {
				t.Fatalf("replace at version %d = %v, want nil", version, err)
			}
		}
		if got, _ := bs.GetBeneficiary(ctx, PayerKey, "a"); got.Version != 6 {
			t.Fatalf("version after four more replaces = %d, want 6", got.Version)
		}

		// A missing record, and one of another payer.
		for _, tc := range []struct{ payer, id string }{{PayerKey, "zz"}, {"other-payer", "a"}} {
			err := bs.ReplaceBeneficiary(ctx, tc.payer, tc.id, 1, next)
			if !errors.Is(err, ErrNotFound) || err.Error() != "replace_beneficiary "+tc.id+": vault not found" {
				t.Fatalf("replace of payer %q id %q = %v, want ErrNotFound with the fixed text", tc.payer, tc.id, err)
			}
		}
	})
}

func TestBeneficiaryExamplesDelete(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(t, env)
		beneMustCreate(t, bs, PayerKey, beneExample("a"))
		beneMustCreate(t, bs, PayerKey, beneExample("b"))
		beneMustCreate(t, bs, "other-payer", beneExample("a"))
		created := beneStoredExample(env, "a")

		// A wrong version keeps the record.
		for _, version := range []int{0, 2} {
			err := bs.DeleteBeneficiary(ctx, PayerKey, "a", version)
			if !errors.Is(err, ErrConditionFailed) || err.Error() != "delete_beneficiary a: vault state condition failed" {
				t.Fatalf("delete at version %d = %v, want ErrConditionFailed with the fixed text", version, err)
			}
			beneWantGet(t, bs, PayerKey, "a", created)
		}

		// The right version removes the record, with its history, and only it.
		if err := bs.DeleteBeneficiary(ctx, PayerKey, "a", 1); err != nil {
			t.Fatalf("delete at the stored version = %v, want nil", err)
		}
		if _, err := bs.GetBeneficiary(ctx, PayerKey, "a"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get after a delete = %v, want ErrNotFound", err)
		}
		beneWantList(t, bs, PayerKey, []BeneficiaryRecord{beneStoredExample(env, "b")})
		beneWantGet(t, bs, "other-payer", "a", created)
		if env.fake != nil && env.fake.itemCount() != 2 {
			t.Fatalf("itemCount = %d after one delete of three records, want 2", env.fake.itemCount())
		}

		// A second delete finds nothing, and the id can be used again.
		err := bs.DeleteBeneficiary(ctx, PayerKey, "a", 1)
		if !errors.Is(err, ErrNotFound) || err.Error() != "delete_beneficiary a: vault not found" {
			t.Fatalf("second delete = %v, want ErrNotFound with the fixed text", err)
		}
		beneMustCreate(t, bs, PayerKey, beneExample("a"))
	})
}

// A record is live up to and including its ExpiresAt second and expired from the
// next one. Only a successful write moves the expiry.
func TestBeneficiaryExamplesExpiryWithAControlledClock(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(t, env)
		start := beneNow(env)
		expiry := start + 2_592_000

		// The caller's own ExpiresAt, of any size, is ignored.
		for i, sent := range []int64{math.MinInt64, -1, 0, 1, expiry + 5, math.MaxInt64} {
			rec := beneExample(fmt.Sprintf("r%d", i))
			rec.ExpiresAt = sent
			beneMustCreate(t, bs, PayerKey, rec)
			got, err := bs.GetBeneficiary(ctx, PayerKey, rec.ID)
			if err != nil || got.ExpiresAt != expiry {
				t.Fatalf("a create that sent ExpiresAt %d stored %d (err %v), want %d", sent, got.ExpiresAt, err, expiry)
			}
		}

		beneMustCreate(t, bs, PayerKey, beneExample("a"))

		// Reads and refused writes do not move the expiry.
		env.clock.Set(time.Unix(expiry-1, 0).UTC())
		if _, err := bs.GetBeneficiary(ctx, PayerKey, "a"); err != nil {
			t.Fatalf("Get one second before the expiry = %v, want nil", err)
		}
		if err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", 7, beneExample("a")); !errors.Is(err, ErrConditionFailed) {
			t.Fatalf("replace at a wrong version = %v, want ErrConditionFailed", err)
		}
		if err := bs.CreateBeneficiary(ctx, PayerKey, beneExample("a")); !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("duplicate create = %v, want ErrAlreadyExists", err)
		}
		if got, err := bs.GetBeneficiary(ctx, PayerKey, "a"); err != nil || got.ExpiresAt != expiry {
			t.Fatalf("expiry after reads and refused writes = %d (err %v), want %d", got.ExpiresAt, err, expiry)
		}

		// At the expiry second the record is still live and still can be replaced.
		env.clock.Set(time.Unix(expiry, 0).UTC())
		if got, err := bs.GetBeneficiary(ctx, PayerKey, "a"); err != nil || got.ExpiresAt != expiry {
			t.Fatalf("Get at the expiry = %d, %v, want the record", got.ExpiresAt, err)
		}
		if list, err := bs.ListBeneficiaries(ctx, PayerKey); err != nil || len(list) != 7 {
			t.Fatalf("list at the expiry = %d records (err %v), want 7", len(list), err)
		}
		if err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", 1, beneExample("a")); err != nil {
			t.Fatalf("replace at the expiry second = %v, want nil", err)
		}
		if got, _ := bs.GetBeneficiary(ctx, PayerKey, "a"); got.ExpiresAt != expiry+2_592_000 {
			t.Fatalf("expiry after a replace at the old expiry = %d, want %d", got.ExpiresAt, expiry+2_592_000)
		}

		// One second later the six records that were not replaced are expired.
		env.clock.Set(time.Unix(expiry+1, 0).UTC())
		list, err := bs.ListBeneficiaries(ctx, PayerKey)
		if err != nil || len(list) != 1 || list[0].ID != "a" {
			t.Fatalf("list one second after the expiry = %#v, %v, want only the replaced record a", list, err)
		}
		for i := 0; i < 6; i++ {
			id := fmt.Sprintf("r%d", i)
			if _, err := bs.GetBeneficiary(ctx, PayerKey, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Get of expired %s = %v, want ErrNotFound", id, err)
			}
			if err := bs.ReplaceBeneficiary(ctx, PayerKey, id, 1, beneExample(id)); !errors.Is(err, ErrNotFound) {
				t.Fatalf("replace of expired %s = %v, want ErrNotFound", id, err)
			}
			if err := bs.DeleteBeneficiary(ctx, PayerKey, id, 1); !errors.Is(err, ErrNotFound) {
				t.Fatalf("delete of expired %s = %v, want ErrNotFound", id, err)
			}
			if err := bs.CreateBeneficiary(ctx, PayerKey, beneExample(id)); !errors.Is(err, ErrAlreadyExists) {
				t.Fatalf("create over expired %s = %v, want ErrAlreadyExists", id, err)
			}
		}

		// An expired item is hidden, not deleted, as DynamoDB keeps it until TTL.
		if env.fake != nil && len(env.fake.rawItemsUnder("BENE#"+PayerKey)) != 7 {
			t.Fatalf("the fake holds %d items, want 7: reads must not delete expired items", len(env.fake.rawItemsUnder("BENE#"+PayerKey)))
		}
	})
}

// An identifier that is empty or not masked is refused with ErrUnmaskedIdentifier
// before any other check, in every state the record id can be in, and nothing is
// written.
func TestBeneficiaryExamplesUnmaskedIdentifierBeforeEveryOtherCheck(t *testing.T) {
	unmasked := []string{"", "4821", "123456789012", "+919876543210", "rahul@okaxis", "XXXX-XXXX-482", "XXXX-XXXX-4821 ", " r***@okaxis", "r**@okaxis"}
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(t, env)
		beneMustCreate(t, bs, PayerKey, beneExample("live"))
		beneMustCreate(t, bs, PayerKey, beneExample("old"))
		old := beneNow(env)
		// "old" expires first because the later record is written after the clock moves.
		env.clock.Advance(2 * time.Second)
		beneMustCreate(t, bs, PayerKey, beneExample("later"))
		env.clock.Set(time.Unix(old+2_592_000+1, 0).UTC()) // "live" and "old" expired, "later" live
		wantLater := beneStored(beneExample("later"), old+2)
		before := beneCalls(env)
		items := 0
		if env.fake != nil {
			items = env.fake.itemCount()
		}

		for _, ident := range unmasked {
			rec := beneExample("x")
			rec.IdentifierMasked = ident
			// create: a free id, an expired id, a live id. replace: a missing id, an
			// expired id, a live id, each at the right and a wrong version.
			for _, id := range []string{"free", "old", "later"} {
				rec.ID = id
				err := bs.CreateBeneficiary(ctx, PayerKey, rec)
				if !errors.Is(err, ErrUnmaskedIdentifier) || err.Error() != "create_beneficiary "+id+": unmasked identifier" {
					t.Fatalf("create of %q with identifier %q = %v, want ErrUnmaskedIdentifier with the fixed text", id, ident, err)
				}
				for _, version := range []int{1, 2, 0} {
					err := bs.ReplaceBeneficiary(ctx, PayerKey, id, version, rec)
					if !errors.Is(err, ErrUnmaskedIdentifier) || err.Error() != "replace_beneficiary "+id+": unmasked identifier" {
						t.Fatalf("replace of %q at version %d with identifier %q = %v, want ErrUnmaskedIdentifier with the fixed text", id, version, ident, err)
					}
				}
			}
		}

		if got := beneCalls(env); got != before {
			t.Fatalf("client calls = %+v after refused writes, want %+v: no request at all", got, before)
		}
		if env.fake != nil && env.fake.itemCount() != items {
			t.Fatalf("itemCount = %d after refused writes, want %d", env.fake.itemCount(), items)
		}
		beneWantList(t, bs, PayerKey, []BeneficiaryRecord{wantLater})
	})
}

// One failure for each error kind, each with the exact text, no other kind, no
// label, identifier or amount in the text, and the stored record unchanged.
func TestBeneficiaryExamplesEachErrorKindAndTheRecordStaysUnchanged(t *testing.T) {
	kinds := []error{ErrAlreadyExists, ErrNotFound, ErrConditionFailed, ErrUnmaskedIdentifier}
	unmasked := beneExample("a")
	unmasked.IdentifierMasked = "4821"

	tests := []struct {
		name string
		want error
		text string
		call func(ctx context.Context, bs BeneficiaryStore) error
	}{
		{"create of a stored id", ErrAlreadyExists, "create_beneficiary a: vault already exists",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.CreateBeneficiary(ctx, PayerKey, beneExample("a"))
			}},
		{"create with an unmasked identifier", ErrUnmaskedIdentifier, "create_beneficiary a: unmasked identifier",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.CreateBeneficiary(ctx, PayerKey, unmasked)
			}},
		{"get of a missing id", ErrNotFound, "get_beneficiary zz: vault not found",
			func(ctx context.Context, bs BeneficiaryStore) error {
				_, err := bs.GetBeneficiary(ctx, PayerKey, "zz")
				return err
			}},
		{"get of another payer's id", ErrNotFound, "get_beneficiary a: vault not found",
			func(ctx context.Context, bs BeneficiaryStore) error {
				_, err := bs.GetBeneficiary(ctx, "other-payer", "a")
				return err
			}},
		{"replace of a missing id", ErrNotFound, "replace_beneficiary zz: vault not found",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.ReplaceBeneficiary(ctx, PayerKey, "zz", 1, beneExample("zz"))
			}},
		{"replace at a wrong version", ErrConditionFailed, "replace_beneficiary a: vault state condition failed",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.ReplaceBeneficiary(ctx, PayerKey, "a", 5, beneExample("a"))
			}},
		{"replace with an unmasked identifier", ErrUnmaskedIdentifier, "replace_beneficiary a: unmasked identifier",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.ReplaceBeneficiary(ctx, PayerKey, "a", 1, unmasked)
			}},
		{"delete of a missing id", ErrNotFound, "delete_beneficiary zz: vault not found",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.DeleteBeneficiary(ctx, PayerKey, "zz", 1)
			}},
		{"delete at a wrong version", ErrConditionFailed, "delete_beneficiary a: vault state condition failed",
			func(ctx context.Context, bs BeneficiaryStore) error {
				return bs.DeleteBeneficiary(ctx, PayerKey, "a", 5)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contractRun(t, func(t *testing.T, env *contractEnv) {
				bs := beneficiaryOf(t, env)
				beneMustCreate(t, bs, PayerKey, beneExample("a"))
				want := beneStoredExample(env, "a")

				err := tt.call(context.Background(), bs)

				for _, kind := range kinds {
					if got := errors.Is(err, kind); got != (kind == tt.want) {
						t.Fatalf("errors.Is(%v, %q) = %v, want %v", err, kind, got, kind == tt.want)
					}
				}
				if err.Error() != tt.text {
					t.Fatalf("error text = %q, want %q", err.Error(), tt.text)
				}
				for _, secret := range beneExampleSecrets {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error text %q holds %q", err.Error(), secret)
					}
				}
				beneWantGet(t, bs, PayerKey, "a", want)
				beneWantList(t, bs, PayerKey, []BeneficiaryRecord{want})
			})
		})
	}
}

func TestBeneficiaryExamplesCancelledContextSendsNoRequest(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		bs := beneficiaryOf(t, env)
		beneMustCreate(t, bs, PayerKey, beneExample("a"))
		want := beneStoredExample(env, "a")
		before := beneCalls(env)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := bs.CreateBeneficiary(ctx, PayerKey, beneExample("b")); !errors.Is(err, context.Canceled) {
			t.Fatalf("create on a cancelled context = %v, want context.Canceled", err)
		}
		if got, err := bs.GetBeneficiary(ctx, PayerKey, "a"); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, BeneficiaryRecord{}) {
			t.Fatalf("get on a cancelled context = %#v, %v, want no record and context.Canceled", got, err)
		}
		if got, err := bs.ListBeneficiaries(ctx, PayerKey); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatalf("list on a cancelled context = %#v, %v, want no list and context.Canceled", got, err)
		}
		if err := bs.ReplaceBeneficiary(ctx, PayerKey, "a", 1, beneExample("a")); !errors.Is(err, context.Canceled) {
			t.Fatalf("replace on a cancelled context = %v, want context.Canceled", err)
		}
		if err := bs.DeleteBeneficiary(ctx, PayerKey, "a", 1); !errors.Is(err, context.Canceled) {
			t.Fatalf("delete on a cancelled context = %v, want context.Canceled", err)
		}

		if got := beneCalls(env); got != before {
			t.Fatalf("client calls = %+v after cancelled methods, want %+v", got, before)
		}
		beneWantList(t, bs, PayerKey, []BeneficiaryRecord{want})
	})
}

// The three families share the table and the payer key, and never meet.
func TestBeneficiaryExamplesKeepVaultsEventsAndRecipientsApart(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		bs, hs := beneficiaryOf(t, env), historyOf(t, env)

		// A vault whose id is the payer key, an event and a recipient under it.
		vault := examplesRecord(PayerKey, StateLocked15M)
		contractMustCreate(t, env.vs, vault)
		event := HistoryEvent{ID: "e1", At: beneNow(env), AmountINR: 85000, BeneficiaryRef: "XXXX-XXXX-1100"}
		historyMustAppend(t, hs, PayerKey, event)
		beneMustCreate(t, bs, PayerKey, beneExample(PayerKey))

		contractWantRecord(t, env.vs, PayerKey, vault)
		historyWantRead(t, hs, PayerKey, 10, []HistoryEvent{event})
		beneWantList(t, bs, PayerKey, []BeneficiaryRecord{beneStoredExample(env, PayerKey)})

		// A recipient id is no vault id: the same text names different things.
		if _, err := env.vs.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("vault Get of a recipient id = %v, want ErrNotFound", err)
		}
		if env.fake != nil {
			pks := map[string]bool{}
			for _, pk := range []string{"VAULT#demo-payer", "HIST#demo-payer", "BENE#demo-payer"} {
				pks[pk] = len(env.fake.rawItemsUnder(pk)) == 1
			}
			if !reflect.DeepEqual(pks, map[string]bool{"VAULT#demo-payer": true, "HIST#demo-payer": true, "BENE#demo-payer": true}) {
				t.Fatalf("items per partition = %v, want one under each of the three", pks)
			}
			if sk := fakeDynamoDBAttrS(env.fake.rawItemsUnder("BENE#demo-payer")[0], "SK"); sk != "B#"+PayerKey {
				t.Fatalf("the recipient's sort key = %q, want %q: it must hold the id", sk, "B#"+PayerKey)
			}
		}
	})

	// The partition prefixes are distinct and none starts another, so no payer key
	// or id can make one family's key look like another's.
	prefixes := []string{pkPrefix, histPKPrefix, benePKPrefix}
	for i, a := range prefixes {
		for j, b := range prefixes {
			if i != j && strings.HasPrefix(a, b) {
				t.Errorf("partition prefix %q starts with %q", a, b)
			}
		}
	}
}

// DynamoDBStore only: how each refused condition is told apart, and how a failing
// client shows. Delete failures come from the wrapper client.
func TestDynamoDBBeneficiaryFailuresAndFollowUpReads(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	// newStore builds a store whose DeleteItem can be made to fail.
	newStore := func() (*DynamoDBStore, *deleteFaultClient, *contractEnv) {
		env := contractNewDynamoEnv()
		faulty := &deleteFaultClient{DynamoDBClient: env.fake}
		ds := NewDynamoDBStore(faulty, contractTable)
		ds.now = env.clock.Now
		return ds, faulty, env
	}

	t.Run("a refused replace reads the item back once to choose the kind", func(t *testing.T) {
		ds, _, env := newStore()
		beneMustCreate(t, ds, PayerKey, beneExample("a"))
		for _, tc := range []struct {
			id      string
			version int
			want    error
		}{{"a", 9, ErrConditionFailed}, {"zz", 1, ErrNotFound}} {
			before := beneCalls(env)
			err := ds.ReplaceBeneficiary(ctx, PayerKey, tc.id, tc.version, beneExample(tc.id))
			if !errors.Is(err, tc.want) {
				t.Fatalf("replace of %q at %d = %v, want %v", tc.id, tc.version, err, tc.want)
			}
			if got := beneCalls(env); got.puts != before.puts+1 || got.gets != before.gets+1 || got.queries != before.queries || got.deletes != before.deletes {
				t.Fatalf("calls %+v -> %+v, want one PutItem and one GetItem more", before, got)
			}
		}
	})

	t.Run("a refused delete reads the item back once to choose the kind", func(t *testing.T) {
		ds, _, env := newStore()
		beneMustCreate(t, ds, PayerKey, beneExample("a"))
		for _, tc := range []struct {
			id      string
			version int
			want    error
		}{{"a", 9, ErrConditionFailed}, {"zz", 1, ErrNotFound}} {
			before := beneCalls(env)
			err := ds.DeleteBeneficiary(ctx, PayerKey, tc.id, tc.version)
			if !errors.Is(err, tc.want) {
				t.Fatalf("delete of %q at %d = %v, want %v", tc.id, tc.version, err, tc.want)
			}
			if got := beneCalls(env); got.deletes != before.deletes+1 || got.gets != before.gets+1 || got.puts != before.puts {
				t.Fatalf("calls %+v -> %+v, want one DeleteItem and one GetItem more", before, got)
			}
		}
		// A delete that works sends one DeleteItem and no read.
		before := beneCalls(env)
		if err := ds.DeleteBeneficiary(ctx, PayerKey, "a", 1); err != nil {
			t.Fatalf("delete = %v, want nil", err)
		}
		if got := beneCalls(env); got.deletes != before.deletes+1 || got.gets != before.gets {
			t.Fatalf("calls %+v -> %+v, want exactly one DeleteItem", before, got)
		}
	})

	t.Run("a record that expired after the condition was checked is told apart by the read", func(t *testing.T) {
		ds, _, env := newStore()
		beneMustCreate(t, ds, PayerKey, beneExample("a"))
		env.clock.Set(time.Unix(beneNow(env)+2_592_000+1, 0).UTC())
		if err := ds.ReplaceBeneficiary(ctx, PayerKey, "a", 1, beneExample("a")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("replace of an expired record at its version = %v, want ErrNotFound", err)
		}
		if err := ds.DeleteBeneficiary(ctx, PayerKey, "a", 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete of an expired record at its version = %v, want ErrNotFound", err)
		}
	})

	t.Run("a client failure keeps the item and the error is the fixed text around the cause", func(t *testing.T) {
		ds, faulty, env := newStore()
		beneMustCreate(t, ds, PayerKey, beneExample("a"))
		want := beneStoredExample(env, "a")

		env.fake.failNext(fakeDynamoDBOpPutItem, boom)
		err := ds.ReplaceBeneficiary(ctx, PayerKey, "a", 1, beneExample("a"))
		if !errors.Is(err, boom) || err.Error() != "replace_beneficiary a: dynamodb request failed" {
			t.Fatalf("replace with a failing put = %v, want the fixed request text around the cause", err)
		}

		faulty.err = boom
		err = ds.DeleteBeneficiary(ctx, PayerKey, "a", 1)
		if !errors.Is(err, boom) || err.Error() != "delete_beneficiary a: dynamodb request failed" {
			t.Fatalf("delete with a failing client = %v, want the fixed request text around the cause", err)
		}
		if faulty.calls != 1 || env.fake.deleteCalls != 0 {
			t.Fatalf("the wrapper saw %d calls and the fake %d, want 1 and 0", faulty.calls, env.fake.deleteCalls)
		}
		faulty.err = nil
		beneWantGet(t, ds, PayerKey, "a", want)

		// Once the failure is gone the same delete works.
		if err := ds.DeleteBeneficiary(ctx, PayerKey, "a", 1); err != nil || faulty.calls != 2 {
			t.Fatalf("delete after the failure = %v (wrapper calls %d), want nil and 2", err, faulty.calls)
		}
	})

	t.Run("an unexpected condition result is not mistaken for a missing record", func(t *testing.T) {
		ds, faulty, env := newStore()
		beneMustCreate(t, ds, PayerKey, beneExample("a"))
		faulty.err = &types.ConditionalCheckFailedException{}
		// The condition fails, the follow-up read finds the record at the version
		// asked for, and the store still reports a failed condition.
		if err := ds.DeleteBeneficiary(ctx, PayerKey, "a", 1); !errors.Is(err, ErrConditionFailed) {
			t.Fatalf("delete with a refused condition on a present record = %v, want ErrConditionFailed", err)
		}
		beneWantGet(t, ds, PayerKey, "a", beneStoredExample(env, "a"))
	})
}

// DynamoDBStore only: the list follows LastEvaluatedKey page by page, each page one
// Query, and reads every record whatever the page size.
func TestDynamoDBBeneficiaryListFollowsLastEvaluatedKey(t *testing.T) {
	env := contractNewDynamoEnv()
	ctx := context.Background()
	bs := beneficiaryOf(t, env)
	ids := []string{"a", "b", "c", "d", "e"}
	for _, id := range ids {
		beneMustCreate(t, bs, PayerKey, beneExample(id))
	}
	// Records of another payer, a vault and an event sit beside them.
	beneMustCreate(t, bs, "other-payer", beneExample("a"))
	contractMustCreate(t, env.vs, examplesRecord("v1", StateLocked15M))
	historyMustAppend(t, historyOf(t, env), PayerKey, HistoryEvent{ID: "e", At: 1})

	env.fake.pageSize = 2
	before := env.fake.queryCalls
	got, err := bs.ListBeneficiaries(ctx, PayerKey)
	if err != nil || len(got) != 5 {
		t.Fatalf("ListBeneficiaries = %d records (err %v), want 5", len(got), err)
	}
	for i, rec := range got {
		if rec.ID != ids[i] {
			t.Fatalf("record %d is %q, want %q: pages must join in ascending order", i, rec.ID, ids[i])
		}
	}
	// Pages of 2, 2 and 1 items: three Queries, the last one without a next key.
	if n := env.fake.queryCalls - before; n != 3 {
		t.Fatalf("list sent %d Query requests for 5 items in pages of 2, want 3", n)
	}
	last := env.fake.lastQuery
	if got := fakeDynamoDBAttrS(last.ExclusiveStartKey, "SK"); got != "B#d" {
		t.Fatalf("the last page started after %q, want %q", got, "B#d")
	}

	// A full page ends the list with one more, empty, Query.
	env.fake.pageSize = 5
	before = env.fake.queryCalls
	if got, err := bs.ListBeneficiaries(ctx, PayerKey); err != nil || len(got) != 5 || env.fake.queryCalls-before != 2 {
		t.Fatalf("list with a page of 5 = %d records (err %v) in %d Queries, want 5 records in 2", len(got), err, env.fake.queryCalls-before)
	}

	// Expired records on every page are dropped.
	env.clock.Advance(time.Hour)
	beneMustCreate(t, bs, PayerKey, beneExample("f"))
	env.clock.Set(time.Unix(beneNow(env)+2_592_000-3600+1, 0).UTC())
	env.fake.pageSize = 2
	list, err := bs.ListBeneficiaries(ctx, PayerKey)
	if err != nil || len(list) != 1 || list[0].ID != "f" {
		t.Fatalf("list after five records expired = %#v, %v, want only f", list, err)
	}
}

// DynamoDBStore sends no Scan, because the client interface has none. The set is
// the four methods the stores call: put, get, query and delete.
func TestDynamoDBClientInterfaceHasDeleteItemAndNoScan(t *testing.T) {
	methods := map[string]bool{}
	clientType := reflect.TypeOf((*DynamoDBClient)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		methods[clientType.Method(i).Name] = true
	}
	want := map[string]bool{"PutItem": true, "GetItem": true, "Query": true, "DeleteItem": true}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("DynamoDBClient methods = %v, want exactly %v (no Scan)", methods, want)
	}
	if _, ok := any((*dynamodb.Client)(nil)).(DynamoDBClient); !ok {
		t.Fatal("*dynamodb.Client does not implement DynamoDBClient")
	}
}

func TestBeneficiaryTypesConstantsAndCopies(t *testing.T) {
	if BeneficiaryTTLSeconds != 2_592_000 {
		t.Errorf("BeneficiaryTTLSeconds = %d, want 2592000 (30 days)", BeneficiaryTTLSeconds)
	}
	if TrustNew != "NEW" || TrustTrusted != "TRUSTED" || ProtectionExtraCare != "EXTRA_CARE" || ProtectionStandard != "STANDARD" {
		t.Errorf("levels = %q %q %q %q, want NEW TRUSTED EXTRA_CARE STANDARD", TrustNew, TrustTrusted, ProtectionExtraCare, ProtectionStandard)
	}
	if got := beneficiarySortKey("abc"); got != "B#abc" {
		t.Errorf("beneficiarySortKey(abc) = %q, want %q", got, "B#abc")
	}

	expiry := []struct{ at, want int64 }{
		{0, 2_592_000},
		{1_790_000_000, 1_792_592_000},
		{math.MinInt64, math.MinInt64 + 2_592_000},
		{math.MaxInt64 - 2_592_000, math.MaxInt64},
		{math.MaxInt64 - 2_591_999, math.MaxInt64}, // saturates, never wraps
		{math.MaxInt64, math.MaxInt64},
	}
	for _, tt := range expiry {
		if got := beneficiaryExpiry(time.Unix(tt.at, 0)); got != tt.want {
			t.Errorf("beneficiaryExpiry(%d) = %d, want %d", tt.at, got, tt.want)
		}
	}
	now := time.Unix(1000, 0)
	for _, tt := range []struct {
		expiresAt int64
		want      bool
	}{{998, true}, {999, true}, {1000, false}, {1001, false}, {0, true}} {
		if got := beneficiaryExpired(BeneficiaryRecord{ExpiresAt: tt.expiresAt}, now); got != tt.want {
			t.Errorf("beneficiaryExpired(ExpiresAt %d at 1000) = %v, want %v", tt.expiresAt, got, tt.want)
		}
	}

	// The JSON of an entry is the three keys the view shows, and an amount of 0 is
	// left out.
	for _, tt := range []struct {
		entry HistoryEntry
		want  string
	}{
		{HistoryEntry{At: 5, Kind: "PAYMENT", AmountINR: 450}, `{"at":5,"kind":"PAYMENT","amount_inr":450}`},
		{HistoryEntry{At: 5, Kind: "ADDED"}, `{"at":5,"kind":"ADDED"}`},
	} {
		got, err := json.Marshal(tt.entry)
		if err != nil || string(got) != tt.want {
			t.Errorf("json of %+v = %s, %v, want %s", tt.entry, got, err, tt.want)
		}
	}

	// clone shares nothing and keeps presence.
	verified, first := int64(5), int64(7)
	orig := BeneficiaryRecord{VerifiedAt: &verified, FirstPaymentINR: &first, History: []HistoryEntry{{At: 1, Kind: "ADDED"}}}
	cp := orig.clone()
	*cp.VerifiedAt, *cp.FirstPaymentINR, cp.History[0].Kind = 99, 99, "changed"
	if verified != 5 || first != 7 || orig.History[0].Kind != "ADDED" {
		t.Fatalf("a change to a clone reached the original: %d %d %q", verified, first, orig.History[0].Kind)
	}
	empty := BeneficiaryRecord{History: []HistoryEntry{}}.clone()
	if empty.History == nil || empty.VerifiedAt != nil || empty.FirstPaymentINR != nil {
		t.Fatalf("clone of an empty history = %#v, want it still empty and non-nil with absent pointers", empty)
	}
	if nilHistory := (BeneficiaryRecord{}).clone(); nilHistory.History != nil {
		t.Fatalf("clone of a nil history = %#v, want nil", nilHistory.History)
	}
}

// The attribute names are written out so that a rename fails here. replaceCondition
// names version and expires_at, and the TTL setting names expires_at.
func TestBeneficiaryRecordAttributeNames(t *testing.T) {
	want := []string{
		"beneficiary_id", "label", "identifier_masked", "added_at", "verified_at", "trust_level", "protection_level",
		"pressure_flag", "version", "payment_count", "first_payment_amount", "history", "expires_at",
	}
	typ := reflect.TypeOf(BeneficiaryRecord{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("dynamodbav"), ",")
		got = append(got, name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dynamodbav names = %v, want %v", got, want)
	}
	if attrVersion != "version" || attrExpiresAt != "expires_at" {
		t.Fatalf("attrVersion = %q, attrExpiresAt = %q, want the names of the Version and ExpiresAt tags", attrVersion, attrExpiresAt)
	}
}

// infra/dynamodb-table.json names the three kinds of item in its Description and
// changes nothing else: no new key attribute and no index (Requirements 13.6,
// 20.2).
func TestInfraTableDescriptionNamesTheThreePartitionsAndChangesNothingElse(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "infra", "dynamodb-table.json"))
	if err != nil {
		t.Fatalf("read infra/dynamodb-table.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse infra/dynamodb-table.json: %v", err)
	}

	description, _ := doc["Description"].(string)
	for _, part := range []string{"VAULT#", "HIST#", "BENE#"} {
		if !strings.Contains(description, part) {
			t.Errorf("Description %q does not name the %s partition", description, part)
		}
	}
	if strings.Contains(strings.ToLower(description), "one item per cooling-off vault, keyed") {
		t.Errorf("Description still has the old text that names one kind of item: %q", description)
	}
	if len(description) > 1024 {
		t.Errorf("Description is %d bytes, over the 1024 bytes CloudFormation allows", len(description))
	}

	// Everything but Description is as it was before this feature.
	delete(doc, "Description")
	wantJSON := `{
	  "AWSTemplateFormatVersion": "2010-09-09",
	  "Resources": {
	    "RukPayVaults": {
	      "Type": "AWS::DynamoDB::Table",
	      "Properties": {
	        "TableName": "RukPayVaults",
	        "BillingMode": "PAY_PER_REQUEST",
	        "AttributeDefinitions": [
	          { "AttributeName": "PK", "AttributeType": "S" },
	          { "AttributeName": "SK", "AttributeType": "S" }
	        ],
	        "KeySchema": [
	          { "AttributeName": "PK", "KeyType": "HASH" },
	          { "AttributeName": "SK", "KeyType": "RANGE" }
	        ],
	        "TimeToLiveSpecification": { "AttributeName": "expires_at", "Enabled": true }
	      }
	    }
	  },
	  "Outputs": {
	    "TableName": {
	      "Description": "Value for the DYNAMODB_TABLE_NAME environment variable.",
	      "Value": { "Ref": "RukPayVaults" }
	    }
	  }
	}`
	var want map[string]any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("parse the expected template: %v", err)
	}
	if !reflect.DeepEqual(doc, want) {
		t.Errorf("infra/dynamodb-table.json differs from the template of the previous release apart from Description:\n got  %v\n want %v", doc, want)
	}
}
