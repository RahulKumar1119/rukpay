/**
 * An independent description of what redaction must do, for the property tests.
 * It keeps its own copy of the patterns, so a mistake in `privacyMasker.ts`
 * cannot hide itself. Test only.
 */

export const REDACTED = "[REDACTED]";

/** Property names that must always hold `[REDACTED]`, in any letter case. */
export const SECRET_KEY = /^(otp|pin|password|passcode|cvv)$/i;

const MASKED = [/^XXXX-XXXX-\d{4}$/, /^\+91-X{6}\d{4}$/, /^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]+$/];
const RAW_UPI = /^[^\s@]+@[A-Za-z0-9.-]+$/;
const RAW_PHONE = /^(\+91)?[6-9]\d{9}$/;
const RAW_ACCOUNT = /^\d{9,18}$/;

const compact = (s: string): string => s.replace(/[\s-]/g, "");

/** True when `s` is already in one of the three masked formats. */
export const isMasked = (s: string): boolean => MASKED.some((re) => re.test(s));

/** True when `s` is a whole unmasked account number, phone number or UPI id. */
export const looksUnmasked = (s: string): boolean =>
  !isMasked(s) && (RAW_UPI.test(s) || RAW_PHONE.test(compact(s)) || RAW_ACCOUNT.test(compact(s)));
