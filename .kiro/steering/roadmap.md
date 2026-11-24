---
inclusion: auto
name: roadmap
description: Post-MVP roadmap for RukPay, tracked in GitHub issue #1 with sub-issues #2 to #13. Phases, dependencies, scope boundary, guarantees to keep, and open decisions. Use when planning, writing a spec for, or building any roadmap feature, or when asked what to build next.
---

# RukPay Roadmap (GitHub issue #1)

Source of truth: https://github.com/RahulKumar1119/rukpay/issues/1 and its sub-issues #2 to #13. Issue state changes over time, so check GitHub (or run the "Roadmap status" hook) before relying on what is open or closed. This file records the plan, not the status.

## Scope boundary (from #1)
RukPay simulates payment decisions. Never describe a simulated approval, hold or cancellation as a real bank outcome, in the UI, copy, notifications or docs. Say "held in the simulation", not "money saved" or "fraud prevented". Real payment integration is a separate engineering and compliance workstream.

## Guarantees every roadmap feature must keep
- The vault state machine and the 15-minute cooling-off lock in `tech-stack.md`.
- The tone, colors, touch targets and PII masking in `anti-panic-ux.md`.
- Existing scores: with no new input or history, a score stays identical to today's. The deterministic rules remain the baseline, and new signals only add capped, deterministic points.
- Existing API conventions and clients keep working (add fields, do not rename or remove).
- Unit, integration and property-based tests (`rapid`, 1000+ checks) for all new behaviour.
- Go 1.21.1, React 18 with strict TypeScript, DynamoDB, existing SSE.

## Phases and dependencies

| Phase | Issue | Feature | Depends on |
|---|---|---|---|
| 1 | #2 | Explainable risk scores | none (blocks #3, #4, #5, #9) |
| 1 | #3 | Custom transaction input and scenario builder | #2 |
| 1 | #4 | Behavioural risk detection | #2 |
| 1 | #5 | Trusted beneficiary management | #2 |
| 2 | #6 | Independent payer and guardian accounts | none (blocks #7, #8, #9, #11) |
| 2 | #7 | Guardian device registration and notifications | #6 |
| 2 | #8 | Multiple guardians, authorization, audit trail | #6, #7 |
| 3 | #9 | Transaction history | #2, #6 |
| 3 | #10 | Weekly and monthly analytics | #9 |
| 3 | #11 | Personalised protection settings and profiles | #6 (uses #4, #5, #7) |
| 4 | #12 | Hindi and regional languages | none to start (pairs with #13) |
| 4 | #13 | Senior-friendly interface | #11 for saving the choice (pairs with #12) |

Start work in this order. Do not start an issue before the ones it depends on exist, unless the design says how it will stand in for them.

## Open decisions (settle in the design, then record the answer in the issue)
- **#4 payer identity before accounts exist:** use one fixed demo payer on the server, or a pseudonymous profile id.
- **#6 sign-in method:** requirement 12.4 of `dual-screen-simulator` forbids any control that asks for an OTP, PIN or password, and `redactObject` blanks those keys. Choose passkeys, signed one-use links, or narrow 12.4 (spec and masker change together).
- **#12 languages and backend text:** which languages follow Hindi, and whether the backend sends stable message codes or translated text. Keep the existing English fields.
- **#12 and #13 where a preference is stored:** the smoke checks forbid `localStorage`, `sessionStorage` and cookies. Keep it in memory and in settings (#11), or allow one named non-sensitive key and narrow the check.
- **#11 weaker settings:** they take effect only after a stated delay and tell the guardians. Server-side floors that no setting can pass: cooling-off of at least 15 minutes, the lock for a large payment to a new beneficiary during an unknown call, and the second confirmation tap for guardian approval.

## Working rules
- One spec per roadmap issue or small group of issues that ship together. Name the issue numbers in the spec's introduction so the spec and the issue stay linked.
- Settle the open decisions that touch an issue before writing its requirements.
- Update the issue when its design settles a decision, and close it only when its acceptance criteria are met and tests pass.
