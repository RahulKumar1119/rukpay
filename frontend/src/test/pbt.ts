/**
 * Shared settings and generators for the fast-check property tests. Test only.
 */
import fc from "fast-check";

/** Runs per property. The spec asks for at least 100. */
export const PBT_RUNS = 1000;

export const digitChar: fc.Arbitrary<string> = fc.integer({ min: 0, max: 9 }).map(String);

/** Any text with every ASCII digit removed. */
export const digitFreeText: fc.Arbitrary<string> = fc
  .string({ unit: "grapheme-ascii" })
  .map((s) => s.replace(/[0-9]/g, ""));

/**
 * A string with between `min` and `max` ASCII digits, spread through digit-free
 * text. Digits often sit next to each other, so long digit runs are common.
 */
export function textWithDigits(min: number, max: number): fc.Arbitrary<string> {
  return fc
    .tuple(fc.array(fc.tuple(digitFreeText, digitChar), { minLength: min, maxLength: max }), digitFreeText)
    .map(([pieces, tail]) => pieces.map(([text, digit]) => text + digit).join("") + tail);
}

/** Number of ASCII digits in `s`. */
export function countDigits(s: string): number {
  return s.replace(/[^0-9]/g, "").length;
}
