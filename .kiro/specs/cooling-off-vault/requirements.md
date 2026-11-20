# Requirements Document

## Introduction

The Cooling-Off Vault turns the Behavioral Risk Engine's scoring into an enforced payment pause. It adds the Go packages `backend/internal/vault` (state machine and time-lock), `backend/internal/guardian` (co-approval, veto, token verification and SSE hub), the HTTP router shared by the two entrypoints `backend/cmd/server` and `backend/cmd/lambda`, and the six public endpoints. It builds on the `behavioral-risk-engine` spec: `risk.Evaluate`, `store.VaultStore` (`Create`, `Get`, `UpdateIf`), the store errors, `store.NewFromEnv` and `ValidateMasked`.

Design decisions fixed for this feature:

- Guardian authorization uses a stateless per-vault token signed with `HMAC_SECRET_KEY`.
- Lock expiry is lazy. No background timer exists. A vault flips to EXPIRED when it is read or acted on after its deadline.
- Every state change goes through `UpdateIf` with the expected state, so one caller wins each race.
- Live updates use an in-process SSE hub on the local server. On Lambda the stream is short-lived and the UI polls `GET /api/v1/vault/{id}`.

## Glossary

- **Vault_Service**: The Go package `backend/internal/vault`. It owns the Vault_State transition rules and the Lock_Deadline.
- **Guardian_Service**: The Go package `backend/internal/guardian`. It owns Guardian_Token issue and verification, guardian actions and the SSE_Hub.
- **Router**: The shared `net/http` handler, built once and used by both the Server_Entrypoint and the Lambda_Entrypoint.
- **Server_Entrypoint**: `backend/cmd/server/main.go`, which serves the Router with `http.ListenAndServe`.
- **Lambda_Entrypoint**: `backend/cmd/lambda/main.go`, which serves the Router through `httpadapter` from `aws-lambda-go-api-proxy`.
- **Vault_State**: One of INSTANT_PASS, REALITY_CHECK, LOCKED_15M, GUARDIAN_APPROVED, VETOED, EXPIRED, as defined in the `store` package.
- **Terminal_State**: A Vault_State from which the Vault_Service permits no transition. VETOED and GUARDIAN_APPROVED are Terminal_States. INSTANT_PASS is also a Terminal_State.
- **Vault_Record**: The persisted vault item defined by the `behavioral-risk-engine` spec. This feature adds the fields `lock_deadline`, `token_expiry` and `settled` to it.
- **Settled**: A boolean on the Vault_Record. It is true when the payment is cleared to proceed: on INSTANT_PASS, after a passed Reality_Check, on GUARDIAN_APPROVED, and after Reconfirmation of an EXPIRED vault.
- **Lock_Duration**: The constant 900 seconds.
- **Lock_Deadline**: The Unix-epoch second at which a LOCKED_15M vault becomes EXPIRED.
- **Reality_Check**: The three questions defined in the RukPay UX rules: Coercion Check, Secrecy Check and Verification Check, each answered Yes or No.
- **Reconfirmation**: A final calm confirmation by the user that the payment should proceed, required after a vault reaches EXPIRED.
- **Guardian_Token**: A string issued for one vault, made of the vault id, a Token_Expiry and an HMAC-SHA256 tag over those two values keyed with `HMAC_SECRET_KEY`.
- **Token_Expiry**: The Unix-epoch second equal to the original Lock_Deadline at the time of locking. It does not change after demo fast-forward.
- **Guardian_Action**: A request carrying `action` (APPROVE or VETO), `token` and, for APPROVE, `confirmed`.
- **Evidence_Summary**: A text record produced on VETOED containing the vault id, masked identifiers, amount, Risk_Score, creation time, veto time and the National Cybercrime Helpline details `1930` and `cybercrime.gov.in`.
- **SSE_Hub**: The in-process publish and subscribe component that fans out Vault_Events to open SSE connections, keyed by vault id.
- **Vault_Event**: A JSON message with the vault id, Vault_State, Settled, remaining lock seconds, a plain-language reason, and masked metadata only.
- **Demo_Mode**: The condition in which the environment variable `DEMO_MODE` equals `true`.
- **Masked_Identifier**: As defined in the `behavioral-risk-engine` spec.
- **PBT_Suite**: The property-based tests written with `pgregory.net/rapid`.

## Requirements

### Requirement 1: Vault Creation From Risk Evaluation

**User Story:** As a payer, I want each payment checked and assigned a protective state, so that safe payments pass and risky ones are paused.

