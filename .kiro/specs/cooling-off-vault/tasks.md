# Implementation Plan: Cooling-Off Vault

## Overview

Skills: load `golang-patterns` before any Go work (see `.kiro/steering/skills.md`).

Implementation language is Go 1.21.1 (module `github.com/<username>/rukpay/backend`), as fixed by the design. The plan starts with the store changes this feature needs from `behavioral-risk-engine` (new `VaultRecord` fields and `NewMemoryStoreWithClock`). It then builds `vault` (rules, lazy expiry, operations), `guardian` (token, evidence, hub, approve/veto), and `httpapi` (config, middleware, router), and ends with the two entrypoints. Dependency direction stays `httpapi` → `guardian` → `vault` → `store`, `risk`.

Property tests use `pgregory.net/rapid` with at least 1000 checks each, run under `go test ./...` with `MemoryStore`, an injected `Clock` and `httptest`. Test files are split per property or topic so independent test tasks never edit the same file. Each test function name must be unique within its package, and each property test carries the comment `// Feature: cooling-off-vault, Property N: <title>`.

## Tasks

- [x] 1. Update the behavioral-risk-engine store for the vault feature
  - [x] 1.1 Replace `LockExpiresAt` with the new `VaultRecord` fields in `backend/internal/store/types.go`
    - Remove `LockExpiresAt time.Time`
    - Add `LockDeadline int64` (`dynamodbav:"lock_deadline"`), `TokenExpiry int64` (`token_expiry`), `Settled bool` (`settled`), `Reason string` (`reason`), `EvidenceSummary string` (`evidence,omitempty`)
    - Keep all identifier fields masked-only; add no field for raw identifiers, OTP, PIN or password
    - Fix any other file in `store` or `risk` that still references `LockExpiresAt`
    - _Requirements: 1.4, 1.8, 4.1, 4.5, 6.4, 9.5_

  - [x] 1.2 Add `NewMemoryStoreWithClock(now func() time.Time)` in `backend/internal/store/memory.go`
    - `NewMemoryStore()` delegates to it with `time.Now`
    - The injected clock drives record-expiry checks in `Get`, so vault tests control record expiry and lock expiry from one fake clock
    - No new `VaultStore` method is added
    - _Requirements: 4.7, 12.11_

  - [x] 1.3 Update the store tests for the new fields
    - Files: `backend/internal/store/contract_test.go`, `backend/internal/store/fake_dynamodb_test.go`, `backend/internal/store/pbt_roundtrip_test.go`, `backend/internal/store/pbt_layout_test.go`, `backend/internal/store/types_test.go`
    - Rapid generators produce the new fields (including zero `LockDeadline`); round trip equality and the DynamoDB item layout cover `lock_deadline`, `token_expiry`, `settled`, `reason`, `evidence`
    - Add a unit test that `NewMemoryStoreWithClock` expires records by the injected clock
    - _Requirements: 1.8, 4.7, 12.11_

