package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var _ DynamoDBClient = (*fakeDynamoDB)(nil)

// Operation names accepted by failNext, failNextConditional and failOn.
const (
	fakeDynamoDBOpPutItem = "PutItem"
	fakeDynamoDBOpGetItem = "GetItem"
)

// Placeholders inside updateCondition ("attribute_exists(PK) AND #s = :expected").
// The expression text lives in dynamodb.go; the fake only needs to know which
// placeholders to resolve.
const (
	fakeDynamoDBNamePlaceholder  = "#s"
	fakeDynamoDBValuePlaceholder = ":expected"
)

// fakeDynamoDB is an in-process DynamoDBClient for tests (Requirements 11.3,
// 11.5). It needs no Docker, DynamoDB Local or network.
//
// Behavior:
//   - Items live in a map keyed by the PK string value. SK must always be
//     skMeta. PutItem and GetItem store and return deep copies, so callers can
//     never mutate a stored item through a map they hold.
//   - PutItem evaluates exactly the two ConditionExpressions DynamoDBStore
//     sends (createCondition and updateCondition) and an absent expression as
//     an unconditional write. A failed condition returns
//     *types.ConditionalCheckFailedException and leaves the stored item
//     unchanged.
//   - Anything else (another expression, a missing or mismatched placeholder,
//     an item without PK or with SK other than META) panics with a message
//     starting "fakeDynamoDB: unsupported". A drifted store cannot pass a test
//     silently.
//   - GetItem returns an output with a nil Item for a missing key, as the real
//     service does. It always reads the latest write, so ConsistentRead is
//     recorded but has no other effect.
//   - failNext, failNextConditional and failOn make a chosen call fail with an
//     arbitrary error. A failed call changes no item.
//   - The context and the option functions are ignored. TTL deletion of
//     expires_at is not emulated, since the store compares expiry itself.
//
// lastPut, lastGet, putCalls and getCalls record every call, including calls
// that fail. They are written under mu, so read them directly only when no
// call is in flight (for example after wg.Wait()).
//
// The zero value is ready to use; newFakeDynamoDB is a convenience.
type fakeDynamoDB struct {
	mu     sync.Mutex
	items  map[string]map[string]types.AttributeValue // keyed by PK value
	faults map[string]fakeDynamoDBFault               // pending failure per operation

	lastPut  *dynamodb.PutItemInput // input of the most recent PutItem, nil before the first call
	lastGet  *dynamodb.GetItemInput // input of the most recent GetItem, nil before the first call
	putCalls int                    // number of PutItem calls
	getCalls int                    // number of GetItem calls
}

// fakeDynamoDBFault is a pending injected failure. It fires when remaining
// reaches zero and is then removed.
type fakeDynamoDBFault struct {
	remaining int
	err       error
}

// fakeDynamoDBCondition reports whether a put may proceed, given the item
// currently stored under the same PK (if any).
type fakeDynamoDBCondition func(old map[string]types.AttributeValue, exists bool) bool

// newFakeDynamoDB returns an empty fake.
func newFakeDynamoDB() *fakeDynamoDB {
	return &fakeDynamoDB{}
}

// PutItem implements DynamoDBClient. See the type comment for the supported
// ConditionExpressions.
func (f *fakeDynamoDB) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.putCalls++
	f.lastPut = in

	// Check the request shape before any injected failure so drift is caught
	// even in tests that inject errors.
	pk := fakeDynamoDBKeyOf(in.Item, "PutItem item")
	allow := fakeDynamoDBParseCondition(in)

	if err := f.takeFault(fakeDynamoDBOpPutItem); err != nil {
		return nil, err
	}

	old, exists := f.items[pk]
	if !allow(old, exists) {
		return nil, &types.ConditionalCheckFailedException{}
	}
	if f.items == nil {
		f.items = make(map[string]map[string]types.AttributeValue)
	}
	f.items[pk] = fakeDynamoDBCloneItem(in.Item)
	return &dynamodb.PutItemOutput{}, nil
}

// GetItem implements DynamoDBClient. A missing key yields an output with a nil
// Item and no error.
func (f *fakeDynamoDB) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.getCalls++
	f.lastGet = in

	pk := fakeDynamoDBKeyOf(in.Key, "GetItem key")
	if err := f.takeFault(fakeDynamoDBOpGetItem); err != nil {
		return nil, err
	}

	item, ok := f.items[pk]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	return &dynamodb.GetItemOutput{Item: fakeDynamoDBCloneItem(item)}, nil
}

