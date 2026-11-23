import { describe, expect, it, vi } from "vitest";

import { installFetchStub } from "../test/stubs";
import {
  hashIdentifier,
  maskAccount,
  maskPhone,
  maskUpi,
  redactObject,
  redactSecret,
} from "./privacyMasker";

describe("maskAccount", () => {
  it.each([
    ["a long account number", "123456789012", "XXXX-XXXX-9012"],
    ["a 16-digit number", "1234567890124821", "XXXX-XXXX-4821"],
    ["digits with hyphens", "1234-5678-9012-4821", "XXXX-XXXX-4821"],
    ["digits with spaces", "1234 5678 9012 4821", "XXXX-XXXX-4821"],
    ["exactly four digits", "4821", "XXXX-XXXX-4821"],
    ["digits mixed with letters", "AC12-34", "XXXX-XXXX-1234"],
  ])("masks %s", (_name, input, want) => {
    expect(maskAccount(input)).toBe(want);
  });

  it.each([
    ["an empty string", ""],
    ["three digits", "482"],
    ["no digits", "account number"],
    ["three digits among letters", "a4b8c2"],
  ])("returns [REDACTED] for %s", (_name, input) => {
    expect(maskAccount(input)).toBe("[REDACTED]");
  });

  it("keeps only the last four digits", () => {
    const out = maskAccount("9876543210123456");
    expect(out).toBe("XXXX-XXXX-3456");
    expect(out).not.toContain("98765");
  });
});

describe("maskPhone", () => {
  it.each([
    ["a ten-digit number", "9876543210", "+91-XXXXXX3210"],
    ["a number with a country code", "+91 98765 43210", "+91-XXXXXX3210"],
    ["a number with hyphens", "+91-98765-43210", "+91-XXXXXX3210"],
    ["exactly four digits", "3210", "+91-XXXXXX3210"],
  ])("masks %s", (_name, input, want) => {
    expect(maskPhone(input)).toBe(want);
  });

  it.each([
    ["an empty string", ""],
    ["three digits", "321"],
    ["no digits", "call me"],
  ])("returns [REDACTED] for %s", (_name, input) => {
    expect(maskPhone(input)).toBe("[REDACTED]");
  });
});

describe("maskUpi", () => {
  it.each([
    ["a simple id", "rahul@okaxis", "r***@okaxis"],
    ["a name with dots", "rahul.k@okaxis", "r***@okaxis"],
    ["a handle with a hyphen", "rahul@ok-axis", "r***@ok-axis"],
    ["a handle with a dot", "rahul@bank.upi", "r***@bank.upi"],
    ["a name that starts with a digit", "9876543210@ybl", "9***@ybl"],
    ["a one-letter name", "r@okaxis", "r***@okaxis"],
    ["more than one @, split at the last", "a@b@c", "a***@c"],
  ])("masks %s", (_name, input, want) => {
    expect(maskUpi(input)).toBe(want);
  });

  it.each([
    ["an empty string", ""],
    ["no @", "rahul"],
    ["an empty name", "@okaxis"],
    ["an empty handle", "rahul@"],
    ["only an @", "@"],
    ["a name that starts with a symbol", "_rahul@okaxis"],
    ["a name that starts with a space", " rahul@okaxis"],
    ["a handle with a space", "rahul@ok axis"],
    ["a handle with an underscore", "rahul@ok_axis"],
    ["a handle that ends the string with a newline", "rahul@okaxis\n"],
  ])("returns [REDACTED] for %s", (_name, input) => {
    expect(maskUpi(input)).toBe("[REDACTED]");
  });

  it("keeps only the first character of the name", () => {
    expect(maskUpi("rahulkumar@okaxis")).not.toContain("ahul");
  });
});

describe("redactSecret", () => {
  it.each(["482913", "1234", "hunter2", "", "[REDACTED]"])("hides %j", (input) => {
    expect(redactSecret(input)).toBe("[REDACTED]");
  });
});

describe("hashIdentifier", () => {
  // Published SHA-256 test vectors.
  const ABC = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
  const EMPTY = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";

  it("returns the SHA-256 digest as 64 lowercase hex characters", async () => {
    await expect(hashIdentifier("abc")).resolves.toBe(ABC);
    await expect(hashIdentifier("")).resolves.toBe(EMPTY);
  });

  it("trims and lowercases before it hashes", async () => {
    await expect(hashIdentifier("  ABC\n")).resolves.toBe(ABC);
    await expect(hashIdentifier("\tAbC ")).resolves.toBe(ABC);
  });

  it("gives different digests for different values", async () => {
    expect(await hashIdentifier("rahul@okaxis")).not.toBe(await hashIdentifier("rahul@okicici"));
  });
});

