// Past ASCII: text the fold may take apart.
const PAST_ASCII = /[^\u0000-\u007f]/u;
const NONSPACING_MARK = /\p{Mn}/gu;
const DIACRITIC = /^\p{Diacritic}$/u;
// The kana voicing marks, which search keeps ("かくせい" doesn't find "がくせい").
const KANA_VOICING = /^[\u3099\u309a]$/u;
// The lower-case letters no decomposition takes apart, as search reads them.
const FOLDED_LETTERS: Record<string, string> = {
  ß: "ss", ς: "σ", æ: "ae", œ: "oe", þ: "th", ð: "d",
  đ: "d", ħ: "h", ı: "i", ł: "l", ø: "o", ŧ: "t",
};
const FOLDED_LETTER = new RegExp(`[${Object.keys(FOLDED_LETTERS).join("")}]`, "gu");

/**
 * Text as search across the workspace (⌘K) compares it, as the server folds
 * it (foldSearchText in workspace_search_language.go) and as Notion's search
 * compares it, either side (seen 2026-11-16): in its compatibility form
 * ("ｃａｆｅ" is "cafe", "ﬁle" "file"), lower case, the final sigma as σ and
 * ß as ss, without the nonspacing diacritic marks of any script — accents,
 * the Cyrillic breve ("иод" finds "йод"), Greek tonos, Hebrew points, Arabic
 * vowel marks, the Devanagari virama and nukta, Thai tone marks — but the
 * kana voicing marks, and with the Latin letters that carry a stroke as
 * their letter (ł l, đ d, ø o, ħ h, ŧ t, ı i) and those written as two as
 * their two (æ ae, œ oe, þ th, ð d): "strasse" finds "Straße", "lodz"
 * "Łódź", "aero" "Ærø", "cafe" "café". The search window's marks read
 * matches so too, and page find those of them the browser's find holds
 * the same (pageFindTextRanges). A database view's search folds otherwise
 * (foldViewSearchText).
 */
export function foldSearchText(value: string) {
  if (!PAST_ASCII.test(value)) return value.toLowerCase();
  return value
    .normalize("NFKD")
    .toLowerCase()
    .replace(NONSPACING_MARK, (mark) => (DIACRITIC.test(mark) && !KANA_VOICING.test(mark) ? "" : mark))
    .replace(FOLDED_LETTER, (letter) => FOLDED_LETTERS[letter])
    .normalize("NFC");
}

/**
 * A word of the scripts written without spaces between words, or that join
 * them — Hangul, Han, kana, Thai, Lao, Khmer, Myanmar, Tibetan — which
 * search finds anywhere in a run of it (searchPairedScript in
 * workspace_search_language.go; the 2026-10-14 search language decision).
 */
export const SEARCH_PAIRED = /^[\p{Script=Hangul}\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Thai}\p{Script=Lao}\p{Script=Khmer}\p{Script=Myanmar}\p{Script=Tibetan}\p{M}]+$/u;

export interface CanonicalTextRange {
  start: number;
  end: number;
}

/** One grapheme of a source text and where its folded text sits in the folded whole. */
export interface FoldedSegment {
  foldedStart: number;
  foldedEnd: number;
  sourceStart: number;
  sourceEnd: number;
}

const graphemeSegmenter =
  typeof Intl.Segmenter === "function"
    ? new Intl.Segmenter("und", { granularity: "grapheme" })
    : null;

function sourceSegments(value: string) {
  if (graphemeSegmenter) {
    return Array.from(graphemeSegmenter.segment(value), (item) => ({
      text: item.segment,
      start: item.index,
      end: item.index + item.segment.length,
    }));
  }

  const segments: Array<{ text: string; start: number; end: number }> = [];
  for (let start = 0; start < value.length; ) {
    const codePoint = value.codePointAt(start);
    const width = codePoint !== undefined && codePoint > 0xffff ? 2 : 1;
    const end = start + width;
    segments.push({ text: value.slice(start, end), start, end });
    start = end;
  }
  return segments;
}

/**
 * A text folded a grapheme at a time, each grapheme's folded text placed in
 * the folded whole, so a match found there maps back to the text as written.
 */
export function foldedTextWithSourceMap(value: string, fold: (text: string) => string = foldSearchText) {
  let folded = "";
  const segments: FoldedSegment[] = [];
  for (const source of sourceSegments(value)) {
    const foldedSegment = fold(source.text);
    if (!foldedSegment) continue;
    const foldedStart = folded.length;
    folded += foldedSegment;
    segments.push({
      foldedStart,
      foldedEnd: folded.length,
      sourceStart: source.start,
      sourceEnd: source.end,
    });
  }
  return { folded, segments };
}

/**
 * Find non-overlapping matches as search compares text (foldSearchText:
 * compatibility forms, case and diacritics aside) and map them back to valid UTF-16
 * ranges in the untouched source string: "cafe" marks "café" as written.
 * Mapping at grapheme boundaries keeps decomposed Korean, accented letters
 * and case-expanding characters intact. The search window's marks read
 * matches here, and page find keeps those its browser's would
 * (pageFindTextRanges).
 */
export function canonicalTextRanges(text: string, query: string): CanonicalTextRange[] {
  const needle = foldSearchText(query);
  if (!needle || !text) return [];

  const mapped = foldedTextWithSourceMap(text);
  const ranges: CanonicalTextRange[] = [];
  let searchFrom = 0;
  let segmentIndex = 0;
  while (searchFrom <= mapped.folded.length - needle.length) {
    const matchStart = mapped.folded.indexOf(needle, searchFrom);
    if (matchStart < 0) break;
    const matchEnd = matchStart + needle.length;
    while (
      segmentIndex < mapped.segments.length &&
      mapped.segments[segmentIndex].foldedEnd <= matchStart
    ) {
      segmentIndex += 1;
    }
    const first = mapped.segments[segmentIndex];
    let lastIndex = segmentIndex;
    while (
      lastIndex < mapped.segments.length &&
      mapped.segments[lastIndex].foldedEnd < matchEnd
    ) {
      lastIndex += 1;
    }
    const last = mapped.segments[lastIndex];
    if (first && last) {
      const range = { start: first.sourceStart, end: last.sourceEnd };
      const previous = ranges.at(-1);
      if (!previous || previous.start !== range.start || previous.end !== range.end) {
        ranges.push(range);
      }
    }
    searchFrom = matchStart + Math.max(needle.length, 1);
  }
  return ranges;
}