#### Acceptance Criteria

1. WHEN a valid `POST /api/v1/transactions/evaluate` request arrives, THE Vault_Service SHALL call `risk.Evaluate` with the five Signals, create a Vault_Record through `VaultStore.Create` with a new random vault id, and return the vault id, Vault_State, Risk_Score and a plain-language message.
2. WHEN `risk.Evaluate` returns INSTANT_PASS, THE Vault_Service SHALL store the Vault_Record with Settled true.
3. WHEN `risk.Evaluate` returns REALITY_CHECK, THE Vault_Service SHALL store the Vault_Record with Settled false.
4. WHEN `risk.Evaluate` returns LOCKED_15M, THE Vault_Service SHALL store the Vault_Record with `lock_deadline` equal to the creation time plus the Lock_Duration and SHALL include a Guardian_Token in the response.
5. THE Vault_Service SHALL generate vault ids from `crypto/rand` with at least 128 bits of entropy.
6. IF `risk.Evaluate` returns a validation error, THEN THE Router SHALL respond with HTTP 400 and a body that names the invalid field.
7. IF the request body contains an identifier that fails `ValidateMasked`, THEN THE Router SHALL respond with HTTP 400 and SHALL NOT call `VaultStore.Create`.
8. THE Router SHALL set the Vault_Record `expires_at` to at least the Lock_Deadline plus 3600 seconds, so that DynamoDB TTL does not remove an active vault.

### Requirement 2: Vault State Machine Rules

**User Story:** As a backend developer, I want one place that decides which transitions are legal, so that no handler can move a vault into an invalid state.

#### Acceptance Criteria

1. THE Vault_Service SHALL permit exactly these transitions: REALITY_CHECK to LOCKED_15M, LOCKED_15M to GUARDIAN_APPROVED, LOCKED_15M to VETOED, and LOCKED_15M to EXPIRED.
2. THE Vault_Service SHALL permit the transition from creation to INSTANT_PASS, REALITY_CHECK or LOCKED_15M only through `risk.Evaluate`.
3. IF a transition outside the set in criteria 1 is requested, THEN THE Vault_Service SHALL return an error `ErrInvalidTransition` and leave the stored Vault_Record unchanged.
4. WHILE a Vault_Record is in VETOED, GUARDIAN_APPROVED or INSTANT_PASS, THE Vault_Service SHALL reject every transition request with `ErrInvalidTransition`.
5. THE Vault_Service SHALL perform every state change through `VaultStore.UpdateIf` with the state it last read as `expectedState`.
6. IF `VaultStore.UpdateIf` returns `ErrConditionFailed`, THEN THE Vault_Service SHALL re-read the Vault_Record and return the current state to the caller with HTTP 409.
7. WHEN several callers attempt a transition on the same vault with the same expected state at the same moment, THE Vault_Service SHALL apply exactly one transition and return HTTP 409 to the others.

### Requirement 3: Reality Check

**User Story:** As a payer under pressure, I want three calm questions, so that a scam call is caught before money moves.

#### Acceptance Criteria

1. WHEN `POST /api/v1/vault/{id}/reality-check` receives answers to all three questions for a vault in REALITY_CHECK, THE Vault_Service SHALL evaluate the answers.
2. IF the Coercion Check answer is Yes, THEN THE Vault_Service SHALL transition the vault to LOCKED_15M.
3. IF the Secrecy Check answer is Yes, THEN THE Vault_Service SHALL transition the vault to LOCKED_15M.
4. IF the Verification Check answer is No, THEN THE Vault_Service SHALL transition the vault to LOCKED_15M.
5. WHEN the Coercion Check answer is No, the Secrecy Check answer is No and the Verification Check answer is Yes, THE Vault_Service SHALL set Settled to true and keep the Vault_State at REALITY_CHECK.
6. WHEN a vault enters LOCKED_15M from a Reality_Check, THE Vault_Service SHALL set `lock_deadline` to the transition time plus the Lock_Duration and issue a Guardian_Token.
7. IF the request omits any of the three answers or holds a value other than Yes or No, THEN THE Router SHALL respond with HTTP 400 and SHALL NOT change the vault.
8. IF the vault is not in REALITY_CHECK, THEN THE Router SHALL respond with HTTP 409 and the current state.
9. THE Vault_Service SHALL NOT store the free-text of any answer beyond the three Yes or No values.

### Requirement 4: Cooling-Off Lock and Lazy Expiry

