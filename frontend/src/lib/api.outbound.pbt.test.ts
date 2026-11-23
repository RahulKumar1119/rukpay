import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { looksUnmasked, REDACTED, SECRET_KEY } from "../test/redactionOracle";
import { entry, maskedValue, rawAccount, rawPhone, rawUpi, tree } from "../test/sensitiveArbitraries";
import { installFetchStub, makeView } from "../test/stubs";
import {
  evaluate,
  guardianAction,
  submitAnswers,
  type Answer,
  type Answers,
  type EvaluateRequest,
  type GuardianActionRequest,
} from "./api";

// Extra properties a caller might add by mistake: secret keys in any letter
// case, raw identifiers, and nested data.
const extras: fc.Arbitrary<Record<string, unknown>> = fc
  .array(entry(tree), { maxLength: 4 })
  .map((entries) => Object.fromEntries(entries));

const anyIdentifier = fc.oneof(rawAccount, rawPhone, rawUpi, maskedValue, fc.string({ unit: "grapheme-ascii" }));

const identifiers = fc.record(
  {
    payer_account_masked: anyIdentifier,
    beneficiary_masked: anyIdentifier,
    upi_masked: anyIdentifier,
    phone_masked: anyIdentifier,
  },
  { requiredKeys: [] },
);

const signals = fc.record({
  amount_inr: fc.integer({ min: 1, max: 10_000_000 }),
  beneficiary_age_hours: fc.integer({ min: 0, max: 100_000 }),
  is_late_night: fc.boolean(),
  active_unknown_call: fc.boolean(),
  rapid_retry_count: fc.integer({ min: 0, max: 20 }),
});

const answer: fc.Arbitrary<Answer> = fc.constantFrom("YES", "NO");

// A token in the shape the backend issues: id.expiry.tag.
const realToken = fc
  .tuple(fc.stringMatching(/^[0-9a-f]{32}$/), fc.stringMatching(/^[0-9]{10}$/), fc.stringMatching(/^[0-9a-f]{64}$/))
  .map(([id, expiry, tag]) => `${id}.${expiry}.${tag}`);

/** Throws when `value` holds a secret key with a real value or an unmasked identifier. */
function assertSafe(value: unknown, path = "body"): void {
  if (typeof value === "string") {
    expect(looksUnmasked(value), `${path} holds an unmasked value`).toBe(false);
  } else if (Array.isArray(value)) {
    value.forEach((item, i) => assertSafe(item, `${path}[${i}]`));
  } else if (typeof value === "object" && value !== null) {
    for (const [key, item] of Object.entries(value)) {
      if (SECRET_KEY.test(key)) expect(item, `${path}.${key}`).toBe(REDACTED);
      else assertSafe(item, `${path}.${key}`);
    }
  }
}

describe("api client properties", () => {
  // Feature: dual-screen-simulator, Property 12: Outbound bodies carry no sensitive values
  // Validates: Requirements 10.8, 12.4, 12.5
  it("Property 12: an evaluate body carries no secret value and no unmasked identifier", async () => {
    await fc.assert(
      fc.asyncProperty(signals, identifiers, extras, async (sig, ids, extra) => {
        const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
        const request = { ...extra, ...sig, ...ids } as EvaluateRequest;

        await evaluate(request);

        const body = stub.calls[0]?.body;
        assertSafe(body);
        expect(body).toMatchObject(sig);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 12: a reality-check body carries no secret value and no unmasked identifier", async () => {
    await fc.assert(
      fc.asyncProperty(answer, answer, answer, extras, async (coercion, secrecy, verification, extra) => {
        const stub = installFetchStub().respond(200, makeView("LOCKED_15M"));
        const answers = { ...extra, coercion, secrecy, verification } as Answers;

        await submitAnswers("v1", answers);

        const body = stub.calls[0]?.body as { answers: unknown };
        assertSafe(body);
        expect(body.answers).toMatchObject({ coercion, secrecy, verification });
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 12: a guardian action body carries no secret value and no unmasked identifier", async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.constantFrom("APPROVE", "VETO"),
        fc.oneof(realToken, fc.string({ unit: "grapheme-ascii" })),
        fc.boolean(),
        extras,
        async (action, token, confirmed, extra) => {
          const stub = installFetchStub().respond(200, { status: "OK", vault: makeView("GUARDIAN_APPROVED") });
          const request = { ...extra, action, token, confirmed } as GuardianActionRequest;

          await guardianAction("v1", request);

          const body = stub.calls[0]?.body;
          assertSafe(body);
          expect(body).toMatchObject({ action, confirmed });
        },
      ),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 12: a real guardian token is sent unchanged", async () => {
    await fc.assert(
      fc.asyncProperty(realToken, async (token) => {
        const stub = installFetchStub().respond(200, { status: "OK", vault: makeView("GUARDIAN_APPROVED") });

        await guardianAction("v1", { action: "APPROVE", token, confirmed: true });

        expect(stub.calls[0]?.body).toEqual({ action: "APPROVE", token, confirmed: true });
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
