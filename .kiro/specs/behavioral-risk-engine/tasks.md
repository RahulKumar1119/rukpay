# Implementation Plan: Behavioral Risk Engine

## Overview

Skills: load `golang-patterns` before any Go work (see `.kiro/steering/skills.md`).

Implementation language: Go 1.21.1 (module `github.com/<username>/rukpay/backend`), as set by the design. The plan builds the `store` core types first (the `risk` package imports `VaultState` from them), then the pure `risk` scorer, then the `MemoryStore`, the `DynamoDBStore` with its factory, and finally a shared contract test suite that runs against both adapters using an in-process fake DynamoDB client. Property tests use `pgregory.net/rapid` with at least 1000 checks each. No SQLite and no logging packages are imported anywhere.

Test files are split per property or topic (not one large `*_pbt_test.go`) so independent test tasks never edit the same file. Each test function name must be unique within its package.

## Tasks

- [x] 1. Set up backend module and dependencies
  - [x] 1.1 Create `backend/go.mod` and package skeletons
    - Create `backend/go.mod` with module `github.com/<username>/rukpay/backend`, Go 1.21.1
    - Add dependencies: `pgregory.net/rapid`, `github.com/aws/aws-sdk-go-v2`, `github.com/aws/aws-sdk-go-v2/config`, `github.com/aws/aws-sdk-go-v2/service/dynamodb`, `github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue`
    - Add `doc.go` files with package comments in `backend/internal/risk` and `backend/internal/store`
    - Do not add any SQLite or logging dependency
    - _Requirements: 8.6, 9.5, 9.6, 12.5_

- [x] 2. Implement store core types, interface and mask validation
  - [x] 2.1 Define `VaultState` constants and `VaultRecord` in `backend/internal/store/types.go`
    - Six string constants (`StateInstantPass`, `StateRealityCheck`, `StateLocked15M`, `StateGuardianApproved`, `StateVetoed`, `StateExpired`), each holding its own name
    - `VaultRecord` with masked-only identifier fields, `dynamodbav` tags, `unixtime` on time fields, and no field for raw identifiers, OTP, PIN or password
    - _Requirements: 6.1, 6.3, 12.1, 12.2_

  - [x] 2.2 Define `VaultStore` interface, sentinel errors and `opError` in `backend/internal/store/store.go`
    - `VaultStore` with exactly `Create`, `Get`, `UpdateIf`
    - `ErrAlreadyExists`, `ErrNotFound`, `ErrConditionFailed`, `ErrUnmaskedIdentifier`
    - `opError` with `Op`, `ID`, `Err`, an `Error()` string of operation and id only, and `Unwrap`
    - _Requirements: 7.1, 7.3, 7.5, 7.7, 7.8, 12.6_

  - [x] 2.3 Implement `ValidateMasked` in `backend/internal/store/mask.go`
    - Regexes for `XXXX-XXXX-\d{4}`, `+91-XXXXXX\d{4}` and `r***@okaxis` style UPI masks
    - Empty identifier fields are allowed; any non-empty field that does not match returns `ErrUnmaskedIdentifier`
    - _Requirements: 12.3, 12.4_

  - [x] 2.4 Write unit tests for state constants and mask validation
    - Files: `backend/internal/store/types_test.go`, `backend/internal/store/mask_test.go`
    - Constants equal their own names; valid masks pass; raw account, phone and UPI values are rejected; empty fields pass
    - _Requirements: 6.1, 12.1, 12.3, 12.4_

