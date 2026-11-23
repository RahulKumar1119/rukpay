import { afterEach, describe, expect, it, vi } from "vitest";

import { installFetchStub, makeView } from "../test/stubs";
import {
  ApiError,
  API_BASE,
  demoFastForward,
  evaluate,
  eventStreamUrl,
  getVault,
  guardianAction,
  isAbortError,
  NetworkError,
  reconfirm,
  REQUEST_TIMEOUT_MS,
  submitAnswers,
  type EvaluateRequest,
} from "./api";

const REQUEST: EvaluateRequest = {
  amount_inr: 85000,
  beneficiary_age_hours: 1,
  is_late_night: true,
  active_unknown_call: true,
  rapid_retry_count: 0,
  beneficiary_masked: "XXXX-XXXX-4821",
};

const errorBody = (code: string, message: string, field?: string) => ({
  error: field === undefined ? { code, message } : { code, message, field },
});

afterEach(() => {
  vi.useRealTimers();
});

describe("base URL", () => {
  async function loadWith(value: string) {
    vi.stubEnv("VITE_API_BASE_URL", value);
    vi.resetModules();
    return import("./api");
  }

  it.each([
    ["an empty value", "", "http://localhost:8080"],
    ["a blank value", "   ", "http://localhost:8080"],
    ["a value with no slash at the end", "https://api.example.com", "https://api.example.com"],
    ["a value with a slash at the end", "https://api.example.com/", "https://api.example.com"],
    ["a value with a path", "https://example.com/rukpay//", "https://example.com/rukpay"],
  ])("resolves %s", async (_name, value, want) => {
    const api = await loadWith(value);
    expect(api.API_BASE).toBe(want);
    expect(api.eventStreamUrl("v1")).toBe(`${want}/api/v1/events/stream?vault_id=v1`);
  });

  it("sends requests to the resolved base", async () => {
    const api = await loadWith("https://api.example.com/");
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    await api.evaluate(REQUEST);
    expect(stub.calls[0]?.url).toBe("https://api.example.com/api/v1/transactions/evaluate");
  });
});

describe("requests", () => {
  it("evaluate posts the request as JSON and returns the View", async () => {
    const view = makeView("LOCKED_15M");
    const stub = installFetchStub().respond(200, view);

    await expect(evaluate(REQUEST)).resolves.toEqual(view);

    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/transactions/evaluate`,
      body: REQUEST,
    });
    expect(stub.calls[0]?.headers["content-type"]).toBe("application/json");
  });

  it("getVault reads with GET and sends no body and no content type", async () => {
    const view = makeView("REALITY_CHECK");
    const stub = installFetchStub().respond(200, view);

    await expect(getVault("abc123")).resolves.toEqual(view);

    expect(stub.calls[0]).toMatchObject({ method: "GET", url: `${API_BASE}/api/v1/vault/abc123`, body: undefined });
    expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
  });

  it("encodes the vault id in the path", async () => {
    const stub = installFetchStub().respond(200, makeView("REALITY_CHECK"));
    await getVault("a/b c?d");
    expect(stub.calls[0]?.url).toBe(`${API_BASE}/api/v1/vault/a%2Fb%20c%3Fd`);
  });

  it("submitAnswers posts only the answers", async () => {
    const stub = installFetchStub().respond(200, makeView("LOCKED_15M"));
    await submitAnswers("v1", { coercion: "YES", secrecy: "NO", verification: "NO" });

    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/vault/v1/reality-check`,
      body: { answers: { coercion: "YES", secrecy: "NO", verification: "NO" } },
    });
  });

  it("reconfirm posts only the reconfirm flag", async () => {
    const stub = installFetchStub().respond(200, makeView("EXPIRED", { settled: true }));
    await reconfirm("v1");

    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/vault/v1/reality-check`,
      body: { reconfirm: true },
    });
  });

  it("guardianAction posts the action, the token and the confirmation", async () => {
    const result = { status: "OK", vault: makeView("GUARDIAN_APPROVED") };
    const stub = installFetchStub().respond(200, result);

    await expect(guardianAction("v1", { action: "APPROVE", token: "v1.1700000900.abcd", confirmed: true })).resolves.toEqual(
      result,
    );

    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/vault/v1/guardian-action`,
      body: { action: "APPROVE", token: "v1.1700000900.abcd", confirmed: true },
    });
  });

  it("demoFastForward posts with no body", async () => {
    const stub = installFetchStub().respond(200, makeView("LOCKED_15M", { remaining_seconds: 3 }));
    await demoFastForward("v1");

    expect(stub.calls[0]).toMatchObject({
      method: "POST",
      url: `${API_BASE}/api/v1/vault/v1/demo-fast-forward`,
      body: undefined,
    });
    expect(stub.calls[0]?.headers["content-type"]).toBeUndefined();
  });

  it("eventStreamUrl encodes the id", () => {
    expect(eventStreamUrl("abc123")).toBe(`${API_BASE}/api/v1/events/stream?vault_id=abc123`);
    expect(eventStreamUrl("a b/c&d=e")).toBe(`${API_BASE}/api/v1/events/stream?vault_id=a%20b%2Fc%26d%3De`);
  });

  it("removes secrets and raw identifiers from the body before it is sent", async () => {
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    const dirty = { ...REQUEST, otp: "482913", upi_masked: "rahul@okaxis", phone_masked: "9876543210" };

    await evaluate(dirty);

    expect(stub.calls[0]?.body).toEqual({
      ...REQUEST,
      otp: "[REDACTED]",
      upi_masked: "r***@okaxis",
      phone_masked: "+91-XXXXXX3210",
    });
  });
});

