/**
 * The typed client for the RukPay backend. Every call goes through one private
 * `request` function, so the rules below hold for all of them:
 *
 * - The body is sent as JSON, after `redactObject` has run over it, so an OTP,
 *   PIN, password or raw identifier cannot leave the browser (Requirement 10.8).
 * - A request that cannot reach the server, that takes longer than 10 seconds,
 *   that gets a 5xx answer or that gets an unreadable body throws
 *   `NetworkError`. Any other non-2xx answer throws `ApiError`.
 * - `GET` and `DELETE` send no body. `POST` and `PATCH` send the JSON of the body
 *   given to them, after the same redaction.
 * - A 409 answer carries the current vault, or the current saved recipient when
 *   the body holds a complete one, on the ApiError.
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

/**
 * The five signals plus optional masked identifiers. There is no field for a raw identifier.
 * `beneficiary_id` names a saved recipient. A request that names one sends none of the masked fields.
 */
export interface EvaluateRequest extends Signals {
  payer_account_masked?: string;
  beneficiary_masked?: string;
  upi_masked?: string;
  phone_masked?: string;
  beneficiary_id?: string;
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

// ---------------------------------------------------------------------------
// Saved recipients. They mirror the JSON of the backend's `beneficiary` package.
// No type here has a field for a raw identifier or for an OTP, PIN, password,
// passcode or CVV (Requirement 22.5). The recipient is held only as a masked
// identifier.
// ---------------------------------------------------------------------------

export type TrustLevel = "NEW" | "TRUSTED";
export type ProtectionLevel = "EXTRA_CARE" | "STANDARD";

/** Why RukPay is careful with a recipient. The server sends them in this order. */
export type RiskIndicator =
  | "ADDED_RECENTLY"
  | "ADDED_UNDER_PRESSURE"
  | "NOT_VERIFIED"
  | "WAITING_PERIOD"
  | "FIRST_PAYMENT_LARGE"
  | "EXTRA_CARE_ON";

export type HistoryKind =
  | "ADDED"
  | "VERIFIED"
  | "VERIFICATION_NOT_PASSED"
  | "TRUSTED"
  | "TRUST_REMOVED"
  | "PROTECTION_RAISED"
  | "PROTECTION_LOWERED"
  | "RENAMED"
  | "PAYMENT";

/** One thing that happened to a recipient. Only a PAYMENT carries an amount. */
export interface HistoryEntry {
  at: number;
  kind: HistoryKind;
  amount_inr?: number;
}

/** A saved recipient as the server shows it. Times are epoch seconds. */
export interface BeneficiaryView {
  beneficiary_id: string;
  label: string;
  identifier_masked: string;
  added_at: number;
  age_hours: number;
  trust_level: TrustLevel;
  protection_level: ProtectionLevel;
  pressure_flag: boolean;
  trust_eligible_at: number;
  /** Never missing. Empty when no indicator applies. */
  indicators: RiskIndicator[];
  /** Present only once the recipient has passed a check. */
  verified_at?: number;
  /** Present only on the answer for one recipient, newest first. */
  history?: HistoryEntry[];
}

export interface AddBeneficiaryRequest {
  label: string;
  identifier_masked: string;
  is_late_night?: boolean;
  active_unknown_call?: boolean;
}

/** At least one of the first three. The two flags say whether the person is under pressure right now. */
export interface PatchBeneficiaryRequest {
  label?: string;
  trust_level?: TrustLevel;
  protection_level?: ProtectionLevel;
  is_late_night?: boolean;
  active_unknown_call?: boolean;
}

/** The three answers to the three questions about a recipient. */
export interface VerificationAnswers {
  coercion: Answer;
  acquaintance: Answer;
  secrecy: Answer;
}

/** `message` is present for NOT_PASSED. */
export interface VerificationResult {
  outcome: "PASSED" | "NOT_PASSED";
  message?: string;
  beneficiary: BeneficiaryView;
}

export interface RemoveResult {
  removed: true;
}

/** The body of every non-2xx answer. A 409 also carries the current View. */
export interface ErrorBody {
  error: { code: string; message: string; field?: string };
  vault?: VaultView;
  /** The current saved recipient. Present on a 409 `state_conflict` of the recipient routes. */
  beneficiary?: BeneficiaryView;
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
  /** The current saved recipient. Set for an HTTP 409 whose body holds a valid one. */
  readonly beneficiary?: BeneficiaryView;