// failNext makes the next call of op return err instead of running. The
// failure fires once and is then cleared. op is fakeDynamoDBOpPutItem or
// fakeDynamoDBOpGetItem.
//
// Failures are tracked per operation, so a pending GetItem failure does not
// touch PutItem calls. To fail only the follow-up read that UpdateIf performs
// after a conditional failure, call failNext(fakeDynamoDBOpGetItem, err) before
// UpdateIf. The UpdateIf put runs normally and, when its condition fails, the
// follow-up GetItem returns err. To also force that put to fail its condition
// regardless of stored state, add failNextConditional(fakeDynamoDBOpPutItem).
func (f *fakeDynamoDB) failNext(op string, err error) {
	f.failOn(op, 1, err)
}

// failNextConditional makes the next call of op return a
// *types.ConditionalCheckFailedException, as if another writer had changed the
// item first.
func (f *fakeDynamoDB) failNextConditional(op string) {
	f.failOn(op, 1, &types.ConditionalCheckFailedException{})
}

// failOn makes the nth call of op from now (n >= 1, so n == 1 is the next
// call) return err instead of running. The failure fires once and is then
// cleared. A second failOn for the same op replaces a pending one.
func (f *fakeDynamoDB) failOn(op string, n int, err error) {
	if op != fakeDynamoDBOpPutItem && op != fakeDynamoDBOpGetItem {
		panic(fakeDynamoDBUnsupportedf("failure injection for operation %q", op))
	}
	if n < 1 || err == nil {
		panic(fakeDynamoDBUnsupportedf("failOn(%s, n=%d, err=%v): n must be >= 1 and err non-nil", op, n, err))
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.faults == nil {
		f.faults = make(map[string]fakeDynamoDBFault)
	}
	f.faults[op] = fakeDynamoDBFault{remaining: n, err: err}
}

// rawItem returns a copy of the item stored for the vault id (PK is
// "VAULT#<id>"), or nil when no such item exists. Use it to inspect what the
// store wrote. Mutating the result does not affect the fake.
func (f *fakeDynamoDB) rawItem(id string) map[string]types.AttributeValue {
	f.mu.Lock()
	defer f.mu.Unlock()

	item, ok := f.items[pkPrefix+id]
	if !ok {
		return nil
	}
	return fakeDynamoDBCloneItem(item)
}

// itemCount returns the number of stored items.
func (f *fakeDynamoDB) itemCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.items)
}

// takeFault counts one call of op against its pending failure and returns the
// error to inject when the failure fires. The caller holds f.mu.
func (f *fakeDynamoDB) takeFault(op string) error {
	fault, ok := f.faults[op]
	if !ok {
		return nil
	}
	fault.remaining--
	if fault.remaining > 0 {
		f.faults[op] = fault
		return nil
	}
	delete(f.faults, op)
	return fault.err
}

// fakeDynamoDBUnsupportedf builds the panic value for any request the fake
// refuses to guess about.
func fakeDynamoDBUnsupportedf(format string, args ...any) error {
	return fmt.Errorf("fakeDynamoDB: unsupported: %s", fmt.Sprintf(format, args...))
}

// fakeDynamoDBKeyOf returns the PK string of attrs and checks that SK is
// skMeta. what names the request part for the panic message.
func fakeDynamoDBKeyOf(attrs map[string]types.AttributeValue, what string) string {
	pk, ok := attrs[attrPK].(*types.AttributeValueMemberS)
	if !ok {
		panic(fakeDynamoDBUnsupportedf("%s has no string attribute %s", what, attrPK))
	}
	sk, ok := attrs[attrSK].(*types.AttributeValueMemberS)
	if !ok || sk.Value != skMeta {
		panic(fakeDynamoDBUnsupportedf("%s must have string attribute %s = %q", what, attrSK, skMeta))
	}
	return pk.Value
}

