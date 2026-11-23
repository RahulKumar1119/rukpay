import fc from "fast-check";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PBT_RUNS } from "../test/pbt";
import { view } from "../test/reducerArbitraries";
import { installEventSourceStub, installFetchStub, makeView } from "../test/stubs";
import { isTerminal } from "./screens";
import { POLL_INTERVAL_MS, VaultSync } from "./vaultSync";

const ID = "vault-a";

// A run of Views for one vault, in any state. Some end the vault and some do not.
const views = fc.array(view(ID), { minLength: 1, maxLength: 10 });

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("vault sync properties", () => {
  // Feature: dual-screen-simulator, Property 7: Terminal states stop the sync (the VaultSync half)
  // Validates: Requirements 8.7
  it("Property 7: after the first final View on the stream, nothing is open or scheduled", async () => {
    await fc.assert(
      fc.asyncProperty(views, async (sequence) => {
        const ES = installEventSourceStub();
        const fetchStub = installFetchStub();
        const onView = vi.fn();
        const sync = new VaultSync(ID, { onView, onUnreachable: vi.fn() });
        sync.start();

        try {
          const stream = ES.latest();
          const firstFinal = sequence.findIndex(isTerminal);
          for (const v of sequence) stream.emit(v);
          stream.fail();
          await vi.advanceTimersByTimeAsync(10 * POLL_INTERVAL_MS);

          if (firstFinal === -1) {
            // No final View: every View got through, and the stream fell back to polling.
            expect(onView).toHaveBeenCalledTimes(sequence.length);
          } else {
            expect(onView).toHaveBeenCalledTimes(firstFinal + 1);
            expect(stream.closed).toBe(true);
            expect(vi.getTimerCount()).toBe(0);
            expect(fetchStub.calls).toHaveLength(0);
          }
        } finally {
          sync.dispose();
        }
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 7: after the first final View from a poll, no request is sent and no timer is set", async () => {
    await fc.assert(
      fc.asyncProperty(views, async (sequence) => {
        const ES = installEventSourceStub();
        const fetchStub = installFetchStub();
        sequence.forEach((v) => fetchStub.respond(200, v));
        const onView = vi.fn();
        const sync = new VaultSync(ID, { onView, onUnreachable: vi.fn() });
        sync.start();

        try {
          ES.latest().fail();
          await vi.advanceTimersByTimeAsync(0);
          for (let i = 1; i < sequence.length; i++) await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
          await vi.advanceTimersByTimeAsync(10 * POLL_INTERVAL_MS);

          const firstFinal = sequence.findIndex(isTerminal);
          if (firstFinal === -1) {
            expect(fetchStub.calls.length).toBeGreaterThanOrEqual(sequence.length);
          } else {
            expect(onView).toHaveBeenCalledTimes(firstFinal + 1);
            expect(fetchStub.calls).toHaveLength(firstFinal + 1);
            expect(vi.getTimerCount()).toBe(0);
          }
        } finally {
          sync.dispose();
        }
      }),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 7: dispose leaves nothing open or scheduled, wherever the sync is", async () => {
    await fc.assert(
      fc.asyncProperty(fc.constantFrom("stream", "polling", "waiting"), async (where) => {
        const ES = installEventSourceStub();
        const fetchStub = installFetchStub().respond(200, makeView("LOCKED_15M", { vault_id: ID }));
        const sync = new VaultSync(ID, { onView: vi.fn(), onUnreachable: vi.fn() });
        sync.start();
        if (where !== "stream") ES.latest().fail();
        if (where === "waiting") await vi.advanceTimersByTimeAsync(0);

        sync.dispose();
        sync.dispose();
        // The aborted request clears its own timeout once the abort reaches it.
        await vi.advanceTimersByTimeAsync(0);

        expect(ES.latest().closed).toBe(true);
        expect(vi.getTimerCount()).toBe(0);
        const before = fetchStub.calls.length;
        await vi.advanceTimersByTimeAsync(60_000);
        expect(fetchStub.calls).toHaveLength(before);
      }),
      { numRuns: PBT_RUNS },
    );
  });
});
