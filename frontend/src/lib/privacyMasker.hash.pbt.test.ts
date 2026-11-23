import fc from "fast-check";
import { describe, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { hashIdentifier } from "./privacyMasker";

// "grapheme" gives valid Unicode text only. A lone surrogate is encoded as the
// replacement character, so two different lone surrogates would hash the same.
// That is a property of text encoding and not of the masker.
const text = fc.oneof(fc.string({ unit: "grapheme-ascii" }), fc.string({ unit: "grapheme" }));

const normalize = (s: string): string => s.trim().toLowerCase();

// Surrounding whitespace, and the letter case of every ASCII letter changed.
const asciiText = fc.string({ unit: "grapheme-ascii" });
const whitespace = fc.stringMatching(/^[ \t\r\n]{0,5}$/);

describe("privacyMasker properties", () => {
  // Feature: dual-screen-simulator, Property 4: Hashing is deterministic and discriminating
  // Validates: Requirements 11.7, 11.8, 15.2
  it("Property 4: the digest is 64 lowercase hex characters and the same every time", async () => {
    await fc.assert(
      fc.asyncProperty(text, async (value) => {
        const first = await hashIdentifier(value);
        const second = await hashIdentifier(value);
        return /^[0-9a-f]{64}$/.test(first) && first === second;
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 4: surrounding whitespace and letter case do not change the digest", async () => {
    await fc.assert(
      fc.asyncProperty(asciiText, whitespace, whitespace, fc.boolean(), async (value, before, after, upper) => {
        const variant = before + (upper ? value.toUpperCase() : value.toLowerCase()) + after;
        return (await hashIdentifier(variant)) === (await hashIdentifier(value));
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 4: values that differ after trimming and lowercasing give different digests", async () => {
    await fc.assert(
      fc.asyncProperty(text, text, async (a, b) => {
        fc.pre(normalize(a) !== normalize(b));
        return (await hashIdentifier(a)) !== (await hashIdentifier(b));
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
