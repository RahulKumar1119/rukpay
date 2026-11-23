/**
 * Test doubles for `fetch` and `EventSource`, plus VaultView fixtures.
 *
 * This file is the only place in `src/` that fakes the backend. Production code
 * never imports it, and no mock data source exists outside `src/test/`.
 *
 * The fixture type below mirrors the VaultView in design.md on purpose. Once
 * `lib/api.ts` exists (task 5.1) this file can import that type instead.
 */
import { vi } from "vitest";

// ---------------------------------------------------------------------------
// VaultView fixtures
// ---------------------------------------------------------------------------

export type FixtureVaultState =
  | "INSTANT_PASS"
  | "REALITY_CHECK"
  | "LOCKED_15M"
  | "GUARDIAN_APPROVED"
  | "VETOED"
  | "EXPIRED";

export interface FixtureMetadata {
  amount_inr?: number;
  payer_account_masked?: string;
  beneficiary_masked?: string;
  upi_masked?: string;
  phone_masked?: string;
}

export interface FixtureVaultView {
  vault_id: string;
  state: FixtureVaultState;
  settled: boolean;
  risk_score: number;
  remaining_seconds: number;
  reconfirm_required: boolean;
  reason: string;
  metadata: FixtureMetadata;
  guardian_token?: string;
  evidence_summary?: string;
}

export const ALL_FIXTURE_STATES: readonly FixtureVaultState[] = [
  "INSTANT_PASS",
  "REALITY_CHECK",
  "LOCKED_15M",
  "GUARDIAN_APPROVED",
  "VETOED",
  "EXPIRED",
];

const BASE_METADATA: FixtureMetadata = {
  amount_inr: 85000,
  payer_account_masked: "XXXX-XXXX-4821",
  beneficiary_masked: "XXXX-XXXX-9034",
};

/**
 * Builds a VaultView the way the backend shapes it for the given state. The
 * guardian token appears only for LOCKED_15M and the evidence only for VETOED.
 * `overrides` wins over every default, so a test can build an off-contract view
 * on purpose.
 */
export function makeView(
  state: FixtureVaultState,
  overrides: Partial<FixtureVaultView> = {},
): FixtureVaultView {
  const base = {
    vault_id: "vault-test-1",
    state,
    settled: false,
    risk_score: 0,
    remaining_seconds: 0,
    reconfirm_required: false,
    reason: "",
    metadata: { ...BASE_METADATA },
  } satisfies FixtureVaultView;

  let view: FixtureVaultView;
  switch (state) {
    case "INSTANT_PASS":
      view = {
        ...base,
        settled: true,
        risk_score: 10,
        reason: "This payment looks like your usual one.",
        metadata: { amount_inr: 450, beneficiary_masked: "r***@okaxis" },
      };
      break;
    case "REALITY_CHECK":
      view = {
        ...base,
        risk_score: 55,
        reason: "A few quick questions first.",
        metadata: { amount_inr: 18000, beneficiary_masked: "XXXX-XXXX-7712" },
      };
      break;
    case "LOCKED_15M":
      view = {
        ...base,
        risk_score: 90,
        remaining_seconds: 900,
        reason: "Large transfer to a new recipient while on a call.",
        guardian_token: "fixture-guardian-token",
      };
      break;
    case "GUARDIAN_APPROVED":
      view = { ...base, settled: true, risk_score: 90, reason: "A family member approved this payment." };
      break;
    case "VETOED":
      view = {
        ...base,
        risk_score: 90,
        reason: "This payment was cancelled.",
        evidence_summary: "Evidence summary for cybercrime.gov.in and helpline 1930.",
      };
      break;
    case "EXPIRED":
      view = { ...base, risk_score: 90, reconfirm_required: true, reason: "The 15-minute pause is over." };
      break;
  }
  return { ...view, ...overrides };
}

/** One fixture per state, keyed by state name. */
export function allStateViews(): Record<FixtureVaultState, FixtureVaultView> {
  return {
    INSTANT_PASS: makeView("INSTANT_PASS"),
    REALITY_CHECK: makeView("REALITY_CHECK"),
    LOCKED_15M: makeView("LOCKED_15M"),
    GUARDIAN_APPROVED: makeView("GUARDIAN_APPROVED"),
    VETOED: makeView("VETOED"),
    EXPIRED: makeView("EXPIRED"),
  };
}

// ---------------------------------------------------------------------------
// fetch stub
// ---------------------------------------------------------------------------

export interface RecordedCall {
  method: string;
  url: string;
  /** The request body parsed as JSON, or the raw string if it is not JSON. */
  body: unknown;
  /** Header names are lowercase. */
  headers: Record<string, string>;
}

type QueuedResponse =
  | { kind: "text"; status: number; text: string }
  | { kind: "reject"; error: unknown }
  | { kind: "hang" };

