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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Example tests for the history stores. Each shared example runs against both
// adapters through contractRun. The randomized checks are in pbt_history_test.go.

func TestHistoryExamplesRoundTrip(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(t, env)
		now := env.clock.Now().Unix()

		written := []HistoryEvent{
			// An empty reference and the largest amount, time and expiry.
			{ID: "big", At: math.MaxInt64, AmountINR: math.MaxInt64, BeneficiaryRef: "", ExpiresAt: HistoryExpiry(math.MaxInt64)},
			{ID: "account", At: now - 30, AmountINR: 450, BeneficiaryRef: "XXXX-XXXX-1100", ExpiresAt: HistoryExpiry(now - 30)},
			{ID: "phone", At: now - 20, AmountINR: 18000, BeneficiaryRef: "+91-XXXXXX3210", ExpiresAt: HistoryExpiry(now - 20)},
			{ID: "upi", At: now - 10, AmountINR: 85000, BeneficiaryRef: "r***@okaxis", ExpiresAt: HistoryExpiry(now - 10)},
			// The smallest values the stores accept are kept as given too.
			{ID: "small", At: math.MinInt64, AmountINR: math.MinInt64, BeneficiaryRef: "", ExpiresAt: 0},
		}
		for _, ev := range written {
			if err := hs.AppendHistory(ctx, PayerKey, ev); err != nil {
				t.Fatalf("AppendHistory(%q) = %v, want nil", ev.ID, err)
			}
		}

		want := []HistoryEvent{written[4], written[1], written[2], written[3], written[0]}
		got, err := hs.RecentHistory(ctx, PayerKey, SnapshotLimit)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("RecentHistory = %#v, %v\nwant %#v", got, err, want)
		}

		// A limit keeps the newest events, not the oldest.
		got, err = hs.RecentHistory(ctx, PayerKey, 2)
		if err != nil || !reflect.DeepEqual(got, want[3:]) {
			t.Fatalf("RecentHistory(limit 2) = %#v, %v\nwant %#v", got, err, want[3:])
		}

		// The result is a copy: changing it changes nothing stored.
		got[0].AmountINR = -1
		again, err := hs.RecentHistory(ctx, PayerKey, 2)
		if err != nil || !reflect.DeepEqual(again, want[3:]) {
			t.Fatalf("a change to a returned slice reached the store: %#v, %v", again, err)
		}

		// Another payer sees none of it, and an empty read is a non-nil list.
		other, err := hs.RecentHistory(ctx, "someone-else", SnapshotLimit)
		if err != nil || other == nil || len(other) != 0 {
			t.Fatalf("RecentHistory of another payer = %#v, %v, want an empty non-nil list", other, err)
		}
	})
}

// Fifty writers with equal and different times, released together, and readers
// that overlap them. Run it with go test -race.
func TestHistoryExamplesFiftyConcurrentWritesAllSurvive(t *testing.T) {
	const writers = 50
	contractRun(t, func(t *testing.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(t, env)
		base := env.clock.Now().Unix() - 100

		events := make([]HistoryEvent, writers)
		for i := range events {
			at := base + int64(i/5) // groups of five share a time
			events[i] = HistoryEvent{
				ID:        fmt.Sprintf("ev-%02d", i),
				At:        at,
				AmountINR: int64(100 * (i + 1)),
				ExpiresAt: HistoryExpiry(at),
			}
			if i%2 == 0 {
				events[i].BeneficiaryRef = "r***@okaxis"
			}
		}

		var (
			ready, done sync.WaitGroup
			start       = make(chan struct{})
			errs        = make([]error, writers+2)
		)
		ready.Add(writers + 2)
		done.Add(writers + 2)
		for i := range events {
			i := i
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				errs[i] = hs.AppendHistory(ctx, PayerKey, events[i])
			}()
		}
		for r := 0; r < 2; r++ {
			r := r
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				_, errs[writers+r] = hs.RecentHistory(ctx, PayerKey, SnapshotLimit)
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("goroutine %d returned %v, want nil", i, err)
			}
		}

		// The events were made in ascending (time, id) order, so that is the
		// order of the read.
		got, err := hs.RecentHistory(ctx, PayerKey, SnapshotLimit)
		if err != nil || !reflect.DeepEqual(got, events) {
			t.Fatalf("RecentHistory(limit %d) returned %d events (err %v), want all %d in order:\n got %#v\nwant %#v",
				SnapshotLimit, len(got), err, writers, got, events)
		}
		newest, err := hs.RecentHistory(ctx, PayerKey, 30)
		if err != nil || !reflect.DeepEqual(newest, events[writers-30:]) {
			t.Fatalf("RecentHistory(limit 30) = %d events (err %v), want the 30 newest", len(newest), err)
		}
	})
}

