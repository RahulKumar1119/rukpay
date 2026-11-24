package store

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	_ VaultStore     = (*DynamoDBStore)(nil)
	_ HistoryStore   = (*DynamoDBStore)(nil)
	_ DynamoDBClient = (*dynamodb.Client)(nil)
)

// DynamoDBClient is the narrow slice of the DynamoDB API that DynamoDBStore
// calls (Requirements 11.1, 11.2, 13.5). *dynamodb.Client satisfies it, and
// tests supply an in-process fake. It has no Scan method, so the store cannot
// send a Scan request.
type DynamoDBClient interface {
	PutItem(ctx context.Context, in *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, in *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	Query(ctx context.Context, in *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// Item layout and condition expressions (Requirements 9.2, 10.1, 10.2). The
// state attribute name must match the dynamodbav tag on VaultRecord.State.
const (
	attrPK    = "PK"
	attrSK    = "SK"
	attrState = "state"

	pkPrefix = "VAULT#"
	skMeta   = "META"

	createCondition = "attribute_not_exists(PK)"
	updateCondition = "attribute_exists(PK) AND #s = :expected"

	// queryByPartition is the key condition of every Query: all items of one
	// partition. It names no index and no filter (Requirements 13.5, 13.6).
	queryByPartition = "PK = :pk"
	queryPKValue     = ":pk"
)

// Fixed messages for client failures. They never include the cause text,
// because an SDK error can echo request details (Requirements 10.5, 12.6).
const (
	msgRequest = "dynamodb request failed"
	msgEncode  = "dynamodb item encode failed"
	msgDecode  = "dynamodb item decode failed"
)

// DynamoDBStore is a VaultStore that keeps one item per vault in a single
// DynamoDB table (Requirement 9.1). The table has partition key PK, sort key
// SK and a TTL attribute named expires_at.
//
// Use NewDynamoDBStore to construct a DynamoDBStore. The zero value is not
// ready for use.
type DynamoDBStore struct {
	client DynamoDBClient
	table  string
	now    func() time.Time // injectable clock, defaults to time.Now
}

// NewDynamoDBStore returns a DynamoDBStore that reads and writes table through
// client and reads time from time.Now.
func NewDynamoDBStore(client DynamoDBClient, table string) *DynamoDBStore {
	return &DynamoDBStore{
		client: client,
		table:  table,
		now:    time.Now,
	}
}

// Create writes rec as a new item. The put is conditional on PK not existing,
// so DynamoDB rejects a duplicate id atomically. It returns
// ErrUnmaskedIdentifier when an identifier field is not masked and
// ErrAlreadyExists when rec.ID is already stored. Both leave the table
// unchanged.
func (s *DynamoDBStore) Create(ctx context.Context, rec VaultRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMasked(rec); err != nil {
		return &opError{Op: "create", ID: rec.ID, Err: err}
	}
	item, err := marshalItem(rec.ID, rec)
	if err != nil {
		return clientError("create", rec.ID, msgEncode, err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.table),
		Item:                item,
		ConditionExpression: aws.String(createCondition),
	})
	switch {
	case err == nil:
		return nil
	case isConditionFailed(err):
		return &opError{Op: "create", ID: rec.ID, Err: ErrAlreadyExists}
	default:
		return clientError("create", rec.ID, msgRequest, err)
	}
}

// Get returns the record stored under id. It returns ErrNotFound when no item
// exists or when the item's ExpiresAt is non-zero and earlier than the current
// time, because DynamoDB TTL deletion can lag (Requirement 9.4). A zero
// ExpiresAt means the record never expires, as in MemoryStore.
//
// Get reads with ConsistentRead so that a Create or UpdateIf that returned
// nil is visible to the next Get, matching MemoryStore.
func (s *DynamoDBStore) Get(ctx context.Context, id string) (VaultRecord, error) {
	if err := ctx.Err(); err != nil {
		return VaultRecord{}, err
	}
	rec, err := s.read(ctx, id)
	if err != nil {
		return VaultRecord{}, &opError{Op: "get", ID: id, Err: err}
	}
	return rec, nil
}

// UpdateIf replaces the item stored under id with next when the item exists
// and its state equals expected. The check and the write are one conditional
// PutItem, so concurrent callers cannot both win (Requirement 10.2). The item
// is keyed by id, not next.ID, as in MemoryStore. UpdateIf accepts any pair of
// states and never changes next.Version (Requirement 7.9).
//
// It returns ErrUnmaskedIdentifier when next has an unmasked identifier. When
// DynamoDB reports a failed condition, a follow-up read chooses ErrNotFound
// (item absent or expired) or ErrConditionFailed (item present, other state)
// (Requirement 10.4). Every error leaves the stored item unchanged.
func (s *DynamoDBStore) UpdateIf(ctx context.Context, id string, expected VaultState, next VaultRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMasked(next); err != nil {
		return &opError{Op: "update", ID: id, Err: err}
	}
	item, err := marshalItem(id, next)
	if err != nil {
		return clientError("update", id, msgEncode, err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                aws.String(s.table),
		Item:                     item,
		ConditionExpression:      aws.String(updateCondition),
		ExpressionAttributeNames: map[string]string{"#s": attrState},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":expected": &types.AttributeValueMemberS{Value: string(expected)},
		},
	})
	if err == nil {
		return nil
	}
	if !isConditionFailed(err) {
		return clientError("update", id, msgRequest, err)
	}

	// The condition failed because the item is missing or in another state.
	// Read it back to tell the two apart.
	if _, err := s.read(ctx, id); err != nil {
		return &opError{Op: "update", ID: id, Err: err}
	}
	return &opError{Op: "update", ID: id, Err: ErrConditionFailed}
}

// read fetches and decodes the item stored under id. It returns the bare
// sentinel ErrNotFound when the item is absent or expired, and a
// *redactedError for client or decode failures. Callers wrap the result in an
// opError.
func (s *DynamoDBStore) read(ctx context.Context, id string) (VaultRecord, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.table),
		Key:            vaultKey(id),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return VaultRecord{}, &redactedError{msg: msgRequest, err: err}
	}
	if len(out.Item) == 0 {
		return VaultRecord{}, ErrNotFound
	}

	var rec VaultRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return VaultRecord{}, &redactedError{msg: msgDecode, err: err}
	}
	if !rec.ExpiresAt.IsZero() && rec.ExpiresAt.Before(s.now()) {
		return VaultRecord{}, ErrNotFound
	}
	return rec, nil
}

