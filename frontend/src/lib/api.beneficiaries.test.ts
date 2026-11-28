import { afterEach, describe, expect, it, vi } from "vitest";

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
  REQUEST_TIMEOUT_MS,
  verifyBeneficiary,
  type BeneficiaryView,
  type EvaluateRequest,
  type VerificationAnswers,
} from "./api";

// ---------------------------------------------------------------------------
// Fixtures. The View is written out here so that a change to the client's own
// types cannot change what the tests expect.
// ---------------------------------------------------------------------------

const ID = "3b0e5c1a9d2f4b6a8c7e1d0f2a3b4c5d";

const VIEW: BeneficiaryView = {
  beneficiary_id: ID,
  label: "Cousin",
  identifier_masked: "r***@okaxis",
  added_at: 1790000000,
  age_hours: 2,
  trust_level: "NEW",
  protection_level: "EXTRA_CARE",
  pressure_flag: false,
  trust_eligible_at: 1790259200,
  indicators: ["ADDED_RECENTLY", "NOT_VERIFIED", "WAITING_PERIOD", "EXTRA_CARE_ON"],
};

const VIEW_WITH_HISTORY: BeneficiaryView = {
  ...VIEW,
  verified_at: 1790003600,
  history: [
    { at: 1790007200, kind: "PAYMENT", amount_inr: 18000 },
    { at: 1790003600, kind: "VERIFIED" },
    { at: 1790000000, kind: "ADDED" },
  ],
};

const ANSWERS: VerificationAnswers = { coercion: "NO", acquaintance: "YES", secrecy: "NO" };

const EVALUATE_BASE: EvaluateRequest = {
  amount_inr: 18000,
  beneficiary_age_hours: 6,
  is_late_night: false,
  active_unknown_call: false,
  rapid_retry_count: 0,
};

const errorBody = (code: string, message: string, field?: string) => ({
  error: field === undefined ? { code, message } : { code, message, field },
});

/** The six calls, each with the method and path the backend routes use. */
const CALLS: { name: string; method: string; path: string; run: (signal?: AbortSignal) => Promise<unknown> }[] = [
  { name: "listBeneficiaries", method: "GET", path: "/api/v1/beneficiaries", run: (s) => listBeneficiaries(s) },
  {
    name: "addBeneficiary",
    method: "POST",
    path: "/api/v1/beneficiaries",
    run: (s) => addBeneficiary({ label: "Cousin", identifier_masked: "r***@okaxis" }, s),
  },
  { name: "getBeneficiary", method: "GET", path: `/api/v1/beneficiaries/${ID}`, run: (s) => getBeneficiary(ID, s) },
  {
    name: "patchBeneficiary",
    method: "PATCH",
    path: `/api/v1/beneficiaries/${ID}`,
    run: (s) => patchBeneficiary(ID, { label: "Uncle" }, s),
  },
  { name: "deleteBeneficiary", method: "DELETE", path: `/api/v1/beneficiaries/${ID}`, run: (s) => deleteBeneficiary(ID, s) },
  {
    name: "verifyBeneficiary",
    method: "POST",
    path: `/api/v1/beneficiaries/${ID}/verification`,
    run: (s) => verifyBeneficiary(ID, ANSWERS, s),
  },
];

const outcomeOf = (promise: Promise<unknown>): Promise<unknown> =>
  promise.then(
    () => null,
    (err: unknown) => err,
  );

