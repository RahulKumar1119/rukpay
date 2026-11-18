# Requirements Document

## Introduction

The Behavioral Risk Engine is the foundation of RukPay's Go backend. It contains two packages in the module `github.com/<username>/rukpay/backend`: `backend/internal/risk`, which turns five payment signals into a bounded risk score and a vault state, and `backend/internal/store`, which persists vault records behind a `VaultStore` interface. The store has a thread-safe in-memory adapter and a DynamoDB adapter that uses the single table `RukPayVaults`. This feature covers scoring and persistence only. Cooling-off state transitions, HTTP handlers, guardian flows and SSE belong to later specs (the `vault` and `guardian` packages).

## Glossary

- **Risk_Engine**: The Go package `backend/internal/risk` that computes a Risk_Score and an Initial_State from Signals.
- **Signals**: The five scoring inputs: `amount_inr`, `beneficiary_age_hours`, `is_late_night`, `active_unknown_call`, `rapid_retry_count`.
- **Risk_Score**: An integer from 0 to 100 produced by the Risk_Engine.
- **Initial_State**: The Vault_State that the Risk_Engine assigns from a Risk_Score. It is one of INSTANT_PASS, REALITY_CHECK or LOCKED_15M.
- **Vault_State**: One of the string constants INSTANT_PASS, REALITY_CHECK, LOCKED_15M, GUARDIAN_APPROVED, VETOED, EXPIRED.
- **Vault_Record**: The persisted item describing one transaction vault. Its fields are the vault id, Vault_State, Risk_Score, masked identifiers, amount, creation time, lock expiry time, version and `expires_at`.
- **Store**: The Go package `backend/internal/store`.
- **VaultStore**: The Go interface in the Store with the methods `Create`, `Get` and `UpdateIf`.
- **MemoryStore**: The in-memory VaultStore adapter guarded by `sync.RWMutex`.
- **DynamoDBStore**: The VaultStore adapter that persists Vault_Records in the DynamoDB table `RukPayVaults`.
- **DynamoDB_Client**: A small Go interface in the Store that exposes only the DynamoDB operations the DynamoDBStore calls (put item, get item, conditional update item).
- **Fake_DynamoDB_Client**: A test implementation of the DynamoDB_Client that runs in process and can simulate conditional-check failures.
- **Masked_Identifier**: An identifier reduced to the formats in the RukPay privacy rules: `XXXX-XXXX-4821` for accounts, `+91-XXXXXX3210` for phones, `r***@okaxis` for UPI IDs.
- **Known_Contact**: A beneficiary whose `beneficiary_age_hours` is 72 or more.
- **New_Contact**: A beneficiary whose `beneficiary_age_hours` is less than 24.
- **PBT_Suite**: The property-based tests written with `pgregory.net/rapid`.

## Requirements

### Requirement 1: Signal Input Contract

**User Story:** As a backend developer, I want a typed and validated Signals input, so that the scoring function receives well-formed data.

#### Acceptance Criteria

1. THE Risk_Engine SHALL define a Signals type with the fields `amount_inr`, `beneficiary_age_hours`, `is_late_night`, `active_unknown_call` and `rapid_retry_count`, using the JSON names given in the Glossary.
2. IF `amount_inr` is less than or equal to 0, THEN THE Risk_Engine SHALL return a validation error identifying the field `amount_inr`.
3. IF `beneficiary_age_hours` is less than 0, THEN THE Risk_Engine SHALL return a validation error identifying the field `beneficiary_age_hours`.
4. IF `rapid_retry_count` is less than 0, THEN THE Risk_Engine SHALL return a validation error identifying the field `rapid_retry_count`.
5. WHEN Signals are valid, THE Risk_Engine SHALL return a Risk_Score and an Initial_State with no error.

### Requirement 2: Additive Weighted Scoring

**User Story:** As a security designer, I want a transparent additive score, so that each signal's contribution can be explained and tested.

#### Acceptance Criteria

1. THE Risk_Engine SHALL compute the Risk_Score as the sum of the per-signal points defined in criteria 2 to 6, capped at a maximum of 100.
2. THE Risk_Engine SHALL award amount points as follows: 0 points for `amount_inr` below 2000, 10 points for 2000 to 9999, 20 points for 10000 to 24999, and 35 points for 25000 or more.
3. THE Risk_Engine SHALL award beneficiary-age points as follows: 25 points for `beneficiary_age_hours` below 24, 10 points for 24 to 71, and 0 points for 72 or more.
4. WHERE `is_late_night` is true, THE Risk_Engine SHALL award 10 points.
5. WHERE `active_unknown_call` is true, THE Risk_Engine SHALL award 20 points.
6. THE Risk_Engine SHALL award retry points as follows: 0 points for `rapid_retry_count` of 0 or 1, 5 points for 2, and 10 points for 3 or more.
7. THE Risk_Engine SHALL return a Risk_Score that is an integer between 0 and 100 inclusive for every valid Signals value.
8. THE Risk_Engine SHALL return the same Risk_Score for identical Signals on every call, with no dependence on time, randomness or external state.

### Requirement 3: Thresholds and State Mapping

