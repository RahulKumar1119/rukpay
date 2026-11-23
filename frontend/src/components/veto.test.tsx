import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { guardianPhone, HIGH_BUTTON, locked, presetButton, renderApp, TOKEN, userPhone, view } from "../test/appHarness";

const SUMMARY = "RukPay stopped this payment.\nVault: vault-app-1\nHelpline: 1930";

async function lockApp() {
  const app = renderApp();
  app.fetchStub.respond(200, locked());
  await app.user.click(presetButton(HIGH_BUTTON));
  await userPhone().findByRole("timer");
  return app;
}

/** Blocks from the named phone and waits for both phones to show the evidence card. */
async function block(from: "user" | "guardian") {
  const app = await lockApp();
  app.fetchStub.respond(200, { status: "OK", vault: view("VETOED", { evidence_summary: SUMMARY }) });
  const button =
    from === "user"
      ? userPhone().getByRole("button", { name: "Block & Report (1930)" })
      : guardianPhone().getByRole("button", { name: "Block & Report Scam (1930)" });
  await app.user.click(button);
  await userPhone().findByText("This payment was stopped");
  return app;
}

describe("the veto", () => {
  describe("the request", () => {
    it.each([
      ["the payer's phone", "user"],
      ["the guardian's phone", "guardian"],
    ] as const)("sends VETO with the held token and confirmed true from %s", async (_name, from) => {
      const { fetchStub } = await block(from);

      const sent = fetchStub.calls[fetchStub.calls.length - 1];
      expect(sent).toMatchObject({ method: "POST", body: { action: "VETO", token: TOKEN, confirmed: true } });
      expect(sent?.url).toContain("/api/v1/vault/vault-app-1/guardian-action");
      expect(fetchStub.calls.filter((c) => c.url.endsWith("/guardian-action"))).toHaveLength(1);
    });

    it("needs no second tap, because blocking is always safe", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(200, { status: "OK", vault: view("VETOED", { evidence_summary: SUMMARY }) });

      await user.click(guardianPhone().getByRole("button", { name: "Block & Report Scam (1930)" }));

      await guardianPhone().findByText("This payment was stopped");
      expect(fetchStub.calls.filter((c) => c.url.endsWith("/guardian-action"))).toHaveLength(1);
    });

    it("stops the live updates once the payment is blocked", async () => {
      const { ES } = await block("guardian");
      expect(ES.latest().closed).toBe(true);
    });
  });

  describe("the evidence card", () => {
    it.each([
      ["the payer's phone", userPhone],
      ["the guardian's phone", guardianPhone],
    ] as const)("shows the summary, the cancelled statement and no timer on %s", async (_name, phone) => {
      await block("user");

      expect(phone().getByText("This payment was stopped")).toBeInTheDocument();
      expect(phone().getByText("This transfer has been permanently cancelled. No money has left the account.")).toBeInTheDocument();
      expect(phone().getByText(/RukPay stopped this payment\./)).toBeInTheDocument();
      expect(phone().queryByRole("timer")).not.toBeInTheDocument();
      expect(phone().queryByRole("button", { name: /Block & Report|Approve/ })).not.toBeInTheDocument();
    });

    it("keeps the summary as text a person can select", async () => {
      await block("guardian");

      const summary = userPhone().getByText(/RukPay stopped this payment\./);
      expect(summary).toHaveClass("select-text", "whitespace-pre-wrap");
      expect(summary.textContent).toBe(SUMMARY);
    });

    it("links the helpline number as tel:1930", async () => {
      await block("user");

      const link = userPhone().getByRole("link", { name: "Call 1930, the National Cybercrime Helpline" });
      expect(link).toHaveAttribute("href", "tel:1930");
      expect(link).toHaveTextContent("1930");
    });

    it("links cybercrime.gov.in so that it opens in a new tab without sharing the page", async () => {
      await block("user");

      const link = userPhone().getByRole("link", { name: "Open cybercrime.gov.in in a new tab" });
      expect(link).toHaveAttribute("href", "https://cybercrime.gov.in");
      expect(link).toHaveAttribute("target", "_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
      expect(link.getAttribute("rel")).toContain("noreferrer");
      expect(link).toHaveTextContent("cybercrime.gov.in");
    });

    it("makes both links at least 48 pixels tall by class", async () => {
      await block("user");

      for (const link of userPhone().getAllByRole("link")) expect(link).toHaveClass("min-h-12");
    });
  });

  describe("copying the summary", () => {
    it("copies it and says so, and the message stays", async () => {
      const { user } = await block("user");

      await user.click(userPhone().getByRole("button", { name: "Copy summary" }));

      expect(await userPhone().findByText("Summary copied.")).toBeInTheDocument();
      expect(await navigator.clipboard.readText()).toBe(SUMMARY);
      // Nothing hides it later: it stays until the page changes.
      await new Promise((resolve) => setTimeout(resolve, 50));
      expect(userPhone().getByText("Summary copied.")).toBeInTheDocument();
    });

    it("says to copy by hand when the clipboard is not there, and keeps the summary visible", async () => {
      const { user } = await block("user");
      const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
      Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });

      try {
        await user.click(userPhone().getByRole("button", { name: "Copy summary" }));

        expect(await userPhone().findByText("Please select the text above and copy it by hand.")).toBeInTheDocument();
        expect(userPhone().getByText(/RukPay stopped this payment\./)).toBeInTheDocument();
        expect(userPhone().queryByText("Summary copied.")).not.toBeInTheDocument();
      } finally {
        if (original) Object.defineProperty(navigator, "clipboard", original);
      }
    });

    it("says to copy by hand when the clipboard refuses", async () => {
      const { user } = await block("user");
      vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error("denied"));

      await user.click(userPhone().getByRole("button", { name: "Copy summary" }));

      expect(await userPhone().findByText("Please select the text above and copy it by hand.")).toBeInTheDocument();
      expect(userPhone().queryByText("Summary copied.")).not.toBeInTheDocument();
    });

    it("keeps the summary in the page only", async () => {
      const setItem = vi.spyOn(Storage.prototype, "setItem");
      const { user } = await block("user");
      await user.click(userPhone().getByRole("button", { name: "Copy summary" }));
      await userPhone().findByText("Summary copied.");

      expect(setItem).not.toHaveBeenCalled();
      expect(document.cookie).toBe("");
      expect(window.location.href).not.toContain("vault");
      await waitFor(() => expect(localStorage.length).toBe(0));
      expect(sessionStorage.length).toBe(0);
    });
  });

  it("shows a calm note, not a broken card, when a blocked vault has no summary", async () => {
    const { user, fetchStub } = await lockApp();
    fetchStub.respond(200, { status: "OK", vault: view("VETOED", { evidence_summary: undefined }) });

    await user.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));

    expect(await userPhone().findByText("The summary is not ready yet. You can still call 1930.")).toBeInTheDocument();
    expect(userPhone().queryByRole("button", { name: "Copy summary" })).not.toBeInTheDocument();
    expect(userPhone().getByRole("link", { name: "Call 1930, the National Cybercrime Helpline" })).toBeInTheDocument();
  });
});
