import fc from "fast-check";
import { describe, expect, it, vi } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { isMasked, looksUnmasked, REDACTED, SECRET_KEY } from "../test/redactionOracle";
import { entry, maskedValue, rawAccount, rawPhone, rawUpi, tree } from "../test/sensitiveArbitraries";
import { installFetchStub, makeView } from "../test/stubs";
import {
  addBeneficiary,
  ApiError,
  API_BASE,
  deleteBeneficiary,
  evaluate,
  getBeneficiary,
  isAbortError,
  listBeneficiaries,
  NetworkError,
  patchBeneficiary,
  verifyBeneficiary,
  type Answer,
  type BeneficiaryView,
  type EvaluateRequest,
  type HistoryKind,
  type PatchBeneficiaryRequest,
  type ProtectionLevel,
  type RiskIndicator,
  type TrustLevel,
  type VerificationAnswers,
} from "./api";

// ---------------------------------------------------------------------------
// The calls under test, and the words the server may use. Both are written out
// here, so a change in the client cannot change what the properties expect.
// ---------------------------------------------------------------------------

const ID = "3b0e5c1a9d2f4b6a8c7e1d0f2a3b4c5d";

const CALLS: { name: string; run: (signal?: AbortSignal) => Promise<unknown> }[] = [
  { name: "listBeneficiaries", run: (s) => listBeneficiaries(s) },
  { name: "addBeneficiary", run: (s) => addBeneficiary({ label: "Cousin", identifier_masked: "r***@okaxis" }, s) },
  { name: "getBeneficiary", run: (s) => getBeneficiary(ID, s) },
  { name: "patchBeneficiary", run: (s) => patchBeneficiary(ID, { label: "Uncle" }, s) },
  { name: "deleteBeneficiary", run: (s) => deleteBeneficiary(ID, s) },
  {
    name: "verifyBeneficiary",
    run: (s) => verifyBeneficiary(ID, { coercion: "NO", acquaintance: "YES", secrecy: "NO" }, s),
  },
];
const anyCall = fc.constantFrom(...CALLS);

const INDICATORS: RiskIndicator[] = [
  "ADDED_RECENTLY",
  "ADDED_UNDER_PRESSURE",
  "NOT_VERIFIED",
  "WAITING_PERIOD",
  "FIRST_PAYMENT_LARGE",
  "EXTRA_CARE_ON",
];
const KINDS: HistoryKind[] = [
  "ADDED",
  "VERIFIED",
  "VERIFICATION_NOT_PASSED",
  "TRUSTED",
  "TRUST_REMOVED",
  "PROTECTION_RAISED",
  "PROTECTION_LOWERED",
  "RENAMED",
  "PAYMENT",
];

const outcomeOf = (promise: Promise<unknown>): Promise<unknown> =>
  promise.then(
    () => null,
    (err: unknown) => err,
  );

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

const text = fc.oneof(fc.string({ unit: "grapheme-ascii" }), fc.string({ unit: "grapheme" }));
const hex32 = fc.stringMatching(/^[0-9a-f]{32}$/);
const epoch = fc.integer({ min: 0, max: 4_000_000_000 });

const historyEntry = fc.record(
  { at: epoch, kind: fc.constantFrom(...KINDS), amount_inr: fc.integer({ min: 1, max: 10_000_000 }) },
  { requiredKeys: ["at", "kind"] },
);

/** A complete View, with `verified_at` and `history` present or absent. */
const view: fc.Arbitrary<BeneficiaryView> = fc.record(
  {
    beneficiary_id: hex32,
    label: text,
    identifier_masked: maskedValue,
    added_at: epoch,
    age_hours: fc.integer({ min: 0, max: 100_000 }),
    trust_level: fc.constantFrom<TrustLevel>("NEW", "TRUSTED"),
    protection_level: fc.constantFrom<ProtectionLevel>("EXTRA_CARE", "STANDARD"),
    pressure_flag: fc.boolean(),
    trust_eligible_at: epoch,
    indicators: fc.subarray(INDICATORS),
    verified_at: epoch,
    history: fc.array(historyEntry, { maxLength: 6 }),
  },
  {
    requiredKeys: [
      "beneficiary_id",
      "label",
      "identifier_masked",
      "added_at",
      "age_hours",
      "trust_level",
      "protection_level",
      "pressure_flag",
      "trust_eligible_at",
      "indicators",
    ],
  },
) as fc.Arbitrary<BeneficiaryView>;