**User Story:** As a payer, I want the amount of friction to match the risk, so that safe payments pass and dangerous ones are held.

#### Acceptance Criteria

1. WHEN the Risk_Score is below 40, THE Risk_Engine SHALL return the Initial_State INSTANT_PASS.
2. WHEN the Risk_Score is from 40 to 74 inclusive, THE Risk_Engine SHALL return the Initial_State REALITY_CHECK.
3. WHEN the Risk_Score is 75 or higher, THE Risk_Engine SHALL return the Initial_State LOCKED_15M.
4. THE Risk_Engine SHALL define the three threshold values 40 and 75 as named constants used by the state mapping.

### Requirement 4: Scoring Guarantees

**User Story:** As a product owner, I want guaranteed outcomes for the clearest scam and clearest safe cases, so that the demo and real behavior are predictable.

#### Acceptance Criteria

1. WHEN `amount_inr` is 25000 or more, `beneficiary_age_hours` is below 24 and `active_unknown_call` is true, THE Risk_Engine SHALL return a Risk_Score of at least 75 and the Initial_State LOCKED_15M, for every value of `is_late_night` and `rapid_retry_count`.
2. WHEN `amount_inr` is below 2000, `is_late_night` is false, the beneficiary is a Known_Contact, `active_unknown_call` is false and `rapid_retry_count` is 0 or 1, THE Risk_Engine SHALL return a Risk_Score below 40 and the Initial_State INSTANT_PASS.
3. WHEN Signals describe ₹450 to a Known_Contact in daytime with no call and no retries, THE Risk_Engine SHALL return the Initial_State INSTANT_PASS.
4. WHEN Signals describe ₹18,000 to a New_Contact in daytime with no call and no retries, THE Risk_Engine SHALL return the Initial_State REALITY_CHECK.
5. WHEN Signals describe ₹85,000 to a New_Contact at late night with an active unknown call, THE Risk_Engine SHALL return the Initial_State LOCKED_15M.

### Requirement 5: Property-Based Verification of Guarantees

**User Story:** As a maintainer, I want randomized tests on the two guarantees, so that no weight change can silently break them.

#### Acceptance Criteria

1. THE PBT_Suite SHALL use `pgregory.net/rapid` to test the lock guarantee in Requirement 4 criterion 1 with at least 1000 generated inputs per run.
2. THE PBT_Suite SHALL use `pgregory.net/rapid` to test the pass guarantee in Requirement 4 criterion 2 with at least 1000 generated inputs per run.
3. THE PBT_Suite SHALL test that the Risk_Score is always between 0 and 100 inclusive across at least 1000 generated valid Signals.
4. THE PBT_Suite SHALL test that the Risk_Score never decreases when `active_unknown_call` changes from false to true with all other Signals unchanged, across at least 1000 generated inputs.
5. THE PBT_Suite SHALL generate `amount_inr`, `beneficiary_age_hours` and `rapid_retry_count` across ranges that include each tier boundary value in Requirement 2.

### Requirement 6: Vault State Constants

**User Story:** As a backend developer, I want one shared set of state constants, so that later packages agree on state names.

#### Acceptance Criteria

1. THE Store SHALL define a Vault_State string type with the constants INSTANT_PASS, REALITY_CHECK, LOCKED_15M, GUARDIAN_APPROVED, VETOED and EXPIRED, each holding its own name as its string value.
2. THE Risk_Engine SHALL use the Store's Vault_State constants, or an identical shared definition, for its Initial_State values.
3. THE Store SHALL leave the validation of transitions between Vault_States to the later `vault` package.

### Requirement 7: VaultStore Interface

**User Story:** As a backend developer, I want a small persistence interface, so that the vault logic can run against memory or DynamoDB.

#### Acceptance Criteria

1. THE VaultStore SHALL declare exactly the methods `Create(ctx, record)`, `Get(ctx, id)` and `UpdateIf(ctx, id, expectedState, newRecord)`.
2. WHEN `Create` is called with a Vault_Record whose id is not yet stored, THE VaultStore SHALL persist the record.
3. IF `Create` is called with an id that is already stored, THEN THE VaultStore SHALL return the error `ErrAlreadyExists` and leave the stored record unchanged.
4. WHEN `Get` is called with a stored id whose `expires_at` has not passed, THE VaultStore SHALL return the stored Vault_Record.
5. IF `Get` is called with an id that is not stored, THEN THE VaultStore SHALL return the error `ErrNotFound`.
6. WHEN `UpdateIf` is called and the stored Vault_State equals `expectedState`, THE VaultStore SHALL replace the stored record with `newRecord` in one atomic operation.
7. IF `UpdateIf` is called and the stored Vault_State differs from `expectedState`, THEN THE VaultStore SHALL return the error `ErrConditionFailed` and leave the stored record unchanged.
8. IF `UpdateIf` is called with an id that is not stored, THEN THE VaultStore SHALL return the error `ErrNotFound`.
9. THE VaultStore SHALL perform persistence only and SHALL accept any Vault_State pair in `UpdateIf` without checking whether the transition is allowed.
10. THE VaultStore SHALL accept a `context.Context` in every method and return the context error when the context is already cancelled.

