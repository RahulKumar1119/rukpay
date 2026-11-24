/**
 * The typed client for the RukPay backend. Every call goes through one private
 * `request` function, so the rules below hold for all of them:
 *
 * - The body is sent as JSON, after `redactObject` has run over it, so an OTP,
 *   PIN, password or raw identifier cannot leave the browser (Requirement 10.8).
 * - A request that cannot reach the server, that takes longer than 10 seconds,
 *   that gets a 5xx answer or that gets an unreadable body throws
 *   `NetworkError`. Any other non-2xx answer throws `ApiError`.
 * - A caller can cancel a request with an `AbortSignal`. A cancelled request
 *   rejects with an AbortError, which `isAbortError` recognizes, so it is never
 *   mistaken for a failure.
 * - Nothing here writes to the console. An error message holds the server's own
 *   words or a fixed phrase, and never the guardian token or an identifier
 *   (Requirement 10.9).
 */
import { redactObject } from "./privacyMasker";

// ---------------------------------------------------------------------------
// Types. They mirror the JSON of the backend's `vault` package.
// ---------------------------------------------------------------------------

export type VaultState =
  | "INSTANT_PASS"
  | "REALITY_CHECK"
  | "LOCKED_15M"
  | "GUARDIAN_APPROVED"
  | "VETOED"
  | "EXPIRED";

export interface Signals {
  amount_inr: number;
  beneficiary_age_hours: number;
  is_late_night: boolean;
  active_unknown_call: boolean;
  rapid_retry_count: number;
}

/** The five signals plus optional masked identifiers. There is no field for a raw identifier. */
export interface EvaluateRequest extends Signals {
  payer_account_masked?: string;
  beneficiary_masked?: string;
  upi_masked?: string;
  phone_masked?: string;
}

export interface VaultMetadata {
  amount_inr?: number;
  payer_account_masked?: string;
  beneficiary_masked?: string;
  upi_masked?: string;
  phone_masked?: string;
}

/**
 * One line of the score's explanation: a signal id, the whole points it added
 * (negative for a cap), and one calm sentence. The browser shows `reason` and
 * `points` as received and never shows `signal`.
 */
export interface ExplanationItem {
  signal: string;
  points: number;
  reason: string;
}
export interface VaultView {
  vault_id: string;
  state: VaultState;
  settled: boolean;
  risk_score: number;
  remaining_seconds: number;
  reconfirm_required: boolean;
  reason: string;
  metadata: VaultMetadata;
  /** Present only while the vault is LOCKED_15M. */
  guardian_token?: string;
  /** Present only once the vault is VETOED. */
  evidence_summary?: string;
  /**
   * Why the score is what it is. Absent when the server omits it, which is not
   * the same as an empty list: an empty list means nothing added any points.
   */
  explanation?: ExplanationItem[];
}

export type Answer = "YES" | "NO";

export interface Answers {
  coercion: Answer;
  secrecy: Answer;
  verification: Answer;
}

/** Either the three answers or a reconfirmation. A body with both, or with neither, does not compile. */
export type RealityCheckRequest =
  | { answers: Answers; reconfirm?: never }
  | { reconfirm: true; answers?: never };

export interface GuardianActionRequest {
  action: "APPROVE" | "VETO";
  token: string;
  confirmed: boolean;
}

export interface GuardianActionResult {
  status: "OK" | "CONFIRMATION_REQUIRED";
  vault: VaultView;
}

/** The body of every non-2xx answer. A 409 also carries the current View. */
export interface ErrorBody {
  error: { code: string; message: string; field?: string };
  vault?: VaultView;
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/** The server answered with a 4xx status and a readable error body. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly field?: string;
  /** The current View. Set for HTTP 409. */
  readonly vault?: VaultView;

  constructor(status: number, code: string, message: string, field?: string, vault?: VaultView) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.field = field;
    this.vault = vault;
  }
}

/** The server could not be reached, took too long, failed (5xx) or sent a body that cannot be read. */
export class NetworkError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "NetworkError";
  }
}