export interface FetchStub {
  /** Every call so far, in order. */
  readonly calls: RecordedCall[];
  /** How many queued responses have not been used yet. */
  readonly pending: () => number;
  /** Queue a response whose body is `body` encoded as JSON. */
  respond(status: number, body?: unknown): FetchStub;
  /** Queue a response with a raw, possibly unparseable, text body. */
  respondRaw(status: number, text: string): FetchStub;
  /** Queue a network failure. The default is the TypeError a browser throws. */
  reject(error?: unknown): FetchStub;
  /** Queue a request that never answers and rejects only when it is aborted. */
  hang(): FetchStub;
  readonly fetch: typeof fetch;
}

function abortError(signal: AbortSignal): unknown {
  if (signal.reason !== undefined) return signal.reason;
  return new DOMException("The operation was aborted.", "AbortError");
}

function parseBody(body: BodyInit | null | undefined): unknown {
  if (typeof body !== "string") return undefined;
  try {
    return JSON.parse(body);
  } catch {
    return body;
  }
}

function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/** Builds a stub without installing it. Most tests call installFetchStub. */
export function createFetchStub(): FetchStub {
  const calls: RecordedCall[] = [];
  const queue: QueuedResponse[] = [];

  const stub: FetchStub = {
    calls,
    pending: () => queue.length,
    respond(status, body) {
      queue.push({ kind: "text", status, text: body === undefined ? "" : JSON.stringify(body) });
      return stub;
    },
    respondRaw(status, text) {
      queue.push({ kind: "text", status, text });
      return stub;
    },
    reject(error) {
      queue.push({ kind: "reject", error: error ?? new TypeError("Failed to fetch") });
      return stub;
    },
    hang() {
      queue.push({ kind: "hang" });
      return stub;
    },
    fetch: (input, init) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const url = requestUrl(input);
      calls.push({
        method,
        url,
        body: parseBody(init?.body),
        headers: Object.fromEntries(new Headers(init?.headers).entries()),
      });

      const next = queue.shift();
      if (!next) {
        return Promise.reject(new TypeError(`fetch stub: no response queued for ${method} ${url}`));
      }

      const signal = init?.signal ?? undefined;
      if (signal?.aborted) return Promise.reject(abortError(signal));

      switch (next.kind) {
        case "reject":
          return Promise.reject(next.error);
        case "hang":
          return new Promise<Response>((_resolve, reject) => {
            signal?.addEventListener("abort", () => reject(abortError(signal)), { once: true });
          });
        case "text":
          return Promise.resolve(
            new Response(next.text === "" ? null : next.text, {
              status: next.status,
              headers: { "Content-Type": "application/json" },
            }),
          );
      }
    },
  };
  return stub;
}

/** Replaces `globalThis.fetch` with a fresh stub. Undone by restoreStubs. */
export function installFetchStub(): FetchStub {
  const stub = createFetchStub();
  vi.stubGlobal("fetch", stub.fetch);
  return stub;
}

// ---------------------------------------------------------------------------
// EventSource stub
// ---------------------------------------------------------------------------

type Listener = (event: Event) => void;

/**
 * A stand-in for the browser EventSource. Tests drive it with `emit` and
 * `fail`. `closed` becomes true only when the code under test calls `close()`,
 * so a test can tell "the stream died" (readyState CLOSED) from "the app shut
 * it down" (closed).
 */
export class StubEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  /** Every instance created since the last install, oldest first. */
  static instances: StubEventSource[] = [];

  static latest(): StubEventSource {
    const last = StubEventSource.instances[StubEventSource.instances.length - 1];
    if (!last) throw new Error("No EventSource has been created yet.");
    return last;
  }

  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;

  readonly url: string;
  readyState: number = StubEventSource.CONNECTING;
  closed = false;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;

  private readonly listeners = new Map<string, Set<Listener>>();

  constructor(url: string | URL) {
    this.url = String(url);
    StubEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: Listener): void {
    const set = this.listeners.get(type) ?? new Set<Listener>();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: Listener): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closed = true;
    this.readyState = StubEventSource.CLOSED;
  }

  /**
   * Delivers one `message` event. A string goes out as it is, which lets a test
   * send text that is not valid JSON. Anything else is sent as JSON.
   */
  emit(data: unknown): void {
    if (this.closed) return;
    const payload = typeof data === "string" ? data : JSON.stringify(data);
    this.readyState = StubEventSource.OPEN;
    this.dispatch(new MessageEvent("message", { data: payload }));
  }

  /** Delivers an `error` event and leaves the stream in the CLOSED state. */
  fail(): void {
    if (this.closed) return;
    this.readyState = StubEventSource.CLOSED;
    this.dispatch(new Event("error"));
  }

  private dispatch(event: Event): void {
    if (event.type === "message") this.onmessage?.(event as MessageEvent);
    if (event.type === "error") this.onerror?.(event);
    if (event.type === "open") this.onopen?.(event);
    this.listeners.get(event.type)?.forEach((listener) => listener(event));
  }
}

/** Replaces `globalThis.EventSource` with the stub class and clears its history. */
export function installEventSourceStub(): typeof StubEventSource {
  StubEventSource.instances = [];
  vi.stubGlobal("EventSource", StubEventSource);
  return StubEventSource;
}

/** Puts back the real globals. setup.ts runs this after every test. */
export function restoreStubs(): void {
  vi.unstubAllGlobals();
  StubEventSource.instances = [];
}
