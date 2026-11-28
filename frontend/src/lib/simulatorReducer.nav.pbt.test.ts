import { act, renderHook } from "@testing-library/react";
import fc from "fast-check";
import { describe, expect, it, vi } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { anyState, keepVaultAction, preset, run, VAULT_A, view, viewAction } from "../test/reducerArbitraries";
import { installEventSourceStub, installFetchStub, makeView } from "../test/stubs";
import { PRESETS } from "./presets";
import { initialState, reduce, type PhoneScreen, type SimAction, type SimState } from "./simulatorReducer";
import { useSimulator } from "./useSimulator";

const SCREENS: readonly PhoneScreen[] = ["pay", "custom", "saved"];
const screen = fc.constantFrom(...SCREENS);

/** Any state the app can reach, standing on any screen. The screen is set by a spread, not by the reducer under test. */
const anyNavState: fc.Arbitrary<SimState> = fc.tuple(anyState, screen).map(([state, phoneScreen]) => ({ ...state, phoneScreen }));

const showScreen: fc.Arbitrary<SimAction> = screen.map((s): SimAction => ({ type: "SHOW_SCREEN", screen: s }));
const startAction: fc.Arbitrary<SimAction> = preset.map((p): SimAction => ({ type: "START", preset: p }));
const evaluatedAction: fc.Arbitrary<SimAction> = view(VAULT_A).map((v): SimAction => ({ type: "EVALUATED", view: v }));

/** Actions that are not SHOW_SCREEN. A new vault or a new scenario may be among them. */
const otherAction: fc.Arbitrary<SimAction> = fc.oneof(startAction, evaluatedAction, viewAction(VAULT_A), keepVaultAction);

/** The screen the model expects after `actions`, starting on `from`: SHOW_SCREEN sets it, EVALUATED sends it to "pay", nothing else touches it. */
function modelScreen(from: PhoneScreen, actions: readonly SimAction[]): PhoneScreen {
  let current = from;
  for (const action of actions) {
    if (action.type === "SHOW_SCREEN") current = action.screen;
    else if (action.type === "EVALUATED") current = "pay";
  }
  return current;
}

