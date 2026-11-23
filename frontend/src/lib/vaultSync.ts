/**
 * Keeps both phones in step with one vault.
 *
 * `VaultSync` opens the event stream and passes every View to its owner. If the
 * stream fails or closes, it polls `GET /api/v1/vault/{id}` instead, one request
 * at a time, two seconds after the last one finished. It stops by itself when it
 * sees a View that cannot change any more, and `dispose()` stops it at any time.
 *
 * It is a plain class with no React in it, so the timers, the stream and the
 * pending request all live in one object with one way to shut them down.
 */
import { eventStreamUrl, getVault, isAbortError, type VaultView } from "./api";
import { isTerminal } from "./screens";

/** Time between the end of one poll and the start of the next, in milliseconds. */
export const POLL_INTERVAL_MS = 2000;

export interface ViewMeta {
  source: "stream" | "poll";
  /**
   * For a poll: what `getSeq()` returned when the request was sent. The reducer
   * uses it to drop a poll result that an action response has overtaken.
   */
  startedAtSeq?: number;
}

export interface SyncHandlers {
  /** Every View for this vault that arrives, in the order it arrives. */
  onView(view: VaultView, meta: ViewMeta): void;
  /** True when a poll fails after working, false when the next one works again. */
  onUnreachable(on: boolean): void;
  /** The owner's arrival counter, read when a poll is sent. */
  getSeq?(): number;
}

type TimerHandle = ReturnType<typeof setTimeout>;

export interface SyncDeps {
  EventSourceCtor?: typeof EventSource;
  setTimeoutFn?: (callback: () => void, ms: number) => TimerHandle;
  clearTimeoutFn?: (handle: TimerHandle) => void;
}

function parseView(data: unknown): VaultView | null {
  if (typeof data !== "string") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return null;
  const id = (parsed as { vault_id?: unknown }).vault_id;
  return typeof id === "string" && id !== "" ? (parsed as VaultView) : null;
}

export class VaultSync {
  private readonly vaultId: string;
  private readonly handlers: SyncHandlers;
  private readonly deps: SyncDeps;

  private stream: EventSource | null = null;
  private timer: TimerHandle | null = null;
  private inflight: AbortController | null = null;
  private started = false;
  private polling = false;
  private closed = false;
  private unreachable = false;

  constructor(vaultId: string, handlers: SyncHandlers, deps: SyncDeps = {}) {
    this.vaultId = vaultId;
    this.handlers = handlers;
    this.deps = deps;
  }

  /** Opens the event stream. It falls back to polling when there is no EventSource or it cannot open. */
  start(): void {
    if (this.started || this.closed) return;
    this.started = true;

    const Ctor = this.deps.EventSourceCtor ?? globalThis.EventSource;
    if (typeof Ctor !== "function") {
      this.startPolling();
      return;
    }
    try {
      const stream = new Ctor(eventStreamUrl(this.vaultId));
      stream.addEventListener("message", this.handleMessage);
      stream.addEventListener("error", this.handleError);
      this.stream = stream;
    } catch {
      this.startPolling();
    }
  }

  /** Closes the stream, clears the timer and cancels a request in flight. Safe to call again. */
  dispose(): void {
    this.closed = true;
    this.closeStream();
    if (this.timer !== null) {
      this.clearTimer(this.timer);
      this.timer = null;
    }
    this.inflight?.abort();
    this.inflight = null;
  }

  private readonly handleMessage = (event: Event): void => {
    const view = parseView((event as MessageEvent).data);
    if (view) this.accept(view, { source: "stream" });
  };

  private readonly handleError = (): void => {
    this.closeStream();
    this.startPolling();
  };

  private closeStream(): void {
    const stream = this.stream;
    if (!stream) return;
    this.stream = null;
    stream.removeEventListener("message", this.handleMessage);
    stream.removeEventListener("error", this.handleError);
    stream.close();
  }

  /** Hands a View to the owner, unless it belongs to another vault. A final View stops the sync. */
  private accept(view: VaultView, meta: ViewMeta): void {
    if (this.closed || view.vault_id !== this.vaultId) return;
    this.handlers.onView(view, meta);
    if (isTerminal(view)) this.dispose();
  }

  private startPolling(): void {
    if (this.closed || this.polling) return;
    this.polling = true;
    void this.poll();
  }

  private async poll(): Promise<void> {
    this.timer = null;
    if (this.closed) return;

    const controller = new AbortController();
    this.inflight = controller;
    const startedAtSeq = this.handlers.getSeq?.();

    let view: VaultView | null = null;
    let failed = false;
    try {
      view = await getVault(this.vaultId, controller.signal);
    } catch (err) {
      if (this.closed || isAbortError(err)) return;
      failed = true;
    } finally {
      if (this.inflight === controller) this.inflight = null;
    }
    if (this.closed) return;

    if (failed) {
      this.setUnreachable(true);
    } else if (view) {
      this.setUnreachable(false);
      this.accept(view, { source: "poll", startedAtSeq });
    }
    if (!this.closed) this.scheduleNext();
  }

  private setUnreachable(on: boolean): void {
    if (this.unreachable === on) return;
    this.unreachable = on;
    this.handlers.onUnreachable(on);
  }

  private scheduleNext(): void {
    const setTimer = this.deps.setTimeoutFn ?? ((callback: () => void, ms: number) => globalThis.setTimeout(callback, ms));
    this.timer = setTimer(() => void this.poll(), POLL_INTERVAL_MS);
  }

  private clearTimer(handle: TimerHandle): void {
    const clear = this.deps.clearTimeoutFn ?? ((h: TimerHandle) => globalThis.clearTimeout(h));
    clear(handle);
  }
}