afterEach(() => {
  vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// The six calls and the evaluate call
// ---------------------------------------------------------------------------

describe("the saved-recipient calls", () => {
  it("listBeneficiaries reads with GET, sends no body and no content type, and returns the list", async () => {
    const stub = installFetchStub().respond(200, [VIEW, VIEW_WITH_HISTORY]);

    await expect(listBeneficiaries()).resolves.toEqual([VIEW, VIEW_WITH_HISTORY]);

    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]).toMatchObject({ method: "GET", url: `${API_BASE}/api/v1/beneficiaries`, body: undefined });
    expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
  });

  it("listBeneficiaries returns an empty list as an empty list", async () => {
    installFetchStub().respond(200, []);
    await expect(listBeneficiaries()).resolves.toEqual([]);
  });

  it("addBeneficiary posts the label, the masked identifier and the two flags, and accepts HTTP 201", async () => {
    const stub = installFetchStub().respond(201, VIEW);

    await expect(
      addBeneficiary({ label: "Cousin", identifier_masked: "r***@okaxis", is_late_night: true, active_unknown_call: false }),
    ).resolves.toEqual(VIEW);

    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/beneficiaries`,
      body: { label: "Cousin", identifier_masked: "r***@okaxis", is_late_night: true, active_unknown_call: false },
    });
    expect(stub.calls[0]?.headers["content-type"]).toBe("application/json");
  });

  it("addBeneficiary leaves out a flag the caller did not give", async () => {
    const stub = installFetchStub().respond(201, VIEW);
    await addBeneficiary({ label: "Cousin", identifier_masked: "XXXX-XXXX-4821" });
    expect(stub.calls[0]?.body).toEqual({ label: "Cousin", identifier_masked: "XXXX-XXXX-4821" });
  });

  it("getBeneficiary reads one recipient with GET, sends no body, and returns its history", async () => {
    const stub = installFetchStub().respond(200, VIEW_WITH_HISTORY);

    await expect(getBeneficiary(ID)).resolves.toEqual(VIEW_WITH_HISTORY);

    expect(stub.calls[0]).toMatchObject({ method: "GET", url: `${API_BASE}/api/v1/beneficiaries/${ID}`, body: undefined });
    expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
  });

  it("patchBeneficiary sends PATCH with the changes and returns the updated View", async () => {
    const updated: BeneficiaryView = { ...VIEW, label: "Uncle" };
    const stub = installFetchStub().respond(200, updated);

    await expect(patchBeneficiary(ID, { label: "Uncle" })).resolves.toEqual(updated);

    expect(stub.calls[0]).toMatchObject({
      method: "PATCH",
      url: `${API_BASE}/api/v1/beneficiaries/${ID}`,
      body: { label: "Uncle" },
    });
    expect(stub.calls[0]?.headers["content-type"]).toBe("application/json");
  });

  it("patchBeneficiary sends the trust level, the care level and the two flags when given", async () => {
    const stub = installFetchStub().respond(200, { ...VIEW, trust_level: "TRUSTED", protection_level: "STANDARD" });

    await patchBeneficiary(ID, {
      trust_level: "TRUSTED",
      protection_level: "STANDARD",
      is_late_night: false,
      active_unknown_call: true,
    });

    expect(stub.calls[0]?.body).toEqual({
      trust_level: "TRUSTED",
      protection_level: "STANDARD",
      is_late_night: false,
      active_unknown_call: true,
    });
  });

  it("deleteBeneficiary sends DELETE with no body and no content type, and returns the result", async () => {
    const stub = installFetchStub().respond(200, { removed: true });

    await expect(deleteBeneficiary(ID)).resolves.toEqual({ removed: true });

    expect(stub.calls[0]).toMatchObject({ method: "DELETE", url: `${API_BASE}/api/v1/beneficiaries/${ID}`, body: undefined });
    expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
  });

  it("verifyBeneficiary posts the three answers under `answers` and nothing else", async () => {
    const result = { outcome: "PASSED", beneficiary: { ...VIEW, verified_at: 1790003600 } };
    const stub = installFetchStub().respond(200, result);

    await expect(verifyBeneficiary(ID, ANSWERS)).resolves.toEqual(result);

    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/beneficiaries/${ID}/verification`,
      body: { answers: { coercion: "NO", acquaintance: "YES", secrecy: "NO" } },
    });
    expect(Object.keys(stub.calls[0]?.body as object)).toEqual(["answers"]);
    expect(Object.keys((stub.calls[0]?.body as { answers: object }).answers).sort()).toEqual(["acquaintance", "coercion", "secrecy"]);
  });

  it("verifyBeneficiary returns the message of a NOT_PASSED outcome", async () => {
    const result = { outcome: "NOT_PASSED", message: "Thank you for being careful.", beneficiary: { ...VIEW, pressure_flag: true } };
    installFetchStub().respond(200, result);
    await expect(verifyBeneficiary(ID, { coercion: "YES", acquaintance: "NO", secrecy: "YES" })).resolves.toEqual(result);
  });

  it.each(CALLS)("$name uses $method and the path $path", async ({ method, path, run }) => {
    const stub = installFetchStub().respond(200, method === "DELETE" ? { removed: true } : VIEW);

    await run();

    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]?.method).toBe(method);
    expect(stub.calls[0]?.url).toBe(`${API_BASE}${path}`);
  });

  it.each(CALLS.filter((call) => call.method === "GET" || call.method === "DELETE"))(
    "$name sends no body and no content type",
    async ({ run }) => {
      const stub = installFetchStub().respond(200, {});
      await run();
      expect(stub.calls[0]?.body).toBeUndefined();
      expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
    },
  );

  it.each(["getBeneficiary", "patchBeneficiary", "deleteBeneficiary", "verifyBeneficiary"])(
    "%s encodes the id in the path",
    async (name) => {
      const stub = installFetchStub().respond(200, {});
      const awkward = "a/b c?d#e%f";
      const encoded = "a%2Fb%20c%3Fd%23e%25f";
      if (name === "getBeneficiary") await getBeneficiary(awkward);
      if (name === "patchBeneficiary") await patchBeneficiary(awkward, { label: "x" });
      if (name === "deleteBeneficiary") await deleteBeneficiary(awkward);
      if (name === "verifyBeneficiary") await verifyBeneficiary(awkward, ANSWERS);

      const suffix = name === "verifyBeneficiary" ? "/verification" : "";
      expect(stub.calls[0]?.url).toBe(`${API_BASE}/api/v1/beneficiaries/${encoded}${suffix}`);
    },
  );

  it("passes the caller's signal on, through one controller of its own", async () => {
    const stub = installFetchStub().respond(200, []);
    await listBeneficiaries(new AbortController().signal);
    expect(stub.calls[0]?.signal).toBeInstanceOf(AbortSignal);
  });
});

