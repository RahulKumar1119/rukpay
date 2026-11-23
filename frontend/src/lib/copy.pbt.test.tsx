import { fireEvent, screen, waitFor } from "@testing-library/react";
import fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  errorBody,
  locked,
  MEDIUM_BUTTON,
  presetButton,
  renderApp,
  SAFE_BUTTON,
  userPhone,
  guardianPhone,
  view,
} from "../test/appHarness";
import { bannedTermsIn, BANNED_TERMS } from "../test/bannedTerms";
import { PBT_RUNS } from "../test/pbt";
import type { VaultView } from "./api";
import { ALL_COPY, COPY } from "./copy";

/** Everything a person or a screen reader can read in `root`: its text, and every aria-label, title and alt. */
function readableStrings(root: HTMLElement): string[] {
  const strings = [root.textContent ?? ""];
  root.querySelectorAll("[aria-label], [title], [alt]").forEach((el) => {
    for (const name of ["aria-label", "title", "alt"]) {
      const value = el.getAttribute(name);
      if (value) strings.push(value);
    }
  });
  return strings;
}

// One fixture for every state, and both settings of the flags that change a screen.
const FIXTURES: [string, VaultView][] = [
  ["INSTANT_PASS", view("INSTANT_PASS")],
  ["REALITY_CHECK with the questions open", view("REALITY_CHECK")],
  ["REALITY_CHECK with the checks passed", view("REALITY_CHECK", { settled: true })],
  ["LOCKED_15M", locked()],
  ["GUARDIAN_APPROVED", view("GUARDIAN_APPROVED", { settled: true })],
  ["VETOED", view("VETOED")],
  ["EXPIRED that needs the final confirmation", view("EXPIRED", { reconfirm_required: true })],
  ["EXPIRED that is done", view("EXPIRED", { reconfirm_required: false, settled: true })],
];

describe("calm copy properties", () => {
  // Feature: dual-screen-simulator, Property 15: Copy is calm
  // Validates: Requirements 13.1, 13.2
  it("Property 15: no string in ALL_COPY holds a banned term", () => {
    for (const text of ALL_COPY) expect(bannedTermsIn(text), text).toEqual([]);

    fc.assert(
      fc.property(fc.constantFrom(...ALL_COPY), (text) => bannedTermsIn(text).length === 0),
      { numRuns: PBT_RUNS },
    );
  });

  it("Property 15: ALL_COPY holds every string in COPY, so no text escapes the check", () => {
    const strings: string[] = [];
    const walk = (value: unknown): void => {
      if (typeof value === "string") strings.push(value);
      else if (Array.isArray(value)) value.forEach(walk);
      else if (typeof value === "object" && value !== null) Object.values(value).forEach(walk);
    };
    walk(COPY);

    expect(strings.length).toBeGreaterThan(50);
    for (const text of strings) expect(ALL_COPY).toContain(text);
    expect(ALL_COPY).toContain("Question 1 of 3");
    expect(ALL_COPY).toContain("Question 3 of 3");
  });

  it("Property 15: the check itself finds each banned term, in any letter case", () => {
    for (const term of BANNED_TERMS) {
      expect(bannedTermsIn(`Something ${term.toUpperCase()} here`)).toContain(term);
      expect(bannedTermsIn(`Something ${term.toLowerCase()} here`)).toContain(term);
    }
  });

  it("describes the lock as a protective pause, and never as an accusation or a restriction", () => {
    const accusing = /\b(accus\w*|suspect\w*|restrict\w*|frozen|freez\w*|fraudster|illegal|violation|criminal)\b/i;
    for (const text of ALL_COPY) expect(text, text).not.toMatch(accusing);

    expect(COPY.vaultCard.pauseMessage).toMatch(/holding this safely/i);
    expect(COPY.vaultCard.pauseMessage).toMatch(/15 minutes/);
    expect(COPY.guardianPanel.alertIntro).toMatch(/safety pause/i);
  });

  it("uses plain words: no copy string has a word of more than 14 letters", () => {
    for (const text of ALL_COPY) {
      for (const word of text.split(/[^A-Za-z]+/)) expect(word.length, `${word} in "${text}"`).toBeLessThanOrEqual(14);
    }
  });

  describe("on screen, for every state", () => {
    it.each(FIXTURES)("shows none of the banned terms in the text, aria-label or title for %s", async (_name, fixture) => {
      const { fetchStub } = renderApp();
      fetchStub.respond(200, fixture);

      fireEvent.click(presetButton(SAFE_BUTTON));
      await waitFor(() => expect(presetButton(SAFE_BUTTON)).toBeEnabled());
      await waitFor(() => expect(screen.queryByText("Choose a payment above to see how RukPay keeps it safe.")).not.toBeInTheDocument());

      const strings = readableStrings(document.body);
      expect(strings.join("\n").length).toBeGreaterThan(200);
      for (const text of strings) expect(bannedTermsIn(text)).toEqual([]);
    });

    it("shows none of them on the guardian's second step, with a notice, or with the unreachable message", async () => {
      const { fetchStub, user } = renderApp();
      fetchStub.respond(200, locked());
      await user.click(presetButton(MEDIUM_BUTTON));
      await userPhone().findByRole("timer");

      await user.click(guardianPhone().getByRole("button", { name: "I Spoke With Them — Safe to Approve" }));
      fetchStub.respond(403, errorBody("forbidden", "This approval link is not valid for this payment, or it has run out."));
      await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));
      await guardianPhone().findByRole("button", { name: "Refresh" });
      fetchStub.reject();
      await user.click(userPhone().getByRole("button", { name: "Block & Report (1930)" }));
      await userPhone().findByText(COPY.unreachable.message);

      for (const text of readableStrings(document.body)) expect(bannedTermsIn(text)).toEqual([]);
    });

    it("shows none of them in the reality-check dialog at each question", async () => {
      const { fetchStub, user } = renderApp();
      fetchStub.respond(200, view("REALITY_CHECK"));
      await user.click(presetButton(MEDIUM_BUTTON));
      const dialog = await screen.findByRole("dialog");

      for (let step = 0; step < 3; step++) {
        for (const text of readableStrings(dialog)) expect(bannedTermsIn(text)).toEqual([]);
        if (step < 2) await user.click(screen.getByRole("button", { name: "No" }));
      }
    });
  });
});
