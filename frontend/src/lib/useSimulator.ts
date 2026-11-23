/**
 * The one place that talks to the backend on behalf of the screens.
 *
 * `createSimulator` holds the logic with no React in it: it calls the API, keeps
 * one `VaultSync`, cancels what is in flight, and turns every outcome into an
 * action for the reducer. `useSimulator` binds it to React state.
 *
 * Failures are shown calmly and never thrown at the screens:
 * - the server cannot be reached: the unreachable marker, with a retry that
 *   repeats the same call;
 * - 409: the View in the answer is applied, and a notice says someone already acted;
 * - 403 on a guardian action: the approval link is no longer valid;
 * - 404 on fast-forward: the demo button is hidden for the session;
 * - 400: the server's own calm message.
 *
 * Nothing here writes to the console, storage, cookies or the URL.
 */
import { useEffect, useRef, useState } from "react";

import {
  ApiError,
  demoFastForward,
  evaluate,
  getVault,
  guardianAction,
  isAbortError,
  reconfirm as reconfirmPayment,
  submitAnswers as sendAnswers,
  type Answers,
  type VaultView,
} from "./api";
import { COPY } from "./copy";
import type { Preset } from "./presets";
import { isTerminal } from "./screens";
import { initialState, reduce, type Notice, type SimAction, type SimState } from "./simulatorReducer";
import { VaultSync } from "./vaultSync";

export interface Simulator {
  state: SimState;
  /** Discards the current vault, then scores the preset's payment. */
  startPreset(preset: Preset): Promise<void>;
  /** Sends the three reality-check answers. */
  submitAnswers(answers: Answers): Promise<void>;
  /** The payer's final confirmation after the pause. */
  reconfirm(): Promise<void>;
  /** The payer chooses "Cancel and keep my money". Sends nothing. */
  cancelReconfirm(): void;
  /** The payer's "Block & Report (1930)". */
  veto(): Promise<void>;
  /** The guardian's approval. `confirmed` is true from the second confirmation step. */
  guardianApprove(confirmed: boolean): Promise<void>;
  /** The guardian's "Block & Report Scam (1930)". */
  guardianVeto(): Promise<void>;
  /** The demo shortcut that skips the wait. */
  fastForward(): Promise<void>;
  /** Reads the current View again. */
  refresh(): Promise<void>;
  /** Repeats the request that failed, when the server could not be reached. */
  retry(): void;
}

/** The commands, without the state. Their identities never change. */
export type SimulatorCommands = Omit<Simulator, "state">;

export interface SimulatorCore {
  commands: SimulatorCommands;
  /** Stops the sync and cancels every request. The core can be used again after it. */
  dispose(): void;
}

interface ErrorContext {
  /** The call is a guardian action, so a 403 means the approval link is no longer valid. */
  guardian?: boolean;
  /** The call is the demo fast-forward, so a 404 means demo mode is off. */
  fastForward?: boolean;
}

const notice = (kind: Notice["kind"], message: string): SimAction => ({ type: "NOTICE", notice: { kind, message } });

