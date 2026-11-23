import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { installEventSourceStub, installFetchStub, makeView, StubEventSource } from "../test/stubs";
import { API_BASE, eventStreamUrl, REQUEST_TIMEOUT_MS } from "./api";
import { POLL_INTERVAL_MS, VaultSync, type SyncHandlers } from "./vaultSync";

const ID = "vault-1";
const locked = (remaining = 600) => makeView("LOCKED_15M", { vault_id: ID, remaining_seconds: remaining });
const errorBody = { error: { code: "internal", message: "x" } };

function makeHandlers(getSeq?: () => number) {
  const onView = vi.fn();
  const onUnreachable = vi.fn();
  const handlers: SyncHandlers = { onView, onUnreachable, getSeq };
  return { handlers, onView, onUnreachable };
}

/** Lets pending promises settle and moves the fake clock forward. */
const advance = async (ms: number): Promise<void> => {
  await vi.advanceTimersByTimeAsync(ms);
};

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("VaultSync event stream", () => {
  it("opens one stream at the event URL for the vault", () => {
    const ES = installEventSourceStub();
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);

    sync.start();
    sync.start();

    expect(ES.instances).toHaveLength(1);
    expect(ES.latest().url).toBe(eventStreamUrl(ID));
    expect(ES.latest().url).toBe(`${API_BASE}/api/v1/events/stream?vault_id=${ID}`);
  });

  it("passes every parsed View to the owner as a stream View", () => {
    const ES = installEventSourceStub();
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();

    const first = locked(600);
    const second = locked(590);
    ES.latest().emit(first);
    ES.latest().emit(second);

    expect(onView).toHaveBeenCalledTimes(2);
    expect(onView).toHaveBeenNthCalledWith(1, first, { source: "stream" });
    expect(onView).toHaveBeenNthCalledWith(2, second, { source: "stream" });
  });

  it.each([
    ["text that is not JSON", "not json"],
    ["JSON null", "null"],
    ["a JSON number", "42"],
    ["a JSON array", "[]"],
    ["an object with no vault_id", '{"state":"LOCKED_15M"}'],
    ["an object with an empty vault_id", '{"vault_id":""}'],
    ["an object with a vault_id that is not text", '{"vault_id":7}'],
  ])("ignores %s and keeps the stream open", (_name, data) => {
    const ES = installEventSourceStub();
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();

    ES.latest().emit(data);

    expect(onView).not.toHaveBeenCalled();
    expect(ES.latest().closed).toBe(false);
  });

  it("discards a View that belongs to another vault", () => {
    const ES = installEventSourceStub();
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();

    ES.latest().emit(makeView("VETOED", { vault_id: "someone-else" }));

    expect(onView).not.toHaveBeenCalled();
    expect(ES.latest().closed).toBe(false);
  });

  it("uses an injected EventSource constructor", () => {
    const seen: string[] = [];
    class Custom extends StubEventSource {
      constructor(url: string | URL) {
        super(url);
        seen.push(String(url));
      }
    }
    const { handlers } = makeHandlers();

    new VaultSync(ID, handlers, { EventSourceCtor: Custom as unknown as typeof EventSource }).start();

    expect(seen).toEqual([eventStreamUrl(ID)]);
  });
});