describe("abort and timeout", () => {
  it("passes a signal to fetch", async () => {
    const stub = installFetchStub().respond(200, makeView("INSTANT_PASS"));
    await getVault("v1");
    expect(stub.calls[0]?.signal).toBeInstanceOf(AbortSignal);
  });

  it("cancels the request when the caller aborts, and the error is an abort, not a failure", async () => {
    const stub = installFetchStub().hang();
    const caller = new AbortController();

    const pending = getVault("v1", caller.signal);
    const outcome = pending.then(
      () => null,
      (err: unknown) => err,
    );
    caller.abort();

    const err = await outcome;
    expect(isAbortError(err)).toBe(true);
    expect(err).not.toBeInstanceOf(NetworkError);
    expect(stub.calls[0]?.signal?.aborted).toBe(true);
  });

  it("rejects with an abort when the signal is already aborted", async () => {
    installFetchStub().hang();
    const caller = new AbortController();
    caller.abort();

    const err = await getVault("v1", caller.signal).then(
      () => null,
      (e: unknown) => e,
    );
    expect(isAbortError(err)).toBe(true);
  });

  it("fails with a NetworkError after 10 seconds", async () => {
    vi.useFakeTimers();
    installFetchStub().hang();
    let outcome: unknown = "pending";
    const done = getVault("v1").catch((err: unknown) => {
      outcome = err;
    });

    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS - 1);
    expect(outcome).toBe("pending");

    await vi.advanceTimersByTimeAsync(1);
    await done;
    expect(outcome).toBeInstanceOf(NetworkError);
    expect(isAbortError(outcome)).toBe(false);
  });

  it("leaves no timer behind after an answer", async () => {
    vi.useFakeTimers();
    installFetchStub().respond(200, makeView("INSTANT_PASS")).respond(404, errorBody("not_found", "Not found."));

    await getVault("v1");
    expect(vi.getTimerCount()).toBe(0);

    await getVault("v1").catch(() => undefined);
    expect(vi.getTimerCount()).toBe(0);
  });
});

