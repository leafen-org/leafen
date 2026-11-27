import { stem } from "porter2";

/**
 * The stem of a word in lower case, English's Porter2 (Snowball) as the
 * porter2 package gives it: "running" and "runs" are "run", "happiness" and
 * "happy" "happi", "generalization" "general". Only a, e, i, o, u and y are
 * vowels to it: a letter past ASCII is a consonant, so "straße" stays as it
 * is and "cafés" is "café". The server's search takes the same stems
 * (search_english_stem.go, the kljensen/snowball package).
 */
export function englishStem(word: string) {
  return stem(word);
}

/** Whether a folded keyword is an English word search finds by its stem: three letters or more, a to z alone (search_english_stem.go). */
export function englishSearchWord(word: string) {
  return /^[a-z]{3,}$/.test(word);
}

/** Whether a folded word of a text has the stem of an English keyword, as the server's search finds it. */
export function englishStemEqual(held: string, keyword: string) {
  return englishSearchWord(keyword) && englishSearchWord(held) && held[0] === keyword[0] && englishStem(held) === englishStem(keyword);
}