// fakeDynamoDBParseCondition turns the put's ConditionExpression into a
// predicate. It panics for any expression other than createCondition and
// updateCondition, and for placeholders the expression does not use or that
// are missing, which the real service also rejects.
func fakeDynamoDBParseCondition(in *dynamodb.PutItemInput) fakeDynamoDBCondition {
	names, values := in.ExpressionAttributeNames, in.ExpressionAttributeValues

	if in.ConditionExpression == nil {
		fakeDynamoDBExpectPlaceholders("put without ConditionExpression", names, values, 0, 0)
		return func(map[string]types.AttributeValue, bool) bool { return true }
	}

	switch expr := *in.ConditionExpression; expr {
	case createCondition:
		fakeDynamoDBExpectPlaceholders(expr, names, values, 0, 0)
		return func(_ map[string]types.AttributeValue, exists bool) bool { return !exists }

	case updateCondition:
		fakeDynamoDBExpectPlaceholders(expr, names, values, 1, 1)
		stateAttr, ok := names[fakeDynamoDBNamePlaceholder]
		if !ok {
			panic(fakeDynamoDBUnsupportedf("%q needs ExpressionAttributeNames[%q]", expr, fakeDynamoDBNamePlaceholder))
		}
		want, ok := values[fakeDynamoDBValuePlaceholder].(*types.AttributeValueMemberS)
		if !ok {
			panic(fakeDynamoDBUnsupportedf("%q needs an S value in ExpressionAttributeValues[%q]", expr, fakeDynamoDBValuePlaceholder))
		}
		expected := want.Value
		return func(old map[string]types.AttributeValue, exists bool) bool {
			if !exists {
				return false
			}
			got, ok := old[stateAttr].(*types.AttributeValueMemberS)
			return ok && got.Value == expected
		}

	default:
		panic(fakeDynamoDBUnsupportedf("ConditionExpression %q", expr))
	}
}

// fakeDynamoDBExpectPlaceholders panics unless the request carries exactly
// wantNames expression attribute names and wantValues expression attribute
// values.
func fakeDynamoDBExpectPlaceholders(what string, names map[string]string, values map[string]types.AttributeValue, wantNames, wantValues int) {
	if len(names) != wantNames || len(values) != wantValues {
		panic(fakeDynamoDBUnsupportedf("%s with %d names and %d values, want %d and %d",
			what, len(names), len(values), wantNames, wantValues))
	}
}

// fakeDynamoDBCloneItem deep-copies an item.
func fakeDynamoDBCloneItem(item map[string]types.AttributeValue) map[string]types.AttributeValue {
	if item == nil {
		return nil
	}
	out := make(map[string]types.AttributeValue, len(item))
	for name, value := range item {
		out[name] = fakeDynamoDBCloneValue(value)
	}
	return out
}

// fakeDynamoDBCloneValue deep-copies an attribute value of any DynamoDB type.
func fakeDynamoDBCloneValue(v types.AttributeValue) types.AttributeValue {
	switch v := v.(type) {
	case nil:
		return nil
	case *types.AttributeValueMemberS:
		return &types.AttributeValueMemberS{Value: v.Value}
	case *types.AttributeValueMemberN:
		return &types.AttributeValueMemberN{Value: v.Value}
	case *types.AttributeValueMemberBOOL:
		return &types.AttributeValueMemberBOOL{Value: v.Value}
	case *types.AttributeValueMemberNULL:
		return &types.AttributeValueMemberNULL{Value: v.Value}
	case *types.AttributeValueMemberB:
		return &types.AttributeValueMemberB{Value: bytes.Clone(v.Value)}
	case *types.AttributeValueMemberSS:
		return &types.AttributeValueMemberSS{Value: slices.Clone(v.Value)}
	case *types.AttributeValueMemberNS:
		return &types.AttributeValueMemberNS{Value: slices.Clone(v.Value)}
	case *types.AttributeValueMemberBS:
		var set [][]byte
		if v.Value != nil {
			set = make([][]byte, len(v.Value))
			for i, b := range v.Value {
				set[i] = bytes.Clone(b)
			}
		}
		return &types.AttributeValueMemberBS{Value: set}
	case *types.AttributeValueMemberL:
		var list []types.AttributeValue
		if v.Value != nil {
			list = make([]types.AttributeValue, len(v.Value))
			for i, elem := range v.Value {
				list[i] = fakeDynamoDBCloneValue(elem)
			}
		}
		return &types.AttributeValueMemberL{Value: list}
	case *types.AttributeValueMemberM:
		return &types.AttributeValueMemberM{Value: fakeDynamoDBCloneItem(v.Value)}
	default:
		panic(fakeDynamoDBUnsupportedf("attribute value type %T", v))
	}
}

