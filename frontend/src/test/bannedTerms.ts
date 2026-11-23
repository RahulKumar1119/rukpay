/**
 * The jargon and alarming phrases that no screen may show, from the anti-panic
 * steering document and design Property 15. Compared case-insensitively. Test only.
 */
export const BANNED_TERMS: readonly string[] = [
  "FATAL ERROR",
  "SECURITY BREACH",
  "ILLEGAL TRANSACTION",
  "YOUR ACCOUNT WILL BE BLOCKED",
  "FRAUDSTER DETECTED",
  "heuristic mismatch",
  "anomaly score",
  "velocity threshold",
  "cryptographic hash",
  "HMAC",
  "SSL/TLS",
  "payload",
  "telemetry",
  "IP reputation",
  "rule engine",
];

/** The banned terms found in `text`, if any. */
export function bannedTermsIn(text: string): string[] {
  const lower = text.toLowerCase();
  return BANNED_TERMS.filter((term) => lower.includes(term.toLowerCase()));
}
