import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Answers } from "../lib/api";
import { COPY } from "../lib/copy";
import {
  errorBody,
  guardianPhone,
  locked,
  MEDIUM_BUTTON,
  presetButton,
  renderApp,
  userPhone,
  view,
} from "../test/appHarness";
import { RealityCheckModal } from "./RealityCheckModal";

const [Q1, Q2, Q3] = COPY.realityCheck.questions;

function renderModal(props: Partial<React.ComponentProps<typeof RealityCheckModal>> = {}) {
  const onSubmit = vi.fn<(answers: Answers) => Promise<void>>().mockResolvedValue(undefined);
  const onRetry = vi.fn();
  const user = userEvent.setup();
  const utils = render(<RealityCheckModal open onSubmit={onSubmit} unreachable={false} onRetry={onRetry} {...props} />);
  return { user, onSubmit, onRetry, ...utils };
}

const yes = () => screen.getByRole("button", { name: "Yes" });
const no = () => screen.getByRole("button", { name: "No" });

describe("RealityCheckModal", () => {
  describe("the questions", () => {
    it("shows one question at a time, in the order coercion, secrecy, verification", async () => {
      const { user } = renderModal();

      expect(screen.getByText("Question 1 of 3")).toBeInTheDocument();
      expect(screen.getByText(Q1)).toBeInTheDocument();
      expect(screen.queryByText(Q2)).not.toBeInTheDocument();
      expect(screen.queryByText(Q3)).not.toBeInTheDocument();

      await user.click(yes());
      expect(screen.getByText("Question 2 of 3")).toBeInTheDocument();
      expect(screen.getByText(Q2)).toBeInTheDocument();
      expect(screen.queryByText(Q1)).not.toBeInTheDocument();

      await user.click(no());
      expect(screen.getByText("Question 3 of 3")).toBeInTheDocument();
      expect(screen.getByText(Q3)).toBeInTheDocument();
    });

    it("uses the exact wording from the steering document", () => {
      expect(Q1).toBe(
        "Are you currently on a phone or video call with someone claiming to be from Police, CBI, Customs, RBI, or your Bank?",
      );
      expect(Q2).toBe("Did the caller ask you NOT to tell your family or bank staff about this transfer?");
      expect(Q3).toBe(
        "Have you independently verified this person or bill without using a phone number they just sent you?",
      );
    });

    it("offers a Yes button and a No button for each question", async () => {
      const { user } = renderModal();
      for (let i = 0; i < 2; i++) {
        expect(yes()).toBeInTheDocument();
        expect(no()).toBeInTheDocument();
        await user.click(yes());
      }
      expect(yes()).toBeInTheDocument();
      expect(no()).toBeInTheDocument();
    });

    it("sends the three answers after the third one, and not before", async () => {
      const { user, onSubmit } = renderModal();

      await user.click(yes());
      await user.click(no());
      expect(onSubmit).not.toHaveBeenCalled();

      await user.click(no());
      expect(onSubmit).toHaveBeenCalledTimes(1);
      expect(onSubmit).toHaveBeenCalledWith({ coercion: "YES", secrecy: "NO", verification: "NO" });
    });
  });

  describe("Back", () => {
    it("is not offered on the first question", () => {
      renderModal();
      expect(screen.queryByRole("button", { name: "Back" })).not.toBeInTheDocument();
    });

    it("returns to the earlier question and shows the answer given there", async () => {
      const { user } = renderModal();
      await user.click(yes());
      await user.click(no());

      await user.click(screen.getByRole("button", { name: "Back" }));
      expect(screen.getByText(Q2)).toBeInTheDocument();
      expect(screen.getByText("Your earlier answer: No")).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Back" }));
      expect(screen.getByText(Q1)).toBeInTheDocument();
      expect(screen.getByText("Your earlier answer: Yes")).toBeInTheDocument();
    });

    it("keeps the earlier answers, and a changed answer replaces only its own", async () => {
      const { user, onSubmit } = renderModal();
      await user.click(yes()); // coercion YES
      await user.click(yes()); // secrecy YES
      await user.click(screen.getByRole("button", { name: "Back" })); // back to secrecy
      await user.click(no()); // secrecy becomes NO
      await user.click(no()); // verification NO

      expect(onSubmit).toHaveBeenCalledWith({ coercion: "YES", secrecy: "NO", verification: "NO" });
    });
  });

  describe("the dialog", () => {
    it("is announced as a modal dialog with a title", () => {
      renderModal();
      const dialog = screen.getByRole("dialog");

      expect(dialog).toHaveAttribute("aria-modal", "true");
      expect(dialog).toHaveAccessibleName("Quick safety check");
    });

    it("renders nothing while closed, and forgets its answers when it closes", async () => {
      const { user, rerender, onSubmit, onRetry } = renderModal();
      await user.click(yes());
      expect(screen.getByText(Q2)).toBeInTheDocument();

      rerender(<RealityCheckModal open={false} onSubmit={onSubmit} unreachable={false} onRetry={onRetry} />);
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

      rerender(<RealityCheckModal open onSubmit={onSubmit} unreachable={false} onRetry={onRetry} />);
      expect(screen.getByText(Q1)).toBeInTheDocument();
      expect(screen.queryByText(/Your earlier answer/)).not.toBeInTheDocument();
    });

    it("moves focus to the first answer button when it opens and on each new question", async () => {
      const { user } = renderModal();
      expect(yes()).toHaveFocus();

      await user.click(no());
      expect(screen.getByText(Q2)).toBeInTheDocument();
      expect(yes()).toHaveFocus();

      await user.click(no());
      expect(yes()).toHaveFocus();
    });

    it("keeps Tab and Shift+Tab inside the dialog", async () => {
      const { user } = renderModal();
      await user.click(yes()); // question 2: Yes, No and Back are the controls
      expect(yes()).toHaveFocus();

      await user.tab();
      expect(no()).toHaveFocus();
      await user.tab();
      expect(screen.getByRole("button", { name: "Back" })).toHaveFocus();

      await user.tab(); // from the last control to the first
      expect(yes()).toHaveFocus();

      await user.tab({ shift: true }); // from the first control to the last
      expect(screen.getByRole("button", { name: "Back" })).toHaveFocus();
    });

    it("returns focus to the control that had it before it opened", () => {
      const outside = document.createElement("button");
      document.body.append(outside);
      outside.focus();
      const onSubmit = vi.fn().mockResolvedValue(undefined);
      const { rerender } = render(<RealityCheckModal open onSubmit={onSubmit} unreachable={false} onRetry={() => undefined} />);
      expect(yes()).toHaveFocus();

      rerender(<RealityCheckModal open={false} onSubmit={onSubmit} unreachable={false} onRetry={() => undefined} />);

      expect(outside).toHaveFocus();
      outside.remove();
    });

    it("uses the amber surface and no red", () => {
      renderModal();
      const dialog = screen.getByRole("dialog");

      expect(dialog).toHaveClass("bg-amber-surface", "text-amber-text");
      expect(dialog.outerHTML).not.toMatch(/veto-red|red-\d{3}|#ff0000/i);
    });
  });

  describe("when the server cannot be reached", () => {
    it("shows the notice in the dialog and calls onRetry from its button", async () => {
      const { user, onRetry } = renderModal({ unreachable: true });

      expect(
        screen.getByText("We can't reach RukPay right now. Your money has not moved. Please try again."),
      ).toBeInTheDocument();
      expect(screen.queryByText(Q1)).not.toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Try again" }));
      expect(onRetry).toHaveBeenCalledTimes(1);
    });

    it("keeps the dialog's focus trap working over the notice", async () => {
      const { user } = renderModal({ unreachable: true });
      const retry = screen.getByRole("button", { name: "Try again" });

      await user.tab();
      expect(retry).toHaveFocus();
      await user.tab();
      expect(retry).toHaveFocus();
    });
  });

  describe("with the whole App", () => {
    async function openCheck() {
      const app = renderApp();
      app.fetchStub.respond(200, view("REALITY_CHECK"));
      await app.user.click(presetButton(MEDIUM_BUTTON));
      const dialog = await screen.findByRole("dialog");
      return { ...app, dialog };
    }

    async function answerAll(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement, answers: ("Yes" | "No")[]) {
      for (const name of answers) await user.click(within(dialog).getByRole("button", { name }));
    }

    it("sends the answers and closes on a locked result, with the vault card and the guardian alert", async () => {
      const { user, fetchStub, dialog } = await openCheck();
      fetchStub.respond(200, locked());

      await answerAll(user, dialog, ["Yes", "No", "No"]);

      const sent = fetchStub.calls[fetchStub.calls.length - 1];
      expect(sent).toMatchObject({
        method: "POST",
        body: { answers: { coercion: "YES", secrecy: "NO", verification: "NO" } },
      });
      expect(sent?.url).toContain("/api/v1/vault/vault-app-1/reality-check");

      expect(await userPhone().findByRole("timer")).toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(guardianPhone().getByText("Please check in with your family member")).toBeInTheDocument();
    });

    it("closes on a calm result and shows the Continue payment button", async () => {
      const { user, fetchStub, dialog } = await openCheck();
      fetchStub.respond(200, view("REALITY_CHECK", { settled: true, reason: "Your answers look fine." }));

      await answerAll(user, dialog, ["No", "No", "Yes"]);

      expect(await userPhone().findByRole("button", { name: "Continue payment" })).toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(userPhone().getByText("Your checks look fine.")).toBeInTheDocument();
    });

    it("closes on a conflict and shows the state returned in the 409 body", async () => {
      const { user, fetchStub, dialog } = await openCheck();
      fetchStub.respond(409, {
        ...errorBody("state_conflict", "This payment has already moved on."),
        vault: view("VETOED", { evidence_summary: "Evidence summary text." }),
      });

      await answerAll(user, dialog, ["No", "No", "No"]);

      expect(await userPhone().findByText("This payment was stopped")).toBeInTheDocument();
      expect(userPhone().getByText("Evidence summary text.")).toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    it("keeps the dialog and the answers when the server cannot be reached, and the retry sends the same answers", async () => {
      const { user, fetchStub, dialog } = await openCheck();
      fetchStub.reject();

      await answerAll(user, dialog, ["Yes", "Yes", "No"]);

      expect(await within(dialog).findByText(/We can't reach RukPay right now/)).toBeInTheDocument();
      expect(screen.getByRole("dialog")).toBe(dialog);
      const first = fetchStub.calls[fetchStub.calls.length - 1];
      expect(first?.body).toEqual({ answers: { coercion: "YES", secrecy: "YES", verification: "NO" } });

      fetchStub.respond(200, locked());
      await user.click(within(dialog).getByRole("button", { name: "Try again" }));

      expect(await userPhone().findByRole("timer")).toBeInTheDocument();
      const retried = fetchStub.calls[fetchStub.calls.length - 1];
      expect(retried?.body).toEqual(first?.body);
      expect(retried).not.toBe(first);
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });
});
