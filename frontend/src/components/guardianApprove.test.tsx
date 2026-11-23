import { waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { errorBody, guardianPhone, HIGH_BUTTON, locked, presetButton, renderApp, TOKEN, userPhone, view } from "../test/appHarness";

const APPROVE = "I Spoke With Them — Safe to Approve";
const CONFIRM = "Are you sure you spoke with them directly?";

async function lockApp() {
  const app = renderApp();
  app.fetchStub.respond(200, locked());
  await app.user.click(presetButton(HIGH_BUTTON));
  await guardianPhone().findByRole("button", { name: APPROVE });
  return app;
}

const approveRequests = (calls: { url: string }[]) => calls.filter((c) => c.url.endsWith("/guardian-action"));

describe("the guardian's approval", () => {
  it("sends nothing on the first tap and asks one more question", async () => {
    const { user, fetchStub } = await lockApp();
    const before = fetchStub.calls.length;

    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));

    expect(fetchStub.calls).toHaveLength(before);
    expect(guardianPhone().getByText(CONFIRM)).toBeInTheDocument();
    expect(guardianPhone().getByRole("button", { name: "Yes, approve" })).toBeInTheDocument();
    expect(guardianPhone().getByRole("button", { name: "Go back" })).toBeInTheDocument();
    expect(guardianPhone().queryByRole("button", { name: APPROVE })).not.toBeInTheDocument();
  });

  it("sends APPROVE with the token and confirmed true on the second tap, then shows both phones approved", async () => {
    const { user, fetchStub } = await lockApp();
    fetchStub.respond(200, { status: "OK", vault: view("GUARDIAN_APPROVED", { settled: true }) });

    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
    await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));

    const sent = approveRequests(fetchStub.calls);
    expect(sent).toHaveLength(1);
    expect(sent[0]).toMatchObject({ method: "POST", body: { action: "APPROVE", token: TOKEN, confirmed: true } });
    expect(sent[0]?.url).toContain("/api/v1/vault/vault-app-1/guardian-action");

    expect(await guardianPhone().findByText("Payment approved")).toBeInTheDocument();
    expect(guardianPhone().queryByRole("timer")).not.toBeInTheDocument();
    expect(userPhone().getByText("Payment approved by your family")).toBeInTheDocument();
    expect(userPhone().getByText("Your family guardian approved this payment. It is on its way.")).toBeInTheDocument();
    expect(userPhone().queryByRole("timer")).not.toBeInTheDocument();
  });

  it("returns to the two buttons on Go back and sends nothing", async () => {
    const { user, fetchStub } = await lockApp();
    const before = fetchStub.calls.length;

    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
    await user.click(guardianPhone().getByRole("button", { name: "Go back" }));

    expect(fetchStub.calls).toHaveLength(before);
    expect(guardianPhone().queryByText(CONFIRM)).not.toBeInTheDocument();
    expect(guardianPhone().getByRole("button", { name: APPROVE })).toBeInTheDocument();
    expect(guardianPhone().getByRole("button", { name: "Block & Report Scam (1930)" })).toBeInTheDocument();
  });

  it("keeps Block & Report within reach on the second step", async () => {
    const { user } = await lockApp();

    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));

    expect(guardianPhone().getByRole("button", { name: "Block & Report Scam (1930)" })).toBeInTheDocument();
  });

  it("keeps the second step on screen when the answer asks for confirmation", async () => {
    const { user, fetchStub } = await lockApp();
    fetchStub.respond(200, { status: "CONFIRMATION_REQUIRED", vault: locked() });

    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
    await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));

    await waitFor(() => expect(approveRequests(fetchStub.calls)).toHaveLength(1));
    expect(guardianPhone().getByText(CONFIRM)).toBeInTheDocument();
    expect(guardianPhone().getByRole("button", { name: "Yes, approve" })).toBeInTheDocument();
    expect(guardianPhone().getByRole("timer")).toBeInTheDocument();
  });

  it("sends the request once when Yes, approve is tapped twice quickly", async () => {
    const { user, fetchStub } = await lockApp();
    const pending = fetchStub.defer();

    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
    await user.dblClick(guardianPhone().getByRole("button", { name: "Yes, approve" }));
    pending.respond(200, { status: "OK", vault: view("GUARDIAN_APPROVED", { settled: true }) });

    await guardianPhone().findByText("Payment approved");
    expect(approveRequests(fetchStub.calls)).toHaveLength(1);
  });

  describe("when the link is no longer valid (HTTP 403)", () => {
    it("says so calmly and offers Refresh, which reads the vault again and clears the message", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(403, errorBody("forbidden", "This approval link is not valid for this payment, or it has run out."));

      await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
      await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));

      expect(await guardianPhone().findByText("This approval link is no longer valid. Please refresh to see the latest.")).toBeInTheDocument();
      const refresh = guardianPhone().getByRole("button", { name: "Refresh" });

      fetchStub.respond(200, locked({ remaining_seconds: 700 }));
      await user.click(refresh);

      await waitFor(() =>
        expect(guardianPhone().queryByText("This approval link is no longer valid. Please refresh to see the latest.")).not.toBeInTheDocument(),
      );
      const read = fetchStub.calls[fetchStub.calls.length - 1];
      expect(read).toMatchObject({ method: "GET" });
      expect(read?.url).toContain("/api/v1/vault/vault-app-1");
      expect(guardianPhone().getByRole("timer")).toHaveTextContent("11:4");
    });

    it("does not change the vault or show the evidence card", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(403, errorBody("forbidden", "x"));

      await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
      await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));

      await guardianPhone().findByRole("button", { name: "Refresh" });
      expect(guardianPhone().getByRole("timer")).toBeInTheDocument();
      expect(userPhone().getByRole("timer")).toBeInTheDocument();
    });
  });

  describe("when someone already acted (HTTP 409)", () => {
    it("applies the returned view on both phones and shows the already-acted message", async () => {
      const { user, fetchStub } = await lockApp();
      fetchStub.respond(409, {
        ...errorBody("state_conflict", "This payment has already moved on."),
        vault: view("VETOED", { evidence_summary: "Evidence summary text." }),
      });

      await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
      await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));

      expect(await guardianPhone().findByText("Someone already acted on this payment.")).toBeInTheDocument();
      expect(guardianPhone().getByText("This payment was stopped")).toBeInTheDocument();
      expect(guardianPhone().getByText("Evidence summary text.")).toBeInTheDocument();
      expect(userPhone().getByText("This payment was stopped")).toBeInTheDocument();
      expect(guardianPhone().queryByRole("button", { name: APPROVE })).not.toBeInTheDocument();
    });
  });

  it("starts the approval steps again for a new vault", async () => {
    const { user, fetchStub } = await lockApp();
    await user.click(guardianPhone().getByRole("button", { name: APPROVE }));
    expect(guardianPhone().getByText(CONFIRM)).toBeInTheDocument();

    fetchStub.respond(200, locked({ vault_id: "vault-app-2" }));
    await user.click(presetButton(HIGH_BUTTON));

    await waitFor(() => expect(guardianPhone().queryByText(CONFIRM)).not.toBeInTheDocument());
    expect(guardianPhone().getByRole("button", { name: APPROVE })).toBeInTheDocument();
  });
});