- [x] 2. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 3. Implement shared redaction and the `vault` package
  - [x] 3.1 Implement `backend/internal/redact/redact.go`
    - `redact.Text(s string) string` replaces matches of `(?i)\b(otp|pin|password|passcode)\b\s*[:=]?\s*\S+` with `[REDACTED]`
    - Lives in its own package so `vault`, `guardian` and `httpapi` can all use it without an import cycle
    - _Requirements: 11.3, 6.8, 8.9_

  - [x] 3.2 Implement transitions and answers in `backend/internal/vault/transitions.go` and `backend/internal/vault/answers.go`
    - `allowed` table and `CanTransition(from, to)` permitting only RC→LOCKED, LOCKED→APPROVED, LOCKED→VETOED, LOCKED→EXPIRED
    - `Answer`, `Answers`, `ErrInvalidAnswers`, and `CheckAnswers` returning `lock = coercion==YES || secrecy==YES || verification==NO`
    - `ErrInvalidTransition`, `ErrNotDemoMode`, `ConflictError{Current View}`
    - _Requirements: 2.1, 2.3, 2.4, 3.2, 3.3, 3.4, 3.5, 3.7_

  - [x] 3.3 Implement calm copy in `backend/internal/vault/messages.go`
    - All user-facing strings as constants, including the protective-pause sentence stating the 15-minute hold
    - `reasonFor(state, signals)` builds the reason from true Signals only, using plain phrases and no banned phrases or technical terms
    - Pass every reason through `redact.Text`
    - _Requirements: 11.3, 11.4, 11.5_

  - [x] 3.4 Implement `View` and `Metadata` in `backend/internal/vault/view.go`
    - `View` JSON fields per design: `vault_id`, `state`, `settled`, `risk_score`, `remaining_seconds`, `reconfirm_required`, `reason`, `metadata`, `guardian_token` (LOCKED_15M only), `evidence_summary` (VETOED only)
    - `view(rec, now)` computes `remaining_seconds = max(0, LockDeadline - now)` only while LOCKED, recomputes the token with `issuer.Issue(id, rec.TokenExpiry)` for LOCKED_15M only, and sets `reconfirm_required` when EXPIRED and not settled
    - Define `Clock`, `TokenIssuer` and `Observer` interfaces
    - _Requirements: 4.1, 4.4, 7.1, 7.2, 7.3, 8.9, 5.9_

  - [x] 3.5 Implement `Service` in `backend/internal/vault/vault.go`
    - `New(store, clock, demo, issuer, observer)` plus a settable evidence-builder function field so `vault` never imports `guardian`
    - `load` with lazy expiry via `UpdateIf(LOCKED_15M → EXPIRED)`, re-reading on `ErrConditionFailed`, publishing on success
    - `apply` returning `*ConflictError` on wrong state or lost race and `ErrInvalidTransition` before any write; bumps `Version`; publishes after a successful write
    - `Evaluate` (validates with `store.ValidateMasked` first, `crypto/rand` 16-byte hex id, `LockDeadline = TokenExpiry = created + 900`, `ExpiresAt >= LockDeadline + 3600`), `Get`, `RealityCheck`, `Reconfirm`, `Approve`, `Veto`, `FastForward` (`min(LockDeadline, now+3)`, `TokenExpiry` untouched, `ErrNotDemoMode` when demo is off)
    - No `go func`, `time.AfterFunc`, `log` or SQLite import in this package
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.7, 1.8, 2.2, 2.5, 2.6, 2.7, 3.1, 3.6, 3.8, 3.9, 4.2, 4.3, 4.5, 4.6, 4.7, 6.1, 6.3, 7.5, 9.1, 9.3, 9.4, 9.5, 9.6, 11.7_

  - [x] 3.6 Write unit tests for the vault service
    - File: `backend/internal/vault/vault_test.go`
    - Demo preset flows (₹450 → INSTANT_PASS, ₹18,000 → REALITY_CHECK, ₹85,000 → LOCKED_15M with token), vault id format (32 hex chars, no collisions across 10,000 ids), cancelled context writes nothing, lazy-expiry race re-read, reality-check reject of an already-settled vault
    - _Requirements: 1.1, 1.5, 2.6, 3.8, 4.2_

  - [x] 3.7 Write property test for the transition table
    - File: `backend/internal/vault/pbt_transitions_test.go`
    - **Property 3: Transition table is exact**
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.4**

  - [x] 3.8 Write property test for creation matching the risk engine
    - File: `backend/internal/vault/pbt_creation_test.go`
    - **Property 1: Creation matches the risk engine**
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.8**

  - [x] 3.9 Write property tests for reality-check outcomes and bad requests
    - File: `backend/internal/vault/pbt_realitycheck_test.go`
    - **Property 6: Reality check outcome** (all 8 answer combinations plus random clock)
    - **Property 7: Bad reality-check requests change nothing**
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8**

  - [x] 3.10 Write property tests for lazy expiry and reconfirmation
    - File: `backend/internal/vault/pbt_expiry_test.go`
    - **Property 8: Lazy expiry follows the clock** (offsets before, at and after `lock_deadline`)
    - **Property 9: Reconfirmation settles only an expired vault**
    - **Validates: Requirements 4.1, 4.2, 4.4, 4.5, 4.6, 7.5**

  - [x] 3.11 Write property tests for operation sequences
    - File: `backend/internal/vault/pbt_sequences_test.go`
    - **Property 4: Operation sequences only make permitted state changes** (model tracks expected state and `Settled`)
    - **Property 10: Settled only where permitted**
    - **Validates: Requirements 2.1, 2.4, 12.2, 12.3, 12.4**

  - [x] 3.12 Write property test for concurrent callers
    - File: `backend/internal/vault/pbt_concurrency_test.go`
    - **Property 5: Concurrent callers produce exactly one winner** (at least 50 goroutines, run with `-race`)
    - **Validates: Requirements 2.5, 2.6, 2.7, 12.9**

  - [x] 3.13 Write property test for fast-forward
    - File: `backend/internal/vault/pbt_fastforward_test.go`
    - **Property 18: Fast-forward only shortens the lock** (service level: `ErrNotDemoMode` with demo off, 409 for non-locked)
    - **Validates: Requirements 9.1, 9.3, 9.4, 9.5, 9.6, 12.8**

  - [x] 3.14 Write property test for the read view
    - File: `backend/internal/vault/pbt_view_test.go`
    - **Property 17: Read view is safe and complete** (token only in LOCKED_15M, evidence only in VETOED, no key, OTP, PIN or password in any field)
    - **Validates: Requirements 5.9, 7.1, 7.2, 7.3, 8.9**

  - [x] 3.15 Write property test for calm and redacted messages
    - File: `backend/internal/vault/pbt_messages_test.go`
    - **Property 20: Messages are calm and redacted**
    - **Validates: Requirements 11.3, 11.4, 11.5**