/** True for the error a request throws when its caller cancelled it. */
export function isAbortError(err: unknown): boolean {
  return typeof err === "object" && err !== null && (err as { name?: unknown }).name === "AbortError";
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const FALLBACK_BASE = "http://localhost:8080";

function resolveBase(raw: string | undefined): string {
  const trimmed = (raw ?? "").trim().replace(/\/+$/, "");
  return trimmed === "" ? FALLBACK_BASE : trimmed;
}

/** The backend origin, with no trailing slash. */
export const API_BASE: string = resolveBase(import.meta.env.VITE_API_BASE_URL);

/** A request that takes longer than this many milliseconds fails with a NetworkError. */
export const REQUEST_TIMEOUT_MS = 10_000;

// ---------------------------------------------------------------------------
// The request function
// ---------------------------------------------------------------------------

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isVaultView(value: unknown): value is VaultView {
  return isRecord(value) && typeof value.vault_id === "string" && value.vault_id !== "";
}

function tryParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/**
 * Builds the ApiError for a 4xx answer, or returns null when the body is not
 * a readable error. For a 409 the View comes from a top-level `vault` property
 * or, failing that, from a body that is itself a View.
 */
function toApiError(status: number, body: unknown): ApiError | null {
  if (!isRecord(body)) return null;

  let view: VaultView | undefined;
  if (status === 409) {
    if (isVaultView(body.vault)) view = body.vault;
    else if (isVaultView(body)) view = body;
  }

  const detail = body.error;
  if (isRecord(detail) && typeof detail.code === "string" && typeof detail.message === "string") {
    const field = typeof detail.field === "string" ? detail.field : undefined;
    return new ApiError(status, detail.code, detail.message, field, view);
  }
  if (view) return new ApiError(status, "state_conflict", `HTTP ${status}`, undefined, view);
  return null;
}

async function request<T>(
  method: "GET" | "POST",
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  // One controller joins the caller's signal and the timeout. A timer, not
  // AbortSignal.timeout, so tests can move it with fake timers.
  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, REQUEST_TIMEOUT_MS);
  const forwardAbort = (): void => controller.abort();
  if (signal?.aborted) controller.abort();
  else signal?.addEventListener("abort", forwardAbort, { once: true });

  const init: RequestInit = { method, signal: controller.signal };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(redactObject(body));
  }

  try {
    const response = await fetch(`${API_BASE}${path}`, init);
    const text = await response.text();

    if (response.ok) {
      const parsed = tryParse(text);
      if (parsed === undefined) throw new NetworkError("The server sent a body that could not be read.");
      return parsed as T;
    }
    if (response.status >= 500) throw new NetworkError(`The server had a problem (HTTP ${response.status}).`);

    const apiError = toApiError(response.status, tryParse(text));
    if (apiError) throw apiError;
    throw new NetworkError("The server sent an error that could not be read.");
  } catch (err) {
    if (err instanceof ApiError || err instanceof NetworkError) throw err;
    if (signal?.aborted) throw new DOMException("The request was cancelled.", "AbortError");
    throw new NetworkError(timedOut ? "The request timed out." : "The server could not be reached.");
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", forwardAbort);
  }
}

// ---------------------------------------------------------------------------
// The calls
// ---------------------------------------------------------------------------

const vaultPath = (id: string): string => `/api/v1/vault/${encodeURIComponent(id)}`;

/** Scores a payment and creates its vault. */
export function evaluate(req: EvaluateRequest, signal?: AbortSignal): Promise<VaultView> {
  return request<VaultView>("POST", "/api/v1/transactions/evaluate", req, signal);
}

/** Reads the current View of a vault. */
export function getVault(id: string, signal?: AbortSignal): Promise<VaultView> {
  return request<VaultView>("GET", vaultPath(id), undefined, signal);
}

/** Sends the three reality-check answers. */
export function submitAnswers(id: string, answers: Answers, signal?: AbortSignal): Promise<VaultView> {
  const body: RealityCheckRequest = { answers };
  return request<VaultView>("POST", `${vaultPath(id)}/reality-check`, body, signal);
}

/** Confirms a payment whose 15-minute pause has ended. */
export function reconfirm(id: string, signal?: AbortSignal): Promise<VaultView> {
  const body: RealityCheckRequest = { reconfirm: true };
  return request<VaultView>("POST", `${vaultPath(id)}/reality-check`, body, signal);
}

/** Approves or vetoes a locked vault. */
export function guardianAction(
  id: string,
  action: GuardianActionRequest,
  signal?: AbortSignal,
): Promise<GuardianActionResult> {
  return request<GuardianActionResult>("POST", `${vaultPath(id)}/guardian-action`, action, signal);
}

/** Moves the lock deadline to a few seconds from now. The server answers 404 when demo mode is off. */
export function demoFastForward(id: string, signal?: AbortSignal): Promise<VaultView> {
  return request<VaultView>("POST", `${vaultPath(id)}/demo-fast-forward`, undefined, signal);
}

/** The URL of the Server-Sent Events stream for one vault. */
export function eventStreamUrl(id: string): string {
  return `${API_BASE}/api/v1/events/stream?vault_id=${encodeURIComponent(id)}`;
}
