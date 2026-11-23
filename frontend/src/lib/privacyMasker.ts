/**
 * Client-side privacy masking.
 *
 * Every function here is pure and synchronous, except `hashIdentifier`, which
 * calls `crypto.subtle`. Nothing in this module writes to the console, storage,
 * the network or the DOM (Requirement 11.11).
 *
 * The three masked formats are the ones the backend accepts in
 * `store.ValidateMasked`, so masked output passes that check:
 *
 *   account  XXXX-XXXX-4821
 *   phone    +91-XXXXXX3210
 *   UPI      r***@okaxis
 */

const REDACTED = "[REDACTED]";

/** Property names whose value is always replaced, in any letter case. */
const SECRET_KEY = /^(otp|pin|password|passcode|cvv)$/i;

// A value that is already in one of the masked formats. These are checked first,
// so masking the same value twice gives the same result.
const MASKED_ACCOUNT = /^XXXX-XXXX-\d{4}$/;
const MASKED_PHONE = /^\+91-X{6}\d{4}$/;
const MASKED_UPI = /^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]+$/;

// A whole-string value that looks like an unmasked identifier. The phone and
// account patterns are tested after spaces and hyphens are removed.
const RAW_UPI = /^[^\s@]+@[A-Za-z0-9.-]+$/;
const RAW_PHONE = /^(\+91)?[6-9]\d{9}$/;
const RAW_ACCOUNT = /^\d{9,18}$/;

const UPI_HANDLE = /^[A-Za-z0-9.-]+$/;
const ALNUM_START = /^[A-Za-z0-9]/;

/** The last four ASCII digits of `raw`, or null when it has fewer than four. */
function lastFourDigits(raw: string): string | null {
  const digits = raw.replace(/\D/g, "");
  return digits.length >= 4 ? digits.slice(-4) : null;
}

/** `XXXX-XXXX-1234` from the last four digits, or `[REDACTED]`. */
export function maskAccount(raw: string): string {
  const last4 = lastFourDigits(raw);
  return last4 === null ? REDACTED : `XXXX-XXXX-${last4}`;
}

/** `+91-XXXXXX3210` from the last four digits, or `[REDACTED]`. */
export function maskPhone(raw: string): string {
  const last4 = lastFourDigits(raw);
  return last4 === null ? REDACTED : `+91-XXXXXX${last4}`;
}

/**
 * `r***@okaxis` from `rahul@okaxis`, or `[REDACTED]`.
 *
 * The value is split at its last `@`. The name must start with a letter or a
 * digit and the handle must use only letters, digits, dots and hyphens, so the
 * output always matches the backend's UPI format.
 */
export function maskUpi(raw: string): string {
  const at = raw.lastIndexOf("@");
  if (at < 1) return REDACTED;
  const name = raw.slice(0, at);
  const handle = raw.slice(at + 1);
  if (!ALNUM_START.test(name) || !UPI_HANDLE.test(handle)) return REDACTED;
  return `${name[0]}***@${handle}`;
}

/** Hides any secret, such as an OTP, PIN or password. The input is ignored. */
export function redactSecret(_raw: string): string {
  return REDACTED;
}

/**
 * The lowercase hex SHA-256 digest of the trimmed, lowercased value. Use it to
 * compare or group identifiers without keeping them.
 *
 * `crypto.subtle` exists only in a secure context (HTTPS or localhost). Where it
 * is missing, the returned promise rejects.
 */
export async function hashIdentifier(value: string): Promise<string> {
  const bytes = new TextEncoder().encode(value.trim().toLowerCase());
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Spaces and hyphens removed, so `98765 43210` and `1234-5678-9012` compare as digits. */
function compact(value: string): string {
  return value.replace(/[\s-]/g, "");
}

function redactString(value: string): string {
  if (MASKED_ACCOUNT.test(value) || MASKED_PHONE.test(value) || MASKED_UPI.test(value)) {
    return value;
  }
  if (RAW_UPI.test(value)) return maskUpi(value);
  const digitsOnly = compact(value);
  if (RAW_PHONE.test(digitsOnly)) return maskPhone(digitsOnly);
  if (RAW_ACCOUNT.test(digitsOnly)) return maskAccount(digitsOnly);
  return value;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null) return false;
  const proto: unknown = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

function redactValue(value: unknown): unknown {
  if (typeof value === "string") return redactString(value);
  if (Array.isArray(value)) return value.map((item: unknown) => redactValue(item));
  if (isPlainObject(value)) {
    // fromEntries defines own properties, so a key named "__proto__" is copied
    // as data and never changes the prototype of the result.
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, SECRET_KEY.test(key) ? REDACTED : redactValue(item)]),
    );
  }
  return value;
}

/**
 * A deep copy of `value` that is safe to send or show.
 *
 * - A property named `otp`, `pin`, `password`, `passcode` or `cvv` (any letter
 *   case, any depth) holds `[REDACTED]`, whatever it held before.
 * - A string that is a whole unmasked UPI id, phone number or account number is
 *   replaced by its masked form. A string already in a masked format is kept, so
 *   applying the function twice gives the same result as applying it once.
 * - Numbers, booleans, `null` and `undefined` pass through.
 *
 * It copies arrays and plain objects, and returns any other value as it is.
 * The input is never modified. The input must not contain a reference cycle.
 */
export function redactObject<T>(value: T): T {
  return redactValue(value) as T;
}
