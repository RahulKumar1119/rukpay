import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { ALL_STATES, makeView } from "../test/stubs";
import type { VaultView } from "./api";
import { deriveGuardianScreen, deriveUserScreen, isTerminal, type GuardianScreen, type UserScreen } from "./screens";

const USER_SCREENS: readonly UserScreen[] = [
  "PROMPT",
  "SENT",
  "CHECKING",
  "CONTINUE",
  "LOCKED",
  "RELEASED",
  "CANCELLED",
  "RECONFIRM",
  "DONE",
];
const GUARDIAN_SCREENS: readonly GuardianScreen[] = ["IDLE", "ALERT", "APPROVED", "VETOED"];

// Any View: every state, with settled and reconfirm_required free of the state,
// so combinations the backend never sends are covered too.
const anyView: fc.Arbitrary<VaultView> = fc
  .record({
    state: fc.constantFrom(...ALL_STATES),
    settled: fc.boolean(),
    reconfirm_required: fc.boolean(),
    remaining_seconds: fc.integer({ min: 0, max: 900 }),
    risk_score: fc.integer({ min: 0, max: 100 }),
  })
  .map(({ state, ...rest }) => makeView(state, rest));

// The mapping table of the design, written out one row per case.
function expectedUser(v: VaultView | null): UserScreen {
  if (v === null) return "PROMPT";
  if (v.state === "INSTANT_PASS") return "SENT";
  if (v.state === "REALITY_CHECK" && !v.settled) return "CHECKING";
  if (v.state === "REALITY_CHECK" && v.settled) return "CONTINUE";
  if (v.state === "LOCKED_15M") return "LOCKED";
  if (v.state === "GUARDIAN_APPROVED") return "RELEASED";
  if (v.state === "VETOED") return "CANCELLED";
  if (v.state === "EXPIRED" && v.reconfirm_required) return "RECONFIRM";
  return "DONE";
}

function expectedGuardian(v: VaultView | null): GuardianScreen {
  if (v === null) return "IDLE";
  if (v.state === "LOCKED_15M") return "ALERT";
  if (v.state === "GUARDIAN_APPROVED") return "APPROVED";
  if (v.state === "VETOED") return "VETOED";
  return "IDLE";
}

const maybeView = fc.oneof({ weight: 9, arbitrary: anyView }, { weight: 1, arbitrary: fc.constant(null) });

describe("screen derivation properties", () => {
  // Feature: dual-screen-simulator, Property 6: Exactly one screen per view
  // Validates: Requirements 3.3, 3.4, 3.5, 3.6, 4.1, 6.1, 6.2, 6.9, 6.10
  it("Property 6: every view gives exactly one known screen on each phone, as the mapping table says", () => {
    fc.assert(
      fc.property(maybeView, (v) => {
        const user = deriveUserScreen(v);
        const guardian = deriveGuardianScreen(v);
        expect(USER_SCREENS.filter((s) => s === user)).toHaveLength(1);
        expect(GUARDIAN_SCREENS.filter((s) => s === guardian)).toHaveLength(1);
        expect(user).toBe(expectedUser(v));
        expect(guardian).toBe(expectedGuardian(v));
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 6: the reality-check modal is open exactly when the state is REALITY_CHECK and not settled", () => {
    fc.assert(
      fc.property(maybeView, (v) => {
        const open = deriveUserScreen(v) === "CHECKING";
        expect(open).toBe(v !== null && v.state === "REALITY_CHECK" && !v.settled);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 6: the guardian alert shows exactly while the state is LOCKED_15M", () => {
    fc.assert(
      fc.property(maybeView, (v) => {
        expect(deriveGuardianScreen(v) === "ALERT").toBe(v !== null && v.state === "LOCKED_15M");
        expect(deriveGuardianScreen(v) === "IDLE").toBe(
          v === null || !["LOCKED_15M", "GUARDIAN_APPROVED", "VETOED"].includes(v.state),
        );
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 6: the re-confirmation step shows exactly when the state is EXPIRED and reconfirm_required is true", () => {
    fc.assert(
      fc.property(maybeView, (v) => {
        expect(deriveUserScreen(v) === "RECONFIRM").toBe(v !== null && v.state === "EXPIRED" && v.reconfirm_required);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 6: a vault in LOCKED_15M always shows the locked screen and the alert together", () => {
    fc.assert(
      fc.property(anyView, (v) => {
        fc.pre(v.state === "LOCKED_15M");
        expect([deriveUserScreen(v), deriveGuardianScreen(v)]).toEqual(["LOCKED", "ALERT"]);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: dual-screen-simulator, Property 7: Terminal states stop the sync (the isTerminal half)
  // Validates: Requirements 8.7
  it("Property 7: isTerminal is true exactly for the states that cannot change", () => {
    fc.assert(
      fc.property(anyView, (v) => {
        const want =
          v.state === "INSTANT_PASS" ||
          v.state === "GUARDIAN_APPROVED" ||
          v.state === "VETOED" ||
          (v.state === "EXPIRED" && !v.reconfirm_required);
        expect(isTerminal(v)).toBe(want);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});

describe("screen derivation examples", () => {
  it.each([
    ["INSTANT_PASS", "SENT", "IDLE"],
    ["LOCKED_15M", "LOCKED", "ALERT"],
    ["GUARDIAN_APPROVED", "RELEASED", "APPROVED"],
    ["VETOED", "CANCELLED", "VETOED"],
  ] as const)("%s shows %s to the payer and %s to the guardian", (state, user, guardian) => {
    const v = makeView(state);
    expect(deriveUserScreen(v)).toBe(user);
    expect(deriveGuardianScreen(v)).toBe(guardian);
  });

  it("shows the questions until the check is settled, then the continue button", () => {
    expect(deriveUserScreen(makeView("REALITY_CHECK", { settled: false }))).toBe("CHECKING");
    expect(deriveUserScreen(makeView("REALITY_CHECK", { settled: true }))).toBe("CONTINUE");
  });

  it("shows the final confirmation for an expired vault only while it is still needed", () => {
    expect(deriveUserScreen(makeView("EXPIRED", { reconfirm_required: true }))).toBe("RECONFIRM");
    expect(deriveUserScreen(makeView("EXPIRED", { reconfirm_required: false, settled: true }))).toBe("DONE");
  });

  it("shows the prompt and an idle guardian when there is no vault", () => {
    expect(deriveUserScreen(null)).toBe("PROMPT");
    expect(deriveGuardianScreen(null)).toBe("IDLE");
  });

  it("falls back to the prompt and an idle guardian for a state it does not know", () => {
    const unknown = { ...makeView("INSTANT_PASS"), state: "SOMETHING_NEW" } as unknown as VaultView;
    expect(deriveUserScreen(unknown)).toBe("PROMPT");
    expect(deriveGuardianScreen(unknown)).toBe("IDLE");
    expect(isTerminal(unknown)).toBe(false);
  });
});
