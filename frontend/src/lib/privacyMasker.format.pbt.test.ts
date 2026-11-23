import fc from "fast-check";
import { describe, it } from "vitest";

import { digitChar, PBT_RUNS, textWithDigits } from "../test/pbt";
import { maskAccount, maskPhone, maskUpi } from "./privacyMasker";

// The patterns the backend's ValidateMasked accepts.
const ACCOUNT_FORMAT = /^XXXX-XXXX-\d{4}$/;
const PHONE_FORMAT = /^\+91-X{6}\d{4}$/;
const UPI_FORMAT = /^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]+$/;

const lastFour = (s: string): string => s.replace(/\D/g, "").slice(-4);

// A string with at least four digits, among any other text.
const atLeastFourDigits = fc.oneof(
  textWithDigits(4, 24),
  fc.stringMatching(/^[0-9]{4,20}$/),
  fc.stringMatching(/^[0-9 -]{4,30}$/).filter((s) => lastFour(s).length === 4),
);

const alnum = fc.constantFrom(..."abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789");

// A UPI id: a name that starts with a letter or digit and holds no "@", then a handle.
const upiParts = fc.record({
  name: fc.tuple(alnum, fc.string({ unit: "grapheme-ascii" }).map((s) => s.replace(/@/g, ""))).map(([a, b]) => a + b),
  handle: fc.stringMatching(/^[A-Za-z0-9.-]{1,20}$/),
});

describe("privacyMasker properties", () => {
  // Feature: dual-screen-simulator, Property 1: Masked output matches the backend formats
  // Validates: Requirements 11.1, 11.2, 11.3, 11.6, 15.2
  it("Property 1: maskAccount and maskPhone match the backend formats and keep the last four digits", () => {
    fc.assert(
      fc.property(atLeastFourDigits, (input) => {
        const account = maskAccount(input);
        const phone = maskPhone(input);
        return (
          ACCOUNT_FORMAT.test(account) &&
          account.endsWith(lastFour(input)) &&
          PHONE_FORMAT.test(phone) &&
          phone.endsWith(lastFour(input))
        );
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 1: four digits in any other place give the same last four", () => {
    fc.assert(
      fc.property(fc.array(digitChar, { minLength: 4, maxLength: 20 }), (digits) => {
        const raw = digits.join("");
        const want = raw.slice(-4);
        return maskAccount(raw) === `XXXX-XXXX-${want}` && maskPhone(raw) === `+91-XXXXXX${want}`;
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 1: maskUpi matches the backend format, starts with the first name character and ends with the handle", () => {
    fc.assert(
      fc.property(upiParts, ({ name, handle }) => {
        const out = maskUpi(`${name}@${handle}`);
        return UPI_FORMAT.test(out) && out.startsWith(`${name[0]}***@`) && out.endsWith(`@${handle}`);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 1: maskUpi never returns a value that fails the backend format, except [REDACTED]", () => {
    fc.assert(
      fc.property(fc.oneof(fc.string({ unit: "grapheme-ascii" }), fc.string({ unit: "binary" })), (input) => {
        const out = maskUpi(input);
        return out === "[REDACTED]" || UPI_FORMAT.test(out);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
