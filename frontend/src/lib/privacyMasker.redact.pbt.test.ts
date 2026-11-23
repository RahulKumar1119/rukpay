import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { maskAccount, maskPhone, maskUpi, redactObject } from "./privacyMasker";

const REDACTED = "[REDACTED]";

// The test keeps its own copy of the patterns, so a mistake in the module's
// patterns cannot hide itself.
const SECRET_KEY = /^(otp|pin|password|passcode|cvv)$/i;
const MASKED = [/^XXXX-XXXX-\d{4}$/, /^\+91-X{6}\d{4}$/, /^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]+$/];
const RAW_UPI = /^[^\s@]+@[A-Za-z0-9.-]+$/;
const RAW_PHONE = /^(\+91)?[6-9]\d{9}$/;
const RAW_ACCOUNT = /^\d{9,18}$/;

const compact = (s: string): string => s.replace(/[\s-]/g, "");
const isMasked = (s: string): boolean => MASKED.some((re) => re.test(s));
const looksUnmasked = (s: string): boolean =>
  !isMasked(s) && (RAW_UPI.test(s) || RAW_PHONE.test(compact(s)) || RAW_ACCOUNT.test(compact(s)));

// Generators for the values the redaction is meant to catch.
const rawAccount = fc.stringMatching(/^[0-9]{9,18}$/);
const rawPhone = fc
  .tuple(fc.constantFrom("", "+91", "+91 ", "+91-"), fc.stringMatching(/^[6-9][0-9]{9}$/))
  .map(([prefix, number]) => prefix + number);
const rawUpi = fc
  .tuple(fc.stringMatching(/^[A-Za-z0-9._-]{1,12}$/), fc.stringMatching(/^[A-Za-z0-9.-]{1,10}$/))
  .map(([name, handle]) => `${name}@${handle}`);
const maskedValue = fc.oneof(
  fc.stringMatching(/^[0-9]{4,12}$/).map(maskAccount),
  fc.stringMatching(/^[0-9]{4,12}$/).map(maskPhone),
  rawUpi.map(maskUpi),
);

const secretKey = fc.constantFrom("otp", "OTP", "pin", "Pin", "password", "PassWord", "passcode", "PASSCODE", "cvv", "Cvv");
const plainKey = fc.oneof(
  fc.string({ unit: "grapheme-ascii", maxLength: 8 }),
  fc.constantFrom("amount_inr", "token", "pinned", "otp_hint", "__proto__", "constructor", "toString"),
);

const leaf: fc.Arbitrary<unknown> = fc.oneof(
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
const entry = (value: fc.Arbitrary<unknown>): fc.Arbitrary<[string, unknown]> =>
  fc.tuple(fc.oneof({ weight: 3, arbitrary: plainKey }, { weight: 1, arbitrary: secretKey }), value);

const tree = fc.letrec<{ value: unknown; array: unknown[]; object: Record<string, unknown> }>((tie) => ({
  value: fc.oneof({ depthSize: "small", maxDepth: 4 }, leaf, tie("array"), tie("object")),
  array: fc.array(tie("value"), { maxLength: 5 }),
  object: fc.array(entry(tie("value")), { maxLength: 5 }).map((entries) => Object.fromEntries(entries)),
})).value;

// ---- helpers that read a value without using the module under test ----

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

/** Every object and array in a tree, so two trees can be checked for shared references. */
function containers(v: unknown, out: Set<object> = new Set()): Set<object> {
  if (Array.isArray(v)) {
    out.add(v);
    v.forEach((item) => containers(item, out));
  } else if (isObject(v)) {
    out.add(v);
    Object.values(v).forEach((item) => containers(item, out));
  }
  return out;
}

/**
 * Compares the input and the output side by side and returns the first problem,
 * or null. It checks that the shape is kept, that only strings and secret keys
 * change, and that each change is a valid redaction.
 */
function firstProblem(input: unknown, output: unknown, path = "$"): string | null {
  if (typeof input === "string") {
    if (typeof output !== "string") return `${path}: a string became ${typeof output}`;
    if (output === input) return looksUnmasked(input) ? `${path}: an unmasked value was kept` : null;
    if (output === REDACTED || isMasked(output)) return looksUnmasked(input) ? null : `${path}: plain text was changed`;
    return `${path}: changed to something that is not a masked form`;
  }
  if (Array.isArray(input)) {
    if (!Array.isArray(output) || output.length !== input.length) return `${path}: array shape changed`;
    for (let i = 0; i < input.length; i++) {
      const problem = firstProblem(input[i], output[i], `${path}[${i}]`);
      if (problem) return problem;
    }
    return null;
  }
  if (isObject(input)) {
    if (!isObject(output)) return `${path}: an object became ${typeof output}`;
    const keys = Object.keys(input);
    if (JSON.stringify(Object.keys(output)) !== JSON.stringify(keys)) return `${path}: keys changed`;
    for (const key of keys) {
      const child = `${path}.${key}`;
      const outValue = Object.getOwnPropertyDescriptor(output, key)?.value as unknown;
      if (SECRET_KEY.test(key)) {
        if (outValue !== REDACTED) return `${child}: a secret key holds ${JSON.stringify(outValue)}`;
        continue;
      }
      const inValue = Object.getOwnPropertyDescriptor(input, key)?.value as unknown;
      const problem = firstProblem(inValue, outValue, child);
      if (problem) return problem;
    }
    return null;
  }
  // Numbers, booleans and null must come back the same.
  return Object.is(input, output) ? null : `${path}: ${String(input)} became ${String(output)}`;
}

describe("privacyMasker properties", () => {
  // Feature: dual-screen-simulator, Property 5: Object redaction is deep, total and idempotent
  // Validates: Requirements 11.9, 15.2
  it("Property 5: secret keys are redacted, unmasked values are masked, and the rest is kept", () => {
    fc.assert(
      fc.property(tree, (input) => {
        const problem = firstProblem(input, redactObject(input));
        expect(problem).toBeNull();
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 5: the result is a deep copy and the input is not modified", () => {
    fc.assert(
      fc.property(tree, (input) => {
        const snapshot = structuredClone(input);
        const output = redactObject(input);

        expect(input).toEqual(snapshot);
        const inputContainers = containers(input);
        for (const container of containers(output)) {
          expect(inputContainers.has(container)).toBe(false);
        }
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 5: applying it twice gives the same result as applying it once", () => {
    fc.assert(
      fc.property(tree, (input) => {
        const once = redactObject(input);
        expect(redactObject(once)).toEqual(once);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 5: a secret key holds [REDACTED] whatever it held, at any depth", () => {
    fc.assert(
      fc.property(secretKey, tree, fc.array(fc.constantFrom("a", "b", "c"), { maxLength: 4 }), (key, held, path) => {
        // Wrap `{ [key]: held }` in a chain of objects and arrays.
        let value: unknown = { [key]: held };
        for (const step of path) value = step === "a" ? [value] : { [step]: value };

        let cursor: unknown = redactObject(value);
        for (const step of [...path].reverse()) {
          cursor = step === "a" ? (cursor as unknown[])[0] : (cursor as Record<string, unknown>)[step];
        }
        expect((cursor as Record<string, unknown>)[key]).toBe(REDACTED);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