func TestHistoryExamplesExpiryBoundary(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		hs := historyOf(t, env)
		now := env.clock.Now().Unix()

		past := HistoryEvent{ID: "past", At: now - 10, AmountINR: 1, ExpiresAt: now - 1}
		equal := HistoryEvent{ID: "equal", At: now - 9, AmountINR: 2, ExpiresAt: now}
		later := HistoryEvent{ID: "later", At: now - 8, AmountINR: 3, ExpiresAt: now + 1}
		never := HistoryEvent{ID: "never", At: now - 7, AmountINR: 4, ExpiresAt: 0}
		for _, ev := range []HistoryEvent{never, later, past, equal} {
			historyMustAppend(t, hs, PayerKey, ev)
		}

		steps := []struct {
			name    string
			advance time.Duration
			want    []HistoryEvent
		}{
			{"at the start: earlier than now is hidden, equal to now is kept", 0, []HistoryEvent{equal, later, never}},
			{"one second later: the event that expired at now is hidden", time.Second, []HistoryEvent{later, never}},
			{"two seconds later", time.Second, []HistoryEvent{never}},
			{"a century later: an ExpiresAt of 0 never expires", 100 * 365 * 24 * time.Hour, []HistoryEvent{never}},
		}
		for _, step := range steps {
			env.clock.Advance(step.advance)
			got, err := hs.RecentHistory(context.Background(), PayerKey, 10)
			if err != nil || !reflect.DeepEqual(got, step.want) {
				t.Fatalf("%s: RecentHistory = %#v, %v, want %#v", step.name, got, err, step.want)
			}
		}

		// A hidden event is still stored, as DynamoDB keeps an expired item
		// until its TTL deletion runs.
		if env.fake != nil && env.fake.itemCount() != 4 {
			t.Fatalf("itemCount = %d, want 4: reads must not delete expired items", env.fake.itemCount())
		}
	})
}

func TestHistoryExamplesCancelledContextSendsNoRequest(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		hs := historyOf(t, env)
		stored := HistoryEvent{ID: "kept", At: env.clock.Now().Unix(), AmountINR: 450}
		historyMustAppend(t, hs, PayerKey, stored)
		before := historyCallsOf(env)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := hs.AppendHistory(ctx, PayerKey, HistoryEvent{ID: "late", At: 1})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("AppendHistory on a cancelled context = %v, want context.Canceled", err)
		}
		for _, limit := range []int{SnapshotLimit, 1, 0} {
			got, err := hs.RecentHistory(ctx, PayerKey, limit)
			if !errors.Is(err, context.Canceled) || got != nil {
				t.Fatalf("RecentHistory(limit %d) on a cancelled context = %v, %v, want nil and context.Canceled", limit, got, err)
			}
		}
		if after := historyCallsOf(env); after != before {
			t.Fatalf("client calls = %+v after cancelled methods, want %+v", after, before)
		}
		historyWantRead(t, hs, PayerKey, 10, []HistoryEvent{stored})
	})
}

func TestHistoryExamplesVaultsAndEventsDoNotMeet(t *testing.T) {
	contractRun(t, func(t *testing.T, env *contractEnv) {
		hs := historyOf(t, env)

		// A vault whose id is the payer key, and events under that payer key.
		vault := examplesRecord(PayerKey, StateLocked15M)
		contractMustCreate(t, env.vs, vault)
		event := HistoryEvent{ID: "e1", At: env.clock.Now().Unix(), AmountINR: 85000, BeneficiaryRef: "XXXX-XXXX-1100"}
		historyMustAppend(t, hs, PayerKey, event)

		contractWantRecord(t, env.vs, PayerKey, vault)
		historyWantRead(t, hs, PayerKey, 10, []HistoryEvent{event})
		if env.fake != nil {
			if pk := fakeDynamoDBAttrS(env.fake.rawItem(PayerKey), "PK"); pk != "VAULT#demo-payer" {
				t.Fatalf("vault PK = %q, want %q", pk, "VAULT#demo-payer")
			}
			if n := len(env.fake.rawItemsUnder("HIST#demo-payer")); n != 1 {
				t.Fatalf("%d items under HIST#demo-payer, want 1", n)
			}
		}
	})
}

