# Implementation Plan: Dual-Screen Simulator

## Overview

Implementation language is TypeScript (strict) on React 18 with Vite, Tailwind CSS and `lucide-react`, all in `frontend/`, as fixed by the design. One task also touches the Go backend: it adds the concrete `EvaluateRequest` and `Metadata` struct definitions that `cooling-off-vault/design.md` names but never declares (the open item flagged in this feature's design).

The plan builds bottom-up: project scaffold, the backend struct definitions, the pure `lib/` modules (masker, copy, API client, presets, screen derivation, countdown), then the reducer, `VaultSync` and `useSimulator`, then components, and finally `App.tsx` wiring and smoke checks. Dependency direction stays components → `useSimulator` → `VaultSync`, `api.ts` → `privacyMasker.ts`.

Tests use Vitest, Testing Library and fast-check (at least 100 runs per property) against a stubbed `fetch` and `EventSource` that live in `src/test/` only. Each property test carries the comment `// Feature: dual-screen-simulator, Property N: <title>`. Test files are split per property or topic so independent test tasks never edit the same file.

## Tasks

- [ ] 1. Scaffold the frontend project
  - [ ] 1.1 Create the Vite, React and TypeScript project files in `frontend/`
    - Files: `package.json`, `tsconfig.json`, `vite.config.ts` (with Vitest config: `jsdom`, `setupFiles`), `index.html`, `src/main.tsx`, `src/test/setup.ts`, `.env.example`
    - Scripts: `dev`, `build`, `preview`, `test` (`vitest run`), `typecheck` (`tsc --noEmit`)
    - `tsconfig.json` has `"strict": true`; dependencies are React, `lucide-react`, Tailwind, Vitest, Testing Library, jsdom and fast-check only (no Redux, Angular or other state library)
    - `.env.example` holds `VITE_API_BASE_URL=http://localhost:8080`
    - Create the folders `src/components/`, `src/lib/`, `src/test/`
    - _Requirements: 14.1, 14.2, 14.3, 14.4, 14.5, 14.6_

  - [ ] 1.2 Configure Tailwind and the global stylesheet
    - Files: `frontend/tailwind.config.ts`, `frontend/postcss.config.js`, `frontend/src/index.css`
    - Theme colors: `amber-surface #FEF3C7`, `amber-ink #92400E`, `slate-surface #1E293B`, `slate-ink #F8FAFC`, `vault-teal #0F766E`, `veto-red #DC2626`; no `#FF0000` anywhere
    - Base font size 16px; `@media (prefers-reduced-motion: reduce)` disables animation and transition; a global `focus-visible` ring style
    - _Requirements: 13.3, 13.5, 13.6, 13.7, 13.9, 13.11_

  - [ ] 1.3 Add the test stubs in `frontend/src/test/stubs.ts`
    - A `fetch` stub that records calls (method, URL, parsed body) and returns queued responses or rejects
    - An `EventSource` stub class with `emit(data)`, `fail()` and a `closed` flag
    - Helpers to build `VaultView` fixtures for all six states
    - Lives under `src/test/` only; no mock data source anywhere else in `src/`
    - _Requirements: 15.12, 15.13, 9.6_

- [ ] 2. Add concrete backend request and metadata structs (open item from design)
  - [ ] 2.1 Define `EvaluateRequest` and `Metadata` in `backend/internal/vault/types.go` and record them in the vault design
    - `EvaluateRequest`: the five Signals with JSON names `amount_inr` (int64), `beneficiary_age_hours` (int64), `is_late_night` (bool), `active_unknown_call` (bool), `rapid_retry_count` (int), plus optional masked identifiers `payer_account_masked`, `beneficiary_masked`, `upi_masked`, `phone_masked` (`omitempty`); expose a method that returns the `risk.Signals`
    - `Metadata`: `amount_inr` plus the same four masked identifier fields, all `omitempty`, used as the `metadata` field of `View`
    - No field for raw identifiers, OTP, PIN or password; identifiers are checked with `store.ValidateMasked` by `Service.Evaluate`
    - Update `.kiro/specs/cooling-off-vault/design.md` so the Go signatures that mention `EvaluateRequest` and `Metadata` point to these definitions and list the JSON names
    - The existing `cooling-off-vault` tasks 3.4 (`View`) and 3.5 (`Service.Evaluate`) must use these types and not declare their own
    - _Requirements: 2.2, 2.3, 2.4, 10.2, 12.5_

  - [ ]* 2.2 Write a unit test for the struct JSON contract
    - File: `backend/internal/vault/types_test.go`
    - Marshal and unmarshal both structs; assert the exact JSON key names above, that optional identifier fields are omitted when empty, and that unknown fields (such as `otp`) are dropped at decode and never appear on re-encode
    - _Requirements: 2.2, 12.4, 12.5_

- [ ] 3. Implement the privacy masker and the copy module
  - [ ] 3.1 Implement `frontend/src/lib/privacyMasker.ts`
    - Export `maskAccount`, `maskPhone`, `maskUpi`, `redactSecret`, `hashIdentifier` (SHA-256 hex of the trimmed, lowercased value via `crypto.subtle.digest`) and `redactObject` (deep copy, idempotent, does not modify its input)
    - `maskUpi` output must match `^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.\-]+$`; malformed input returns `[REDACTED]`
    - `redactObject` replaces the values of keys `otp|pin|password|passcode|cvv` (case-insensitive) and maps whole-string unmasked account, phone and UPI values to their masked form; already-masked strings, numbers and booleans pass through
    - Pure apart from `hashIdentifier`: no console, storage, network or DOM use
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8, 11.9, 11.10, 11.11_

  - [ ]* 3.2 Write unit tests for the masker
    - File: `frontend/src/lib/privacyMasker.test.ts`
    - Each Masking_Format, fewer-than-four-digit and malformed cases, a known SHA-256 vector for `hashIdentifier`, and `redactObject` on a sample request body
    - _Requirements: 15.1, 11.1, 11.2, 11.3, 11.5, 11.7, 11.9_

  - [ ]* 3.3 Write property test for masked output formats
    - File: `frontend/src/lib/privacyMasker.format.pbt.test.ts`
    - **Property 1: Masked output matches the backend formats**
    - **Validates: Requirements 11.1, 11.2, 11.3, 11.6, 15.2**

  - [ ]* 3.4 Write property test for digit runs
    - File: `frontend/src/lib/privacyMasker.digits.pbt.test.ts`
    - **Property 2: No long digit run survives masking**
    - **Validates: Requirements 11.10, 15.2**

  - [ ]* 3.5 Write property test for malformed identifiers
    - File: `frontend/src/lib/privacyMasker.malformed.pbt.test.ts`
    - **Property 3: Malformed identifiers are redacted**
    - **Validates: Requirements 11.4, 11.5**

  - [ ]* 3.6 Write property test for hashing
    - File: `frontend/src/lib/privacyMasker.hash.pbt.test.ts`
    - **Property 4: Hashing is deterministic and discriminating** (async property)
    - **Validates: Requirements 11.7, 11.8, 15.2**

  - [ ]* 3.7 Write property test for object redaction
    - File: `frontend/src/lib/privacyMasker.redact.pbt.test.ts`
    - **Property 5: Object redaction is deep, total and idempotent**
    - **Validates: Requirements 11.9, 15.2**

  - [ ] 3.8 Implement `frontend/src/lib/copy.ts`
    - Every user-facing string as a constant grouped by screen, plus `ALL_COPY: string[]` listing them all
    - Steering wording for the three reality-check questions, the protective-pause message ("We're holding this safely for 15 minutes so you have space to verify."), the Unreachable_Notice text ("We can't reach RukPay right now. Your money has not moved. Please try again."), the guardian second-confirmation text, button labels, and notice messages for `BAD_REQUEST`, `LINK_INVALID`, `ALREADY_ACTED` and `COPY_FAILED`
    - Plain, 6th-grade language; no Banned_Terms; lock described as a protective pause
    - _Requirements: 13.1, 13.2, 9.1, 4.2, 6.4, 6.11, 6.12, 7.5_

- [ ] 4. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Implement the API client, presets, screen derivation and countdown
  - [ ] 5.1 Implement `frontend/src/lib/api.ts`
    - Export the types from the design (`VaultState`, `Signals`, `EvaluateRequest`, `VaultMetadata`, `VaultView`, `Answers`, `RealityCheckRequest` as a union of `answers` and `reconfirm`, `GuardianActionRequest`, `GuardianActionResult`, `ErrorBody`), `ApiError` (status, code, message, field, vault for 409), `NetworkError`, `API_BASE`, `REQUEST_TIMEOUT_MS = 10_000`
    - Export `evaluate`, `getVault`, `submitAnswers`, `reconfirm`, `guardianAction`, `demoFastForward` and `eventStreamUrl`
    - One private `request` sends JSON with `Content-Type: application/json`, passes the body through `redactObject`, combines the caller's `AbortSignal` with a 10-second timeout, throws `NetworkError` on fetch failure, timeout, 5xx or unreadable body, and `ApiError` on other non-2xx responses (409 carries the View from `vault` or a View-shaped body)
    - Base URL from `import.meta.env.VITE_API_BASE_URL`, falling back to `http://localhost:8080` when empty
    - No `console` call; error messages never include the token or a Masked_Identifier
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9, 9.1, 9.4, 9.5_

  - [ ]* 5.2 Write unit tests for the API client
    - File: `frontend/src/lib/api.test.ts`
    - Base URL fallback, JSON content type, `AbortSignal` forwarded, 10-second timeout produces `NetworkError`, 409 carries the View, `eventStreamUrl` encodes the id
    - _Requirements: 10.1, 10.4, 10.5, 10.6, 10.7_

  - [ ]* 5.3 Write property test for outbound bodies
    - File: `frontend/src/lib/api.outbound.pbt.test.ts`
    - **Property 12: Outbound bodies carry no sensitive values**
    - **Validates: Requirements 10.8, 12.4, 12.5**

  - [ ]* 5.4 Write property test for error fields
    - File: `frontend/src/lib/api.errors.pbt.test.ts`
    - **Property 14: Errors carry the server fields and no secrets**
    - **Validates: Requirements 10.5, 10.9**

  - [ ] 5.5 Implement `frontend/src/lib/presets.ts`
    - `Preset` interface and `PRESETS` tuple with exactly the three presets: Safe (450, 720, false, false, 0; "Local store"; `s***@okaxis`), Medium (18000, 6, false, false, 0; "New contact"; `XXXX-XXXX-3307`), High (85000, 1, true, true, 0; "New account"; `XXXX-XXXX-4821`, time label "11:45 PM")
    - Each request carries the masked identifier in `upi_masked` (UPI preset) or `beneficiary_masked` (account presets); no raw identifiers
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.9, 12.2_

  - [ ]* 5.6 Write unit tests for presets
    - File: `frontend/src/lib/presets.test.ts`
    - Button labels, exact request bodies for all three presets, and every identifier matches one of the three mask regexes
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 12.2_

  - [ ] 5.7 Implement `frontend/src/lib/screens.ts`
    - `UserScreen`, `GuardianScreen`, `deriveUserScreen`, `deriveGuardianScreen`, `isTerminal` following the mapping tables in the design
    - _Requirements: 3.2, 3.3, 3.4, 3.5, 3.6, 4.1, 6.1, 6.2, 6.9, 6.10, 8.7_

  - [ ]* 5.8 Write property test for screen derivation
    - File: `frontend/src/lib/screens.pbt.test.ts`
    - **Property 6: Exactly one screen per view**
    - **Validates: Requirements 3.3, 3.4, 3.5, 3.6, 4.1, 6.1, 6.2, 6.9, 6.10**

  - [ ] 5.9 Implement `frontend/src/lib/countdown.ts`
    - `endTimeFrom`, `secondsLeft` (`max(0, ceil(...))`), `formatCountdown` (`mm:ss`, clamped at `00:00`), `parseCountdown`, `spokenCountdown`, and the `useCountdown(view, onZero)` hook
    - The hook recomputes the local end time on each LOCKED_15M View, ticks once per second from `Date.now()`, calls `onZero` once when the count first reaches zero, refreshes the spoken label at most every 10 seconds, and clears its interval on unmount or when the state leaves LOCKED_15M
    - _Requirements: 5.2, 5.3, 5.4, 5.5, 5.6_

  - [ ]* 5.10 Write property test for countdown arithmetic
    - File: `frontend/src/lib/countdown.pbt.test.ts`
    - **Property 8: Countdown arithmetic**
    - **Validates: Requirements 5.2, 5.3, 5.4, 15.4**

  - [ ]* 5.11 Write unit tests for the countdown hook
    - File: `frontend/src/lib/countdown.test.tsx`
    - Fake timers: `mm:ss` format, never below `00:00`, recompute when a new `remaining_seconds` arrives, exactly one `onZero` call at `00:00`, spoken label changes no more than once per 10 seconds
    - _Requirements: 15.3, 5.3, 5.4, 5.5, 5.6_

- [ ] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Implement state, real-time sync and the simulator hook
  - [ ] 7.1 Implement `frontend/src/lib/simulatorReducer.ts`
    - `SimState`, `SimAction`, `Notice`, `initialState`, `reduce`
    - `VIEW` rules: discard a foreign `vault_id`; drop a poll result whose `startedAtSeq` is older than `lastActionSeq`; otherwise the latest arrival replaces the earlier view; keep the previous Guardian_Token when a LOCKED_15M view has none, clear it when the state leaves LOCKED_15M; token held in state only
    - `START` returns `initialState` with the preset set and `busy` true (no vault, token, notice or unreachable marker)
    - Handle `EVALUATED`, `BUSY`, `UNREACHABLE`, `NOTICE`, `FAST_FORWARD_UNAVAILABLE`, `CANCEL_RECONFIRM`
    - _Requirements: 2.5, 3.8, 5.9, 6.14, 8.3, 8.9, 8.10, 12.1_

  - [ ]* 7.2 Write property test for token retention
    - File: `frontend/src/lib/simulatorReducer.token.pbt.test.ts`
    - **Property 9: Token retention**
    - **Validates: Requirements 8.3, 6.14**

  - [ ]* 7.3 Write property test for foreign views
    - File: `frontend/src/lib/simulatorReducer.foreign.pbt.test.ts`
    - **Property 10: Foreign views are discarded**
    - **Validates: Requirements 8.9**

  - [ ]* 7.4 Write property test for arrival order
    - File: `frontend/src/lib/simulatorReducer.arrival.pbt.test.ts`
    - **Property 11: Newest arrival wins, and stale polls are dropped**
    - **Validates: Requirements 8.2, 8.10**

  - [ ]* 7.5 Write property test for starting a preset
    - File: `frontend/src/lib/simulatorReducer.start.pbt.test.ts`
    - **Property 16: New preset leaves no residue**
    - **Validates: Requirements 2.5**

  - [ ] 7.6 Implement `frontend/src/lib/vaultSync.ts`
    - `VaultSync` class with `start()` and idempotent `dispose()`; injectable `EventSource` constructor and timer functions
    - Open `EventSource(eventStreamUrl(id))`; parse each `message` as a `VaultView` and call `onView` (ignore unparseable data); on `error` or close, close the stream and start polling unless the last view was terminal
    - Polling uses a `setTimeout` chain every 2000 ms that schedules the next poll only after the previous one finishes, with an `AbortController`; a failed poll calls `onUnreachable(true)` and keeps polling; the next success calls `onUnreachable(false)`
    - Stop everything after a terminal view; `dispose()` closes the stream, clears timers and aborts the in-flight poll
    - _Requirements: 8.1, 8.2, 8.4, 8.5, 8.6, 8.7, 8.8, 9.7_

  - [ ]* 7.7 Write unit tests for `VaultSync`
    - File: `frontend/src/lib/vaultSync.test.ts`
    - Fake timers and the stub `EventSource`: stream URL, message parsing, fall back to polling on error, 2-second spacing with no overlap while a poll is pending, stop on a terminal view, `dispose()` on unmount and on a new preset, poll failure keeps polling and toggles the unreachable callback
    - _Requirements: 15.10, 8.1, 8.4, 8.5, 8.6, 8.7, 8.8, 9.7_

  - [ ]* 7.8 Write property test for terminal states
    - File: `frontend/src/lib/vaultSync.pbt.test.ts`
    - **Property 7: Terminal states stop the sync**
    - **Validates: Requirements 8.7**

  - [ ] 7.9 Implement `frontend/src/lib/useSimulator.ts`
    - Owns the reducer, one `VaultSync` and one `AbortController` per in-flight action; exposes `startPreset`, `submitAnswers`, `reconfirm`, `cancelReconfirm`, `veto`, `guardianApprove(confirmed)`, `guardianVeto`, `fastForward`, `refresh`, `retry`
    - `startPreset` disposes the old sync, aborts in-flight requests, dispatches `START`, calls `evaluate`, and starts a `VaultSync` when the first view is not terminal; an unmount effect calls `dispose()`
    - A `run` wrapper maps `NetworkError` to `UNREACHABLE` with a retry closure that repeats the same call (reality-check retry resubmits the kept answers), 409 to `VIEW` plus the "someone already acted" notice, guardian 403 to the `LINK_INVALID` notice, fast-forward 404 to `FAST_FORWARD_UNAVAILABLE`, 400 to a notice with the server `message`; success dispatches `VIEW` with `source: "action"`
    - `veto` and `guardianVeto` send `{ action: "VETO", token, confirmed: true }` using the held Guardian_Token; no `console`, storage, cookie or URL use
    - _Requirements: 2.5, 2.10, 3.7, 3.10, 4.4, 4.9, 4.10, 5.8, 5.9, 6.5, 6.7, 6.8, 6.11, 6.12, 8.8, 9.1, 9.2, 9.3, 9.4, 9.5, 12.3_

- [ ] 8. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 9. Implement the components and wire the App
  - [ ] 9.1 Implement `PhoneFrame.tsx`, `EvidenceCard.tsx` and `UnreachableNotice.tsx` in `frontend/src/components/`
    - `PhoneFrame`: phone-shaped frame, `<section aria-label>` with the given label
    - `EvidenceCard`: selectable `evidence_summary`, statement that the transfer is permanently cancelled and no money has left the account, `tel:1930` link, `cybercrime.gov.in` link with `target="_blank"` and `rel="noopener noreferrer"`, "Copy summary" button with persistent "Summary copied." text and a persistent select-by-hand message when the clipboard is unavailable or rejects; summary kept in the DOM only
    - `UnreachableNotice`: exact message text and a "Try again" button
    - All controls at least 48x48px with 12px spacing, visible focus ring, lucide icon beside status text
    - _Requirements: 1.4, 7.1, 7.2, 7.3, 7.4, 7.5, 9.1, 9.2, 9.3, 12.6, 13.4, 13.9, 13.10_

  - [ ] 9.2 Implement `RealityCheckModal.tsx`
    - Props `{ open, onSubmit, unreachable, onRetry }`; one question at a time in the order coercion, secrecy, verification with "Yes" and "No" buttons, "Question N of 3" progress, and a "Back" control that keeps earlier answers
    - After the third answer call `onSubmit({ coercion, secrecy, verification })`
    - `role="dialog"`, `aria-modal="true"`, `aria-labelledby` title, keyboard focus trap (Tab and Shift+Tab loop), focus moved to the first answer button on each new question
    - On network failure show the `UnreachableNotice` and keep the three answers so retry resubmits them
    - Amber surface (`#FEF3C7` / `#92400E`), questions at 18px or larger, no red
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.7, 4.8, 4.10, 13.3, 13.6_

  - [ ] 9.3 Implement `CoolingOffVaultCard.tsx`
    - Props `{ view, onExpire, onFastForward, fastForwardHidden }`; vault icon, protective-pause message, countdown from `useCountdown` inside `role="timer"` with the spoken `aria-label`, countdown at 20px or larger
    - "Demo: skip the wait" button hidden when `fastForwardHidden` is true
    - Deep teal surface `#0F766E` with white text
    - _Requirements: 5.1, 5.5, 5.6, 5.7, 5.8, 5.9, 5.10, 13.3, 13.5_

  - [ ] 9.4 Implement `PresetBar.tsx`
    - Three buttons from `PRESETS` labeled "Safe daily payment", "Medium-risk unusual transfer" and "High-risk panic transfer"
    - All three disabled while `busy`, with a persistent "Checking this payment…" status
    - _Requirements: 2.1, 2.10, 13.4, 13.8_

  - [ ] 9.5 Implement `UserPhonePanel.tsx`
    - Show the payee label, the preset's Masked_Identifier, the amount with `₹` (from `metadata.amount_inr` when present, else the preset), a status line with a lucide icon, and the View `reason`
    - Switch on `deriveUserScreen`: PROMPT, SENT, CHECKING (status line plus `RealityCheckModal`), CONTINUE ("Continue payment" button and calm message, moves to the SENT confirmation through local state), LOCKED (`CoolingOffVaultCard` and the red "Block & Report (1930)" button), RELEASED (names the family guardian's approval), CANCELLED (`EvidenceCard`), RECONFIRM ("Yes, this is my payment" calls `reconfirm`; "Cancel and keep my money" dispatches `CANCEL_RECONFIRM` with no request), DONE
    - Show inline notices and the `UnreachableNotice` until the state changes or the user acts; no toasts
    - _Requirements: 2.6, 2.7, 2.8, 2.9, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 4.5, 4.6, 4.9, 12.1, 13.7, 13.8_

  - [ ] 9.6 Implement `GuardianPhonePanel.tsx`
    - Switch on `deriveGuardianScreen`: IDLE ("Nothing needs your attention right now"), ALERT (`reason`, amount, payee Masked_Identifier, countdown, "I Spoke With Them — Safe to Approve" and the red "Block & Report Scam (1930)"), APPROVED ("Payment approved"), VETOED (`EvidenceCard`)
    - First tap on approve sets local `confirming` and sends nothing; "Are you sure you spoke with them directly?" with "Yes, approve" (sends APPROVE with `confirmed` true) and "Go back" (no request); `CONFIRMATION_REQUIRED` keeps the second step visible
    - 403 shows the approval-link-no-longer-valid message with a "Refresh" button; 409 shows the "someone already acted" message
    - Show only `reason`, amount and Masked_Identifiers; take the Guardian_Token from reducer state and never write it to storage, cookies or the URL
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8, 6.9, 6.10, 6.11, 6.12, 6.13, 6.14, 13.7_

  - [ ] 9.7 Implement `frontend/src/App.tsx` and mount it in `main.tsx`
    - Root `flex flex-col gap-6 lg:flex-row` layout: Preset_Bar above both panels; User_Phone_Panel ("Your phone") on the left and Guardian_Phone_Panel ("Family guardian's phone") on the right at 1024px and wider, stacked below that width
    - Call `useSimulator` once and pass state and commands down; no login, sign-up or configuration step
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.5, 8.8, 14.6_

  - [ ]* 9.8 Write component tests for the Reality_Check_Modal flow
    - File: `frontend/src/components/RealityCheckModal.test.tsx`
    - One question at a time in order, Back keeps answers, submitted body, close on a LOCKED_15M result, close and show the returned state on 409, unreachable retry resubmits the kept answers, `role="dialog"`, `aria-modal`, focus trap and focus move
    - _Requirements: 15.6, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10_

  - [ ]* 9.9 Write property test for the reality-check body
    - File: `frontend/src/components/RealityCheckModal.answers.pbt.test.tsx`
    - **Property 13: Reality-check body reflects the answers**
    - **Validates: Requirements 4.4**

  - [ ]* 9.10 Write component tests for the lock state
    - File: `frontend/src/components/lockState.test.tsx`
    - The vault card shows the countdown, a `role="timer"` element and the fast-forward button; the Guardian panel shows the alert; a fast-forward 404 hides the button for the session; the Block & Report button sends the VETO body with the held token
    - _Requirements: 15.7, 5.1, 5.7, 5.8, 5.9, 6.2, 3.9, 3.10_

  - [ ]* 9.11 Write component tests for guardian approval
    - File: `frontend/src/components/guardianApprove.test.tsx`
    - No request after the first tap, second tap sends `{ action: "APPROVE", token, confirmed: true }`, "Go back" sends nothing, `CONFIRMATION_REQUIRED` keeps the second step, 403 shows the link message and "Refresh", 409 applies the returned view and shows the already-acted message
    - _Requirements: 15.8, 6.4, 6.5, 6.6, 6.8, 6.9, 6.11, 6.12_

  - [ ]* 9.12 Write component tests for the veto flow
    - File: `frontend/src/components/veto.test.tsx`
    - VETO request from both phones, Evidence_Card text, `tel:1930` link, `cybercrime.gov.in` link with `rel="noopener noreferrer"`, copy success and clipboard-unavailable messages
    - _Requirements: 15.9, 3.10, 6.7, 6.10, 7.1, 7.2, 7.3, 7.4, 7.5_

  - [ ]* 9.13 Write component tests for the presets
    - File: `frontend/src/App.presets.test.tsx`
    - For each preset: request body sent and the panel shown for INSTANT_PASS, REALITY_CHECK and LOCKED_15M (with the Guardian alert); all three buttons disabled with "Checking this payment…" while evaluate is in flight; pressing a preset discards the previous vault and disposes the old sync; payee label and Masked_Identifier shown; no login step
    - _Requirements: 15.5, 1.5, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10_

  - [ ]* 9.14 Write component tests for the Unreachable_Notice
    - File: `frontend/src/components/unreachable.test.tsx`
    - Exact message text, "Try again" repeats the same failed request, the notice stays until the retry succeeds or a new preset starts, HTTP 400 shows the server message, HTTP 500 shows the notice, poll loss shows it until a poll succeeds
    - _Requirements: 15.11, 9.1, 9.2, 9.3, 9.4, 9.5, 9.7_

  - [ ]* 9.15 Write property test for calm copy
    - File: `frontend/src/lib/copy.pbt.test.tsx`
    - **Property 15: Copy is calm**
    - Check every string in `ALL_COPY` and the rendered text, `aria-label` and `title` of the App for every Vault_State fixture against the Banned_Terms, case-insensitively
    - **Validates: Requirements 13.1, 13.2**

- [ ] 10. Add smoke checks
  - [ ]* 10.1 Write smoke tests for project-wide rules
    - File: `frontend/src/test/smoke.test.ts`
    - Source scan of `src/` outside `src/test/` finds no `localStorage`, `sessionStorage`, `document.cookie`, `console.` or `FF0000`; a spy on `Storage.prototype.setItem` and `document.cookie` stays untouched through a locked-to-approved flow; WCAG contrast of each theme text and background pair is at least 7:1 for alert text and the countdown; every rendered `button` and `a` has a `min-h-12` class; `package.json` has no Redux, Angular or other state library
    - _Requirements: 6.14, 12.3, 13.4, 13.5, 13.6, 14.5, 9.6_

- [ ] 11. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP.
- Task 2.1 is the open item flagged in this feature's design: the vault design names `EvaluateRequest` and `Metadata` without declaring fields. It should land before `cooling-off-vault` tasks 3.4 and 3.5 so those tasks reuse these types. Until then `frontend/src/lib/api.ts` is the single place on the client that holds the field names.
- Property tests for the masker and the reducer are split into one file per property, so the file names differ from the grouped names in the design's testing table. This keeps parallel test tasks from editing the same file.
- Property 7 (terminal states stop the sync) depends on `isTerminal` from task 5.7 and `VaultSync` from task 7.6.
- Each task references specific requirements for traceability; property tasks cite the design property number.
- Property tests validate universal correctness properties; unit, component and smoke tests validate specific examples.
- Running `npm run build` and `npm run typecheck` is part of the developer's verification and is not a task here.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "2.2", "3.1", "3.8"] },
    { "id": 2, "tasks": ["3.2", "3.3", "3.4", "3.5", "3.6", "3.7", "5.1"] },
    { "id": 3, "tasks": ["5.2", "5.3", "5.4", "5.5", "5.7", "5.9"] },
    { "id": 4, "tasks": ["5.6", "5.8", "5.10", "5.11", "7.1", "7.6", "9.1", "9.3", "9.4"] },
    { "id": 5, "tasks": ["7.2", "7.3", "7.4", "7.5", "7.7", "7.8", "7.9", "9.2"] },
    { "id": 6, "tasks": ["9.5", "9.6", "9.9"] },
    { "id": 7, "tasks": ["9.7"] },
    { "id": 8, "tasks": ["9.8", "9.10", "9.11", "9.12", "9.13", "9.14", "9.15", "10.1"] }
  ]
}
```