// ---------------------------------------------------------------------------
// Self-tests. These check the fake, not the stores that use it.
// ---------------------------------------------------------------------------

const fakeDynamoDBTestTable = "RukPayVaultsTest"

// fakeDynamoDBItem builds a minimal item for vault id with a state attribute
// and a marker attribute that tests use to tell versions of an item apart.
func fakeDynamoDBItem(id, state, marker string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		attrPK:    &types.AttributeValueMemberS{Value: pkPrefix + id},
		attrSK:    &types.AttributeValueMemberS{Value: skMeta},
		attrState: &types.AttributeValueMemberS{Value: state},
		"marker":  &types.AttributeValueMemberS{Value: marker},
	}
}

// fakeDynamoDBMarker returns the marker attribute of item, or "" if absent.
func fakeDynamoDBMarker(item map[string]types.AttributeValue) string {
	if m, ok := item["marker"].(*types.AttributeValueMemberS); ok {
		return m.Value
	}
	return ""
}

// fakeDynamoDBPutInput is an unconditional put of item.
func fakeDynamoDBPutInput(item map[string]types.AttributeValue) *dynamodb.PutItemInput {
	return &dynamodb.PutItemInput{TableName: aws.String(fakeDynamoDBTestTable), Item: item}
}

// fakeDynamoDBCreateInput is a put of item guarded by createCondition.
func fakeDynamoDBCreateInput(item map[string]types.AttributeValue) *dynamodb.PutItemInput {
	in := fakeDynamoDBPutInput(item)
	in.ConditionExpression = aws.String(createCondition)
	return in
}

// fakeDynamoDBUpdateInput is a put of item guarded by updateCondition with the
// given expected state, shaped like the input DynamoDBStore.UpdateIf builds.
func fakeDynamoDBUpdateInput(item map[string]types.AttributeValue, expected string) *dynamodb.PutItemInput {
	in := fakeDynamoDBPutInput(item)
	in.ConditionExpression = aws.String(updateCondition)
	in.ExpressionAttributeNames = map[string]string{fakeDynamoDBNamePlaceholder: attrState}
	in.ExpressionAttributeValues = map[string]types.AttributeValue{
		fakeDynamoDBValuePlaceholder: &types.AttributeValueMemberS{Value: expected},
	}
	return in
}

// fakeDynamoDBGetInput is a strongly consistent read of vault id.
func fakeDynamoDBGetInput(id string) *dynamodb.GetItemInput {
	return &dynamodb.GetItemInput{
		TableName:      aws.String(fakeDynamoDBTestTable),
		Key:            vaultKey(id),
		ConsistentRead: aws.Bool(true),
	}
}

// fakeDynamoDBSeed stores an item through an unconditional put.
func fakeDynamoDBSeed(t *testing.T, f *fakeDynamoDB, id, state, marker string) {
	t.Helper()
	if _, err := f.PutItem(context.Background(), fakeDynamoDBPutInput(fakeDynamoDBItem(id, state, marker))); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// fakeDynamoDBCall runs one PutItem or GetItem on vault "v1" and returns its
// error. The put writes marker "new".
func fakeDynamoDBCall(f *fakeDynamoDB, op string) error {
	ctx := context.Background()
	switch op {
	case fakeDynamoDBOpPutItem:
		_, err := f.PutItem(ctx, fakeDynamoDBPutInput(fakeDynamoDBItem("v1", "LOCKED_15M", "new")))
		return err
	case fakeDynamoDBOpGetItem:
		_, err := f.GetItem(ctx, fakeDynamoDBGetInput("v1"))
		return err
	}
	panic("fakeDynamoDBCall: unknown operation " + op)
}

func fakeDynamoDBIsConditionFailed(err error) bool {
	var failed *types.ConditionalCheckFailedException
	return errors.As(err, &failed)
}

// fakeDynamoDBMustPanic fails the test unless fn panics with the fake's
// "unsupported" message.
func fakeDynamoDBMustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("call returned, want a panic")
			return
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, "fakeDynamoDB: unsupported") {
			t.Errorf("panic = %q, want a fakeDynamoDB unsupported message", msg)
		}
	}()
	fn()
}

