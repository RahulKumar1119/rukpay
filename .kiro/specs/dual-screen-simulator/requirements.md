# Requirements Document

## Introduction

The Dual-Screen Simulator is the RukPay frontend in `frontend/`. It is a React 18 and TypeScript (strict) application built with Vite and styled with Tailwind CSS, with `lucide-react` icons. Desktop view shows the User Payment Phone on the left and the Family Guardian Phone on the right in one viewport, so a judge can watch an end-to-end co-approval or veto without switching screens.

The simulator talks to the real RukPay backend only (no mock mode). It builds on two existing specs:

- `behavioral-risk-engine`: the five Signals (`amount_inr`, `beneficiary_age_hours`, `is_late_night`, `active_unknown_call`, `rapid_retry_count`), the Risk_Score thresholds (below 40, 40 to 74, 75 and above) and the Vault_States.
- `cooling-off-vault`: the six HTTP endpoints, the Vault_View JSON body (`vault_id`, `state`, `settled`, `risk_score`, `remaining_seconds`, `reconfirm_required`, `reason`, `metadata`, `guardian_token`, `evidence_summary`), the error body `{ "error": { "code", "message", "field" } }`, the reality-check route that accepts either `answers` or `reconfirm: true`, and the guardian action body `{ action, token, confirmed }` with the result status `OK` or `CONFIRMATION_REQUIRED`.

Design decisions fixed for this feature:

- Real-time sync uses `EventSource` on `GET /api/v1/events/stream?vault_id=`. When the stream fails or closes, the client falls back to polling `GET /api/v1/vault/{id}` every 2 seconds.
- The backend is the only data source. When the backend cannot be reached, the simulator shows a calm message and a retry button.
- Tests use Vitest, Testing Library and fast-check.
- All user-facing copy and visual design follow the `anti-panic-ux` steering document. All stack choices follow the `tech-stack` steering document.

## Glossary

- **Simulator**: The React application in `frontend/`.
- **App**: The root component `App.tsx` that renders the Preset_Bar and the two phone panels.
- **User_Phone_Panel**: The left panel, component `UserPhonePanel.tsx`, simulating the payer's UPI or bank transfer screen.
- **Guardian_Phone_Panel**: The right panel, component `GuardianPhonePanel.tsx`, simulating the trusted family co-approver screen.
- **Reality_Check_Modal**: The component `RealityCheckModal.tsx` that asks the three calm safety questions.
- **Cooling_Off_Vault_Card**: The component `CoolingOffVaultCard.tsx` that shows the live countdown and the Fast_Forward_Button.
- **Fast_Forward_Button**: The control in the Cooling_Off_Vault_Card that calls `POST /api/v1/vault/{id}/demo-fast-forward`.
- **Preset_Bar**: The row of three one-click preset buttons at the top of the App.
- **Preset**: One of the three fixed demo scenarios: Safe_Preset, Medium_Preset, High_Risk_Preset.
- **Safe_Preset**: ₹450 to a known local store, beneficiary age 720 hours, daytime, no unknown call, zero retries. Expected state INSTANT_PASS.
- **Medium_Preset**: ₹18,000 to a new contact, beneficiary age 6 hours, daytime, no unknown call, zero retries. Expected state REALITY_CHECK.
- **High_Risk_Preset**: ₹85,000 at 11:45 PM to a brand-new account, beneficiary age 1 hour, late night, active unknown call, zero retries. Expected state LOCKED_15M with a Guardian alert.
- **API_Client**: The typed module `lib/api.ts` that wraps every backend call.
- **Privacy_Masker**: The module `lib/privacyMasker.ts` that masks and hashes sensitive identifiers.
- **Vault_View**: The JSON body the backend returns for a vault, as defined in `cooling-off-vault`.
- **Vault_State**: One of INSTANT_PASS, REALITY_CHECK, LOCKED_15M, GUARDIAN_APPROVED, VETOED, EXPIRED.
- **Vault_Sync**: The client logic that keeps the Vault_View current using Event_Stream with Poll_Fallback.
- **Event_Stream**: The `EventSource` connection to `GET /api/v1/events/stream`.
- **Poll_Fallback**: Repeated `GET /api/v1/vault/{id}` requests every 2 seconds.
- **Guardian_Token**: The opaque string in the Vault_View `guardian_token` field while the vault is LOCKED_15M.
- **Countdown**: The remaining time shown by the Cooling_Off_Vault_Card in `mm:ss` form.
- **Evidence_Card**: The card shown after a veto that holds the `evidence_summary`, the helpline number `1930` and `cybercrime.gov.in`.
- **Unreachable_Notice**: The calm message and retry button shown when the backend cannot be reached.
- **Masked_Identifier**: An identifier in one of the Masking_Formats.
- **Masking_Formats**: Account `XXXX-XXXX-4821`, phone `+91-XXXXXX3210`, UPI `r***@okaxis`, and `[REDACTED]` for OTP, PIN and password values.
- **Sensitive_Value**: A raw account number, phone number, UPI id, OTP, PIN or password.
- **Banned_Terms**: The jargon and alarmist phrases listed in the `anti-panic-ux` steering document.

