package store

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// This file extends fakeDynamoDB with the Query operation that history reads
// and recipient lists use (Requirements 13.10, 20.8), with DeleteItem and the
// replaceCondition of the Beneficiary_Record writes, and holds the self-tests of
// those extensions and of the second and third key families.
// fake_dynamodb_test.go holds the rest of the fake.

// fakeDynamoDBQueryShape is a Query request after the fake has checked it.
type fakeDynamoDBQueryShape struct {
	pk       string // the partition to read
	forward  bool   // ascending sort key order
	limit    int    // 0 means no Limit
	hasStart bool   // an ExclusiveStartKey was given
	startSK  string // the sort key of the ExclusiveStartKey
}

// Query implements DynamoDBClient for exactly the request DynamoDBStore sends:
// KeyConditionExpression queryByPartition with one :pk value, and optionally
// ScanIndexForward, Limit, ConsistentRead and ExclusiveStartKey. Any other part
// of the request (an IndexName, a filter, a projection, another expression, a
// Limit below 1) panics with a message starting "fakeDynamoDB: unsupported",
// before any item is read.
//
// Items of the partition come back sorted by sort key, ascending unless
// ScanIndexForward is false. A response holds at most Limit items, and at most
// pageSize when the knob is set. Like the real service it returns
// LastEvaluatedKey whenever it stops at that count, even when no item follows,
// so a caller cannot treat the key as proof that more items exist.
// ExclusiveStartKey resumes strictly after the given key in the direction of
// the read. ConsistentRead is recorded and has no other effect, since the fake
// always reads the latest write.
func (f *fakeDynamoDB) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.queryCalls++
	f.lastQuery = in

	// Check the request shape before any injected failure, as PutItem does.
	q := fakeDynamoDBParseQuery(in)
	if err := f.takeFault(fakeDynamoDBOpQuery); err != nil {
		return nil, err
	}

	bySK := make(map[string]map[string]types.AttributeValue)
	sks := make([]string, 0)
	for _, item := range f.items {
		if fakeDynamoDBAttrS(item, attrPK) != q.pk {
			continue
		}
		sk := fakeDynamoDBAttrS(item, attrSK)
		bySK[sk] = item
		sks = append(sks, sk)
	}
	sort.Strings(sks)
	if !q.forward {
		for i, j := 0, len(sks)-1; i < j; i, j = i+1, j-1 {
			sks[i], sks[j] = sks[j], sks[i]
		}
	}
	if q.hasStart {
		kept := make([]string, 0, len(sks))
		for _, sk := range sks {
			if (q.forward && sk > q.startSK) || (!q.forward && sk < q.startSK) {
				kept = append(kept, sk)
			}
		}
		sks = kept
	}

	pageLimit := q.limit
	if f.pageSize > 0 && (pageLimit == 0 || f.pageSize < pageLimit) {
		pageLimit = f.pageSize
	}
	out := &dynamodb.QueryOutput{}
	if pageLimit > 0 && len(sks) >= pageLimit {
		sks = sks[:pageLimit]
		out.LastEvaluatedKey = map[string]types.AttributeValue{
			attrPK: &types.AttributeValueMemberS{Value: q.pk},
			attrSK: &types.AttributeValueMemberS{Value: sks[len(sks)-1]},
		}
	}

	out.Items = make([]map[string]types.AttributeValue, 0, len(sks))
	for _, sk := range sks {
		out.Items = append(out.Items, fakeDynamoDBCloneItem(bySK[sk]))
	}
	out.Count = int32(len(out.Items))
	out.ScannedCount = out.Count
	return out, nil
}

// fakeDynamoDBParseQuery checks that in is a request the fake knows and returns
// its parts. It panics for anything else.
func fakeDynamoDBParseQuery(in *dynamodb.QueryInput) fakeDynamoDBQueryShape {
	if in.KeyConditionExpression == nil || *in.KeyConditionExpression != queryByPartition {
		got := "<nil>"
		if in.KeyConditionExpression != nil {
			got = *in.KeyConditionExpression
		}
		panic(fakeDynamoDBUnsupportedf("Query KeyConditionExpression %q, want %q", got, queryByPartition))
	}
	if len(in.ExpressionAttributeNames) != 0 {
		panic(fakeDynamoDBUnsupportedf("Query ExpressionAttributeNames %v", in.ExpressionAttributeNames))
	}
	if len(in.ExpressionAttributeValues) != 1 {
		panic(fakeDynamoDBUnsupportedf("Query with %d ExpressionAttributeValues, want 1 (%s)",
			len(in.ExpressionAttributeValues), queryPKValue))
	}
	pk, ok := in.ExpressionAttributeValues[queryPKValue].(*types.AttributeValueMemberS)
	if !ok {
		panic(fakeDynamoDBUnsupportedf("Query needs a string value in ExpressionAttributeValues[%q]", queryPKValue))
	}

	switch {
	case in.IndexName != nil:
		panic(fakeDynamoDBUnsupportedf("Query IndexName %q", *in.IndexName))
	case in.FilterExpression != nil:
		panic(fakeDynamoDBUnsupportedf("Query FilterExpression %q", *in.FilterExpression))
	case in.ProjectionExpression != nil:
		panic(fakeDynamoDBUnsupportedf("Query ProjectionExpression %q", *in.ProjectionExpression))
	case in.Select != "":
		panic(fakeDynamoDBUnsupportedf("Query Select %q", in.Select))
	case len(in.AttributesToGet) != 0:
		panic(fakeDynamoDBUnsupportedf("Query AttributesToGet %v", in.AttributesToGet))
	case in.ConditionalOperator != "":
		panic(fakeDynamoDBUnsupportedf("Query ConditionalOperator %q", in.ConditionalOperator))
	case len(in.KeyConditions) != 0:
		panic(fakeDynamoDBUnsupportedf("Query KeyConditions"))
	case len(in.QueryFilter) != 0:
		panic(fakeDynamoDBUnsupportedf("Query QueryFilter"))
	case in.ReturnConsumedCapacity != "":
		panic(fakeDynamoDBUnsupportedf("Query ReturnConsumedCapacity %q", in.ReturnConsumedCapacity))
	}

	q := fakeDynamoDBQueryShape{pk: pk.Value, forward: in.ScanIndexForward == nil || *in.ScanIndexForward}
	if in.Limit != nil {
		if *in.Limit < 1 {
			panic(fakeDynamoDBUnsupportedf("Query Limit %d, the real service needs 1 or more", *in.Limit))
		}
		q.limit = int(*in.Limit)
	}
	if len(in.ExclusiveStartKey) != 0 {
		startPK, okPK := in.ExclusiveStartKey[attrPK].(*types.AttributeValueMemberS)
		startSK, okSK := in.ExclusiveStartKey[attrSK].(*types.AttributeValueMemberS)
		if !okPK || !okSK || len(in.ExclusiveStartKey) != 2 || startPK.Value != q.pk {
			panic(fakeDynamoDBUnsupportedf("Query ExclusiveStartKey that is not a (PK, SK) pair of the queried partition"))
		}
		q.hasStart, q.startSK = true, startSK.Value
	}
	return q
}

