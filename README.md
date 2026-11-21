# RukPay (रुक-पे): Pause Before You Pay

[![CodeQL](https://github.com/RahulKumar1119/rukpay/actions/workflows/codeql.yml/badge.svg)](https://github.com/RahulKumar1119/rukpay/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Anti-panic behavioral payment firewall combining scam-stress detection, 15-minute cooling-off locks, and family co-approval. Built for Kiroween 2026.

> **Project status:** the backend foundation is built and tested: the behavioral risk engine and the vault store (in-memory and DynamoDB). The cooling-off state machine, HTTP API, guardian flow and the React front end are planned and not built yet. See [Status](#status).

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

Three more states complete the vault life cycle: `GUARDIAN_APPROVED`, `VETOED` and `EXPIRED`. The rules for moving between states belong to the planned `vault` package.

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

### 5. The reality check and the guardian (planned)

The three reality-check questions are:

1. Are you on a call with someone claiming to be Police, CBI, Customs, RBI or your bank?
2. Did the caller ask you not to tell your family or bank staff?
3. Have you checked this person or bill without using a phone number they just sent you?

A "yes" to the first two, or a "no" to the third, locks the payment. The guardian's screen then shows a plain-language reason and two actions: **I Spoke With Them — Safe to Approve** (with a second confirmation tap) and **Block & Report Scam (1930)**, which cancels the transfer and shows India's National Cybercrime Helpline details.

## Status

| Area | State |
|---|---|
| Risk engine (`backend/internal/risk`) | Built and tested |
| Vault store: types, interface, mask validation | Built and tested |
| In-memory store (`MemoryStore`) | Built and tested |
| DynamoDB store (`DynamoDBStore`) and `NewFromEnv` factory | Built. Tested against an in-process fake only. Not yet run against real DynamoDB or DynamoDB Local. |
| Table definition (`infra/dynamodb-table.json`) | Written, not deployed |
| Cooling-off state machine (`vault`), guardian flow, HTTP API, SSE | Planned. Specs: [`cooling-off-vault`](.kiro/specs/cooling-off-vault) |
| React front end with the two-phone demo | Planned. Specs: [`dual-screen-simulator`](.kiro/specs/dual-screen-simulator) |
| Lambda entry point, `infra/deploy.sh`, SAM template | Planned |

## Architecture

```text
  User phone panel  ───────┐                      ┌───────  Guardian phone panel
  (payment simulator)      │                      │         (approve or block)
                           ▼                      ▼
                    HTTP API (net/http, SSE) ─── planned
                           │
         ┌─────────────────┼──────────────────┐
         ▼                 ▼                  ▼
   risk (built)      vault (planned)    guardian (planned)
   signals → score   cooling-off state  co-approval and veto
   → first state     machine
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

The `risk` package imports `store` for the `VaultState` type. `store` never imports `risk`.

### Tech stack

| Layer | Choice |
|---|---|
| Backend | Go 1.21.1, standard-library `net/http` (no third-party router) |
| Persistence | Amazon DynamoDB, single table. No SQLite. |
| Local and test storage | Thread-safe in-memory store |
| Planned hosting | AWS Lambda (ARM64, `provided.al2023`) with a Function URL, in `ap-south-1` |
| Real-time updates | Server-Sent Events, with short polling as a fallback |
| Property-based tests | [`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid) |
| Front end (planned) | React 18 and TypeScript (strict), Vite, Tailwind CSS |

## Repository layout

```text
rukpay/
├── backend/                     Go module github.com/RahulKumar1119/rukpay/backend
│   └── internal/
│       ├── risk/                Risk scoring: Signals, Validate, Score, MapState, Evaluate
│       └── store/               VaultStore, MemoryStore, DynamoDBStore, mask validation, factory
├── infra/
│   └── dynamodb-table.json      CloudFormation template for the RukPayVaults table
├── .kiro/
│   ├── specs/                   Requirements, design and tasks for each feature
│   ├── steering/                Always-on rules: tech stack, anti-panic UX, skills
│   ├── hooks/                   Automation hooks (run Go tests on save, PII and tone check)
│   └── skills/                  Repo-local skills (Go property-test conventions)
├── .github/workflows/           CodeQL analysis
├── AGENTS.md                    Rules for AI coding agents working in this repo
└── LICENSE
```

The `frontend/` directory, `backend/cmd/` and the remaining `internal/` packages (`vault`, `guardian`) do not exist yet.

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

`store.NewFromEnv` reads these environment variables.

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `STORAGE_DRIVER` | Yes | none | `memory` or `dynamodb`. An unset, empty or unknown value is an error, so a deployment that lost its configuration fails loudly instead of quietly keeping vaults in memory. |
| `DYNAMODB_TABLE_NAME` | No | `RukPayVaults` | Table used by the `dynamodb` driver. |
| `DYNAMODB_ENDPOINT` | No | the AWS endpoint | Endpoint override, for example `http://localhost:8000` for DynamoDB Local. |

The `dynamodb` driver uses the standard AWS SDK for Go v2 credential and region lookup. The client is built without a network call, and credentials are resolved on the first request.

Planned variables for the HTTP service are `HMAC_SECRET_KEY` and `ALLOWED_ORIGIN`.

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

- **Persistence only.** `UpdateIf` replaces a record when the stored state equals the expected state. It accepts any pair of states and does not check whether the move is allowed. The `vault` package will own those rules.
- **Atomic updates.** `MemoryStore` takes a write lock for the check and the replace. `DynamoDBStore` sends one conditional `PutItem`: `attribute_not_exists(PK)` for `Create` and `attribute_exists(PK) AND #s = :expected` for `UpdateIf`. When that condition fails, one follow-up read decides between `ErrNotFound` and `ErrConditionFailed`.
- **Expiry.** DynamoDB TTL deletion can lag, so both stores compare `expires_at` with the clock on read. A record whose `expires_at` has passed is reported as `ErrNotFound`. A zero `expires_at` never expires.
- **Copies.** `Get` returns a copy. Changing it does not change the stored record.
- **Context.** Every method returns the context error when the context is already cancelled.

Known gap: if an expired item has not been deleted yet, `DynamoDBStore.UpdateIf` can still succeed when the state matches, while `MemoryStore.UpdateIf` returns `ErrNotFound`.

## Planned API

These endpoints are the contract for the HTTP layer. None of them exist yet.

| Method and path | Purpose |
|---|---|
| `POST /api/v1/transactions/evaluate` | Score the signals, store the vault, return the first state |
| `POST /api/v1/vault/{id}/reality-check` | Submit the three answers. Escalates to `LOCKED_15M` on a scam sign. |
| `POST /api/v1/vault/{id}/guardian-action` | Guardian `APPROVE` or `VETO`, verified with an HMAC |
| `POST /api/v1/vault/{id}/demo-fast-forward` | Judge helper that moves the 15-minute clock forward in about 3 seconds |
| `GET /api/v1/vault/{id}` | Read the vault state (polling) |
| `GET /api/v1/events/stream` | Server-Sent Events for both phone panels |

## Privacy and safety

- Identifiers are masked before they reach the store: accounts as `XXXX-XXXX-4821`, phones as `+91-XXXXXX3210`, UPI IDs as `r***@okaxis`. `ValidateMasked` rejects anything else with `ErrUnmaskedIdentifier`, so a raw value is never written.
- OTPs, PINs and passwords have no place in any record, log line or event.
- Error messages carry only the operation name and the vault id. They never include amounts, scores or identifiers, and a client error's own text is replaced by a fixed message while the original stays reachable with `errors.Is`.
- The backend packages import no logging package and no SQLite package. A test checks this.

## Testing

| Layer | What it covers |
|---|---|
| Unit tests | Demo presets, every tier boundary, threshold mapping, validation errors, state constants, mask formats, `NewFromEnv`, condition-expression strings, cancelled contexts |
| Property tests (`rapid`) | 15 properties, 21 runs when the store properties are counted once per adapter, each at 1,000 inputs: scoring against an independent reference model, determinism, lock and pass guarantees, call monotonicity, and store behavior |
| Shared contract tests | The same store properties run once against `MemoryStore` and once against `DynamoDBStore` over a fake client, so both adapters face identical tests |
| Concurrency test | 50 goroutines call `UpdateIf` on one id. Exactly one wins. Run with `-race`. |
| Import checks | The packages import no SQLite or logging package |

The fake DynamoDB client (`fake_dynamodb_test.go`) keeps items in memory, evaluates the two condition expressions the store uses, and stops with a clear error on any request shape it does not know, so a changed expression cannot pass silently.

## Development with Kiro

The project uses spec-driven development. Each feature has requirements, a design and a task list under `.kiro/specs/`:

| Spec | State |
|---|---|
| [`behavioral-risk-engine`](.kiro/specs/behavioral-risk-engine) | All tasks complete |
| [`cooling-off-vault`](.kiro/specs/cooling-off-vault) | Planned |
| [`dual-screen-simulator`](.kiro/specs/dual-screen-simulator) | Planned |

Always-on guidance lives in `.kiro/steering/`: the tech stack, the anti-panic UX and privacy rules, and the skills each part of the code must use. Two hooks in `.kiro/hooks/` run the Go tests when backend code changes and check front-end files for alarmist wording and unmasked data.

## Roadmap

1. Cooling-off vault: the state machine, the 15-minute lock with its tamper-evident hash, and guardian approval tokens.
2. HTTP API and Server-Sent Events, plus the Lambda entry point.
3. React front end with the two-phone layout and the three one-click presets.
4. One-command deploy: `infra/deploy.sh` and a SAM template for the table, the Lambda Function URL and its permissions.
5. Run the DynamoDB adapter against DynamoDB Local and real DynamoDB.

## License

[MIT](LICENSE). Copyright (c) 2026 RahulKumar Surati.