## Requirements

### Requirement 1: Dual-Screen Layout

**User Story:** As a hackathon judge, I want the payer and the family guardian visible side by side, so that I can follow the full co-approval flow in one viewport.

#### Acceptance Criteria

1. WHILE the viewport width is at least 1024 pixels, THE App SHALL render the User_Phone_Panel on the left and the Guardian_Phone_Panel on the right in the same row.
2. WHILE the viewport width is below 1024 pixels, THE App SHALL stack the User_Phone_Panel above the Guardian_Phone_Panel in a single column.
3. THE App SHALL render the Preset_Bar above both panels.
4. THE App SHALL render each panel inside a phone-shaped frame with an accessible label naming the panel ("Your phone" and "Family guardian's phone").
5. THE App SHALL require no login, sign-up or configuration step before the first preset can be used.

### Requirement 2: One-Click Presets

**User Story:** As a hackathon judge, I want three one-click scenarios, so that I can see each protection level without typing anything.

#### Acceptance Criteria

1. THE Preset_Bar SHALL show exactly three buttons labeled "Safe daily payment", "Medium-risk unusual transfer" and "High-risk panic transfer".
2. WHEN the Safe_Preset button is pressed, THE API_Client SHALL send `POST /api/v1/transactions/evaluate` with `amount_inr` 450, `beneficiary_age_hours` 720, `is_late_night` false, `active_unknown_call` false and `rapid_retry_count` 0.
3. WHEN the Medium_Preset button is pressed, THE API_Client SHALL send `POST /api/v1/transactions/evaluate` with `amount_inr` 18000, `beneficiary_age_hours` 6, `is_late_night` false, `active_unknown_call` false and `rapid_retry_count` 0.
4. WHEN the High_Risk_Preset button is pressed, THE API_Client SHALL send `POST /api/v1/transactions/evaluate` with `amount_inr` 85000, `beneficiary_age_hours` 1, `is_late_night` true, `active_unknown_call` true and `rapid_retry_count` 0.
5. WHEN a preset is pressed, THE App SHALL discard any previous vault, close any open Event_Stream, stop any Poll_Fallback, and then start the new scenario.
6. WHEN the evaluate response returns INSTANT_PASS, THE User_Phone_Panel SHALL show a payment-sent confirmation in plain language.
7. WHEN the evaluate response returns REALITY_CHECK, THE User_Phone_Panel SHALL open the Reality_Check_Modal.
8. WHEN the evaluate response returns LOCKED_15M, THE User_Phone_Panel SHALL show the Cooling_Off_Vault_Card and THE Guardian_Phone_Panel SHALL show the Guardian alert for the same vault.
9. THE User_Phone_Panel SHALL show each preset's payee label as a name only ("Local store", "New contact", "New account") together with a Masked_Identifier.
10. WHILE an evaluate request is in flight, THE Preset_Bar SHALL disable all three buttons and show a calm "Checking this payment…" status.

