---
inclusion: always
---

# RukPay — Tech Stack & Architectural Steering Document

## 1. Project Identity & Core Domain
- **App Name:** RukPay (रुक-पे — "Pause Before You Pay")
- **Hackathon Target:** Kiroween 2026 (Category: Frankenstein; Bonus: Best Kiro Crew App, Best Demo)
- **Purpose:** An anti-panic behavioral firewall for digital payments combining real-time scam-stress detection, cryptographic 15-minute cooling-off locks, and trusted family co-approval.

---

## 2. Strictly Enforced Stack (No Deviations)

### Backend (`/backend`)
- **Language:** Go 1.21.1 (no Go 1.22+ language features or APIs) (`go.mod` module name: `github.com/<username>/rukpay/backend`)
- **HTTP Router:** Standard library `net/http` with a small in-repo method-and-path dispatcher, because Go 1.21 `ServeMux` has no method or `{id}` patterns (e.g. `POST /api/v1/transactions/evaluate`). Avoid third-party routers.
- **Deployment Target:** **AWS Lambda** (using `github.com/aws/aws-lambda-go/lambda` + `github.com/awslabs/aws-lambda-go-api-proxy/httpadapter`) with local `http.ListenAndServe` fallback when `AWS_LAMBDA_FUNCTION_NAME` is unset.
- **Real-Time Communication:** Server-Sent Events (`SSE` via `text/event-stream`) or short-polling fallback (`/api/v1/vault/{id}`) for seamless compatibility across local servers, AWS Lambda Function URLs, and API Gateway.
- **Persistence Layer (DynamoDB Only — NO SQLite):**
  - Primary cloud persistence: **Amazon DynamoDB** (`github.com/aws/aws-sdk-go-v2/service/dynamodb` + `feature/dynamodb/attributevalue`) using single-table design (`RukPayVaults` table with `PK: VAULT#<id>`, `SK: META` and TTL attribute `expires_at`).
  - Local / Unit / PBT test adapter: Clean `VaultStore` repository interface with a thread-safe `MemoryStore` (`sync.RWMutex`) when `STORAGE_DRIVER=memory` (or DynamoDB Local via `DYNAMODB_ENDPOINT=http://localhost:8000` when `STORAGE_DRIVER=dynamodb`). **NEVER import or use SQLite.**
- **Cryptography:** Go standard library `crypto/sha256`, `crypto/hmac`, and `crypto/rand` for tamper-evident cooling-off vault lock hashes and guardian approval tokens.
- **Property-Based Testing (PBT):** `pgregory.net/rapid` (preferred) or Go stdlib `testing/quick` for verifying state-machine and risk-scoring invariants across 1,000+ randomized inputs.

### Frontend (`/frontend`)
- **Framework:** **React 18+ with TypeScript** (Strict mode enabled in `tsconfig.json`). **NEVER use Angular.**
- **Build Tool:** **Vite**
- **Styling:** **Tailwind CSS v3/v4** with `lucide-react` for clean, accessible iconography.
- **State & Networking:** Native `fetch` + `EventSource` (SSE with graceful polling fallback) for real-time updates; avoid Redux or heavy state libraries.
- **Client-Side Privacy:** Web Crypto API (`crypto.subtle`) for hashing/masking sensitive user identifiers before transmitting telemetry.

---

## 3. Directory & File Conventions

