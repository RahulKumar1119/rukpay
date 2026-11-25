package store

import (
	"context"
	"errors"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// Environment variables read by NewFromEnv.
const (
	envStorageDriver = "STORAGE_DRIVER"
	envTableName     = "DYNAMODB_TABLE_NAME"
	envEndpoint      = "DYNAMODB_ENDPOINT"
)

// Accepted values of STORAGE_DRIVER.
const (
	driverMemory   = "memory"
	driverDynamoDB = "dynamodb"
)

// defaultTableName is used when DYNAMODB_TABLE_NAME is unset or empty
// (Requirement 9.1). NewDynamoDBStore does not apply it.
const defaultTableName = "RukPayVaults"

// msgAWSConfig is the fixed message for a failed AWS config load. It never
// includes the cause text, because that text can echo profile names, file
// paths or endpoints (Requirement 12.6).
const msgAWSConfig = "aws config load failed"

// NewFromEnv builds the VaultStore selected by the environment.
//
//	STORAGE_DRIVER       "memory" or "dynamodb" (required)
//	DYNAMODB_TABLE_NAME  table for the dynamodb driver (default "RukPayVaults")
//	DYNAMODB_ENDPOINT    optional endpoint override for the dynamodb driver,
//	                     such as http://localhost:8000 for DynamoDB Local
//
// "memory" returns a MemoryStore (Requirement 8.5). "dynamodb" returns a
// DynamoDBStore over an AWS SDK for Go v2 client (Requirement 9.5).
//
// An unset, empty or unknown STORAGE_DRIVER is an error rather than a silent
// fallback to memory, because a deployment that lost its configuration would
// otherwise keep vaults only in process memory and lose them on restart. The
// error names the variable only and never echoes the supplied value, an
// endpoint, a table name or credentials (Requirement 12.6).
//
// Construction makes no network calls. The SDK resolves region and credentials
// lazily on the first request, so the dynamodb driver can be built without AWS
// credentials and the memory driver needs no AWS configuration at all.
func NewFromEnv(ctx context.Context) (VaultStore, error) {
	switch os.Getenv(envStorageDriver) {
	case driverMemory:
		return NewMemoryStore(), nil
	case driverDynamoDB:
		return newDynamoDBFromEnv(ctx)
	default:
		return nil, errors.New("store: unsupported " + envStorageDriver)
	}
}

// Extensions returns the HistoryStore and the BeneficiaryStore that vs also
// implements, so a caller holding the VaultStore that NewFromEnv built can reach
// the other two interfaces without a type assertion of its own. Each result is
// nil when vs does not implement that interface, and a nil vs gives two nils.
// MemoryStore and DynamoDBStore implement both. NewFromEnv itself is unchanged,
// because it returns the VaultStore interface that its callers assert on.
func Extensions(vs VaultStore) (HistoryStore, BeneficiaryStore) {
	history, _ := vs.(HistoryStore)
	beneficiaries, _ := vs.(BeneficiaryStore)
	return history, beneficiaries
}

// newDynamoDBFromEnv builds a DynamoDBStore from the default AWS config chain,
// the table name and the optional endpoint override. It returns the VaultStore
// interface so that a failure yields a nil interface value, not a typed nil.
func newDynamoDBFromEnv(ctx context.Context) (VaultStore, error) {
	table := os.Getenv(envTableName)
	if table == "" {
		table = defaultTableName
	}
	endpoint := os.Getenv(envEndpoint)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, &redactedError{msg: msgAWSConfig, err: err}
	}

	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return NewDynamoDBStore(client, table), nil
}