### Requirement 3: User Phone Panel

**User Story:** As a payer, I want a simple transfer screen that tells me clearly what is happening, so that I stay calm while my payment is checked.

#### Acceptance Criteria

1. THE User_Phone_Panel SHALL show the payee label, the Masked_Identifier, the amount in rupees with the ₹ symbol and a status line for the current Vault_State.
2. WHILE no vault exists, THE User_Phone_Panel SHALL show a prompt that tells the user to choose a preset.
3. WHILE the Vault_State is REALITY_CHECK and `settled` is true, THE User_Phone_Panel SHALL show a "Continue payment" button and a calm message that the checks look fine.
4. WHILE the Vault_State is GUARDIAN_APPROVED, THE User_Phone_Panel SHALL show a payment-released confirmation naming the family guardian's approval.
5. WHILE the Vault_State is VETOED, THE User_Phone_Panel SHALL show the Evidence_Card.
6. WHILE the Vault_State is EXPIRED and `reconfirm_required` is true, THE User_Phone_Panel SHALL show a final calm re-confirmation step with a "Yes, this is my payment" button and a "Cancel and keep my money" button.
7. WHEN the user presses "Yes, this is my payment", THE API_Client SHALL send `POST /api/v1/vault/{id}/reality-check` with the body `{ "reconfirm": true }`.
8. WHEN the user presses "Cancel and keep my money" during re-confirmation, THE User_Phone_Panel SHALL return to the prompt that tells the user to choose a preset, without sending a payment request.
9. WHILE the Vault_State is LOCKED_15M, THE User_Phone_Panel SHALL show a "Block & Report (1930)" button.
10. WHEN the user presses "Block & Report (1930)", THE API_Client SHALL send `POST /api/v1/vault/{id}/guardian-action` with `action` VETO and the Guardian_Token from the latest Vault_View.
11. THE User_Phone_Panel SHALL show the `reason` text from the Vault_View as the explanation for the current state.

### Requirement 4: Reality Check Modal

**User Story:** As a payer who may be under pressure, I want three calm questions one at a time, so that I can think clearly before my money moves.

#### Acceptance Criteria

1. WHILE the Vault_State is REALITY_CHECK and `settled` is false, THE Reality_Check_Modal SHALL be open.
2. THE Reality_Check_Modal SHALL show one question at a time in this order: Coercion Check, Secrecy Check, Verification Check, with the wording from the `anti-panic-ux` steering document.
3. THE Reality_Check_Modal SHALL show a "Yes" button and a "No" button for each question and a progress indicator naming the current question ("Question 1 of 3").
4. WHEN the user answers the third question, THE API_Client SHALL send `POST /api/v1/vault/{id}/reality-check` with the body `{ "answers": { "coercion": "YES|NO", "secrecy": "YES|NO", "verification": "YES|NO" } }` holding the three recorded answers.
5. WHEN the backend returns LOCKED_15M after a reality check, THE Reality_Check_Modal SHALL close and THE User_Phone_Panel SHALL show the Cooling_Off_Vault_Card.
6. WHEN the backend returns REALITY_CHECK with `settled` true, THE Reality_Check_Modal SHALL close and THE User_Phone_Panel SHALL show the "Continue payment" button.
7. WHILE the Reality_Check_Modal is open, THE Reality_Check_Modal SHALL trap keyboard focus, move focus to the first answer button on each new question, and be announced with `role="dialog"`, `aria-modal="true"` and an accessible title.
8. THE Reality_Check_Modal SHALL provide a "Back" control that returns to the previous question and keeps the earlier answers.
9. IF the reality-check request fails with HTTP 409, THEN THE Reality_Check_Modal SHALL close and THE App SHALL show the state returned in the 409 body.
10. IF the reality-check request cannot reach the backend, THEN THE Reality_Check_Modal SHALL show the Unreachable_Notice and keep the three answers so the retry button resubmits them.

