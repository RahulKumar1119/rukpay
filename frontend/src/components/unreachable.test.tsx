import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PRESETS } from "../lib/presets";
import {
  errorBody,
  guardianPhone,
  HIGH_BUTTON,
  locked,
  presetButton,
  renderApp,
  SAFE_BUTTON,
  userPhone,
  view,
} from "../test/appHarness";

const MESSAGE = "We can't reach RukPay right now. Your money has not moved. Please try again.";

const retryButton = () => screen.getByRole("button", { name: "Try again" });

afterEach(() => {
  vi.useRealTimers();
});

describe("the unreachable notice", () => {
  describe("when a payment cannot be started", () => {
    it("shows the exact message and a Try again button, and money has not moved", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.reject();

      await user.click(presetButton(HIGH_BUTTON));

      expect(await userPhone().findByText(MESSAGE)).toBeInTheDocument();
      expect(userPhone().getByRole("button", { name: "Try again" })).toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
      // The preset buttons work again, so another payment can be chosen.
      expect(presetButton(HIGH_BUTTON)).toBeEnabled();
    });

    it("repeats the same request when Try again is pressed, and shows the result", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.reject();
      await user.click(presetButton(HIGH_BUTTON));
      await userPhone().findByText(MESSAGE);

      fetchStub.respond(200, locked());
      await user.click(retryButton());

      expect(await userPhone().findByRole("timer")).toBeInTheDocument();
      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();
      expect(fetchStub.calls).toHaveLength(2);
      expect(fetchStub.calls[1]).toMatchObject({ method: "POST", body: PRESETS[2].request });
      expect(fetchStub.calls[1]?.url).toBe(fetchStub.calls[0]?.url);
    });

    it("stays on screen while the retry fails again, and goes only when a retry works", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.reject();
      await user.click(presetButton(HIGH_BUTTON));
      await userPhone().findByText(MESSAGE);

      fetchStub.reject();
      await user.click(retryButton());
      await waitFor(() => expect(fetchStub.calls).toHaveLength(2));
      expect(await userPhone().findByText(MESSAGE)).toBeInTheDocument();

      fetchStub.respond(200, locked());
      await user.click(retryButton());
      await userPhone().findByRole("timer");
      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();
    });

    it("goes away when the person chooses a new payment", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.reject();
      await user.click(presetButton(HIGH_BUTTON));
      await userPhone().findByText(MESSAGE);

      const pending = fetchStub.defer();
      await user.click(presetButton(SAFE_BUTTON));

      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();
      pending.respond(200, view("INSTANT_PASS"));
      expect(await userPhone().findByText("Payment sent")).toBeInTheDocument();
    });
  });

  describe("other answers from the server", () => {
    it("shows the server's calm message for HTTP 400, and not the unreachable notice", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.respond(400, errorBody("invalid_field", "Please check the amount and try again."));

      await user.click(presetButton(HIGH_BUTTON));

      expect(await userPhone().findByText("Please check the amount and try again.")).toBeInTheDocument();
      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
      expect(presetButton(HIGH_BUTTON)).toBeEnabled();
    });

    it.each([500, 503])("shows the unreachable notice for HTTP %i", async (status) => {
      const { user, fetchStub } = renderApp();
      fetchStub.respond(status, errorBody("internal", "Something went wrong on our side. Please try again in a moment."));

      await user.click(presetButton(HIGH_BUTTON));

      expect(await userPhone().findByText(MESSAGE)).toBeInTheDocument();
      expect(userPhone().queryByText(/Something went wrong on our side/)).not.toBeInTheDocument();
    });

    it("shows the unreachable notice for an answer that cannot be read", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.respondRaw(200, "<html>bad gateway</html>");

      await user.click(presetButton(HIGH_BUTTON));

      expect(await userPhone().findByText(MESSAGE)).toBeInTheDocument();
    });

    it("uses no alarming words in the notice", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.reject();

      await user.click(presetButton(HIGH_BUTTON));

      const notice = (await userPhone().findByText(MESSAGE)).closest("[role='status']");
      expect(notice?.textContent).not.toMatch(/error|fail|fatal|breach|illegal/i);
    });
  });

  describe("when a request from a locked payment cannot be sent", () => {
    it("repeats the same request on Try again", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.respond(200, locked());
      await user.click(presetButton(HIGH_BUTTON));
      await userPhone().findByRole("timer");

      fetchStub.reject();
      await user.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));
      await userPhone().findByText(MESSAGE);
      const failed = fetchStub.calls[fetchStub.calls.length - 1];

      fetchStub.respond(200, { status: "OK", vault: view("VETOED", { evidence_summary: "Summary." }) });
      await user.click(retryButton());

      expect(await userPhone().findByText("This payment was stopped")).toBeInTheDocument();
      const retried = fetchStub.calls[fetchStub.calls.length - 1];
      expect(retried?.body).toEqual(failed?.body);
      expect(retried?.url).toBe(failed?.url);
      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();
    });
  });

  describe("when the live updates are lost", () => {
    async function lockWithFakeTimers() {
      vi.useFakeTimers();
      const app = renderApp();
      app.fetchStub.respond(200, locked());
      fireEvent.click(presetButton(HIGH_BUTTON));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(userPhone().getByRole("timer")).toBeInTheDocument();
      return app;
    }

    const advance = (ms: number) =>
      act(async () => {
        await vi.advanceTimersByTimeAsync(ms);
      });

    it("shows the notice until a poll works again, and keeps polling every 2 seconds", async () => {
      const { ES, fetchStub } = await lockWithFakeTimers();
      fetchStub.reject().reject().respond(200, locked({ remaining_seconds: 850 })).respond(200, locked({ remaining_seconds: 848 }));

      act(() => ES.latest().fail());
      await advance(0);
      expect(userPhone().getByText(MESSAGE)).toBeInTheDocument();

      await advance(2000);
      expect(userPhone().getByText(MESSAGE)).toBeInTheDocument();

      await advance(2000);
      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();
      expect(userPhone().getByRole("timer")).toHaveTextContent("14:1");

      await advance(2000);
      expect(fetchStub.calls.filter((c) => c.method === "GET")).toHaveLength(4);
    });

    it("lets Try again read the vault at once", async () => {
      const { ES, fetchStub } = await lockWithFakeTimers();
      fetchStub.reject().respond(200, locked({ remaining_seconds: 840 }));

      act(() => ES.latest().fail());
      await advance(0);
      expect(userPhone().getByText(MESSAGE)).toBeInTheDocument();

      fireEvent.click(userPhone().getByRole("button", { name: "Try again" }));
      await advance(0);

      expect(fetchStub.calls.filter((c) => c.method === "GET")).toHaveLength(2);
      expect(userPhone().getByRole("timer")).toHaveTextContent("14:00");
    });

    it("does not let a working poll hide a request that failed and was never retried", async () => {
      const { ES, fetchStub } = await lockWithFakeTimers();

      // The payer's Block & Report cannot be sent.
      fetchStub.reject();
      fireEvent.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));
      await advance(0);
      expect(userPhone().getByText(MESSAGE)).toBeInTheDocument();

      // The stream drops, and the first poll works.
      fetchStub.respond(200, locked({ remaining_seconds: 800 }));
      act(() => ES.latest().fail());
      await advance(0);

      expect(userPhone().getByText(MESSAGE)).toBeInTheDocument();
    });

    it("shows the notice when a request takes longer than 10 seconds", async () => {
      vi.useFakeTimers();
      const { fetchStub } = renderApp();
      fetchStub.hang();

      fireEvent.click(presetButton(HIGH_BUTTON));
      await advance(9_999);
      expect(userPhone().queryByText(MESSAGE)).not.toBeInTheDocument();

      await advance(1);
      expect(userPhone().getByText(MESSAGE)).toBeInTheDocument();
    });
  });
});