**User Story:** As a payer, I want my money held safely for 15 minutes, so that I have time to verify who is asking.

#### Acceptance Criteria

1. WHILE a vault is in LOCKED_15M and the current time is before `lock_deadline`, THE Vault_Service SHALL keep Settled false and report the remaining lock seconds on every read.
2. WHEN a vault in LOCKED_15M is read or acted on at a time equal to or after `lock_deadline`, THE Vault_Service SHALL transition it to EXPIRED through `VaultStore.UpdateIf` with expected state LOCKED_15M before processing the request.
3. THE Vault_Service SHALL rely on no background timer, goroutine or scheduled job to expire vaults.
4. WHILE a vault is in EXPIRED and Settled is false, THE Vault_Service SHALL report that Reconfirmation is required.
5. WHEN the user sends Reconfirmation for a vault in EXPIRED, THE Vault_Service SHALL set Settled to true and keep the Vault_State at EXPIRED.
6. IF a Reconfirmation arrives for a vault that is not in EXPIRED, THEN THE Router SHALL respond with HTTP 409 and the current state.
7. THE Vault_Service SHALL take the current time from an injectable clock, so that tests can control time.

### Requirement 5: Guardian Token Issue and Verification

**User Story:** As a family guardian, I want a link that only I can use for this one vault, so that nobody else can approve or block it.

#### Acceptance Criteria

1. WHEN a vault enters LOCKED_15M, THE Guardian_Service SHALL issue a Guardian_Token computed as HMAC-SHA256, keyed with `HMAC_SECRET_KEY`, over the vault id and the Token_Expiry.
2. THE Guardian_Service SHALL set the Token_Expiry to the original Lock_Deadline.
3. WHEN a Guardian_Action arrives, THE Guardian_Service SHALL recompute the HMAC tag and compare it to the supplied tag using `crypto/hmac.Equal` before reading any other part of the request.
4. IF the tag does not match, THEN THE Guardian_Service SHALL respond with HTTP 403 and SHALL NOT call `VaultStore.UpdateIf`.
5. IF the Token_Expiry is earlier than or equal to the current time, THEN THE Guardian_Service SHALL respond with HTTP 403 and SHALL NOT call `VaultStore.UpdateIf`.
6. IF the vault id inside the token differs from the vault id in the request path, THEN THE Guardian_Service SHALL respond with HTTP 403.
7. IF the token is malformed, THEN THE Guardian_Service SHALL respond with HTTP 403 with the same body as a tag mismatch.
8. IF `HMAC_SECRET_KEY` is unset or empty at startup, THEN THE Server_Entrypoint and THE Lambda_Entrypoint SHALL exit with a startup error before serving requests, unless Demo_Mode is true, in which case the entrypoint SHALL generate a random in-memory key.
9. THE Guardian_Service SHALL keep `HMAC_SECRET_KEY` out of every response, log line, Vault_Record and Vault_Event.

### Requirement 6: Guardian Approval and Veto

**User Story:** As a family guardian, I want to approve only after speaking to my relative, and to block a scam in one tap, so that the payment is safe either way.

#### Acceptance Criteria

1. WHEN `POST /api/v1/vault/{id}/guardian-action` carries a valid Guardian_Token, `action` APPROVE and `confirmed` true for a vault in LOCKED_15M before its deadline, THE Guardian_Service SHALL transition the vault to GUARDIAN_APPROVED with Settled true.
2. WHEN the request carries a valid Guardian_Token and `action` APPROVE with `confirmed` false or absent, THE Guardian_Service SHALL respond with HTTP 200 and the status `CONFIRMATION_REQUIRED` and SHALL NOT change the vault.
3. WHEN the request carries a valid Guardian_Token and `action` VETO for a vault in LOCKED_15M before its deadline, THE Guardian_Service SHALL transition the vault to VETOED with Settled false and store an Evidence_Summary.
4. THE Guardian_Service SHALL return the Evidence_Summary in the VETO response and in later reads of a VETOED vault.
5. IF the `action` value is neither APPROVE nor VETO, THEN THE Router SHALL respond with HTTP 400.
6. IF the vault is not in LOCKED_15M, THEN THE Guardian_Service SHALL respond with HTTP 409 and the current state, and SHALL NOT change the vault.
7. THE Guardian_Service SHALL permit a user veto of a LOCKED_15M vault through the same endpoint using the Guardian_Token held by the user panel.
8. THE Evidence_Summary SHALL contain Masked_Identifiers only and no OTP, PIN or password.