### Requirement 5: Cooling-Off Vault Card

**User Story:** As a payer, I want to see my money held safely with a visible countdown, so that I understand the pause is protective.

#### Acceptance Criteria

1. WHILE the Vault_State is LOCKED_15M, THE Cooling_Off_Vault_Card SHALL show a vault icon, a plain-language protective-pause message and the Countdown in `mm:ss` form.
2. WHEN a Vault_View with state LOCKED_15M arrives, THE Cooling_Off_Vault_Card SHALL compute a local end time as the current client time plus `remaining_seconds`.
3. WHILE the Vault_State is LOCKED_15M, THE Cooling_Off_Vault_Card SHALL update the Countdown once per second from the local end time and SHALL display `00:00` rather than a negative value.
4. WHEN a Vault_View with a different `remaining_seconds` arrives, THE Cooling_Off_Vault_Card SHALL recompute the local end time from that Vault_View.
5. WHEN the Countdown reaches `00:00`, THE Cooling_Off_Vault_Card SHALL request the current Vault_View once and SHALL show the state the backend returns.
6. THE Cooling_Off_Vault_Card SHALL present the Countdown inside an element with `role="timer"` and an `aria-label` that spells the remaining time in minutes and seconds, and SHALL update that label no more than once every 10 seconds to avoid constant screen-reader interruption.
7. WHERE the backend accepts demo fast-forward, THE Cooling_Off_Vault_Card SHALL show the Fast_Forward_Button labeled "Demo: skip the wait".
8. WHEN the Fast_Forward_Button is pressed, THE API_Client SHALL send `POST /api/v1/vault/{id}/demo-fast-forward` and THE Cooling_Off_Vault_Card SHALL apply the Vault_View returned.
9. IF the fast-forward request returns HTTP 404, THEN THE Cooling_Off_Vault_Card SHALL hide the Fast_Forward_Button for the rest of the session.
10. WHILE the Vault_State is LOCKED_15M, THE Cooling_Off_Vault_Card SHALL use the deep teal palette (`#0F766E`) from the `anti-panic-ux` steering document.

### Requirement 6: Guardian Phone Panel

**User Story:** As a trusted family member, I want a clear summary and two safe actions, so that I can approve only after I have spoken with the payer, or block a scam.

#### Acceptance Criteria

1. WHILE no vault is in LOCKED_15M, THE Guardian_Phone_Panel SHALL show a calm "Nothing needs your attention right now" message.
2. WHILE the Vault_State is LOCKED_15M, THE Guardian_Phone_Panel SHALL show the plain-language `reason`, the amount, the payee's Masked_Identifier and the Countdown.
3. WHILE the Vault_State is LOCKED_15M, THE Guardian_Phone_Panel SHALL show two buttons: "I Spoke With Them — Safe to Approve" and "Block & Report Scam (1930)".
4. WHEN the guardian presses "I Spoke With Them — Safe to Approve" the first time, THE Guardian_Phone_Panel SHALL show a second confirmation step with the text "Are you sure you spoke with them directly?" and a "Yes, approve" button and a "Go back" button, and SHALL send no approve request.
5. WHEN the guardian presses "Yes, approve", THE API_Client SHALL send `POST /api/v1/vault/{id}/guardian-action` with `action` APPROVE, the Guardian_Token and `confirmed` true.
6. WHEN the guardian presses "Go back", THE Guardian_Phone_Panel SHALL return to the two-button view without sending a request.
7. WHEN the guardian presses "Block & Report Scam (1930)", THE API_Client SHALL send `POST /api/v1/vault/{id}/guardian-action` with `action` VETO, the Guardian_Token and `confirmed` true.
8. WHEN the guardian action result has status `CONFIRMATION_REQUIRED`, THE Guardian_Phone_Panel SHALL keep the second confirmation step visible.
9. WHEN the Vault_State becomes GUARDIAN_APPROVED, THE Guardian_Phone_Panel SHALL show a "Payment approved" confirmation.
10. WHEN the Vault_State becomes VETOED, THE Guardian_Phone_Panel SHALL show the Evidence_Card.
11. IF a guardian action returns HTTP 403, THEN THE Guardian_Phone_Panel SHALL show a calm message that the approval link is no longer valid and SHALL offer a "Refresh" button that requests the current Vault_View.
12. IF a guardian action returns HTTP 409, THEN THE Guardian_Phone_Panel SHALL apply the Vault_View carried in the 409 body and SHALL show a calm message that someone already acted on this payment.
13. THE Guardian_Phone_Panel SHALL show only the `reason`, the amount and Masked_Identifiers, and SHALL NOT show full account numbers, raw UPI ids or private messages.
14. THE Guardian_Phone_Panel SHALL take the Guardian_Token from the latest Vault_View and SHALL hold it in memory only, never in `localStorage`, `sessionStorage`, cookies or the URL.

