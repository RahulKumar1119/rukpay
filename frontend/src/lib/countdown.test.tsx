import { act, renderHook } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { makeView } from "../test/stubs";
import type { VaultView } from "./api";
import { formatCountdown, parseCountdown, spokenCountdown, SPOKEN_REFRESH_MS, useCountdown } from "./countdown";

const locked = (remaining: number, overrides: Partial<VaultView> = {}): VaultView =>
  makeView("LOCKED_15M", { remaining_seconds: remaining, ...overrides });

const advance = (ms: number): void => {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
};

function setup(initial: VaultView | null, onZero: () => void = () => undefined) {
  return renderHook(({ view, cb }: { view: VaultView | null; cb: () => void }) => useCountdown(view, cb), {
    initialProps: { view: initial, cb: onZero },
  });
}

beforeEach(() => {
  vi.useFakeTimers({ now: new Date("2026-11-23T12:00:00Z") });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("formatCountdown and parseCountdown", () => {
  it.each([
    [900, "15:00"],
    [899, "14:59"],
    [605, "10:05"],
    [60, "01:00"],
    [59, "00:59"],
    [1, "00:01"],
    [0, "00:00"],
  ])("formats %i seconds as %s", (seconds, text) => {
    expect(formatCountdown(seconds)).toBe(text);
    expect(parseCountdown(text)).toBe(seconds);
  });

  it("shows 00:00 for a negative or invalid value", () => {
    expect(formatCountdown(-5)).toBe("00:00");
    expect(formatCountdown(Number.NaN)).toBe("00:00");
    expect(formatCountdown(Number.POSITIVE_INFINITY)).toBe("00:00");
  });

  it("returns NaN for text that is not mm:ss", () => {
    for (const text of ["", "1:5", "15:60", "-1:00", "15:00 ", "ab:cd"]) {
      expect(parseCountdown(text), text).toBeNaN();
    }
  });
});

describe("spokenCountdown", () => {
  it.each([
    [845, "14 minutes and 5 seconds"],
    [900, "15 minutes"],
    [60, "1 minute"],
    [61, "1 minute and 1 second"],
    [59, "59 seconds"],
    [1, "1 second"],
    [0, "0 seconds"],
  ])("says %i seconds as %j", (seconds, words) => {
    expect(spokenCountdown(seconds)).toBe(words);
  });
});

describe("useCountdown", () => {
  it("shows mm:ss and counts down once a second", () => {
    const { result } = setup(locked(900));
    expect(result.current.text).toBe("15:00");

    advance(1000);
    expect(result.current.text).toBe("14:59");

    advance(59_000);
    expect(result.current.text).toBe("14:00");
  });

  it("counts from the clock, so a late timer does not lose time", () => {
    const { result } = setup(locked(900));
    advance(5000);
    expect(result.current.text).toBe("14:55");

    // One tick that arrives 30 seconds late still shows the true time.
    vi.setSystemTime(Date.now() + 30_000);
    advance(1000);
    expect(result.current.text).toBe("14:24");
  });

  it("never shows less than 00:00", () => {
    const { result } = setup(locked(2));
    advance(2000);
    expect(result.current.text).toBe("00:00");

    advance(30_000);
    expect(result.current.text).toBe("00:00");
    expect(result.current.text).not.toContain("-");
  });

  it("starts again from the new value when a View with a different remaining_seconds arrives", () => {
    const view = locked(900);
    const { result, rerender } = setup(view);
    advance(30_000);
    expect(result.current.text).toBe("14:30");

    rerender({ view: { ...view, remaining_seconds: 600 }, cb: () => undefined });
    expect(result.current.text).toBe("10:00");

    advance(1000);
    expect(result.current.text).toBe("09:59");
  });

  it("can move up when the new View has more time", () => {
    const view = locked(100);
    const { result, rerender } = setup(view);
    advance(40_000);
    expect(result.current.text).toBe("01:00");

    rerender({ view: { ...view, remaining_seconds: 300 }, cb: () => undefined });
    expect(result.current.text).toBe("05:00");
  });

  it("does not restart when the same View is rendered again", () => {
    const view = locked(900);
    const { result, rerender } = setup(view);
    advance(10_000);

    rerender({ view: { ...view }, cb: () => undefined });
    expect(result.current.text).toBe("14:50");
    advance(1000);
    expect(result.current.text).toBe("14:49");
  });

  it("calls onZero exactly once when the count reaches 00:00", () => {
    const onZero = vi.fn();
    setup(locked(3), onZero);

    advance(2999);
    expect(onZero).not.toHaveBeenCalled();

    advance(1);
    expect(onZero).toHaveBeenCalledTimes(1);

    advance(60_000);
    expect(onZero).toHaveBeenCalledTimes(1);
  });

  it("calls onZero once for a vault that is already at zero, even in strict mode", () => {
    const onZero = vi.fn();
    const wrapper = ({ children }: { children: ReactNode }) => <StrictMode>{children}</StrictMode>;
    renderHook(() => useCountdown(locked(0), onZero), { wrapper });

    advance(5000);
    expect(onZero).toHaveBeenCalledTimes(1);
  });

  it("calls the latest onZero, not the one from the first render", () => {
    const first = vi.fn();
    const second = vi.fn();
    const view = locked(5);
    const { rerender } = setup(view, first);

    rerender({ view, cb: second });
    advance(5000);

    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("calls onZero again for a different vault", () => {
    const onZero = vi.fn();
    const { rerender } = setup(locked(2, { vault_id: "one" }), onZero);
    advance(2000);
    expect(onZero).toHaveBeenCalledTimes(1);

    rerender({ view: locked(2, { vault_id: "two" }), cb: onZero });
    advance(2000);
    expect(onZero).toHaveBeenCalledTimes(2);
  });

  it("does not call onZero when the state leaves LOCKED_15M before zero, and it stops its timer", () => {
    const onZero = vi.fn();
    const { result, rerender } = setup(locked(10), onZero);
    advance(4000);

    rerender({ view: makeView("GUARDIAN_APPROVED"), cb: onZero });
    expect(vi.getTimerCount()).toBe(0);
    expect(result.current.text).toBe("00:00");

    advance(60_000);
    expect(onZero).not.toHaveBeenCalled();
  });

  it("shows 00:00 and keeps no timer for a null View or a state that is not locked", () => {
    const { result, rerender } = setup(null);
    expect(result.current.text).toBe("00:00");
    expect(vi.getTimerCount()).toBe(0);

    rerender({ view: makeView("REALITY_CHECK"), cb: () => undefined });
    expect(result.current.text).toBe("00:00");
    expect(vi.getTimerCount()).toBe(0);
  });

  it("clears its timer on unmount", () => {
    const onZero = vi.fn();
    const { unmount } = setup(locked(5), onZero);
    expect(vi.getTimerCount()).toBe(1);

    unmount();
    expect(vi.getTimerCount()).toBe(0);

    advance(10_000);
    expect(onZero).not.toHaveBeenCalled();
  });

  it("keeps one timer through strict mode's extra effect run", () => {
    const wrapper = ({ children }: { children: ReactNode }) => <StrictMode>{children}</StrictMode>;
    renderHook(() => useCountdown(locked(900), () => undefined), { wrapper });
    expect(vi.getTimerCount()).toBe(1);
  });

  describe("spoken label", () => {
    it("starts as the time in words", () => {
      const { result } = setup(locked(845));
      expect(result.current.spoken).toBe("14 minutes and 5 seconds");
    });

    it("changes no more than once every 10 seconds", () => {
      const { result } = setup(locked(900));
      const changes: number[] = [];
      let previous = result.current.spoken;

      for (let second = 1; second <= 120; second++) {
        advance(1000);
        if (result.current.spoken !== previous) {
          changes.push(second);
          previous = result.current.spoken;
        }
      }

      // 120 seconds at one change per 10 seconds is at most 12 changes.
      expect(changes.length).toBeGreaterThan(0);
      expect(changes.length).toBeLessThanOrEqual(120 / (SPOKEN_REFRESH_MS / 1000));
      for (let i = 1; i < changes.length; i++) {
        const gap = (changes[i] as number) - (changes[i - 1] as number);
        expect(gap).toBeGreaterThanOrEqual(SPOKEN_REFRESH_MS / 1000);
      }
    });

    it("stays the same while the display ticks through nine seconds", () => {
      const { result } = setup(locked(900));
      const first = result.current.spoken;
      for (let i = 0; i < 9; i++) advance(1000);
      expect(result.current.text).toBe("14:51");
      expect(result.current.spoken).toBe(first);

      advance(1000);
      expect(result.current.spoken).toBe("14 minutes and 50 seconds");
    });

    it("does not change when a new View arrives inside the 10 seconds", () => {
      const view = locked(900);
      const { result, rerender } = setup(view);
      const first = result.current.spoken;
      advance(3000);

      rerender({ view: { ...view, remaining_seconds: 840 }, cb: () => undefined });
      expect(result.current.text).toBe("14:00");
      expect(result.current.spoken).toBe(first);
    });
  });
});