- [x] 3. Implement the risk scoring engine
  - [x] 3.1 Implement `backend/internal/risk/risk.go`
    - Constants `RealityCheckThreshold = 40`, `LockThreshold = 75`, `MaxScore = 100`
    - `Signals` with the five JSON-tagged fields, `ValidationError`, `Result`
    - `Validate` (checks amount, age, retry in that order), `amountPoints`, `agePoints`, `retryPoints`, `Score` (capped sum), `MapState` (uses `store.VaultState`), `Evaluate`
    - No time, randomness, logging or external state
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 3.1, 3.2, 3.3, 3.4, 6.2, 12.4, 12.5_

  - [x] 3.2 Write unit tests for the risk engine
    - File: `backend/internal/risk/risk_test.go`
    - Demo presets: ₹450 known contact is INSTANT_PASS, ₹18,000 new contact is REALITY_CHECK, ₹85,000 new contact late night on a call is LOCKED_15M
    - Tier boundary examples (1999/2000, 9999/10000, 24999/25000, 23/24, 71/72, retry 1/2/3), score 39/40 and 74/75 state mapping, validation error field names
    - _Requirements: 1.2, 1.3, 1.4, 2.2, 2.3, 2.6, 3.1, 3.2, 3.3, 4.3, 4.4, 4.5_

  - [x] 3.3 Write property test for invalid signal rejection
    - File: `backend/internal/risk/pbt_invalid_test.go`
    - **Property 1: Invalid signals are rejected with the offending field**
    - **Validates: Requirements 1.2, 1.3, 1.4**

  - [x] 3.4 Write property test for reference-model scoring and range
    - File: `backend/internal/risk/pbt_score_test.go`
    - **Property 2: Score equals the reference model and stays in range**
    - Independent reference implementation in the test; generators include every tier boundary value
    - **Validates: Requirements 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 5.3, 5.5**

  - [x] 3.5 Write property test for determinism
    - File: `backend/internal/risk/pbt_determinism_test.go`
    - **Property 3: Scoring is deterministic**
    - **Validates: Requirements 2.8**

  - [x] 3.6 Write property test for state mapping thresholds
    - File: `backend/internal/risk/pbt_mapstate_test.go`
    - **Property 4: State mapping follows the thresholds**
    - **Validates: Requirements 3.1, 3.2, 3.3**

  - [x] 3.7 Write property test for the lock guarantee
    - File: `backend/internal/risk/pbt_lock_test.go`
    - **Property 5: Lock guarantee**
    - At least 1000 generated inputs
    - **Validates: Requirements 4.1, 5.1**

  - [x] 3.8 Write property test for the pass guarantee
    - File: `backend/internal/risk/pbt_pass_test.go`
    - **Property 6: Pass guarantee**
    - At least 1000 generated inputs
    - **Validates: Requirements 4.2, 5.2**

  - [x] 3.9 Write property test for call monotonicity
    - File: `backend/internal/risk/pbt_monotonic_test.go`
    - **Property 7: Unknown call never lowers the score**
    - At least 1000 generated inputs
    - **Validates: Requirements 5.4**

- [x] 4. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Implement MemoryStore
  - [x] 5.1 Implement `backend/internal/store/memory.go`
    - `MemoryStore` with `sync.RWMutex`, map storage and an injectable `now` clock
    - Every method returns `ctx.Err()` first when the context is cancelled
    - `Create` (write lock, `ErrAlreadyExists`), `Get` (read lock, `ErrNotFound` for missing or expired, returns a copy), `UpdateIf` (write lock, `ErrNotFound`, `ErrConditionFailed`, replace)
    - `Create` and `UpdateIf` call `ValidateMasked` before writing
    - Wrap all returned sentinels in `opError` (operation and id only)
    - _Requirements: 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9, 7.10, 8.1, 8.2, 8.3, 8.4, 12.3, 12.5, 12.6_

- [x] 6. Implement DynamoDBStore and factory
  - [x] 6.1 Implement `backend/internal/store/dynamodb.go`
    - `DynamoDBClient` interface with only `PutItem` and `GetItem`; `DynamoDBStore` with client, table, `now`; `NewDynamoDBStore`
    - Items use `PK = VAULT#<id>`, `SK = META`, `attributevalue.MarshalMap`, and `expires_at` as epoch-seconds Number
    - `Create`: `PutItem` with `attribute_not_exists(PK)`; conditional failure maps to `ErrAlreadyExists`
    - `Get`: `GetItem`; empty item or `ExpiresAt` before `now()` maps to `ErrNotFound`
    - `UpdateIf`: `PutItem` with `attribute_exists(PK) AND #s = :expected`; on `ConditionalCheckFailedException` do a follow-up `GetItem` to choose `ErrNotFound` or `ErrConditionFailed`
    - Other client errors become an `opError` with a fixed message that still unwraps to the original
    - Check `ctx.Err()` first and call `ValidateMasked` before any write
    - _Requirements: 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9, 7.10, 9.1, 9.2, 9.3, 9.4, 9.6, 10.1, 10.2, 10.3, 10.4, 10.5, 11.1, 11.2, 12.3, 12.6_

  - [x] 6.2 Implement `NewFromEnv` in `backend/internal/store/factory.go`
    - Read `STORAGE_DRIVER` (`memory` or `dynamodb`), `DYNAMODB_TABLE_NAME` (default `RukPayVaults`), `DYNAMODB_ENDPOINT` (optional `BaseEndpoint`)
    - Build the AWS SDK v2 client via `config.LoadDefaultConfig` and `dynamodb.NewFromConfig`
    - Unknown driver returns an error that names the variable only
    - _Requirements: 8.5, 9.1, 9.5_

  - [x] 6.3 Write the fake DynamoDB client in `backend/internal/store/fake_dynamodb_test.go`
    - In-memory item map implementing `DynamoDBClient`
    - Evaluates `attribute_not_exists(PK)` and `attribute_exists(PK) AND #s = :expected`
    - Can be configured to fail a chosen call with `ConditionalCheckFailedException` or an arbitrary error; records the last input
    - _Requirements: 11.3, 11.5_

