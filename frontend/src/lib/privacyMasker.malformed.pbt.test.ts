import fc from "fast-check";
import { describe, it } from "vitest";

import { PBT_RUNS, textWithDigits } from "../test/pbt";
import { maskAccount, maskPhone, maskUpi, redactSecret } from "./privacyMasker";

const REDACTED = "[REDACTED]";

// Text that holds at most three ASCII digits.
const fewerThanFourDigits = fc.oneof(
  textWithDigits(0, 3),
  fc.string({ unit: "binary" }).map((s) => s.replace(/[0-9]/g, "")),
);

const anyText = fc.oneof(fc.string({ unit: "grapheme-ascii" }), fc.string({ unit: "binary" }));

// Text with no "@".
const noAt = anyText.map((s) => s.replace(/@/g, ""));

describe("privacyMasker properties", () => {
  // Feature: dual-screen-simulator, Property 3: Malformed identifiers are redacted
  // Validates: Requirements 11.4, 11.5
  it("Property 3: maskAccount and maskPhone return [REDACTED] below four digits", () => {
    fc.assert(
      fc.property(fewerThanFourDigits, (input) => maskAccount(input) === REDACTED && maskPhone(input) === REDACTED),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 3: maskUpi returns [REDACTED] when there is no @", () => {
    fc.assert(
      fc.property(noAt, (input) => maskUpi(input) === REDACTED),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 3: maskUpi returns [REDACTED] for an empty name", () => {
    fc.assert(
      fc.property(noAt, (handle) => maskUpi(`@${handle}`) === REDACTED),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 3: maskUpi returns [REDACTED] for an empty handle", () => {
    fc.assert(
      fc.property(noAt, (name) => maskUpi(`${name}@`) === REDACTED),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 3: maskUpi returns [REDACTED] for a name that does not start with a letter or digit", () => {
    const badStart = fc.constantFrom("_", "-", ".", " ", "*", "+", "é", "#");
    fc.assert(
      fc.property(badStart, noAt, fc.stringMatching(/^[A-Za-z0-9.-]{1,10}$/), (first, rest, handle) => {
        return maskUpi(`${first}${rest}@${handle}`) === REDACTED;
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 3: redactSecret returns [REDACTED] for any string", () => {
    fc.assert(
      fc.property(anyText, (input) => redactSecret(input) === REDACTED),
      { numRuns: PBT_RUNS },
    );
  });
});