// fakeDynamoDBAttrS returns the string value of attribute name, or "".
func fakeDynamoDBAttrS(item map[string]types.AttributeValue, name string) string {
	if s, ok := item[name].(*types.AttributeValueMemberS); ok {
		return s.Value
	}
	return ""
}

// rawItemAt returns a copy of the item stored under (pk, sk), or nil. Use it to
// inspect what a store wrote under a key family other than the vault one.
func (f *fakeDynamoDB) rawItemAt(pk, sk string) map[string]types.AttributeValue {
	f.mu.Lock()
	defer f.mu.Unlock()

	item, ok := f.items[fakeDynamoDBItemKey(pk, sk)]
	if !ok {
		return nil
	}
	return fakeDynamoDBCloneItem(item)
}

// rawItemsUnder returns copies of every item with partition key pk, ordered by
// ascending sort key.
func (f *fakeDynamoDB) rawItemsUnder(pk string) []map[string]types.AttributeValue {
	f.mu.Lock()
	defer f.mu.Unlock()

	var items []map[string]types.AttributeValue
	for _, item := range f.items {
		if fakeDynamoDBAttrS(item, attrPK) == pk {
			items = append(items, fakeDynamoDBCloneItem(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return fakeDynamoDBAttrS(items[i], attrSK) < fakeDynamoDBAttrS(items[j], attrSK)
	})
	return items
}

// ---------------------------------------------------------------------------
// Self-tests of the Query extension and of the history key family. They check
// the fake, not the stores that use it.
// ---------------------------------------------------------------------------

// fakeDynamoDBHistoryItem builds a minimal history item under payer.
func fakeDynamoDBHistoryItem(payer, sk, marker string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		attrPK:   &types.AttributeValueMemberS{Value: "HIST#" + payer},
		attrSK:   &types.AttributeValueMemberS{Value: sk},
		"marker": &types.AttributeValueMemberS{Value: marker},
	}
}

// fakeDynamoDBQueryInput is a Query of payer's history partition with no
// optional part, shaped like the one DynamoDBStore.RecentHistory builds.
func fakeDynamoDBQueryInput(payer string) *dynamodb.QueryInput {
	return &dynamodb.QueryInput{
		TableName:              aws.String(fakeDynamoDBTestTable),
		KeyConditionExpression: aws.String(queryByPartition),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			queryPKValue: &types.AttributeValueMemberS{Value: "HIST#" + payer},
		},
	}
}

// fakeDynamoDBSeedHistory stores one history item per sort key, in the order
// given, with the sort key as its marker.
func fakeDynamoDBSeedHistory(t *testing.T, f *fakeDynamoDB, payer string, sks ...string) {
	t.Helper()
	for _, sk := range sks {
		in := fakeDynamoDBCreateInput(fakeDynamoDBHistoryItem(payer, sk, sk))
		if _, err := f.PutItem(context.Background(), in); err != nil {
			t.Fatalf("seed %s/%s: %v", payer, sk, err)
		}
	}
}

// fakeDynamoDBQuerySKs returns the sort keys of out.Items in order.
func fakeDynamoDBQuerySKs(out *dynamodb.QueryOutput) []string {
	sks := make([]string, 0, len(out.Items))
	for _, item := range out.Items {
		sks = append(sks, fakeDynamoDBAttrS(item, attrSK))
	}
	return sks
}

func fakeDynamoDBMustQuery(t *testing.T, f *fakeDynamoDB, in *dynamodb.QueryInput) *dynamodb.QueryOutput {
	t.Helper()
	out, err := f.Query(context.Background(), in)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return out
}

func TestFakeDynamoDBKeyFamilies(t *testing.T) {
	put := func(f *fakeDynamoDB, pk, sk string) {
		item := map[string]types.AttributeValue{
			attrPK: &types.AttributeValueMemberS{Value: pk},
			attrSK: &types.AttributeValueMemberS{Value: sk},
		}
		_, _ = f.PutItem(context.Background(), fakeDynamoDBPutInput(item))
	}

	t.Run("known pairs are accepted", func(t *testing.T) {
		f := newFakeDynamoDB()
		for _, pair := range [][2]string{
			{"VAULT#v1", "META"},
			{"HIST#demo-payer", "E#00000000000000000001#a"},
			{"HIST#", "E#"},
		} {
			put(f, pair[0], pair[1])
		}
		if n := f.itemCount(); n != 3 {
			t.Fatalf("itemCount = %d, want 3", n)
		}
	})

	t.Run("any other pair stops the test", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"HIST#p", "META"},
			{"HIST#p", "e#1"},
			{"HIST#p", ""},
			{"VAULT#v1", "E#1"},
			{"VAULT#v1", "OTHER"},
			{"OTHER#x", "META"},
			{"OTHER#x", "E#1"},
			{"", "META"},
			{"v1", "META"},
		} {
			pair := pair
			t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
				f := newFakeDynamoDB()
				fakeDynamoDBMustPanic(t, func() { put(f, pair[0], pair[1]) })
				fakeDynamoDBMustPanic(t, func() {
					_, _ = f.GetItem(context.Background(), &dynamodb.GetItemInput{Key: map[string]types.AttributeValue{
						attrPK: &types.AttributeValueMemberS{Value: pair[0]},
						attrSK: &types.AttributeValueMemberS{Value: pair[1]},
					}})
				})
				if n := f.itemCount(); n != 0 {
					t.Fatalf("itemCount = %d after a rejected request, want 0", n)
				}
			})
		}
	})
}

func TestFakeDynamoDBItemsAreKeyedByPKAndSK(t *testing.T) {
	ctx := context.Background()
	f := newFakeDynamoDB()
	fakeDynamoDBSeed(t, f, "x", "LOCKED_15M", "vault")

	// A history item whose PK differs only by the family prefix is a different
	// item, and attribute_not_exists(PK) looks at the item with the same key.
	fakeDynamoDBSeedHistory(t, f, "x", "E#1")
	if n := f.itemCount(); n != 2 {
		t.Fatalf("itemCount = %d, want 2", n)
	}
	if m := fakeDynamoDBMarker(f.rawItem("x")); m != "vault" {
		t.Fatalf("the vault item has marker %q, want %q", m, "vault")
	}

	// The same PK with another SK is another item.
	fakeDynamoDBSeedHistory(t, f, "x", "E#2")
	if n := f.itemCount(); n != 3 {
		t.Fatalf("itemCount = %d, want 3", n)
	}

	// The same (PK, SK) twice fails the create condition and keeps the first.
	dup := fakeDynamoDBCreateInput(fakeDynamoDBHistoryItem("x", "E#1", "second"))
	if _, err := f.PutItem(ctx, dup); !fakeDynamoDBIsConditionFailed(err) {
		t.Fatalf("duplicate create error = %v, want ConditionalCheckFailedException", err)
	}
	if m := fakeDynamoDBMarker(f.rawItemAt("HIST#x", "E#1")); m != "E#1" {
		t.Fatalf("stored marker = %q, want the first write %q", m, "E#1")
	}

	// GetItem finds each item by its own pair.
	get := &dynamodb.GetItemInput{Key: map[string]types.AttributeValue{
		attrPK: &types.AttributeValueMemberS{Value: "HIST#x"},
		attrSK: &types.AttributeValueMemberS{Value: "E#2"},
	}}
	out, err := f.GetItem(ctx, get)
	if err != nil || fakeDynamoDBMarker(out.Item) != "E#2" {
		t.Fatalf("GetItem = %v, %v, want the E#2 item", out, err)
	}
}

