# RukPay (रुक-पे): Pause Before You Pay

[![CodeQL](https://github.com/RahulKumar1119/rukpay/actions/workflows/codeql.yml/badge.svg)](https://github.com/RahulKumar1119/rukpay/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Anti-panic behavioral payment firewall combining scam-stress detection, 15-minute cooling-off locks, and family co-approval. Built for Kiroween 2026.

> **Project status:** the backend foundation is built and tested: the behavioral risk engine, the vault store (in-memory and DynamoDB), the cooling-off vault rules, the guardian flow and the HTTP API, with a local server and a Lambda entry point. The React front end is built: the two-phone demo with three one-click presets. The one-command deploy is planned and not built yet. See [Status](#status).

## The problem

In a "digital arrest" scam, a caller poses as police, CBI, customs or the bank and keeps the victim on the phone, frightened and rushed, until they transfer money. Bank warnings do not help in that moment. Flashing red banners and technical wording add to the panic, and the scammer simply tells the victim to ignore them.

RukPay does not try to out-shout the scammer. It slows the room down:

1. It scores each payment on a few signals that scams tend to share.
2. Low-risk payments go through with no friction.
3. Medium-risk payments get three calm questions first.
4. High-risk payments are held in a 15-minute protective pause, and a trusted family member is asked to approve or block them.

## How it works

### 1. Score the payment

The risk engine reads five signals and adds up points. The score is capped at 100.

| Signal | Condition | Points |
|---|---|---|
| `amount_inr` | below ₹2,000 | 0 |
| | ₹2,000 to ₹9,999 | 10 |
| | ₹10,000 to ₹24,999 | 20 |
| | ₹25,000 or more | 35 |
| `beneficiary_age_hours` | below 24 hours | 25 |
| | 24 to 71 hours | 10 |
| | 72 hours or more | 0 |
| `is_late_night` | true | 10 |
| `active_unknown_call` | true | 20 |
| `rapid_retry_count` | 0 or 1 | 0 |
| | 2 | 5 |
| | 3 or more | 10 |

The same signals always give the same score. The engine uses no clock, randomness or outside state.

### 2. Pick the first state

| Risk score | State | What happens |
|---|---|---|
| below 40 | `INSTANT_PASS` | The payment settles at once. |
| 40 to 74 | `REALITY_CHECK` | The user answers three calm questions first. |
| 75 or more | `LOCKED_15M` | The money is held for 900 seconds and a guardian is alerted. |

Three more states complete the vault life cycle: `GUARDIAN_APPROVED`, `VETOED` and `EXPIRED`. The rules for moving between states are in the `vault` package.

### 3. Demo presets

| Preset | Signals | Score | State |
|---|---|---|---|
| Safe daily payment | ₹450 to a known contact, daytime, no call | 0 | `INSTANT_PASS` |
| Unusual transfer | ₹18,000 to a new contact, daytime, no call | 45 | `REALITY_CHECK` |
| "Digital arrest" panic transfer | ₹85,000 to a brand-new account at 11:45 PM during an unknown call | 90 | `LOCKED_15M` |

### 4. Guarantees the tests enforce

- A payment of ₹25,000 or more, to a beneficiary under 24 hours old, during an unknown call, always scores 75 or more and is locked. The lowest possible score for that case is 80.
- A payment below ₹2,000, to a beneficiary 72 hours old or more, in daytime, with no call and at most one retry, always scores below 40 and passes.
- Turning `active_unknown_call` on never lowers the score.

### 5. The reality check and the guardian

The reality check is built in the `vault` package, and the approve and block actions in the `guardian` package. The guardian's screen is planned.

The three reality-check questions are:

1. Are you on a call with someone claiming to be Police, CBI, Customs, RBI or your bank?
2. Did the caller ask you not to tell your family or bank staff?
3. Have you checked this person or bill without using a phone number they just sent you?

A "yes" to the first two, or a "no" to the third, locks the payment for 900 seconds. Three calm answers settle the vault and leave it in `REALITY_CHECK`. The guardian's screen then shows a plain-language reason and two actions: **I Spoke With Them — Safe to Approve** (with a second confirmation tap) and **Block & Report Scam (1930)**, which cancels the transfer and shows India's National Cybercrime Helpline details.

## Status

| Area | State |
|---|---|
| Risk engine (`backend/internal/risk`) | Built and tested |
| Vault store: types, interface, mask validation | Built and tested |
| In-memory store (`MemoryStore`) | Built and tested |
| DynamoDB store (`DynamoDBStore`) and `NewFromEnv` factory | Built. Tested against an in-process fake only. Not yet run against real DynamoDB or DynamoDB Local. |
| Table definition (`infra/dynamodb-table.json`) | Written, not deployed |
| Cooling-off vault rules (`backend/internal/vault`) and redaction (`backend/internal/redact`) | Built and tested. Spec: [`cooling-off-vault`](.kiro/specs/cooling-off-vault) |
| Guardian token, approve and veto, evidence summary and event hub (`backend/internal/guardian`) | Built and tested. Same spec. |
| HTTP API (`backend/internal/httpapi`), the local server (`cmd/server`) and the Lambda entry point (`cmd/lambda`) | Built and tested. Same spec. The Lambda function has not been deployed. |
| React front end with the two-phone demo | Built and tested: both phones, the preset bar, the reality-check dialog, the vault card with its countdown, guardian approve and block, the evidence card and the unreachable notice. Walked through once in a real browser against the local server. Not yet deployed. Spec: [`dual-screen-simulator`](.kiro/specs/dual-screen-simulator) |
| `infra/deploy.sh` and the SAM template | Planned |

## Architecture

```text
  User phone panel  ───────┐                      ┌───────  Guardian phone panel
  (payment simulator)      │                      │         (approve or block)
                           ▼                      ▼
                    HTTP API (net/http, SSE) ─── built
                           │
         ┌─────────────────┼──────────────────┐
         ▼                 ▼                  ▼
   risk (built)       vault (built)     guardian (built)
   signals → score    cooling-off       co-approval and veto
   → first state      state machine
         │                 │
         └───────┬─────────┘
                 ▼
          store (built)
          VaultStore interface
          ┌───────────┴────────────┐
          ▼                        ▼
     MemoryStore              DynamoDBStore
     local and tests          table RukPayVaults
```

The `risk` package imports `store` for the `VaultState` type. `store` never imports `risk`. `vault` uses `risk`, `store` and `redact`, and reaches the guardian only through two small interfaces, so it never imports `guardian`. `guardian` imports `vault`, `store` and `redact`.

### Tech stack

| Layer | Choice |
|---|---|
| Backend | Go 1.21.1, standard-library `net/http` (no third-party router) |
| Persistence | Amazon DynamoDB, single table. No SQLite. |
| Local and test storage | Thread-safe in-memory store |
| Hosting | AWS Lambda (ARM64, `provided.al2023`) with a Function URL, in `ap-south-1`. Not deployed yet. |
| Real-time updates | Server-Sent Events, with short polling as a fallback |
| Property-based tests | [`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid) |
| Front end | React 18 and TypeScript (strict), Vite, Tailwind CSS v3, `lucide-react`. Tests: Vitest, Testing Library, fast-check. |

## Repository layout

```text
rukpay/
├── backend/                     Go module github.com/RahulKumar1119/rukpay/backend
│   ├── cmd/
│   │   ├── server/              Local server, also runs as a Lambda function when AWS_LAMBDA_FUNCTION_NAME is set
│   │   └── lambda/              Lambda entry point
│   └── internal/
│       ├── guardian/            Signed guardian token, approve and veto, evidence summary, event hub
│       ├── httpapi/             The six routes, CORS, body limit, logging, config, event stream
│       ├── redact/              Removes OTP, PIN and password values from text
│       ├── risk/                Risk scoring: Signals, Validate, Score, MapState, Evaluate
│       ├── store/               VaultStore, MemoryStore, DynamoDBStore, mask validation, factory
│       └── vault/               Cooling-off rules: transitions, lazy expiry, reality check, fast-forward
├── frontend/                    React and Vite app
│   └── src/
│       ├── components/          PhoneFrame, UserPhonePanel, GuardianPhonePanel, RealityCheckModal,
│       │                        CoolingOffVaultCard, EvidenceCard, PresetBar, UnreachableNotice
│       ├── lib/                 privacyMasker, copy, api, presets, screens, countdown,
│       │                        simulatorReducer, vaultSync, useSimulator
│       └── test/                Vitest setup, the fetch and EventSource stubs, property-test helpers
├── infra/
│   └── dynamodb-table.json      CloudFormation template for the RukPayVaults table
├── .kiro/
│   ├── specs/                   Requirements, design and tasks for each feature
│   ├── steering/                Always-on rules: tech stack, anti-panic UX, skills
│   ├── hooks/                   Automation hooks (run Go tests on save, PII and tone check)
│   └── skills/                  Repo-local skills (Go property-test conventions)
├── .github/workflows/           CodeQL analysis (Go, TypeScript) and CI (backend and frontend checks)
├── AGENTS.md                    Rules for AI coding agents working in this repo
└── LICENSE
```

## Getting started

You need Go 1.21.1. The tests need no Docker, no AWS account and no network.

```bash
git clone git@github.com:RahulKumar1119/rukpay.git
cd rukpay/backend

go vet ./...
go test -race ./...
```

Each property test runs at least 1,000 generated inputs. The tests raise `rapid`'s default of 100 on their own, so you do not need a flag. To set the count yourself:

```bash
go test -race ./... -rapid.checks=1000
```

When a property test fails, `rapid` saves the failing input under `testdata/rapid/` next to the test. Commit it as a regression case, or delete it once the bug is fixed.

### Run the server

```bash
cd backend
DEMO_MODE=true go run ./cmd/server          # memory store, random signing key, port 8080
```

`DEMO_MODE=true` also turns on the demo fast-forward route and lets the server start without `HMAC_SECRET_KEY`. Outside demo mode, set a secret key:

```bash
HMAC_SECRET_KEY="$(openssl rand -hex 32)" go run ./cmd/server
```

Try the high-risk preset:

```bash
curl -s localhost:8080/api/v1/transactions/evaluate \
  -d '{"amount_inr":85000,"beneficiary_age_hours":1,"is_late_night":true,"active_unknown_call":true,"rapid_retry_count":0}'
```

The answer holds the vault id, the state `LOCKED_15M` and a `guardian_token`. To build the Lambda binary:

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap ./cmd/lambda
```

### Run the front end

You need Node 22.12 or later.

```bash
cd frontend
cp .env.example .env          # VITE_API_BASE_URL=http://localhost:8080
npm install
npm run dev                   # http://localhost:5173
```

Start the backend first, in demo mode, so the "Demo: skip the wait" button works (see [Run the server](#run-the-server)). Then open the page and press a preset. There is no login.

The dev server uses port 5173 and fails if the port is taken, because the backend allows that origin by default. Other scripts: `npm run typecheck`, `npm run build` and `npm test`. Each property test runs 1,000 generated inputs.

### Use the risk engine

```go
import (
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
)

res, err := risk.Evaluate(risk.Signals{
	AmountINR:         85000,
	BeneficiaryAgeHrs: 2,
	IsLateNight:       true,
	ActiveUnknownCall: true,
	RapidRetryCount:   0,
})
// err == nil, res.Score == 90, res.State == store.StateLocked15M
```

`Evaluate` returns a `*risk.ValidationError` that names the field when `amount_inr` is 0 or less, or when `beneficiary_age_hours` or `rapid_retry_count` is negative.

### Use the store

```go
import (
	"context"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

vs, err := store.NewFromEnv(ctx) // reads STORAGE_DRIVER and friends

err = vs.Create(ctx, rec)                                        // store.ErrAlreadyExists on a duplicate id
rec, err = vs.Get(ctx, id)                                       // store.ErrNotFound if missing or expired
err = vs.UpdateIf(ctx, id, store.StateLocked15M, nextRecord)     // store.ErrConditionFailed on a state mismatch
```

Check errors with `errors.Is`.

## Configuration

The server reads these environment variables. The first three go to `store.NewFromEnv`.

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `STORAGE_DRIVER` | Yes | none | `memory` or `dynamodb`. An unset, empty or unknown value is an error, so a deployment that lost its configuration fails loudly instead of quietly keeping vaults in memory. |
| `DYNAMODB_TABLE_NAME` | No | `RukPayVaults` | Table used by the `dynamodb` driver. |
| `DYNAMODB_ENDPOINT` | No | the AWS endpoint | Endpoint override, for example `http://localhost:8000` for DynamoDB Local. |

The `dynamodb` driver uses the standard AWS SDK for Go v2 credential and region lookup. The client is built without a network call, and credentials are resolved on the first request.

The HTTP service adds these. When `STORAGE_DRIVER` is unset, the service uses `memory`, while `store.NewFromEnv` on its own still requires it.

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `HMAC_SECRET_KEY` | Yes, unless `DEMO_MODE=true` | none | Key that signs guardian tokens. The server refuses to start without it. In demo mode a random key is made for the life of the process. |
| `DEMO_MODE` | No | off | `true` turns on `demo-fast-forward`, which moves a lock to 3 seconds from now. Any other value leaves it off, and the route answers 404. |
| `ALLOWED_ORIGIN` | No | `http://localhost:5173` | The one browser origin that CORS allows. |
| `PORT` | No | `8080` | Port of the local server. |
| `AWS_LAMBDA_FUNCTION_NAME` | Set by AWS | none | When set, the server runs as a Lambda function and streams close after 25 seconds. |

## Data model

### Vault record

One record describes one vault. It holds masked identifiers only.

| Field | Type | Notes |
|---|---|---|
| `id` | string | Vault id |
| `state` | string | One of the six vault states |
| `risk_score` | int | 0 to 100 |
| `payer_account_masked`, `beneficiary_masked` | string | `XXXX-XXXX-4821`, optional |
| `upi_masked` | string | `r***@okaxis`, optional |
| `phone_masked` | string | `+91-XXXXXX3210`, optional |
| `amount_inr` | int64 | Amount in rupees |
| `created_at` | time | Stored as Unix epoch seconds |
| `version` | int | Owned by the caller. The store never changes it. |
| `expires_at` | time | Unix epoch seconds. DynamoDB TTL attribute. |
| `lock_deadline` | int64 | When the lock ends, in epoch seconds. 0 means never locked. |
| `token_expiry` | int64 | The lock deadline as first set. A demo fast-forward never changes it. |
| `settled` | bool | The user has finished the step this state asks for |
| `reason` | string | Plain-language explanation for the user. No identifiers. |
| `evidence` | string | Masked report written when a vault is vetoed. Optional. |

There is no field for raw account numbers, raw UPI IDs, raw phone numbers, OTPs, PINs or passwords.

### DynamoDB table

`infra/dynamodb-table.json` defines the table `RukPayVaults`:

- Partition key `PK` (string) is `VAULT#<id>`. Sort key `SK` (string) is `META`.
- On-demand billing.
- TTL is enabled on `expires_at`.

It is a CloudFormation template, so deploy it with CloudFormation. The AWS CLI's `create-table` command cannot read it.

```bash
aws cloudformation deploy \
  --template-file infra/dynamodb-table.json \
  --stack-name rukpay-vaults \
  --region ap-south-1
```

### Store behavior

- **Persistence only.** `UpdateIf` replaces a record when the stored state equals the expected state. It accepts any pair of states and does not check whether the move is allowed. The `vault` package owns those rules.
- **Atomic updates.** `MemoryStore` takes a write lock for the check and the replace. `DynamoDBStore` sends one conditional `PutItem`: `attribute_not_exists(PK)` for `Create` and `attribute_exists(PK) AND #s = :expected` for `UpdateIf`. When that condition fails, one follow-up read decides between `ErrNotFound` and `ErrConditionFailed`.
- **Expiry.** DynamoDB TTL deletion can lag, so both stores compare `expires_at` with the clock on read. A record whose `expires_at` has passed is reported as `ErrNotFound`. A zero `expires_at` never expires.
- **Copies.** `Get` returns a copy. Changing it does not change the stored record.
- **Context.** Every method returns the context error when the context is already cancelled.

Known gap: if an expired item has not been deleted yet, `DynamoDBStore.UpdateIf` can still succeed when the state matches, while `MemoryStore.UpdateIf` returns `ErrNotFound`.

## API

One handler serves all six routes, locally and on Lambda. Every response is JSON except the event stream, and none is cached.

| Method and path | Purpose |
|---|---|
| `POST /api/v1/transactions/evaluate` | Score the signals, store the vault, return its view |
| `POST /api/v1/vault/{id}/reality-check` | Body is exactly one of `{"answers": {"coercion", "secrecy", "verification"}}` (each `"YES"` or `"NO"`) or `{"reconfirm": true}`. Answers lock or settle a `REALITY_CHECK` vault. Reconfirm settles an `EXPIRED` one. |
| `POST /api/v1/vault/{id}/guardian-action` | `{"action": "APPROVE" or "VETO", "token", "confirmed"}`. The token is checked first. `APPROVE` needs `"confirmed": true`, the second tap. |
| `POST /api/v1/vault/{id}/demo-fast-forward` | Moves a lock to 3 seconds from now. Answers 404 unless `DEMO_MODE=true`. |
| `GET /api/v1/vault/{id}` | The current view. A locked vault includes its `guardian_token`. A vetoed one includes `evidence_summary`. |
| `GET /api/v1/events/stream?vault_id=` | Server-Sent Events: the current view first, then each change, with a `: keep-alive` comment every 15 seconds. Events never carry the token. |

The view has `vault_id`, `state`, `settled`, `risk_score`, `remaining_seconds`, `reconfirm_required`, `reason`, `metadata` (masked identifiers and `amount_inr`), and, only when they apply, `guardian_token` and `evidence_summary`.

Errors share one shape, with calm fixed messages:

```json
{ "error": { "code": "invalid_field", "message": "Please check the amount and try again.", "field": "amount_inr" } }
```

| Status | Codes |
|---|---|
| 400 | `invalid_field` (names the field), `invalid_identifier`, `invalid_request` |
| 403 | `forbidden`: one identical body for a bad tag, a malformed token, another vault's token and an expired token |
| 404 | `not_found` |
| 405 | `method_not_allowed`, with an `Allow` header |
| 409 | `state_conflict`: the body also holds the current view as `vault` |
| 413 | `too_large`: request bodies are limited to 64 KiB |
| 500 | `internal`: a fixed message that never includes store text |

The router is a small dispatcher in `httpapi/route.go`, because Go 1.21's `ServeMux` has no method or `{id}` patterns. On Lambda the stream closes after 25 seconds and the client falls back to polling the read route.

## Privacy and safety

- Identifiers are masked before they reach the store: accounts as `XXXX-XXXX-4821`, phones as `+91-XXXXXX3210`, UPI IDs as `r***@okaxis`. `ValidateMasked` rejects anything else with `ErrUnmaskedIdentifier`, so a raw value is never written.
- OTPs, PINs and passwords have no place in any record, log line or event. Request bodies are decoded into typed structs, so an unknown field such as `otp` is dropped before it reaches the service, and `redact.Text` rewrites any such field in text the service builds.
- The server writes one log line per request: the route pattern, the status and the vault id. It never logs a body, a signal, an identifier, an amount, a token or the key, and it logs a vault id only when it has the 32-character hex shape.
- Error messages carry only the operation name and the vault id. They never include amounts, scores or identifiers, and a client error's own text is replaced by a fixed message while the original stays reachable with `errors.Is`.
- No package imports SQLite, and `store`, `risk` and `vault` import no logging package. Tests check this. Only `httpapi` and the entry points write output.

## Testing

| Layer | What it covers |
|---|---|
| Unit tests | Demo presets, every tier boundary, threshold mapping, validation errors, state constants, mask formats, `NewFromEnv`, condition-expression strings, cancelled contexts |
| Property tests (`rapid`) | Every property runs at least 1,000 generated inputs. Risk: scoring against an independent reference model, determinism, lock and pass guarantees, call monotonicity. Store: round trip, duplicates, compare-and-swap, copies, expiry, unmasked identifiers, item layout, error text. Vault: the exact transition table, creation, reality-check outcomes, lazy expiry, reconfirmation, random operation sequences checked against a model, fast-forward, the read view, calm and redacted messages. Guardian: the token round trip, any single changed byte breaking a token, confirmed approval, veto evidence, refused actions on non-locked or expired vaults, and the event hub never blocking. |
| Shared contract tests | The same store properties run once against `MemoryStore` and once against `DynamoDBStore` over a fake client, so both adapters face identical tests |
| Concurrency tests | 50 goroutines call `UpdateIf` on one id, and 50 to 64 goroutines approve or veto one locked vault, and many goroutines publish to and cancel hub subscriptions. Exactly one wins each time. Run with `-race`. |
| HTTP tests | The six routes, CORS, the 64 KiB limit, bad JSON, identical 403 bodies, error bodies free of store text, the event stream (first event, keep-alive, change events, disconnect, the Lambda limit), log lines, and the startup configuration |
| Import checks | No package imports SQLite. `store`, `risk` and `vault` import no logger. `vault` starts no goroutine and no timer. |

The fake DynamoDB client (`fake_dynamodb_test.go`) keeps items in memory, evaluates the two condition expressions the store uses, and stops with a clear error on any request shape it does not know, so a changed expression cannot pass silently.

## Development with Kiro

The project uses spec-driven development. Each feature has requirements, a design and a task list under `.kiro/specs/`:

| Spec | State |
|---|---|
| [`behavioral-risk-engine`](.kiro/specs/behavioral-risk-engine) | All tasks complete |
| [`cooling-off-vault`](.kiro/specs/cooling-off-vault) | All tasks complete: the store changes, `vault`, `guardian`, `httpapi` and the two entry points. |
| [`dual-screen-simulator`](.kiro/specs/dual-screen-simulator) | Planned |

Always-on guidance lives in `.kiro/steering/`: the tech stack, the anti-panic UX and privacy rules, and the skills each part of the code must use. Two hooks in `.kiro/hooks/` run the Go tests when backend code changes and check front-end files for alarmist wording and unmasked data.

## Roadmap

1. ~~React front end with the two-phone layout and the three one-click presets.~~ Built, with smoke checks for the project-wide rules (no storage, console or pure red, 48 pixel controls, 7:1 contrast, no state library).
2. One-command deploy: `infra/deploy.sh` and a SAM template for the table, the Lambda Function URL and its permissions.
3. Run the DynamoDB adapter against DynamoDB Local and real DynamoDB.

## License

[MIT](LICENSE). Copyright (c) 2026 RahulKumar Surati.