### Requirement 7: Vault Read Endpoint

**User Story:** As a UI developer, I want a simple read endpoint, so that both phone panels can show the current vault, including when streaming is unavailable.

#### Acceptance Criteria

1. WHEN `GET /api/v1/vault/{id}` is called for a stored vault, THE Router SHALL return the vault id, Vault_State, Settled, Risk_Score, remaining lock seconds, plain-language reason, and masked metadata.
2. WHILE a vault is in LOCKED_15M, THE Router SHALL include the Guardian_Token in the response so that the guardian panel can act.
3. WHILE a vault is in VETOED, THE Router SHALL include the Evidence_Summary in the response.
4. IF the vault id is not stored, THEN THE Router SHALL respond with HTTP 404 and a body that holds no submitted values.
5. THE Router SHALL apply lazy expiry from Requirement 4 criterion 2 before building the response.

### Requirement 8: SSE Event Stream

**User Story:** As a guardian, I want to see the lock appear on my phone right away, so that I can act within the 15 minutes.

#### Acceptance Criteria

1. WHEN `GET /api/v1/events/stream?vault_id={id}` is called for a stored vault, THE Router SHALL respond with `Content-Type: text/event-stream`, disable response buffering, and send the current Vault_Event as the first event.
2. WHEN a vault changes state, THE Guardian_Service SHALL publish a Vault_Event through the SSE_Hub to every open connection for that vault id.
3. WHEN a client disconnects, THE SSE_Hub SHALL remove the subscription and release its resources.
4. WHILE a connection is open, THE Router SHALL send a comment keep-alive line every 15 seconds.
5. IF the `vault_id` query parameter is missing or the vault is not stored, THEN THE Router SHALL respond with HTTP 400 or HTTP 404 respectively, without opening a stream.
6. WHILE running on Lambda, THE Router SHALL close each stream after at most 25 seconds, so that the client falls back to polling `GET /api/v1/vault/{id}`.
7. THE Router SHALL treat the process as running on Lambda WHEN the environment variable `AWS_LAMBDA_FUNCTION_NAME` is set.
8. THE SSE_Hub SHALL drop an event for a slow subscriber instead of blocking the publisher.
9. THE Vault_Event SHALL contain Masked_Identifiers only and SHALL contain no OTP, PIN, password or HMAC key.

### Requirement 9: Demo Fast-Forward

**User Story:** As a hackathon judge, I want to skip the 15-minute wait, so that I can see the expiry flow in seconds.

#### Acceptance Criteria

1. WHILE Demo_Mode is true, WHEN `POST /api/v1/vault/{id}/demo-fast-forward` is called for a vault in LOCKED_15M, THE Vault_Service SHALL set `lock_deadline` to the current time plus 3 seconds through `VaultStore.UpdateIf` with expected state LOCKED_15M.
2. WHILE Demo_Mode is false or unset, THE Router SHALL respond to `POST /api/v1/vault/{id}/demo-fast-forward` with HTTP 404.
3. IF the vault is not in LOCKED_15M, THEN THE Router SHALL respond with HTTP 409 and the current state.
4. IF the current `lock_deadline` is already within 3 seconds, THEN THE Vault_Service SHALL leave `lock_deadline` unchanged.
5. THE Vault_Service SHALL leave `token_expiry` unchanged during fast-forward.
6. THE Vault_Service SHALL never move `lock_deadline` later than its previous value.

### Requirement 10: Shared Router and Entrypoints

**User Story:** As a deployer, I want one router served locally or on Lambda, so that behavior is identical in both places.

#### Acceptance Criteria

