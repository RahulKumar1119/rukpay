package store

import (
	"context"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// benePKPrefix starts the partition key of every Beneficiary_Record item. It
// differs from pkPrefix and histPKPrefix, so a record never shares a partition
// with a Vault_Record or a History_Event whatever the payer key or the id holds
// (Requirement 20.2).
const benePKPrefix = "BENE#"

// Attribute names that replaceCondition compares. They must match the dynamodbav
// tags on BeneficiaryRecord.Version and BeneficiaryRecord.ExpiresAt.
const (
	attrVersion   = "version"
	attrExpiresAt = "expires_at"
)

// Placeholders inside replaceCondition.
const (
	replaceNameVersion   = "#v"
	replaceNameExpires   = "#e"
	replaceValueExpected = ":expected"
	replaceValueNow      = ":now"
)

// beneficiaryPK returns the partition key that holds the records of payer.
func beneficiaryPK(payer string) string { return benePKPrefix + payer }

// beneficiaryItemKey returns the primary key of the item for the record with id
// under payer.
func beneficiaryItemKey(payer, id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		attrPK: &types.AttributeValueMemberS{Value: beneficiaryPK(payer)},
		attrSK: &types.AttributeValueMemberS{Value: beneficiarySortKey(id)},
	}
}

// marshalBeneficiary encodes rec and adds the PK and SK attributes for (payer,
// id). The record's own fields never use the names PK or SK.
func marshalBeneficiary(payer, id string, rec BeneficiaryRecord) (map[string]types.AttributeValue, error) {
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return nil, err
	}
	for name, value := range beneficiaryItemKey(payer, id) {
		item[name] = value
	}
	return item, nil
}

// replaceGuard returns the names and values that replaceCondition needs for the
// version the caller read and the time of this request.
func replaceGuard(expectedVersion int, nowSeconds int64) (map[string]string, map[string]types.AttributeValue) {
	names := map[string]string{
		replaceNameVersion: attrVersion,
		replaceNameExpires: attrExpiresAt,
	}
	values := map[string]types.AttributeValue{
		replaceValueExpected: &types.AttributeValueMemberN{Value: strconv.Itoa(expectedVersion)},
		replaceValueNow:      &types.AttributeValueMemberN{Value: strconv.FormatInt(nowSeconds, 10)},
	}
	return names, values
}

// CreateBeneficiary writes rec as a new item with the store's ExpiresAt. The put
// is conditional on the key not existing, so DynamoDB rejects a duplicate id
// atomically, an expired item included. An unmasked identifier is refused before
// any request. See BeneficiaryStore.
func (s *DynamoDBStore) CreateBeneficiary(ctx context.Context, payer string, rec BeneficiaryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMaskedIdentifier(rec.IdentifierMasked); err != nil {
		return &opError{Op: opCreateBeneficiary, ID: rec.ID, Err: err}
	}
	rec.ExpiresAt = beneficiaryExpiry(s.now())
	item, err := marshalBeneficiary(payer, rec.ID, rec)
	if err != nil {
		return clientError(opCreateBeneficiary, rec.ID, msgEncode, err)
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
		return &opError{Op: opCreateBeneficiary, ID: rec.ID, Err: ErrAlreadyExists}
	default:
		return clientError(opCreateBeneficiary, rec.ID, msgRequest, err)
	}
}

// GetBeneficiary reads the item with a consistent GetItem, so that a write that
// returned nil is visible to the next read, as in MemoryStore. See
// BeneficiaryStore.
func (s *DynamoDBStore) GetBeneficiary(ctx context.Context, payer, id string) (BeneficiaryRecord, error) {
	if err := ctx.Err(); err != nil {
		return BeneficiaryRecord{}, err
	}
	rec, err := s.readBeneficiary(ctx, payer, id)
	if err != nil {
		return BeneficiaryRecord{}, &opError{Op: opGetBeneficiary, ID: id, Err: err}
	}
	return rec, nil
}