### Requirement 7: Evidence Card After Veto

**User Story:** As a scam victim, I want clear next steps after a block, so that I know how to report the scam.

#### Acceptance Criteria

1. WHEN the Vault_State is VETOED, THE Evidence_Card SHALL show the text of `evidence_summary` from the Vault_View.
2. WHEN the Vault_State is VETOED, THE Evidence_Card SHALL show the National Cybercrime Helpline number `1930` as a `tel:1930` link and the address `cybercrime.gov.in` as a link that opens in a new tab with `rel="noopener noreferrer"`.
3. THE Evidence_Card SHALL state that the transfer has been permanently cancelled and that no money has left the account.
4. THE Evidence_Card SHALL provide a "Copy summary" button that copies `evidence_summary` to the clipboard and confirms the copy with persistent on-screen text.
5. IF the clipboard is unavailable, THEN THE Evidence_Card SHALL leave the summary visible and selectable and show a calm message to select and copy it by hand.

### Requirement 8: Real-Time Vault Sync

**User Story:** As a judge, I want both phones to update immediately when either side acts, so that the demo feels live.

#### Acceptance Criteria

1. WHEN a vault is created, THE Vault_Sync SHALL open an Event_Stream to `GET /api/v1/events/stream?vault_id={id}` using the `VITE_API_BASE_URL` origin.
2. WHEN the Event_Stream delivers a `message` event, THE Vault_Sync SHALL parse the data as a Vault_View and publish it to both phone panels.
3. WHEN a Vault_View arrives that has no `guardian_token`, THE Vault_Sync SHALL keep the last Guardian_Token received for the same vault.
4. IF the Event_Stream raises an error or closes while the vault is not in a terminal state, THEN THE Vault_Sync SHALL close the Event_Stream and start the Poll_Fallback.
5. WHILE the Poll_Fallback is active, THE Vault_Sync SHALL request `GET /api/v1/vault/{id}` every 2 seconds and publish each Vault_View.
6. WHILE the Poll_Fallback is active, THE Vault_Sync SHALL wait for the previous poll request to finish before it sends the next one.
7. WHEN the Vault_State is INSTANT_PASS, GUARDIAN_APPROVED, VETOED, or EXPIRED with `reconfirm_required` false, THE Vault_Sync SHALL close the Event_Stream and stop the Poll_Fallback.
8. WHEN the App unmounts or a new preset starts, THE Vault_Sync SHALL close the Event_Stream and cancel all pending polls and timers.
9. IF an incoming Vault_View carries a `vault_id` that differs from the current vault, THEN THE Vault_Sync SHALL discard it.
10. WHERE the Event_Stream is open, THE Vault_Sync SHALL also accept Vault_Views returned directly by action responses and SHALL apply whichever view is newer by comparing the order in which the responses arrived.

