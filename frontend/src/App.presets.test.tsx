import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PRESETS } from "./lib/presets";
import {
  guardianPhone,
  HIGH_BUTTON,
  locked,
  MEDIUM_BUTTON,
  presetButton,
  renderApp,
  SAFE_BUTTON,
  userPhone,
  view,
} from "./test/appHarness";

describe("App presets", () => {
  describe("the safe daily payment", () => {
    it("sends the safe request and shows the payment sent", async () => {
      const { user, fetchStub, ES } = renderApp();
      fetchStub.respond(200, view("INSTANT_PASS", { reason: "This payment looks like your usual one." }));

      await user.click(presetButton(SAFE_BUTTON));

      expect(fetchStub.calls).toHaveLength(1);
      expect(fetchStub.calls[0]).toMatchObject({ method: "POST", body: PRESETS[0].request });
      expect(await userPhone().findByText("Payment sent")).toBeInTheDocument();
      expect(userPhone().getByText("Your payment has been sent. Nothing looked unusual.")).toBeInTheDocument();
      expect(userPhone().getByText("This payment looks like your usual one.")).toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
      // A payment that is already settled needs no live updates.
      expect(ES.instances).toHaveLength(0);
    });

    it("shows the payee as a label and a masked identifier, and the amount in rupees", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.respond(200, view("INSTANT_PASS", { metadata: { amount_inr: 450, upi_masked: "s***@okaxis" } }));

      await user.click(presetButton(SAFE_BUTTON));

      expect(await userPhone().findByText("Local store")).toBeInTheDocument();
      expect(userPhone().getByText("s***@okaxis")).toBeInTheDocument();
      expect(userPhone().getByText("₹450")).toBeInTheDocument();
      expect(userPhone().getByText("6:30 PM")).toBeInTheDocument();
    });
  });

  describe("the medium-risk transfer", () => {
    it("sends the medium request and opens the reality check", async () => {
      const { user, fetchStub, ES } = renderApp();
      fetchStub.respond(200, view("REALITY_CHECK", { metadata: { amount_inr: 18000, beneficiary_masked: "XXXX-XXXX-3307" } }));

      await user.click(presetButton(MEDIUM_BUTTON));

      expect(fetchStub.calls[0]).toMatchObject({ method: "POST", body: PRESETS[1].request });
      const dialog = await screen.findByRole("dialog");
      expect(dialog).toHaveAccessibleName("Quick safety check");
      expect(screen.getByText("Question 1 of 3")).toBeInTheDocument();
      expect(userPhone().getByText("New contact")).toBeInTheDocument();
      expect(userPhone().getByText("XXXX-XXXX-3307")).toBeInTheDocument();
      expect(userPhone().getByText("₹18,000")).toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
      // The vault can still change, so the live updates start.
      expect(ES.instances).toHaveLength(1);
    });
  });

  describe("the high-risk panic transfer", () => {
    it("sends the high-risk request, shows the vault card and raises the guardian alert", async () => {
      const { user, fetchStub, ES } = renderApp();
      fetchStub.respond(
        200,
        locked({
          reason: "A large transfer to a new recipient while you are on a call.",
          metadata: { amount_inr: 85000, beneficiary_masked: "XXXX-XXXX-4821" },
        }),
      );

      await user.click(presetButton(HIGH_BUTTON));

      expect(fetchStub.calls[0]).toMatchObject({ method: "POST", body: PRESETS[2].request });

      const timer = await userPhone().findByRole("timer");
      expect(timer).toHaveTextContent("15:00");
      expect(userPhone().getByText("Your money is safe in the vault")).toBeInTheDocument();
      expect(userPhone().getByText("We're holding this safely for 15 minutes so you have space to verify.")).toBeInTheDocument();
      expect(userPhone().getByText("New account")).toBeInTheDocument();
      expect(userPhone().getByText("11:45 PM")).toBeInTheDocument();
      expect(userPhone().getByText("₹85,000")).toBeInTheDocument();

      // The guardian sees the same vault at once.
      expect(guardianPhone().getByText("Please check in with your family member")).toBeInTheDocument();
      expect(guardianPhone().getByText("A large transfer to a new recipient while you are on a call.")).toBeInTheDocument();
      expect(guardianPhone().getByText("₹85,000")).toBeInTheDocument();
      expect(guardianPhone().getByText("XXXX-XXXX-4821")).toBeInTheDocument();
      expect(guardianPhone().getByRole("timer")).toBeInTheDocument();
      expect(guardianPhone().getByRole("button", { name: "I Spoke With Them — Safe to Approve" })).toBeInTheDocument();
      expect(ES.instances).toHaveLength(1);
      expect(ES.latest().url).toContain("vault_id=vault-app-1");
    });
  });

  describe("while a payment is being checked", () => {
    it("disables all three buttons and says so until the answer arrives", async () => {
      const { user, fetchStub } = renderApp();
      const pending = fetchStub.defer();

      await user.click(presetButton(SAFE_BUTTON));

      for (const name of [SAFE_BUTTON, MEDIUM_BUTTON, HIGH_BUTTON]) {
        expect(presetButton(name)).toBeDisabled();
      }
      expect(screen.getByText("Checking this payment…")).toBeInTheDocument();

      pending.respond(200, view("INSTANT_PASS"));

      expect(await userPhone().findByText("Payment sent")).toBeInTheDocument();
      for (const name of [SAFE_BUTTON, MEDIUM_BUTTON, HIGH_BUTTON]) {
        expect(presetButton(name)).toBeEnabled();
      }
      expect(screen.queryByText("Checking this payment…")).not.toBeInTheDocument();
    });
  });

  describe("choosing another preset", () => {
    it("discards the old vault and closes the old stream, then starts the new scenario", async () => {
      const { user, fetchStub, ES } = renderApp();
      fetchStub.respond(200, locked());
      await user.click(presetButton(HIGH_BUTTON));
      await userPhone().findByRole("timer");
      const oldStream = ES.latest();
      expect(guardianPhone().getByRole("button", { name: "Block & Report Scam (1930)" })).toBeInTheDocument();

      fetchStub.respond(200, view("INSTANT_PASS", { vault_id: "vault-app-2" }));
      await user.click(presetButton(SAFE_BUTTON));

      expect(await userPhone().findByText("Payment sent")).toBeInTheDocument();
      expect(oldStream.closed).toBe(true);
      expect(userPhone().queryByRole("timer")).not.toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
      expect(guardianPhone().queryByRole("button", { name: "Block & Report Scam (1930)" })).not.toBeInTheDocument();
    });

    it("cancels a request still waiting for the old payment and ignores its answer", async () => {
      const { user, fetchStub } = renderApp();
      fetchStub.respond(200, view("REALITY_CHECK", { vault_id: "vault-old" }));
      await user.click(presetButton(MEDIUM_BUTTON));
      const dialog = await screen.findByRole("dialog");

      // The three answers are sent, and the answer to them is slow.
      const slow = fetchStub.defer();
      for (let i = 0; i < 3; i++) await user.click(within(dialog).getByRole("button", { name: "No" }));
      expect(await within(dialog).findByText("Checking your answers…")).toBeInTheDocument();
      const sent = fetchStub.calls[fetchStub.calls.length - 1];
      expect(sent?.url).toContain("/vault/vault-old/reality-check");

      // A new payment starts, then the old answer finally arrives.
      fetchStub.respond(200, view("INSTANT_PASS", { vault_id: "vault-new" }));
      await user.click(presetButton(SAFE_BUTTON));
      expect(await userPhone().findByText("Payment sent")).toBeInTheDocument();
      expect(sent?.signal?.aborted).toBe(true);

      slow.respond(200, locked({ vault_id: "vault-old" }));
      await new Promise((resolve) => setTimeout(resolve, 20));

      expect(userPhone().getByText("Payment sent")).toBeInTheDocument();
      expect(userPhone().queryByRole("timer")).not.toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
    });
  });

  describe("the page itself", () => {
    it("asks for no login, sign-up or setup", () => {
      renderApp();

      expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
      expect(screen.queryByLabelText(/password|email|username/i)).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /log ?in|sign ?(in|up)|register/i })).not.toBeInTheDocument();
      expect(screen.getAllByRole("button")).toHaveLength(3);
    });

    it("shows the three presets first, then the payer's phone, then the guardian's phone", () => {
      renderApp();
      const bar = screen.getByRole("group", { name: "Choose a payment to try" });
      const payer = screen.getByRole("region", { name: "Your phone" });
      const guardian = screen.getByRole("region", { name: "Family guardian's phone" });

      expect(bar.compareDocumentPosition(payer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(payer.compareDocumentPosition(guardian) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it("puts the phones in a row from 1024 pixels up, and in a column below", () => {
      renderApp();
      const payer = screen.getByRole("region", { name: "Your phone" });
      const row = payer.parentElement?.parentElement;

      expect(row).toHaveClass("flex", "flex-col", "lg:flex-row");
    });

    it("tells the user to choose a payment before anything is chosen", () => {
      renderApp();

      expect(userPhone().getByText("Choose a payment above to see how RukPay keeps it safe.")).toBeInTheDocument();
      expect(guardianPhone().getByText("Nothing needs your attention right now.")).toBeInTheDocument();
    });
  });
});