// vaultKey returns the primary key of the item for id.
func vaultKey(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		attrPK: &types.AttributeValueMemberS{Value: pkPrefix + id},
		attrSK: &types.AttributeValueMemberS{Value: skMeta},
	}
}

// marshalItem encodes rec with attributevalue and adds the PK and SK
// attributes for id. The record's own fields never use the names PK or SK.
func marshalItem(id string, rec VaultRecord) (map[string]types.AttributeValue, error) {
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return nil, err
	}
	for name, value := range vaultKey(id) {
		item[name] = value
	}
	return item, nil
}

// isConditionFailed reports whether err is DynamoDB's conditional check
// failure.
func isConditionFailed(err error) bool {
	var failed *types.ConditionalCheckFailedException
	return errors.As(err, &failed)
}

// redactedError replaces a client error's text with a fixed message while
// keeping the original reachable through Unwrap, so errors.Is and errors.As
// still match it. This keeps record values that an SDK error might echo out of
// Error() strings (Requirements 10.5, 12.6). Callers must not format the
// unwrapped error into logs.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() error { return e.err }

// clientError wraps a client or codec failure in an opError whose message has
// only the operation, the vault id and a fixed description.
func clientError(op, id, msg string, cause error) error {
	return &opError{Op: op, ID: id, Err: &redactedError{msg: msg, err: cause}}
}