func TestFakeDynamoDBConditionalPut(t *testing.T) {
	const (
		locked   = "LOCKED_15M"
		approved = "GUARDIAN_APPROVED"
	)
	next := func() map[string]types.AttributeValue { return fakeDynamoDBItem("v1", approved, "new") }

	tests := []struct {
		name       string
		existing   string // state of the seeded item (marker "old"); "" seeds nothing
		in         *dynamodb.PutItemInput
		wantFailed bool
		wantMarker string // marker of the stored item afterwards; "" means no item
	}{
		{"create on empty table succeeds", "", fakeDynamoDBCreateInput(next()), false, "new"},
		{"create on existing item fails and keeps it", locked, fakeDynamoDBCreateInput(next()), true, "old"},
		{"update with matching state replaces the item", locked, fakeDynamoDBUpdateInput(next(), locked), false, "new"},
		{"update with other state fails and keeps the item", locked, fakeDynamoDBUpdateInput(next(), "VETOED"), true, "old"},
		{"update of a missing item fails and creates nothing", "", fakeDynamoDBUpdateInput(next(), locked), true, ""},
		{"unconditional put overwrites", locked, fakeDynamoDBPutInput(next()), false, "new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDynamoDB()
			if tt.existing != "" {
				fakeDynamoDBSeed(t, f, "v1", tt.existing, "old")
			}

			_, err := f.PutItem(context.Background(), tt.in)

			if gotFailed := fakeDynamoDBIsConditionFailed(err); gotFailed != tt.wantFailed {
				t.Fatalf("condition failed = %v (err = %v), want %v", gotFailed, err, tt.wantFailed)
			}
			if !tt.wantFailed && err != nil {
				t.Fatalf("PutItem: %v", err)
			}
			got := f.rawItem("v1")
			if tt.wantMarker == "" {
				if got != nil || f.itemCount() != 0 {
					t.Fatalf("stored item = %v, count = %d, want none", got, f.itemCount())
				}
				return
			}
			if m := fakeDynamoDBMarker(got); m != tt.wantMarker {
				t.Fatalf("stored marker = %q, want %q", m, tt.wantMarker)
			}
		})
	}
}

func TestFakeDynamoDBGetItem(t *testing.T) {
	f := newFakeDynamoDB()
	fakeDynamoDBSeed(t, f, "v1", "LOCKED_15M", "first")
	fakeDynamoDBSeed(t, f, "v2", "VETOED", "second")

	tests := []struct {
		id         string
		wantMarker string // "" means the item is missing
	}{
		{"v1", "first"},
		{"v2", "second"},
		{"v3", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			out, err := f.GetItem(context.Background(), fakeDynamoDBGetInput(tt.id))
			if err != nil {
				t.Fatalf("GetItem: %v", err)
			}
			if out == nil {
				t.Fatal("GetItem returned a nil output")
			}
			if tt.wantMarker == "" {
				if len(out.Item) != 0 {
					t.Fatalf("Item = %v, want empty for a missing key", out.Item)
				}
				return
			}
			if m := fakeDynamoDBMarker(out.Item); m != tt.wantMarker {
				t.Fatalf("marker = %q, want %q", m, tt.wantMarker)
			}
		})
	}
}

