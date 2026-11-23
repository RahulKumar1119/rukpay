import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { anyState, preset } from "../test/reducerArbitraries";
import { initialState, reduce } from "./simulatorReducer";

describe("simulator reducer properties", () => {
  // Feature: dual-screen-simulator, Property 16: New preset leaves no residue
  // Validates: Requirements 2.5
  it("Property 16: START leaves no vault, token, notice or unreachable mark, and sets busy", () => {
    fc.assert(
      fc.property(anyState, preset, (state, p) => {
        const next = reduce(state, { type: "START", preset: p });

        expect(next.preset).toBe(p);
        expect(next.vault).toBeNull();
        expect(next.guardianToken).toBeNull();
        expect(next.notice).toBeNull();
        expect(next.unreachable).toBeNull();
        expect(next.busy).toBe(true);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 16: START also resets the counters and the cancelled confirmation", () => {
    fc.assert(
      fc.property(anyState, preset, (state, p) => {
        const next = reduce(state, { type: "START", preset: p });

        expect(next.requestSeq).toBe(0);
        expect(next.lastActionSeq).toBe(0);
        expect(next.reconfirmCancelled).toBe(false);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 16: START keeps the session-wide finding that fast-forward is off", () => {
    fc.assert(
      fc.property(anyState, preset, (state, p) => {
        expect(reduce(state, { type: "START", preset: p }).fastForwardHidden).toBe(state.fastForwardHidden);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 16: the state after START does not depend on the state before it", () => {
    fc.assert(
      fc.property(anyState, anyState, preset, (a, b, p) => {
        const fromA = reduce({ ...a, fastForwardHidden: false }, { type: "START", preset: p });
        const fromB = reduce({ ...b, fastForwardHidden: false }, { type: "START", preset: p });
        expect(fromA).toEqual(fromB);
        expect(fromA).toEqual({ ...initialState, preset: p, busy: true });
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
