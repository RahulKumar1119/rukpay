import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { maskedValue, rawAccount, rawUpi } from "../test/sensitiveArbitraries";
import { ALL_STATES, installFetchStub, makeView } from "../test/stubs";
import { ApiError, evaluate, getVault, guardianAction, NetworkError, type VaultView } from "./api";

const text = fc.oneof(fc.string({ unit: "grapheme-ascii" }), fc.string({ unit: "grapheme" }));

const errorDetail = fc.record({
  code: text,
  message: text,
  field: fc.option(text, { nil: undefined }),
});

const view: fc.Arbitrary<VaultView> = fc
  .record({
    vault_id: fc.stringMatching(/^[0-9a-f]{32}$/),
    state: fc.constantFrom(...ALL_STATES),
    remaining_seconds: fc.integer({ min: 0, max: 900 }),
    reason: text,
  })
  .map(({ state, ...rest }) => makeView(state, rest));

type Detail = { code: string; message: string; field: string | undefined };

/** The body the server sends: `field` is left out when it is undefined. */
function bodyFor(detail: Detail, vault?: VaultView) {
  const error = detail.field === undefined ? { code: detail.code, message: detail.message } : { ...detail };
  return vault ? { error, vault } : { error };
}

// Identifiers long enough that they cannot appear in a short message by chance.
const longToken = fc.stringMatching(/^[0-9a-f]{16,32}\.[0-9]{10}\.[0-9a-f]{16,64}$/);
const longIdentifier = fc.oneof(rawAccount, rawUpi, maskedValue).filter((s) => s.length >= 8);

describe("api client properties", () => {
  // Feature: dual-screen-simulator, Property 14: Errors carry the server fields and no secrets
  // Validates: Requirements 10.5, 10.9
  it("Property 14: an ApiError has the status, code, message and field the server sent", async () => {
    await fc.assert(
      fc.asyncProperty(fc.integer({ min: 400, max: 499 }), errorDetail, view, async (status, detail, current) => {
        installFetchStub().respond(status, bodyFor(detail, status === 409 ? current : undefined));

        const err = await getVault("v1").then(
          () => null,
          (e: unknown) => e,
        );

        expect(err).toBeInstanceOf(ApiError);
        const apiError = err as ApiError;
        expect(apiError.status).toBe(status);
        expect(apiError.code).toBe(detail.code);
        expect(apiError.message).toBe(detail.message);
        expect(apiError.field).toBe(detail.field);
        if (status === 409) expect(apiError.vault).toEqual(current);
        else expect(apiError.vault).toBeUndefined();
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 14: a 5xx answer is a NetworkError, whatever the body says", async () => {
    await fc.assert(
      fc.asyncProperty(fc.integer({ min: 500, max: 599 }), errorDetail, async (status, detail) => {
        installFetchStub().respond(status, bodyFor(detail));

        const err = await getVault("v1").then(
          () => null,
          (e: unknown) => e,
        );

        expect(err).toBeInstanceOf(NetworkError);
        expect(err).not.toBeInstanceOf(ApiError);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 14: no error message holds the guardian token or an identifier from the request", async () => {
    await fc.assert(
      fc.asyncProperty(
        longToken,
        longIdentifier,
        errorDetail,
        fc.constantFrom<"4xx" | "5xx" | "unreadable" | "unreachable">("4xx", "5xx", "unreadable", "unreachable"),
        async (token, identifier, detail, kind) => {
          // The server's own words are the one thing an error may repeat, so
          // the generated message must not hold the secret by chance.
          fc.pre(!detail.message.includes(token) && !detail.message.includes(identifier));
          fc.pre(!detail.code.includes(token) && !detail.code.includes(identifier));

          const stub = installFetchStub();
          if (kind === "4xx") stub.respond(403, bodyFor(detail));
          else if (kind === "5xx") stub.respond(503, bodyFor(detail));
          else if (kind === "unreadable") stub.respondRaw(200, `not json ${token} ${identifier}`);
          else stub.reject(new TypeError("Failed to fetch"));

          const err = await guardianAction(`v-${identifier.length}`, { action: "APPROVE", token, confirmed: true }).then(
            () => null,
            (e: unknown) => e,
          );

          expect(err).toBeInstanceOf(Error);
          const message = (err as Error).message;
          expect(message).not.toContain(token);
          expect(message).not.toContain(identifier);
        },
      ),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 14: an evaluate error never repeats the masked identifiers it sent", async () => {
    await fc.assert(
      fc.asyncProperty(longIdentifier, errorDetail, async (identifier, detail) => {
        fc.pre(!detail.message.includes(identifier));
        installFetchStub().respond(400, bodyFor(detail));

        const err = await evaluate({
          amount_inr: 450,
          beneficiary_age_hours: 720,
          is_late_night: false,
          active_unknown_call: false,
          rapid_retry_count: 0,
          upi_masked: identifier,
        }).then(
          () => null,
          (e: unknown) => e,
        );

        expect((err as Error).message).not.toContain(identifier);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