func TestFakeDynamoDBInjectedFailures(t *testing.T) {
	boom := errors.New("boom")

	t.Run("error is returned once and then cleared", func(t *testing.T) {
		tests := []struct {
			name string
			op   string
			err  error
		}{
			{"PutItem arbitrary error", fakeDynamoDBOpPutItem, boom},
			{"PutItem conditional failure", fakeDynamoDBOpPutItem, &types.ConditionalCheckFailedException{}},
			{"GetItem arbitrary error", fakeDynamoDBOpGetItem, boom},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := newFakeDynamoDB()
				fakeDynamoDBSeed(t, f, "v1", "LOCKED_15M", "old")

				f.failNext(tt.op, tt.err)
				if err := fakeDynamoDBCall(f, tt.op); !errors.Is(err, tt.err) {
					t.Fatalf("first call error = %v, want %v", err, tt.err)
				}
				if m := fakeDynamoDBMarker(f.rawItem("v1")); m != "old" {
					t.Fatalf("stored marker after a failed call = %q, want %q", m, "old")
				}
				if err := fakeDynamoDBCall(f, tt.op); err != nil {
					t.Fatalf("second call error = %v, want nil", err)
				}
			})
		}
	})

	t.Run("failNextConditional returns ConditionalCheckFailedException once", func(t *testing.T) {
		f := newFakeDynamoDB()
		f.failNextConditional(fakeDynamoDBOpPutItem)
		if err := fakeDynamoDBCall(f, fakeDynamoDBOpPutItem); !fakeDynamoDBIsConditionFailed(err) {
			t.Fatalf("first call error = %v, want ConditionalCheckFailedException", err)
		}
		if err := fakeDynamoDBCall(f, fakeDynamoDBOpPutItem); err != nil {
			t.Fatalf("second call error = %v, want nil", err)
		}
	})

	t.Run("failOn fails only the Nth call", func(t *testing.T) {
		f := newFakeDynamoDB()
		f.failOn(fakeDynamoDBOpPutItem, 3, boom)
		for call := 1; call <= 4; call++ {
			err := fakeDynamoDBCall(f, fakeDynamoDBOpPutItem)
			if failed := err != nil; failed != (call == 3) {
				t.Fatalf("call %d error = %v, want failure only on call 3", call, err)
			}
		}
	})

	t.Run("a GetItem failure skips PutItem calls, as for the follow-up read of UpdateIf", func(t *testing.T) {
		f := newFakeDynamoDB()
		fakeDynamoDBSeed(t, f, "v1", "LOCKED_15M", "old")
		f.failNext(fakeDynamoDBOpGetItem, boom)

		if err := fakeDynamoDBCall(f, fakeDynamoDBOpPutItem); err != nil {
			t.Fatalf("PutItem error = %v, want nil", err)
		}
		if err := fakeDynamoDBCall(f, fakeDynamoDBOpGetItem); !errors.Is(err, boom) {
			t.Fatalf("GetItem error = %v, want %v", err, boom)
		}
		if err := fakeDynamoDBCall(f, fakeDynamoDBOpGetItem); err != nil {
			t.Fatalf("second GetItem error = %v, want nil", err)
		}
	})
}

// fakeDynamoDBNestedItem returns an item with every container type the clone
// must copy.
func fakeDynamoDBNestedItem() map[string]types.AttributeValue {
	item := fakeDynamoDBItem("v1", "LOCKED_15M", "orig")
	item["map"] = &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
		"s": &types.AttributeValueMemberS{Value: "orig"},
		"l": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberN{Value: "1"}}},
	}}
	item["ss"] = &types.AttributeValueMemberSS{Value: []string{"a", "b"}}
	item["b"] = &types.AttributeValueMemberB{Value: []byte("xyz")}
	return item
}

// fakeDynamoDBVandalize changes every part of item it can reach.
func fakeDynamoDBVandalize(item map[string]types.AttributeValue) {
	item["marker"].(*types.AttributeValueMemberS).Value = "mutated"
	item["extra"] = &types.AttributeValueMemberS{Value: "mutated"}
	nested := item["map"].(*types.AttributeValueMemberM).Value
	nested["s"].(*types.AttributeValueMemberS).Value = "mutated"
	nested["l"].(*types.AttributeValueMemberL).Value[0] = &types.AttributeValueMemberN{Value: "99"}
	item["ss"].(*types.AttributeValueMemberSS).Value[0] = "mutated"
	item["b"].(*types.AttributeValueMemberB).Value[0] = 'Z'
}