describe("errors", () => {
  it("turns a 4xx answer into an ApiError with the server's fields", async () => {
    installFetchStub().respond(400, errorBody("invalid_field", "Please check the amount and try again.", "amount_inr"));

    const err = await evaluate(REQUEST).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({
      status: 400,
      code: "invalid_field",
      message: "Please check the amount and try again.",
      field: "amount_inr",
      vault: undefined,
    });
  });

  it.each([403, 404, 413])("turns HTTP %i into an ApiError", async (status) => {
    installFetchStub().respond(status, errorBody("some_code", "A calm message."));
    const err = await getVault("v1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status, code: "some_code", message: "A calm message.", field: undefined });
  });

  it("carries the View from the vault property of a 409", async () => {
    const current = makeView("VETOED");
    installFetchStub().respond(409, { ...errorBody("state_conflict", "This payment has already moved on."), vault: current });

    const err = await submitAnswers("v1", { coercion: "NO", secrecy: "NO", verification: "YES" }).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).vault).toEqual(current);
  });

  it("carries the View from a 409 body that is itself a View", async () => {
    const current = makeView("GUARDIAN_APPROVED");
    installFetchStub().respond(409, current);

    const err = await reconfirm("v1").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).vault).toEqual(current);
  });

  it("prefers the vault property over a View-shaped body", async () => {
    const inner = makeView("VETOED", { vault_id: "inner" });
    installFetchStub().respond(409, { ...makeView("EXPIRED", { vault_id: "outer" }), ...errorBody("state_conflict", "x"), vault: inner });

    const err = (await getVault("v1").catch((e: unknown) => e)) as ApiError;
    expect(err.vault?.vault_id).toBe("inner");
  });

  it("has no View for a 409 that carries none", async () => {
    installFetchStub().respond(409, errorBody("state_conflict", "Moved on."));
    const err = (await getVault("v1").catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.vault).toBeUndefined();
  });

  it("ignores a vault property on an answer that is not a 409", async () => {
    installFetchStub().respond(400, { ...errorBody("invalid_request", "No."), vault: makeView("VETOED") });
    const err = (await getVault("v1").catch((e: unknown) => e)) as ApiError;
    expect(err.vault).toBeUndefined();
  });

  it.each([500, 502, 503])("turns HTTP %i into a NetworkError, even with an error body", async (status) => {
    installFetchStub().respond(status, errorBody("internal", "Something went wrong on our side."));
    const err = await getVault("v1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(NetworkError);
    expect(err).not.toBeInstanceOf(ApiError);
  });

  it.each([
    ["a 2xx answer that is not JSON", 200, "<html>hello</html>"],
    ["an empty 2xx answer", 200, ""],
    ["a 4xx answer that is not JSON", 400, "bad gateway text"],
    ["a 4xx answer with no error object", 404, "{}"],
    ["a 4xx answer whose error has no message", 400, '{"error":{"code":"x"}}'],
  ])("turns %s into a NetworkError", async (_name, status, text) => {
    installFetchStub().respondRaw(status, text);
    const err = await getVault("v1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(NetworkError);
  });

  it("turns a failed fetch into a NetworkError", async () => {
    installFetchStub().reject();
    const err = await getVault("v1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(NetworkError);
    expect(isAbortError(err)).toBe(false);
  });

  it("writes nothing to the console on any path", async () => {
    const spies = (["log", "info", "warn", "error", "debug"] as const).map((m) =>
      vi.spyOn(console, m).mockImplementation(() => undefined),
    );
    installFetchStub()
      .respond(200, makeView("INSTANT_PASS"))
      .respond(400, errorBody("invalid_field", "No."))
      .respond(503, errorBody("internal", "No."))
      .respondRaw(200, "nope")
      .reject();

    for (let i = 0; i < 5; i++) await getVault("v1").catch(() => undefined);

    spies.forEach((spy) => expect(spy).not.toHaveBeenCalled());
    vi.restoreAllMocks();
  });
});
