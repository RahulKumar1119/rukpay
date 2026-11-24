/**
 * A reading-level check for the new copy: a word and sentence counter and the
 * Flesch-Kincaid grade level. Test only.
 *
 * The syllable counter is a heuristic. It counts groups of vowels (a, e, i, o,
 * u and y) and takes one off for a silent ending: a final "e" that does not
 * follow "l" (make, phone), and "ed" after a consonant other than "t" or "d"
 * (asked). A word of three letters or fewer is one syllable. Each run of digits
 * is one more syllable, so "15" is one and "15-minute" is three. It is rough, but it is the same on every run, and it
 * rates the three fixed verification questions at 4.2, 6.3 and 6.0.
 */
/** A word is a run of non-space characters that holds a letter or a digit, so "15-minute" is one word. */
export function wordsOf(text: string): string[] {
  return text.split(/\s+/).filter((token) => /[A-Za-z0-9]/.test(token));
}
/** The sentences of `text`, split after ".", "!", "?" or "…". Always at least one for text with a word. */
export function sentencesOf(text: string): string[] {
  return text
    .split(/[.!?…]+(?:\s+|$)/)
    .map((part) => part.trim())
    .filter((part) => part !== "");
}
/** The most words in any one sentence of `text`. */
export function longestSentenceWords(text: string): number {
  return Math.max(0, ...sentencesOf(text).map((sentence) => wordsOf(sentence).length));
}
/** The syllables of the letters of `word`, with at least one when there is a letter. */
function letterSyllables(word: string): number {
  if (word === "") return 0;
  if (word.length <= 3) return 1;
  let count = (word.match(/[aeiouy]+/g) ?? []).length;
  if (/[^aeiouyl]e$/.test(word) || /[^aeiouytd]ed$/.test(word)) count -= 1;
  return Math.max(1, count);
}
export function countSyllables(token: string): number {
  const digitRuns = (token.match(/[0-9]+/g) ?? []).length;
  return letterSyllables(token.toLowerCase().replace(/[^a-z]/g, "")) + digitRuns;
}
/** The Flesch-Kincaid grade level: 0.39 * words per sentence + 11.8 * syllables per word - 15.59. */
export function gradeLevel(text: string): number {
  const words = wordsOf(text);
  if (words.length === 0) return 0;
  const sentences = Math.max(1, sentencesOf(text).length);
  const syllables = words.reduce((sum, word) => sum + countSyllables(word), 0);
  return 0.39 * (words.length / sentences) + 11.8 * (syllables / words.length) - 15.59;
}
