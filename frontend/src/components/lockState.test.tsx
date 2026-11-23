import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { errorBody, guardianPhone, HIGH_BUTTON, locked, presetButton, renderApp, TOKEN, userPhone, view } from "../test/appHarness";

async function lockApp(extra: Parameters<typeof locked>[0] = {}) {
  const app = renderApp();
  app.fetchStub.respond(200, locked(extra));
  await app.user.click(presetButton(HIGH_BUTTON));
  await userPhone().findByRole("timer");
  return app;
}

describe("the locked state", () => {
  describe("the vault card", () => {
    it("shows the protective pause, a vault icon heading and the countdown in mm:ss", async () => {
      await lockApp();

      const phone = userPhone();
      expect(phone.getByRole("heading", { name: "Safety pause is on" })).toBeInTheDocument();
      expect(phone.getByText("We're holding this safely for 15 minutes so you have space to verify.")).toBeInTheDocument();
      expect(phone.getByRole("timer")).toHaveTextContent("15:00");
    });

    it("puts the countdown in a timer with a label that spells the time out", async () => {
      await lockApp({ remaining_seconds: 845 });

      const timer = userPhone().getByRole("timer");
      expect(timer).toHaveTextContent("14:05");
      expect(timer).toHaveAccessibleName("Time left: 14 minutes and 5 seconds");
    });

    it("counts down once a second", async () => {
      await lockApp({ remaining_seconds: 900 });

      await waitFor(() => expect(userPhone().getByRole("timer")).toHaveTextContent("14:59"), { timeout: 3000 });
    });

    it("shows the demo button, and the payer's red Block & Report button", async () => {
      await lockApp();

      expect(userPhone().getByRole("button", { name: "Demo: skip the wait" })).toBeInTheDocument();
      const block = userPhone().getByRole("button", { name: "Block & Report (1930)" });
      expect(block).toHaveClass("bg-veto-red", "text-white", "min-h-12");
    });

    it("uses the deep teal palette", async () => {
      await lockApp();

      const card = userPhone().getByRole("region", { name: "Safety pause is on" });
      expect(card).toHaveClass("bg-vault-teal");
    });
  });

  describe("the guardian's phone", () => {
    it("shows the alert with the reason, the amount, the masked payee and the countdown", async () => {
      await lockApp({
        reason: "A large transfer to a new account late at night.",
        metadata: { amount_inr: 85000, beneficiary_masked: "XXXX-XXXX-4821" },
      });

      const phone = guardianPhone();
      expect(phone.getByText("Please check in with your family member")).toBeInTheDocument();
      expect(phone.getByText("A large transfer to a new account late at night.")).toBeInTheDocument();
      expect(phone.getByText("₹85,000")).toBeInTheDocument();
      expect(phone.getByText("XXXX-XXXX-4821")).toBeInTheDocument();
      expect(phone.getByRole("timer")).toHaveTextContent("15:00");
      expect(phone.getByRole("button", { name: "I Spoke With Them — Safe to Approve" })).toBeInTheDocument();
      expect(phone.getByRole("button", { name: "Block & Report Scam (1930)" })).toBeInTheDocument();
    });

    it("never shows a full account number, a raw UPI id or the guardian token", async () => {
      await lockApp();

      const text = document.body.textContent ?? "";
      expect(text).not.toContain(TOKEN);
      expect(document.body.innerHTML).not.toContain(TOKEN);
      expect(text).not.toMatch(/\b\d{9,18}\b/);
    });
  });

  describe("the demo fast-forward", () => {
    it("posts to the fast-forward route and shows the shorter countdown", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(200, locked({ remaining_seconds: 3 }));

      await user.click(userPhone().getByRole("button", { name: "Demo: skip the wait" }));

      const sent = fetchStub.calls[fetchStub.calls.length - 1];
      expect(sent).toMatchObject({ method: "POST", body: undefined });
      expect(sent?.url).toContain("/api/v1/vault/vault-app-1/demo-fast-forward");
      await waitFor(() => expect(userPhone().getByRole("timer")).toHaveTextContent("00:03"));
      expect(guardianPhone().getByRole("timer")).toHaveTextContent("00:03");
    });

    it("hides the button for the rest of the session after a 404, even for a new payment", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(404, errorBody("not_found", "We could not find that payment."));

      await user.click(userPhone().getByRole("button", { name: "Demo: skip the wait" }));

      await waitFor(() => expect(userPhone().queryByRole("button", { name: "Demo: skip the wait" })).not.toBeInTheDocument());
      expect(userPhone().getByRole("timer")).toBeInTheDocument();

      fetchStub.respond(200, locked({ vault_id: "vault-app-2" }));
      await user.click(presetButton(HIGH_BUTTON));
      await waitFor(() => expect(userPhone().getByRole("timer")).toBeInTheDocument());
      expect(userPhone().queryByRole("button", { name: "Demo: skip the wait" })).not.toBeInTheDocument();
    });
  });

  describe("the end of the pause", () => {
    it("reads the vault once at 00:00 and shows the state the server returns", async () => {
      const { fetchStub } = await lockApp({ remaining_seconds: 1 });
      fetchStub.respond(200, view("EXPIRED", { reconfirm_required: true, reason: "The pause is over." }));

      expect(await userPhone().findByRole("button", { name: "Yes, this is my payment" }, { timeout: 4000 })).toBeInTheDocument();

      const reads = fetchStub.calls.filter((c) => c.method === "GET");
      expect(reads).toHaveLength(1);
      expect(reads[0]?.url).toContain("/api/v1/vault/vault-app-1");
      expect(userPhone().queryByRole("timer")).not.toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
    });
  });

  describe("Block & Report on the payer's phone", () => {
    it("sends the VETO body with the token held from the vault, and shows the evidence card", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(200, {
        status: "OK",
        vault: view("VETOED", { evidence_summary: "Evidence summary for the helpline." }),
      });

      await user.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));

      const sent = fetchStub.calls[fetchStub.calls.length - 1];
      expect(sent).toMatchObject({ method: "POST", body: { action: "VETO", token: TOKEN, confirmed: true } });
      expect(sent?.url).toContain("/api/v1/vault/vault-app-1/guardian-action");
      expect(await userPhone().findByText("This payment was stopped")).toBeInTheDocument();
      expect(userPhone().getByText("Evidence summary for the helpline.")).toBeInTheDocument();
      expect(userPhone().queryByRole("timer")).not.toBeInTheDocument();
    });

    it("keeps the token after the event stream sends a view that has none", async () => {
      const { user, fetchStub, ES } = await lockApp();
      ES.latest().emit(locked({ guardian_token: undefined, remaining_seconds: 800 }));
      await waitFor(() => expect(userPhone().getByRole("timer")).toHaveTextContent("13:2"));
      fetchStub.respond(200, { status: "OK", vault: view("VETOED", { evidence_summary: "x" }) });

      await user.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));

      expect(fetchStub.calls[fetchStub.calls.length - 1]?.body).toEqual({ action: "VETO", token: TOKEN, confirmed: true });
      await screen.findAllByText("This payment was stopped");
    });

    it("sends the request once when the button is tapped twice quickly", async () => {
      const { user, fetchStub } = await lockApp();
      const pending = fetchStub.defer();
      const block = userPhone().getByRole("button", { name: "Block & Report (1930)" });
      const before = fetchStub.calls.length;

      await user.dblClick(block);
      pending.respond(200, { status: "OK", vault: view("VETOED", { evidence_summary: "x" }) });

      await screen.findAllByText("This payment was stopped");
      expect(fetchStub.calls.length - before).toBe(1);
    });
  });
});
