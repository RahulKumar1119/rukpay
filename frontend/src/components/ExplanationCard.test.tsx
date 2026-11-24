import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import fc from "fast-check";
import { describe, expect, it } from "vitest";
import type { ExplanationItem, VaultView } from "../lib/api";
import { COPY } from "../lib/copy";
import { HIGH_BUTTON, locked, MEDIUM_BUTTON, presetButton, renderApp, SAFE_BUTTON, userPhone, view } from "../test/appHarness";
import { makeView } from "../test/stubs";
import { ExplanationCard } from "./ExplanationCard";
/** The five baseline lines of a high-risk payment, in the order the backend sends them. */
const FIVE: ExplanationItem[] = [
  { signal: "amount", points: 35, reason: "This is a very large payment." },
  { signal: "beneficiary_age", points: 25, reason: "The recipient was added less than a day ago." },
  { signal: "late_night", points: 10, reason: "The payment is being made late at night." },
  { signal: "unknown_call", points: 20, reason: "A call from someone you do not know is going on." },
  { signal: "rapid_retries", points: 5, reason: "The payment has been tried a couple of times in a row." },
];
const HEADING = COPY.explanation.heading;
const card = () => screen.getByRole("region", { name: HEADING });
const lines = () => within(card()).getAllByRole("listitem");
/** The points text, written out here and not taken from the code under test. */
const signed = (n: number): string => `${n < 0 ? "-" : "+"}${Math.abs(n)} ${Math.abs(n) === 1 ? "point" : "points"}`;
describe("the explanation card", () => {
  describe("with a list of contributions", () => {
    it("shows one line for each, with its reason and its points as text, in the order received", () => {
      render(<ExplanationCard view={makeView("LOCKED_15M", { risk_score: 95, explanation: FIVE })} />);
      expect(card()).toBeInTheDocument();
      expect(within(card()).getByRole("heading", { name: "Why RukPay looked closer" })).toBeInTheDocument();
      expect(lines().map((line) => line.textContent)).toEqual([
        "This is a very large payment.+35 points",
        "The recipient was added less than a day ago.+25 points",
        "The payment is being made late at night.+10 points",
        "A call from someone you do not know is going on.+20 points",
        "The payment has been tried a couple of times in a row.+5 points",
      ]);
      expect(screen.getByText("+35 points")).toBeInTheDocument();
      expect(screen.getByText("+20 points")).toBeInTheDocument();
    });
    it("does not sort the lines: a smaller number can come before a larger one", () => {
      render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: [FIVE[2] as ExplanationItem, FIVE[0] as ExplanationItem] })} />);
      expect(lines().map((line) => line.textContent)).toEqual([
        "The payment is being made late at night.+10 points",
        "This is a very large payment.+35 points",
      ]);
    });
    it("ends with a total line that is the risk score of the same view, not a sum worked out in the browser", () => {
      // The five lines add up to 95. The view says 77, so a browser that adds would show 95.
      render(<ExplanationCard view={makeView("LOCKED_15M", { risk_score: 77, explanation: FIVE })} />);
      const total = within(card()).getByText("Total: 77 points");
      expect(total).toHaveClass("text-xl");
      expect(within(card()).queryByText(/Total: 95/)).not.toBeInTheDocument();
      // The total comes after every line.
      const everything = Array.from(card().querySelectorAll("li, p"));
      expect(everything[everything.length - 1]).toBe(total);
    });
    it("says 'point' for a score of 1 and 'points' for 0 and for any other score", () => {
      const one = render(<ExplanationCard view={makeView("LOCKED_15M", { risk_score: 1, explanation: [FIVE[4] as ExplanationItem] })} />);
      expect(screen.getByText("Total: 1 point")).toBeInTheDocument();
      one.unmount();
      render(<ExplanationCard view={makeView("LOCKED_15M", { risk_score: 0, explanation: [FIVE[4] as ExplanationItem] })} />);
      expect(screen.getByText("Total: 0 points")).toBeInTheDocument();
    });
    it("shows each points number exactly as received, with a plus sign for 0 and 'point' for 1", () => {
      const items: ExplanationItem[] = [
        { signal: "a", points: 1, reason: "One." },
        { signal: "b", points: 7, reason: "Seven." },
        { signal: "c", points: 100, reason: "Hundred." },
      ];
      render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: items })} />);
      expect(lines().map((line) => line.textContent)).toEqual(["One.+1 point", "Seven.+7 points", "Hundred.+100 points"]);
    });
    it("never shows a signal value, as text or in any attribute", () => {
      const items = FIVE.map((item, index) => ({ ...item, signal: `signal_marker_${index}_zq9` }));
      const { container } = render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: items })} />);
      expect(container.textContent).not.toContain("zq9");
      expect(container.innerHTML).not.toContain("zq9");
    });
    it("has no toggle, button, timer or dismiss control, and its heading is a labelled region", () => {
      render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: FIVE })} />);
      expect(within(card()).queryAllByRole("button")).toHaveLength(0);
      expect(within(card()).queryAllByRole("link")).toHaveLength(0);
      expect(within(card()).queryByRole("timer")).not.toBeInTheDocument();
      expect(card().querySelector("[aria-expanded], [aria-pressed], details, summary")).toBeNull();
    });
    it("hides its icon from screen readers, and keeps every line at 16 pixels or larger", () => {
      render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: FIVE })} />);
      for (const icon of Array.from(card().querySelectorAll("svg"))) expect(icon).toHaveAttribute("aria-hidden", "true");
      for (const line of lines()) {
        for (const part of Array.from(line.children)) expect(part.className).toMatch(/\btext-(base|lg|xl)\b/);
      }
    });
  });
  describe("with the cap lines", () => {
    it("shows a behaviour cap and a score cap as their own lines, in the position received, with a minus sign", () => {
      const items: ExplanationItem[] = [
        { signal: "amount", points: 35, reason: "This is a very large payment." },
        { signal: "velocity", points: 15, reason: "A lot of payments were made in the last few minutes." },
        { signal: "daily_frequency", points: 15, reason: "There have been a lot of payments in the last day." },
        { signal: "behaviour_cap", points: -5, reason: "The extra points from recent activity have an upper limit." },
        { signal: "beneficiary_care", points: 10, reason: "This recipient is new or has not been checked yet." },
        { signal: "score_cap", points: -8, reason: "The total cannot go above its upper limit." },
      ];
      render(<ExplanationCard view={makeView("LOCKED_15M", { risk_score: 100, explanation: items })} />);
      expect(lines().map((line) => line.textContent)).toEqual([
        "This is a very large payment.+35 points",
        "A lot of payments were made in the last few minutes.+15 points",
        "There have been a lot of payments in the last day.+15 points",
        "The extra points from recent activity have an upper limit.-5 points",
        "This recipient is new or has not been checked yet.+10 points",
        "The total cannot go above its upper limit.-8 points",
      ]);
      expect(screen.getByText("-5 points")).toBeInTheDocument();
      expect(screen.getByText("-8 points")).toBeInTheDocument();
      expect(screen.getByText("Total: 100 points")).toBeInTheDocument();
    });
    it("shows a minus one as '-1 point'", () => {
      render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: [{ signal: "score_cap", points: -1, reason: "The total cannot go above its upper limit." }] })} />);
      expect(screen.getByText("-1 point")).toBeInTheDocument();
    });
  });
  describe("with an empty list", () => {
    it("shows the one calm sentence, no line and no total", () => {
      render(<ExplanationCard view={makeView("INSTANT_PASS", { risk_score: 0, explanation: [] })} />);
      expect(within(card()).getByText("Nothing about this payment asked for extra care.")).toBeInTheDocument();
      expect(within(card()).queryAllByRole("listitem")).toHaveLength(0);
      expect(within(card()).queryByRole("list")).not.toBeInTheDocument();
      expect(card().textContent).not.toMatch(/Total|point/);
    });
  });
  describe("with no usable explanation", () => {
    it("shows no card and no error when the view has no `explanation`", () => {
      const { container } = render(<ExplanationCard view={makeView("LOCKED_15M")} />);
      expect(container).toBeEmptyDOMElement();
      expect(screen.queryByRole("region")).not.toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });
    it.each([
      ["not a list", "text"],
      ["null", null],
      ["an item with no signal", [{ points: 5, reason: "A reason." }]],
      ["an item whose points are not whole", [{ signal: "a", points: 1.5, reason: "A reason." }]],
      ["an item whose points are text", [{ signal: "a", points: "5", reason: "A reason." }]],
      ["an item with an empty reason", [{ signal: "a", points: 5, reason: "" }]],
      ["one bad item among good ones", [{ signal: "a", points: 5, reason: "Good." }, { signal: "b", points: 5 }]],
    ])("shows no card and no error for %s", (_name, explanation) => {
      // The server's body is not checked on arrival, so a wrong shape can reach the card.
      const { container } = render(<ExplanationCard view={makeView("LOCKED_15M", { explanation } as unknown as Partial<VaultView>)} />);
      expect(container).toBeEmptyDOMElement();
    });
  });
  describe("with markup in a reason", () => {
    it("shows the characters as received and never reads them as HTML", () => {
      const reason = '<b>bold</b> &amp; <img src=x onerror="alert(1)"> <script>alert(2)</script>';
      render(<ExplanationCard view={makeView("LOCKED_15M", { explanation: [{ signal: "amount", points: 10, reason }] })} />);
      expect(lines()[0]?.textContent).toBe(`${reason}+10 points`);
      expect(card().querySelector("b, img, script")).toBeNull();
    });
  });
  // Feature: risk-intelligence, Property 32: The explanation is read and shown as received
  // Validates: Requirements 4.1, 4.4, 4.5, 4.9
  it("Property 32 (shown): for any list, one line per item in order, the points as a sign and words, the total from the score, and no signal", () => {
    const reason = fc.string({ unit: "grapheme-ascii", minLength: 1, maxLength: 60 }).filter((text) => !text.includes("zq9"));
    const item = fc.record({ points: fc.integer({ min: -1000, max: 1000 }), reason });
    fc.assert(
      fc.property(fc.array(item, { minLength: 1, maxLength: 8 }), fc.integer({ min: 0, max: 100 }), (list, score) => {
        const explanation = list.map((entry, index) => ({ signal: `marker_${index}_zq9`, ...entry }));
        const { container, unmount } = render(<ExplanationCard view={makeView("LOCKED_15M", { risk_score: score, explanation })} />);
        const shown = lines();
        expect(shown).toHaveLength(list.length);
        shown.forEach((line, index) => {
          const entry = list[index] as { points: number; reason: string };
          expect(line.textContent).toBe(`${entry.reason}${signed(entry.points)}`);
        });
        expect(within(card()).getByText(`Total: ${score} ${score === 1 ? "point" : "points"}`)).toBeInTheDocument();
        expect(container.innerHTML).not.toContain("zq9");
        expect(card().querySelectorAll("li")).toHaveLength(list.length);
        unmount();
      }),
      { numRuns: 100 },
    );
  }, 60_000);
});
describe("the explanation card on the payer's phone", () => {
  it("shows the card with the evaluate answer, and the guardian's phone does not show it", async () => {
    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, locked({ risk_score: 90, explanation: FIVE }));
    await user.click(presetButton(HIGH_BUTTON));
    const phone = userPhone();
    const region = await phone.findByRole("region", { name: HEADING });
    expect(within(region).getAllByRole("listitem")).toHaveLength(5);
    expect(within(region).getByText("Total: 90 points")).toBeInTheDocument();
    // The card follows the state block of the phone.
    expect(phone.getByRole("region", { name: "Safety pause is on" }).compareDocumentPosition(region) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getAllByRole("region", { name: HEADING })).toHaveLength(1);
  });
  it("shows the calm sentence for the safe payment, which earns no points", async () => {
    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, view("INSTANT_PASS", { risk_score: 0, explanation: [] }));
    await user.click(presetButton(SAFE_BUTTON));
    const region = await userPhone().findByRole("region", { name: HEADING });
    expect(within(region).getByText("Nothing about this payment asked for extra care.")).toBeInTheDocument();
    expect(within(region).queryAllByRole("listitem")).toHaveLength(0);
  });
  it("shows no card and no error for an answer with no `explanation`, and every other part stays as it was", async () => {
    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, locked({ reason: "A large transfer to a new recipient while you are on a call." }));
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("timer");
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(userPhone().getByText("A large transfer to a new recipient while you are on a call.")).toBeInTheDocument();
    expect(userPhone().getByRole("region", { name: "Safety pause is on" })).toBeInTheDocument();
    expect(userPhone().getByRole("button", { name: "Block & Report (1930)" })).toBeInTheDocument();
  });
  it("follows a stream event, and a vault read when the stream fails", async () => {
    const { user, fetchStub, ES } = renderApp();
    fetchStub.respond(200, locked({ risk_score: 90, explanation: FIVE }));
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("region", { name: HEADING });
    const streamed: ExplanationItem[] = [{ signal: "amount", points: 20, reason: "This is a large payment." }];
    act(() => ES.latest().emit(locked({ risk_score: 20, remaining_seconds: 800, explanation: streamed })));
    await waitFor(() => expect(within(card()).getAllByRole("listitem")).toHaveLength(1));
    expect(within(card()).getByText("This is a large payment.")).toBeInTheDocument();
    expect(within(card()).getByText("Total: 20 points")).toBeInTheDocument();
    const polled: ExplanationItem[] = [{ signal: "late_night", points: 10, reason: "The payment is being made late at night." }];
    fetchStub.respond(200, locked({ risk_score: 10, remaining_seconds: 700, explanation: polled }));
    act(() => ES.latest().fail());
    await waitFor(() => expect(within(card()).getByText("The payment is being made late at night.")).toBeInTheDocument());
    expect(within(card()).getByText("Total: 10 points")).toBeInTheDocument();
  });
  it("stays through every state change of the same vault", async () => {
    const { user, fetchStub, ES } = renderApp();
    fetchStub.respond(200, locked({ risk_score: 95, explanation: FIVE }));
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("region", { name: HEADING });
    const expected = FIVE.map((item) => `${item.reason}${signed(item.points)}`);
    // The pause ends and the payer has to confirm.
    act(() => ES.latest().emit(view("EXPIRED", { risk_score: 95, reconfirm_required: true, explanation: FIVE })));
    await userPhone().findByText("One last check");
    expect(lines().map((line) => line.textContent)).toEqual(expected);
    // The payer confirms.
    fetchStub.respond(200, view("EXPIRED", { risk_score: 95, reconfirm_required: false, settled: true, explanation: FIVE }));
    fireEvent.click(userPhone().getByRole("button", { name: "Yes, this is my payment" }));
    await userPhone().findByText("Payment confirmed");
    expect(lines().map((line) => line.textContent)).toEqual(expected);
  });
  it("stays when the guardian approves and when the vault is blocked", async () => {
    const approved = renderApp();
    approved.fetchStub.respond(200, locked({ risk_score: 95, explanation: FIVE }));
    await approved.user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("region", { name: HEADING });
    act(() => approved.ES.latest().emit(view("GUARDIAN_APPROVED", { settled: true, risk_score: 95, explanation: FIVE })));
    await userPhone().findByText("Payment approved by your family");
    expect(lines()).toHaveLength(5);
  });
  it("stays when the payment is blocked", async () => {
    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, locked({ risk_score: 95, explanation: FIVE }));
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("region", { name: HEADING });
    fetchStub.respond(200, { status: "OK", vault: view("VETOED", { risk_score: 95, evidence_summary: "Summary.", explanation: FIVE }) });
    await user.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));
    await userPhone().findByText("This payment was stopped");
    expect(lines()).toHaveLength(5);
  });
  it("is removed as soon as a new scenario starts, and the new vault shows its own card", async () => {
    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, locked({ risk_score: 95, explanation: FIVE }));
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("region", { name: HEADING });
    const pending = fetchStub.defer();
    await user.click(presetButton(MEDIUM_BUTTON));
    // The old vault is gone while the new answer is on its way.
    expect(screen.queryByRole("region", { name: HEADING })).not.toBeInTheDocument();
    const fresh: ExplanationItem[] = [{ signal: "amount", points: 20, reason: "This is a large payment." }];
    pending.respond(200, view("INSTANT_PASS", { risk_score: 20, explanation: fresh }));
    const region = await userPhone().findByRole("region", { name: HEADING });
    expect(within(region).getAllByRole("listitem")).toHaveLength(1);
    expect(within(region).getByText("This is a large payment.")).toBeInTheDocument();
  });
  it("does not disappear after a long time, because nothing hides it", async () => {
    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, locked({ risk_score: 95, explanation: FIVE }));
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("region", { name: HEADING });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1500));
    });
    expect(lines()).toHaveLength(5);
  });
});
