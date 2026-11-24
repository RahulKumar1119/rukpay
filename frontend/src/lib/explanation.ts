/**
 * Reading the `explanation` of a vault view, and choosing what the guardian
 * sees. Everything here is pure: the same input always gives the same output.
 *
 * The browser never adds up, rounds or changes the points the server sent. It
 * passes each item through as it arrived, and the total line on the card is the
 * `risk_score` of the same view.
 */
import type { ExplanationItem } from "./api";
// The text builders live in copy.ts, next to `questionProgress`, because
// `ALL_COPY` holds sample outputs of them and copy.ts cannot import this file
// without a cycle. They are re-exported here so callers have one place to look.
export { pointsText, totalText } from "./copy";
/** What a view's `explanation` says. Absent and empty are different answers. */
export type ExplanationRead =
  /** No key, not a list, or an item that is not usable. Show nothing and no error. */
  | { kind: "absent" }
  /** A list with no items: nothing about the payment added any points. */
  | { kind: "empty" }
  | { kind: "items"; items: ExplanationItem[] };
/** How many lines the guardian's summary holds at most. */
export const GUARDIAN_SUMMARY_LIMIT = 3;
function isItem(value: unknown): value is ExplanationItem {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const item = value as Record<string, unknown>;
  return (
    typeof item.signal === "string" &&
    typeof item.points === "number" &&
    Number.isInteger(item.points) &&
    typeof item.reason === "string" &&
    item.reason !== ""
  );
}
/**
 * `absent` unless `explanation` is a list whose every item has a string
 * `signal`, an integer `points` and a non-empty string `reason`. The items come
 * back exactly as they arrived.
 */
export function readExplanation(view: { readonly explanation?: unknown } | null | undefined): ExplanationRead {
  const value = view?.explanation;
  if (!Array.isArray(value)) return { kind: "absent" };
  if (!value.every(isItem)) return { kind: "absent" };
  return value.length === 0 ? { kind: "empty" } : { kind: "items", items: value };
}
/**
 * The items the guardian sees: those with points above 0, the most points
 * first, equal points in the order received, at most three. The input is not
 * changed and the items are the same objects.
 */
export function guardianTop(items: readonly ExplanationItem[]): ExplanationItem[] {
  return items
    .map((item, index) => ({ item, index }))
    .filter(({ item }) => item.points > 0)
    .sort((a, b) => (a.item.points < b.item.points ? 1 : a.item.points > b.item.points ? -1 : a.index - b.index))
    .slice(0, GUARDIAN_SUMMARY_LIMIT)
    .map(({ item }) => item);
}