describe("evaluate with a saved recipient", () => {
  it("sends beneficiary_id and no masked identifier when the caller gives only the id", async () => {
    const stub = installFetchStub().respond(200, makeView("REALITY_CHECK"));

    await evaluate({ ...EVALUATE_BASE, beneficiary_id: ID });

    expect(stub.calls[0]).toMatchObject({ method: "POST", url: `${API_BASE}/api/v1/transactions/evaluate` });
    expect(stub.calls[0]?.body).toEqual({ ...EVALUATE_BASE, beneficiary_id: ID });
  });

  it("leaves beneficiary_id out when the caller gives none", async () => {
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    await evaluate({ ...EVALUATE_BASE, upi_masked: "s***@okaxis" });
    expect(stub.calls[0]?.body).toEqual({ ...EVALUATE_BASE, upi_masked: "s***@okaxis" });
    expect(stub.calls[0]?.body).not.toHaveProperty("beneficiary_id");
  });

  it("keeps the five signals and the four masked fields with the same names", async () => {
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    const full: EvaluateRequest = {
      ...EVALUATE_BASE,
      payer_account_masked: "XXXX-XXXX-1111",
      beneficiary_masked: "XXXX-XXXX-2222",
      upi_masked: "a***@okaxis",
      phone_masked: "+91-XXXXXX3333",
    };
    await evaluate(full);
    expect(stub.calls[0]?.body).toEqual(full);
  });
});

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

