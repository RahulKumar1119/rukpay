import { act, render, screen, waitFor, within } from "@testing-library/react";
import fc from "fast-check";
import { describe, expect, it } from "vitest";
import type { ExplanationItem, VaultState, VaultView } from "../lib/api";
import { COPY } from "../lib/copy";
import { guardianPhone, HIGH_BUTTON, locked, presetButton, renderApp, userPhone, view } from "../test/appHarness";
import { ALL_STATES, makeView } from "../test/stubs";
import { GuardianSummary } from "./GuardianSummary";
const HEADING = COPY.explanation.guardianHeading;
const item = (signal: string, points: number, reason = `Reason for ${signal}.`): ExplanationItem => ({ signal, points, reason });
/** The points text, written out here and not taken from the code under test. */
const signed = (n: number): string => `${n < 0 ? "-" : "+"}${Math.abs(n)} ${Math.abs(n) === 1 ? "point" : "points"}`;
const summary = () => screen.getByRole("region", { name: HEADING });
const lines = () => within(summary()).getAllByRole("listitem");
const lockedView = (explanation?: unknown): VaultView =>
  makeView("LOCKED_15M", explanation === undefined ? {} : ({ explanation } as unknown as Partial<VaultView>));
describe("the guardian summary", () => {
  describe("how many lines it holds", () => {
    it("shows one line for one contribution", () => {
      render(<GuardianSummary view={lockedView([item("amount", 35)])} />);
      expect(within(summary()).getByRole("heading", { name: "What RukPay noticed" })).toBeInTheDocument();
      expect(lines().map((line) => line.textContent)).toEqual(["Reason for amount.+35 points"]);
    });
    it("shows three lines for three contributions, the most points first", () => {
      render(<GuardianSummary view={lockedView([item("late_night", 10), item("amount", 35), item("unknown_call", 20)])} />);
      expect(lines().map((line) => line.textContent)).toEqual([
        "Reason for amount.+35 points",
        "Reason for unknown_call.+20 points",
        "Reason for late_night.+10 points",
      ]);
    });
    it("shows only the first three of four or more, and never a fourth", () => {
      const list = [item("a", 5), item("b", 35), item("c", 10), item("d", 20), item("e", 25), item("f", 1)];
      render(<GuardianSummary view={lockedView(list)} />);
      expect(lines().map((line) => line.textContent)).toEqual([
        "Reason for b.+35 points",
        "Reason for e.+25 points",
        "Reason for d.+20 points",
      ]);
    });
    it("keeps equal points in the order received", () => {
      const list = [item("first", 10), item("second", 20), item("third", 10), item("fourth", 20), item("fifth", 10)];
      render(<GuardianSummary view={lockedView(list)} />);
      expect(lines().map((line) => line.textContent)).toEqual([
        "Reason for second.+20 points",
        "Reason for fourth.+20 points",
        "Reason for first.+10 points",
      ]);
    });
    it("leaves out the cap lines and anything at or below 0 points", () => {
      const list = [item("behaviour_cap", -5), item("zero", 0), item("amount", 10), item("score_cap", -8)];
      render(<GuardianSummary view={lockedView(list)} />);
      expect(lines().map((line) => line.textContent)).toEqual(["Reason for amount.+10 points"]);
    });
    it.each([
      ["an empty list", []],
      ["only cap lines", [item("behaviour_cap", -5), item("score_cap", -2)]],
      ["only lines of 0 points", [item("zero", 0)]],
    ])("shows nothing for %s", (_name, list) => {
      const { container } = render(<GuardianSummary view={lockedView(list)} />);
      expect(container).toBeEmptyDOMElement();
    });
  });
  describe("when there is nothing usable", () => {
    it.each([
      ["no `explanation`", undefined],
      ["an `explanation` that is not a list", "text"],
      ["an item with no reason", [{ signal: "a", points: 5 }]],
      ["an item with points that are not whole", [{ signal: "a", points: 2.5, reason: "A reason." }]],
    ])("shows nothing and no error for %s", (_name, explanation) => {
      const { container } = render(<GuardianSummary view={lockedView(explanation)} />);
      expect(container).toBeEmptyDOMElement();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });
  });
  describe("which states show it", () => {
    it.each(ALL_STATES.filter((state) => state !== "LOCKED_15M"))("shows nothing in %s, even with contributions that earned points", (state: VaultState) => {
      const { container } = render(<GuardianSummary view={makeView(state, { explanation: [item("amount", 35)] })} />);
      expect(container).toBeEmptyDOMElement();
    });
    it("shows its lines in LOCKED_15M", () => {
      render(<GuardianSummary view={lockedView([item("amount", 35)])} />);
      expect(lines()).toHaveLength(1);
    });
  });
  describe("what it holds", () => {
    it("holds no signal value and no masked identifier", () => {
      const fixture = makeView("LOCKED_15M", {
        metadata: { amount_inr: 85000, beneficiary_masked: "XXXX-XXXX-9034", upi_masked: "r***@okaxis", phone_masked: "+91-XXXXXX3210" },
        explanation: [item("signal_marker_zq9", 35, "This is a very large payment.")],
      });
      const { container } = render(<GuardianSummary view={fixture} />);
      const text = container.textContent ?? "";
      expect(text).not.toContain("zq9");
      expect(container.innerHTML).not.toContain("zq9");
      for (const masked of ["XXXX-XXXX-9034", "r***@okaxis", "+91-XXXXXX3210"]) expect(container.innerHTML).not.toContain(masked);
      expect(text).not.toMatch(/\d{5,}/);
    });
    it("has no button, link or timer, and keeps its text at 16 pixels or larger", () => {
      render(<GuardianSummary view={lockedView([item("amount", 35)])} />);
      expect(within(summary()).queryAllByRole("button")).toHaveLength(0);
      expect(within(summary()).queryAllByRole("link")).toHaveLength(0);
      for (const part of Array.from(lines()[0]?.children ?? [])) expect(part.className).toMatch(/\btext-(base|lg|xl)\b/);
      expect(within(summary()).getByRole("heading").className).toMatch(/\btext-(lg|xl)\b/);
    });
    it("shows markup as the characters received and never reads it as HTML", () => {
      const reason = '<b>bold</b> <img src=x onerror="alert(1)"> &amp;';
      render(<GuardianSummary view={lockedView([item("amount", 10, reason)])} />);
      expect(lines()[0]?.textContent).toBe(`${reason}+10 points`);
      expect(summary().querySelector("b, img")).toBeNull();
    });
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.7, 4.8, 4.9
  it("Property 32 (guardian shown): for any list, at most 3 lines of positive points, the most first, ties in received order, and nothing outside LOCKED_15M", () => {
    const reason = fc.string({ unit: "grapheme-ascii", minLength: 1, maxLength: 40 }).filter((text) => !text.includes("zq9"));
    // Points from -3 to 4 so that ties, zeros and cap lines are all common.
    const entry = fc.record({ points: fc.integer({ min: -3, max: 4 }), reason });
    fc.assert(
      fc.property(fc.array(entry, { maxLength: 9 }), fc.constantFrom(...ALL_STATES), (list, state) => {
        const explanation = list.map((e, index) => ({ signal: `marker_${index}_zq9`, ...e }));
        // The expected lines, by repeated selection of the earliest of the highest.
        const pool = list.map((e, index) => ({ ...e, index })).filter((e) => e.points > 0);
        const expected: string[] = [];
        while (expected.length < 3 && pool.length > 0) {
          let best = 0;
          pool.forEach((e, k) => {
            const b = pool[best] as { points: number; index: number };
            if (e.points > b.points || (e.points === b.points && e.index < b.index)) best = k;
          });
          const picked = pool.splice(best, 1)[0] as { points: number; reason: string };
          expected.push(`${picked.reason}${signed(picked.points)}`);
        }
        const { container, unmount } = render(<GuardianSummary view={makeView(state, { explanation })} />);
        if (state !== "LOCKED_15M" || expected.length === 0) {
          expect(container).toBeEmptyDOMElement();
        } else {
          expect(lines().map((line) => line.textContent)).toEqual(expected);
          expect(container.innerHTML).not.toContain("zq9");
        }
        unmount();
      }),
      { numRuns: 100 },
    );
  }, 60_000);
});
describe("the guardian summary on the guardian's phone", () => {
  const REASON = "A large transfer to a new account late at night.";
  const FIVE: ExplanationItem[] = [
    item("amount", 35, "This is a very large payment."),
    item("beneficiary_age", 25, "The recipient was added less than a day ago."),
    item("late_night", 10, "The payment is being made late at night."),
    item("unknown_call", 20, "A call from someone you do not know is going on."),
    item("rapid_retries", 5, "The payment has been tried a couple of times in a row."),
  ];
  async function lockApp(extra: Partial<VaultView> = {}) {
    const app = renderApp();
    app.fetchStub.respond(200, locked({ reason: REASON, metadata: { amount_inr: 85000, beneficiary_masked: "XXXX-XXXX-4821" }, ...extra }));
    await app.user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("timer");
    return app;
  }
  it("shows the three largest reasons with their points inside the alert box, under the reason, the amount and the payee", async () => {
    await lockApp({ explanation: FIVE });
    const region = guardianPhone().getByRole("region", { name: HEADING });
    expect(within(region).getAllByRole("listitem").map((line) => line.textContent)).toEqual([
      "This is a very large payment.+35 points",
      "The recipient was added less than a day ago.+25 points",
      "A call from someone you do not know is going on.+20 points",
    ]);
    const box = guardianPhone().getByText(REASON).parentElement as HTMLElement;
    expect(box).toContainElement(region);
    expect(box).toContainElement(guardianPhone().getByText("₹85,000"));
    expect(box).toContainElement(guardianPhone().getByText("XXXX-XXXX-4821"));
    expect(Array.from(box.querySelectorAll("p, section")).pop()).toBe(region);
  });
  it("keeps the existing alert content, and the two action buttons with the same labels and order", async () => {
    await lockApp({ explanation: FIVE });
    const phone = guardianPhone();
    expect(phone.getByText("Please check in with your family member")).toBeInTheDocument();
    expect(phone.getByText(REASON)).toBeInTheDocument();
    expect(phone.getByText("₹85,000")).toBeInTheDocument();
    expect(phone.getByText("XXXX-XXXX-4821")).toBeInTheDocument();
    expect(phone.getByRole("timer")).toHaveTextContent("15:00");
    expect(phone.getAllByRole("button").map((button) => button.textContent)).toEqual([
      "I Spoke With Them — Safe to Approve",
      "Block & Report Scam (1930)",
    ]);
  });
  it("is not on the payer's phone, which shows the whole card instead", async () => {
    await lockApp({ explanation: FIVE });
    expect(userPhone().queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
    expect(userPhone().getByRole("region", { name: COPY.explanation.heading })).toBeInTheDocument();
  });
  it("shows nothing and no error when the vault has no `explanation`", async () => {
    await lockApp();
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(guardianPhone().getByText(REASON)).toBeInTheDocument();
    expect(guardianPhone().getAllByRole("button")).toHaveLength(2);
  });
  it("shows nothing when the explanation is empty, but the payer's phone still shows its calm sentence", async () => {
    await lockApp({ explanation: [] });
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
    expect(userPhone().getByText("Nothing about this payment asked for extra care.")).toBeInTheDocument();
  });
  it("goes when the guardian's alert goes: after an approval", async () => {
    const { ES } = await lockApp({ explanation: FIVE });
    expect(guardianPhone().getByRole("region", { name: HEADING })).toBeInTheDocument();
    act(() => ES.latest().emit(view("GUARDIAN_APPROVED", { settled: true, explanation: FIVE })));
    await guardianPhone().findByText("Payment approved");
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
  });
  it("goes when the payment is blocked", async () => {
    const { ES } = await lockApp({ explanation: FIVE });
    act(() => ES.latest().emit(view("VETOED", { evidence_summary: "Summary.", explanation: FIVE })));
    await guardianPhone().findByText("This payment was stopped");
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
  });
  it("goes when the pause ends", async () => {
    const { ES } = await lockApp({ explanation: FIVE });
    act(() => ES.latest().emit(view("EXPIRED", { reconfirm_required: true, explanation: FIVE })));
    await guardianPhone().findByText("Nothing needs your attention right now.");
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
  });
  it("follows a stream event that changes the explanation", async () => {
    const { ES } = await lockApp({ explanation: FIVE });
    act(() => ES.latest().emit(locked({ reason: REASON, remaining_seconds: 800, explanation: [item("amount", 20, "This is a large payment.")] })));
    await waitFor(() => expect(within(guardianPhone().getByRole("region", { name: HEADING })).getAllByRole("listitem")).toHaveLength(1));
    expect(guardianPhone().getByText("This is a large payment.")).toBeInTheDocument();
  });
});
