import fc from "fast-check";
import { describe, expect, it } from "vitest";

import type { VaultView } from "./api";
import { PBT_RUNS } from "../test/pbt";
import { preset, source, VAULT_A, view } from "../test/reducerArbitraries";
import { initialState, reduce, type SimState } from "./simulatorReducer";

const baseView: VaultView = {
  vault_id: VAULT_A,
  state: "LOCKED_15M",
  settled: false,
  risk_score: 90,
  remaining_seconds: 900,
  reconfirm_required: false,
  reason: "",
  metadata: {},
};

/**
 * What the held token must be after `history`, worked out from the whole list of
 * Views: look back to the latest View that is not LOCKED_15M, and take the last
 * non-empty token among the Views after it.
 */
function expectedToken(history: readonly VaultView[]): string | null {
  const last = history[history.length - 1];
  if (!last || last.state !== "LOCKED_15M") return null;

  let token: string | null = null;
  for (let i = history.length - 1; i >= 0; i--) {
    const v = history[i];
    if (!v || v.state !== "LOCKED_15M") break;
    if (v.guardian_token) {
      token = v.guardian_token;
      break;
    }
  }
  return token;
}

describe("simulator reducer properties", () => {
  // Feature: dual-screen-simulator, Property 9: Token retention
  // Validates: Requirements 8.3, 6.14
  it("Property 9: the held token is the latest non-empty token while the vault stays LOCKED_15M, and none otherwise", () => {
    fc.assert(
      fc.property(
        preset,
        view(VAULT_A),
        fc.array(fc.tuple(view(VAULT_A), source), { maxLength: 15 }),
        (p, first, rest) => {
          let state: SimState = reduce(reduce(initialState, { type: "START", preset: p }), { type: "EVALUATED", view: first });
          const history: VaultView[] = [first];
          expect(state.guardianToken).toBe(expectedToken(history));

          for (const [next, src] of rest) {
            // Polls here carry no start number, so none is dropped as stale.
            state = reduce(state, { type: "VIEW", view: next, source: src });
            history.push(next);
            expect(state.guardianToken, `after ${next.state}`).toBe(expectedToken(history));
          }
        },
      ),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 9: a LOCKED_15M View with no token never clears the held token", () => {
    fc.assert(
      fc.property(fc.stringMatching(/^[a-z0-9]{6,12}$/), fc.constantFrom("", undefined), source, (token, missing, src) => {
        const locked = (t?: string): VaultView => {
          const v: VaultView = { ...baseView, state: "LOCKED_15M" };
          if (t !== undefined) v.guardian_token = t;
          return v;
        };
        let state = reduce(initialState, { type: "EVALUATED", view: locked(token) });
        state = reduce(state, { type: "VIEW", view: locked(missing), source: src });
        expect(state.guardianToken).toBe(token);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 9: any other state clears the token, even a View that carries one", () => {
    fc.assert(
      fc.property(
        fc.constantFrom("INSTANT_PASS", "REALITY_CHECK", "GUARDIAN_APPROVED", "VETOED", "EXPIRED"),
        fc.stringMatching(/^[a-z0-9]{6,12}$/),
        (otherState, stray) => {
          let state = reduce(initialState, { type: "EVALUATED", view: { ...baseView, state: "LOCKED_15M", guardian_token: "held-token" } });
          expect(state.guardianToken).toBe("held-token");

          state = reduce(state, { type: "VIEW", view: { ...baseView, state: otherState, guardian_token: stray }, source: "action" });
          expect(state.guardianToken).toBeNull();
        },
      ),
      { numRuns: PBT_RUNS },
    );
  });
});