describe("errors from the saved-recipient routes", () => {
  it.each(["getBeneficiary", "patchBeneficiary", "deleteBeneficiary", "verifyBeneficiary"] as const)(
    "%s turns not_found into an ApiError with no recipient",
    async (name) => {
      installFetchStub().respond(404, errorBody("not_found", "We could not find that recipient."));
      const run = CALLS.find((call) => call.name === name)?.run;
      const err = await outcomeOf((run as (signal?: AbortSignal) => Promise<unknown>)());

      expect(err).toBeInstanceOf(ApiError);
      expect(err).toMatchObject({
        status: 404,
        code: "not_found",
        message: "We could not find that recipient.",
        field: undefined,
        vault: undefined,
        beneficiary: undefined,
      });
    },
  );

  it("carries the current recipient from a 409 state_conflict", async () => {
    const current: BeneficiaryView = { ...VIEW, label: "Changed", trust_level: "TRUSTED", protection_level: "STANDARD" };
    installFetchStub().respond(409, {
      ...errorBody("state_conflict", "This recipient was just changed. Here is how it looks now."),
      beneficiary: current,
    });

    const err = await outcomeOf(patchBeneficiary(ID, { label: "Mine" }));

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 409, code: "state_conflict", message: "This recipient was just changed. Here is how it looks now." });
    expect((err as ApiError).beneficiary).toEqual(current);
    expect((err as ApiError).vault).toBeUndefined();
  });

  it("carries a recipient that has a verified time and a history", async () => {
    installFetchStub().respond(409, { ...errorBody("state_conflict", "Changed."), beneficiary: VIEW_WITH_HISTORY });
    const err = await outcomeOf(deleteBeneficiary(ID));
    expect((err as ApiError).beneficiary).toEqual(VIEW_WITH_HISTORY);
  });

  it("carries the recipient for a verification that lost a race too", async () => {
    installFetchStub().respond(409, { ...errorBody("state_conflict", "Changed."), beneficiary: VIEW });
    const err = await outcomeOf(verifyBeneficiary(ID, ANSWERS));
    expect((err as ApiError).beneficiary).toEqual(VIEW);
  });

  it.each([
    ["a key missing", (v: Record<string, unknown>) => Object.fromEntries(Object.entries(v).filter(([k]) => k !== "label"))],
    ["a level in the wrong letter case", (v: Record<string, unknown>) => ({ ...v, trust_level: "trusted" })],
    ["a level the server does not use", (v: Record<string, unknown>) => ({ ...v, protection_level: "NONE" })],
    ["indicators that are not a list", (v: Record<string, unknown>) => ({ ...v, indicators: "ADDED_RECENTLY" })],
    ["an indicator the client has no sentence for", (v: Record<string, unknown>) => ({ ...v, indicators: ["SOMETHING_NEW"] })],
    ["a flag that is a string", (v: Record<string, unknown>) => ({ ...v, pressure_flag: "false" })],
    ["an hour count that is a string", (v: Record<string, unknown>) => ({ ...v, age_hours: "2" })],
    ["a time that is not whole", (v: Record<string, unknown>) => ({ ...v, added_at: 1.5 })],
    ["an empty id", (v: Record<string, unknown>) => ({ ...v, beneficiary_id: "" })],
    ["a verified time that is null", (v: Record<string, unknown>) => ({ ...v, verified_at: null })],
    ["a history entry of an unknown kind", (v: Record<string, unknown>) => ({ ...v, history: [{ at: 1, kind: "BOUGHT" }] })],
    ["a history that is not a list", (v: Record<string, unknown>) => ({ ...v, history: {} })],
  ])("gives an ApiError with no recipient when a 409 holds a View with %s", async (_name, corrupt) => {
    installFetchStub().respond(409, { ...errorBody("state_conflict", "Changed."), beneficiary: corrupt({ ...VIEW }) });

    const err = await outcomeOf(patchBeneficiary(ID, { label: "Mine" }));

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 409, code: "state_conflict" });
    expect((err as ApiError).beneficiary).toBeUndefined();
  });

  it.each([["null", null], ["a string", "Cousin"], ["a list", [VIEW]], ["an empty object", {}]])(
    "gives no recipient when the `beneficiary` of a 409 is %s",
    async (_name, junk) => {
      installFetchStub().respond(409, { ...errorBody("state_conflict", "Changed."), beneficiary: junk });
      const err = await outcomeOf(patchBeneficiary(ID, { label: "Mine" }));
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).beneficiary).toBeUndefined();
    },
  );

  it("has no recipient for a 409 that carries none", async () => {
    installFetchStub().respond(409, errorBody("duplicate_beneficiary", "That recipient is already on your list."));
    const err = await outcomeOf(addBeneficiary({ label: "Cousin", identifier_masked: "r***@okaxis" }));
    expect(err).toMatchObject({ status: 409, code: "duplicate_beneficiary", beneficiary: undefined });
  });

  it.each([400, 404, 413])("ignores a recipient on an answer that is HTTP %i", async (status) => {
    installFetchStub().respond(status, { ...errorBody("some_code", "A calm message."), beneficiary: VIEW });
    const err = await outcomeOf(getBeneficiary(ID));
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).beneficiary).toBeUndefined();
  });

  it("does not read a 409 that has a recipient and no error object as an ApiError", async () => {
    installFetchStub().respond(409, { beneficiary: VIEW });
    const err = await outcomeOf(patchBeneficiary(ID, { label: "Mine" }));
    expect(err).toBeInstanceOf(NetworkError);
    expect(err).not.toBeInstanceOf(ApiError);
  });

  it("keeps the code, the message and the field of the other recipient errors", async () => {
    const cases: [number, string, string, string | undefined][] = [
      [409, "beneficiary_limit", "Your list is full. Please remove a recipient before adding another.", undefined],
      [409, "not_ready", "RukPay needs a little more time before it can trust this recipient.", "trust_level"],
      [400, "invalid_field", "Please use a short nickname of up to 40 characters, with no long number and no @ sign.", "label"],
      [400, "invalid_request", "Please check the request and try again.", undefined],
      [413, "too_large", "That request is too large.", undefined],
    ];
    for (const [status, code, message, field] of cases) {
      installFetchStub().respond(status, errorBody(code, message, field));
      const err = await outcomeOf(patchBeneficiary(ID, { trust_level: "TRUSTED" }));
      expect(err).toBeInstanceOf(ApiError);
      expect(err).toMatchObject({ status, code, message, field });
    }
  });

  it.each(CALLS)("$name turns HTTP 500, 502 and 503 into a NetworkError, even with an error body", async ({ run }) => {
    for (const status of [500, 502, 503]) {
      installFetchStub().respond(status, errorBody("internal", "Something went wrong on our side."));
      const err = await outcomeOf(run());
      expect(err).toBeInstanceOf(NetworkError);
      expect(err).not.toBeInstanceOf(ApiError);
    }
  });

  it.each([
    ["a 2xx answer that is not JSON", 200, "<html>hello</html>"],
    ["an empty 2xx answer", 200, ""],
    ["an empty 201 answer", 201, ""],
    ["a 4xx answer that is not JSON", 400, "bad gateway text"],
    ["a 4xx answer with no error object", 404, "{}"],
    ["a 4xx answer whose error has no message", 409, '{"error":{"code":"state_conflict"}}'],
  ])("turns %s into a NetworkError", async (_name, status, text) => {
    installFetchStub().respondRaw(status, text);
    const err = await outcomeOf(patchBeneficiary(ID, { label: "Mine" }));
    expect(err).toBeInstanceOf(NetworkError);
    expect(err).not.toBeInstanceOf(ApiError);
  });

  it.each(CALLS)("$name turns a failed fetch into a NetworkError that is not an abort", async ({ run }) => {
    installFetchStub().reject();
    const err = await outcomeOf(run());
    expect(err).toBeInstanceOf(NetworkError);
    expect(isAbortError(err)).toBe(false);
  });

  it.each(CALLS)("$name fails with a NetworkError after 10 seconds, and not a moment before", async ({ run }) => {
    vi.useFakeTimers();
    installFetchStub().hang();
    let outcome: unknown = "pending";
    const done = run().catch((err: unknown) => {
      outcome = err;
    });

    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS - 1);
    expect(outcome).toBe("pending");

    await vi.advanceTimersByTimeAsync(1);
    await done;
    expect(outcome).toBeInstanceOf(NetworkError);
    expect(isAbortError(outcome)).toBe(false);
  });

  it.each(CALLS)("$name rejects with an abort, and not a NetworkError, when the caller cancels", async ({ run }) => {
    const stub = installFetchStub().hang();
    const caller = new AbortController();

    const outcome = outcomeOf(run(caller.signal));
    caller.abort();

    const err = await outcome;
    expect(isAbortError(err)).toBe(true);
    expect(err).not.toBeInstanceOf(NetworkError);
    expect(err).not.toBeInstanceOf(ApiError);
    expect(stub.calls[0]?.signal?.aborted).toBe(true);
  });

  it.each(CALLS)("$name rejects with an abort when the signal was already aborted", async ({ run }) => {
    installFetchStub().hang();
    const caller = new AbortController();
    caller.abort();

    const err = await outcomeOf(run(caller.signal));
    expect(isAbortError(err)).toBe(true);
    expect(err).not.toBeInstanceOf(NetworkError);
  });

  it("leaves no timer behind after an answer or a failure", async () => {
    vi.useFakeTimers();
    installFetchStub()
      .respond(200, [])
      .respond(409, { ...errorBody("state_conflict", "Changed."), beneficiary: VIEW })
      .respond(503, errorBody("internal", "No."));

    await listBeneficiaries();
    expect(vi.getTimerCount()).toBe(0);
    await outcomeOf(patchBeneficiary(ID, { label: "Mine" }));
    expect(vi.getTimerCount()).toBe(0);
    await outcomeOf(deleteBeneficiary(ID));
    expect(vi.getTimerCount()).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// What leaves the browser
// ---------------------------------------------------------------------------

describe("outbound redaction for the saved-recipient calls", () => {
  const SECRETS = { otp: "482913", pin: "1234", password: "hunter2hunter2", passcode: "letmein", cvv: "123" };
  const REDACTED_SECRETS = { otp: "[REDACTED]", pin: "[REDACTED]", password: "[REDACTED]", passcode: "[REDACTED]", cvv: "[REDACTED]" };

  it("addBeneficiary replaces a secret with [REDACTED] and a raw identifier with its masked form", async () => {
    const stub = installFetchStub().respond(201, VIEW);
    const request = { label: "Cousin", identifier_masked: "rahul@okaxis", is_late_night: false, ...SECRETS };

    await addBeneficiary(request);

    expect(stub.calls[0]?.body).toEqual({ label: "Cousin", identifier_masked: "r***@okaxis", is_late_night: false, ...REDACTED_SECRETS });
  });

  it.each([
    ["a raw account number", "123456789012", "XXXX-XXXX-9012"],
    ["a raw phone number", "9876543210", "+91-XXXXXX3210"],
    ["a raw phone number with its country code", "+91 98765 43210", "+91-XXXXXX3210"],
    ["a raw UPI id", "meera@okhdfc", "m***@okhdfc"],
  ])("addBeneficiary masks %s in the identifier field", async (_name, raw, masked) => {
    const stub = installFetchStub().respond(201, VIEW);

    await addBeneficiary({ label: "Cousin", identifier_masked: raw });

    expect((stub.calls[0]?.body as { identifier_masked: string }).identifier_masked).toBe(masked);
    expect(JSON.stringify(stub.calls[0]?.body)).not.toContain(raw);
  });

  it("addBeneficiary keeps an identifier that is already masked, so masking twice changes nothing", async () => {
    const stub = installFetchStub().respond(201, VIEW).respond(201, VIEW).respond(201, VIEW);
    for (const masked of ["XXXX-XXXX-4821", "+91-XXXXXX3210", "r***@okaxis"]) {
      await addBeneficiary({ label: "Cousin", identifier_masked: masked });
    }
    expect(stub.calls.map((call) => (call.body as { identifier_masked: string }).identifier_masked)).toEqual([
      "XXXX-XXXX-4821",
      "+91-XXXXXX3210",
      "r***@okaxis",
    ]);
  });

  it("patchBeneficiary redacts a secret key in any letter case", async () => {
    const stub = installFetchStub().respond(200, VIEW);
    await patchBeneficiary(ID, { label: "Uncle", OTP: "1", Pin: "2", PassWord: "3", PASSCODE: "4", Cvv: "5" } as never);
    expect(stub.calls[0]?.body).toEqual({
      label: "Uncle",
      OTP: "[REDACTED]",
      Pin: "[REDACTED]",
      PassWord: "[REDACTED]",
      PASSCODE: "[REDACTED]",
      Cvv: "[REDACTED]",
    });
  });

  it("verifyBeneficiary redacts a secret key at any depth", async () => {
    const stub = installFetchStub().respond(200, { outcome: "PASSED", beneficiary: VIEW });
    await verifyBeneficiary(ID, { ...ANSWERS, ...SECRETS, nested: { otp: "9", deeper: [{ pin: "8" }] } } as never);
    expect(stub.calls[0]?.body).toEqual({
      answers: { ...ANSWERS, ...REDACTED_SECRETS, nested: { otp: "[REDACTED]", deeper: [{ pin: "[REDACTED]" }] } },
    });
  });

  it("evaluate redacts a secret and a raw identifier next to beneficiary_id", async () => {
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    await evaluate({ ...EVALUATE_BASE, beneficiary_id: ID, upi_masked: "rahul@okaxis", otp: "482913" } as EvaluateRequest);
    expect(stub.calls[0]?.body).toEqual({ ...EVALUATE_BASE, beneficiary_id: ID, upi_masked: "r***@okaxis", otp: "[REDACTED]" });
  });

  it("sends a Beneficiary_Id unchanged", async () => {
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    await evaluate({ ...EVALUATE_BASE, beneficiary_id: "0123456789abcdef0123456789abcdef" });
    expect((stub.calls[0]?.body as { beneficiary_id: string }).beneficiary_id).toBe("0123456789abcdef0123456789abcdef");
  });
});

describe("the saved-recipient calls and the console", () => {
  it("write nothing to the console on any path", async () => {
    const spies = (["log", "info", "warn", "error", "debug", "trace", "table", "dir", "group", "time"] as const).map((method) =>
      vi.spyOn(console, method).mockImplementation(() => undefined),
    );

    for (const call of CALLS) {
      for (const queue of [
        (s: ReturnType<typeof installFetchStub>) => s.respond(200, call.method === "DELETE" ? { removed: true } : VIEW),
        (s: ReturnType<typeof installFetchStub>) => s.respond(404, errorBody("not_found", "No.")),
        (s: ReturnType<typeof installFetchStub>) => s.respond(409, { ...errorBody("state_conflict", "No."), beneficiary: VIEW }),
        (s: ReturnType<typeof installFetchStub>) => s.respond(409, { ...errorBody("state_conflict", "No."), beneficiary: { junk: 1 } }),
        (s: ReturnType<typeof installFetchStub>) => s.respond(503, errorBody("internal", "No.")),
        (s: ReturnType<typeof installFetchStub>) => s.respondRaw(200, "not json"),
        (s: ReturnType<typeof installFetchStub>) => s.reject(),
      ]) {
        queue(installFetchStub());
        await outcomeOf(call.run());
      }
    }

    const caller = new AbortController();
    installFetchStub().hang();
    const aborted = outcomeOf(listBeneficiaries(caller.signal));
    caller.abort();
    await aborted;

    vi.useFakeTimers();
    installFetchStub().hang();
    const timedOut = outcomeOf(listBeneficiaries());
    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS);
    await timedOut;

    spies.forEach((spy) => expect(spy).not.toHaveBeenCalled());
  });
});

// ---------------------------------------------------------------------------
// The types, read from the source
// ---------------------------------------------------------------------------

const apiSource = Object.values(
  import.meta.glob<string>("./api.ts", { query: "?raw", import: "default", eager: true }),
)[0] as string;

/** The source with `/* *\/` and `//` comments removed, so a sentence that names a word is not read as code. */
const codeOf = (source: string): string => source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");

/** The member names declared on lines of the form `name: type;` or `name?: type;`. */
function memberNames(block: string): string[] {
  return Array.from(block.matchAll(/^\s*(?:readonly\s+)?([A-Za-z_]\w*)\??\s*:/gm), (match) => match[1] as string);
}

describe("the types of the client", () => {
  const code = codeOf(apiSource);

  it("reads the source it is meant to check", () => {
    expect(apiSource.length).toBeGreaterThan(1000);
    expect(code).toContain("export interface BeneficiaryView");
    expect(code).toContain("export interface VerificationAnswers");
  });

  it("declares no field named otp, pin, password, passcode or cvv, in any letter case", () => {
    const names = memberNames(code);
    expect(names.length).toBeGreaterThan(60);
    expect(names.filter((name) => /^(otp|pin|password|passcode|cvv)$/i.test(name))).toEqual([]);
  });

  it("gives BeneficiaryView exactly the keys of the View, with no field for a raw identifier", () => {
    const block = /export interface BeneficiaryView \{([\s\S]*?)\n\}/.exec(code)?.[1] ?? "";
    expect(memberNames(block).sort()).toEqual(
      [
        "added_at",
        "age_hours",
        "beneficiary_id",
        "history",
        "identifier_masked",
        "indicators",
        "label",
        "pressure_flag",
        "protection_level",
        "trust_eligible_at",
        "trust_level",
        "verified_at",
      ].sort(),
    );
  });

  it("gives the request and result types exactly the keys of their routes", () => {
    const keysOf = (name: string): string[] => {
      const block = new RegExp(`export interface ${name}(?: extends \\w+)? \\{([\\s\\S]*?)\\n\\}`).exec(code)?.[1] ?? "";
      return memberNames(block).sort();
    };
    expect(keysOf("AddBeneficiaryRequest")).toEqual(["active_unknown_call", "identifier_masked", "is_late_night", "label"]);
    expect(keysOf("PatchBeneficiaryRequest")).toEqual(["active_unknown_call", "is_late_night", "label", "protection_level", "trust_level"]);
    expect(keysOf("VerificationAnswers")).toEqual(["acquaintance", "coercion", "secrecy"]);
    expect(keysOf("VerificationResult")).toEqual(["beneficiary", "message", "outcome"]);
    expect(keysOf("RemoveResult")).toEqual(["removed"]);
    expect(keysOf("HistoryEntry")).toEqual(["amount_inr", "at", "kind"]);
    expect(keysOf("EvaluateRequest")).toEqual(["beneficiary_id", "beneficiary_masked", "payer_account_masked", "phone_masked", "upi_masked"]);
  });
});