### Requirement 9: Backend Unreachable Handling

**User Story:** As a judge or user, I want a calm message when the backend is down, so that I know what to do instead of seeing an error page.

#### Acceptance Criteria

1. IF any API_Client request fails because the network is unreachable or times out after 10 seconds, THEN THE Simulator SHALL show the Unreachable_Notice with the text "We can't reach RukPay right now. Your money has not moved. Please try again." and a "Try again" button.
2. WHEN the "Try again" button is pressed, THE Simulator SHALL repeat the request that failed.
3. THE Unreachable_Notice SHALL stay on screen until the retry succeeds or the user chooses a new preset.
4. IF the backend returns HTTP 400 with an error body, THEN THE Simulator SHALL show the `message` from the body in plain language without technical terms.
5. IF the backend returns HTTP 500 or an unreadable body, THEN THE Simulator SHALL show the Unreachable_Notice.
6. THE Simulator SHALL contain no mock data source, no mock mode switch and no code path that fabricates a Vault_View.
7. IF the Poll_Fallback loses connection, THEN THE Vault_Sync SHALL keep polling every 2 seconds and THE Simulator SHALL show the Unreachable_Notice until a poll succeeds.

### Requirement 10: Typed API Client

**User Story:** As a developer, I want one typed client for every backend call, so that the contract is checked at compile time.

#### Acceptance Criteria

1. THE API_Client SHALL read the base URL from `import.meta.env.VITE_API_BASE_URL` and SHALL fall back to `http://localhost:8080` when the variable is empty.
2. THE API_Client SHALL export TypeScript types for the evaluate request, the Vault_View, the reality-check request (a union of `answers` and `reconfirm`), the guardian action request, the guardian action result and the error body.
3. THE API_Client SHALL export one function for each of: evaluate, get vault, submit reality check answers, reconfirm, guardian action, demo fast-forward, and a function that returns the Event_Stream URL for a vault id.
4. THE API_Client SHALL send JSON with the header `Content-Type: application/json` and SHALL parse every response body as JSON.
5. WHEN the backend returns a non-2xx status, THE API_Client SHALL throw a typed `ApiError` that carries the status, the error `code`, the `message`, the optional `field` and, for HTTP 409, the Vault_View from the body.
6. WHEN a request cannot reach the backend, THE API_Client SHALL throw a typed `NetworkError`.
7. THE API_Client SHALL pass an `AbortSignal` to each request so callers can cancel it.
8. THE API_Client SHALL pass every outbound body through the Privacy_Masker redaction before dispatch, so that no Sensitive_Value is sent.
9. THE API_Client SHALL write nothing to the console and SHALL NOT include a Guardian_Token or Masked_Identifier in any thrown error message.

### Requirement 11: Privacy Masker

**User Story:** As a privacy-conscious user, I want sensitive identifiers masked before they leave my device, so that no one sees my full account, phone or UPI details.

#### Acceptance Criteria