const REQUIRED_KEYS = [
  "beneficiary_id",
  "label",
  "identifier_masked",
  "added_at",
  "age_hours",
  "trust_level",
  "protection_level",
  "pressure_flag",
  "trust_eligible_at",
  "indicators",
];

type Plain = Record<string, unknown>;
const without = (v: Plain, key: string): Plain => Object.fromEntries(Object.entries(v).filter(([k]) => k !== key));

/** Each of these turns a complete View into one that must not be read as a View. */
const corruptions: ((v: Plain) => Plain)[] = [
  ...REQUIRED_KEYS.map((key) => (v: Plain): Plain => without(v, key)),
  (v) => ({ ...v, beneficiary_id: "" }),
  (v) => ({ ...v, beneficiary_id: 7 }),
  (v) => ({ ...v, label: null }),
  (v) => ({ ...v, identifier_masked: 12 }),
  (v) => ({ ...v, added_at: "1790000000" }),
  (v) => ({ ...v, added_at: 1.5 }),
  (v) => ({ ...v, age_hours: "2" }),
  (v) => ({ ...v, trust_eligible_at: null }),
  (v) => ({ ...v, trust_level: "trusted" }),
  (v) => ({ ...v, trust_level: "BLOCKED" }),
  (v) => ({ ...v, protection_level: "extra_care" }),
  (v) => ({ ...v, protection_level: "NONE" }),
  (v) => ({ ...v, pressure_flag: "false" }),
  (v) => ({ ...v, pressure_flag: 0 }),
  (v) => ({ ...v, indicators: "ADDED_RECENTLY" }),
  (v) => ({ ...v, indicators: null }),
  (v) => ({ ...v, indicators: [...(v.indicators as string[]), "SOMETHING_NEW"] }),
  (v) => ({ ...v, indicators: [7] }),
  (v) => ({ ...v, verified_at: null }),
  (v) => ({ ...v, verified_at: "1790003600" }),
  (v) => ({ ...v, history: {} }),
  (v) => ({ ...v, history: [null] }),
  (v) => ({ ...v, history: [{ at: 1, kind: "BOUGHT" }] }),
  (v) => ({ ...v, history: [{ at: "1", kind: "ADDED" }] }),
  (v) => ({ ...v, history: [{ at: 1, kind: "PAYMENT", amount_inr: "10" }] }),
];
const corrupted: fc.Arbitrary<Plain> = fc
  .tuple(view, fc.constantFrom(...corruptions))
  .map(([v, corrupt]) => corrupt(v as unknown as Plain));

const errorDetail = fc.record({ code: text, message: text, field: fc.option(text, { nil: undefined }) });
type Detail = { code: string; message: string; field: string | undefined };

/** The body the server sends: `field` is left out when it is undefined. */
function errorBodyFor(detail: Detail, extra: Plain = {}): Plain {
  const error = detail.field === undefined ? { code: detail.code, message: detail.message } : { ...detail };
  return { error, ...extra };
}

/** Text that is not a JSON document, and answers that are JSON but are not an error body. */
const nonJson = fc.oneof(
  fc.string({ unit: "grapheme-ascii" }),
  fc.string({ unit: "grapheme" }),
  fc.constantFrom("", " ", "{", "[1,", "<html></html>", "undefined", "NaN", "{'a':1}"),
).filter((s) => {
  try {
    JSON.parse(s);
    return false;
  } catch {
    return true;
  }
});
const notAnErrorBody = fc.constantFrom(
  "null",
  "[]",
  "1",
  '"text"',
  "true",
  "{}",
  '{"error":null}',
  '{"error":"x"}',
  '{"error":{"code":"x"}}',
  '{"error":{"message":"x"}}',
  '{"error":{"code":1,"message":"x"}}',
);

// ---------------------------------------------------------------------------
// The independent description of a safe body (see redactionOracle.ts)
// ---------------------------------------------------------------------------

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

const extras: fc.Arbitrary<Record<string, unknown>> = fc
  .array(entry(tree), { maxLength: 4 })
  .map((entries) => Object.fromEntries(entries));

const anyIdentifier = fc.oneof(rawAccount, rawPhone, rawUpi, maskedValue, fc.string({ unit: "grapheme-ascii" }));

