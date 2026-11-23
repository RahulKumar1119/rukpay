/**
 * The state of the simulator and the one pure function that changes it.
 *
 * Everything the screens show comes from here: the current vault, the guardian
 * token, the busy flag, the notices. Nothing in this file touches the network,
 * a timer or storage, and the guardian token lives only in this state, never in
 * storage, a cookie or the URL (Requirement 6.14).
 */
import type { VaultView } from "./api";
import type { Preset } from "./presets";

export type NoticeKind = "BAD_REQUEST" | "LINK_INVALID" | "ALREADY_ACTED" | "COPY_FAILED";

/** A calm message shown inline until the state changes or the person acts. */
export interface Notice {
  kind: NoticeKind;
  message: string;
}

/** Who reported that the server cannot be reached: a request the person made, or the background poll. */
export type UnreachableSource = "action" | "poll";

export interface SimState {
  preset: Preset | null;
  vault: VaultView | null;
  /** The guardian token of the vault while it is LOCKED_15M. Memory only. */
  guardianToken: string | null;
  /** Counts every accepted View, so arrival order can be compared. */
  requestSeq: number;
  /** The value of `requestSeq` right after the last accepted action response. */
  lastActionSeq: number;
  /** True while the first evaluate request is in flight. */
  busy: boolean;
  /** Set while the server cannot be reached. `retry` repeats what failed. */
  unreachable: { retry: () => void; source: UnreachableSource } | null;
  notice: Notice | null;
  /** The server said demo fast-forward is off, so the button stays hidden for the session. */
  fastForwardHidden: boolean;
  /** The payer chose "Cancel and keep my money" at the final confirmation. */
  reconfirmCancelled: boolean;
}

export type ViewSource = "stream" | "poll" | "action";

export type SimAction =
  /** A new scenario starts. Everything from the old one is discarded. */
  | { type: "START"; preset: Preset }
  /** The first View of a new vault, from the evaluate response. */
  | { type: "EVALUATED"; view: VaultView }
  /** A View from the event stream, from a poll or from an action response. */
  | { type: "VIEW"; view: VaultView; source: ViewSource; startedAtSeq?: number }
  | { type: "BUSY"; busy: boolean }
  /** With `retry`, marks the server unreachable. With `retry: null`, clears the mark. */
  | { type: "UNREACHABLE"; retry: (() => void) | null; source?: UnreachableSource }
  | { type: "NOTICE"; notice: Notice | null }
  | { type: "FAST_FORWARD_UNAVAILABLE" }
  | { type: "CANCEL_RECONFIRM" };

export const initialState: SimState = {
  preset: null,
  vault: null,
  guardianToken: null,
  requestSeq: 0,
  lastActionSeq: 0,
  busy: false,
  unreachable: null,
  notice: null,
  fastForwardHidden: false,
  reconfirmCancelled: false,
};

/**
 * The token to hold after `view`. It is kept only while the vault is
 * LOCKED_15M. A LOCKED_15M View without a token, such as one from the event
 * stream, leaves the held token alone (Requirement 8.3). Any other state clears
 * it, because the backend stops issuing it.
 */
function tokenAfter(held: string | null, view: VaultView): string | null {
  if (view.state !== "LOCKED_15M") return null;
  return view.guardian_token ? view.guardian_token : held;
}

export function reduce(s: SimState, a: SimAction): SimState {
  switch (a.type) {
    case "START":
      // The "demo fast-forward is off" finding holds for the whole session, so
      // it survives a new scenario. Nothing else does.
      return { ...initialState, preset: a.preset, busy: true, fastForwardHidden: s.fastForwardHidden };

    case "EVALUATED": {
      const seq = s.requestSeq + 1;
      return {
        ...s,
        vault: a.view,
        guardianToken: tokenAfter(null, a.view),
        requestSeq: seq,
        lastActionSeq: seq,
        busy: false,
        notice: null,
        reconfirmCancelled: false,
      };
    }

    case "VIEW": {
      const { view, source } = a;
      // A View for another vault is not ours (Requirement 8.9).
      if (s.vault !== null && view.vault_id !== s.vault.vault_id) return s;
      // A poll that began before an action response was applied would undo it.
      // A poll that does not say when it started cannot be judged, so it is kept.
      if (source === "poll" && a.startedAtSeq !== undefined && a.startedAtSeq < s.lastActionSeq) return s;

      const seq = s.requestSeq + 1;
      const stateChanged = s.vault === null || s.vault.state !== view.state;
      return {
        ...s,
        vault: view,
        guardianToken: tokenAfter(s.guardianToken, view),
        requestSeq: seq,
        lastActionSeq: source === "action" ? seq : s.lastActionSeq,
        notice: stateChanged ? null : s.notice,
        reconfirmCancelled: stateChanged ? false : s.reconfirmCancelled,
      };
    }

    case "BUSY":
      return { ...s, busy: a.busy };

    case "UNREACHABLE": {
      if (a.retry) return { ...s, unreachable: { retry: a.retry, source: a.source ?? "action" } };
      // A poll that works again must not hide a failed request that was never retried.
      if (s.unreachable === null) return s;
      if (a.source !== undefined && a.source !== s.unreachable.source) return s;
      return { ...s, unreachable: null };
    }

    case "NOTICE":
      return { ...s, notice: a.notice };

    case "FAST_FORWARD_UNAVAILABLE":
      return { ...s, fastForwardHidden: true };

    case "CANCEL_RECONFIRM":
      if (s.vault?.state !== "EXPIRED" || !s.vault.reconfirm_required) return s;
      return { ...s, reconfirmCancelled: true };
  }
}
