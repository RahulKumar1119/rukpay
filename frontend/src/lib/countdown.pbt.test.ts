import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { endTimeFrom, formatCountdown, parseCountdown, secondsLeft, spokenCountdown } from "./countdown";

const nowMs = fc.integer({ min: 0, max: 4_000_000_000_000 });
const remaining = fc.integer({ min: 0, max: 900 });
const elapsedMs = fc.integer({ min: 0, max: 2_000_000 });

describe("countdown properties", () => {
  // Feature: dual-screen-simulator, Property 8: Countdown arithmetic
  // Validates: Requirements 5.2, 5.3, 5.4, 15.4
  it("Property 8: the seconds left are max(0, ceil(remaining - elapsed / 1000))", () => {
    fc.assert(
      fc.property(nowMs, remaining, elapsedMs, (now, r, e) => {
        const left = secondsLeft(endTimeFrom(now, r), now + e);
        expect(left).toBe(Math.max(0, Math.ceil(r - e / 1000)));
        expect(left).toBeGreaterThanOrEqual(0);
        expect(left).toBeLessThanOrEqual(r);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 8: the count never goes up while time passes", () => {
    fc.assert(
      fc.property(nowMs, remaining, elapsedMs, elapsedMs, (now, r, a, b) => {
        const end = endTimeFrom(now, r);
        const [earlier, later] = a <= b ? [a, b] : [b, a];
        expect(secondsLeft(end, now + later)).toBeLessThanOrEqual(secondsLeft(end, now + earlier));
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 8: the formatted text for 0 to 900 seconds is mm:ss and converts back to the same number", () => {
    fc.assert(
      fc.property(remaining, (n) => {
        const text = formatCountdown(n);
        expect(text).toMatch(/^\d{2}:\d{2}$/);
        expect(parseCountdown(text)).toBe(n);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 8: the formatted text round-trips for any whole number of seconds", () => {
    fc.assert(
      fc.property(fc.integer({ min: 0, max: 1_000_000 }), (n) => {
        expect(parseCountdown(formatCountdown(n))).toBe(n);
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 8: the formatted text never holds a negative value or a bad number", () => {
    fc.assert(
      fc.property(
        fc.oneof(
          fc.integer({ min: -1_000_000, max: 1_000_000 }),
          fc.double({ min: -1e6, max: 1e6, noNaN: true }),
          fc.constantFrom(Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY, -0),
        ),
        (n) => {
          const text = formatCountdown(n);
          expect(text).toMatch(/^\d{2,}:[0-5]\d$/);
          expect(text).not.toContain("-");
          if (!(n > 0)) expect(text).toBe("00:00");
        },
      ),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 8: the display depends on the newest view and its arrival time only", () => {
    const arrival = fc.record({ gapMs: fc.integer({ min: 1, max: 60_000 }), remaining });
    fc.assert(
      fc.property(nowMs, fc.array(arrival, { minLength: 1, maxLength: 8 }), elapsedMs, (start, arrivals, e) => {
        let at = start;
        let end = 0;
        for (const a of arrivals) {
          at += a.gapMs;
          end = endTimeFrom(at, a.remaining); // each view replaces the earlier end time
        }
        const last = arrivals[arrivals.length - 1];
        if (!last) throw new Error("the generator gave no arrival");
        expect(secondsLeft(end, at + e)).toBe(Math.max(0, Math.ceil(last.remaining - e / 1000)));
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 8: the spoken time says the same number of seconds as the display", () => {
    fc.assert(
      fc.property(fc.integer({ min: 0, max: 5999 }), (n) => {
        const words = spokenCountdown(n);
        const match = /^(?:(\d+) minutes?)?(?: and )?(?:(\d+) seconds?)?$/.exec(words);
        expect(match, words).not.toBeNull();
        const minutes = Number(match?.[1] ?? 0);
        const seconds = Number(match?.[2] ?? 0);
        expect(minutes * 60 + seconds).toBe(n);
        // Singular for one, plural otherwise.
        if (match?.[1] !== undefined) expect(words.includes(`${minutes} minute${minutes === 1 ? "" : "s"}`)).toBe(true);
        if (match?.[2] !== undefined) expect(words.includes(`${seconds} second${seconds === 1 ? "" : "s"}`)).toBe(true);
        // A part that is zero is left out, except for a count of zero seconds.
        if (n > 0) expect(words).not.toMatch(/\b0 (minute|second)/);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