1. WHEN `maskAccount` receives a string with at least four digits, THE Privacy_Masker SHALL return `XXXX-XXXX-` followed by the last four digits.
2. WHEN `maskPhone` receives a string with at least four digits, THE Privacy_Masker SHALL return `+91-XXXXXX` followed by the last four digits.
3. WHEN `maskUpi` receives a string of the form `name@handle`, THE Privacy_Masker SHALL return the first character of `name`, then `***`, then `@` and the handle.
4. WHEN `redactSecret` receives any string, THE Privacy_Masker SHALL return `[REDACTED]`.
5. IF `maskAccount` or `maskPhone` receives a string with fewer than four digits, or `maskUpi` receives a string without `@` or with an empty name or handle, THEN THE Privacy_Masker SHALL return `[REDACTED]`.
6. THE Privacy_Masker SHALL produce account output that matches `^XXXX-XXXX-\d{4}$`, phone output that matches `^\+91-X{6}\d{4}$` and UPI output that matches `^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.\-]+$` for every input that has a valid masked form, so that the output passes the backend `ValidateMasked` check.
7. THE Privacy_Masker SHALL export `hashIdentifier(value)` that returns the lowercase hex SHA-256 digest of the trimmed, lowercased value, computed with `crypto.subtle.digest`.
8. WHEN `hashIdentifier` receives the same value twice, THE Privacy_Masker SHALL return the same digest, and WHEN it receives different values, THE Privacy_Masker SHALL return different digests.
9. THE Privacy_Masker SHALL export `redactObject(value)` that returns a deep copy in which every property named `otp`, `pin`, `password`, `passcode` or `cvv` (case-insensitive) holds `[REDACTED]`, and every string that matches an unmasked account, phone or UPI pattern is replaced by its masked form.
10. THE Privacy_Masker SHALL produce output that holds no digit run longer than four digits from the input for account and phone masking.
11. THE Privacy_Masker SHALL be pure apart from `hashIdentifier`, and SHALL NOT write to the console, storage, the network or the DOM.

### Requirement 12: Privacy in State, Logs and Network

**User Story:** As a privacy-conscious user, I want no raw identifiers kept or sent anywhere, so that the simulator cannot leak them.

#### Acceptance Criteria

1. THE Simulator SHALL hold only Masked_Identifiers in React state, in context, in props and in any hook.
2. THE Preset definitions SHALL contain only Masked_Identifiers and plain payee labels, and SHALL contain no raw account number, UPI id or phone number.
3. THE Simulator SHALL write no vault data, Masked_Identifier or Guardian_Token to `console`, `localStorage`, `sessionStorage`, cookies or URL parameters.
4. THE Simulator SHALL send no OTP, PIN or password value to the backend, and no UI control SHALL ask for one.
5. THE Simulator SHALL send only the Signals, Masked_Identifiers, the reality-check answers, the reconfirm flag and guardian action fields defined in `cooling-off-vault`.
6. THE Simulator SHALL keep the `evidence_summary` text in memory only.

### Requirement 13: Anti-Panic Copy and Visual Standards

**User Story:** As an elderly or anxious user, I want calm, readable, easy-to-tap screens, so that I can act correctly under stress.

#### Acceptance Criteria

1. THE Simulator SHALL write all user-facing copy in plain, conversational English at a 6th-grade reading level and SHALL contain none of the Banned_Terms in any rendered text, `aria-label` or `title` attribute.
2. THE Simulator SHALL describe the 15-minute lock as a protective pause and SHALL NOT describe it as an accusation or a restriction.
3. THE Simulator SHALL render body text at 16 pixels or larger, and the reality-check questions, the Countdown and the state status line at 18 pixels or larger.
4. THE Simulator SHALL render every interactive control with a hit area of at least 48 by 48 pixels and at least 12 pixels of space between adjacent controls.
5. THE Simulator SHALL use text and background colors with a contrast ratio of at least 7:1 for all alert text and the Countdown.
6. THE Simulator SHALL use the warm amber surface (`#FEF3C7` background, `#92400E` text) or the calm slate surface (`#1E293B` / `#F8FAFC`) for the Reality_Check_Modal and SHALL NOT use pure red (`#FF0000`) anywhere.
7. THE Simulator SHALL reserve the red color `#DC2626` with white text for the "Block & Report" buttons only.
8. THE Simulator SHALL keep every safety warning and status message on screen until the user acts or the state changes, and SHALL NOT use auto-dismissing toasts or snackbars.
9. THE Simulator SHALL show a visible focus ring on every interactive control and SHALL make every control reachable and operable by keyboard alone.
10. THE Simulator SHALL show a lucide-react icon alongside each state status line, and SHALL NOT convey state by color alone.
11. WHERE the user has set `prefers-reduced-motion: reduce`, THE Simulator SHALL disable non-essential animation.
12. THE Simulator SHALL label every icon-only control with an `aria-label`.

