---
inclusion: always
---

# RukPay — Anti-Panic UX, Behavioral Psychology & Privacy Steering Document

## 1. Core Psychological Design Philosophy ("De-Escalation First")
When a victim is trapped in a social-engineering scam (such as a "Digital Arrest", fake RBI/CBI threat, or courier customs trap), their sympathetic nervous system is in acute fight-or-flight mode. Traditional banking warnings—flashing red banners, loud error sirens, or dense technical jargon—compound their panic and push them to obey the scammer's instructions faster.

**RukPay's Golden Rule:** *Do not out-shout the scammer; slow down the room.*

---

## 2. Copywriting & Tone Guardrails (Strictly Enforced)

### Mandatory Tone
- **Calm, Grounded & Reassuring:** Write like a trusted, level-headed family member sitting beside the user.
- **Plain, Everyday Language:** Use simple, conversational English (and optional Hindi/Hinglish helper subtitles where helpful) readable at a 6th-grade reading level.
- **Agency-Preserving:** Frame the 15-minute cooling-off lock as a **protective pause** ("We're holding this safely for 15 minutes so you have space to verify"), never as an accusation ("Suspected Fraud Detected" or "Account Restricted").

### Banned Jargon & Alarmist Phrasing (NEVER Use in User-Facing UI)
- **NEVER use technical terms:** `heuristic mismatch`, `anomaly score`, `velocity threshold`, `cryptographic hash`, `HMAC`, `SSL/TLS`, `payload`, `telemetry`, `IP reputation`, or `rule engine`.
- **NEVER use blaming/scary phrases:** `FATAL ERROR`, `SECURITY BREACH`, `ILLEGAL TRANSACTION`, `YOUR ACCOUNT WILL BE BLOCKED`, or `FRAUDSTER DETECTED`.

### Approved Copy Examples
- **Instead of:** *"Error 403: High-velocity transfer anomaly triggered cooling-off state."*
  - **Use:** *"Take a breath. Because this is a large transfer to a new recipient while you're on a call, RukPay has placed a 15-minute safety pause on it."*
- **Instead of:** *"Confirm non-coercion parameters."*
  - **Use:** *"Quick safety check: Is anyone on the phone telling you to keep this payment secret or threatening arrest/account freeze?"*

---

## 3. Visual Hierarchy, Color & Accessibility Standards (Elder-Friendly)

### Color Palette (De-Escalating & High Contrast)
- **Primary Intervention Surface:** Warm Amber / Soft Sand (`#FEF3C7` background with `#92400E` text) or Calm Indigo/Slate (`#1E293B` / `#F8FAFC`) — avoid harsh neon red (`#FF0000`) during the user's reality check.
- **Cooling-Off Vault Lock State:** Deep Shield Teal / Emerald (`#0F766E`) to signal *"Your money is safe inside the vault"* rather than *"Something broke."*
- **Veto / Emergency Helpline Action:** Reserved exclusively for the **"Block & Report (1930)"** button (`#DC2626` with clear white text) so the escape hatch is unmistakable.

### Typography & Touch Targets
- **Minimum Body Font Size:** `16px` (`1rem`) for standard text; `18px–20px` (`1.125rem–1.25rem`) for reality-check questions and timer status.
- **Minimum Interactive Target Size:** `48px × 48px` for all buttons (`Yes` / `No`, `Approve`, `Block & Report`) with at least `12px` spacing between adjacent buttons to prevent accidental mis-taps by trembling hands.
- **Contrast Ratio:** Minimum `7:1` (WCAG AAA) for all alert text and countdown timers.
- **Zero Auto-Dismissing Toasts:** Never hide important safety warnings behind disappearing snackbars; keep state persistent on screen until acted upon.

---

## 4. The 3-Question Calm Reality Check Specification
When `risk_score` is between `40` and `74` (or before entering `LOCKED_15M`), present **one calm card at a time** (or a clean 3-item checklist) with large `Yes` / `No` buttons:

1. **Coercion Check:** *"Are you currently on a phone or video call with someone claiming to be from Police, CBI, Customs, RBI, or your Bank?"*
   - (`Yes` → Immediately escalates to `LOCKED_15M` + alerts Guardian)
2. **Secrecy Check:** *"Did the caller ask you NOT to tell your family or bank staff about this transfer?"*
   - (`Yes` → Immediately escalates to `LOCKED_15M` + alerts Guardian)
3. **Verification Check:** *"Have you independently verified this person or bill without using a phone number they just sent you?"*
   - (`No` → Escalates to `LOCKED_15M` + alerts Guardian)

---

## 5. Guardian Co-Approval Card Rules (Right-Side Panel)
When a transaction enters `LOCKED_15M`, the Family Guardian panel must display:
- **Plain-English Reason Summary:** E.g., *"₹85,000 transfer to a brand-new account at 11:45 PM while on an active unknown call."*
- **Sanitized Metadata Only:** Never expose full bank account numbers, raw UPI handles, or private messages.
- **Two Clear Actions:**
  1. **"I Spoke With Them — Safe to Approve"** (Requires second confirmation tap to prevent accidental approval).
  2. **"Block & Report Scam (1930)"** (Immediately moves vault to `VETOED`, permanently cancels transfer, and displays India's National Cybercrime Helpline `1930` / `cybercrime.gov.in` evidence card).

---

## 6. Strict Client-Side & Server-Side PII Redaction Rules
- **Masking Format:**
  - Account Numbers: `XXXX-XXXX-4821` (last 4 digits only)
  - Phone Numbers: `+91-XXXXXX3210`
  - UPI IDs: `r***@okaxis`
  - OTPs / Passwords / PINs: Strictly `[REDACTED]` — never stored in memory, state, logs, or SSE events.
- **Implementation Enforcement:** All frontend telemetry must pass through `frontend/src/lib/privacyMasker.ts` before network dispatch, and Go backend middleware must strip/mask any unredacted patterns before writing to SQLite/DynamoDB or broadcasting via SSE.
