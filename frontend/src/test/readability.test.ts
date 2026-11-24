import { describe, expect, it } from "vitest";
import { AVOID_WORDS, avoidWordsIn, EXTRA_AVOID_WORDS } from "./avoidWords";
import { BANNED_TERMS } from "./bannedTerms";
import { countSyllables, gradeLevel, longestSentenceWords, sentencesOf, wordsOf } from "./readability";
/** The most a string of 8 words or more may score (Requirement 25 criterion 8). */
const MAX_GRADE = 6.9;
describe("the syllable counter", () => {
  it.each([
    ["a", 1],
    ["the", 1],
    ["you", 1],
    ["make", 1],
    ["phone", 1],
    ["care", 1],
    ["table", 2],
    ["people", 2],
    ["payment", 2],
    ["extra", 2],
    ["person", 2],
    ["family", 3],
    ["already", 3],
    ["beautiful", 3],
    ["asked", 1],
    ["looked", 1],
    ["noticed", 2],
    ["wanted", 2],
    ["15", 1],
    ["15-minute", 3],
    ["", 0],
  ])("counts %j as %i", (word, syllables) => {
    expect(countSyllables(word)).toBe(syllables);
  });
});
describe("words and sentences", () => {
  it("counts a run of non-space characters as one word, so a hyphenated word is one", () => {
    expect(wordsOf("A 15-minute safety pause.")).toEqual(["A", "15-minute", "safety", "pause."]);
    expect(wordsOf("  spaced   out \n words ")).toEqual(["spaced", "out", "words"]);
    expect(wordsOf("")).toEqual([]);
  });
  it("splits sentences after a full stop, a question mark, an exclamation mark or an ellipsis", () => {
    expect(sentencesOf("One. Two? Three! Four… Five")).toEqual(["One", "Two", "Three", "Four", "Five"]);
    expect(sentencesOf("Total: 90 points")).toEqual(["Total: 90 points"]);
    expect(sentencesOf("")).toEqual([]);
  });
  it("finds the longest sentence in words", () => {
    expect(longestSentenceWords("One two. One two three four. Five.")).toBe(4);
    expect(longestSentenceWords("")).toBe(0);
  });
});
describe("the Flesch-Kincaid grade", () => {
  it("follows the formula on a sentence worked out by hand", () => {
    // 6 words, 6 syllables, 1 sentence: 0.39 * 6 + 11.8 * 1 - 15.59 = -1.45
    expect(gradeLevel("The cat sat on the mat.")).toBeCloseTo(-1.45, 2);
  });
  it("is 0 for text with no words", () => {
    expect(gradeLevel("")).toBe(0);
    expect(gradeLevel("… ! ?")).toBe(0);
  });
  it("rates a dense sentence above the limit, so the check can fail", () => {
    const dense = "Notwithstanding unforeseen circumstances, individuals should independently authenticate communications.";
    expect(gradeLevel(dense)).toBeGreaterThan(MAX_GRADE);
  });
  it("rates plain calm copy under the limit", () => {
    expect(gradeLevel("Nothing about this payment asked for extra care.")).toBeLessThan(MAX_GRADE);
    expect(gradeLevel("Take a breath and call a family member you trust.")).toBeLessThan(MAX_GRADE);
  });
  // Design conflict C-8. Requirement 17 criterion 8 fixes these three questions,
  // so they cannot be reworded to read more easily. This counter rates them at
  // 4.2, 6.3 and 6.0, all under the limit, so no question needs an exemption. If a
  // change to the counter pushes one over, give that one question a one-line
  // exemption with a comment. Do not reword it.
  it.each([
    ["Did someone on a phone or video call ask you to add this person?", 4.2],
    ["Do you know this person, and have you spoken to them on a number you already had?", 6.3],
    ["Did anyone ask you to keep this person a secret from your family or bank?", 6.0],
  ])("keeps the fixed verification question under the limit: %s", (question, expected) => {
    expect(gradeLevel(question)).toBeLessThanOrEqual(MAX_GRADE);
    expect(gradeLevel(question)).toBeCloseTo(expected, 1);
  });
});
describe("the Avoid_Words", () => {
  it("hold the shipped banned terms and the extra words of the spec", () => {
    expect(AVOID_WORDS).toEqual([...BANNED_TERMS, ...EXTRA_AVOID_WORDS]);
    for (const word of ["velocity", "anomaly", "heuristic", "deviation", "suspicious", "fraud", "fraudulent", "criminal"]) {
      expect(AVOID_WORDS).toContain(word);
    }
    for (const word of ["dangerous", "illegal", "scammer", "huge", "massive", "enormous", "urgent", "warning", "alert", "threat"]) {
      expect(AVOID_WORDS).toContain(word);
    }
  });
  it("are found anywhere in a string, in any letter case, inside a longer word too", () => {
    for (const word of AVOID_WORDS) {
      expect(avoidWordsIn(`Some ${word.toUpperCase()} text`)).toContain(word);
      expect(avoidWordsIn(`x${word.toLowerCase()}y`)).toContain(word);
    }
    expect(avoidWordsIn("alerting")).toContain("alert");
    expect(avoidWordsIn("a fraudster")).toContain("fraud");
  });
  it("find nothing in calm text", () => {
    expect(avoidWordsIn("Nothing about this payment asked for extra care.")).toEqual([]);
    expect(avoidWordsIn("Why RukPay looked closer")).toEqual([]);
  });
});