- [x] 4. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Implement the `guardian` package
  - [ ] 5.1 Implement `Signer` in `backend/internal/guardian/token.go`
    - `NewSigner`, `Issue` (`<id>.<expiry>.<hex HMAC-SHA256(key, id+"."+expiry)>`), `Verify(token, pathID, now)`
    - `Verify` order: exactly three parts, recompute tag, `hmac.Equal`, `id == pathID`, `expiry > now`; every failure returns the single `ErrForbidden`
    - Never expose the key in errors
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.9_

  - [ ] 5.2 Implement `BuildEvidence` in `backend/internal/guardian/evidence.go`
    - Lists vault id, masked identifiers, amount, Risk_Score, creation time, veto time, `National Cybercrime Helpline: 1930` and `Report online: cybercrime.gov.in`
    - Reads only `VaultRecord` fields and passes the result through `redact.Text`
    - _Requirements: 6.3, 6.4, 6.8_

  - [ ] 5.3 Implement the SSE `Hub` in `backend/internal/guardian/hub.go`
    - `Subscribe(id)` returns a buffered channel (capacity 8) and `cancel`; `Publish(vault.View)` marshals once without the Guardian_Token and sends non-blocking per subscriber; `cancel` removes and closes the channel and deletes empty map entries
    - Implements `vault.Observer`
    - _Requirements: 8.2, 8.3, 8.8, 8.9_

  - [ ] 5.4 Implement guardian `Service` in `backend/internal/guardian/service.go`
    - `Action`, `Result`, `ErrInvalidAction`; `Act` verifies the token first (no store call on failure), then handles unknown action (400), APPROVE unconfirmed (`CONFIRMATION_REQUIRED`, no write), APPROVE confirmed (`vault.Approve`), VETO (`vault.Veto`)
    - Wire `BuildEvidence` into the vault service's evidence-builder field
    - _Requirements: 5.3, 5.4, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7_

  - [ ]* 5.5 Write property tests for token round trip and tampering
    - File: `backend/internal/guardian/pbt_token_test.go`
    - **Property 12: Guardian token round trip**
    - **Property 13: Any tampering breaks the token** (single-byte change, other vault id, other key, wrong part count; identical failure result; no `UpdateIf` call on failure)
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 12.5, 12.6**

  - [ ]* 5.6 Write property tests for approve, veto and refused actions
    - File: `backend/internal/guardian/pbt_actions_test.go`
    - **Property 14: Approval needs a confirmed second tap**
    - **Property 15: Veto cancels and produces masked evidence**
    - **Property 16: Guardian actions on non-locked or expired vaults are refused**
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.8, 12.10**

  - [ ]* 5.7 Write property test for the hub
    - File: `backend/internal/guardian/pbt_hub_test.go`
    - **Property 19: Hub never blocks and cleans up**
    - **Validates: Requirements 8.2, 8.3, 8.8**

  - [ ]* 5.8 Write unit tests for evidence text
    - File: `backend/internal/guardian/evidence_test.go`
    - Contains vault id, `1930`, `cybercrime.gov.in`; an OTP/PIN pattern in input becomes `[REDACTED]`
    - _Requirements: 6.4, 6.8_

