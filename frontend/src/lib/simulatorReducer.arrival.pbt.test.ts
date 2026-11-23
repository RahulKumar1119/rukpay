import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { preset, source, stateWithVault, VAULT_A, view } from "../test/reducerArbitraries";
import { initialState, reduce, type SimState } from "./simulatorReducer";

describe("simulator reducer properties", () => {
  // Feature: dual-screen-simulator, Property 11: Newest arrival wins, and stale polls are dropped
  // Validates: Requirements 8.10, 8.2
  it("Property 11: after any run of accepted Views, the vault is the last one that arrived", () => {
    fc.assert(
      fc.property(preset, view(VAULT_A), fc.array(fc.tuple(view(VAULT_A), source), { minLength: 1, maxLength: 15 }), (p, first, rest) => {
        let state: SimState = reduce(reduce(initialState, { type: "START", preset: p }), { type: "EVALUATED", view: first });
        let last = first;

        for (const [next, src] of rest) {
          // A poll that starts now is not older than any action so far.
          state = reduce(state, { type: "VIEW", view: next, source: src, startedAtSeq: state.requestSeq });
          last = next;
          expect(state.vault).toBe(last);
        }
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 11: a poll that started before the latest action response leaves the state unchanged", () => {
    fc.assert(
      fc.property(stateWithVault(VAULT_A), view(VAULT_A), view(VAULT_A), fc.nat(), (state, actionView, pollView, pick) => {
        // An action response is applied. Any poll that began earlier is now stale.
        const afterAction = reduce(state, { type: "VIEW", view: actionView, source: "action" });
        expect(afterAction.lastActionSeq).toBeGreaterThan(0);
        const startedAtSeq = pick % afterAction.lastActionSeq; // 0 to lastActionSeq - 1

        const next = reduce(afterAction, { type: "VIEW", view: pollView, source: "poll", startedAtSeq });

        expect(next).toBe(afterAction);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 11: a poll that started after the latest action response is applied", () => {
    fc.assert(
      fc.property(stateWithVault(VAULT_A), view(VAULT_A), view(VAULT_A), fc.nat({ max: 20 }), (state, actionView, pollView, extra) => {
        const afterAction = reduce(state, { type: "VIEW", view: actionView, source: "action" });
        const startedAtSeq = afterAction.lastActionSeq + extra; // at or after the action

        const next = reduce(afterAction, { type: "VIEW", view: pollView, source: "poll", startedAtSeq });

        expect(next.vault).toBe(pollView);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 11: stream Views are never dropped as stale, whatever the earlier actions were", () => {
    fc.assert(
      fc.property(stateWithVault(VAULT_A), view(VAULT_A), (state, streamView) => {
        const next = reduce(state, { type: "VIEW", view: streamView, source: "stream" });
        expect(next.vault).toBe(streamView);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 11: an old poll cannot undo a newer action result", () => {
    fc.assert(
      fc.property(preset, view(VAULT_A), view(VAULT_A), view(VAULT_A), (p, first, polled, acted) => {
        let state = reduce(reduce(initialState, { type: "START", preset: p }), { type: "EVALUATED", view: first });
        const pollStartedAt = state.requestSeq; // the poll is sent here

        state = reduce(state, { type: "VIEW", view: acted, source: "action" }); // an action answers first
        state = reduce(state, { type: "VIEW", view: polled, source: "poll", startedAtSeq: pollStartedAt }); // the old poll answers later

        expect(state.vault).toBe(acted);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