describe("redactObject", () => {
  it("redacts a sample evaluate request body", () => {
    const body = {
      amount_inr: 85000,
      beneficiary_age_hours: 1,
      is_late_night: true,
      active_unknown_call: true,
      rapid_retry_count: 0,
      payer_account_masked: "XXXX-XXXX-4821",
      beneficiary_masked: "123456789012",
      upi_masked: "rahul@okaxis",
      phone_masked: "+91 98765 43210",
      otp: "482913",
      PIN: 1234,
    };
    expect(redactObject(body)).toEqual({
      amount_inr: 85000,
      beneficiary_age_hours: 1,
      is_late_night: true,
      active_unknown_call: true,
      rapid_retry_count: 0,
      payer_account_masked: "XXXX-XXXX-4821",
      beneficiary_masked: "XXXX-XXXX-9012",
      upi_masked: "r***@okaxis",
      phone_masked: "+91-XXXXXX3210",
      otp: "[REDACTED]",
      PIN: "[REDACTED]",
    });
  });

  it("redacts every secret key name in any letter case", () => {
    const out = redactObject({
      otp: "1",
      OTP: "2",
      Pin: "3",
      password: "4",
      PassWord: "5",
      passcode: "6",
      cvv: "7",
      CVV: "8",
    });
    expect(Object.values(out)).toEqual(Array(8).fill("[REDACTED]"));
  });

  it("does not touch a key that only contains a secret word", () => {
    const body = { pinned: "keep", otp_hint: "keep", my_password: "keep", opinion: "keep" };
    expect(redactObject(body)).toEqual(body);
  });

  it("works at any depth, through arrays", () => {
    const body = {
      steps: [{ answers: { coercion: "NO" }, secrets: { otp: "1", nested: [{ pin: "2" }] } }],
      note: "9876543210",
    };
    expect(redactObject(body)).toEqual({
      steps: [{ answers: { coercion: "NO" }, secrets: { otp: "[REDACTED]", nested: [{ pin: "[REDACTED]" }] } }],
      note: "+91-XXXXXX3210",
    });
  });

  it("leaves masked values, numbers, booleans, null and plain text alone", () => {
    const body = {
      account: "XXXX-XXXX-4821",
      phone: "+91-XXXXXX3210",
      upi: "r***@okaxis",
      redacted: "[REDACTED]",
      amount_inr: 450,
      flag: false,
      nothing: null,
      action: "APPROVE",
      token: "3f9a.1700000900.0a1b2c",
      reason: "A large transfer to a new recipient.",
    };
    expect(redactObject(body)).toEqual(body);
  });

  it("does not treat short digit strings or dates as identifiers", () => {
    const body = { a: "12345678", b: "2026-11-23", c: "1930", d: "5555" };
    expect(redactObject(body)).toEqual(body);
  });

  it("masks a bare string and top-level arrays", () => {
    expect(redactObject("rahul@okaxis")).toBe("r***@okaxis");
    expect(redactObject(["9876543210", 7])).toEqual(["+91-XXXXXX3210", 7]);
  });

  it("returns a deep copy and leaves its input unchanged", () => {
    const input = { list: [{ otp: "1", account: "123456789012" }], text: "hello" };
    const snapshot = structuredClone(input);
    const out = redactObject(input);

    expect(input).toEqual(snapshot);
    expect(out).not.toBe(input);
    expect(out.list).not.toBe(input.list);
    expect(out.list[0]).not.toBe(input.list[0]);
  });

  it("is idempotent", () => {
    const once = redactObject({ upi: "rahul@okaxis", n: "123456789012", p: "9876543210", otp: "1" });
    expect(redactObject(once)).toEqual(once);
  });

  it("copies a key named __proto__ as data and keeps the prototype", () => {
    const input = JSON.parse('{"__proto__": {"otp": "1", "x": "9876543210"}, "ok": 1}') as Record<string, unknown>;
    const out = redactObject(input);

    expect(Object.getPrototypeOf(out)).toBe(Object.prototype);
    expect(Object.keys(out)).toEqual(["__proto__", "ok"]);
    expect(Object.getOwnPropertyDescriptor(out, "__proto__")?.value).toEqual({
      otp: "[REDACTED]",
      x: "+91-XXXXXX3210",
    });
    expect(({} as Record<string, unknown>).otp).toBeUndefined();
  });

  it("returns values that are not arrays or plain objects as they are", () => {
    const date = new Date(0);
    expect(redactObject(date)).toBe(date);
    expect(redactObject(undefined)).toBeUndefined();
    expect(redactObject(null)).toBeNull();
  });
});

describe("purity", () => {
  it("uses no console, storage or network", async () => {
    const consoleSpies = (["log", "info", "warn", "error", "debug"] as const).map((m) =>
      vi.spyOn(console, m).mockImplementation(() => undefined),
    );
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const fetchStub = installFetchStub();

    maskAccount("1234567890124821");
    maskPhone("9876543210");
    maskUpi("rahul@okaxis");
    redactSecret("482913");
    redactObject({ otp: "1", upi: "rahul@okaxis", list: ["9876543210"] });
    await hashIdentifier("abc");

    consoleSpies.forEach((spy) => expect(spy).not.toHaveBeenCalled());
    expect(setItem).not.toHaveBeenCalled();
    expect(fetchStub.calls).toHaveLength(0);
    expect(document.cookie).toBe("");
    vi.restoreAllMocks();
  });
});