describe("VaultSync polling", () => {
  it("falls back to polling the moment the stream fails, and closes the stream", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().respond(200, locked(580));
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();

    ES.latest().fail();
    await advance(0);

    expect(ES.latest().closed).toBe(true);
    expect(fetchStub.calls).toHaveLength(1);
    expect(fetchStub.calls[0]).toMatchObject({ method: "GET", url: `${API_BASE}/api/v1/vault/${ID}` });
    expect(onView).toHaveBeenCalledWith(locked(580), { source: "poll", startedAtSeq: undefined });
  });

  it("polls without a stream when the browser has no EventSource", async () => {
    const fetchStub = installFetchStub().respond(200, locked());
    const { handlers, onView } = makeHandlers();

    new VaultSync(ID, handlers).start();
    await advance(0);

    expect(fetchStub.calls).toHaveLength(1);
    expect(onView).toHaveBeenCalledTimes(1);
  });

  it("sends the next poll two seconds after the last one finished", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().respond(200, locked(580)).respond(200, locked(578)).respond(200, locked(576));
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();
    ES.latest().fail();

    await advance(0);
    expect(fetchStub.calls).toHaveLength(1);

    await advance(POLL_INTERVAL_MS - 1);
    expect(fetchStub.calls).toHaveLength(1);
    await advance(1);
    expect(fetchStub.calls).toHaveLength(2);

    await advance(POLL_INTERVAL_MS);
    expect(fetchStub.calls).toHaveLength(3);
    expect(onView).toHaveBeenCalledTimes(3);
  });

  it("sends no second poll while the first one is still waiting", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().hang();
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();

    await advance(REQUEST_TIMEOUT_MS - 1000);

    expect(fetchStub.calls).toHaveLength(1);
    sync.dispose();
  });

  it("counts the two seconds from when a slow poll ends", async () => {
    const ES = installEventSourceStub();
    // The first poll never answers, so it ends at the 10-second timeout.
    const fetchStub = installFetchStub().hang().respond(200, locked());
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();

    await advance(REQUEST_TIMEOUT_MS + POLL_INTERVAL_MS - 1);
    expect(fetchStub.calls).toHaveLength(1);

    await advance(1);
    expect(fetchStub.calls).toHaveLength(2);
    sync.dispose();
  });

  it("gives each poll the owner's counter value from the moment it was sent", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().respond(200, locked(580)).respond(200, locked(578));
    let seq = 4;
    const { handlers, onView } = makeHandlers(() => seq);
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();

    await advance(0);
    seq = 9;
    await advance(POLL_INTERVAL_MS);

    expect(fetchStub.calls).toHaveLength(2);
    expect(onView).toHaveBeenNthCalledWith(1, locked(580), { source: "poll", startedAtSeq: 4 });
    expect(onView).toHaveBeenNthCalledWith(2, locked(578), { source: "poll", startedAtSeq: 9 });
    sync.dispose();
  });

  it("keeps polling after a failure and reports the loss once, then the recovery once", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub()
      .respond(200, locked(580))
      .reject()
      .respond(503, errorBody)
      .respond(200, locked(570))
      .respond(200, locked(568));
    const { handlers, onView, onUnreachable } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();

    await advance(0); // works
    expect(onUnreachable).not.toHaveBeenCalled();

    await advance(POLL_INTERVAL_MS); // fails
    expect(onUnreachable).toHaveBeenCalledTimes(1);
    expect(onUnreachable).toHaveBeenLastCalledWith(true);

    await advance(POLL_INTERVAL_MS); // fails again, nothing new to report
    expect(onUnreachable).toHaveBeenCalledTimes(1);

    await advance(POLL_INTERVAL_MS); // works again
    expect(onUnreachable).toHaveBeenCalledTimes(2);
    expect(onUnreachable).toHaveBeenLastCalledWith(false);

    await advance(POLL_INTERVAL_MS);
    expect(onUnreachable).toHaveBeenCalledTimes(2);
    expect(fetchStub.calls).toHaveLength(5);
    expect(onView).toHaveBeenCalledTimes(3);
    sync.dispose();
  });

  it("treats a poll that answers for another vault as a working poll that carries nothing", async () => {
    const ES = installEventSourceStub();
    installFetchStub().reject().respond(200, makeView("LOCKED_15M", { vault_id: "someone-else" }));
    const { handlers, onView, onUnreachable } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();

    await advance(0);
    await advance(POLL_INTERVAL_MS);

    expect(onUnreachable.mock.calls).toEqual([[true], [false]]);
    expect(onView).not.toHaveBeenCalled();
    sync.dispose();
  });
});