export function createSimulator(io: {
  getState: () => SimState;
  dispatch: (action: SimAction) => void;
}): SimulatorCore {
  const { getState, dispatch } = io;

  // Bumped whenever a scenario starts or the core is disposed. An answer that
  // belongs to an older number is dropped.
  let generation = 0;
  let sync: VaultSync | null = null;
  const inflight = new Set<AbortController>();

  const stopSync = (): void => {
    sync?.dispose();
    sync = null;
  };

  const abortAll = (): void => {
    inflight.forEach((controller) => controller.abort());
    inflight.clear();
  };

  const startSync = (vaultId: string): void => {
    const mine = new VaultSync(vaultId, {
      onView: (view, meta) => {
        if (sync !== mine) return;
        dispatch({ type: "VIEW", view, source: meta.source, startedAtSeq: meta.startedAtSeq });
      },
      onUnreachable: (on) => {
        if (sync !== mine) return;
        dispatch(
          on
            ? { type: "UNREACHABLE", retry: () => void refresh(), source: "poll" }
            : { type: "UNREACHABLE", retry: null, source: "poll" },
        );
      },
      getSeq: () => getState().requestSeq,
    });
    sync = mine;
    mine.start();
  };

  /** Shows a View that an action returned. A final View ends the live updates. */
  const applyView = (view: VaultView): void => {
    dispatch({ type: "VIEW", view, source: "action" });
    if (isTerminal(view)) stopSync();
  };

  const handleApiError = (err: ApiError, retry: () => Promise<void>, ctx: ErrorContext): void => {
    if (err.status === 409) {
      if (err.vault) applyView(err.vault);
      dispatch(notice("ALREADY_ACTED", COPY.notices.ALREADY_ACTED));
    } else if (err.status === 403 && ctx.guardian) {
      dispatch(notice("LINK_INVALID", COPY.notices.LINK_INVALID));
    } else if (err.status === 404 && ctx.fastForward) {
      dispatch({ type: "FAST_FORWARD_UNAVAILABLE" });
    } else if (err.status === 404) {
      // The payment is gone from the server. Trying again, or choosing a preset, is all that is left.
      dispatch({ type: "UNREACHABLE", retry: () => void retry(), source: "action" });
    } else {
      dispatch(notice("BAD_REQUEST", err.message || COPY.notices.BAD_REQUEST));
    }
  };

  /**
   * Runs one request for the person. It cancels with the rest on a new
   * scenario, ignores an answer that arrives too late, and maps each failure to
   * what the screen shows.
   */
  async function execute<T>(
    call: (signal: AbortSignal) => Promise<T>,
    onResult: (result: T) => void,
    retry: () => Promise<void>,
    ctx: ErrorContext = {},
  ): Promise<void> {
    const gen = generation;
    const controller = new AbortController();
    inflight.add(controller);
    dispatch({ type: "NOTICE", notice: null });
    try {
      const result = await call(controller.signal);
      if (gen !== generation) return;
      dispatch({ type: "UNREACHABLE", retry: null });
      onResult(result);
    } catch (err) {
      if (gen !== generation || isAbortError(err)) return;
      if (err instanceof ApiError) handleApiError(err, retry, ctx);
      else dispatch({ type: "UNREACHABLE", retry: () => void retry(), source: "action" });
    } finally {
      inflight.delete(controller);
    }
  }

  const vaultId = (): string | null => getState().vault?.vault_id ?? null;

  async function startPreset(preset: Preset): Promise<void> {
    stopSync();
    abortAll();
    generation += 1;
    const gen = generation;
    dispatch({ type: "START", preset });

    const controller = new AbortController();
    inflight.add(controller);
    try {
      const view = await evaluate(preset.request, controller.signal);
      if (gen !== generation) return;
      dispatch({ type: "EVALUATED", view });
      if (!isTerminal(view)) startSync(view.vault_id);
    } catch (err) {
      if (gen !== generation || isAbortError(err)) return;
      dispatch({ type: "BUSY", busy: false });
      if (err instanceof ApiError) {
        dispatch(notice("BAD_REQUEST", err.message || COPY.notices.BAD_REQUEST));
      } else {
        dispatch({ type: "UNREACHABLE", retry: () => void startPreset(preset), source: "action" });
      }
    } finally {
      inflight.delete(controller);
    }
  }

  async function submitAnswers(answers: Answers): Promise<void> {
    const id = vaultId();
    if (!id) return;
    await execute((signal) => sendAnswers(id, answers, signal), applyView, () => submitAnswers(answers));
  }

  async function reconfirm(): Promise<void> {
    const id = vaultId();
    if (!id) return;
    await execute((signal) => reconfirmPayment(id, signal), applyView, reconfirm);
  }

  async function refresh(): Promise<void> {
    const id = vaultId();
    if (!id) return;
    await execute((signal) => getVault(id, signal), applyView, refresh);
  }

  async function fastForward(): Promise<void> {
    const id = vaultId();
    if (!id) return;
    await execute((signal) => demoFastForward(id, signal), applyView, fastForward, { fastForward: true });
  }

  /** Approves or vetoes with the guardian token the simulator holds. */
  async function sendGuardianAction(action: "APPROVE" | "VETO", confirmed: boolean): Promise<void> {
    const id = vaultId();
    if (!id) return;

    let token = getState().guardianToken;
    if (!token) {
      // A View from the event stream carries no token, so read the vault to get one.
      await refresh();
      token = getState().guardianToken;
      if (!token) {
        if (!getState().unreachable) dispatch(notice("LINK_INVALID", COPY.notices.LINK_INVALID));
        return;
      }
    }

    const held = token;
    await execute(
      (signal) => guardianAction(id, { action, token: held, confirmed }, signal),
      (result) => applyView(result.vault),
      () => sendGuardianAction(action, confirmed),
      { guardian: true },
    );
  }

  const commands: SimulatorCommands = {
    startPreset,
    submitAnswers,
    reconfirm,
    cancelReconfirm: () => {
      const { vault } = getState();
      if (vault?.state !== "EXPIRED" || !vault.reconfirm_required) return;
      dispatch({ type: "CANCEL_RECONFIRM" });
      // Nothing more can happen to this payment, so stop watching it.
      stopSync();
    },
    veto: () => sendGuardianAction("VETO", true),
    guardianApprove: (confirmed) => sendGuardianAction("APPROVE", confirmed),
    guardianVeto: () => sendGuardianAction("VETO", true),
    fastForward,
    refresh,
    retry: () => getState().unreachable?.retry(),
  };

  return {
    commands,
    dispose: () => {
      generation += 1;
      stopSync();
      abortAll();
    },
  };
}

/**
 * Owns the simulator state for the App. Call it once and pass `state` and the
 * commands down. The commands keep the same identity from render to render.
 */
export function useSimulator(): Simulator {
  // The reducer runs here, not inside React, so a command can read the newest
  // state at once, even before React has re-rendered.
  const stateRef = useRef<SimState>(initialState);
  const [state, setState] = useState<SimState>(initialState);

  const coreRef = useRef<SimulatorCore | null>(null);
  if (coreRef.current === null) {
    coreRef.current = createSimulator({
      getState: () => stateRef.current,
      dispatch: (action) => {
        stateRef.current = reduce(stateRef.current, action);
        setState(stateRef.current);
      },
    });
  }
  const core = coreRef.current;

  useEffect(() => () => core.dispose(), [core]);

  return { state, ...core.commands };
}
