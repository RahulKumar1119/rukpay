/**
 * Generators for the reducer property tests. Test only.
 */
import fc from "fast-check";

import type { VaultView } from "../lib/api";
import { PRESETS } from "../lib/presets";
import { initialState, reduce, type SimAction, type SimState, type ViewSource } from "../lib/simulatorReducer";
import { ALL_STATES, makeView } from "./stubs";

export const VAULT_A = "vault-a";
export const VAULT_B = "vault-b";

export const preset = fc.constantFrom(...PRESETS);
export const source: fc.Arbitrary<ViewSource> = fc.constantFrom("stream", "poll", "action");

/** A non-empty token, an empty one, or none at all. */
const maybeToken = fc.oneof(
  { weight: 3, arbitrary: fc.stringMatching(/^[a-z0-9]{6,12}$/) },
  { weight: 1, arbitrary: fc.constant("") },
  { weight: 2, arbitrary: fc.constant(undefined) },
);

/** A View of `vaultId` in any state, with a token that may be present, empty or missing in any state. */
export function view(vaultId: string): fc.Arbitrary<VaultView> {
  return fc
    .record({
      state: fc.constantFrom(...ALL_STATES),
      token: maybeToken,
      settled: fc.boolean(),
      reconfirm_required: fc.boolean(),
      remaining_seconds: fc.integer({ min: 0, max: 900 }),
    })
    .map(({ state, token, ...rest }) => {
      const v = makeView(state, { vault_id: vaultId, ...rest });
      delete v.guardian_token;
      if (token !== undefined) v.guardian_token = token;
      return v;
    });
}

/** A VIEW action for `vaultId`. A poll may carry the sequence number it started at. */
export function viewAction(vaultId: string): fc.Arbitrary<SimAction> {
  return fc
    .record({
      view: view(vaultId),
      source,
      startedAtSeq: fc.option(fc.integer({ min: 0, max: 40 }), { nil: undefined }),
    })
    .map(({ view: v, source: s, startedAtSeq }) => ({ type: "VIEW", view: v, source: s, startedAtSeq }));
}

const noop = (): void => undefined;

/** Actions that never start a new scenario, so they keep the current vault. */
export const keepVaultAction: fc.Arbitrary<SimAction> = fc.oneof(
  fc.boolean().map((busy): SimAction => ({ type: "BUSY", busy })),
  fc.constant<SimAction>({ type: "NOTICE", notice: null }),
  fc.constant<SimAction>({ type: "NOTICE", notice: { kind: "ALREADY_ACTED", message: "Someone already acted on this payment." } }),
  fc.constantFrom<"action" | "poll" | undefined>("action", "poll", undefined).map(
    (src): SimAction => ({ type: "UNREACHABLE", retry: null, source: src }),
  ),
  fc.constantFrom<"action" | "poll">("action", "poll").map(
    (src): SimAction => ({ type: "UNREACHABLE", retry: noop, source: src }),
  ),
  fc.constant<SimAction>({ type: "FAST_FORWARD_UNAVAILABLE" }),
  fc.constant<SimAction>({ type: "CANCEL_RECONFIRM" }),
);

/** Applies actions in order, from `start`. */
export function run(start: SimState, actions: readonly SimAction[]): SimState {
  return actions.reduce(reduce, start);
}

/** A state that holds a vault of `vaultId`, reached through a random sequence of actions. */
export function stateWithVault(vaultId: string): fc.Arbitrary<SimState> {
  return fc
    .tuple(
      preset,
      view(vaultId),
      fc.array(fc.oneof(viewAction(vaultId), keepVaultAction), { maxLength: 12 }),
    )
    .map(([p, first, actions]) =>
      run(initialState, [{ type: "START", preset: p }, { type: "EVALUATED", view: first }, ...actions]),
    );
}

/** Any state the app can reach, including ones with no vault and ones that follow a new START. */
export const anyState: fc.Arbitrary<SimState> = fc
  .array(
    fc.oneof(
      preset.map((p): SimAction => ({ type: "START", preset: p })),
      view(VAULT_A).map((v): SimAction => ({ type: "EVALUATED", view: v })),
      viewAction(VAULT_A),
      keepVaultAction,
    ),
    { maxLength: 16 },
  )
  .map((actions) => run(initialState, actions));
