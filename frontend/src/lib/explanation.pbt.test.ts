import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { PBT_RUNS } from "../test/pbt";
import type { ExplanationItem } from "./api";
import { pointsText, totalText } from "./copy";
import { GUARDIAN_SUMMARY_LIMIT, guardianTop, readExplanation } from "./explanation";
// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------
/** An item the card can show. Extra keys are allowed, because a server may add them. */
const usableItem: fc.Arbitrary<ExplanationItem> = fc
  .record({
    signal: fc.string(),
    points: fc.oneof(fc.integer({ min: -60, max: 60 }), fc.integer()),
    reason: fc.string({ minLength: 1 }),
  })
  .chain((base) => fc.oneof(fc.constant(base), fc.string().map((extra) => ({ ...base, extra }) as ExplanationItem)));
/** An item that is not usable, for each way the rule can fail. */
const badItem: fc.Arbitrary<unknown> = fc.oneof(
  fc.record({ points: fc.integer(), reason: fc.string({ minLength: 1 }) }), // no signal
  fc.record({ signal: fc.integer(), points: fc.integer(), reason: fc.string({ minLength: 1 }) }), // signal is not a string
  fc.record({ signal: fc.string(), reason: fc.string({ minLength: 1 }) }), // no points
  fc.record({ signal: fc.string(), points: fc.string(), reason: fc.string({ minLength: 1 }) }), // points is a string
  fc.record({ signal: fc.string(), points: fc.double({ noNaN: true }).filter((n) => !Number.isInteger(n)), reason: fc.string({ minLength: 1 }) }), // not whole
  fc.record({ signal: fc.string(), points: fc.constantFrom(Number.NaN, Infinity, -Infinity), reason: fc.string({ minLength: 1 }) }),
  fc.record({ signal: fc.string(), points: fc.integer(), reason: fc.constant("") }), // empty reason
  fc.record({ signal: fc.string(), points: fc.integer(), reason: fc.integer() }), // reason is not a string
  fc.record({ signal: fc.string(), points: fc.integer() }), // no reason
  fc.constantFrom(null, undefined, "text", 7, true, [], [1, 2]),
);
/** Independent statement of Requirement 4 criterion 3, written from the requirement and not from the code. */
function expectedKind(value: unknown): "absent" | "empty" | "items" {
  if (!Array.isArray(value)) return "absent";
  for (const item of value) {
    if (typeof item !== "object" || item === null || Array.isArray(item)) return "absent";
    const fields = item as Record<string, unknown>;
    if (typeof fields.signal !== "string") return "absent";
    if (typeof fields.points !== "number" || !Number.isInteger(fields.points)) return "absent";
    if (typeof fields.reason !== "string" || fields.reason.length === 0) return "absent";
  }
  return value.length === 0 ? "empty" : "items";
}
/** The guardian's items by repeated selection: the most points, the earliest first, three times at most. No sort. */
function expectedTop(items: ExplanationItem[]): ExplanationItem[] {
  const pool = items.map((item, index) => ({ item, index })).filter((entry) => entry.item.points > 0);
  const picked: ExplanationItem[] = [];
  while (picked.length < 3 && pool.length > 0) {
    let best = 0;
    for (let k = 1; k < pool.length; k++) {
      const a = pool[k] as { item: ExplanationItem; index: number };
      const b = pool[best] as { item: ExplanationItem; index: number };
      if (a.item.points > b.item.points || (a.item.points === b.item.points && a.index < b.index)) best = k;
    }
    picked.push((pool.splice(best, 1)[0] as { item: ExplanationItem }).item);
  }
  return picked;
}
/** Whole numbers that include the awkward ones: zero, minus zero, one, a 32-bit value, a safe integer and a number too big for plain digits. */
const wholeNumber: fc.Arbitrary<number> = fc.oneof(
  fc.integer({ min: -1000, max: 1000 }),
  fc.integer(),
  fc.maxSafeInteger(),
  fc.constantFrom(0, -0, 1, -1, 1e21, -1e21, Number.MAX_SAFE_INTEGER, Number.MIN_SAFE_INTEGER),
);
describe("explanation properties", () => {
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.3
  it("Property 32 (reading): any value of `explanation` is absent, empty or items exactly as the rule says", () => {
    fc.assert(
      fc.property(fc.anything(), (value) => {
        expect(readExplanation({ explanation: value }).kind).toBe(expectedKind(value));
      }),
      { numRuns: PBT_RUNS },
    );
    fc.assert(
      fc.property(fc.array(fc.oneof(usableItem, badItem), { maxLength: 6 }), (list) => {
        expect(readExplanation({ explanation: list }).kind).toBe(expectedKind(list));
      }),
      { numRuns: PBT_RUNS },
    );
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.3
  it("Property 32 (reading): a missing key, a missing view and one bad item among good ones are absent", () => {
    expect(readExplanation({})).toEqual({ kind: "absent" });
    expect(readExplanation(null)).toEqual({ kind: "absent" });
    expect(readExplanation(undefined)).toEqual({ kind: "absent" });
    expect(readExplanation({ explanation: [] })).toEqual({ kind: "empty" });
    fc.assert(
      fc.property(fc.array(usableItem, { maxLength: 5 }), badItem, fc.nat(), (good, bad, at) => {
        const list: unknown[] = [...good];
        list.splice(at % (good.length + 1), 0, bad);
        expect(readExplanation({ explanation: list })).toEqual({ kind: "absent" });
      }),
      { numRuns: PBT_RUNS },
    );
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.1, 4.5
  it("Property 32 (reading): usable items come back as they arrived, in order, with no points changed", () => {
    fc.assert(
      fc.property(fc.array(usableItem, { minLength: 1, maxLength: 8 }), (list) => {
        const before = JSON.stringify(list);
        const read = readExplanation({ explanation: list });
        expect(read.kind).toBe("items");
        if (read.kind !== "items") return;
        expect(read.items).toHaveLength(list.length);
        read.items.forEach((item, index) => expect(item).toBe(list[index]));
        expect(JSON.stringify(list)).toBe(before);
      }),
      { numRuns: PBT_RUNS },
    );
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.9
  it("Property 32 (points text): a sign, the whole number and 'point' for one or 'points' for any other", () => {
    fc.assert(
      fc.property(wholeNumber, (n) => {
        const text = pointsText(n);
        const match = /^([+-])(\d+) (points?)$/.exec(text);
        expect(match, text).not.toBeNull();
        const [, sign, digits, word] = match as RegExpExecArray;
        expect(sign).toBe(n < 0 ? "-" : "+");
        expect(BigInt(digits as string)).toBe(BigInt(Math.abs(n)));
        expect(word).toBe(Math.abs(n) === 1 ? "point" : "points");
      }),
      { numRuns: PBT_RUNS },
    );
    expect(pointsText(20)).toBe("+20 points");
    expect(pointsText(-5)).toBe("-5 points");
    expect(pointsText(1)).toBe("+1 point");
    expect(pointsText(-1)).toBe("-1 point");
    expect(pointsText(0)).toBe("+0 points");
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.1
  it("Property 32 (total text): the score as a whole number and 'point' or 'points'", () => {
    fc.assert(
      fc.property(wholeNumber, (n) => {
        const match = /^Total: (-?\d+) (points?)$/.exec(totalText(n));
        expect(match, totalText(n)).not.toBeNull();
        expect(BigInt((match as RegExpExecArray)[1] as string)).toBe(BigInt(n));
        expect((match as RegExpExecArray)[2]).toBe(Math.abs(n) === 1 ? "point" : "points");
      }),
      { numRuns: PBT_RUNS },
    );
    expect(totalText(90)).toBe("Total: 90 points");
    expect(totalText(1)).toBe("Total: 1 point");
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.7
  it("Property 32 (guardian top): at most 3 items with points above 0, the most points first, ties in the order received", () => {
    // Points from -3 to 3 so that equal points are common.
    const tied = fc.array(
      fc.record({ signal: fc.string(), points: fc.integer({ min: -3, max: 3 }), reason: fc.string({ minLength: 1 }) }),
      { maxLength: 10 },
    );
    const wide = fc.array(usableItem, { maxLength: 10 });
    fc.assert(
      fc.property(fc.oneof(tied, wide), (items) => {
        const before = JSON.stringify(items);
        const top = guardianTop(items);
        expect(top).toHaveLength(Math.min(GUARDIAN_SUMMARY_LIMIT, items.filter((item) => item.points > 0).length));
        expect(top.length).toBeLessThanOrEqual(3);
        for (const item of top) expect(item.points).toBeGreaterThan(0);
        for (let i = 1; i < top.length; i++) expect((top[i - 1] as ExplanationItem).points).toBeGreaterThanOrEqual((top[i] as ExplanationItem).points);
        const expected = expectedTop(items);
        expect(top).toHaveLength(expected.length);
        top.forEach((item, index) => expect(item).toBe(expected[index]));
        expect(JSON.stringify(items)).toBe(before);
      }),
      { numRuns: PBT_RUNS },
    );
  });
  it("Property 32 (guardian top): keeps the first three of four or more, and equal points in received order", () => {
    const item = (signal: string, points: number): ExplanationItem => ({ signal, points, reason: `Reason ${signal}.` });
    const list = [item("a", 10), item("b", 20), item("c", 10), item("d", 20), item("e", 5), item("cap", -5)];
    expect(guardianTop(list).map((i) => i.signal)).toEqual(["b", "d", "a"]);
    expect(guardianTop([item("only", 7)]).map((i) => i.signal)).toEqual(["only"]);
    expect(guardianTop([])).toEqual([]);
    expect(guardianTop([item("zero", 0), item("cap", -5)])).toEqual([]);
  });
});