- [ ] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Implement the `httpapi` package and entrypoints
  - [ ] 7.1 Implement config in `backend/internal/httpapi/config.go`
    - `Config`, `LoadConfig(getenv)`: defaults `ALLOWED_ORIGIN=http://localhost:5173`, `PORT=8080`, `STORAGE_DRIVER=memory`; `OnLambda` from `AWS_LAMBDA_FUNCTION_NAME`; `DemoMode` from `DEMO_MODE=="true"`
    - Error when `HMAC_SECRET_KEY` is empty and not Demo_Mode; in Demo_Mode generate 32 random bytes; error text never contains the key
    - _Requirements: 5.8, 5.9, 10.2, 10.4, 10.9_

  - [ ] 7.2 Implement errors and middleware in `backend/internal/httpapi/errors.go` and `backend/internal/httpapi/middleware.go`
    - One error body shape `{error:{code,message,field?}}` with the status table from the design (400 `invalid_field`/`invalid_identifier`/`invalid_request`, 403 identical `forbidden`, 404, 409 with current View, 413, 500 fixed message)
    - Middleware chain `logging` → `cors` → `bodyLimit` (64 KiB, 413); logging writes method, route pattern, status and vault id only; CORS echoes the origin only when it equals `AllowedOrigin` and answers `OPTIONS`
    - Calm plain-language messages; no store error text, stack text or submitted values
    - _Requirements: 10.7, 10.8, 10.9, 10.10, 10.11, 11.1, 11.2, 11.6, 5.4, 5.7_

  - [ ] 7.3 Implement the router and `Build` in `backend/internal/httpapi/router.go`
    - Register the six method/path routes through the in-repo dispatcher (`route.go`), not Go 1.22 `ServeMux` patterns
    - `evaluate` maps validation errors to 400 with the field name; `realityCheck` decodes `{answers?, reconfirm?}` with pointer fields, accepts exactly one (both, neither or `reconfirm:false` is 400) and dispatches to `RealityCheck` or `Reconfirm`; `guardianAction`, `getVault`, `fastForward` (404 when demo is off, before reading the vault)
    - `stream`: 400 missing `vault_id`, 404 unknown id, event-stream headers with `X-Accel-Buffering: no`, current View first, hub subscription, `: keep-alive` every 15 s (interval injectable), 25 s limit when `OnLambda`
    - `Build(ctx, cfg)` wires `store.NewFromEnv`, `Signer`, `Hub`, `vault.Service`, `guardian.Service`
    - _Requirements: 2.6, 3.1, 3.7, 3.8, 4.5, 4.6, 6.5, 6.6, 7.1, 7.4, 8.1, 8.2, 8.4, 8.5, 8.6, 8.7, 9.2, 10.1, 10.5, 1.6, 1.7_

  - [ ] 7.4 Implement the entrypoints and dependencies
    - Files: `backend/cmd/server/main.go`, `backend/cmd/lambda/main.go`, `backend/go.mod`
    - Server: `LoadConfig` then `Build`; print the error and exit non-zero on failure; run `lambda.Start(httpadapter.NewV2(h).ProxyWithContext)` when `OnLambda`, otherwise `http.ListenAndServe(":"+Port, h)`
    - Lambda entrypoint builds the same router and calls `lambda.Start` only (built with `-tags lambda.norpc`)
    - Add `github.com/aws/aws-lambda-go` and `github.com/awslabs/aws-lambda-go-api-proxy` to `go.mod`
    - _Requirements: 5.8, 10.2, 10.3, 10.4, 10.5, 10.6_

  - [ ]* 7.5 Write property tests for request validation and reconfirm payload shape
    - File: `backend/internal/httpapi/pbt_requests_test.go`
    - **Property 2: Invalid input never creates a vault** (invalid Signals or unmasked identifiers give 400 and no store record)
    - **Property 11: Reconfirm payload shape**
    - **Validates: Requirements 1.6, 1.7, 3.7, 4.5**

  - [ ]* 7.6 Write example and integration tests for the router
    - File: `backend/internal/httpapi/httpapi_test.go`
    - Route registration for all six patterns; end-to-end flows (locked → approve with second tap; locked → veto with evidence; locked → fast-forward → EXPIRED → reconfirm through `reality-check`); 404 on missing id for GET and stream, 400 on missing `vault_id`; CORS (allowed, other origin, preflight, default origin); 413 over 64 KiB; 400 on invalid JSON; 403 body identical for every token failure; error bodies carry code and message only
    - SSE: first event is current View, headers, `: keep-alive` with shortened interval, disconnect removes subscription, Lambda flag closes the stream at the injected limit
    - Logging: output after a request with a distinctive amount and masked UPI holds only method, route, status and vault id
    - _Requirements: 6.1, 6.3, 7.4, 8.1, 8.3, 8.4, 8.5, 8.6, 8.7, 10.1, 10.7, 10.8, 10.9, 10.10, 10.11, 11.1, 11.2, 11.6_

  - [ ]* 7.7 Write unit tests for startup config
    - File: `backend/internal/httpapi/config_test.go`
    - Empty `HMAC_SECRET_KEY` returns an error without the key in the text; Demo_Mode with no key generates one; defaults for origin, port and driver; `OnLambda` detection
    - _Requirements: 5.8, 5.9, 10.2, 10.4, 10.9_

  - [ ]* 7.8 Write smoke tests for forbidden imports and constructs
    - File: `backend/internal/vault/imports_test.go`
    - Parse imports with `go/parser` for `vault`, `guardian` and `httpapi`: no SQLite package; `vault` has no `log` import and no `go` statement or `time.AfterFunc`
    - _Requirements: 4.3, 11.7_