/** A label that is not itself a raw identifier, so redaction leaves it as it is. */
const plainLabel = fc.string({ unit: "grapheme-ascii", maxLength: 40 }).filter((s) => !looksUnmasked(s));

const answer: fc.Arbitrary<Answer> = fc.constantFrom("YES", "NO");

const signals = fc.record({
  amount_inr: fc.integer({ min: 1, max: 10_000_000 }),
  beneficiary_age_hours: fc.integer({ min: 0, max: 100_000 }),
  is_late_night: fc.boolean(),
  active_unknown_call: fc.boolean(),
  rapid_retry_count: fc.integer({ min: 0, max: 20 }),
});

describe("saved-recipient client properties", () => {
  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.2, 22.5, 22.9
  it("Property 37: an add body carries no secret value and no unmasked identifier, and keeps its own fields", async () => {
    await fc.assert(
      fc.asyncProperty(extras, plainLabel, anyIdentifier, fc.boolean(), fc.boolean(), async (extra, label, identifier, late, call) => {
        const stub = installFetchStub().respond(201, makeView("INSTANT_PASS"));

        await addBeneficiary({ ...extra, label, identifier_masked: identifier, is_late_night: late, active_unknown_call: call });

        const body = stub.calls[0]?.body as Record<string, unknown>;
        assertSafe(body);
        expect(body).toMatchObject({ label, is_late_night: late, active_unknown_call: call });
        // A value that is already masked is sent as it is. A raw one is sent masked.
        if (isMasked(identifier)) expect(body.identifier_masked).toBe(identifier);
        else if (looksUnmasked(identifier)) expect(body.identifier_masked).not.toBe(identifier);
        else expect(body.identifier_masked).toBe(identifier);
        expect(stub.calls[0]?.method).toBe("POST");
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.2, 22.5
  it("Property 37: a patch body carries no secret value and no unmasked identifier, and keeps its own fields", async () => {
    const changes = fc.record(
      {
        label: plainLabel,
        trust_level: fc.constantFrom<TrustLevel>("NEW", "TRUSTED"),
        protection_level: fc.constantFrom<ProtectionLevel>("EXTRA_CARE", "STANDARD"),
        is_late_night: fc.boolean(),
        active_unknown_call: fc.boolean(),
      },
      { requiredKeys: [] },
    );
    await fc.assert(
      fc.asyncProperty(extras, changes, hex32, async (extra, change, id) => {
        const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));

        await patchBeneficiary(id, { ...extra, ...change } as PatchBeneficiaryRequest);

        const body = stub.calls[0]?.body;
        assertSafe(body);
        expect(body).toMatchObject(change);
        expect(stub.calls[0]?.method).toBe("PATCH");
        expect(stub.calls[0]?.url).toBe(`${API_BASE}/api/v1/beneficiaries/${id}`);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.2, 22.4, 22.5
  it("Property 37: a verification body is `answers` alone, carries no secret value, and keeps the three answers", async () => {
    await fc.assert(
      fc.asyncProperty(extras, answer, answer, answer, hex32, async (extra, coercion, acquaintance, secrecy, id) => {
        const stub = installFetchStub().respond(200, { outcome: "PASSED", beneficiary: makeView("INSTANT_PASS") });

        await verifyBeneficiary(id, { ...extra, coercion, acquaintance, secrecy } as VerificationAnswers);

        const body = stub.calls[0]?.body as { answers: unknown };
        assertSafe(body);
        expect(Object.keys(body)).toEqual(["answers"]);
        expect(body.answers).toMatchObject({ coercion, acquaintance, secrecy });
        expect(stub.calls[0]?.url).toBe(`${API_BASE}/api/v1/beneficiaries/${id}/verification`);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.3, 22.5
  it("Property 37: an evaluate body that names a recipient carries no secret value, and its id is sent as it is", async () => {
    await fc.assert(
      fc.asyncProperty(signals, hex32, fc.option(anyIdentifier, { nil: undefined }), extras, async (sig, id, identifier, extra) => {
        const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
        const request = { ...extra, ...sig, beneficiary_id: id, ...(identifier === undefined ? {} : { upi_masked: identifier }) };

        await evaluate(request as EvaluateRequest);

        const body = stub.calls[0]?.body;
        assertSafe(body);
        expect(body).toMatchObject({ ...sig, beneficiary_id: id });
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.2, 22.4
  it("Property 37: a GET or a DELETE sends no body and no content type, and the id stays in one path segment", async () => {
    const ids = fc.string({ unit: "grapheme" }).filter((s) => !/^\.*$/.test(s) || s === "");
    await fc.assert(
      fc.asyncProperty(ids, fc.constantFrom("get", "delete", "patch", "verify"), async (id, which) => {
        const stub = installFetchStub().respond(200, {});
        if (which === "get") await getBeneficiary(id);
        else if (which === "delete") await deleteBeneficiary(id);
        else if (which === "patch") await patchBeneficiary(id, { label: "x" });
        else await verifyBeneficiary(id, { coercion: "NO", acquaintance: "YES", secrecy: "NO" });

        const call = stub.calls[0];
        if (which === "get" || which === "delete") {
          expect(call?.body).toBeUndefined();
          expect(call?.headers["content-type"]).toBeUndefined();
        }
        // The path is read back with a URL parser: the id is one segment, whatever characters it holds.
        const url = new URL(call?.url as string);
        expect(url.search).toBe("");
        expect(url.hash).toBe("");
        const segments = url.pathname.split("/");
        expect(segments.slice(0, 4)).toEqual(["", "api", "v1", "beneficiaries"]);
        expect(segments).toHaveLength(which === "verify" ? 6 : 5);
        expect(decodeURIComponent(segments[4] as string)).toBe(id);
        if (which === "verify") expect(segments[5]).toBe("verification");
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.2, 22.4
  it("Property 37: the list call sends no body and no content type", async () => {
    const stub = installFetchStub().respond(200, []);
    await listBeneficiaries();
    expect(stub.calls[0]?.body).toBeUndefined();
    expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a 4xx answer with a readable error body is an ApiError with the status, code, message and field", async () => {
    await fc.assert(
      fc.asyncProperty(anyCall, fc.integer({ min: 400, max: 499 }), errorDetail, async (call, status, detail) => {
        installFetchStub().respond(status, errorBodyFor(detail));

        const err = await outcomeOf(call.run());

        expect(err).toBeInstanceOf(ApiError);
        const apiError = err as ApiError;
        expect(apiError.status).toBe(status);
        expect(apiError.code).toBe(detail.code);
        expect(apiError.message).toBe(detail.message);
        expect(apiError.field).toBe(detail.field);
        expect(apiError.beneficiary).toBeUndefined();
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a 409 carries a complete View, and only a 409 does", async () => {
    await fc.assert(
      fc.asyncProperty(anyCall, fc.integer({ min: 400, max: 499 }), errorDetail, view, async (call, status, detail, current) => {
        installFetchStub().respond(status, errorBodyFor(detail, { beneficiary: current }));

        const err = await outcomeOf(call.run());

        expect(err).toBeInstanceOf(ApiError);
        const apiError = err as ApiError;
        expect(apiError.status).toBe(status);
        if (status === 409) expect(apiError.beneficiary).toEqual(current);
        else expect(apiError.beneficiary).toBeUndefined();
        expect(apiError.vault).toBeUndefined();
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a 409 whose View is not complete is still an ApiError, with no View", async () => {
    await fc.assert(
      fc.asyncProperty(anyCall, errorDetail, corrupted, async (call, detail, broken) => {
        installFetchStub().respond(409, errorBodyFor(detail, { beneficiary: broken }));

        const err = await outcomeOf(call.run());

        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).status).toBe(409);
        expect((err as ApiError).code).toBe(detail.code);
        expect((err as ApiError).beneficiary).toBeUndefined();
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a 5xx answer is a NetworkError, whatever the body says", async () => {
    await fc.assert(
      fc.asyncProperty(anyCall, fc.integer({ min: 500, max: 599 }), errorDetail, fc.option(view, { nil: undefined }), async (call, status, detail, current) => {
        installFetchStub().respond(status, errorBodyFor(detail, current ? { beneficiary: current } : {}));

        const err = await outcomeOf(call.run());

        expect(err).toBeInstanceOf(NetworkError);
        expect(err).not.toBeInstanceOf(ApiError);
        expect(isAbortError(err)).toBe(false);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a 2xx body that is not JSON is a NetworkError", async () => {
    await fc.assert(
      fc.asyncProperty(anyCall, fc.constantFrom(200, 201, 202), nonJson, async (call, status, body) => {
        installFetchStub().respondRaw(status, body);

        const err = await outcomeOf(call.run());

        expect(err).toBeInstanceOf(NetworkError);
        expect(err).not.toBeInstanceOf(ApiError);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a 4xx answer with no readable error body is a NetworkError", async () => {
    await fc.assert(
      fc.asyncProperty(
        anyCall,
        fc.integer({ min: 400, max: 499 }),
        fc.oneof(nonJson, notAnErrorBody),
        fc.option(view, { nil: undefined }),
        async (call, status, body, current) => {
          // A View beside no error object does not make an error body.
          const stub = installFetchStub();
          if (current && status === 409) stub.respond(status, { beneficiary: current });
          else stub.respondRaw(status, body);

          const err = await outcomeOf(call.run());

          expect(err).toBeInstanceOf(NetworkError);
          expect(err).not.toBeInstanceOf(ApiError);
        },
      ),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a failure that the caller did not cause is a NetworkError, even one that is named AbortError", async () => {
    const failure = fc.oneof(
      fc.constant(new TypeError("Failed to fetch")),
      text.map((message) => new Error(message)),
      text,
      fc.constant(undefined),
      fc.constant(null),
      fc.constant(new DOMException("The operation timed out.", "TimeoutError")),
      // The client's own timer aborts with this name. It is a time-out, not a cancel.
      fc.constant(new DOMException("The operation was aborted.", "AbortError")),
    );
    await fc.assert(
      fc.asyncProperty(anyCall, failure, async (call, error) => {
        installFetchStub().reject(error);

        const err = await outcomeOf(call.run());

        expect(err).toBeInstanceOf(NetworkError);
        expect(isAbortError(err)).toBe(false);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.9
  it("Property 37: a call the caller cancels rejects as an abort, before or after it is sent", async () => {
    await fc.assert(
      fc.asyncProperty(anyCall, fc.boolean(), async (call, abortFirst) => {
        const stub = installFetchStub().hang();
        const caller = new AbortController();
        if (abortFirst) caller.abort();

        const outcome = outcomeOf(call.run(caller.signal));
        if (!abortFirst) caller.abort();

        const err = await outcome;
        expect(isAbortError(err)).toBe(true);
        expect(err).not.toBeInstanceOf(NetworkError);
        expect(err).not.toBeInstanceOf(ApiError);
        expect(stub.calls[0]?.signal?.aborted).toBe(true);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 37: The client sends redacted bodies and maps errors
  // Validates: Requirements 22.2, 22.9
  it("Property 37: no call writes to the console, whatever the answer", async () => {
    const spies = (["log", "info", "warn", "error", "debug", "trace"] as const).map((method) =>
      vi.spyOn(console, method).mockImplementation(() => undefined),
    );
    const answerKind = fc.oneof(
      fc.record({ kind: fc.constant("ok" as const), status: fc.constantFrom(200, 201), view }),
      fc.record({ kind: fc.constant("error" as const), status: fc.integer({ min: 400, max: 599 }), detail: errorDetail, view }),
      fc.record({ kind: fc.constant("raw" as const), status: fc.integer({ min: 200, max: 599 }), body: nonJson }),
      fc.record({ kind: fc.constant("down" as const) }),
      fc.record({ kind: fc.constant("cancel" as const) }),
    );

    await fc.assert(
      fc.asyncProperty(anyCall, answerKind, async (call, scenario) => {
        const stub = installFetchStub();
        const caller = new AbortController();
        if (scenario.kind === "ok") stub.respond(scenario.status, scenario.view);
        else if (scenario.kind === "error") stub.respond(scenario.status, errorBodyFor(scenario.detail, { beneficiary: scenario.view }));
        else if (scenario.kind === "raw") stub.respondRaw(scenario.status, scenario.body);
        else if (scenario.kind === "down") stub.reject();
        else stub.hang();

        const outcome = outcomeOf(call.run(caller.signal));
        if (scenario.kind === "cancel") caller.abort();
        await outcome;
      }),
      { numRuns: PBT_RUNS },
    );

    spies.forEach((spy) => expect(spy).not.toHaveBeenCalled());
  });
});
