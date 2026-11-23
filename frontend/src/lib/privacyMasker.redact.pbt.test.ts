import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { isMasked, looksUnmasked, REDACTED, SECRET_KEY } from "../test/redactionOracle";
import { secretKey, tree } from "../test/sensitiveArbitraries";
import { redactObject } from "./privacyMasker";

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
