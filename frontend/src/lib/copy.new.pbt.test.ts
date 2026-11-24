import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { avoidWordsIn } from "../test/avoidWords";
import { PBT_RUNS } from "../test/pbt";
import { gradeLevel, longestSentenceWords, wordsOf } from "../test/readability";
import { ALL_COPY, COPY, NEW_COPY_GROUPS, NEW_COPY_HELPER_SAMPLES, pointsText, totalText } from "./copy";
// ---------------------------------------------------------------------------
// The strings under test: every string of the groups this feature added to
// COPY, and the sample output of every text builder it added. A group added to
// NEW_COPY_GROUPS later is checked with no change to this file.
// ---------------------------------------------------------------------------
function stringsIn(value: unknown, out: string[] = []): string[] {
  if (typeof value === "string") out.push(value);
  else if (Array.isArray(value)) value.forEach((item: unknown) => stringsIn(item, out));
  else if (typeof value === "object" && value !== null) Object.values(value).forEach((item: unknown) => stringsIn(item, out));
  return out;
}
const groupStrings = (group: keyof typeof COPY): string[] => stringsIn(COPY[group]);
const NEW_COPY: string[] = [...NEW_COPY_GROUPS.flatMap(groupStrings), ...NEW_COPY_HELPER_SAMPLES];
/** The most a string of 8 words or more may score. */
const MAX_GRADE = 6.9;
const MAX_SENTENCE_WORDS = 20;
const FORBIDDEN_PHRASES = ["money saved", "fraud prevented", "your bank", "the bank", "real money", "money is safe", "money was returned"];
const FORBIDDEN_WORDS = ["freeze", "frozen", "restrict", "suspected"];
/** Words that say a payment was approved, held or cancelled. Such a string must say it is a simulation. */
const OUTCOME_WORDS = /approv|held|hold|block|cancel/i;
/** The one exact label that closes a form and says nothing about a payment. */
const EXEMPT_FROM_SIMULATION_WORD = new Set(["Cancel"]);
/** Words a field of the custom payment form may never ask for. */
const CREDENTIAL_WORDS = /\b(otp|pin|password|cvv)\b|account number|upi id|phone number/i;
/** The rules a string of the new copy breaks, by name. Empty when it is calm, simulated and short. */
function problemsIn(text: string): string[] {
  const lower = text.toLowerCase();
  const problems: string[] = [];
  if (avoidWordsIn(text).length > 0) problems.push("avoid word");
  if (FORBIDDEN_PHRASES.some((phrase) => lower.includes(phrase))) problems.push("forbidden phrase");
  if (FORBIDDEN_WORDS.some((word) => lower.includes(word))) problems.push("forbidden word");
  if (lower.replace(/protective pause/g, "").includes("pause")) problems.push("pause outside protective pause");
  if (longestSentenceWords(text) > MAX_SENTENCE_WORDS) problems.push("sentence over 20 words");
  if (wordsOf(text).length >= 8 && gradeLevel(text) > MAX_GRADE) problems.push("reading grade over 6.9");
  if (OUTCOME_WORDS.test(text) && !EXEMPT_FROM_SIMULATION_WORD.has(text) && !/simulat/i.test(text)) {
    problems.push("outcome without simulation word");
  }
  return problems;
}
/** The credential rule, which applies to the custom payment form's group only. */
const credentialProblems = (text: string): string[] => (CREDENTIAL_WORDS.test(text) ? ["credential word"] : []);
describe("new copy properties", () => {
  it("Property 38: the groups under test exist and are not empty", () => {
    expect(NEW_COPY_GROUPS).toContain("explanation");
    for (const group of NEW_COPY_GROUPS) {
      expect(Object.keys(COPY), group).toContain(group);
      expect(groupStrings(group).length, group).toBeGreaterThan(0);
    }
    expect(NEW_COPY_HELPER_SAMPLES.length).toBeGreaterThan(0);
    expect(NEW_COPY.length).toBeGreaterThan(8);
  });
  // Feature: risk-intelligence, Property 38: New copy is calm, simulated and short
  // Validates: Requirements 5.10, 7.7, 25.2, 25.3, 25.4, 25.6, 25.8, 25.9
  it("Property 38: every string of the new copy passes every rule, by a direct loop", () => {
    for (const text of NEW_COPY) expect(problemsIn(text), text).toEqual([]);
  });
  // Feature: risk-intelligence, Property 38: New copy is calm, simulated and short
  // Validates: Requirements 25.2, 25.3, 25.4, 25.6, 25.8, 25.9
  it("Property 38: any string picked from the new copy passes every rule", () => {
    fc.assert(
      fc.property(fc.constantFrom(...NEW_COPY), (text) => problemsIn(text).length === 0),
      { numRuns: PBT_RUNS },
    );
  });
  // Feature: risk-intelligence, Property 38: New copy is calm, simulated and short
  // Validates: Requirements 5.10, 7.7
  it("Property 38: the custom payment form's copy never asks for a credential, an account number, a UPI id or a phone number", () => {
    const scenario = stringsIn((COPY as Record<string, unknown>)["scenario"]);
    // The `scenario` group is added with the custom payment form. Until then there is nothing to check.
    for (const text of scenario) expect(credentialProblems(text), text).toEqual([]);
    fc.assert(
      fc.property(fc.constantFrom("scenario text", ...scenario), (text) => credentialProblems(text).length === 0),
      { numRuns: PBT_RUNS },
    );
  });
  // Feature: risk-intelligence, Property 38: New copy is calm, simulated and short
  // Validates: Requirements 25.8
  it("Property 38: the text builders give calm text for any whole number", () => {
    fc.assert(
      fc.property(fc.integer({ min: -1_000_000, max: 1_000_000 }), (n) => {
        expect(problemsIn(pointsText(n))).toEqual([]);
        expect(problemsIn(totalText(n))).toEqual([]);
      }),
      { numRuns: PBT_RUNS },
    );
  });
  it("Property 38: the samples cover the sign and the singular and plural words", () => {
    expect(NEW_COPY_HELPER_SAMPLES).toEqual(expect.arrayContaining(["+20 points", "-5 points", "+1 point", "-1 point", "+0 points"]));
    expect(NEW_COPY_HELPER_SAMPLES).toEqual(expect.arrayContaining(["Total: 90 points", "Total: 1 point", "Total: 0 points"]));
  });
  it("Property 38: the explanation strings are the texts the design fixes", () => {
    expect(COPY.explanation).toEqual({
      heading: "Why RukPay looked closer",
      guardianHeading: "What RukPay noticed",
      empty: "Nothing about this payment asked for extra care.",
      total: "Total",
      point: "point",
      points: "points",
    });
    expect(COPY.explanation.empty.split(/\s+/).length).toBeLessThanOrEqual(MAX_SENTENCE_WORDS);
  });
  // Feature: risk-intelligence, Property 38: New copy is calm, simulated and short
  // Validates: Requirements 25.1
  it("Property 38: ALL_COPY holds every string of COPY and every text-builder sample", () => {
    const written = stringsIn(COPY);
    expect(written.length).toBeGreaterThan(50);
    for (const text of written) expect(ALL_COPY, text).toContain(text);
    for (const text of NEW_COPY_HELPER_SAMPLES) expect(ALL_COPY, text).toContain(text);
    for (const text of NEW_COPY) expect(ALL_COPY, text).toContain(text);
  });
  describe("the rules themselves", () => {
    it("find each thing they check for", () => {
      expect(problemsIn("This is a warning.")).toContain("avoid word");
      expect(problemsIn("There was an ALERT.")).toContain("avoid word");
      expect(problemsIn("Money saved today.")).toContain("forbidden phrase");
      expect(problemsIn("Ask your bank.")).toContain("forbidden phrase");
      expect(problemsIn("We will freeze this.")).toContain("forbidden word");
      expect(problemsIn("This looks suspected.")).toContain("forbidden word");
      expect(problemsIn("A short pause.")).toContain("pause outside protective pause");
      expect(problemsIn("The simulated protective pause is on.")).toEqual([]);
      expect(problemsIn(`${"word ".repeat(21).trim()}.`)).toContain("sentence over 20 words");
      expect(problemsIn(`${"word ".repeat(20).trim()}.`)).not.toContain("sentence over 20 words");
      expect(problemsIn("Notwithstanding unforeseen circumstances, individuals should independently authenticate communications.")).toContain(
        "reading grade over 6.9",
      );
    });
    it("ask for the word simulation in any string about an approval, a hold or a cancellation", () => {
      expect(problemsIn("The payment was approved.")).toContain("outcome without simulation word");
      expect(problemsIn("The payment is held for now.")).toContain("outcome without simulation word");
      expect(problemsIn("The payment was cancelled.")).toContain("outcome without simulation word");
      expect(problemsIn("The simulated payment was cancelled.")).toEqual([]);
      expect(problemsIn("The payment is held in the simulation.")).toEqual([]);
      expect(problemsIn("Cancel")).toEqual([]);
      expect(problemsIn("Cancel it")).toContain("outcome without simulation word");
    });
    it("find a credential word in the form's copy", () => {
      for (const text of ["Enter your OTP", "Your PIN", "Type your password", "The CVV code", "Account number", "UPI id", "A phone number"]) {
        expect(credentialProblems(text), text).toEqual(["credential word"]);
      }
      expect(credentialProblems("Amount in rupees")).toEqual([]);
      expect(credentialProblems("Please be careful")).toEqual([]);
    });
  });
});