  constructor(
    status: number,
    code: string,
    message: string,
    field?: string,
    vault?: VaultView,
    beneficiary?: BeneficiaryView,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.field = field;
    this.vault = vault;
    this.beneficiary = beneficiary;
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

// The words the server may use. A View that holds any other word is not read as
// a View, so the screens never meet a code they have no sentence for.
const TRUST_LEVELS: readonly string[] = ["NEW", "TRUSTED"] satisfies readonly TrustLevel[];
const PROTECTION_LEVELS: readonly string[] = ["EXTRA_CARE", "STANDARD"] satisfies readonly ProtectionLevel[];
const RISK_INDICATORS: readonly string[] = [
  "ADDED_RECENTLY",
  "ADDED_UNDER_PRESSURE",
  "NOT_VERIFIED",
  "WAITING_PERIOD",
  "FIRST_PAYMENT_LARGE",
  "EXTRA_CARE_ON",
] satisfies readonly RiskIndicator[];
const HISTORY_KINDS: readonly string[] = [
  "ADDED",
  "VERIFIED",
  "VERIFICATION_NOT_PASSED",
  "TRUSTED",
  "TRUST_REMOVED",
  "PROTECTION_RAISED",
  "PROTECTION_LOWERED",
  "RENAMED",
  "PAYMENT",
] satisfies readonly HistoryKind[];

const isWholeNumber = (value: unknown): value is number => typeof value === "number" && Number.isInteger(value);
const isOneOf = (words: readonly string[], value: unknown): boolean => typeof value === "string" && words.includes(value);

function isHistoryEntry(value: unknown): value is HistoryEntry {
  return (
    isRecord(value) &&
    isWholeNumber(value.at) &&
    isOneOf(HISTORY_KINDS, value.kind) &&
    (value.amount_inr === undefined || isWholeNumber(value.amount_inr))
  );
}

/**
 * True only for a complete saved recipient: every key of `BeneficiaryView` with
 * the right type, known words for the levels and the indicators, `verified_at`
 * and `history` either absent or well formed. A 409 body whose recipient fails
 * this check still gives an ApiError, with no recipient on it.
 */
function isBeneficiaryView(value: unknown): value is BeneficiaryView {
  return (
    isRecord(value) &&
    typeof value.beneficiary_id === "string" &&
    value.beneficiary_id !== "" &&
    typeof value.label === "string" &&
    typeof value.identifier_masked === "string" &&
    isWholeNumber(value.added_at) &&
    isWholeNumber(value.age_hours) &&
    isOneOf(TRUST_LEVELS, value.trust_level) &&
    isOneOf(PROTECTION_LEVELS, value.protection_level) &&
    typeof value.pressure_flag === "boolean" &&
    isWholeNumber(value.trust_eligible_at) &&
    Array.isArray(value.indicators) &&
    value.indicators.every((code: unknown) => isOneOf(RISK_INDICATORS, code)) &&
    (value.verified_at === undefined || isWholeNumber(value.verified_at)) &&
    (value.history === undefined || (Array.isArray(value.history) && value.history.every(isHistoryEntry)))
  );
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
 * or, failing that, from a body that is itself a View, and the saved recipient
 * comes from a top-level `beneficiary` property when it is a complete one.
 */
function toApiError(status: number, body: unknown): ApiError | null {
  if (!isRecord(body)) return null;

  let view: VaultView | undefined;
  let recipient: BeneficiaryView | undefined;
  if (status === 409) {
    if (isVaultView(body.vault)) view = body.vault;
    else if (isVaultView(body)) view = body;
    if (isBeneficiaryView(body.beneficiary)) recipient = body.beneficiary;
  }

  const detail = body.error;
  if (isRecord(detail) && typeof detail.code === "string" && typeof detail.message === "string") {
    const field = typeof detail.field === "string" ? detail.field : undefined;
    return new ApiError(status, detail.code, detail.message, field, view, recipient);
  }
  if (view) return new ApiError(status, "state_conflict", `HTTP ${status}`, undefined, view);
  return null;
}

/**
 * `GET` and `DELETE` take no body, and the types say so. `POST` and `PATCH` take
 * an optional one. A body is run through `redactObject` before it is sent.
 */
function request<T>(method: "GET" | "DELETE", path: string, body?: undefined, signal?: AbortSignal): Promise<T>;
function request<T>(method: "POST" | "PATCH", path: string, body?: unknown, signal?: AbortSignal): Promise<T>;
async function request<T>(
  method: "GET" | "POST" | "PATCH" | "DELETE",
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

// ---------------------------------------------------------------------------
// Saved recipients
// ---------------------------------------------------------------------------

const beneficiaryPath = (id: string): string => `/api/v1/beneficiaries/${encodeURIComponent(id)}`;

/** Lists the saved recipients, earliest first, without their history. */
export function listBeneficiaries(signal?: AbortSignal): Promise<BeneficiaryView[]> {
  return request<BeneficiaryView[]>("GET", "/api/v1/beneficiaries", undefined, signal);
}

/** Saves a recipient. `identifier_masked` must already be masked. The server answers 201 with the new View. */
export function addBeneficiary(body: AddBeneficiaryRequest, signal?: AbortSignal): Promise<BeneficiaryView> {
  return request<BeneficiaryView>("POST", "/api/v1/beneficiaries", body, signal);
}

/** Reads one saved recipient, with its history, newest first. */
export function getBeneficiary(id: string, signal?: AbortSignal): Promise<BeneficiaryView> {
  return request<BeneficiaryView>("GET", beneficiaryPath(id), undefined, signal);
}

/** Renames a recipient or changes its trust or care. A refusal is an ApiError, and a lost race carries the current View. */
export function patchBeneficiary(
  id: string,
  body: PatchBeneficiaryRequest,
  signal?: AbortSignal,
): Promise<BeneficiaryView> {
  return request<BeneficiaryView>("PATCH", beneficiaryPath(id), body, signal);
}

/** Removes a recipient and its history. */
export function deleteBeneficiary(id: string, signal?: AbortSignal): Promise<RemoveResult> {
  return request<RemoveResult>("DELETE", beneficiaryPath(id), undefined, signal);
}

/** Sends the three answers about a recipient. The body is `{ answers }` and nothing else. */
export function verifyBeneficiary(
  id: string,
  answers: VerificationAnswers,
  signal?: AbortSignal,
): Promise<VerificationResult> {
  return request<VerificationResult>("POST", `${beneficiaryPath(id)}/verification`, { answers }, signal);
}

/** The URL of the Server-Sent Events stream for one vault. */
export function eventStreamUrl(id: string): string {
  return `${API_BASE}/api/v1/events/stream?vault_id=${encodeURIComponent(id)}`;
}