func TestFakeDynamoDBQueryOrderLimitAndPaging(t *testing.T) {
	setup := func() *fakeDynamoDB {
		f := newFakeDynamoDB()
		// Inserted out of order, with other partitions around them.
		fakeDynamoDBSeedHistory(t, f, "p", "E#3", "E#1", "E#5", "E#2", "E#4")
		fakeDynamoDBSeedHistory(t, f, "q", "E#9")
		fakeDynamoDBSeed(t, f, "p", "LOCKED_15M", "vault")
		return f
	}
	asc := []string{"E#1", "E#2", "E#3", "E#4", "E#5"}
	desc := []string{"E#5", "E#4", "E#3", "E#2", "E#1"}

	query := func(f *fakeDynamoDB, forward *bool, limit *int32, start map[string]types.AttributeValue) *dynamodb.QueryOutput {
		in := fakeDynamoDBQueryInput("p")
		in.ScanIndexForward, in.Limit, in.ExclusiveStartKey = forward, limit, start
		return fakeDynamoDBMustQuery(t, f, in)
	}
	yes, no := aws.Bool(true), aws.Bool(false)

	t.Run("ascending by default and only the partition asked for", func(t *testing.T) {
		f := setup()
		for _, forward := range []*bool{nil, yes} {
			out := query(f, forward, nil, nil)
			if got := fakeDynamoDBQuerySKs(out); !reflect.DeepEqual(got, asc) {
				t.Fatalf("sort keys = %v, want %v", got, asc)
			}
			if out.Count != 5 || out.ScannedCount != 5 || out.LastEvaluatedKey != nil {
				t.Fatalf("Count = %d, ScannedCount = %d, LastEvaluatedKey = %v, want 5, 5, nil",
					out.Count, out.ScannedCount, out.LastEvaluatedKey)
			}
		}
	})

	t.Run("descending", func(t *testing.T) {
		out := query(setup(), no, nil, nil)
		if got := fakeDynamoDBQuerySKs(out); !reflect.DeepEqual(got, desc) {
			t.Fatalf("sort keys = %v, want %v", got, desc)
		}
	})

	t.Run("Limit stops early and names the last key it read", func(t *testing.T) {
		out := query(setup(), no, aws.Int32(2), nil)
		if got := fakeDynamoDBQuerySKs(out); !reflect.DeepEqual(got, []string{"E#5", "E#4"}) {
			t.Fatalf("sort keys = %v, want [E#5 E#4]", got)
		}
		want := map[string]types.AttributeValue{
			attrPK: &types.AttributeValueMemberS{Value: "HIST#p"},
			attrSK: &types.AttributeValueMemberS{Value: "E#4"},
		}
		if !reflect.DeepEqual(out.LastEvaluatedKey, want) {
			t.Fatalf("LastEvaluatedKey = %v, want %v", out.LastEvaluatedKey, want)
		}
	})

	t.Run("LastEvaluatedKey is also set when Limit equals the item count, as in the real service", func(t *testing.T) {
		if out := query(setup(), yes, aws.Int32(5), nil); out.LastEvaluatedKey == nil || len(out.Items) != 5 {
			t.Fatalf("items = %d, LastEvaluatedKey = %v, want 5 items and a key", len(out.Items), out.LastEvaluatedKey)
		}
		if out := query(setup(), yes, aws.Int32(6), nil); out.LastEvaluatedKey != nil || len(out.Items) != 5 {
			t.Fatalf("items = %d, LastEvaluatedKey = %v, want 5 items and no key", len(out.Items), out.LastEvaluatedKey)
		}
	})

	t.Run("ExclusiveStartKey pages through every item once, in both directions", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			forward *bool
			want    []string
		}{{"ascending", yes, asc}, {"descending", no, desc}} {
			f := setup()
			var got []string
			var start map[string]types.AttributeValue
			for page := 0; page < 10; page++ {
				out := query(f, tc.forward, aws.Int32(2), start)
				got = append(got, fakeDynamoDBQuerySKs(out)...)
				if out.LastEvaluatedKey == nil {
					break
				}
				start = out.LastEvaluatedKey
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s: paged sort keys = %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	t.Run("the pageSize knob caps a response like a small page", func(t *testing.T) {
		f := setup()
		f.pageSize = 2
		if out := query(f, no, nil, nil); len(out.Items) != 2 || out.LastEvaluatedKey == nil {
			t.Fatalf("without Limit: items = %d, LastEvaluatedKey = %v, want 2 and a key", len(out.Items), out.LastEvaluatedKey)
		}
		if out := query(f, no, aws.Int32(4), nil); len(out.Items) != 2 {
			t.Fatalf("Limit 4 with pageSize 2: items = %d, want 2", len(out.Items))
		}
		f.pageSize = 3
		if out := query(f, no, aws.Int32(2), nil); len(out.Items) != 2 {
			t.Fatalf("Limit 2 with pageSize 3: items = %d, want 2", len(out.Items))
		}
	})

	t.Run("an empty partition gives an empty non-nil list", func(t *testing.T) {
		in := fakeDynamoDBQueryInput("nobody")
		out := fakeDynamoDBMustQuery(t, setup(), in)
		if out.Items == nil || len(out.Items) != 0 || out.Count != 0 {
			t.Fatalf("Items = %#v, Count = %d, want an empty non-nil list and 0", out.Items, out.Count)
		}
	})

	t.Run("returned items are copies", func(t *testing.T) {
		f := setup()
		out := query(f, nil, nil, nil)
		out.Items[0]["marker"].(*types.AttributeValueMemberS).Value = "mutated"
		out.Items[1]["extra"] = &types.AttributeValueMemberS{Value: "mutated"}
		if m := fakeDynamoDBMarker(f.rawItemAt("HIST#p", "E#1")); m != "E#1" {
			t.Fatalf("stored marker = %q, want %q", m, "E#1")
		}
		if _, ok := f.rawItemAt("HIST#p", "E#2")["extra"]; ok {
			t.Fatal("a change to a returned item reached the fake")
		}
	})

	t.Run("rawItemsUnder lists one partition by sort key", func(t *testing.T) {
		f := setup()
		var got []string
		for _, item := range f.rawItemsUnder("HIST#p") {
			got = append(got, fakeDynamoDBAttrS(item, attrSK))
		}
		if !reflect.DeepEqual(got, asc) {
			t.Fatalf("rawItemsUnder = %v, want %v", got, asc)
		}
	})
}

func TestFakeDynamoDBQueryRecordsInputsAndInjectsFailures(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	t.Run("every call is recorded, failed ones too", func(t *testing.T) {
		f := newFakeDynamoDB()
		if f.lastQuery != nil || f.queryCalls != 0 {
			t.Fatal("a new fake should have recorded no Query")
		}
		first := fakeDynamoDBQueryInput("p")
		first.ConsistentRead = aws.Bool(true)
		fakeDynamoDBMustQuery(t, f, first)
		if f.lastQuery != first || f.queryCalls != 1 {
			t.Fatalf("after one Query: lastQuery same = %v, queryCalls = %d, want true and 1", f.lastQuery == first, f.queryCalls)
		}
		if f.lastQuery.ConsistentRead == nil || !*f.lastQuery.ConsistentRead {
			t.Error("ConsistentRead was not recorded as true")
		}

		second := fakeDynamoDBQueryInput("p")
		f.failNext(fakeDynamoDBOpQuery, boom)
		if _, err := f.Query(ctx, second); !errors.Is(err, boom) {
			t.Fatalf("injected Query error = %v, want %v", err, boom)
		}
		if f.lastQuery != second || f.queryCalls != 2 {
			t.Fatalf("a failed Query must be recorded: lastQuery same = %v, queryCalls = %d", f.lastQuery == second, f.queryCalls)
		}
		if f.putCalls != 0 || f.getCalls != 0 {
			t.Fatalf("putCalls = %d, getCalls = %d after Queries, want 0 and 0", f.putCalls, f.getCalls)
		}
	})

	t.Run("failNext fires once and failOn fires on the Nth call", func(t *testing.T) {
		f := newFakeDynamoDB()
		f.failNext(fakeDynamoDBOpQuery, boom)
		if _, err := f.Query(ctx, fakeDynamoDBQueryInput("p")); !errors.Is(err, boom) {
			t.Fatalf("first call error = %v, want %v", err, boom)
		}
		if _, err := f.Query(ctx, fakeDynamoDBQueryInput("p")); err != nil {
			t.Fatalf("second call error = %v, want nil", err)
		}

		f.failOn(fakeDynamoDBOpQuery, 3, boom)
		for call := 1; call <= 4; call++ {
			_, err := f.Query(ctx, fakeDynamoDBQueryInput("p"))
			if failed := err != nil; failed != (call == 3) {
				t.Fatalf("call %d error = %v, want failure only on call 3", call, err)
			}
		}
	})

	t.Run("a Query fault leaves PutItem and GetItem alone", func(t *testing.T) {
		f := newFakeDynamoDB()
		fakeDynamoDBSeed(t, f, "v1", "LOCKED_15M", "old")
		f.failNext(fakeDynamoDBOpQuery, boom)
		if err := fakeDynamoDBCall(f, fakeDynamoDBOpPutItem); err != nil {
			t.Fatalf("PutItem error = %v, want nil", err)
		}
		if err := fakeDynamoDBCall(f, fakeDynamoDBOpGetItem); err != nil {
			t.Fatalf("GetItem error = %v, want nil", err)
		}
		if _, err := f.Query(ctx, fakeDynamoDBQueryInput("p")); !errors.Is(err, boom) {
			t.Fatalf("Query error = %v, want %v", err, boom)
		}
	})

	t.Run("a PutItem fault leaves Query alone", func(t *testing.T) {
		f := newFakeDynamoDB()
		f.failNext(fakeDynamoDBOpPutItem, boom)
		fakeDynamoDBMustQuery(t, f, fakeDynamoDBQueryInput("p"))
	})
}

func TestFakeDynamoDBRejectsUnsupportedQueries(t *testing.T) {
	bad := func(edit func(in *dynamodb.QueryInput)) func(*fakeDynamoDB) {
		return func(f *fakeDynamoDB) {
			in := fakeDynamoDBQueryInput("p")
			edit(in)
			_, _ = f.Query(context.Background(), in)
		}
	}
	otherPK := map[string]types.AttributeValue{
		attrPK: &types.AttributeValueMemberS{Value: "HIST#other"},
		attrSK: &types.AttributeValueMemberS{Value: "E#1"},
	}

	tests := []struct {
		name string
		call func(f *fakeDynamoDB)
	}{
		{"no KeyConditionExpression", bad(func(in *dynamodb.QueryInput) { in.KeyConditionExpression = nil })},
		{"another KeyConditionExpression", bad(func(in *dynamodb.QueryInput) {
			in.KeyConditionExpression = aws.String("PK = :pk AND begins_with(SK, :s)")
		})},
		{"KeyConditionExpression with other spacing", bad(func(in *dynamodb.QueryInput) {
			in.KeyConditionExpression = aws.String("PK=:pk")
		})},
		{"expression attribute names", bad(func(in *dynamodb.QueryInput) {
			in.ExpressionAttributeNames = map[string]string{"#p": "PK"}
		})},
		{"no expression attribute values", bad(func(in *dynamodb.QueryInput) { in.ExpressionAttributeValues = nil })},
		{"an extra expression attribute value", bad(func(in *dynamodb.QueryInput) {
			in.ExpressionAttributeValues[":s"] = &types.AttributeValueMemberS{Value: "E#"}
		})},
		{"a misnamed value placeholder", bad(func(in *dynamodb.QueryInput) {
			in.ExpressionAttributeValues = map[string]types.AttributeValue{":other": &types.AttributeValueMemberS{Value: "HIST#p"}}
		})},
		{"a non-string :pk", bad(func(in *dynamodb.QueryInput) {
			in.ExpressionAttributeValues[queryPKValue] = &types.AttributeValueMemberN{Value: "1"}
		})},
		{"an IndexName", bad(func(in *dynamodb.QueryInput) { in.IndexName = aws.String("by-time") })},
		{"a FilterExpression", bad(func(in *dynamodb.QueryInput) { in.FilterExpression = aws.String("attribute_exists(at)") })},
		{"a ProjectionExpression", bad(func(in *dynamodb.QueryInput) { in.ProjectionExpression = aws.String("at") })},
		{"a Select", bad(func(in *dynamodb.QueryInput) { in.Select = types.SelectCount })},
		{"AttributesToGet", bad(func(in *dynamodb.QueryInput) { in.AttributesToGet = []string{"at"} })},
		{"a ConditionalOperator", bad(func(in *dynamodb.QueryInput) { in.ConditionalOperator = types.ConditionalOperatorAnd })},
		{"KeyConditions", bad(func(in *dynamodb.QueryInput) {
			in.KeyConditions = map[string]types.Condition{"PK": {ComparisonOperator: types.ComparisonOperatorEq}}
		})},
		{"a QueryFilter", bad(func(in *dynamodb.QueryInput) {
			in.QueryFilter = map[string]types.Condition{"at": {ComparisonOperator: types.ComparisonOperatorEq}}
		})},
		{"ReturnConsumedCapacity", bad(func(in *dynamodb.QueryInput) { in.ReturnConsumedCapacity = types.ReturnConsumedCapacityTotal })},
		{"a Limit of 0", bad(func(in *dynamodb.QueryInput) { in.Limit = aws.Int32(0) })},
		{"a negative Limit", bad(func(in *dynamodb.QueryInput) { in.Limit = aws.Int32(-3) })},
		{"an ExclusiveStartKey of another partition", bad(func(in *dynamodb.QueryInput) { in.ExclusiveStartKey = otherPK })},
		{"an ExclusiveStartKey without SK", bad(func(in *dynamodb.QueryInput) {
			in.ExclusiveStartKey = map[string]types.AttributeValue{attrPK: otherPK[attrPK]}
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDynamoDB()
			fakeDynamoDBSeedHistory(t, f, "p", "E#1")
			before := f.itemCount()
			fakeDynamoDBMustPanic(t, func() { tt.call(f) })
			// A panic must not leave the mutex held or change an item.
			if n := f.itemCount(); n != before {
				t.Fatalf("itemCount = %d after a rejected request, want %d", n, before)
			}
		})
	}
}

// The fake evaluates the key condition below by its meaning. If dynamodb.go
// changes either text, update fakeDynamoDBParseQuery first, then this test.
func TestFakeDynamoDBQueryConstantsPinned(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"queryByPartition", queryByPartition, "PK = :pk"},
		{"queryPKValue", queryPKValue, ":pk"},
		{"histPKPrefix", histPKPrefix, "HIST#"},
		{"skHistoryPrefix", skHistoryPrefix, "E#"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, but the fake evaluates %q; update the fake before changing it", tt.name, tt.got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// DeleteItem, replaceCondition and the BENE# key family (Requirements 13.10,
// 20.8). The code below is part of the fake. Its self-tests follow it.
// ---------------------------------------------------------------------------

// fakeDynamoDBReplaceCondition returns the predicate of replaceCondition for a
// put or a delete. It needs exactly the names #v and #e and the values :expected
// and :now, the values as integer Numbers. It holds when the item exists, the
// attribute #v names holds a Number equal to :expected, and the attribute #e
// names holds a Number at or above :now. An attribute that is missing or is not
// a Number makes it false, as in DynamoDB. A placeholder that is missing or has
// the wrong type panics, as the real service rejects it.
func fakeDynamoDBReplaceCondition(expr string, names map[string]string, values map[string]types.AttributeValue) fakeDynamoDBCondition {
	fakeDynamoDBExpectPlaceholders(expr, names, values, 2, 2)
	versionAttr, ok := names[replaceNameVersion]
	if !ok {
		panic(fakeDynamoDBUnsupportedf("%q needs ExpressionAttributeNames[%q]", expr, replaceNameVersion))
	}
	expiresAttr, ok := names[replaceNameExpires]
	if !ok {
		panic(fakeDynamoDBUnsupportedf("%q needs ExpressionAttributeNames[%q]", expr, replaceNameExpires))
	}
	expected := fakeDynamoDBNumberValue(expr, values, replaceValueExpected)
	now := fakeDynamoDBNumberValue(expr, values, replaceValueNow)

	return func(old map[string]types.AttributeValue, exists bool) bool {
		if !exists {
			return false
		}
		version, ok := fakeDynamoDBStoredNumber(old, versionAttr)
		if !ok || version != expected {
			return false
		}
		expires, ok := fakeDynamoDBStoredNumber(old, expiresAttr)
		return ok && expires >= now
	}
}

// fakeDynamoDBNumberValue returns the integer behind the Number placeholder
// name, or panics when it is missing, not a Number or not an integer.
func fakeDynamoDBNumberValue(expr string, values map[string]types.AttributeValue, name string) int64 {
	n, ok := values[name].(*types.AttributeValueMemberN)
	if !ok {
		panic(fakeDynamoDBUnsupportedf("%q needs an N value in ExpressionAttributeValues[%q]", expr, name))
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		panic(fakeDynamoDBUnsupportedf("%q needs an integer in ExpressionAttributeValues[%q], got %q", expr, name, n.Value))
	}
	return v
}

// fakeDynamoDBStoredNumber returns the integer in attribute name of a stored
// item. ok is false when the attribute is missing or is not a Number. A Number
// that is not an integer panics, because no store writes one to these attributes.
func fakeDynamoDBStoredNumber(item map[string]types.AttributeValue, name string) (int64, bool) {
	n, ok := item[name].(*types.AttributeValueMemberN)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		panic(fakeDynamoDBUnsupportedf("stored attribute %q holds the Number %q, which is not an int64", name, n.Value))
	}
	return v, true
}

// DeleteItem implements DynamoDBClient for the request DynamoDBStore sends: a key
// of a known family and either no ConditionExpression or replaceCondition with
// its names and values. Any other part of the request (a ReturnValues, a legacy
// Expected, another expression) panics with a message starting "fakeDynamoDB:
// unsupported", before any item is touched. A failed condition returns
// *types.ConditionalCheckFailedException and leaves the item as it was. Deleting
// a missing key without a condition succeeds, as in the real service.
//
// failNext and failOn do not offer DeleteItem: an existing self-test uses that
// name as its example of an unknown operation. Tests inject delete failures with
// a wrapper client (design note I-11).
func (f *fakeDynamoDB) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleteCalls++
	f.lastDelete = in

	key := fakeDynamoDBKeyOf(in.Key, "DeleteItem key")
	switch {
	case in.ReturnValues != "":
		panic(fakeDynamoDBUnsupportedf("DeleteItem ReturnValues %q", in.ReturnValues))
	case in.ReturnValuesOnConditionCheckFailure != "":
		panic(fakeDynamoDBUnsupportedf("DeleteItem ReturnValuesOnConditionCheckFailure %q", in.ReturnValuesOnConditionCheckFailure))
	case in.ReturnConsumedCapacity != "":
		panic(fakeDynamoDBUnsupportedf("DeleteItem ReturnConsumedCapacity %q", in.ReturnConsumedCapacity))
	case in.ReturnItemCollectionMetrics != "":
		panic(fakeDynamoDBUnsupportedf("DeleteItem ReturnItemCollectionMetrics %q", in.ReturnItemCollectionMetrics))
	case len(in.Expected) != 0:
		panic(fakeDynamoDBUnsupportedf("DeleteItem Expected"))
	case in.ConditionalOperator != "":
		panic(fakeDynamoDBUnsupportedf("DeleteItem ConditionalOperator %q", in.ConditionalOperator))
	}
	allow := fakeDynamoDBConditionOf("delete", in.ConditionExpression, in.ExpressionAttributeNames, in.ExpressionAttributeValues)

	old, exists := f.items[key]
	if !allow(old, exists) {
		return nil, &types.ConditionalCheckFailedException{}
	}
	delete(f.items, key)
	return &dynamodb.DeleteItemOutput{}, nil
}

// ---------------------------------------------------------------------------
// Self-tests of DeleteItem, replaceCondition and the BENE# family. They check
// the fake, not the stores that use it.
// ---------------------------------------------------------------------------

// fakeDynamoDBBeneItem builds a minimal recipient item under payer with the
// attributes replaceCondition reads.
func fakeDynamoDBBeneItem(payer, id string, version, expiresAt int64, marker string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		attrPK:        &types.AttributeValueMemberS{Value: benePKPrefix + payer},
		attrSK:        &types.AttributeValueMemberS{Value: skBeneficiaryPrefix + id},
		attrVersion:   &types.AttributeValueMemberN{Value: strconv.FormatInt(version, 10)},
		attrExpiresAt: &types.AttributeValueMemberN{Value: strconv.FormatInt(expiresAt, 10)},
		"marker":      &types.AttributeValueMemberS{Value: marker},
	}
}

// fakeDynamoDBReplaceInput is a put of item guarded by replaceCondition, shaped
// like the input DynamoDBStore.ReplaceBeneficiary builds.
func fakeDynamoDBReplaceInput(item map[string]types.AttributeValue, expected, now int64) *dynamodb.PutItemInput {
	names, values := replaceGuard(int(expected), now)
	in := fakeDynamoDBPutInput(item)
	in.ConditionExpression = aws.String(replaceCondition)
	in.ExpressionAttributeNames = names
	in.ExpressionAttributeValues = values
	return in
}

// fakeDynamoDBDeleteInput is a delete of the recipient (payer, id) guarded by
// replaceCondition, shaped like the input DynamoDBStore.DeleteBeneficiary builds.
func fakeDynamoDBDeleteInput(payer, id string, expected, now int64) *dynamodb.DeleteItemInput {
	names, values := replaceGuard(int(expected), now)
	return &dynamodb.DeleteItemInput{
		TableName:                 aws.String(fakeDynamoDBTestTable),
		Key:                       beneficiaryItemKey(payer, id),
		ConditionExpression:       aws.String(replaceCondition),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	}
}

// fakeDynamoDBSeedBene stores one recipient item through an unconditional put.
func fakeDynamoDBSeedBene(t *testing.T, f *fakeDynamoDB, payer, id string, version, expiresAt int64, marker string) {
	t.Helper()
	if _, err := f.PutItem(context.Background(), fakeDynamoDBPutInput(fakeDynamoDBBeneItem(payer, id, version, expiresAt, marker))); err != nil {
		t.Fatalf("seed %s/%s: %v", payer, id, err)
	}
}

// fakeDynamoDBBeneMarker returns the marker of the recipient item (payer, id), or
// "" when there is none.
func fakeDynamoDBBeneMarker(f *fakeDynamoDB, payer, id string) string {
	return fakeDynamoDBMarker(f.rawItemAt(benePKPrefix+payer, skBeneficiaryPrefix+id))
}

func TestFakeDynamoDBBeneficiaryKeyFamily(t *testing.T) {
	put := func(f *fakeDynamoDB, pk, sk string) {
		item := map[string]types.AttributeValue{
			attrPK: &types.AttributeValueMemberS{Value: pk},
			attrSK: &types.AttributeValueMemberS{Value: sk},
		}
		_, _ = f.PutItem(context.Background(), fakeDynamoDBPutInput(item))
	}

	t.Run("BENE# with a B# sort key is accepted beside the other families", func(t *testing.T) {
		f := newFakeDynamoDB()
		for _, pair := range [][2]string{
			{"BENE#demo-payer", "B#0123456789abcdef0123456789abcdef"},
			{"BENE#p", "B#1"},
			{"BENE#", "B#"},
			{"VAULT#v1", "META"},
			{"HIST#p", "E#1"},
		} {
			put(f, pair[0], pair[1])
		}
		if n := f.itemCount(); n != 5 {
			t.Fatalf("itemCount = %d, want 5", n)
		}
	})

	t.Run("BENE# with any other sort key stops the test", func(t *testing.T) {
		for _, sk := range []string{"META", "E#1", "b#1", "OTHER", ""} {
			sk := sk
			t.Run(sk, func(t *testing.T) {
				f := newFakeDynamoDB()
				fakeDynamoDBMustPanic(t, func() { put(f, "BENE#p", sk) })
				fakeDynamoDBMustPanic(t, func() {
					_, _ = f.GetItem(context.Background(), &dynamodb.GetItemInput{Key: map[string]types.AttributeValue{
						attrPK: &types.AttributeValueMemberS{Value: "BENE#p"},
						attrSK: &types.AttributeValueMemberS{Value: sk},
					}})
				})
				if n := f.itemCount(); n != 0 {
					t.Fatalf("itemCount = %d after a rejected request, want 0", n)
				}
			})
		}
	})

	t.Run("a B# sort key under another family stops the test", func(t *testing.T) {
		for _, pk := range []string{"VAULT#v1", "HIST#p", "OTHER#p"} {
			pk := pk
			t.Run(pk, func(t *testing.T) {
				f := newFakeDynamoDB()
				fakeDynamoDBMustPanic(t, func() { put(f, pk, "B#1") })
				if n := f.itemCount(); n != 0 {
					t.Fatalf("itemCount = %d after a rejected request, want 0", n)
				}
			})
		}
	})

	t.Run("the three families never meet, even for the same name", func(t *testing.T) {
		f := newFakeDynamoDB()
		fakeDynamoDBSeed(t, f, "x", "LOCKED_15M", "vault")
		fakeDynamoDBSeedHistory(t, f, "x", "E#1")
		fakeDynamoDBSeedBene(t, f, "x", "1", 1, 100, "bene")
		if n := f.itemCount(); n != 3 {
			t.Fatalf("itemCount = %d, want 3", n)
		}
		if got := fakeDynamoDBBeneMarker(f, "x", "1"); got != "bene" {
			t.Fatalf("bene marker = %q, want %q", got, "bene")
		}
		out := fakeDynamoDBMustQuery(t, f, &dynamodb.QueryInput{
			TableName:              aws.String(fakeDynamoDBTestTable),
			KeyConditionExpression: aws.String(queryByPartition),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				queryPKValue: &types.AttributeValueMemberS{Value: "BENE#x"},
			},
		})
		if got := fakeDynamoDBQuerySKs(out); !reflect.DeepEqual(got, []string{"B#1"}) {
			t.Fatalf("Query on BENE#x returned %v, want only B#1", got)
		}
	})
}