- [ ] 7. Shared contract tests for both adapters
  - [ ]* 7.1 Write the contract harness in `backend/internal/store/contract_test.go`
    - Helper that runs a test body once with `MemoryStore` and once with `DynamoDBStore` over the fake client, each with a controllable clock
    - Rapid generators for valid `VaultRecord` values with masked identifiers
    - _Requirements: 11.5, 11.6_

  - [ ]* 7.2 Write property test for Create/Get round trip
    - File: `backend/internal/store/pbt_roundtrip_test.go`
    - **Property 8: Create then Get round trip**
    - **Validates: Requirements 7.2, 7.4, 9.6**

  - [ ]* 7.3 Write property test for duplicate Create
    - File: `backend/internal/store/pbt_duplicate_test.go`
    - **Property 9: Duplicate Create is rejected and leaves the record unchanged**
    - **Validates: Requirements 7.3, 10.3**

  - [ ]* 7.4 Write property test for UpdateIf compare-and-swap
    - File: `backend/internal/store/pbt_updateif_test.go`
    - **Property 10: UpdateIf is a compare-and-swap on state**
    - Include state pairs that are not legal transitions
    - **Validates: Requirements 7.6, 7.7, 7.9, 10.4**

  - [ ]* 7.5 Write property test for returned copies
    - File: `backend/internal/store/pbt_copy_test.go`
    - **Property 11: Returned records are copies**
    - **Validates: Requirements 8.3**

  - [ ]* 7.6 Write property test for expired records
    - File: `backend/internal/store/pbt_expired_test.go`
    - **Property 12: Expired records are not found**
    - **Validates: Requirements 7.4, 9.4**

  - [ ]* 7.7 Write property test for unmasked identifiers
    - File: `backend/internal/store/pbt_unmasked_test.go`
    - **Property 14: Unmasked identifiers are never persisted**
    - **Validates: Requirements 12.3**

  - [ ]* 7.8 Write property test for DynamoDB item layout
    - File: `backend/internal/store/pbt_layout_test.go`
    - **Property 13: DynamoDB item layout**
    - DynamoDB adapter only, inspecting the fake client's stored item
    - **Validates: Requirements 9.2, 9.3**

  - [ ]* 7.9 Write property test for error wrapping and non-leakage
    - File: `backend/internal/store/pbt_errors_test.go`
    - **Property 15: Errors wrap the cause and leak no record values**
    - DynamoDB adapter only, with an injected non-conditional client error
    - **Validates: Requirements 10.5, 12.6**

  - [ ]* 7.10 Write unit tests for DynamoDB and factory examples
    - File: `backend/internal/store/dynamodb_test.go`
    - Duplicate create, missing get, expired get, successful and failed conditional update, update of a missing id (conditional failure then empty read gives `ErrNotFound`), non-conditional client error, `ConditionExpression` strings, cancelled context on every method, default table name `RukPayVaults`, `NewFromEnv` driver selection and unknown-driver error
    - _Requirements: 7.5, 7.8, 7.10, 8.5, 9.1, 9.4, 9.5, 10.1, 10.2, 10.3, 10.4, 10.5, 11.4_

  - [ ]* 7.11 Write a concurrency test for MemoryStore
    - File: `backend/internal/store/memory_concurrency_test.go`
    - 50 goroutines call `UpdateIf` on one id with the same expected state; exactly one succeeds and the rest get `ErrConditionFailed` (run with `go test -race`)
    - _Requirements: 8.2, 8.4_

  - [ ]* 7.12 Write smoke tests for forbidden imports
    - File: `backend/internal/store/imports_test.go`
    - Parse package imports with `go/parser` for `store` and `risk`; assert no SQLite package and no `log`, `log/slog` or third-party logger import
    - _Requirements: 8.6, 12.5_

- [ ] 8. Verify infrastructure schema
  - [ ] 8.1 Create or confirm `infra/dynamodb-table.json`
    - Table `RukPayVaults`: partition key `PK` (S), sort key `SK` (S), on-demand billing, TTL attribute `expires_at`
    - Leave the file unchanged if it already matches
    - _Requirements: 9.1, 9.2, 9.3_

- [ ] 9. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP
- Each task references specific requirements for traceability
- The design text for Requirement 3.4 says "three threshold values" but only two exist (40 and 75); the plan implements two named constants
- Property tests run at least 1000 checks and carry a comment tag `// Feature: behavioral-risk-engine, Property N: <title>`
- Requirement 5 describes the PBT suite itself and is covered by tasks 3.4, 3.7, 3.8 and 3.9
- Run `go test -race ./...` from `backend/` for the concurrency test

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1", "8.1"] },
    { "id": 2, "tasks": ["2.2", "2.3", "3.1"] },
    { "id": 3, "tasks": ["2.4", "3.2", "3.3", "3.4", "3.5", "3.6", "3.7", "3.8", "3.9", "5.1", "6.1"] },
    { "id": 4, "tasks": ["6.2", "6.3"] },
    { "id": 5, "tasks": ["7.1", "7.9", "7.10", "7.8", "7.11", "7.12"] },
    { "id": 6, "tasks": ["7.2", "7.3", "7.4", "7.5", "7.6", "7.7"] }
  ]
}
```
