import fc from "fast-check";
import { describe, it } from "vitest";

import { PBT_RUNS, textWithDigits } from "../test/pbt";
import { maskAccount, maskPhone } from "./privacyMasker";

// Any text: printable ASCII, every UTF-16 code unit, and strings built to hold
// long runs of digits.
const anyInput = fc.oneof(
  fc.string({ unit: "grapheme-ascii" }),
  fc.string({ unit: "binary" }),
  textWithDigits(0, 40),
  fc.stringMatching(/^[0-9]{0,40}$/),
);

describe("privacyMasker properties", () => {
  // Feature: dual-screen-simulator, Property 2: No long digit run survives masking
  // Validates: Requirements 11.10, 15.2
  it("Property 2: maskAccount output holds no run of more than four digits", () => {
    fc.assert(
      fc.property(anyInput, (input) => !/\d{5,}/.test(maskAccount(input))),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 2: maskPhone output holds no run of more than four digits", () => {
    fc.assert(
      fc.property(anyInput, (input) => !/\d{5,}/.test(maskPhone(input))),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 2: the only digits in the output are the last four of the input", () => {
    fc.assert(
      fc.property(textWithDigits(5, 30), (input) => {
        const last4 = input.replace(/\D/g, "").slice(-4);
        // The phone format starts with the fixed country code "+91-".
        const phoneDigits = maskPhone(input).replace(/^\+91-/, "").replace(/\D/g, "");
        return maskAccount(input).replace(/\D/g, "") === last4 && phoneDigits === last4;
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
