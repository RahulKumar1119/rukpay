package store

import (
	"context"
	"math"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// histPKPrefix starts the partition key of every History_Event item. It differs
// from pkPrefix, so a History_Event never shares a partition with a Vault_Record
// whatever the payer key or the vault id holds (Requirement 13.3).
const histPKPrefix = "HIST#"

// historyPK returns the partition key that holds the events of payer.
func historyPK(payer string) string { return histPKPrefix + payer }

// historyItemKey returns the primary key of the item for the event with time at
// and id under payer.
func historyItemKey(payer string, at int64, id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		attrPK: &types.AttributeValueMemberS{Value: historyPK(payer)},
		attrSK: &types.AttributeValueMemberS{Value: historySortKey(at, id)},
	}
}

// AppendHistory writes ev as its own item under payer. The put is conditional on
// the key not existing, so DynamoDB rejects a duplicate (payer, time, id)
// atomically, and events with different ids never overwrite one another. An
// unmasked reference is refused before any request. See HistoryStore.
func (s *DynamoDBStore) AppendHistory(ctx context.Context, payer string, ev HistoryEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateHistoryEvent(ev); err != nil {
		return &opError{Op: opAppendHistory, ID: ev.ID, Err: err}
	}
	item, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return clientError(opAppendHistory, ev.ID, msgEncode, err)
	}
	for name, value := range historyItemKey(payer, ev.At, ev.ID) {
		item[name] = value
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
		return &opError{Op: opAppendHistory, ID: ev.ID, Err: ErrAlreadyExists}
	default:
		return clientError(opAppendHistory, ev.ID, msgRequest, err)
	}
}

// RecentHistory serves the read with exactly one Query on the partition of
// payer: newest first, at most limit items, strongly consistent, on the base
// table. It sends no Scan, no index name and no filter, and no request at all
// for a limit below 1. It drops expired items after the Query, because
// DynamoDB TTL deletion can lag, and returns the rest oldest first. See
// HistoryStore.
//
// It does not follow LastEvaluatedKey. DynamoDB returns one when it stops at
// Limit, and the events past it are older than the ones wanted. A response page
// holds up to 1 MB, far more than SnapshotLimit events of under 200 bytes.
func (s *DynamoDBStore) RecentHistory(ctx context.Context, payer string, limit int) ([]HistoryEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 {
		return []HistoryEvent{}, nil
	}
	if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}

	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String(queryByPartition),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			queryPKValue: &types.AttributeValueMemberS{Value: historyPK(payer)},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(limit)),
		ConsistentRead:   aws.Bool(true),
	})
	if err != nil {
		return nil, clientError(opRecentHistory, opNoID, msgRequest, err)
	}

	now := s.now()
	events := make([]HistoryEvent, 0, len(out.Items))
	// The items arrive newest first. Walk them backwards for ascending order.
	for i := len(out.Items) - 1; i >= 0; i-- {
		var ev HistoryEvent
		if err := attributevalue.UnmarshalMap(out.Items[i], &ev); err != nil {
			return nil, clientError(opRecentHistory, opNoID, msgDecode, err)
		}
		if !historyExpired(ev, now) {
			events = append(events, ev)
		}
	}
	return events, nil
}