func TestFakeDynamoDBIsolatesStoredItems(t *testing.T) {
	want := fakeDynamoDBNestedItem()
	f := newFakeDynamoDB()

	put := fakeDynamoDBNestedItem()
	if _, err := f.PutItem(context.Background(), fakeDynamoDBPutInput(put)); err != nil {
		t.Fatalf("PutItem: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(t *testing.T)
	}{
		{"caller mutates the map it put", func(*testing.T) { fakeDynamoDBVandalize(put) }},
		{"caller mutates the output of GetItem", func(t *testing.T) {
			out, err := f.GetItem(context.Background(), fakeDynamoDBGetInput("v1"))
			if err != nil {
				t.Fatalf("GetItem: %v", err)
			}
			fakeDynamoDBVandalize(out.Item)
		}},
		{"caller mutates the result of rawItem", func(*testing.T) { fakeDynamoDBVandalize(f.rawItem("v1")) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mutate(t)
			if got := f.rawItem("v1"); !reflect.DeepEqual(got, want) {
				t.Fatalf("stored item changed:\n got  %v\n want %v", got, want)
			}
		})
	}
}

func TestFakeDynamoDBRecordsInputs(t *testing.T) {
	ctx := context.Background()
	f := newFakeDynamoDB()
	if f.lastPut != nil || f.lastGet != nil || f.putCalls != 0 || f.getCalls != 0 {
		t.Fatal("a new fake should have recorded nothing")
	}

	create := fakeDynamoDBCreateInput(fakeDynamoDBItem("v1", "LOCKED_15M", "a"))
	if _, err := f.PutItem(ctx, create); err != nil {
		t.Fatalf("create: %v", err)
	}
	if f.lastPut != create || f.putCalls != 1 {
		t.Fatalf("after create: lastPut same = %v, putCalls = %d, want true and 1", f.lastPut == create, f.putCalls)
	}
	if got := aws.ToString(f.lastPut.TableName); got != fakeDynamoDBTestTable {
		t.Errorf("TableName = %q, want %q", got, fakeDynamoDBTestTable)
	}
	if got := aws.ToString(f.lastPut.ConditionExpression); got != createCondition {
		t.Errorf("ConditionExpression = %q, want %q", got, createCondition)
	}

	update := fakeDynamoDBUpdateInput(fakeDynamoDBItem("v1", "VETOED", "b"), "LOCKED_15M")
	f.failNext(fakeDynamoDBOpPutItem, errors.New("boom"))
	if _, err := f.PutItem(ctx, update); err == nil {
		t.Fatal("update: want the injected error")
	}
	if f.lastPut != update || f.putCalls != 2 {
		t.Fatalf("a failed call must still be recorded: lastPut same = %v, putCalls = %d", f.lastPut == update, f.putCalls)
	}
	if got := f.lastPut.ExpressionAttributeNames[fakeDynamoDBNamePlaceholder]; got != attrState {
		t.Errorf("name %s = %q, want %q", fakeDynamoDBNamePlaceholder, got, attrState)
	}
	if got, ok := f.lastPut.ExpressionAttributeValues[fakeDynamoDBValuePlaceholder].(*types.AttributeValueMemberS); !ok || got.Value != "LOCKED_15M" {
		t.Errorf("value %s = %v, want S LOCKED_15M", fakeDynamoDBValuePlaceholder, f.lastPut.ExpressionAttributeValues[fakeDynamoDBValuePlaceholder])
	}

	get := fakeDynamoDBGetInput("v1")
	if _, err := f.GetItem(ctx, get); err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if f.lastGet != get || f.getCalls != 1 {
		t.Fatalf("after get: lastGet same = %v, getCalls = %d, want true and 1", f.lastGet == get, f.getCalls)
	}
	if f.lastGet.ConsistentRead == nil || !*f.lastGet.ConsistentRead {
		t.Error("ConsistentRead was not recorded as true")
	}
	if f.putCalls != 2 {
		t.Errorf("putCalls = %d after a GetItem, want 2", f.putCalls)
	}
}

func TestFakeDynamoDBRejectsUnsupportedRequests(t *testing.T) {
	item := func() map[string]types.AttributeValue { return fakeDynamoDBItem("v1", "LOCKED_15M", "a") }
	expected := map[string]types.AttributeValue{fakeDynamoDBValuePlaceholder: &types.AttributeValueMemberS{Value: "LOCKED_15M"}}
	state := map[string]string{fakeDynamoDBNamePlaceholder: attrState}

	withExpr := func(expr string, names map[string]string, values map[string]types.AttributeValue) func(*fakeDynamoDB) {
		return func(f *fakeDynamoDB) {
			in := fakeDynamoDBPutInput(item())
			in.ConditionExpression = aws.String(expr)
			in.ExpressionAttributeNames = names
			in.ExpressionAttributeValues = values
			_, _ = f.PutItem(context.Background(), in)
		}
	}
	noSK := item()
	delete(noSK, attrSK)
	otherSK := item()
	otherSK[attrSK] = &types.AttributeValueMemberS{Value: "OTHER"}
	noPK := item()
	delete(noPK, attrPK)

	tests := []struct {
		name string
		call func(f *fakeDynamoDB)
	}{
		{"unknown ConditionExpression", withExpr("attribute_exists(PK)", nil, nil)},
		{"update condition without the name placeholder", withExpr(updateCondition, nil, expected)},
		{"update condition without the value placeholder", withExpr(updateCondition, state, nil)},
		{"update condition with a misnamed name placeholder", withExpr(updateCondition,
			map[string]string{"#x": attrState}, expected)},
		{"update condition with a misnamed value placeholder", withExpr(updateCondition, state,
			map[string]types.AttributeValue{":other": &types.AttributeValueMemberS{Value: "LOCKED_15M"}})},
		{"update condition with a non-string expected value", withExpr(updateCondition, state,
			map[string]types.AttributeValue{fakeDynamoDBValuePlaceholder: &types.AttributeValueMemberN{Value: "1"}})},
		{"create condition with unused placeholders", withExpr(createCondition, state, nil)},
		{"unconditional put with placeholders", func(f *fakeDynamoDB) {
			in := fakeDynamoDBPutInput(item())
			in.ExpressionAttributeValues = expected
			_, _ = f.PutItem(context.Background(), in)
		}},
		{"item without PK", func(f *fakeDynamoDB) { _, _ = f.PutItem(context.Background(), fakeDynamoDBPutInput(noPK)) }},
		{"item without SK", func(f *fakeDynamoDB) { _, _ = f.PutItem(context.Background(), fakeDynamoDBPutInput(noSK)) }},
		{"item with SK other than META", func(f *fakeDynamoDB) { _, _ = f.PutItem(context.Background(), fakeDynamoDBPutInput(otherSK)) }},
		{"get key without PK", func(f *fakeDynamoDB) {
			_, _ = f.GetItem(context.Background(), &dynamodb.GetItemInput{Key: map[string]types.AttributeValue{}})
		}},
		{"failure injection for an unknown operation", func(f *fakeDynamoDB) { f.failNext("DeleteItem", errors.New("boom")) }},
		{"failure injection with n below 1", func(f *fakeDynamoDB) { f.failOn(fakeDynamoDBOpPutItem, 0, errors.New("boom")) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDynamoDB()
			fakeDynamoDBMustPanic(t, func() { tt.call(f) })
			// A panic must not leave the mutex held or write anything.
			if n := f.itemCount(); n != 0 {
				t.Fatalf("itemCount = %d after a rejected request, want 0", n)
			}
		})
	}
}

// The fake evaluates the two expressions below by their meaning. If dynamodb.go
// changes either text, update fakeDynamoDBParseCondition first, then this test.
func TestFakeDynamoDBConditionConstantsPinned(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"createCondition", createCondition, "attribute_not_exists(PK)"},
		{"updateCondition", updateCondition, "attribute_exists(PK) AND #s = :expected"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, but the fake evaluates %q; update the fake before changing the expression", tt.name, tt.got, tt.want)
		}
	}
}

func TestFakeDynamoDBConcurrentCreate(t *testing.T) {
	const workers = 50
	f := newFakeDynamoDB()
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			item := fakeDynamoDBItem("v1", "LOCKED_15M", fmt.Sprint(i))
			_, errs[i] = f.PutItem(ctx, fakeDynamoDBCreateInput(item))
			if _, err := f.GetItem(ctx, fakeDynamoDBGetInput("v1")); err != nil {
				t.Errorf("GetItem: %v", err)
			}
		}()
	}
	wg.Wait()

	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case !fakeDynamoDBIsConditionFailed(err):
			t.Errorf("worker %d error = %v, want nil or ConditionalCheckFailedException", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d creates succeeded, want exactly 1", wins)
	}
	if f.putCalls != workers || f.getCalls != workers || f.itemCount() != 1 {
		t.Fatalf("putCalls = %d, getCalls = %d, itemCount = %d, want %d, %d, 1", f.putCalls, f.getCalls, f.itemCount(), workers, workers)
	}
}
