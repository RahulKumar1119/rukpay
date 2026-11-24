# Implementation Plan: Risk Intelligence

## GitHub Issues

**Commits and issues:** put `Refs #N` in the commit message of every task (N is the issue on the `GitHub issue:` line of the task's group). An issue may be closed only when every task of its group and the issue's acceptance criteria are done and the tests pass.

This plan builds Phase 1 of the RukPay roadmap. The work is tracked on roadmap issue #1 (https://github.com/RahulKumar1119/rukpay/issues/1) and its four sub-issues:

| Issue | Feature | Depends on | Task groups |
|---|---|---|---|
| [#2](https://github.com/RahulKumar1119/rukpay/issues/2) | Explainable risk scores | none. Blocks #3, #4 and #5 | 1 to 6 |
| [#4](https://github.com/RahulKumar1119/rukpay/issues/4) | Behavioural risk detection | #2 | 7 to 10 |
| [#5](https://github.com/RahulKumar1119/rukpay/issues/5) | Trusted beneficiary management | #2 | 11 to 20 |
| [#3](https://github.com/RahulKumar1119/rukpay/issues/3) | Custom transaction input and fraud scenario builder | #2, and #5 for its saved-recipient chooser | 21 to 23 |

Order of work: #2, then #4, then #5, then #3. #4 changes the scoring engine and the history storage that #5 extends. #3 comes last because its recipient chooser reads the Beneficiary API of #5. Group 18 (front-end groundwork) serves #3 and #5, and groups 24 to 26 serve all four issues. Each group names its issue on a `GitHub issue:` line.

## Overview

Skills: load `golang-patterns` before any Go work and `web-design-guidelines` before any frontend work (see `.kiro/steering/skills.md`). `anti-panic-ux.md` wins where the two conflict.

Languages are fixed by the design: Go 1.21.1 in `backend/` (module `github.com/RahulKumar1119/rukpay/backend`; no range-over-int, no `ServeMux` method patterns, no `Request.PathValue`, no SQLite) and strict TypeScript on React 18 in `frontend/` (runtime dependencies stay `react`, `react-dom` and `lucide-react`).

The plan follows the data path of one payment evaluation. Groups 1 to 6 (#2) make the score explain itself: the stored `Explanation`, `risk.Assess` for the five baseline signals, the vault response field, and the explanation card. Groups 7 to 10 (#4) add the history store, the capped behavioural points and history recording. Groups 11 to 20 (#5) add care points, the `beneficiary` package, payments that name a recipient, the six HTTP routes, and the manager screen with phone navigation. Groups 21 to 23 (#3) add the custom payment form, the three stories and the Simulate flow. Groups 24 to 26 hold the repo-wide import test, the README and the final verification.

Dependency direction stays `httpapi` → `guardian` → `vault` → `beneficiary` (pure helpers) → `risk` → `store`. `store` imports no other RukPay package.

Tests sit next to the code they test. Backend property tests use `pgregory.net/rapid` with at least 1000 checks (each new test package copies the `init()` of the shipped `harness_test.go`, and each test starts with `// Feature: risk-intelligence, Property N: <title>`). Frontend property tests use `fast-check`: 1000 runs for pure functions (`PBT_RUNS` in `src/test/pbt.ts`) and at least 100 for rendered components. Frontend tests run against the stubbed `fetch` and `EventSource` in `src/test/`. Test files are split per property or topic so independent test tasks never edit the same file, and every test function name is unique within its package.

## Tasks

- [x] 1. Store the Explanation on the vault record
  - GitHub issue: #2 (https://github.com/RahulKumar1119/rukpay/issues/2)
  - [x] 1.1 Add `Contribution` and `VaultRecord.Explanation` to `backend/internal/store`
    - Files: `store/types.go`, `store/memory.go`, `store/types_test.go`
    - `Contribution{Signal string, Points int, Reason string}` with `json` and `dynamodbav` tags `signal`, `points`, `reason`
    - `Explanation []Contribution` with `dynamodbav:"explanation,omitempty"` as the last field of `VaultRecord`; a nil slice means absent and a non-nil empty slice means present and empty
    - Add `VaultRecord.clone()` that copies the slice (nil stays nil, empty stays empty). `MemoryStore.Create`, `Get` and `UpdateIf` store and return clones, so a caller cannot change a stored Explanation
    - **Design conflict C-1 (needs the user's confirmation):** append `"explanation"` to the attribute list pinned in `TestVaultRecordAttributeNames` in `store/types_test.go`. This is the one edit of an existing test that Requirement 23.7 needs, because `UpdateIf` writes the whole item. Change nothing else in that test
    - **Done, with one more compile-only edit the user approved (conflict C-9):** `VaultRecord` holds a slice now, so it can no longer be compared with `==` or `!=`. 18 comparisons in existing tests (`vault`, `guardian`, `httpapi`) were rewritten from `a != b` to `!reflect.DeepEqual(a, b)` with the `reflect` import added, and no assertion changed. Later tasks that compare whole records in tests must use `reflect.DeepEqual` too
    - _Requirements: 2.3, 2.4, 2.5, 2.10, 23.4, 23.7_

  - [x] 1.2 Write the Explanation canary and round-trip tests
    - File: `store/explanation_canary_test.go`
    - Pin the SDK rule (`attributevalue` v1.14.3): a nil slice is omitted, an empty slice is stored as an empty list and reads back as an empty non-nil slice. An item without the attribute reads back nil with no error
    - Round trip a nil, an empty and a filled Explanation through `MemoryStore` and through `DynamoDBStore` over `fakeDynamoDB`; check equal Contributions in equal order and that changing a returned slice changes nothing stored
    - _Requirements: 2.4, 2.5, 23.4_

  - [x] 1.3 Write property test for Explanation storage
    - File: `store/pbt_explanation_test.go`
    - **Property 13: The Explanation survives storage, state changes and the view** (storage half: any generated Explanation reads back equal from both adapters; the vault half is task 3.3)
    - **Validates: Requirements 2.3, 2.4, 2.5, 23.4**

- [ ] 2. Make the scoring engine return contributions
  - GitHub issue: #2 (https://github.com/RahulKumar1119/rukpay/issues/2)
  - [ ] 2.1 Write the baseline explanation sentences in `backend/internal/risk/sentences.go`
    - The file declares only `const` strings: `sentAmountMid`, `sentAmountHigh`, `sentAmountTop`, `sentAgeNew`, `sentAgeRecent`, `sentLateNight`, `sentUnknownCall`, `sentRetriesMid`, `sentRetriesHigh`, `sentBehaviourCap`, `sentScoreCap`, with the exact texts of the design's sentence table. Signal ids go in `explain.go`, not here
    - Each is one sentence of 1 to 20 words, starts with a capital letter, ends with one `.`, and holds no digit, rupee figure, `@` or Avoid_Word
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.7_

  - [ ] 2.2 Implement `Assess` for the baseline signals in `backend/internal/risk/explain.go`
    - `type Contribution = store.Contribution`; the Signal_Id constants for every id in the design; a `tier{min, points, sentence}` type and `amountTiers`; the shipped `agePoints` and `retryPoints` mapped to a sentence through a two-row lookup; late night 10 and unknown call 20 as fixed constants
    - `Input{Signals}` and `Assessment{Score, State, Baseline, Explanation}` (later groups add fields). `Assess` calls `Validate` and returns its `*ValidationError`, builds the five Contributions in the fixed order (`amount`, `beneficiary_age`, `late_night`, `unknown_call`, `rapid_retries`), appends one only when its points are above 0, returns a non-nil list (empty for a score of 0), and sets `State` with `MapState`
    - Do not edit `risk.go`. Pure function: no clock, I/O, randomness or map-order dependence
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.7, 1.8, 1.9, 1.10, 1.11, 9.1, 9.3, 9.4, 9.5, 9.10, 9.12, 9.13_

  - [ ] 2.3 Add the independent references and generators for the risk tests
    - Files: `risk/reference_test.go`, `risk/generators_test.go`
    - `reference_test.go`: a baseline model written from the tier tables of `behavioral-risk-engine` Requirement 2 (sharing no code, table or constant with `risk`), and `frozenEvaluate`, a frozen copy of the shipped `Evaluate`
    - `generators_test.go`: `genSignals` that reaches every tier boundary (1999/2000, 9999/10000, 24999/25000, age 23/24 and 71/72, retries 1/2/3, `MaxInt64` amount) and `genInput`, the one generator every `Assess` property test calls. Groups 8 and 12 widen `genInput` with history and recipients, so the property tests need no edit
    - _Requirements: 27.2, 27.4_

  - [ ] 2.4 Write property test for the sum and the caps
    - File: `risk/pbt_explain_sum_test.go`
    - **Property 1: Contributions add up to the score and the caps are exact**
    - Write it against any `Assess` output so it also covers the cap lines once group 8 adds them (a `behaviour_cap` line only when the behavioural sum is above 25, a `score_cap` line only when the total is above 100)
    - **Validates: Requirements 1.1, 1.4, 1.5, 1.6, 1.10, 1.12, 2.8, 9.1, 9.2, 11.7**

  - [ ] 2.5 Write property test for shape, order and uniqueness
    - File: `risk/pbt_explain_shape_test.go`
    - **Property 2: Explanation shape, order and uniqueness** (includes the JSON key set and `[]` for an empty list)
    - **Validates: Requirements 1.2, 1.3, 1.7, 1.10, 2.9**

  - [ ] 2.6 Write property test for determinism
    - File: `risk/pbt_explain_determinism_test.go`
    - **Property 3: Scoring is deterministic and does not touch its inputs**
    - **Validates: Requirements 1.8, 9.5, 9.10**

  - [ ] 2.7 Write property test for the baseline
    - File: `risk/pbt_baseline_test.go`
    - **Property 4: The baseline matches the shipped rules, and no history changes nothing** (compares with the reference model and with `frozenEvaluate`)
    - **Validates: Requirements 1.9, 9.3, 9.4, 9.12, 23.1**

  - [ ] 2.8 Write property test for invalid signals
    - File: `risk/pbt_assess_invalid_test.go`
    - **Property 10: Invalid signals produce the validation error**
    - **Validates: Requirements 1.11, 9.13**

  - [ ] 2.9 Write the sentence scan
    - File: `risk/sentences_test.go`
    - **Property 11: Every explanation sentence is calm**
    - Parse `sentences.go` with `go/parser`, check every string constant against Requirement 3 criteria 2 to 6 (word count, one terminal `.`, no Avoid_Word as a substring, no digit run over 4, no `@`, no masked format, no rupee figure or amount word, `redact.Text` returns it unchanged) and fail when no constant is found. Also check that two inputs with the same Signal_Id and tier give identical `reason` bytes
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**
    - _Requirements: 27.7_

  - [ ] 2.10 Write the import test for `risk`
    - File: `risk/imports_test.go`
    - No non-test file of `backend/internal/risk` imports `os`, `io`, `io/ioutil`, `io/fs`, `net` or any `net/` path, `os/exec`, `syscall`, `database/sql`, `log`, `log/slog`, any `github.com/aws/` package or a logger banned in `store/imports_test.go`. `store` is allowed. Fail when the directory has no Go file
    - _Requirements: 9.10, 27.12_

- [ ] 3. Return the Explanation from the vault service and the API
  - GitHub issue: #2 (https://github.com/RahulKumar1119/rukpay/issues/2)
  - [ ] 3.1 Add `Explanation` to `vault.View` in `backend/internal/vault/view.go`
    - `Explanation *[]risk.Contribution` with `json:"explanation,omitempty"`. `view()` gives a nil pointer for a nil record slice (key omitted) and a pointer to a copy that is never nil for a non-nil slice, so an empty Explanation is written as `[]`
    - Keep every existing View field unchanged. `apply`, `load` and the lazy-expiry write copy the whole record, so the Explanation and the score never change after creation
    - _Requirements: 2.1, 2.2, 2.5, 2.6, 2.7, 2.8, 2.9_

  - [ ] 3.2 Move `Evaluate` into `backend/internal/vault/evaluate.go` and score with `Assess`
    - Keep the exported signature. Order: `store.ValidateMasked`, then `risk.Assess` (its `*ValidationError` is returned as before), then read the clock once, build the record with `Explanation = a.Explanation`, then one `Create`. The `Reason` still comes from `reasonFor`, so it is byte for byte as before
    - Return the shipped errors for a rejected request or a failed write, with no View and no Explanation. Remove the old body from `vault.go`
    - _Requirements: 2.1, 2.3, 2.10, 2.11, 2.12, 3.9, 3.11, 9.3, 23.1, 23.3_

  - [ ] 3.3 Write property test for the stored Explanation through state changes
    - File: `vault/pbt_explain_test.go`
    - **Property 13: The Explanation survives storage, state changes and the view** (vault half: after any sequence of reality check, approve, veto, fast-forward, lazy expiry and reconfirm the Explanation and score are unchanged; the view key is absent for nil, `[]` for empty and the stored list otherwise; the viewed points add up to `risk_score`)
    - **Validates: Requirements 2.2, 2.5, 2.7, 2.8**

  - [ ] 3.4 Write the JSON contract test for `explanation` and update the existing key-set tests
    - File: `httpapi/contract_json_test.go` (extended in task 16.9)
    - Pin the exact key set of a Contribution (`signal`, `points`, `reason`) and the optional `explanation` key of a View; proves the key is omitted for an old record
    - Run the existing tests. Where a test pins the exact JSON key set of a View or an evaluate response, add the one key `explanation` and change nothing else (Requirement 23.7). List each such edit in the commit message
    - _Requirements: 2.6, 2.9, 23.7, 27.11_

  - [ ] 3.5 Write router example tests for the Explanation
    - File: `httpapi/explanation_test.go`
    - The ₹85,000 late-night call payment returns `explanation` in the evaluate response, in `GET /api/v1/vault/{id}` and in the first stream event, equal in order and values. A request body that holds an `explanation` field is ignored. An old record stored with no Explanation reads back with no `explanation` key and every other field unchanged. A rejected request returns the shipped error with no `explanation`
    - _Requirements: 2.1, 2.2, 2.5, 2.6, 2.11, 2.12_

- [ ] 4. Checkpoint - Backend for #2
  - Run in `backend/`: `gofmt -l .` (no output), `go vet ./...`, `go test -race -count=1 ./... -rapid.checks=1000`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Show the Explanation on both phones
  - GitHub issue: #2 (https://github.com/RahulKumar1119/rukpay/issues/2)
  - [ ] 5.1 Add the Explanation types to `frontend/src/lib/api.ts` and the test fixtures
    - Export `ExplanationItem {signal: string; points: number; reason: string}` and add an optional `explanation?: ExplanationItem[]` to `VaultView` (absent when the server omits it). Keep every other export unchanged
    - Let `makeView` in `src/test/stubs.ts` accept an `explanation`
    - _Requirements: 22.1, 23.5_

  - [ ] 5.2 Implement `frontend/src/lib/explanation.ts` and the `explanation` copy
    - In `copy.ts` add `COPY.explanation` (heading "Why RukPay looked closer", guardian heading "What RukPay noticed", the empty-list sentence, `total`, `point`, `points`) and `NEW_COPY_GROUPS`, the list of groups this feature adds (later groups append to it). Use the exact texts of the design's copy table
    - `readExplanation(view)` returns `absent` (no key, not an array, or an item without a string `signal`, an integer `points` and a non-empty string `reason`), `empty` or `items`. `pointsText(n)` gives "+20 points", "-5 points", "+1 point". `guardianTop(items)` keeps items with points above 0, stable-sorted by points descending, first three. No points are summed or changed in the browser
    - _Requirements: 4.1, 4.2, 4.3, 4.5, 4.7, 4.9, 25.1_

  - [ ] 5.3 Add the copy test helpers
    - Files: `src/test/avoidWords.ts` (the Avoid_Words: the shipped banned terms in `src/test/bannedTerms.ts` plus the extra words of the glossary), `src/test/readability.ts` (a vowel-group syllable counter and the Flesch-Kincaid grade), `src/test/readability.test.ts` (known sentences)
    - The second verification question may need a one-line exemption comment if the counter puts it over 6.9 (design conflict C-8; Requirement 17.8 fixes its wording)
    - _Requirements: 25.8, 28.7_

  - [ ] 5.4 Write property test for new copy
    - File: `src/lib/copy.new.pbt.test.ts`
    - **Property 38: New copy is calm, simulated and short**
    - Loop over every string of the groups in `NEW_COPY_GROUPS` and the sample output of each text helper (so later groups are checked with no edit). Also test that every string of `COPY` is in `ALL_COPY`. The credential-word rule (no OTP, PIN, password, CVV, "account number", "UPI id" or "phone number") applies to the `scenario` group and starts to bite when group 19 creates it
    - **Validates: Requirements 5.10, 7.7, 25.2, 25.3, 25.4, 25.6, 25.8, 25.9**

  - [ ] 5.5 Write property test for reading the explanation
    - File: `src/lib/explanation.pbt.test.ts`
    - **Property 32: The explanation is read and shown as received**
    - **Validates: Requirements 4.1, 4.3, 4.5, 4.7, 4.8, 4.9**
    - _Requirements: 28.1_

  - [ ] 5.6 Implement `ExplanationCard.tsx` and `GuardianSummary.tsx` and wire both phones
    - Files: `src/components/ExplanationCard.tsx`, `src/components/GuardianSummary.tsx`, `UserPhonePanel.tsx`, `GuardianPhonePanel.tsx`
    - Card: `<section aria-labelledby>` with one `<li>` per Contribution in received order (the `reason` as plain text and a bold points chip), `behaviour_cap` and `score_cap` as ordinary lines, a last line "Total: N points" from `view.risk_score` at 20px, and for an empty list only the calm sentence. It shows no `signal` value and has no toggle, timer or dismiss control
    - `UserPhonePanel` shows the card after the state block whenever the vault has a usable explanation and removes it only when the vault is discarded. The reality-check modal still covers the whole phone. `GuardianPhonePanel` shows the summary (at most three lines from `guardianTop`) inside the alert box only while the vault is LOCKED_15M, and leaves the two action buttons, labels and order untouched
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 26.1, 26.2, 26.3, 26.4, 26.6_

  - [ ] 5.7 Write component tests for the card and the summary
    - Files: `src/components/ExplanationCard.test.tsx`, `src/components/GuardianSummary.test.tsx`
    - Four or more items with reasons, "+20 points" text, received order and a total equal to `risk_score`; an empty list shows the calm sentence and no line; no `explanation` shows no card and no error; the two cap lines; the summary for 0, 1, 3 and 4 or more items (at most 3 lines, most points first, ties in received order); markup in a `reason` stays text
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.7, 28.4_

  - [ ] 5.8 Extend the smoke tests for the card
    - File: `src/test/smoke.test.ts`
    - Add scenes for the card with a full and with an empty list. Keep every shipped rule: no storage, console, pure red, hit areas of 48px by class, 12px gaps, 7:1 contrast
    - _Requirements: 26.1, 26.3, 26.4, 28.8_

- [ ] 6. Checkpoint - Front end for #2
  - Run in `frontend/`: `npm run typecheck`, `npm run build`, `npm test`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Store the payer's recent history
  - GitHub issue: #4 (https://github.com/RahulKumar1119/rukpay/issues/4)
  - [ ] 7.1 Add the masked-identifier helpers in `backend/internal/store/mask.go`
    - `MaskFormat` (`MaskNone`, `MaskAccount`, `MaskPhone`, `MaskUPI`), `MaskedFormat(v)` and `ValidateMaskedIdentifier(v)` (returns `ErrUnmaskedIdentifier`, rejects the empty string), using the same three patterns as `ValidateMasked`, which stays unchanged
    - Write `store/mask_identifier_test.go` next to it
    - _Requirements: 13.9, 24.1_

  - [ ] 7.2 Define the history types in `backend/internal/store/history.go`
    - `PayerKey = "demo-payer"`, `HistoryRetentionSeconds = 604_800`, `SnapshotLimit = 100`, `HistoryEvent{ID, At, AmountINR, BeneficiaryRef, ExpiresAt}` with `dynamodbav` tags, `HistoryExpiry(at)` (saturating add), the `HistoryStore` interface (`AppendHistory`, `RecentHistory`), and the sort-key helper `historySortKey(at, id)` = `E#` + `%020d` of `uint64(at) ^ (1<<63)` + `#` + id. Leave `VaultStore` with its three methods
    - _Requirements: 12.6, 12.10, 13.1, 13.3, 13.4, 24.8_

  - [ ] 7.3 Implement history in `MemoryStore`
    - Files: `store/memory.go` (add the `history` map), `store/memory_history.go`
    - `AppendHistory`: context check, `ErrUnmaskedIdentifier` first for a non-empty unmasked ref, `ErrAlreadyExists` for a stored `(at, id)`. `RecentHistory`: limit below 1 returns an empty list; sort by `(At, ID)` descending, keep `limit`, drop events with `ExpiresAt != 0 && ExpiresAt < now`, return ascending. Errors are `opError` values with no event field in the text
    - _Requirements: 13.2, 13.7, 13.9, 13.11, 13.12, 13.13, 24.5_

  - [ ] 7.4 Implement history in `DynamoDBStore`
    - Files: `store/dynamodb.go` (add `Query` to `DynamoDBClient`, add `queryByPartition = "PK = :pk"`), `store/dynamodb_history.go`
    - Items: `PK = HIST#<payer>`, `SK` from `historySortKey`, `createCondition` on the write. `RecentHistory` sends exactly one `Query` (`ScanIndexForward: false`, `Limit`, `ConsistentRead: true`, no `IndexName`), drops expired items, reverses to ascending, and sends no request for a limit below 1. No `Scan` exists in the client interface
    - The test package compiles only when `fakeDynamoDB` has the new method, so add a stub `Query` to the fake that panics with `fakeDynamoDB: unsupported ...`; task 7.5 replaces it with the real one
    - _Requirements: 13.2, 13.3, 13.4, 13.5, 13.6, 13.7, 13.9, 13.11, 13.12, 13.13, 24.5_

  - [ ] 7.5 Extend the `fakeDynamoDB` test helper for history
    - Files: `store/fake_dynamodb_test.go` (helper code only; leave every existing `Test...` function unedited), `store/fake_dynamodb_ext_test.go`
    - Key families `VAULT#`/`META`, `HIST#`/`E#...`; any other pair panics with `fakeDynamoDB: unsupported ...`. Add `Query` for exactly `queryByPartition` with `ScanIndexForward`, `Limit`, `ConsistentRead`, `ExclusiveStartKey`, `LastEvaluatedKey` and a `pageSize` knob; any `IndexName`, filter, projection or other expression panics. Record `queryCalls` and `lastQuery`; support `failNext` and `failOn` for `Query`
    - Self-tests in the new file; the existing `SK = OTHER` self-test must still pass
    - _Requirements: 13.10_

  - [ ] 7.6 Write property test for the history store contract
    - File: `store/pbt_history_test.go`
    - **Property 14: The history store contract** (one function run against `MemoryStore` and against `DynamoDBStore` over the fake)
    - **Validates: Requirements 12.10, 13.3, 13.4, 13.7, 13.9, 13.11, 13.12, 13.13, 24.5**

  - [ ] 7.7 Write history contract example tests
    - File: `store/history_contract_test.go`
    - Round trip with an empty ref and `MaxInt64` amount; 50 concurrent writes with equal times and different ids all survive and a limit-100 read returns all 50; expiry boundary (equal to now is kept, 0 never expires); a cancelled context sends no request; unknown fake shapes stop the test with a clear error; one `Query`, no `IndexName`
    - _Requirements: 13.5, 13.8, 13.10, 13.13, 27.14_

- [ ] 8. Score behaviour with capped extra points
  - GitHub issue: #4 (https://github.com/RahulKumar1119/rukpay/issues/4)
  - [ ] 8.1 Add the window signals in `backend/internal/risk/behaviour.go`
    - Extend `explain.go`: constants `BehaviourCap = 25`, `VelocityWindow = 600`, `FrequencyWindow = 86_400`, `RepeatWindow = 3_600`; `HistoryEvent{At, AmountINR, BeneficiaryRef}`; `Input` gains `Now`, `History`, `BeneficiaryRef`; `Assessment` gains `Behaviour`
    - `within(at, now, w)` (uint64 subtraction after the `at > now` test, an event exactly `w` seconds old is outside), tier tables `velocityTiers`, `dailyTiers`, `repeatTiers` (with their sentences added to `sentences.go`). Repeat counts only a non-empty, exactly equal ref
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 3.1, 3.7_

  - [ ] 8.2 Implement amount deviation in `behaviour.go`
    - `deviationPoints(amount, history)` with `DeviationMinEvents = 5` and `DeviationMinAmount = 2000`, using `amount/10 >= largest` and `amount/3 >= largest` so no multiplication can overflow; add the two deviation sentences to `sentences.go`
    - _Requirements: 11.1, 11.2, 9.11, 3.1, 3.7_

  - [ ] 8.3 Implement the Pattern_Table in `backend/internal/risk/patterns.go`
    - `FraudPattern{ID, WindowSeconds, MinSmallCount, SmallMax, LargeMin, Points, Reason}`, `patternTable` with `test_then_large` (3600 s, 3 small, 1000, 10000, 15 points, one calm `Reason`), `Patterns()` returning a copy, and one generic scorer that awards each entry at most once under `pattern_<id>`
    - _Requirements: 11.3, 11.4, 11.5, 11.6, 11.8, 3.7_

  - [ ] 8.4 Finish the `Assess` pipeline with the behaviour cap and the score cap
    - In `explain.go`: steps 6 to 11 and 13 of the design's table. Sum the behavioural points, add a `behaviour_cap` Contribution (`25 - sum`) only when the sum is above 25, then a `score_cap` Contribution (`100 - total`) only when baseline + behaviour is above 100. `Score = min(100, baseline + min(25, behaviour))`, `State = MapState(score)`
    - _Requirements: 1.5, 1.6, 1.12, 9.1, 9.2, 9.12, 10.8, 11.7_

  - [ ] 8.5 Widen the risk test generators and add the behaviour reference models
    - Files: `risk/generators_test.go`, `risk/reference_behaviour_test.go`
    - `genInput` gains a History_Snapshot of 0 to 100 events with ages at 599/600/601, 3599/3600/3601, 86399/86400 and 604800 seconds and in the future, repeated and empty refs, behavioural sums below, at and above 25, totals above 100, and `MaxInt64` amounts. Add an equal-time reordering case for Property 3
    - `reference_behaviour_test.go`: independent models for windows and tiers, deviation (with `math/big`) and patterns that share no code or constant with `risk`
    - Run the Property 1 to 4 and 10 tests unchanged; they must still pass
    - _Requirements: 27.2, 27.3_

  - [ ] 8.6 Write property test for window counts
    - File: `risk/pbt_windows_test.go`
    - **Property 6: Window counts and tiers** (also: points never fall as events are added one at a time, Requirement 27.6)
    - **Validates: Requirements 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8**
    - _Requirements: 27.6_

  - [ ] 8.7 Write property test for amount deviation
    - File: `risk/pbt_deviation_test.go`
    - **Property 7: Amount deviation is exact for every int64**
    - **Validates: Requirements 9.11, 11.1, 11.2**

  - [ ] 8.8 Write property test for fraud patterns
    - File: `risk/pbt_patterns_test.go`
    - **Property 8: Fraud patterns are table-driven and score once** (the test appends a row to `patternTable`, restores it in `t.Cleanup`, and checks that the row scores with no scoring change)
    - **Validates: Requirements 11.3, 11.4, 11.5, 11.6, 11.8**

  - [ ] 8.9 Write property test for the guarantees
    - File: `risk/pbt_assess_guarantees_test.go`
    - **Property 5: Lock, pass and preset guarantees** (baseline of 75 or more locks, baseline of 0 gives at most 35 and INSTANT_PASS, the three Preset signal sets give INSTANT_PASS, REALITY_CHECK and LOCKED_15M, and the three story signal sets score at least 90, 65 and 40)
    - **Validates: Requirements 7.2, 7.3, 7.4, 7.9, 9.6, 9.7, 9.8, 9.9**
    - _Requirements: 27.5_

  - [ ] 8.10 Extend the sentence scan to the Pattern_Table
    - File: `risk/sentences_test.go`
    - Also check every `Patterns()` entry `Reason` against Requirement 3 criteria 2 to 6, and that each `ID` is unique lower-case snake case. Fail when the table is empty
    - **Property 11: Every explanation sentence is calm**
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**
    - _Requirements: 27.7_

- [ ] 9. Record history when a payment is evaluated
  - GitHub issue: #4 (https://github.com/RahulKumar1119/rukpay/issues/4)
  - [ ] 9.1 Read the snapshot and record the event in `backend/internal/vault/evaluate.go`
    - `Service.SetHistory(store.HistoryStore)`; a nil store means history is off. Read `now` once; `snapshot()` calls `RecentHistory(ctx, store.PayerKey, 100)`, keeps events with `at > now || now-at < 604800` and maps them to `risk.HistoryEvent`; any error gives an empty snapshot
    - Beneficiary_Ref is the first non-empty of `beneficiary_masked`, `upi_masked`, `phone_masked`. Call `risk.Assess` with the snapshot, `Now` and the ref. After `Create` succeeds, append one `store.HistoryEvent` (its own 128-bit random id, `At = now`, the request amount, the ref, `ExpiresAt = store.HistoryExpiry(now)`); ignore an append error. Run in line: no goroutine, no timer
    - **Design conflict C-6:** the service sets the event's `expires_at`; the store keeps the value it is given
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5, 12.6, 12.7, 12.8, 12.10_

  - [ ] 9.2 Add the behavioural summary phrases in `backend/internal/vault/messages.go`
    - Constants `msgPhraseVelocity`, `msgPhraseDaily`, `msgPhraseRepeat`, `msgPhraseDeviation`, `msgPhrasePattern` (each at most 8 words, no Avoid_Word). Add `reasonForExplained(state, signals, explanation)`: the old sentence plus one phrase per behavioural Contribution in Explanation order, chosen by Signal_Id alone, the fallback phrase only when the combined list is empty. Use it for REALITY_CHECK and LOCKED_15M; INSTANT_PASS and every later reason stay as before
    - _Requirements: 3.8, 3.9, 3.10, 3.11_

  - [ ] 9.3 Write property test for the snapshot and the event
    - File: `vault/pbt_history_test.go`
    - **Property 16: The snapshot and the event follow the recording rules** (a recording `HistoryStore` wrapper; the Explanation must equal `risk.Assess` run on the expected snapshot)
    - **Validates: Requirements 10.5, 12.1, 12.3, 12.4, 12.5, 12.6, 12.10**

  - [ ] 9.4 Write property test for failure paths
    - File: `vault/pbt_degrade_test.go`
    - **Property 17: Every failure path gives today's result** (failed snapshot read, failed event write, failed vault write, invalid request; the beneficiary cases are added in task 14.5)
    - **Validates: Requirements 2.11, 12.2, 12.7, 12.8**

  - [ ] 9.5 Write property test for concurrent evaluations
    - File: `vault/pbt_concurrent_history_test.go`
    - **Property 18: Concurrent evaluations leave one event each** (50 to 200 goroutines released together)
    - **Validates: Requirements 12.9, 27.10**

  - [ ] 9.6 Write property and example tests for the summary phrases
    - Files: `vault/phrases_test.go`, `vault/pbt_phrases_test.go`
    - `phrases_test.go` reads every constant whose name starts `msgPhrase` and applies Requirement 3.10. The property compares `reasonForExplained` with `reasonFor`
    - **Property 12: The vault reason keeps its old form and gains calm phrases**
    - **Validates: Requirements 3.8, 3.9, 3.10, 3.11**

  - [ ] 9.7 Wire history into `httpapi` and test it
    - Files: `httpapi/router.go`, `httpapi/history_test.go`
    - Add `Deps.History store.HistoryStore`. `assemble` calls `SetHistory`, and when `Deps.Store` is nil it creates one `MemoryStore` for all stores. `Build` uses a type assertion on the store from `NewFromEnv` (task 11.5 replaces it with `store.Extensions`). `Deps{Store: x}` with a nil `History` keeps today's scoring and store call counts
    - Test: a second and third evaluation see the first (a velocity line appears), and after many evaluations the three Presets still give INSTANT_PASS, REALITY_CHECK and LOCKED_15M
    - _Requirements: 9.9, 12.1, 23.1, 23.6_

- [ ] 10. Checkpoint - Backend risk and storage for #4
  - Run in `backend/`: `gofmt -l .` (no output), `go vet ./...`, `go test -race -count=1 ./... -rapid.checks=1000`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 11. Store beneficiary records
  - GitHub issue: #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 11.1 Define the beneficiary types and interface in `backend/internal/store/beneficiary.go`
    - `TrustLevel` (`NEW`, `TRUSTED`), `ProtectionLevel` (`EXTRA_CARE`, `STANDARD`), `HistoryEntry{At, Kind, AmountINR}`, `BeneficiaryRecord` (id, label, masked identifier, added time, `VerifiedAt *int64`, trust, protection, pressure flag, version, payment count, `FirstPaymentINR *int64`, `History []HistoryEntry`, `ExpiresAt`) with `dynamodbav` tags, `BeneficiaryTTLSeconds = 2_592_000`, a deep `clone()`, and `BeneficiaryStore` with `CreateBeneficiary`, `GetBeneficiary`, `ListBeneficiaries`, `ReplaceBeneficiary(ctx, payer, id, expectedVersion, next)` and `DeleteBeneficiary(ctx, payer, id, expectedVersion)`
    - **Design note I-4:** delete takes the expected version so a PATCH and a DELETE that read one version cannot both apply
    - _Requirements: 14.1, 19.7, 20.1, 20.4, 20.5_

  - [ ] 11.2 Implement beneficiaries in `MemoryStore`
    - Files: `store/memory.go` (add the `bene` map), `store/memory_beneficiary.go`
    - `ErrUnmaskedIdentifier` before any other check; create on a stored id (expired or not) is `ErrAlreadyExists` and sets `ExpiresAt = now + 2,592,000`; replace and delete under the write lock check missing or expired (`ErrNotFound`) then version (`ErrConditionFailed`), and replace stores `Version = expected + 1` and a fresh expiry; list returns unexpired copies in ascending id order. Errors are `opError` values with the operation and id only
    - _Requirements: 20.1, 20.3, 20.4, 20.5, 20.6, 20.7, 20.10, 20.11, 20.12, 24.5, 24.8_

  - [ ] 11.3 Implement beneficiaries in `DynamoDBStore`
    - Files: `store/dynamodb.go` (add `DeleteItem` to `DynamoDBClient`, add `replaceCondition = "attribute_exists(PK) AND #v = :expected AND #e >= :now"`), `store/dynamodb_beneficiary.go`
    - Items: `PK = BENE#<payer>`, `SK = B#<id>`. Create uses `createCondition`; get is a consistent `GetItem`; list is one `Query` per page that follows `LastEvaluatedKey`; replace is a conditional `PutItem` and delete a conditional `DeleteItem`, both with `replaceCondition`, and on a failed condition a follow-up `GetItem` chooses `ErrNotFound` or `ErrConditionFailed`
    - Add a stub `DeleteItem` to `fakeDynamoDB` that panics with `fakeDynamoDB: unsupported ...` so the test package still compiles; task 11.4 replaces it with the real one
    - _Requirements: 20.2, 20.3, 20.4, 20.5, 20.6, 20.7, 20.9, 20.10, 20.11, 20.12_

  - [ ] 11.4 Extend the `fakeDynamoDB` test helper for beneficiaries
    - Files: `store/fake_dynamodb_test.go` (helper code only), `store/fake_dynamodb_ext_test.go`
    - Add the `BENE#`/`B#` key family, `DeleteItem`, `replaceCondition` parsing (`#v`, `#e`, `:expected`, `:now`, all numeric) for `PutItem` and `DeleteItem`, and the recorders `deleteCalls` and `lastDelete`. Do not offer `failNext` for `DeleteItem`: an existing self-test uses that name as its unknown-operation example. Inject delete failures with a small wrapper client type in the new test file (design note I-11)
    - _Requirements: 13.10, 20.8_

  - [ ] 11.5 Add `store.Extensions` and use it in `Build`
    - `func Extensions(vs VaultStore) (HistoryStore, BeneficiaryStore)` in `store/factory.go`, each result nil when `vs` does not implement it; `NewFromEnv` stays as it is. Replace the type assertion of task 9.7 in `httpapi.Build` with it for the history store (task 16.2 wires the beneficiary result). Unit test in `store/factory_test.go` (new file): both adapters return both extensions, and a plain `VaultStore` returns two nils
    - _Requirements: 13.2, 20.1_

  - [ ] 11.6 Update the table description in `infra/dynamodb-table.json`
    - Only the `Description` changes, to name the `HIST#` and `BENE#` partitions beside `VAULT#`. Add no key attribute and no index
    - _Requirements: 13.6, 20.2_

  - [ ] 11.7 Write property test for the beneficiary store contract
    - File: `store/pbt_beneficiary_test.go`
    - **Property 15: The beneficiary store contract** (run against both adapters; includes one `Query` listing exactly the payer's records)
    - **Validates: Requirements 20.2, 20.3, 20.4, 20.5, 20.6, 20.7, 20.10, 20.11, 20.12**

  - [ ] 11.8 Write property test for racing replaces at the store
    - File: `store/pbt_beneficiary_race_test.go`
    - **Property 27: Racing writers produce one winner** (store half: at least 20 goroutines replace or delete the same record at the same version on both adapters; one wins, the rest get `ErrConditionFailed`, and the stored record equals the winner's with version + 1)
    - **Validates: Requirements 20.3, 20.9, 27.10**

  - [ ] 11.9 Write beneficiary store contract examples
    - File: `store/beneficiary_contract_test.go`
    - Create, duplicate (expired too), get, list order, conditional update, delete, expiry with a controlled clock, `ErrUnmaskedIdentifier` before every other check, a failure for each error kind with the record unchanged, and error text with no label, identifier or amount
    - _Requirements: 20.5, 20.6, 20.7, 20.8, 27.14_

- [ ] 12. Add care points for saved recipients
  - GitHub issue: #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 12.1 Implement care points in `backend/internal/risk/care.go`
    - `CareCap = 10`, `Recipient{Protection, Verified, Pressure}`, `carePoints(r)`: 0 for a nil recipient or `STANDARD`; for `EXTRA_CARE`, 10 when there is no verified time or the flag is set, else 5. `Input` gains `Recipient *Recipient`, `Assessment` gains `Care`. Add `sentCareList` (5) and `sentCareNew` (10) to `sentences.go`, add step 12 (`beneficiary_care`) to `Assess` before the `score_cap` check, and cap the Care_Points at 10. Also add `LargePaymentThreshold = 10_000` (rupees) to `explain.go`; `beneficiary` reads it for the `FIRST_PAYMENT_LARGE` indicator
    - _Requirements: 18.6, 18.7, 18.8, 18.9, 9.2, 1.6_

  - [ ] 12.2 Write property test for care points and widen the generator
    - Files: `risk/generators_test.go` (add a `Recipient` to `genInput`, including `STANDARD`, each `EXTRA_CARE` case and a nil recipient), `risk/pbt_care_test.go`
    - **Property 9: Care points only add and follow the recipient**
    - Re-run Properties 1 to 5 and 10 with the widened generator; they must pass unchanged
    - **Validates: Requirements 18.6, 18.7, 18.8, 18.9**

- [ ] 13. Build the beneficiary rules and service
  - GitHub issue: #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 13.1 Create the `beneficiary` package skeleton in `backend/internal/beneficiary`
    - Files: `rules.go` (constants `WaitNormal = 72*3600`, `WaitPressure = 168*3600`, `MaxBeneficiaries = 50`, `MaxHistory = 50`, `MaxLabelRunes = 40`), `errors.go` (`Refusal{Cause, Field}` with the four causes, `ConflictError{Current View}`), `messages.go` (`msgNotPassed`, `msgNotReadyNotVerified`, `msgNotReadyWaiting`, `msgNotReadyPressure`, `msgNotReadyStandard` with the design's texts), `harness_test.go` (copy of the `init()` that raises `-rapid.checks` to 1000)
    - **Design conflict C-3:** four fixed `not_ready` texts, all different from each other
    - _Requirements: 15.11, 16.5, 16.9, 17.3_

  - [ ] 13.2 Write the message tests
    - File: `beneficiary/messages_test.go`
    - Scan every `msg` constant: one sentence of 1 to 160 characters, no Avoid_Word, no submitted value; the four `not_ready` texts differ pairwise; the `NOT_PASSED` message is 1 to 200 characters
    - _Requirements: 15.11, 16.5, 17.3_

  - [ ] 13.3 Implement the pure rules in `beneficiary/rules.go`
    - `ApplyPatch(rec, PatchRequest, now)` applies `label`, `trust_level`, `protection_level` in that order, each judged against the record the earlier changes left, and returns the next record or the first `*Refusal`. Implement the transition table exactly: grant needs a verified time, `now >= EligibleAt` and no pressure (checked in that order); `NEW` always works and resets protection; `EXTRA_CARE` always works; `STANDARD` needs no pressure and `TRUSTED`; a grant from `NEW` sets `STANDARD` unless `EXTRA_CARE` is asked in the same request
    - `Classify(Answers)`, `ApplyVerification(rec, outcome, now)`, `WithPayment(rec, now, amount)`, `AgeHours`, `EligibleAt`, `Check` (`STANDARD` implies `TRUSTED`; `TRUSTED` implies a verified time)
    - History entries from one request are emitted in the fixed order `ADDED, VERIFIED, VERIFICATION_NOT_PASSED, TRUSTED, TRUST_REMOVED, PROTECTION_RAISED, PROTECTION_LOWERED, RENAMED, PAYMENT`, only for values that changed, and the list keeps the last 50. **Design conflict C-7:** a `NOT_PASSED` verification appends one `VERIFICATION_NOT_PASSED` and also `TRUST_REMOVED` or `PROTECTION_RAISED` when those values change
    - _Requirements: 15.5, 15.6, 16.1, 16.2, 16.3, 16.4, 16.6, 16.7, 16.8, 16.9, 16.10, 16.11, 16.12, 16.16, 17.1, 17.2, 17.3, 17.5, 17.6, 18.11, 18.13, 19.1, 19.2, 19.7_

  - [ ] 13.4 Implement the view in `beneficiary/view.go`
    - `View` with the JSON keys of the design (`beneficiary_id`, `label`, `identifier_masked`, `added_at`, `age_hours`, `trust_level`, `protection_level`, `pressure_flag`, `trust_eligible_at`, `indicators` never null, `verified_at` only when set, `history` omitted unless asked for). `NewView(rec, now, withHistory)` computes `age_hours`, `trust_eligible_at` and the six indicators in the fixed order from the record and `now` only. The view never carries the payment count or the first payment amount
    - _Requirements: 19.3, 19.4, 19.5, 19.6, 19.2_

  - [ ] 13.5 Implement the service in `beneficiary/service.go`
    - `New(store.BeneficiaryStore, payer, Clock)`, `Add`, `List` (added time, then id), `Get` (history newest first), `Patch`, `Remove`, `Verify`, `ValidID` (32 lowercase hex). `Add` trims the label and checks it (1 to 40 runes, no digit run of 5 or more, no `@`, `redact.Text` unchanged), checks the identifier with `store.ValidateMaskedIdentifier`, refuses a duplicate masked identifier (case-sensitive) and a 51st record, sets the Pressure_Flag from the add context, and draws a new `crypto/rand` id up to three times on an id collision
    - `Patch`, `Verify` and `Remove` read once, call a pure rule, then write with the version read. On `ErrConditionFailed` they re-read once and return `*ConflictError` or `ErrNotFound`, with no retry. Each method reads `now` once from the injected clock
    - _Requirements: 14.1, 14.2, 14.3, 14.4, 14.5, 14.6, 14.7, 14.8, 14.9, 15.8, 15.10, 16.12, 16.13, 17.10, 17.11_

  - [ ] 13.6 Write property test for the transition table
    - File: `beneficiary/pbt_rules_test.go`
    - **Property 21: Trust changes follow the transition table** (compare `ApplyPatch` with an independent reference model; one second before the waiting period is refused, at it is granted)
    - **Validates: Requirements 15.5, 15.6, 16.1, 16.2, 16.3, 16.4, 16.6, 16.7, 16.8, 16.9, 16.11, 16.12, 16.16**

  - [ ] 13.7 Write property test for the trust invariants
    - File: `beneficiary/pbt_invariants_test.go`
    - **Property 22: The trust invariants hold after every operation** (sequences of 1 to 40 operations, clock steps that land on and one second before each waiting period)
    - **Validates: Requirements 16.10, 19.9, 27.8**

  - [ ] 13.8 Write property test for history entries
    - File: `beneficiary/pbt_history_entries_test.go`
    - **Property 23: History entries are complete, ordered and bounded**
    - **Validates: Requirements 19.1, 19.2, 19.7**

  - [ ] 13.9 Write property test for derived view fields
    - File: `beneficiary/pbt_view_test.go`
    - **Property 24: Derived view fields depend only on the record and the time**
    - **Validates: Requirements 19.3, 19.4, 19.5, 19.6**

  - [ ] 13.10 Write property test for verification
    - File: `beneficiary/pbt_verify_test.go`
    - **Property 25: Verification depends only on the outcome** (all eight answer combinations)
    - **Validates: Requirements 17.1, 17.2, 17.3, 17.5, 17.6, 17.10**

  - [ ] 13.11 Write property test for labels and identifiers
    - File: `beneficiary/pbt_validation_test.go`
    - **Property 26: Labels and identifiers are checked and never kept raw**
    - **Validates: Requirements 14.3, 14.4, 14.9**

  - [ ] 13.12 Write property test for racing writers at the service
    - File: `beneficiary/pbt_race_test.go`
    - **Property 27: Racing writers produce one winner** (service half: 20 or more goroutines each apply a different PATCH, verification or DELETE to one record that all read at one version, using a store wrapper that gates the reads)
    - **Validates: Requirements 15.10, 20.9, 27.10**

  - [ ] 13.13 Write service example tests
    - File: `beneficiary/service_test.go`
    - Duplicate identifier, the 50 limit, id collision retry, conflict with the current View, not found for expired or wrongly shaped ids, store read and write failures, and the pressure flag set from the add context
    - _Requirements: 14.5, 14.6, 14.7, 14.8, 14.10, 15.9, 15.10, 17.11_

- [ ] 14. Let a payment name a saved recipient
  - GitHub issue: #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 14.1 Resolve the recipient in `backend/internal/vault/evaluate.go`
    - `EvaluateRequest` gains `BeneficiaryID *string` (`json:"beneficiary_id,omitempty"`) and `BeneficiaryAgeOmitted bool` (`json:"-"`); the struct stays comparable. Add `Service.SetBeneficiaries` and `ErrInvalidBeneficiary`
    - `resolveRecipient`: no id means nothing is read and the old rules apply. A wrongly shaped, unknown or expired id, or no beneficiary store, returns `ErrInvalidBeneficiary` before anything is created. A stored record gives the Beneficiary_Ref, ignores the request's three masked fields, stores its identifier in the matching Vault_Record field, and sets the age to `max(0, floor((now-added)/3600))` when no age was sent or to the smaller of the two when one was. A read error other than not found degrades to the request values with 0 care points. Pass the `Recipient` to `Assess`
    - _Requirements: 12.5, 12.11, 18.1, 18.2, 18.3, 18.4, 18.5, 18.10_

  - [ ] 14.2 Record the payment on the beneficiary and add the care phrase
    - After `Create` succeeds, when a record was read, call `ReplaceBeneficiary` with `beneficiary.WithPayment(rec, now, amount)`; ignore any error or conflict and never retry. Add `msgPhraseCare` and the `beneficiary_care` case to `reasonForExplained`
    - _Requirements: 18.8, 18.11, 18.12, 18.13, 3.8, 3.10_

  - [ ] 14.3 Write property test for named recipients
    - File: `vault/pbt_named_test.go`
    - **Property 19: A named Beneficiary sets the age and the identifier** (differences of 3599, 3600 and 3601 seconds, a request age one below, equal to and one above, a clock earlier than the added time)
    - **Validates: Requirements 18.1, 18.2, 18.3, 18.4, 18.5**
    - _Requirements: 27.9_

  - [ ] 14.4 Write property test for payments on a recipient
    - File: `vault/pbt_payment_test.go`
    - **Property 20: A payment updates the Beneficiary once**
    - **Validates: Requirements 18.11, 18.13, 19.1**

  - [ ] 14.5 Extend the history, failure and phrase tests for recipients
    - Files: `vault/pbt_degrade_beneficiary_test.go`, `vault/pbt_phrases_test.go` (add `beneficiary_care` to the generator), `vault/pbt_history_test.go` (the ref of a named recipient is its record identifier)
    - **Property 17: Every failure path gives today's result** (a failed beneficiary read adds 0 care points and no PAYMENT; a failed, conflicting or expired PAYMENT write leaves the vault as it was)
    - **Validates: Requirements 12.11, 18.10, 18.12**

- [ ] 15. Checkpoint - Backend risk, storage and vault work for #5
  - Run in `backend/`: `gofmt -l .` (no output), `go vet ./...`, `go test -race -count=1 ./... -rapid.checks=1000`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 16. Serve the Beneficiary API
  - GitHub issue: #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 16.1 Add the quiet route, error codes and CORS methods
    - Files: `httpapi/route.go`, `httpapi/errors.go`, `httpapi/middleware.go`
    - `handleQuiet` registers a route whose `{id}` is not passed to `setVaultID`, so a Beneficiary_Id is never logged. Add the codes `duplicate_beneficiary`, `beneficiary_limit`, `not_ready`, `state_conflict` with the design's fixed messages, `errorBody.Beneficiary *beneficiary.View`, `classifyBeneficiary(err)`, and a `classify` case that maps `vault.ErrInvalidBeneficiary` to 400 `invalid_field` with field `beneficiary_id`. `Access-Control-Allow-Methods` becomes `GET, POST, PATCH, DELETE, OPTIONS`
    - _Requirements: 15.1, 15.9, 15.11, 15.12, 15.13, 18.5, 24.4_

  - [ ] 16.2 Wire the services and split the body decoding
    - Files: `httpapi/router.go`
    - `Deps.Beneficiaries store.BeneficiaryStore`; `Build` fills `Deps.History` and `Deps.Beneficiaries` from `store.Extensions` (task 11.5); `assemble` calls `SetBeneficiaries`, builds `beneficiary.New(deps.Beneficiaries, store.PayerKey, deps.Clock)` and registers the six routes. Split `decodeJSON` into `readBody` and `decodeBytes` (same behaviour); `decodeEvaluate` probes the raw body once and sets `BeneficiaryAgeOmitted` when `beneficiary_age_hours` is absent or null
    - _Requirements: 15.1, 15.14, 15.15, 18.3, 23.2_

  - [ ] 16.3 Implement the list, add and get handlers in `httpapi/beneficiaries.go`
    - List returns a bare array (`[]` when empty), ordered by added time then id, without history. Add decodes `label`, `identifier_masked`, `is_late_night`, `active_unknown_call` and returns 201 with the View, or 400 `invalid_field` (`label` when both are at fault), 409 `duplicate_beneficiary`, 409 `beneficiary_limit`. Get returns the View with history newest first. Check order: size, shape, field values, id not stored, then 409
    - _Requirements: 14.5, 14.6, 14.7, 14.8, 14.10, 15.2, 15.3, 15.4, 15.9, 15.14, 15.15, 15.16_

  - [ ] 16.4 Implement the patch, delete and verification handlers
    - **Design conflicts C-4 and C-5:** a PATCH holds `label`, `trust_level`, `protection_level` plus the optional `is_late_night` and `active_unknown_call`. The two flags and `answers` are decoded raw; anything other than the JSON `true` or `false` gives `invalid_field` naming the flag, and `answers` that is not an object, or an answer that is not exactly `YES` or `NO`, gives `invalid_field` naming `answers` or the first offending answer. Every other wrong JSON type is `invalid_request`
    - PATCH returns 200 with the View, 409 `not_ready` with `field` and the cause's message, or 409 `state_conflict` with the current View; DELETE returns 200 `{"removed": true}`; verification returns `{"outcome", "message"?, "beneficiary"}`. Store failures give 500 `internal`
    - _Requirements: 15.5, 15.6, 15.7, 15.8, 15.10, 15.14, 15.15, 15.16, 16.14, 16.15, 17.1, 17.4, 17.11_

  - [ ] 16.5 Write router example tests for the six routes
    - Files: `httpapi/beneficiaries_test.go`, `httpapi/beneficiaries_limits_test.go`
    - One success per route with the right status and keys; every status and code of Requirements 14 to 18; the four `not_ready` texts; body of exactly 65,536 bytes accepted, 65,537 bytes gives 413, invalid JSON gives 400; the CORS preflight lists `PATCH` and `DELETE`; log capture with a sentinel label, identifier and id leaves only method, pattern and status
    - _Requirements: 15.1, 15.2, 15.3, 15.4, 15.8, 15.9, 15.11, 15.12, 15.13, 15.14, 27.13_

  - [ ] 16.6 Write evaluate tests for `beneficiary_id`
    - File: `httpapi/evaluate_beneficiary_test.go`
    - A bad, unknown or expired id gives 400 `invalid_field` with field `beneficiary_id` and creates nothing; a named recipient scores with its age and care points, writes the PAYMENT entry, and the request's masked fields are ignored; an omitted `beneficiary_age_hours` differs from `0`
    - _Requirements: 18.1, 18.2, 18.3, 18.4, 18.5, 18.11_

  - [ ] 16.7 Write property test for error answers
    - File: `httpapi/pbt_beneficiaries_test.go`
    - **Property 28: Error answers are fixed, safe and in the stated order**
    - **Validates: Requirements 15.7, 15.9, 15.11, 15.15, 15.16, 16.15, 24.4**

  - [ ] 16.8 Write property test for privacy
    - File: `httpapi/pbt_privacy_test.go`
    - **Property 29: Nothing submitted is kept, logged or echoed** (sentinel OTP, PIN, password, CVV, raw account, UPI and phone values in known and unknown fields of the evaluate route and every Beneficiary route)
    - **Validates: Requirements 14.9, 15.13, 24.1, 24.2, 24.3, 24.9, 24.10**

  - [ ] 16.9 Extend the JSON contract test for the new bodies
    - File: `httpapi/contract_json_test.go`
    - Pin the key set of the Beneficiary_View (`verified_at` only when set), the POST body, the PATCH body (five keys, design conflict C-5), the verification body, and the `beneficiary_id` added to the evaluate body; round-trip each type; prove unknown fields such as `otp`, `pin`, `password`, `passcode`, `cvv`, `account_number`, `upi_id` and `phone` decode, are dropped, and never appear when the value is encoded again
    - _Requirements: 22.4, 27.11_

  - [ ] 16.10 Write the backward-compatibility regression test
    - File: `httpapi/compat_test.go`
    - With no new field, an empty history and no recipient, the evaluate response has the same `state`, `risk_score`, `settled`, `remaining_seconds`, `reconfirm_required`, `metadata` and token fields as before; the six old routes keep their methods, statuses and codes; the vault state machine, 900 s lock, lazy expiry and fast-forward behave as before; the three Presets keep their states after history and recipients exist
    - _Requirements: 23.1, 23.2, 23.3, 23.6, 9.9_

- [ ] 17. Checkpoint - HTTP API for #5
  - Run in `backend/`: `gofmt -l .` (no output), `go vet ./...`, `go test -race -count=1 ./... -rapid.checks=1000`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 18. Add the front-end groundwork shared by the two new screens
  - GitHub issues: #3 (https://github.com/RahulKumar1119/rukpay/issues/3) and #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 18.1 Add the Beneficiary API client to `frontend/src/lib/api.ts`
    - Types `TrustLevel`, `ProtectionLevel`, `RiskIndicator`, `HistoryKind`, `HistoryEntry`, `BeneficiaryView` (no field for a raw identifier), `AddBeneficiaryRequest`, `PatchBeneficiaryRequest` (with the two optional flags), `VerificationAnswers`, `VerificationResult`, `RemoveResult`; `beneficiary_id?` on `EvaluateRequest`; `ErrorBody.beneficiary` and `ApiError.beneficiary` for a 409 with a valid View
    - `request()` accepts `PATCH` and `DELETE` and sends no body for `GET` and `DELETE`. Export `listBeneficiaries`, `addBeneficiary`, `getBeneficiary`, `patchBeneficiary`, `deleteBeneficiary`, `verifyBeneficiary(id, answers)` (id through `encodeURIComponent`, each with an optional `AbortSignal`). No type declares a field named `otp`, `pin`, `password`, `passcode` or `cvv`; no `console` call on any path
    - _Requirements: 22.1, 22.2, 22.3, 22.4, 22.5, 22.9_

  - [ ] 18.2 Write the API client tests
    - Files: `src/lib/api.beneficiaries.test.ts`, `src/lib/api.beneficiaries.pbt.test.ts`
    - Example tests against the stubbed `fetch`: each of the six calls and evaluate with `beneficiary_id` (method, path, body or no body, parsed result), `PATCH` and `DELETE`, error mapping (`not_found`, `state_conflict` with the View, network failure, 10 s time-out, 5xx, unreadable body, abort), outbound redaction of `otp`, `pin`, `password`, `passcode`, `cvv` and raw identifiers, and no `console` call
    - **Property 37: The client sends redacted bodies and maps errors**
    - **Validates: Requirements 22.2, 22.5, 22.9**
    - _Requirements: 28.6, 28.9_

  - [ ] 18.3 Add the shared UI pieces
    - Files: `src/components/ui.ts`, `src/components/SimulationNotice.tsx`, `src/lib/copy.ts`
    - Add `BUTTON_ACTION`, `BUTTON_QUIET`, `FIELD`, `CHOICE_ROW` and the contrast-safe disabled style (dark text on white with a dashed border) using only tokens the smoke test knows (`white`, `slate-50`, `slate-100`, `slate-surface`, `slate-ink`, `amber-surface`, `amber-text`). No red, no new palette colour
    - `SimulationNotice` is an amber `NOTICE_BOX` with an `Info` icon and the one sentence `COPY.simulationNotice`, no close control, no timer. Add the `simulationNotice` string and the `phoneNav` group (labels "Phone screens", "Pay", "Custom payment", "Saved recipients") and append `simulationNotice` and `phoneNav` to `NEW_COPY_GROUPS`
    - _Requirements: 22.6, 25.1, 25.5, 26.1, 26.2, 26.3, 26.4, 26.7, 26.8_

  - [ ] 18.4 Add phone navigation state to the simulator
    - Files: `src/lib/simulatorReducer.ts`, `src/lib/useSimulator.ts`
    - `PhoneScreen = "pay" | "custom" | "saved"`, `phoneScreen` in `SimState` (initial `pay`), action `SHOW_SCREEN` that changes only `phoneScreen`, `EVALUATED` sets `pay`, `START` keeps it. `useSimulator` exposes `showScreen(screen)`. Nothing reads or writes storage, the URL or a cookie. The shipped start-reducer property must pass unchanged
    - _Requirements: 8.5, 22.7, 22.10_

  - [ ] 18.5 Write property test for navigation state
    - File: `src/lib/simulatorReducer.nav.pbt.test.ts`
    - **Property 33: Navigation changes only the screen**
    - **Validates: Requirements 8.5, 22.7, 22.10**

- [ ] 19. Build the saved recipients screen
  - GitHub issue: #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 19.1 Implement `src/lib/beneficiaries.ts` and the `beneficiary` and `verification` copy
    - `maskerFor(kind)` (`maskAccount`, `maskUpi`, `maskPhone`), `ageText(hours)` ("2 hours ago" under 48 hours, whole days from 48), `addedText(hours)` ("Added 2 hours ago", "Added less than 1 hour ago"), `historyText(entry, now)`. Add the `beneficiary` and `verification` groups to `copy.ts` with the design's texts (the three questions character for character from Requirement 17.8), and the helpers `agoText` and `verificationProgress` ("Question 1 of 3"). Append both groups to `NEW_COPY_GROUPS` and push sample helper output into `ALL_COPY`
    - _Requirements: 17.7, 17.8, 19.8, 21.1, 21.2, 21.11, 21.12, 25.1_

  - [ ] 19.2 Write property test for indicator and age text
    - File: `src/lib/beneficiaries.pbt.test.ts`
    - **Property 39: Indicator and age text come from the view**
    - **Validates: Requirements 19.8, 21.1**

  - [ ] 19.3 Implement `src/lib/beneficiaryReducer.ts`
    - Pure reducer with `BeneficiaryState` and the actions of the design: `LOADED`, `UPSERT`, `REMOVED`, `ROW_PRESS`, `ROW_NOTE`, `FORM_NOTE`, `UNREACHABLE`/`REACHABLE`, `CONFIRM_REMOVE`/`CANCEL_REMOVE`, plus open history, rename and verification. A row note stays until `ROW_PRESS` for that row or until the row leaves the list. No timer
    - _Requirements: 21.7, 21.9, 21.10, 21.16_

  - [ ] 19.4 Write property test for the reducer
    - File: `src/lib/beneficiaryReducer.pbt.test.ts`
    - **Property 35: Manager notes stay until the person acts**
    - **Validates: Requirements 21.7, 21.9, 21.16**

  - [ ] 19.5 Implement `src/lib/useBeneficiaries.ts`
    - Binds the reducer to the six API calls: `load`, `add`, `patch`, `remove`, `history`, `verify`, `retry`. It keeps the last failed request as a closure (for an add, the body already masked), maps `not_ready`, `NOT_PASSED` and other `ApiError` values to row or form notes, `state_conflict` to `UPSERT` plus a note, and `NetworkError` to `UNREACHABLE`. It aborts in-flight calls on unmount and uses no clock, timer, console or storage
    - _Requirements: 21.1, 21.9, 21.13, 21.16_

  - [ ] 19.6 Implement `useFocusTrap.ts` and `VerificationDialog.tsx`
    - Files: `src/components/useFocusTrap.ts` (the same Tab loop as `RealityCheckModal`, as a hook; leave `RealityCheckModal.tsx` untouched), `src/components/VerificationDialog.tsx`
    - `role="dialog"`, `aria-modal="true"`, `aria-labelledby` the visible heading; "Question n of 3", the question at 20px, Yes and No buttons side by side with `gap-3`, a Back button that keeps answers and is disabled on question 1; focus moves to Yes on every new question including after Back; no timer. After the third answer send one request with `coercion`, `acquaintance`, `secrecy` as `YES` or `NO` (Yes is `YES` for every question). Show "Checking your answers…" while waiting; on a network failure keep the dialog open with the unreachable notice and the same answers
    - _Requirements: 17.7, 17.8, 17.9, 17.12, 26.1, 26.2, 26.5_

  - [ ] 19.7 Write the verification dialog tests
    - File: `src/components/VerificationDialog.test.tsx`
    - Questions one at a time in order with the exact wording and progress line, Back keeps answers and is disabled on question 1, focus moves to Yes each time, focus trap, one request with the three answers after the third, `NOT_PASSED` message returned to the caller, network failure keeps the answers
    - _Requirements: 17.7, 17.8, 17.9, 17.12, 28.5_

  - [ ] 19.8 Implement the manager shell and the add form in `BeneficiaryManager.tsx`
    - Heading, the shared-data note, the nickname prompt, `SimulationNotice`, and the list loaded with `useBeneficiaries`. Add form: kind choice (account, UPI, phone; account first), nickname field (`maxLength={40}`), one identifier field that is uncontrolled (a `ref`, no `value`, `autoComplete="off"`, `spellCheck={false}`), two labelled switches (both off at start; their state lives in the manager so Trust this recipient can reuse it), and Add. On Add read `ref.current.value` into a local const, clear the field first, mask it by kind, drop the local value; if the result is `[REDACTED]` show the calm message and send nothing, otherwise send `{label, identifier_masked, is_late_night, active_unknown_call}`. Show the pressure sentence beside a new recipient with the flag set
    - _Requirements: 21.1, 21.2, 21.3, 21.4, 21.5, 21.6, 21.7, 21.11, 21.12, 26.1, 26.2_

  - [ ] 19.9 Implement the recipient rows and their actions
    - In `BeneficiaryManager.tsx`: each row shows label, masked identifier, trust and care words, how long ago it was added and one sentence per indicator in server order. Seven buttons on every row in a `flex flex-wrap gap-3` container: Check this recipient (opens the dialog), Trust this recipient (PATCH `TRUSTED` with the add form's switches as the flags), Remove trust (`NEW`), Keep extra care (`EXTRA_CARE`), Rename (inline field, Save sends the label only, Cancel), Remove (a sentence, "Yes, remove" and Cancel; one `DELETE` only after the second tap), Show history (GET one, entries newest first as plain sentences with how long ago and, for a payment, the amount)
    - Notes (`not_ready`, `NOT_PASSED`, other errors) stay beside the row until one of its buttons is pressed. A `state_conflict` replaces the row with the View in the error. `UnreachableNotice` with a retry repeating the last failed request. The browser makes no trust decision
    - _Requirements: 21.1, 21.7, 21.8, 21.9, 21.10, 21.13, 21.14, 21.15, 21.16, 19.8_

  - [ ] 19.10 Write the manager row and action tests
    - Files: `src/test/beneficiaryStubs.ts` (View fixtures for all six indicators), `src/components/BeneficiaryManager.rows.test.tsx`
    - The list from the server; all six indicator sentences in received order and none for an empty list; a `not_ready` message beside its row only, still there after 60 simulated seconds, gone after a button of that row is pressed; Remove sends nothing on the first tap and exactly one `DELETE` on the second; rename, history, trust and conflict flows; the unreachable notice and retry
    - _Requirements: 19.8, 21.1, 21.7, 21.8, 21.9, 21.10, 21.13, 21.14, 21.15, 21.16, 28.5_

  - [ ] 19.11 Write the add-flow tests and the masking property
    - Files: `src/components/BeneficiaryManager.add.test.tsx`, `src/components/BeneficiaryManager.add.pbt.test.tsx`
    - Add in each of account, UPI and phone with a raw identifier held in a test constant: the body holds the mask output and the raw text appears in no body, page text, field value, `console` call or storage. `[REDACTED]` sends nothing and keeps kind, nickname and switches
    - **Property 36: A raw identifier is masked at the boundary and kept nowhere** (at least 100 runs)
    - **Validates: Requirements 21.3, 21.4, 21.5**

  - [ ] 19.12 Add the phone navigation and wire the screens
    - Files: `src/components/PhoneNav.tsx`, `src/components/ScenarioBuilder.tsx` (shell only), `src/components/UserPhonePanel.tsx`, `src/lib/copy.ts`
    - `PhoneNav`: `<nav aria-label>` with three buttons in a `grid grid-cols-3 gap-3`, each `min-h-12 min-w-12`, text wraps at 16px, the selected one has `aria-current="page"`, a filled style, bold text and a `Check` icon. Selecting one calls `sim.showScreen` and changes nothing else
    - `UserPhonePanel` shows the nav above the screen: `pay` is the existing content, `saved` is `BeneficiaryManager`, `custom` is the `ScenarioBuilder` shell. The shell holds only its heading (`COPY.phoneNav.custom`), one intro sentence and `SimulationNotice`; group 22 completes it. Create the `scenario` copy group here with the `intro` string and append `scenario` to `NEW_COPY_GROUPS`; task 21.2 extends it. The reality-check modal still covers the whole phone
    - _Requirements: 21.12, 22.6, 22.7, 22.8, 22.10, 8.8_

  - [ ] 19.13 Write the navigation tests
    - File: `src/components/PhoneNav.test.tsx`
    - Exactly three choices in order, one with `aria-current="page"`, Pay selected at start and after a reload, switching changes no URL, history, storage or cookie and leaves the vault, countdown and guardian panel as they were, and at 1024px and wider the payer phone stays left of the guardian phone while either new screen is open
    - _Requirements: 22.6, 22.7, 22.8, 22.10_

  - [ ] 19.14 Extend the smoke tests for the manager and the navigation
    - File: `src/test/smoke.test.ts`
    - Add scenes: manager empty, with indicators, with a `not_ready` note, with the dialog open, with the Remove confirmation, and the navigation. Extend the hit-area and 12px-gap class checks to `input` fields and `label` rows. Keep every shipped rule (no storage, console, pure red, 7:1 contrast, dependencies only `react`, `react-dom` and `lucide-react`)
    - _Requirements: 26.1, 26.2, 26.3, 26.4, 26.5, 26.6, 26.7, 26.9, 28.8_

- [ ] 20. Checkpoint - Front end for #5
  - Run in `frontend/`: `npm run typecheck`, `npm run build`, `npm test`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 21. Build the custom payment logic
  - GitHub issue: #3 (https://github.com/RahulKumar1119/rukpay/issues/3)
  - [ ] 21.1 Implement `frontend/src/lib/scenario.ts`
    - `parseWholeNumber(text, min)`: remove only leading and trailing U+0020, require `/^[0-9]+$/`, convert with `BigInt`, compare to `min` and `9_007_199_254_740_991n` as `bigint`, return `Number` or `null`. `checkValues` (amount minimum 1, hours and retries minimum 0); `DEMO_CONTACTS` (the four entries of the design, masked values valid for `store.ValidateMasked`); `RecipientChoice`; `FRAUD_SCENARIOS` (Digital arrest 60000/2/late night on/call on/0, Courier and customs 12500/5/off/on/1, KYC expiry 4999/3/off/off/2); `maskedField`, `buildRequest` (the five Signals plus exactly the recipient fields of Requirement 6, no other key), `toPaymentScenario`, `applyFraudScenario`
    - Add the `PaymentScenario{payeeLabel, payeeMasked, timeLabel, request}` interface to `src/lib/presets.ts` and make `Preset` extend it, with no value change (`toPaymentScenario` returns it)
    - **Design conflict C-2 (needs the user's confirmation):** Requirement 6.9 says a story press clears any recipient selection, Requirement 7.8 says it keeps a selected Demo_Contact and clears only a saved recipient. Implement 7.8 as the design chooses, behind one constant `FRAUD_SCENARIO_KEEPS_DEMO_CONTACT = true` with a comment naming both requirements. Setting it to `false` follows 6.9
    - _Requirements: 5.2, 5.3, 5.6, 5.11, 6.1, 6.3, 6.4, 6.5, 6.7, 6.9, 7.2, 7.3, 7.4, 7.8, 8.1, 8.6_

  - [ ] 21.2 Complete the `scenario` copy group
    - File: `src/lib/copy.ts` (extends the group created in task 19.12)
    - The design's texts: labels, placeholders, switch labels, recipient legend and group headings, `loadingSaved`, `savedLoadFailed`, `savedEmpty`, the three validation messages, "Simulate", "Checking this payment…", the three story labels and descriptions (each with "simulation" and no claim that a payment was approved, held or cancelled). No label or hint uses OTP, PIN, password, CVV, "account number", "UPI id" or "phone number"
    - _Requirements: 5.9, 5.10, 5.12, 6.6, 6.10, 7.1, 7.7, 25.1, 25.3_

  - [ ] 21.3 Write unit tests for the scenario data
    - File: `src/lib/scenario.test.ts`
    - Four Demo_Contacts, each masked value matches one of the three `ValidateMasked` patterns, labels follow Requirement 6.1, defaults include one of 72 hours or more and one below 24; exact story values; `buildRequest` per contact format and for a saved recipient; the C-2 reading (a story keeps a Demo_Contact and clears a saved recipient)
    - _Requirements: 6.1, 6.3, 6.4, 6.5, 7.2, 7.3, 7.4, 7.8_

  - [ ] 21.4 Write property test for number parsing
    - File: `src/lib/scenario.parse.pbt.test.ts`
    - **Property 30: Whole-number parsing is exact** (empty, signs, decimals, exponents, separators, inner spaces, letters, leading zeros, 400-digit strings, values at and above 9007199254740992)
    - **Validates: Requirements 5.2, 5.3, 5.6, 5.11, 5.13**
    - _Requirements: 28.1_

  - [ ] 21.5 Write property test for the request body
    - File: `src/lib/scenario.request.pbt.test.ts`
    - **Property 31: The request body equals the form** (`buildRequest` is deeply equal to the five values plus the recipient fields only, `redactObject(body)` equals the body, and the three Presets keep their shipped bodies)
    - **Validates: Requirements 6.3, 6.4, 6.5, 6.7, 8.1, 8.6, 8.9, 22.3, 22.4**
    - _Requirements: 28.1, 28.3_

  - [ ] 21.6 Let the simulator start a custom scenario
    - Files: `src/lib/simulatorReducer.ts`, `src/lib/useSimulator.ts`
    - `START` takes a `PaymentScenario` (the type from task 21.1; a `Preset` still fits) and keeps `phoneScreen`. Add `startScenario(scenario)` returning `started`, `rejected {field?, message}` (HTTP 400 with a body, no notice dispatched) or `unreachable` (the notice is set with a retry that repeats the same scenario). `startPreset` keeps its behaviour and shares one inner `runScenario`. Both paths discard the previous vault, close the stream and stop the poll before the request is sent
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.9, 8.10, 8.11, 2.5_

  - [ ] 21.7 Write tests for starting a scenario
    - File: `src/lib/useSimulator.scenario.test.tsx`
    - A custom start sends one request with the given body; a 400 returns `rejected` with the server `field` and `message` and sets no notice; a network failure shows the unreachable notice and its retry repeats the same scenario; the previous vault is discarded first; the shipped Preset tests pass with no edit
    - _Requirements: 8.1, 8.2, 8.11, 23.6_

  - [ ] 21.8 Implement `src/lib/useScenarioForm.ts`
    - Form values, messages and the story note, in memory only, kept in `App` so values survive a visit to another screen. `setField`, `selectRecipient` (a Demo_Contact fills hours with its default and stays editable; a saved recipient fills hours with its `age_hours`, read-only), `applyScenario` (clears messages, sets the story note, sends nothing), `submit()` (client messages for bad fields, returns the first bad field, otherwise clears messages and returns the `PaymentScenario`), `reject(field, message)` (maps `amount_inr`, `beneficiary_age_hours`, `rapid_retry_count` to their fields and any other field to the form message), `clearMessages()`, `leave()` (clears messages and note, keeps values)
    - _Requirements: 5.2, 5.3, 5.4, 5.7, 6.3, 6.4, 6.8, 7.5, 8.11_

  - [ ] 21.9 Write property test for message lifetime
    - File: `src/lib/useScenarioForm.pbt.test.ts`
    - **Property 34: Form messages stay until the stated events** (random sequences of edits, switches, recipient choices, story and Preset presses, failed and valid submits, and fake time)
    - **Validates: Requirements 5.4, 26.7**

- [ ] 22. Build the custom payment screen
  - GitHub issue: #3 (https://github.com/RahulKumar1119/rukpay/issues/3)
  - [ ] 22.1 Complete `ScenarioBuilder.tsx`
    - Files: `src/components/ScenarioBuilder.tsx`, `src/lib/useSavedRecipients.ts` (loads the list once on mount through `listBeneficiaries`, aborts on unmount, reports loading, ready, empty or failed)
    - Replace the shell: heading, intro, `SimulationNotice`; the three story buttons with their descriptions visible; amount, hours and retries as `type="text"`, `inputMode="numeric"`, `autoComplete="off"`, with no `onPaste`, no `maxLength` and no input filter; each message in a persistent `aria-live="polite"` container with `aria-invalid` and `aria-describedby` set only while it exists; two `role="switch"` rows; a recipient `<fieldset>` with two radio groups, each with its own visible heading, the Demo_Contacts first and the saved recipients second (its loading, failure or empty sentence directly under the second heading), no radio selected at first; Simulate and, while busy, a `role="status"` line "Checking this payment…" with all its buttons disabled. On a failed submit focus the first bad field
    - **Design conflict C-2:** a story press keeps a Demo_Contact (see task 21.1)
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.7, 5.8, 5.9, 5.10, 5.12, 5.13, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8, 6.9, 6.10, 7.1, 7.5, 7.6, 8.4, 8.7, 8.8, 26.1, 26.2, 26.3, 26.5, 26.6, 26.7, 26.8_

  - [ ] 22.2 Wire the builder into `App` and the payer phone
    - Files: `src/App.tsx`, `src/components/UserPhonePanel.tsx`, `src/components/PresetBar.tsx`
    - Call `useScenarioForm` once in `App` and pass it down; the Preset buttons call `form.clearMessages()`; Simulate calls `sim.startScenario` and sends a `rejected` result to `form.reject`; the panel shows the unreachable notice above the builder on the custom screen and hides the masked payee line when `payeeMasked` is empty; all three Preset buttons are disabled while a request is in flight
    - _Requirements: 5.4, 5.7, 8.1, 8.2, 8.3, 8.4, 8.5, 8.10, 8.11, 7.6_

  - [ ] 22.3 Write builder component tests for values and messages
    - File: `src/components/ScenarioBuilder.test.tsx`
    - Typing and pasting into the three fields and toggling both switches; the amount values empty, `0`, `-5`, `12.5`, `abc`, `9007199254740992` and the hours and retry values empty, `-1`, `1.5`, `abc`, `9007199254740992`, each with its message beside the field, `aria-invalid`, `aria-describedby`, no request, and the message kept until the value is corrected; the accepted bounds `1`, `9007199254740991` and `0`, `9007199254740991` send one request; a server error with a field puts its message beside that field and one without puts it above Simulate
    - _Requirements: 5.2, 5.3, 5.4, 5.5, 5.7, 5.11, 5.13, 28.2_

  - [ ] 22.4 Write builder component tests for recipients and stories
    - File: `src/components/ScenarioBuilder.recipient.test.tsx`
    - The request body for a Demo_Contact in each of the account, UPI and phone formats and for a saved recipient (exact equality, so an extra, missing or renamed key fails); the hours field after each choice; the saved-recipient load failure and empty messages; each story button's exact values and description with no request sent before Simulate; a story keeps a Demo_Contact and clears a saved recipient (C-2)
    - _Requirements: 6.2, 6.3, 6.4, 6.5, 6.6, 6.8, 6.9, 6.10, 7.1, 7.2, 7.3, 7.4, 7.5, 7.8, 28.2_

  - [ ] 22.5 Write property test for invalid values
    - File: `src/components/ScenarioBuilder.invalid.pbt.test.tsx`
    - **Property 31: The request body equals the form** (component half, at least 100 runs: any amount, hours or retry text that is not a whole number in range sends no request and shows the message beside that field)
    - **Validates: Requirements 5.2, 5.3, 8.1**
    - _Requirements: 28.3_

  - [ ] 22.6 Write the Simulate flow tests
    - File: `src/App.simulate.test.tsx`
    - Simulate sends one evaluate request with the form values; the previous vault is gone from both phones before the request; every button is disabled with "Checking this payment…" while the request is in flight; the response switches the navigation to Pay and shows the state screen, the explanation card and, for LOCKED_15M, the guardian alert; a 400 and a network failure behave as Requirement 8.11 says; the three Presets still send their shipped bodies and reach their states; no login or setup step
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.7, 8.9, 8.10, 8.11, 23.6, 28.2, 28.9_

  - [ ] 22.7 Extend the smoke tests for the builder
    - File: `src/test/smoke.test.ts`
    - Add `onPaste` to the forbidden-source scan and add scenes: the empty builder, the builder with a validation message and after a story fill. Keep every shipped rule
    - _Requirements: 5.13, 26.1, 26.3, 26.4, 28.8_

- [ ] 23. Checkpoint - Front end for #3
  - Run in `frontend/`: `npm run typecheck`, `npm run build`, `npm test`
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 24. Add the repo-wide import test and the README
  - GitHub issues: #2 (https://github.com/RahulKumar1119/rukpay/issues/2), #3 (https://github.com/RahulKumar1119/rukpay/issues/3), #4 (https://github.com/RahulKumar1119/rukpay/issues/4) and #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 24.1 Write the repo-wide import test
    - File: `backend/internal/repocheck/imports_test.go` (a package that holds only this test)
    - No Go file under `backend`, test files included, imports a path containing `sqlite` in any letter case, and neither `go.mod` nor `go.sum` mentions `sqlite`. Cover every package directory under `backend` (so every new package) and fail when a directory yields no Go file. Add a blank import of `pgregory.net/rapid` so the package accepts the `-rapid.checks=1000` flag that the checkpoints pass to `go test ./...`
    - _Requirements: 24.7, 27.12_

  - [ ] 24.2 Update `README.md` for the new features
    - Add a "Risk intelligence" section. Its first sentence says RukPay simulates payment decisions and makes, holds or cancels no real payment, and every later sentence about a score, an approval, a hold or a cancellation uses a word that starts with "simulat". Cover the explanation field, the behavioural signals and their caps, saved recipients and the trust rules, the custom payment screen and the three stories, the new routes and `PATCH`/`DELETE` CORS methods, the `explanation` attribute and the `HIST#` and `BENE#` items of the table, and the new tests
    - Add a short deployment note: the function's IAM policy must allow `dynamodb:Query` and `dynamodb:DeleteItem` as well as `PutItem` and `GetItem`; the repository has no SAM template yet. Record that the demo payer and the recipient list are shared by every visitor of one deployment until roadmap issue #6
    - _Requirements: 25.7, 14.1, 12.1, 13.3, 20.2_

- [ ] 25. Run the whole verification
  - GitHub issues: #2 (https://github.com/RahulKumar1119/rukpay/issues/2), #3 (https://github.com/RahulKumar1119/rukpay/issues/3), #4 (https://github.com/RahulKumar1119/rukpay/issues/4) and #5 (https://github.com/RahulKumar1119/rukpay/issues/5)
  - [ ] 25.1 Run the backend checks and fix any failure
    - In `backend/`: `gofmt -l .` prints nothing, `go vet ./...`, `go test -race -count=1 ./... -rapid.checks=1000`, and `go build ./cmd/server ./cmd/lambda`
    - _Requirements: 23.7, 27.1_

  - [ ] 25.2 Run the frontend checks and fix any failure
    - In `frontend/`: `npm run typecheck`, `npm run build`, `npm test` (a single run, zero failed or skipped tests, no network). Confirm `package.json` runtime dependencies are still `react`, `react-dom` and `lucide-react`
    - _Requirements: 26.9, 28.10_

  - [ ] 25.3 Audit the edits to existing tests and the privacy rules
    - Run `git diff --name-status` for test files that existed before this work. The only edits allowed are `"explanation"` in `store/types_test.go` (design conflict C-1), the one added key in tests that pin an exact View or response key set (Requirement 23.7), and helper code in `store/fake_dynamodb_test.go`. No existing assertion may change
    - Search `backend` and `frontend/src` for `sqlite`, `console.`, `localStorage`, `sessionStorage` and `document.cookie` outside tests, and for any field named `otp`, `pin`, `password`, `passcode` or `cvv` in a new type. Fix anything found
    - _Requirements: 23.7, 24.6, 24.7_

- [ ] 26. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP. No task here is marked, because Requirements 27 and 28 and the roadmap guarantees require the tests. Put `*` after the checkbox of a test sub-task only if you decide to skip it.
- Every test file name is chosen so that independent test tasks never edit the same file. A few names differ from the design's testing table for that reason (for example one `risk/pbt_*_test.go` file per property instead of `pbt_assess_test.go`).
- Groups 1 to 6 are #2 and block everything else. Groups 7 and 8 do not depend on each other, but group 9 needs both. #4 has no front-end task of its own: the explanation card and guardian summary from group 5 show the behavioural lines because they render whatever the server sends.
- `genInput` in `risk/generators_test.go` is the one generator every `Assess` property uses. Tasks 8.5 and 12.2 widen it, so earlier property files are not edited.
- Tasks 7.5 and 11.4 edit helper code in the existing `store/fake_dynamodb_test.go`. They add features and leave every existing `Test...` function and its assertions as they are.
- Open design conflicts that need the user's ruling are marked in their tasks: C-1 (task 1.1, add `"explanation"` to the attribute list pinned in `store/types_test.go`) and C-2 (tasks 21.1 and 22.1, a story button keeps a selected demo contact, following Requirement 7.8). The other conflicts (C-3 to C-8) are implemented as the design resolves them, and each task that touches one names it.
- Task 19.12 adds a `ScenarioBuilder` shell so the navigation has a screen to show. Task 22.1 completes it.
- Each task references specific requirements for traceability. Property tasks cite the design property number and the requirements it validates; a property that grows across groups (Properties 1 to 5, 11, 12, 16, 17 and 31) names the group that extends it.
- The tasks do not create or edit GitHub issues, and they do not cover deployment.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "2.2", "2.9"] },
    { "id": 2, "tasks": ["2.3", "2.10", "3.1"] },
    { "id": 3, "tasks": ["2.4", "2.5", "2.6", "2.7", "2.8", "3.2"] },
    { "id": 4, "tasks": ["3.3", "3.4", "3.5"] },
    { "id": 5, "tasks": ["5.1", "5.3"] },
    { "id": 6, "tasks": ["5.2"] },
    { "id": 7, "tasks": ["5.4", "5.5", "5.6"] },
    { "id": 8, "tasks": ["5.7", "5.8"] },
    { "id": 9, "tasks": ["7.1", "7.2", "8.1"] },
    { "id": 10, "tasks": ["7.3", "7.4", "8.2", "8.3"] },
    { "id": 11, "tasks": ["7.5", "8.4", "8.10"] },
    { "id": 12, "tasks": ["7.6", "7.7", "8.5", "9.1"] },
    { "id": 13, "tasks": ["8.6", "8.7", "8.8", "8.9", "9.2", "9.3", "9.4", "9.5", "9.7"] },
    { "id": 14, "tasks": ["9.6"] },
    { "id": 15, "tasks": ["11.1"] },
    { "id": 16, "tasks": ["11.2", "11.3", "11.6", "12.1", "13.1"] },
    { "id": 17, "tasks": ["11.4", "11.5", "12.2", "13.2", "13.3"] },
    { "id": 18, "tasks": ["11.7", "11.8", "11.9", "13.4", "13.6", "13.8", "13.10"] },
    { "id": 19, "tasks": ["13.5", "13.9"] },
    { "id": 20, "tasks": ["13.7", "13.11", "13.12", "13.13", "14.1"] },
    { "id": 21, "tasks": ["14.2", "14.3"] },
    { "id": 22, "tasks": ["14.4", "14.5"] },
    { "id": 23, "tasks": ["16.1"] },
    { "id": 24, "tasks": ["16.2"] },
    { "id": 25, "tasks": ["16.3"] },
    { "id": 26, "tasks": ["16.4"] },
    { "id": 27, "tasks": ["16.5", "16.6", "16.7", "16.8", "16.9", "16.10"] },
    { "id": 28, "tasks": ["18.1", "18.3", "18.4"] },
    { "id": 29, "tasks": ["18.2", "18.5", "19.1", "19.3"] },
    { "id": 30, "tasks": ["19.2", "19.4", "19.5", "19.6"] },
    { "id": 31, "tasks": ["19.7", "19.8"] },
    { "id": 32, "tasks": ["19.9", "19.11"] },
    { "id": 33, "tasks": ["19.10", "19.12"] },
    { "id": 34, "tasks": ["19.13", "19.14"] },
    { "id": 35, "tasks": ["21.1", "21.2"] },
    { "id": 36, "tasks": ["21.3", "21.4", "21.5", "21.6"] },
    { "id": 37, "tasks": ["21.7", "21.8"] },
    { "id": 38, "tasks": ["21.9", "22.1"] },
    { "id": 39, "tasks": ["22.2"] },
    { "id": 40, "tasks": ["22.3", "22.4", "22.5", "22.6", "22.7"] },
    { "id": 41, "tasks": ["24.1", "24.2", "25.2"] },
    { "id": 42, "tasks": ["25.1"] },
    { "id": 43, "tasks": ["25.3"] }
  ]
}
```
