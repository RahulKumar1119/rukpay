import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { source, stateWithVault, VAULT_A, VAULT_B, view } from "../test/reducerArbitraries";
import { reduce } from "./simulatorReducer";

// Other vault ids: a fixed one, and random ones that differ from VAULT_A.
const otherVaultId = fc.oneof(
  fc.constant(VAULT_B),
  fc.stringMatching(/^[0-9a-f]{32}$/),
  fc.string({ unit: "grapheme-ascii", minLength: 1 }).filter((id) => id !== VAULT_A),
);

// A View of any vault but VAULT_A, in any state.
const foreignView = otherVaultId.chain((id) => view(id));

const startedAt = fc.option(fc.integer({ min: 0, max: 40 }), { nil: undefined });

describe("simulator reducer properties", () => {
  // Feature: dual-screen-simulator, Property 10: Foreign views are discarded
  // Validates: Requirements 8.9
  it("Property 10: a View of another vault leaves the state unchanged", () => {
    fc.assert(
      fc.property(stateWithVault(VAULT_A), foreignView, source, startedAt, (state, foreign, src, startedAtSeq) => {
        expect(state.vault?.vault_id).toBe(VAULT_A);

        const next = reduce(state, { type: "VIEW", view: foreign, source: src, startedAtSeq });

        expect(next).toBe(state);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 10: a foreign terminal View does not change a vault that is still locked", () => {
    fc.assert(
      fc.property(stateWithVault(VAULT_A), otherVaultId, source, (state, otherId, src) => {
        const before = state.vault;
        if (!before) throw new Error("the generator must give a state with a vault");

        const next = reduce(state, {
          type: "VIEW",
          view: { ...before, vault_id: otherId, state: "VETOED", evidence_summary: "x" },
          source: src,
        });

        expect(next.vault).toBe(before);
        expect(next.guardianToken).toBe(state.guardianToken);
        expect(next.requestSeq).toBe(state.requestSeq);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 10: a View of the same vault from an action is accepted", () => {
    fc.assert(
      fc.property(stateWithVault(VAULT_A), view(VAULT_A), (state, next) => {
        const after = reduce(state, { type: "VIEW", view: next, source: "action" });

        expect(after.vault).toBe(next);
        expect(after.requestSeq).toBe(state.requestSeq + 1);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