```text
rukpay/
├── .kiro/
│   ├── specs/                  # Spec-driven requirements, design, and task files
│   ├── steering/               # Always-on architectural, UX, and privacy guardrails
│   └── hooks/                  # Automated Kiro hooks (test-on-save, PII checks)
├── backend/
│   ├── cmd/
│   │   ├── server/main.go      # Local HTTP server entrypoint
│   │   └── lambda/main.go      # AWS Lambda entrypoint wrapping the same router
│   ├── internal/
│   │   ├── risk/               # Behavioral risk scoring engine + rapid PBT tests
│   │   ├── vault/              # Cooling-off state machine + cryptographic lock
│   │   ├── guardian/           # Family co-approval & veto handlers + SSE hub
│   │   └── store/              # VaultStore interface + DynamoDB & MemoryStore adapters
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   │   ├── UserPhonePanel.tsx      # Left screen: UPI/Bank transfer simulator
│   │   │   ├── RealityCheckModal.tsx   # 3-question anti-panic calm intervention
│   │   │   ├── CoolingOffVaultCard.tsx # Live 15-min timer + fast-forward demo control
│   │   │   └── GuardianPhonePanel.tsx  # Right screen: Trusted family co-approver UI
│   │   ├── lib/
│   │   │   ├── api.ts                  # Typed API client
│   │   │   └── privacyMasker.ts        # Client-side PII redaction utility
│   │   └── App.tsx                     # Dual-screen split layout + 1-click demo presets
│   └── package.json
├── infra/
│   └── dynamodb-table.json     # AWS CLI / CloudFormation schema for RukPayVaults table
└── README.md

4. API & State Machine Contract
Cooling-Off Vault States (Strict FSM)
INSTANT_PASS — Risk score < 40; settles immediately with zero friction.

REALITY_CHECK — Risk score 40–74; pauses UI for 3 calm situational questions before proceeding.

LOCKED_15M — Risk score >= 75 (or failed reality check); funds frozen in cryptographic time-lock for 900 seconds (15m), persisted in DynamoDB, and Guardian alert dispatched.

GUARDIAN_APPROVED — Trusted family co-approver explicitly verifies & approves; unlocks transfer.

VETOED — Guardian or User clicks "Block & Report (1930)"; permanently cancels transfer and generates cyber-helpline evidence summary.

EXPIRED — 15-minute timer completes without veto; requires final calm re-confirmation from user.

Core Endpoints
POST /api/v1/transactions/evaluate — Evaluates risk signals (amount_inr, beneficiary_age_hours, is_late_night, active_unknown_call, rapid_retry_count) and persists state to DynamoDB (RukPayVaults).

POST /api/v1/vault/{id}/reality-check — Submits answers to the 3 anti-panic questions; escalates to LOCKED_15M if scam coercion indicators are detected.

POST /api/v1/vault/{id}/guardian-action — Accepts APPROVE or VETO from Guardian panel with HMAC verification and conditional DynamoDB update (ConditionExpression preventing race conditions).

POST /api/v1/vault/{id}/demo-fast-forward — Hackathon judge helper endpoint to advance the 15-minute clock in 3 seconds during live demos.

GET /api/v1/vault/{id} & GET /api/v1/events/stream — Real-time state sync for User and Guardian panels.

5. UX, Demo & Code Quality Standards
Dual-Screen Split Layout: Desktop view MUST display the User Payment Phone on the left and the Family Guardian Phone on the right simultaneously so hackathon judges can observe end-to-end co-approval and veto flows in a single viewport.

Zero-Login Judge Experience: Include 3 one-click preset buttons at the top of the simulator:

Safe Daily Payment (₹450 to known local store -> INSTANT_PASS)

Medium-Risk Unusual Transfer (₹18,000 to new contact -> REALITY_CHECK)

High-Risk "Digital Arrest" Panic Transfer (₹85,000 at 11:45 PM to brand-new account during active unknown call -> LOCKED_15M + Guardian Alert)

Error Handling & Security: Never log raw account numbers, UPI IDs, or phone numbers in Go server logs, DynamoDB items, or SSE payloads; always mask via privacyMasker.ts and backend redaction middleware.

6. Deployment & Hosting Architecture (Zero-Cost / AWS Free Tier Ready)
Backend Deployment (AWS Lambda + Function URL)
Compute: Single compiled Go binary (GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap ./cmd/lambda) deployed to AWS Lambda (provided.al2023 on ARM64 Graviton2).

Public HTTPS Endpoint: Expose via AWS Lambda Function URL (AuthType: NONE, CORS enabled for the frontend domain) — avoids API Gateway complexity and supports streaming responses cleanly.

Database: Amazon DynamoDB (RukPayVaults table, On-Demand capacity mode, TTL enabled on expires_at attribute) in ap-south-1 (Mumbai) for low-latency demo performance.

Environment Variables:

STORAGE_DRIVER=dynamodb

DYNAMODB_TABLE_NAME=RukPayVaults

HMAC_SECRET_KEY=<generated-secret>

ALLOWED_ORIGIN=https://<frontend-domain>

Frontend Deployment (AWS Amplify Hosting / S3 + CloudFront)
Build Output: Static Vite bundle (npm run build -> frontend/dist/).

Hosting: Deploy frontend/dist/ to AWS Amplify Hosting connected directly to the main branch of the public GitHub repository.

Runtime Config: Inject backend Lambda Function URL via Vite build-time env var VITE_API_BASE_URL.

Infrastructure-as-Code & One-Command Deploy (/infra)
Include infra/deploy.sh (using AWS CLI) and infra/template.yaml (AWS SAM / CloudFormation) so anyone cloning the repo—or hackathon judges—can provision the DynamoDB table, Lambda Function URL, and IAM permissions in one command.

