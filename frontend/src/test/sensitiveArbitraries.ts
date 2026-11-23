/**
 * Generators for values that redaction is meant to catch, and for trees that
 * mix them with ordinary data. They build input only. What the output must look
 * like is described separately in `redactionOracle.ts`. Test only.
 */
import fc from "fast-check";

import { maskAccount, maskPhone, maskUpi } from "../lib/privacyMasker";
import { REDACTED } from "./redactionOracle";

export const rawAccount = fc.stringMatching(/^[0-9]{9,18}$/);
export const rawPhone = fc
  .tuple(fc.constantFrom("", "+91", "+91 ", "+91-"), fc.stringMatching(/^[6-9][0-9]{9}$/))
  .map(([prefix, number]) => prefix + number);
export const rawUpi = fc
  .tuple(fc.stringMatching(/^[A-Za-z0-9._-]{1,12}$/), fc.stringMatching(/^[A-Za-z0-9.-]{1,10}$/))
  .map(([name, handle]) => `${name}@${handle}`);
export const maskedValue = fc.oneof(
  fc.stringMatching(/^[0-9]{4,12}$/).map(maskAccount),
  fc.stringMatching(/^[0-9]{4,12}$/).map(maskPhone),
  rawUpi.map(maskUpi),
);

export const secretKey = fc.constantFrom("otp", "OTP", "pin", "Pin", "password", "PassWord", "passcode", "PASSCODE", "cvv", "Cvv");
export const plainKey = fc.oneof(
  fc.string({ unit: "grapheme-ascii", maxLength: 8 }),
  fc.constantFrom("amount_inr", "token", "pinned", "otp_hint", "__proto__", "constructor", "toString"),
);

export const leaf: fc.Arbitrary<unknown> = fc.oneof(
  fc.integer(),
  fc.double({ noNaN: true }),
  fc.boolean(),
  fc.constant(null),
  fc.string({ unit: "grapheme-ascii" }),
  rawAccount,
  rawPhone,
  rawUpi,
  maskedValue,
  fc.constant(REDACTED),
);

// Objects are built with fromEntries, which creates own properties even for a
// key named "__proto__".
export const entry = (value: fc.Arbitrary<unknown>): fc.Arbitrary<[string, unknown]> =>
  fc.tuple(fc.oneof({ weight: 3, arbitrary: plainKey }, { weight: 1, arbitrary: secretKey }), value);

export const tree = fc.letrec<{ value: unknown; array: unknown[]; object: Record<string, unknown> }>((tie) => ({
  value: fc.oneof({ depthSize: "small", maxDepth: 4 }, leaf, tie("array"), tie("object")),
  array: fc.array(tie("value"), { maxLength: 5 }),
  object: fc.array(entry(tie("value")), { maxLength: 5 }).map((entries) => Object.fromEntries(entries)),
})).value;