describe("VaultSync stopping", () => {
  it.each([
    ["INSTANT_PASS", makeView("INSTANT_PASS", { vault_id: ID })],
    ["GUARDIAN_APPROVED", makeView("GUARDIAN_APPROVED", { vault_id: ID })],
    ["VETOED", makeView("VETOED", { vault_id: ID })],
    ["EXPIRED with no confirmation needed", makeView("EXPIRED", { vault_id: ID, reconfirm_required: false })],
  ])("stops on a final %s View from the stream", async (_name, final) => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub();
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();

    ES.latest().emit(final);
    ES.latest().emit(locked());
    ES.latest().fail();
    await advance(60_000);

    expect(onView).toHaveBeenCalledTimes(1);
    expect(ES.latest().closed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    expect(fetchStub.calls).toHaveLength(0);
  });

  it("keeps going when an expired vault still needs the payer's confirmation", () => {
    const ES = installEventSourceStub();
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();

    ES.latest().emit(makeView("EXPIRED", { vault_id: ID, reconfirm_required: true }));

    expect(onView).toHaveBeenCalledTimes(1);
    expect(ES.latest().closed).toBe(false);
  });

  it("stops polling on a final View from a poll", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub()
      .respond(200, locked(580))
      .respond(200, makeView("VETOED", { vault_id: ID }))
      .respond(200, locked(500));
    const { handlers, onView } = makeHandlers();
    new VaultSync(ID, handlers).start();
    ES.latest().fail();

    await advance(0);
    await advance(POLL_INTERVAL_MS);
    await advance(60_000);

    expect(fetchStub.calls).toHaveLength(2);
    expect(onView).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not poll when the stream closes after a final View", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub();
    const { handlers } = makeHandlers();
    new VaultSync(ID, handlers).start();

    ES.latest().emit(makeView("GUARDIAN_APPROVED", { vault_id: ID }));
    ES.latest().fail();
    await advance(10_000);

    expect(fetchStub.calls).toHaveLength(0);
  });

  it("polls when the stream closes after a View that is not final", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().respond(200, locked(500));
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();

    ES.latest().emit(locked(580));
    ES.latest().fail();
    await advance(0);

    expect(fetchStub.calls).toHaveLength(1);
    sync.dispose();
  });
});

describe("VaultSync dispose", () => {
  it("closes the stream, and ignores later events", () => {
    const ES = installEventSourceStub();
    const { handlers, onView } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();

    sync.dispose();
    ES.latest().emit(locked());

    expect(ES.latest().closed).toBe(true);
    expect(onView).not.toHaveBeenCalled();
  });

  it("clears the poll timer", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().respond(200, locked(580)).respond(200, locked(578));
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();
    await advance(0);
    expect(vi.getTimerCount()).toBe(1);

    sync.dispose();

    expect(vi.getTimerCount()).toBe(0);
    await advance(60_000);
    expect(fetchStub.calls).toHaveLength(1);
  });

  it("aborts a poll in flight and reports nothing about it", async () => {
    const ES = installEventSourceStub();
    const fetchStub = installFetchStub().hang();
    const { handlers, onView, onUnreachable } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();
    ES.latest().fail();
    await advance(0);
    expect(fetchStub.calls[0]?.signal?.aborted).toBe(false);

    sync.dispose();
    // Cancelled at once, not by the 10-second timeout that would end it later.
    expect(fetchStub.calls[0]?.signal?.aborted).toBe(true);

    await advance(60_000);

    expect(fetchStub.calls).toHaveLength(1);
    expect(onView).not.toHaveBeenCalled();
    expect(onUnreachable).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("can be called again and before start without harm", () => {
    const ES = installEventSourceStub();
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);

    sync.dispose();
    sync.dispose();
    sync.start();

    expect(ES.instances).toHaveLength(0);
  });

  it("cannot be started again after it is disposed", () => {
    const ES = installEventSourceStub();
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers);
    sync.start();

    sync.dispose();
    sync.start();

    expect(ES.instances).toHaveLength(1);
  });

  it("uses injected timer functions", async () => {
    const ES = installEventSourceStub();
    installFetchStub().respond(200, locked());
    const set = vi.fn((callback: () => void, ms: number) => setTimeout(callback, ms));
    const clear = vi.fn((handle: ReturnType<typeof setTimeout>) => clearTimeout(handle));
    const { handlers } = makeHandlers();
    const sync = new VaultSync(ID, handlers, { setTimeoutFn: set, clearTimeoutFn: clear });
    sync.start();
    ES.latest().fail();
    await advance(0);

    expect(set).toHaveBeenCalledWith(expect.any(Function), POLL_INTERVAL_MS);

    sync.dispose();
    expect(clear).toHaveBeenCalledTimes(1);
  });
});