// ListBeneficiaries sends one Query per page on the partition of payer, in
// ascending sort key order, and follows LastEvaluatedKey until a page has none.
// It sends no Scan, names no index and sets no Limit, filter or projection.
// Expired records are dropped after each page, because DynamoDB TTL deletion can
// lag. See BeneficiaryStore.
func (s *DynamoDBStore) ListBeneficiaries(ctx context.Context, payer string) ([]BeneficiaryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	records := make([]BeneficiaryRecord, 0)
	var start map[string]types.AttributeValue
	for {
		out, err := s.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(s.table),
			KeyConditionExpression: aws.String(queryByPartition),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				queryPKValue: &types.AttributeValueMemberS{Value: beneficiaryPK(payer)},
			},
			ScanIndexForward:  aws.Bool(true),
			ConsistentRead:    aws.Bool(true),
			ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, clientError(opListBeneficiaries, opNoID, msgRequest, err)
		}

		now := s.now()
		for _, item := range out.Items {
			var rec BeneficiaryRecord
			if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
				return nil, clientError(opListBeneficiaries, opNoID, msgDecode, err)
			}
			if !beneficiaryExpired(rec, now) {
				records = append(records, rec)
			}
		}
		if len(out.LastEvaluatedKey) == 0 {
			return records, nil
		}
		start = out.LastEvaluatedKey
	}
}

// ReplaceBeneficiary writes next with Version expectedVersion + 1 and a new
// ExpiresAt in one PutItem guarded by replaceCondition, so concurrent callers
// with the same expected version cannot both win. When the condition fails, a
// follow-up read chooses ErrNotFound (item absent or expired) or
// ErrConditionFailed (item present at another version), as UpdateIf does. An
// unmasked identifier is refused before any request. See BeneficiaryStore.
func (s *DynamoDBStore) ReplaceBeneficiary(ctx context.Context, payer, id string, expectedVersion int, next BeneficiaryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateMaskedIdentifier(next.IdentifierMasked); err != nil {
		return &opError{Op: opReplaceBeneficiary, ID: id, Err: err}
	}
	now := s.now()
	next.Version = expectedVersion + 1
	next.ExpiresAt = beneficiaryExpiry(now)
	item, err := marshalBeneficiary(payer, id, next)
	if err != nil {
		return clientError(opReplaceBeneficiary, id, msgEncode, err)
	}
	names, values := replaceGuard(expectedVersion, now.Unix())

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 aws.String(s.table),
		Item:                      item,
		ConditionExpression:       aws.String(replaceCondition),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	})
	if err == nil {
		return nil
	}
	if !isConditionFailed(err) {
		return clientError(opReplaceBeneficiary, id, msgRequest, err)
	}
	return s.whyRefused(ctx, opReplaceBeneficiary, payer, id)
}

// DeleteBeneficiary removes the item with one DeleteItem guarded by the same
// replaceCondition, and chooses the error the same way. See BeneficiaryStore.
func (s *DynamoDBStore) DeleteBeneficiary(ctx context.Context, payer, id string, expectedVersion int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	names, values := replaceGuard(expectedVersion, s.now().Unix())

	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:                 aws.String(s.table),
		Key:                       beneficiaryItemKey(payer, id),
		ConditionExpression:       aws.String(replaceCondition),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	})
	if err == nil {
		return nil
	}
	if !isConditionFailed(err) {
		return clientError(opDeleteBeneficiary, id, msgRequest, err)
	}
	return s.whyRefused(ctx, opDeleteBeneficiary, payer, id)
}

// whyRefused turns a failed replaceCondition into an error. It reads the item
// back: an absent or expired item is ErrNotFound, and an item that is there is
// ErrConditionFailed, because its version is not the one the caller read. A
// failed read is returned as the failure of op.
func (s *DynamoDBStore) whyRefused(ctx context.Context, op, payer, id string) error {
	if _, err := s.readBeneficiary(ctx, payer, id); err != nil {
		return &opError{Op: op, ID: id, Err: err}
	}
	return &opError{Op: op, ID: id, Err: ErrConditionFailed}
}

// readBeneficiary fetches and decodes the item for (payer, id). It returns the
// bare sentinel ErrNotFound when the item is absent or expired, and a
// *redactedError for client or decode failures. Callers wrap the result in an
// opError.
func (s *DynamoDBStore) readBeneficiary(ctx context.Context, payer, id string) (BeneficiaryRecord, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.table),
		Key:            beneficiaryItemKey(payer, id),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return BeneficiaryRecord{}, &redactedError{msg: msgRequest, err: err}
	}
	if len(out.Item) == 0 {
		return BeneficiaryRecord{}, ErrNotFound
	}

	var rec BeneficiaryRecord
	if err := attributevalue.UnmarshalMap(out.Item, &rec); err != nil {
		return BeneficiaryRecord{}, &redactedError{msg: msgDecode, err: err}
	}
	if beneficiaryExpired(rec, s.now()) {
		return BeneficiaryRecord{}, ErrNotFound
	}
	return rec, nil
}
