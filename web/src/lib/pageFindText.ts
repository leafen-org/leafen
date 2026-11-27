import { canonicalTextRanges, type CanonicalTextRange } from "./textMatch";

// Past ASCII: text whose letters search folds beyond their case.
const PAST_ASCII = /[^\u0000-\u007f]/u;
// The browser's letters as its find compares them: its collation at the
// first strength, case and accents aside.
const collator = typeof Intl === "object" ? new Intl.Collator(undefined, { sensitivity: "base" }) : null;

/**
 * Where page find finds a query in a text, as the browser's find (Notion's
 * page find) does: the letters its collation holds the same at the first
 * strength (seen 2026-11-16 in Chrome) — "strasse" finds "Straße", "lodz"
 * "Łódź", "ae" "Æ", "da nang" "Đà Nẵng", "ελλαδα" "Ελλάδα", "елка" "ёлка",
 * "cafe" "ｃａｆｅ", "file" "ﬁle", "שלום" "שָׁלוֹם", "كتب" "كَتَبَ", "นำ" "น้ำ" —
 * which search across the workspace finds too (foldSearchText), but not the
 * letters its collation keeps apart that search folds: "иод" doesn't find
 * "йод", nor "ilik" "ılık", "thing" "Þing", "कषमा" "क्षमा". Each range covers
 * whole characters as written.
 */
export function pageFindTextRanges(text: string, query: string): CanonicalTextRange[] {
  const ranges = canonicalTextRanges(text, query);
  if (!collator || ranges.length === 0 || (!PAST_ASCII.test(text) && !PAST_ASCII.test(query))) return ranges;
  return ranges.filter((range) => collator.compare(text.slice(range.start, range.end), query) === 0);
}