// RecentHistory is one Query on the partition of the payer, with the flags of
// Requirement 13.5 and nothing more, and no request at all for a limit below 1.
func TestDynamoDBHistoryReadIsOneQueryWithoutAnIndex(t *testing.T) {
	env := contractNewDynamoEnv()
	ctx := context.Background()
	hs := historyOf(t, env)

	// The client interface has no Scan, so the store cannot send one.
	methods := map[string]bool{}
	clientType := reflect.TypeOf((*DynamoDBClient)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		methods[clientType.Method(i).Name] = true
	}
	if want := map[string]bool{"PutItem": true, "GetItem": true, "Query": true, "DeleteItem": true}; !reflect.DeepEqual(methods, want) {
		t.Fatalf("DynamoDBClient methods = %v, want exactly %v (no Scan)", methods, want)
	}

	historyMustAppend(t, hs, PayerKey, HistoryEvent{ID: "e1", At: env.clock.Now().Unix(), AmountINR: 450})
	puts, gets := env.fake.putCalls, env.fake.getCalls

	if _, err := hs.RecentHistory(ctx, PayerKey, SnapshotLimit); err != nil {
		t.Fatalf("RecentHistory = %v, want nil", err)
	}
	if env.fake.queryCalls != 1 || env.fake.putCalls != puts || env.fake.getCalls != gets {
		t.Fatalf("calls after one read: queries %d, puts %d (was %d), gets %d (was %d), want exactly one Query and nothing else",
			env.fake.queryCalls, env.fake.putCalls, puts, env.fake.getCalls, gets)
	}

	q := env.fake.lastQuery
	if q.TableName == nil || *q.TableName != contractTable {
		t.Errorf("TableName = %v, want %q", q.TableName, contractTable)
	}
	if q.KeyConditionExpression == nil || *q.KeyConditionExpression != "PK = :pk" {
		t.Errorf("KeyConditionExpression = %v, want %q", q.KeyConditionExpression, "PK = :pk")
	}
	wantValues := map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: "HIST#demo-payer"}}
	if !reflect.DeepEqual(q.ExpressionAttributeValues, wantValues) {
		t.Errorf("ExpressionAttributeValues = %v, want %v", q.ExpressionAttributeValues, wantValues)
	}
	if q.ScanIndexForward == nil || *q.ScanIndexForward {
		t.Errorf("ScanIndexForward = %v, want false (newest first)", q.ScanIndexForward)
	}
	if q.Limit == nil || *q.Limit != SnapshotLimit {
		t.Errorf("Limit = %v, want %d", q.Limit, SnapshotLimit)
	}
	if q.ConsistentRead == nil || !*q.ConsistentRead {
		t.Errorf("ConsistentRead = %v, want true", q.ConsistentRead)
	}
	if q.IndexName != nil || q.FilterExpression != nil || q.ProjectionExpression != nil ||
		q.Select != "" || len(q.ExpressionAttributeNames) != 0 || len(q.ExclusiveStartKey) != 0 {
		t.Errorf("Query names an index, a filter, a projection, names or a start key: %+v", q)
	}

	// A limit below 1 sends nothing.
	for _, limit := range []int{0, -1, math.MinInt32} {
		if got, err := hs.RecentHistory(ctx, PayerKey, limit); err != nil || got == nil || len(got) != 0 {
			t.Fatalf("RecentHistory(limit %d) = %#v, %v, want an empty non-nil list", limit, got, err)
		}
	}
	if env.fake.queryCalls != 1 {
		t.Fatalf("queryCalls = %d after reads with a limit below 1, want 1", env.fake.queryCalls)
	}
}

