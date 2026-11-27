import { foldedTextWithSourceMap, type CanonicalTextRange } from "@/lib/textMatch";

// The combining diacritical marks (U+0300–U+036F): the accents a view's
// search leaves out.
const DIACRITICAL_MARKS = /[\u0300-\u036f]/gu;
// Past ASCII: text the fold may decompose (ASCII text is only lowered, in a
// third of the time on 2026-11-16's 50,000 cells).
const PAST_ASCII = /[^\u0000-\u007f]/u;

/**
 * Text as a database view's search compares it, as Notion's view search
 * (seen 2026-11-16 in two tabs): decomposed, lower case and without the
 * combining diacritical marks U+0300–U+036F, either side — "cafe" finds
 * "café" and "café" finds "cafe", "sao" "São", "pho" "phở", "nang" "Nẵng",
 * "иод" "йод", "ελλαδα" "Ελλάδα", "istanbul" "İstanbul". A letter that
 * doesn't decompose keeps its form ("da" doesn't find "Đà", nor "ae" "Æ",
 * "o" "ø", "strasse" "Straße", "l" "Ł"), and marks outside that block stay
 * (the kana voicing mark: "かくせい" doesn't find "がくせい"; Hebrew points),
 * as does a compatibility form ("file" doesn't find "ﬁle", "cafe" not
 * "ｃａｆｅ"). Compared decomposed, a Hangul syllable finds the syllables
 * it begins ("해" finds "행"). The server's view search folds alike
 * (foldDatabaseViewSearchText). Search across the workspace (⌘K) folds
 * otherwise (foldSearchText).
 */
export function foldViewSearchText(value: string) {
  const text = String(value);
  if (!PAST_ASCII.test(text)) return text.toLowerCase();
  return text.normalize("NFD").toLowerCase().replace(DIACRITICAL_MARKS, "");
}

/**
 * Where a view's search marks what it found in a text, as Notion's paints a
 * row's title (seen 2026-11-16): each match of whole letters as written —
 * "cafe" marks "café", "nang" "Nẵng" — and none that ends inside a letter,
 * as "해" finds "행" with nothing painted.
 */
export function viewSearchTextRanges(text: string, query: string): CanonicalTextRange[] {
  const needle = foldViewSearchText(query);
  if (!needle || !text) return [];
  const { folded, segments } = foldedTextWithSourceMap(text, foldViewSearchText);
  const starts = new Map(segments.map((segment) => [segment.foldedStart, segment]));
  const ends = new Map(segments.map((segment) => [segment.foldedEnd, segment]));
  const ranges: CanonicalTextRange[] = [];
  for (let at = folded.indexOf(needle); at >= 0; ) {
    const first = starts.get(at);
    const last = ends.get(at + needle.length);
    if (first && last) {
      ranges.push({ start: first.sourceStart, end: last.sourceEnd });
      at = folded.indexOf(needle, at + needle.length);
    } else {
      at = folded.indexOf(needle, at + 1);
    }
  }
  return ranges;
}