func TestFakeDynamoDBReplaceConditionOnPutItem(t *testing.T) {
	const now = 1000
	next := func() map[string]types.AttributeValue { return fakeDynamoDBBeneItem("p", "a", 4, now+50, "new") }

	tests := []struct {
		name       string
		seed       func(t *testing.T, f *fakeDynamoDB)
		in         *dynamodb.PutItemInput
		wantFailed bool
		wantMarker string // marker of the stored item afterwards; "" means no item
	}{
		{"matching version and a live record replaces", func(t *testing.T, f *fakeDynamoDB) { fakeDynamoDBSeedBene(t, f, "p", "a", 3, now+10, "old") },
			fakeDynamoDBReplaceInput(next(), 3, now), false, "new"},
		{"an expiry equal to now still replaces", func(t *testing.T, f *fakeDynamoDB) { fakeDynamoDBSeedBene(t, f, "p", "a", 3, now, "old") },
			fakeDynamoDBReplaceInput(next(), 3, now), false, "new"},
		{"an expiry one second before now is refused", func(t *testing.T, f *fakeDynamoDB) { fakeDynamoDBSeedBene(t, f, "p", "a", 3, now-1, "old") },
			fakeDynamoDBReplaceInput(next(), 3, now), true, "old"},
		{"a version one above is refused", func(t *testing.T, f *fakeDynamoDB) { fakeDynamoDBSeedBene(t, f, "p", "a", 4, now+10, "old") },
			fakeDynamoDBReplaceInput(next(), 3, now), true, "old"},
		{"a version one below is refused", func(t *testing.T, f *fakeDynamoDB) { fakeDynamoDBSeedBene(t, f, "p", "a", 2, now+10, "old") },
			fakeDynamoDBReplaceInput(next(), 3, now), true, "old"},
		{"a missing item is refused and nothing is created", func(*testing.T, *fakeDynamoDB) {},
			fakeDynamoDBReplaceInput(next(), 3, now), true, ""},
		{"an item without a version attribute is refused", func(t *testing.T, f *fakeDynamoDB) {
			item := fakeDynamoDBBeneItem("p", "a", 3, now+10, "old")
			delete(item, attrVersion)
			if _, err := f.PutItem(context.Background(), fakeDynamoDBPutInput(item)); err != nil {
				t.Fatal(err)
			}
		}, fakeDynamoDBReplaceInput(next(), 3, now), true, "old"},
		{"an item without an expires_at attribute is refused", func(t *testing.T, f *fakeDynamoDB) {
			item := fakeDynamoDBBeneItem("p", "a", 3, now+10, "old")
			delete(item, attrExpiresAt)
			if _, err := f.PutItem(context.Background(), fakeDynamoDBPutInput(item)); err != nil {
				t.Fatal(err)
			}
		}, fakeDynamoDBReplaceInput(next(), 3, now), true, "old"},
		{"a version that is not a Number is refused", func(t *testing.T, f *fakeDynamoDB) {
			item := fakeDynamoDBBeneItem("p", "a", 3, now+10, "old")
			item[attrVersion] = &types.AttributeValueMemberS{Value: "3"}
			if _, err := f.PutItem(context.Background(), fakeDynamoDBPutInput(item)); err != nil {
				t.Fatal(err)
			}
		}, fakeDynamoDBReplaceInput(next(), 3, now), true, "old"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDynamoDB()
			tt.seed(t, f)

			_, err := f.PutItem(context.Background(), tt.in)

			if got := fakeDynamoDBIsConditionFailed(err); got != tt.wantFailed {
				t.Fatalf("condition failed = %v (err = %v), want %v", got, err, tt.wantFailed)
			}
			if got := fakeDynamoDBBeneMarker(f, "p", "a"); got != tt.wantMarker {
				t.Fatalf("stored marker = %q, want %q", got, tt.wantMarker)
			}
		})
	}
}