1. THE Router SHALL register the routes `POST /api/v1/transactions/evaluate`, `POST /api/v1/vault/{id}/reality-check`, `POST /api/v1/vault/{id}/guardian-action`, `POST /api/v1/vault/{id}/demo-fast-forward`, `GET /api/v1/vault/{id}` and `GET /api/v1/events/stream` using a method-and-path dispatcher built on `net/http` that works on Go 1.21.1 (no Go 1.22 `ServeMux` patterns).
2. THE Server_Entrypoint SHALL build the Router and serve it with `http.ListenAndServe` on the address from the `PORT` environment variable, defaulting to `8080`.
3. THE Lambda_Entrypoint SHALL build the same Router and serve it through `httpadapter` and `lambda.Start`.
4. WHEN `STORAGE_DRIVER` is unset, THE Server_Entrypoint SHALL use the `memory` driver.
5. THE Server_Entrypoint and THE Lambda_Entrypoint SHALL create the store with `store.NewFromEnv`, reading `STORAGE_DRIVER`, `DYNAMODB_TABLE_NAME` and `DYNAMODB_ENDPOINT`.
6. WHEN `AWS_LAMBDA_FUNCTION_NAME` is set, THE Server_Entrypoint SHALL run the Lambda path instead of `http.ListenAndServe`, so that one binary works in both places.
7. THE Router SHALL read `ALLOWED_ORIGIN` for CORS and respond to preflight `OPTIONS` requests with the allowed methods, headers and origin.
8. IF the request `Origin` differs from `ALLOWED_ORIGIN`, THEN THE Router SHALL omit the `Access-Control-Allow-Origin` header.
9. IF `ALLOWED_ORIGIN` is unset, THEN THE Router SHALL allow the origin `http://localhost:5173` only.
10. THE Router SHALL limit each request body to 64 KiB and respond with HTTP 413 when the limit is exceeded.
11. THE Router SHALL respond to a body that is not valid JSON with HTTP 400.

### Requirement 11: Privacy, Logging and Calm Messaging

**User Story:** As a privacy-conscious user under stress, I want no sensitive data kept and only calm words shown, so that RukPay helps without alarming me.

#### Acceptance Criteria

1. THE Router SHALL write log lines that contain the method, route pattern, status code and vault id only.
2. THE Router SHALL NOT write request bodies, Signals, Masked_Identifiers, amounts, tokens or the HMAC key to logs.
3. THE Router SHALL apply a redaction step that replaces any value matching an OTP, PIN or password field name with `[REDACTED]` before it is stored, logged or published.
4. THE Vault_Service SHALL produce user-facing messages in calm plain language, with no phrases from the banned list (`FATAL ERROR`, `SECURITY BREACH`, `ILLEGAL TRANSACTION`, `YOUR ACCOUNT WILL BE BLOCKED`, `FRAUDSTER DETECTED`) and no banned technical terms.
5. WHEN a vault enters LOCKED_15M, THE Vault_Service SHALL set the reason message to a protective-pause sentence that states the 15-minute hold and the reasons in plain English.
6. THE Router SHALL return error bodies that contain an error code and a plain-language message and no stack trace, store error text or submitted values.
7. THE Vault_Service SHALL contain no import of any SQLite package.

### Requirement 12: Property-Based Verification

**User Story:** As a maintainer, I want randomized tests on the state machine and token checks, so that refactors cannot break the safety guarantees.

#### Acceptance Criteria

1. THE PBT_Suite SHALL use `pgregory.net/rapid` with at least 1000 generated cases per property.
2. THE PBT_Suite SHALL test that, for any generated sequence of operations (reality check, guardian action, fast-forward, read, reconfirm, clock advance), every observed Vault_State change belongs to the permitted transitions in Requirement 2 criterion 1.
3. THE PBT_Suite SHALL test that, once a vault is in VETOED or GUARDIAN_APPROVED, no later operation sequence changes its Vault_State.
4. THE PBT_Suite SHALL test that Settled is true only in INSTANT_PASS, GUARDIAN_APPROVED, a REALITY_CHECK vault with a passed Reality_Check, or an EXPIRED vault after Reconfirmation.
5. THE PBT_Suite SHALL test that a Guardian_Token verifies true for the vault id and Token_Expiry it was issued for, and false when any single byte of the token, the vault id or the key is changed.
6. THE PBT_Suite SHALL test that a Guardian_Token with a Token_Expiry at or before the current time never verifies.
7. THE PBT_Suite SHALL test that for every combination of the three answers, the vault enters LOCKED_15M exactly when the Coercion Check is Yes, the Secrecy Check is Yes or the Verification Check is No.
8. THE PBT_Suite SHALL test that fast-forward never increases `lock_deadline` and never changes `token_expiry`.
9. THE PBT_Suite SHALL test, with at least 50 concurrent goroutines against a MemoryStore, that exactly one of a concurrent APPROVE and VETO succeeds on a LOCKED_15M vault.
10. THE PBT_Suite SHALL test that an expired LOCKED_15M vault never ends in GUARDIAN_APPROVED, for any Guardian_Action sent at or after `lock_deadline`.
11. THE PBT_Suite SHALL run to completion with `go test ./...` using the MemoryStore, an injectable clock, and no network access.