### Requirement 8: MemoryStore Adapter

**User Story:** As a test author, I want a safe in-memory store, so that unit and property tests run without any cloud dependency.

#### Acceptance Criteria

1. THE MemoryStore SHALL implement the VaultStore interface.
2. THE MemoryStore SHALL guard its data with a `sync.RWMutex`, taking the read lock for `Get` and the write lock for `Create` and `UpdateIf`.
3. WHEN a caller modifies a Vault_Record returned by `Get`, THE MemoryStore SHALL keep the stored record unchanged by returning a copy.
4. WHEN many goroutines call `UpdateIf` on the same id with the same `expectedState`, THE MemoryStore SHALL let exactly one call succeed and return `ErrConditionFailed` to the others.
5. WHEN `STORAGE_DRIVER` is `memory`, THE Store SHALL construct a MemoryStore.
6. THE Store SHALL contain no import of any SQLite package.

### Requirement 9: DynamoDB Item Layout

**User Story:** As a cloud engineer, I want one well-defined table layout, so that persistence works on AWS Free Tier with automatic cleanup.

#### Acceptance Criteria

1. THE DynamoDBStore SHALL write every Vault_Record to the single table named by `DYNAMODB_TABLE_NAME`, with the default name `RukPayVaults`.
2. THE DynamoDBStore SHALL set the partition key `PK` to `VAULT#<id>` and the sort key `SK` to `META` on every item.
3. THE DynamoDBStore SHALL store `expires_at` as a Unix-epoch seconds number so that DynamoDB TTL can remove expired items.
4. WHEN `Get` reads an item whose `expires_at` is earlier than the current time, THE DynamoDBStore SHALL return `ErrNotFound`, since DynamoDB TTL deletion can lag.
5. WHEN `STORAGE_DRIVER` is `dynamodb`, THE Store SHALL construct a DynamoDBStore using the AWS SDK for Go v2 and, WHERE `DYNAMODB_ENDPOINT` is set, that endpoint.
6. THE DynamoDBStore SHALL marshal and unmarshal Vault_Records with `attributevalue` from `github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue`.

### Requirement 10: DynamoDB Conditional Writes

**User Story:** As a cloud engineer, I want race-free updates, so that two concurrent actions on one vault cannot both win.

#### Acceptance Criteria

1. WHEN `Create` runs, THE DynamoDBStore SHALL send a put with the condition that `PK` does not already exist.
2. WHEN `UpdateIf` runs, THE DynamoDBStore SHALL send an update or put with a ConditionExpression requiring that the stored state attribute equals `expectedState` and that the item exists.
3. IF DynamoDB reports a `ConditionalCheckFailedException` for `Create`, THEN THE DynamoDBStore SHALL return `ErrAlreadyExists`.
4. IF DynamoDB reports a `ConditionalCheckFailedException` for `UpdateIf`, THEN THE DynamoDBStore SHALL return `ErrConditionFailed`, or `ErrNotFound` when a follow-up read finds no item.
5. IF the DynamoDB_Client returns any other error, THEN THE DynamoDBStore SHALL return an error that wraps the original error and contains no Vault_Record field values.

### Requirement 11: DynamoDB Testability Without Docker

**User Story:** As a contributor, I want DynamoDB logic tested offline, so that tests run anywhere with `go test`.

#### Acceptance Criteria

1. THE DynamoDBStore SHALL depend only on the DynamoDB_Client interface and not on the concrete AWS SDK client type.
2. THE DynamoDB_Client SHALL expose only the operations that the DynamoDBStore calls.
3. THE Store test suite SHALL include a Fake_DynamoDB_Client that stores items in memory and can be configured to return a `ConditionalCheckFailedException` for a chosen call.
4. THE Store test suite SHALL cover with the Fake_DynamoDB_Client: successful create and get, duplicate create, get of a missing id, get of an expired item, successful conditional update, conditional update failure, update of a missing id, and a non-conditional client error.
5. THE Store test suite SHALL run to completion with `go test ./...` without DynamoDB Local, Docker or network access.
6. THE Store test suite SHALL run the same behavioral tests against both the MemoryStore and the DynamoDBStore backed by the Fake_DynamoDB_Client.

### Requirement 12: Privacy and Redaction

**User Story:** As a privacy-conscious user, I want no sensitive identifiers kept or logged, so that RukPay never becomes a data leak.

#### Acceptance Criteria

1. THE Vault_Record SHALL hold only Masked_Identifiers for the payer account, beneficiary account, UPI ID and phone number, and SHALL have no field for raw values of these identifiers.
2. THE Vault_Record SHALL have no field for an OTP, PIN or password.
3. IF a Vault_Record contains an identifier that does not match a Masked_Identifier format, THEN THE Store SHALL return the error `ErrUnmaskedIdentifier` from `Create` and `UpdateIf` without persisting the record.
4. THE Risk_Engine SHALL accept Signals that contain no account number, UPI ID or phone number.
5. THE Risk_Engine and THE Store SHALL write no Signals or Vault_Record field values to logs.
6. THE error values returned by THE Store SHALL contain the vault id and operation name only, with no identifier or amount values.