// One Query is all a read sends, even when the response page is short and holds
// LastEvaluatedKey. The events past it are older than the ones wanted.
func TestDynamoDBHistoryReadDoesNotFollowLastEvaluatedKey(t *testing.T) {
	env := contractNewDynamoEnv()
	hs := historyOf(t, env)
	now := env.clock.Now().Unix()
	for i := int64(0); i < 5; i++ {
		historyMustAppend(t, hs, PayerKey, HistoryEvent{ID: fmt.Sprintf("e%d", i), At: now - 10 + i, AmountINR: i})
	}

	env.fake.pageSize = 2
	got, err := hs.RecentHistory(context.Background(), PayerKey, SnapshotLimit)
	if err != nil {
		t.Fatalf("RecentHistory = %v, want nil", err)
	}
	if env.fake.queryCalls != 1 {
		t.Fatalf("queryCalls = %d for a short page, want exactly 1", env.fake.queryCalls)
	}
	if len(got) != 2 || got[0].ID != "e3" || got[1].ID != "e4" {
		t.Fatalf("RecentHistory = %#v, want the two newest events e3 and e4, oldest first", got)
	}
}

// historyDriftClient edits each request before it reaches the fake, the way a
// store that had drifted from the shapes the fake knows would send it.
type historyDriftClient struct {
	DynamoDBClient
	editQuery func(*dynamodb.QueryInput)
	editPut   func(*dynamodb.PutItemInput)
}

func (c historyDriftClient) Query(ctx context.Context, in *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if c.editQuery != nil {
		c.editQuery(in)
	}
	return c.DynamoDBClient.Query(ctx, in, opts...)
}

func (c historyDriftClient) PutItem(ctx context.Context, in *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if c.editPut != nil {
		c.editPut(in)
	}
	return c.DynamoDBClient.PutItem(ctx, in, opts...)
}

// An unknown request shape stops the test with a message that names the part,
// and changes no item.
func TestFakeDynamoDBStopsTheTestOnRequestShapesTheStoreDriftsTo(t *testing.T) {
	str := func(s string) *string { return &s }
	i32 := func(n int32) *int32 { return &n }

	tests := []struct {
		name      string
		editQuery func(*dynamodb.QueryInput)
		editPut   func(*dynamodb.PutItemInput)
		wantPart  string
	}{
		{name: "a Query on an index", wantPart: "IndexName",
			editQuery: func(in *dynamodb.QueryInput) { in.IndexName = str("by-time") }},
		{name: "a Query with a filter", wantPart: "FilterExpression",
			editQuery: func(in *dynamodb.QueryInput) { in.FilterExpression = str("amount_inr > :a") }},
		{name: "a Query with a projection", wantPart: "ProjectionExpression",
			editQuery: func(in *dynamodb.QueryInput) { in.ProjectionExpression = str("at") }},
		{name: "a Query with another key condition", wantPart: "KeyConditionExpression",
			editQuery: func(in *dynamodb.QueryInput) { in.KeyConditionExpression = str("PK = :pk AND SK > :sk") }},
		{name: "a Query with a Limit of 0", wantPart: "Limit",
			editQuery: func(in *dynamodb.QueryInput) { in.Limit = i32(0) }},
		{name: "a put with another condition", wantPart: "ConditionExpression",
			editPut: func(in *dynamodb.PutItemInput) { in.ConditionExpression = str("attribute_exists(PK)") }},
		{name: "a history put with the vault sort key", wantPart: "SK",
			editPut: func(in *dynamodb.PutItemInput) {
				in.Item[attrSK] = &types.AttributeValueMemberS{Value: skMeta}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeDynamoDB()
			ds := NewDynamoDBStore(historyDriftClient{DynamoDBClient: fake, editQuery: tt.editQuery, editPut: tt.editPut}, contractTable)

			func() {
				defer func() {
					r := recover()
					if r == nil {
						t.Error("the store's request returned, want the fake to stop the test with a panic")
						return
					}
					msg := fmt.Sprint(r)
					if !strings.Contains(msg, "fakeDynamoDB: unsupported") || !strings.Contains(msg, tt.wantPart) {
						t.Errorf("panic = %q, want a fakeDynamoDB unsupported message that names %q", msg, tt.wantPart)
					}
				}()
				if tt.editPut != nil {
					_ = ds.AppendHistory(context.Background(), PayerKey, HistoryEvent{ID: "e1", At: 1})
					return
				}
				_, _ = ds.RecentHistory(context.Background(), PayerKey, 10)
			}()
			if n := fake.itemCount(); n != 0 {
				t.Errorf("itemCount = %d after a rejected request, want 0", n)
			}
		})
	}
}