### Requirement 14: Project Setup and Build

**User Story:** As a developer, I want a standard Vite project with strict TypeScript, so that I can build, test and deploy it with one command each.

#### Acceptance Criteria

1. THE Simulator SHALL live in `frontend/` with React 18 or later, TypeScript with `"strict": true`, Vite, Tailwind CSS and `lucide-react`.
2. THE Simulator SHALL provide the npm scripts `dev`, `build`, `preview`, `test` and `typecheck`, where `test` runs Vitest once without watch mode.
3. WHEN `npm run build` runs, THE Simulator SHALL emit a static bundle to `frontend/dist/` and finish with no TypeScript error.
4. THE Simulator SHALL read `VITE_API_BASE_URL` at build time, and SHALL ship a `frontend/.env.example` that names it.
5. THE Simulator SHALL use native `fetch` and `EventSource` for networking and SHALL NOT depend on Redux, Angular or another state library.
6. THE Simulator SHALL place components in `frontend/src/components/` and helpers in `frontend/src/lib/` with the file names `UserPhonePanel.tsx`, `RealityCheckModal.tsx`, `CoolingOffVaultCard.tsx`, `GuardianPhonePanel.tsx`, `api.ts` and `privacyMasker.ts`.

### Requirement 15: Automated Tests

**User Story:** As a developer, I want automated tests for masking, the countdown and each user flow, so that I can change the code with confidence.

#### Acceptance Criteria

1. THE Simulator SHALL include Vitest unit tests for the Privacy_Masker that cover each Masking_Format, the fewer-than-four-digit and malformed cases, `hashIdentifier` and `redactObject`.
2. THE Simulator SHALL include fast-check property tests for the Privacy_Masker, each running at least 100 generated inputs, that check: account and phone output matches the masking regex for any digit string of four or more digits; no input digit run longer than four digits appears in masked output; `hashIdentifier` is deterministic and returns 64 lowercase hex characters; `redactObject` output holds no key named `otp`, `pin` or `password` with a value other than `[REDACTED]`.
3. THE Simulator SHALL include tests for the Countdown logic that use fake timers and cover: the `mm:ss` format, a value that never goes below `00:00`, a recompute when a new `remaining_seconds` arrives, and one fetch at `00:00`.
4. THE Simulator SHALL include a fast-check property test that, for any `remaining_seconds` from 0 to 900, the formatted Countdown matches `^\d{2}:\d{2}$` and converts back to the same number of seconds.
5. THE Simulator SHALL include a Testing Library component test for each preset that checks the request body sent and the panel shown for INSTANT_PASS, REALITY_CHECK and LOCKED_15M.
6. THE Simulator SHALL include a component test for the Reality_Check_Modal that covers the one-question-at-a-time order, the Back control, the submitted `answers` body, and the close on a LOCKED_15M result.
7. THE Simulator SHALL include a component test for the lock state that checks the Cooling_Off_Vault_Card shows the Countdown and the Fast_Forward_Button, and that the Guardian_Phone_Panel shows the alert.
8. THE Simulator SHALL include a component test for the guardian approve flow that checks no request is sent after the first tap, and that the second tap sends `confirmed` true.
9. THE Simulator SHALL include a component test for the veto flow that checks the VETO request, the Evidence_Card, the `tel:1930` link and the `cybercrime.gov.in` link.
10. THE Simulator SHALL include tests for Vault_Sync that cover the fall back from Event_Stream to the 2-second Poll_Fallback, the stop on a terminal state, and cleanup on unmount.
11. THE Simulator SHALL include a test for the Unreachable_Notice that checks the message text and that the "Try again" button repeats the failed request.
12. THE Simulator SHALL run all tests against a stubbed `fetch` and a stubbed `EventSource` inside the test files only, and SHALL ship no mock data source in `src/`.
13. WHEN `npm test` runs, THE Simulator SHALL finish with all tests passing and no network access.