func TestFakeDynamoDBDeleteItem(t *testing.T) {
	const now = 1000
	ctx := context.Background()

	t.Run("a matching version and a live record is deleted", func(t *testing.T) {
		f := newFakeDynamoDB()
		fakeDynamoDBSeedBene(t, f, "p", "a", 3, now+10, "old")
		fakeDynamoDBSeedBene(t, f, "p", "b", 3, now+10, "other")
		if _, err := f.DeleteItem(ctx, fakeDynamoDBDeleteInput("p", "a", 3, now)); err != nil {
			t.Fatalf("DeleteItem: %v", err)
		}
		if got := fakeDynamoDBBeneMarker(f, "p", "a"); got != "" {
			t.Fatalf("the deleted item is still stored with marker %q", got)
		}
		if got := fakeDynamoDBBeneMarker(f, "p", "b"); got != "other" {
			t.Fatalf("another item has marker %q, want %q", got, "other")
		}
	})

	t.Run("a refused condition keeps the item", func(t *testing.T) {
		for _, tc := range []struct {
			name           string
			version, until int64
		}{
			{"another version", 4, now + 10},
			{"an expired record", 3, now - 1},
		} {
			f := newFakeDynamoDB()
			fakeDynamoDBSeedBene(t, f, "p", "a", tc.version, tc.until, "old")
			_, err := f.DeleteItem(ctx, fakeDynamoDBDeleteInput("p", "a", 3, now))
			if !fakeDynamoDBIsConditionFailed(err) {
				t.Fatalf("%s: error = %v, want ConditionalCheckFailedException", tc.name, err)
			}
			if got := fakeDynamoDBBeneMarker(f, "p", "a"); got != "old" {
				t.Fatalf("%s: marker = %q, want the item kept", tc.name, got)
			}
		}
	})

	t.Run("a missing item fails the condition, and succeeds without one", func(t *testing.T) {
		f := newFakeDynamoDB()
		if _, err := f.DeleteItem(ctx, fakeDynamoDBDeleteInput("p", "a", 3, now)); !fakeDynamoDBIsConditionFailed(err) {
			t.Fatalf("conditional delete of a missing item = %v, want ConditionalCheckFailedException", err)
		}
		plain := &dynamodb.DeleteItemInput{TableName: aws.String(fakeDynamoDBTestTable), Key: beneficiaryItemKey("p", "a")}
		if _, err := f.DeleteItem(ctx, plain); err != nil {
			t.Fatalf("unconditional delete of a missing item = %v, want nil", err)
		}
	})

	t.Run("an unconditional delete removes any item of a known family", func(t *testing.T) {
		f := newFakeDynamoDB()
		fakeDynamoDBSeed(t, f, "v1", "LOCKED_15M", "vault")
		fakeDynamoDBSeedHistory(t, f, "p", "E#1")
		fakeDynamoDBSeedBene(t, f, "p", "a", 1, 5, "bene")
		keys := []map[string]types.AttributeValue{
			vaultKey("v1"),
			{
				attrPK: &types.AttributeValueMemberS{Value: "HIST#p"},
				attrSK: &types.AttributeValueMemberS{Value: "E#1"},
			},
			beneficiaryItemKey("p", "a"),
		}
		for _, key := range keys {
			if _, err := f.DeleteItem(ctx, &dynamodb.DeleteItemInput{Key: key}); err != nil {
				t.Fatalf("DeleteItem: %v", err)
			}
		}
		if n := f.itemCount(); n != 0 {
			t.Fatalf("itemCount = %d, want 0", n)
		}
	})

	t.Run("every call is recorded, failed ones too", func(t *testing.T) {
		f := newFakeDynamoDB()
		if f.lastDelete != nil || f.deleteCalls != 0 {
			t.Fatal("a new fake should have recorded no DeleteItem")
		}
		first := fakeDynamoDBDeleteInput("p", "a", 3, now)
		_, _ = f.DeleteItem(ctx, first)
		second := fakeDynamoDBDeleteInput("p", "b", 4, now)
		_, _ = f.DeleteItem(ctx, second)
		if f.lastDelete != second || f.deleteCalls != 2 {
			t.Fatalf("lastDelete same = %v, deleteCalls = %d, want true and 2", f.lastDelete == second, f.deleteCalls)
		}
		if got := aws.ToString(f.lastDelete.ConditionExpression); got != replaceCondition {
			t.Errorf("ConditionExpression = %q, want %q", got, replaceCondition)
		}
		if f.putCalls != 0 || f.getCalls != 0 || f.queryCalls != 0 {
			t.Errorf("putCalls = %d, getCalls = %d, queryCalls = %d after deletes, want 0", f.putCalls, f.getCalls, f.queryCalls)
		}
	})

	t.Run("failure injection is not offered for DeleteItem", func(t *testing.T) {
		f := newFakeDynamoDB()
		fakeDynamoDBMustPanic(t, func() { f.failNext("DeleteItem", errors.New("boom")) })
		fakeDynamoDBMustPanic(t, func() { f.failOn("DeleteItem", 1, errors.New("boom")) })
		fakeDynamoDBMustPanic(t, func() { f.failNextConditional("DeleteItem") })
	})
}

