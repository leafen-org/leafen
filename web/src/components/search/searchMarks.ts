import { englishStem } from "@/lib/englishStem";
import { canonicalTextRanges, foldSearchText, type CanonicalTextRange } from "@/lib/textMatch";
import { SEARCH_PAIRED, SEARCH_WORD } from "./searchModel";
import { searchCommonWord, searchLeadRanges } from "./searchLead";

const WORD_PART = /\p{N}+|[\p{L}\p{M}]+/gu;
// A keyword that may mark a word it starts: of letters alone or of digits alone.
const STARTING_WORD = /^(?:[\p{L}\p{M}]+|\p{Nd}+)$/u;

/**
 * A query's word as its marks read it: folded, its English stem, and as typed
 * in lower case with its accents, which a word's start is compared with;
 * whether it may mark the letters it starts a word with (typedStart).
 */
type MarkWord = { folded: string; stem: string; paired: boolean; typed: string; lineStart: boolean; part?: true };

/** A query's words as its marks read them: folded, and their English stems. */
function markWords(query: string): MarkWord[] {
  const words = query.replace(/["“”]/gu, " ").normalize("NFC").match(SEARCH_WORD) ?? [];
  const read = (word: string) => {
    const folded = foldSearchText(word);
    return { folded, stem: englishStem(word.toLowerCase()), paired: SEARCH_PAIRED.test(folded), typed: word.toLowerCase() };
  };
  const marked: MarkWord[] = words.flatMap((word) => {
    const reading = read(word);
    if (!reading.folded) return [];
    return [{ ...reading, lineStart: STARTING_WORD.test(word) && !reading.paired }];
  });
  // A word of letters and digits marks by its parts too, each whole, after the words (seen 2026-11-24:
  // "Tarvokin4" marks "[Tarvokin]4291" and a title "[4] 스프린트").
  for (const word of words) {
    const parts = word.match(WORD_PART) ?? [];
    if (parts.length > 1) marked.push(...parts.map((part) => ({ ...read(part), lineStart: false, part: true as const })));
  }
  return marked;
}

/** A text's words and the letters-or-digits parts of a word holding both ("Tarvokin4291": Tarvokin, 4291), where each stands. */
function markTokens(text: string) {
  const tokens: Array<{ text: string; start: number; end: number; word: boolean }> = [];
  for (const match of text.matchAll(SEARCH_WORD)) {
    const start = match.index, word = match[0];
    tokens.push({ text: word, start, end: start + word.length, word: true });
    const parts = [...word.matchAll(WORD_PART)];
    if (parts.length > 1) for (const part of parts) {
      tokens.push({ text: part[0], start: start + part.index, end: start + part.index + part[0].length, word: false });
    }
  }
  return tokens;
}

type MarkTokens = ReturnType<typeof markTokens>;

/** Whether a word of a text starts with a keyword as typed, lower case and accents kept ("caf" and "café" start "Cafés", "cafe" doesn't). */
function wordStarts(text: string, typed: string) {
  return text.length > typed.length && text.slice(0, typed.length).toLowerCase() === typed;
}

/**
 * Where a keyword also marks a word it starts, as Notion's search marks it
 * (seen 2026-11-24 in two tabs at 13:1x and 13:4x by w2474, again at 15:3x
 * and 15:5x by w2496, on pages five minutes old and more, in the business
 * trial workspace; a free one the same): `lead`, a title leading the results
 * (searchLead.ts), the words its search answered by, whole
 * (searchLeadRanges); `typedStart`, a result's title found by several words
 * (searchTitleTypedStart) or a line of a body (searchTypedStart), the last
 * word typed marking the letters it typed at the start of a word ("jx plumb"
 * "w2496 [jx] 71834 [plumb]ert", "zz vexlo" "[vexlo]rran", "zz 7123"
 * "[7123]4", "grindles li" "[li]ne", "zz v" "[v]exlorran"; not inside a word
 * — "orran" — nor typed before another — "vexlo zz"; one word alone, "plumb",
 * marks no title so).
 */
export type SearchMarkStarts = { lead?: boolean; typedStart?: string };

/**
 * Where the search window marks its query in a text, as Notion's search
 * marks it (seen 2026-11-16 in ⌘K, Chrome 1440): a whole word, or a part of
 * letters or of digits of one ("tarvokin" and "4291" in "Tarvokin4291"), that
 * is a word of the query, or a part of one of letters and digits, folded (foldSearchText: "strasse" marks "Straße",
 * "aero" "Ærø", "nang" "Nẵng") or has its English stem (englishStem, read
 * before the accents go: "run" and "runs" mark "running", "happy"
 * "happiness", "car" "cars", "general" "generalization"); not "strass" in
 * "Straße", nor "cafe" in "Cafés". A word's front alone marks only where
 * `starts` says (SearchMarkStarts): Notion marked no front on 2026-11-16, and
 * none on pages a few minutes old on 2026-11-24 ("longe", "stras", "morv",
 * "gener" at 1 to 4 minutes, "vexlo" at 2 to 5), whose search reads them
 * only once its index holds them. A word of a script written without spaces
 * is marked where it stands in a word, two letters of it at least ("가나" in
 * "가나다라마", "방학" in "여름방학계획표"); a letter alone marks only a word
 * that is it, or it and one letter more ("검" marks "검색", "칠" "칠"; "여"
 * nothing in "여름방학계획표" but in a title leading the results).
 */
export function searchMarkRanges(text: string, query: string, starts: SearchMarkStarts = {}): CanonicalTextRange[] {
  if (starts.lead) return searchLeadRanges(text, query);
  return wordRanges(text, query, starts).map(({ start, end }) => ({ start, end }));
}

/** Where a word of the query is marked whole in a text's tokens, as searchMarkRanges marks it. */
function wholeRanges(text: string, word: MarkWord, tokens: MarkTokens): CanonicalTextRange[] {
  if (word.paired) return pairedRanges(text, word.folded, tokens);
  return tokens.filter((token) => foldSearchText(token.text) === word.folded
    || englishStem(token.text.normalize("NFC").toLowerCase()) === word.stem);
}

/** searchMarkRanges, each range with the query word it marks, and whether it stands alone, not in a run: a word's start, a part of a word typed. */
function wordRanges(text: string, query: string, starts: SearchMarkStarts) {
  const words = markWords(query);
  if (!words.length || !text) return [];
  const tokens = markTokens(text);
  const typedStart = starts.typedStart?.normalize("NFC").toLowerCase();
  const ranges: Array<CanonicalTextRange & { word: number; alone?: true; part?: true }> = [];
  for (const [index, word] of words.entries()) {
    const whole = wholeRanges(text, word, tokens);
    // The letters typed, where they stand as written.
    if (word.lineStart && word.typed === typedStart) for (const token of tokens) {
      if (!token.word || !wordStarts(token.text.normalize("NFC"), word.typed)) continue;
      if (!whole.some((range) => range.start === token.start) && token.text === token.text.normalize("NFC")) {
        ranges.push({ start: token.start, end: token.start + word.typed.length, word: index, alone: true });
      }
    }
    ranges.push(...whole.map(({ start, end }) => ({ start, end, word: index, ...(word.part ? { alone: true as const, part: true as const } : {}) })));
  }
  // A word marked whole isn't marked by a typed word's parts again.
  return ranges.filter((range) => !range.part || !ranges.some((other) => !other.part && other.start <= range.start && range.end <= other.end));
}

const WORD_CHARACTER = /[\p{L}\p{N}\p{M}]/u;

/**
 * searchMarkRanges with the query's words standing in its order, each after the
 * one before with only spaces or marks between, as one range, as Notion marks
 * them (seen 2026-11-24: "11/23", "11-23", "11.23" and "11 23" one for "11/23"
 * and for "11 23", "2026/11/23" for "2026-11-23"; "23/11" and "11/23" for
 * "2026-11-23" each word on its own; an agent's highlight the same,
 * mcp_search_highlight_marks.go). A word marked by its start stands on its
 * own ("[zz] [vexlo]rran").
 */
export function searchMarkRuns(text: string, query: string, starts: SearchMarkStarts = {}): CanonicalTextRange[] {
  if (starts.lead) return searchLeadRanges(text, query);
  const found = wordRanges(text, query, starts).sort((a, b) => a.start - b.start || b.end - a.end);
  const count = markWords(query).filter((word) => !word.part).length;
  const runs: CanonicalTextRange[] = [];
  for (let index = 0; index < found.length;) {
    let end = index + 1;
    if (count > 1 && found[index].word === 0 && !found[index].alone) {
      while (end - index < count && end < found.length && found[end].word === end - index && !found[end].alone
        && found[end].start >= found[end - 1].end && !WORD_CHARACTER.test(text.slice(found[end - 1].end, found[end].start))) end++;
      if (end - index < count) end = index + 1;
    }
    runs.push({ start: found[index].start, end: found[end - 1].end });
    index = end;
  }
  return runs;
}

/** Whether a word of a text starts with a keyword typed, not being it ("gri" in "grindlewick"). */
function startsOnly(text: string, word: MarkWord, tokens: MarkTokens) {
  return word.lineStart && tokens.some((token) => token.word && wordStarts(token.text.normalize("NFC"), word.typed))
    && !wholeRanges(text, word, tokens).some((range) => tokens.some((token) => token.word && token.start === range.start && token.end === range.end));
}

/**
 * Whether a text holds a keyword as written: a word or a part of one that is
 * it, folded, or a word of a script written without spaces holding it — not
 * a word of its English stem, which a title marks but a result's line marks
 * again (seen 2026-11-24 in two tabs: "run zzqq" and "running" mark
 * "{running} x {running} x {running}" under "w2486 lx [runs]", "kestroms
 * zzqq" "{kestroms} line one" under "w2486 lx [kestrom]"; "jump zzqq" leaves
 * "jump trevo" unmarked under "w2486 lx [jump]").
 */
export function searchTextHoldsWritten(text: string, keyword: string) {
  const word = markWords(keyword).find((candidate) => !candidate.part);
  if (!word || !text) return false;
  const tokens = markTokens(text);
  if (word.paired) return pairedRanges(text, word.folded, tokens).length > 0;
  return tokens.some((token) => foldSearchText(token.text) === word.folded);
}

/**
 * Whether a result's title holds a keyword of a query only as the start of a
 * word it marks so: the last word typed of several (searchTitleTypedStart —
 * "gri" in "w2474 qa [gri]ndlewick" for "line gri").
 */
export function searchTitleHoldsStart(title: string, query: string, keyword: string) {
  const typed = searchTitleTypedStart(query);
  const word = markWords(keyword).find((candidate) => !candidate.part);
  return Boolean(typed && word && word.typed === typed) && startsOnly(title, word!, markTokens(title));
}

/**
 * The last word typed of several, which a result's title marks by the
 * letters it starts a word with, as Notion's rows found by its server mark it
 * (seen 2026-11-24 in two tabs: "jx plumb" "w2496 [jx] 71834 [plumb]ert",
 * "plumb" alone nothing in it); none after a closing quote. A title leading
 * the results marks the words its search answered by instead (searchLead.ts).
 */
export function searchTitleTypedStart(query: string) {
  if (/["“”]\s*$/u.test(query)) return undefined;
  const words = markWords(query).filter((word) => !word.part);
  const word = words.at(-1);
  // A common English word doesn't count ("the plumb" marks nothing in "w2496 jx 71834 plumbert", seen 2026-11-24).
  return words.filter((other) => !searchCommonWord(other.typed)).length > 1 && word?.lineStart ? word.typed : undefined;
}

/**
 * The last word typed, which a line marks by the start of a word it starts
 * (SearchMarkStarts.typedStart), where the title shown beside the line holds
 * it neither whole nor as a word's start ("line gri" marks nothing in
 * "grindle line one" under "w2474 qa grindlewick"), nor its place whole;
 * none after a closing quote.
 */
export function searchTypedStart(query: string, title: string, place = "") {
  if (/["“”]\s*$/u.test(query)) return undefined;
  const word = markWords(query).filter((word) => !word.part).at(-1);
  if (!word?.lineStart) return undefined;
  const titleTokens = markTokens(title);
  const held = wholeRanges(title, word, titleTokens).length > 0 || startsOnly(title, word, titleTokens)
    || wholeRanges(place, word, markTokens(place)).length > 0;
  return held ? undefined : word.typed;
}

/** A word of a script written without spaces where it stands; a letter alone, a word of one or two letters it starts. */
function pairedRanges(text: string, folded: string, tokens: MarkTokens) {
  const found = canonicalTextRanges(text, folded);
  if ([...folded].length > 1) return found;
  return found.flatMap((range) => {
    const token = tokens.find((candidate) => candidate.word && candidate.start === range.start && range.end <= candidate.end);
    return token && [...token.text].length <= 2 ? [{ start: token.start, end: token.end }] : [];
  });
}