- [ ] 8. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP.
- Task 1 edits the `behavioral-risk-engine` store (`VaultRecord` and `NewMemoryStoreWithClock`). It must land before any `vault` code, and the already-planned store tests from that spec need the matching update in 1.3.
- The design lists `redact.Text` under `httpapi/redact.go`, but `vault` and `guardian` also need it and must not import `httpapi`. Task 3.1 places it in `internal/redact` to keep the dependency direction `httpapi` → `guardian` → `vault` → `store`.
- Each task references specific requirements for traceability; property tasks cite the design property number.
- Property tests validate universal correctness properties; unit and example tests validate specific cases.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "3.1", "3.2", "5.1"] },
    { "id": 1, "tasks": ["1.2", "3.3", "3.4", "5.2", "7.1", "3.7", "5.5"] },
    { "id": 2, "tasks": ["1.3", "3.5", "3.15", "5.3"] },
    { "id": 3, "tasks": ["3.6", "3.8", "3.9", "3.10", "3.11", "3.12", "3.13", "3.14", "5.4", "5.7", "5.8"] },
    { "id": 4, "tasks": ["5.6", "7.2", "7.7"] },
    { "id": 5, "tasks": ["7.3"] },
    { "id": 6, "tasks": ["7.4", "7.5", "7.6"] },
    { "id": 7, "tasks": ["7.8"] }
  ]
}
```