func TestFakeDynamoDBRejectsUnsupportedDeletesAndReplaces(t *testing.T) {
	const now = 1000
	good := func() *dynamodb.DeleteItemInput { return fakeDynamoDBDeleteInput("p", "a", 3, now) }
	badDelete := func(edit func(in *dynamodb.DeleteItemInput)) func(*fakeDynamoDB) {
		return func(f *fakeDynamoDB) {
			in := good()
			edit(in)
			_, _ = f.DeleteItem(context.Background(), in)
		}
	}
	badReplace := func(edit func(in *dynamodb.PutItemInput)) func(*fakeDynamoDB) {
		return func(f *fakeDynamoDB) {
			in := fakeDynamoDBReplaceInput(fakeDynamoDBBeneItem("p", "a", 4, now+50, "new"), 3, now)
			edit(in)
			_, _ = f.PutItem(context.Background(), in)
		}
	}
	number := func(v string) types.AttributeValue { return &types.AttributeValueMemberN{Value: v} }

	tests := []struct {
		name string
		call func(f *fakeDynamoDB)
	}{
		{"a delete with another condition", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ConditionExpression = aws.String("attribute_exists(PK)")
		})},
		{"a delete with the create condition", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ConditionExpression = aws.String(createCondition)
			in.ExpressionAttributeNames, in.ExpressionAttributeValues = nil, nil
		})},
		{"a delete with the update condition", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ConditionExpression = aws.String(updateCondition)
		})},
		{"a delete without a condition but with placeholders", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ConditionExpression = nil
		})},
		{"a delete without the version name", badDelete(func(in *dynamodb.DeleteItemInput) {
			delete(in.ExpressionAttributeNames, replaceNameVersion)
		})},
		{"a delete with a misnamed expiry name", badDelete(func(in *dynamodb.DeleteItemInput) {
			delete(in.ExpressionAttributeNames, replaceNameExpires)
			in.ExpressionAttributeNames["#x"] = attrExpiresAt
		})},
		{"a delete without the expected value", badDelete(func(in *dynamodb.DeleteItemInput) {
			delete(in.ExpressionAttributeValues, replaceValueExpected)
		})},
		{"a delete with a non-numeric now", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ExpressionAttributeValues[replaceValueNow] = &types.AttributeValueMemberS{Value: "1000"}
		})},
		{"a delete with a non-integer expected value", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ExpressionAttributeValues[replaceValueExpected] = number("3.5")
		})},
		{"a delete with an extra value", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ExpressionAttributeValues[":extra"] = number("1")
		})},
		{"a delete with ReturnValues", badDelete(func(in *dynamodb.DeleteItemInput) { in.ReturnValues = types.ReturnValueAllOld })},
		{"a delete with ReturnValuesOnConditionCheckFailure", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ReturnValuesOnConditionCheckFailure = types.ReturnValuesOnConditionCheckFailureAllOld
		})},
		{"a delete with ReturnConsumedCapacity", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ReturnConsumedCapacity = types.ReturnConsumedCapacityTotal
		})},
		{"a delete with ReturnItemCollectionMetrics", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ReturnItemCollectionMetrics = types.ReturnItemCollectionMetricsSize
		})},
		{"a delete with a legacy Expected", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.Expected = map[string]types.ExpectedAttributeValue{"version": {}}
		})},
		{"a delete with a ConditionalOperator", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.ConditionalOperator = types.ConditionalOperatorAnd
		})},
		{"a delete of a key outside the three families", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.Key = map[string]types.AttributeValue{
				attrPK: &types.AttributeValueMemberS{Value: "OTHER#p"},
				attrSK: &types.AttributeValueMemberS{Value: "B#a"},
			}
		})},
		{"a delete of a key without SK", badDelete(func(in *dynamodb.DeleteItemInput) {
			in.Key = map[string]types.AttributeValue{attrPK: &types.AttributeValueMemberS{Value: "BENE#p"}}
		})},
		{"a replace without the version name", badReplace(func(in *dynamodb.PutItemInput) {
			delete(in.ExpressionAttributeNames, replaceNameVersion)
		})},
		{"a replace without the now value", badReplace(func(in *dynamodb.PutItemInput) {
			delete(in.ExpressionAttributeValues, replaceValueNow)
		})},
		{"a replace with a string expected value", badReplace(func(in *dynamodb.PutItemInput) {
			in.ExpressionAttributeValues[replaceValueExpected] = &types.AttributeValueMemberS{Value: "3"}
		})},
		{"a replace with an extra name", badReplace(func(in *dynamodb.PutItemInput) {
			in.ExpressionAttributeNames["#x"] = "label"
		})},
		{"a replace of a stored item whose version is not an integer", func(f *fakeDynamoDB) {
			item := fakeDynamoDBBeneItem("p", "a", 3, now+10, "old")
			item[attrVersion] = &types.AttributeValueMemberN{Value: "3.5"}
			_, _ = f.PutItem(context.Background(), fakeDynamoDBPutInput(item))
			in := fakeDynamoDBReplaceInput(fakeDynamoDBBeneItem("p", "a", 4, now+50, "new"), 3, now)
			_, _ = f.PutItem(context.Background(), in)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDynamoDB()
			fakeDynamoDBSeedBene(t, f, "p", "a", 3, now+10, "old")
			before := f.itemCount()
			fakeDynamoDBMustPanic(t, func() { tt.call(f) })
			if n := f.itemCount(); n < before {
				t.Fatalf("itemCount = %d after a rejected request, want at least %d", n, before)
			}
			// A panic must not leave the mutex held.
			if got := fakeDynamoDBBeneMarker(f, "p", "a"); got == "" {
				t.Fatal("the seeded item is gone after a rejected request")
			}
		})
	}
}

// The fake evaluates the expression and the key prefixes below by their
// meaning. If dynamodb.go or beneficiary.go changes any of them, update the fake
// first, then this test.
func TestFakeDynamoDBBeneficiaryConstantsPinned(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"replaceCondition", replaceCondition, "attribute_exists(PK) AND #v = :expected AND #e >= :now"},
		{"replaceNameVersion", replaceNameVersion, "#v"},
		{"replaceNameExpires", replaceNameExpires, "#e"},
		{"replaceValueExpected", replaceValueExpected, ":expected"},
		{"replaceValueNow", replaceValueNow, ":now"},
		{"attrVersion", attrVersion, "version"},
		{"attrExpiresAt", attrExpiresAt, "expires_at"},
		{"benePKPrefix", benePKPrefix, "BENE#"},
		{"skBeneficiaryPrefix", skBeneficiaryPrefix, "B#"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, but the fake evaluates %q; update the fake before changing it", tt.name, tt.got, tt.want)
		}
	}
}