describe("simulator navigation properties", () => {
  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 22.10
  it("Property 33: the simulator starts on the pay screen", () => {
    expect(initialState.phoneScreen).toBe("pay");
    expect(reduce(initialState, { type: "BUSY", busy: false }).phoneScreen).toBe("pay");
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 8.5, 22.7, 22.10
  it("Property 33: SHOW_SCREEN changes phoneScreen and no other field", () => {
    fc.assert(
      fc.property(anyNavState, screen, (state, target) => {
        const before = { ...state };

        const next = reduce(state, { type: "SHOW_SCREEN", screen: target });

        expect(next.phoneScreen).toBe(target);
        expect(Object.keys(next).sort()).toEqual(Object.keys(state).sort());
        for (const key of Object.keys(state) as (keyof SimState)[]) {
          if (key === "phoneScreen") continue;
          // The very same value, not a copy: the vault, the token, the counters and the notices are not touched.
          expect(Object.is(next[key], state[key]), `${key} changed`).toBe(true);
        }
        // The state that came in is left as it was.
        for (const key of Object.keys(before) as (keyof SimState)[]) expect(Object.is(state[key], before[key])).toBe(true);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 22.7
  it("Property 33: showing a screen twice, or the one already shown, gives the same state as showing it once", () => {
    fc.assert(
      fc.property(anyNavState, screen, (state, target) => {
        const once = reduce(state, { type: "SHOW_SCREEN", screen: target });
        const twice = reduce(once, { type: "SHOW_SCREEN", screen: target });
        expect(twice).toEqual(once);
        expect(reduce(state, { type: "SHOW_SCREEN", screen: state.phoneScreen })).toEqual(state);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 8.5
  it("Property 33: EVALUATED sets the screen to pay, from any screen, and still keeps the vault of the answer", () => {
    fc.assert(
      fc.property(anyNavState, view(VAULT_A), (state, answer) => {
        const next = reduce(state, { type: "EVALUATED", view: answer });
        expect(next.phoneScreen).toBe("pay");
        expect(next.vault).toBe(answer);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 22.7
  it("Property 33: START keeps the screen, and resets the rest as before", () => {
    fc.assert(
      fc.property(anyNavState, preset, (state, p) => {
        const next = reduce(state, { type: "START", preset: p });
        expect(next.phoneScreen).toBe(state.phoneScreen);
        expect(next).toEqual({ ...initialState, preset: p, busy: true, fastForwardHidden: state.fastForwardHidden, phoneScreen: state.phoneScreen });
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 22.7
  it("Property 33: no other action changes the screen", () => {
    const quiet = fc.oneof(viewAction(VAULT_A), keepVaultAction);
    fc.assert(
      fc.property(anyNavState, quiet, (state, action) => {
        expect(reduce(state, action).phoneScreen).toBe(state.phoneScreen);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 8.5, 22.7, 22.10
  it("Property 33: after any run of actions the screen is the one the model says", () => {
    fc.assert(
      fc.property(fc.array(fc.oneof(showScreen, otherAction), { maxLength: 24 }), (actions) => {
        expect(run(initialState, actions).phoneScreen).toBe(modelScreen("pay", actions));
      }),
      { numRuns: PBT_RUNS },
    );
  });

  // Feature: risk-intelligence, Property 33: Navigation changes only the screen
  // Validates: Requirements 22.7
  it("Property 33: no action reads or writes storage, a cookie, the address or the history", () => {
    const reads = [vi.spyOn(Storage.prototype, "getItem"), vi.spyOn(Storage.prototype, "key"), vi.spyOn(Document.prototype, "cookie", "get")];
    const writes = [
      vi.spyOn(Storage.prototype, "setItem"),
      vi.spyOn(Storage.prototype, "removeItem"),
      vi.spyOn(Storage.prototype, "clear"),
      vi.spyOn(Document.prototype, "cookie", "set"),
      vi.spyOn(window.history, "pushState"),
      vi.spyOn(window.history, "replaceState"),
    ];
    const address = window.location.href;

    fc.assert(
      fc.property(anyNavState, fc.array(fc.oneof(showScreen, otherAction), { maxLength: 12 }), (state, actions) => {
        run(state, actions);
      }),
      { numRuns: PBT_RUNS },
    );

    for (const spy of [...reads, ...writes]) expect(spy).not.toHaveBeenCalled();
    expect(window.location.href).toBe(address);
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.cookie).toBe("");
  });
});

describe("useSimulator navigation", () => {
  it("starts on pay and exposes showScreen, which changes only the screen and sends nothing", () => {
    const fetchStub = installFetchStub();
    const ES = installEventSourceStub();
    const { result } = renderHook(() => useSimulator());
    expect(result.current.state.phoneScreen).toBe("pay");

    for (const target of ["custom", "saved", "pay", "saved", "saved", "custom"] as const) {
      const before = result.current.state;
      act(() => result.current.showScreen(target));
      expect(result.current.state).toEqual({ ...before, phoneScreen: target });
    }

    expect(fetchStub.calls).toHaveLength(0);
    expect(ES.instances).toHaveLength(0);
  });

  it("keeps the same showScreen function from one render to the next", () => {
    const { result } = renderHook(() => useSimulator());
    const first = result.current.showScreen;
    act(() => first("saved"));
    act(() => result.current.showScreen("custom"));
    expect(result.current.showScreen).toBe(first);
  });

  it("leaves a vault, its token and its notices as they were when the screen changes", async () => {
    const fetchStub = installFetchStub().respond(200, makeView("LOCKED_15M", { guardian_token: "held-token" }));
    installEventSourceStub();
    const { result } = renderHook(() => useSimulator());
    await act(async () => {
      await result.current.startPreset(PRESETS[2]);
    });
    const before = result.current.state;
    expect(before.vault?.state).toBe("LOCKED_15M");
    expect(before.guardianToken).toBe("held-token");

    act(() => result.current.showScreen("saved"));

    expect(result.current.state).toEqual({ ...before, phoneScreen: "saved" });
    expect(result.current.state.vault).toBe(before.vault);
    expect(fetchStub.calls).toHaveLength(1);
  });

  it("keeps the screen while a payment is being checked and returns to pay when the answer arrives", async () => {
    const fetchStub = installFetchStub();
    installEventSourceStub();
    const pending = fetchStub.defer();
    const { result } = renderHook(() => useSimulator());
    act(() => result.current.showScreen("custom"));

    let started: Promise<void> = Promise.resolve();
    act(() => {
      started = result.current.startPreset(PRESETS[0]);
    });
    expect(result.current.state.busy).toBe(true);
    expect(result.current.state.phoneScreen).toBe("custom");

    await act(async () => {
      pending.respond(200, makeView("INSTANT_PASS"));
      await started;
    });
    expect(result.current.state.busy).toBe(false);
    expect(result.current.state.vault?.state).toBe("INSTANT_PASS");
    expect(result.current.state.phoneScreen).toBe("pay");
  });

  it("stays on the screen when the payment request fails", async () => {
    installFetchStub().reject();
    installEventSourceStub();
    const { result } = renderHook(() => useSimulator());
    act(() => result.current.showScreen("saved"));

    await act(async () => {
      await result.current.startPreset(PRESETS[1]);
    });

    expect(result.current.state.unreachable).not.toBeNull();
    expect(result.current.state.phoneScreen).toBe("saved");
  });
});