func TestHistoryConstantsAndHelpers(t *testing.T) {
	if PayerKey != "demo-payer" || HistoryRetentionSeconds != 604800 || SnapshotLimit != 100 {
		t.Errorf("PayerKey = %q, HistoryRetentionSeconds = %d, SnapshotLimit = %d, want %q, 604800, 100",
			PayerKey, HistoryRetentionSeconds, SnapshotLimit, "demo-payer")
	}

	expiry := []struct {
		at, want int64
	}{
		{0, 604800},
		{1790000000, 1790604800},
		{math.MinInt64, math.MinInt64 + 604800},
		{math.MaxInt64 - 604801, math.MaxInt64 - 1},
		{math.MaxInt64 - 604800, math.MaxInt64},
		{math.MaxInt64 - 1, math.MaxInt64}, // saturates, never wraps
		{math.MaxInt64, math.MaxInt64},
	}
	for _, tt := range expiry {
		if got := HistoryExpiry(tt.at); got != tt.want {
			t.Errorf("HistoryExpiry(%d) = %d, want %d", tt.at, got, tt.want)
		}
	}

	keys := []struct {
		at   int64
		id   string
		want string
	}{
		{math.MinInt64, "x", "E#00000000000000000000#x"},
		{-1, "x", "E#09223372036854775807#x"},
		{0, "a", "E#09223372036854775808#a"},
		{1, "", "E#09223372036854775809#"},
		{math.MaxInt64, "z#z", "E#18446744073709551615#z#z"},
	}
	for _, tt := range keys {
		if got := historySortKey(tt.at, tt.id); got != tt.want {
			t.Errorf("historySortKey(%d, %q) = %q, want %q", tt.at, tt.id, got, tt.want)
		}
	}
	sorted := []string{
		historySortKey(math.MinInt64, "b"), historySortKey(-5, "b"), historySortKey(0, "a"),
		historySortKey(0, "a#"), historySortKey(0, "b"), historySortKey(7, ""), historySortKey(math.MaxInt64, "a"),
	}
	if !sort.StringsAreSorted(sorted) {
		t.Errorf("sort keys are not in ascending (time, id) order: %q", sorted)
	}
}

// The history items live in the existing table, so the table definition needs
// the same keys and TTL attribute and no index. The store names no index
// (Requirement 13.6), so the file must define none that it relies on.
func TestInfraTableServesHistoryWithoutAnIndex(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "infra", "dynamodb-table.json"))
	if err != nil {
		t.Fatalf("read infra/dynamodb-table.json: %v", err)
	}
	var doc struct {
		Resources map[string]struct {
			Properties struct {
				AttributeDefinitions    []struct{ AttributeName, AttributeType string }
				KeySchema               []struct{ AttributeName, KeyType string }
				TimeToLiveSpecification struct {
					AttributeName string
					Enabled       bool
				}
				GlobalSecondaryIndexes []json.RawMessage
				LocalSecondaryIndexes  []json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse infra/dynamodb-table.json: %v", err)
	}
	table, ok := doc.Resources["RukPayVaults"]
	if !ok {
		t.Fatal("infra/dynamodb-table.json has no RukPayVaults resource")
	}
	p := table.Properties

	wantDefs := []struct{ AttributeName, AttributeType string }{{"PK", "S"}, {"SK", "S"}}
	if !reflect.DeepEqual(p.AttributeDefinitions, wantDefs) {
		t.Errorf("AttributeDefinitions = %v, want %v", p.AttributeDefinitions, wantDefs)
	}
	wantKeys := []struct{ AttributeName, KeyType string }{{"PK", "HASH"}, {"SK", "RANGE"}}
	if !reflect.DeepEqual(p.KeySchema, wantKeys) {
		t.Errorf("KeySchema = %v, want %v", p.KeySchema, wantKeys)
	}
	if p.TimeToLiveSpecification.AttributeName != "expires_at" || !p.TimeToLiveSpecification.Enabled {
		t.Errorf("TimeToLiveSpecification = %+v, want expires_at enabled", p.TimeToLiveSpecification)
	}
	if len(p.GlobalSecondaryIndexes) != 0 || len(p.LocalSecondaryIndexes) != 0 {
		t.Errorf("the table defines an index, but no store request names one")
	}
}
