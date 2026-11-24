/**
 * The Avoid_Words of the risk-intelligence spec: the banned terms of the
 * dual-screen simulator plus the words below. A string must hold none of them
 * anywhere, inside a longer word too, compared without regard to letter case, so
 * "alerting" and "fraudster" are found as well. Test only.
 */
import { BANNED_TERMS } from "./bannedTerms";
/** The words the risk-intelligence spec adds to the shipped banned terms. */
export const EXTRA_AVOID_WORDS: readonly string[] = [
  "velocity",
  "anomaly",
  "heuristic",
  "deviation",
  "suspicious",
  "fraud",
  "fraudulent",
  "criminal",
  "dangerous",
  "illegal",
  "scammer",
  "huge",
  "massive",
  "enormous",
  "urgent",
  "warning",
  "alert",
  "threat",
];
export const AVOID_WORDS: readonly string[] = [...BANNED_TERMS, ...EXTRA_AVOID_WORDS];
/** The Avoid_Words found in `text`, in list order, if any. */
export function avoidWordsIn(text: string): string[] {
  const lower = text.toLowerCase();
  return AVOID_WORDS.filter((word) => lower.includes(word.toLowerCase()));
}
