import { compareGraphOrder } from "@/contentGraph";
import type { GraphBlock } from "@/contentGraph";

// A numbered list is a run of adjacent numbered items under one parent. An
// item with a start of its own counts anew from it wherever it stands; one
// without counts on from the item before it, or from 1. An item with no
// format of its own takes the item before it's, and the run's first one its
// depth's, Notion's 1. → a. → i. cycle (seen 2026-11-10: 1. 2. 7. 8.; a. 2.
// 3. 4. after picking letters for the first of four; a. e. for a start of 5
// after "a.").

export type NumberedListFormat = "numbers" | "letters" | "roman";

/** The formats "목록 형식" offers on a list's first item, after "기본", which leaves the item none of its own. */
export const NUMBERED_LIST_FORMATS: readonly NumberedListFormat[] = ["numbers", "letters", "roman"];

const DEPTH_FORMATS: readonly NumberedListFormat[] = ["numbers", "letters", "roman"];

function byPosition(left: GraphBlock, right: GraphBlock) {
  return compareGraphOrder(left, right);
}

export function numberedListStoredFormat(block: GraphBlock): NumberedListFormat | null {
  const value = block.content.listFormat;
  return value === "numbers" || value === "letters" || value === "roman" ? value : null;
}

function storedStart(block: GraphBlock) {
  const value = block.content.listStartIndex;
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 1 ? value : null;
}

function letters(value: number) {
  let result = "";
  for (let rest = value; rest > 0; rest = Math.floor((rest - 1) / 26)) {
    result = String.fromCharCode(97 + ((rest - 1) % 26)) + result;
  }
  return result;
}

// Roman numerals as Notion's marker writes them (its code, seen 2026-11-11):
// each symbol as often as it goes in, largest first, a bar over v to m for
// 5,000 to 1,000,000, and the subtractive pairs only below a thousand —
// 4,000 "mmmm", 9,000 "v̄mmmm", 10,000,000 ten m̄. Notion's page stops
// responding from about a hundred million (seen at 123,456,789: it measures
// every marker up to the number), where Leafen writes the digits.
const ROMAN: readonly [number, string][] = [
  [1_000_000, "m\u0304"], [500_000, "d\u0304"], [100_000, "c\u0304"], [50_000, "l\u0304"], [10_000, "x\u0304"],
  [5_000, "v\u0304"], [1000, "m"], [900, "cm"], [500, "d"], [400, "cd"], [100, "c"], [90, "xc"],
  [50, "l"], [40, "xl"], [10, "x"], [9, "ix"], [5, "v"], [4, "iv"], [1, "i"],
];
const ROMAN_DIGITS_FROM = 100_000_000;

function roman(value: number) {
  if (value >= ROMAN_DIGITS_FROM) return String(value);
  let rest = value;
  let result = "";
  for (const [amount, symbol] of ROMAN) {
    while (rest >= amount) {
      result += symbol;
      rest -= amount;
    }
  }
  return result;
}

/**
 * What a typed marker counts as, read as letters ("c" 3) and as roman
 * numerals ("iv" 4); null where it is no such marker. Roman numerals count as
 * Notion's do, each letter added, or taken off before a larger one, however
 * they are written (seen 2026-11-11: "iiii." goes on after "iii." as "iv.").
 */
export function numberedListMarkerValues(head: string): { letters: number | null; roman: number | null } {
  if (!/^[a-z]{1,8}$/u.test(head)) return { letters: null, roman: null };
  let value = 0;
  for (const char of head) value = value * 26 + char.charCodeAt(0) - 96;
  let total = 0;
  const digits = [...head].map((char) => ROMAN.find(([, symbol]) => symbol === char)?.[0] ?? 0);
  digits.forEach((digit, index) => { total += digit < (digits[index + 1] ?? 0) ? -digit : digit; });
  return { letters: value, roman: total > 0 && !digits.includes(0) ? total : null };
}

function formatNumberedListMarker(value: number, format: NumberedListFormat) {
  if (format === "letters") return letters(value);
  if (format === "roman") return roman(value);
  return String(value);
}

type NumberedPlace = { number: number; format: NumberedListFormat | null };

const runIndexes = new WeakMap<readonly GraphBlock[], {
  byId: Map<string, GraphBlock>;
  starts: Map<string, { first: GraphBlock; offset: number }>;
  places: Map<string, NumberedPlace>;
}>();

// Native graph revisions are immutable. Build sibling runs once per revision,
// rather than sorting the whole page for every rendered list item.
function runIndex(blocks: readonly GraphBlock[]) {
  const cached = runIndexes.get(blocks);
  if (cached) return cached;
  const byId = new Map<string, GraphBlock>(), children = new Map<string | null, GraphBlock[]>();
  for (const block of blocks) {
    byId.set(block.id, block);
    const siblings = children.get(block.parentId) ?? [];
    siblings.push(block); children.set(block.parentId, siblings);
  }
  const starts = new Map<string, { first: GraphBlock; offset: number }>();
  const places = new Map<string, NumberedPlace>();
  for (const siblings of children.values()) {
    siblings.sort(byPosition);
    let first = siblings[0], offset = 0, previous: NumberedPlace | null = null;
    for (let index = 0; index < siblings.length; index++) {
      const block = siblings[index];
      if (index > 0 && siblings[index - 1].type === "numbered_list_item") offset++;
      else { first = block; offset = 0; }
      starts.set(block.id, { first, offset });
      if (block.type !== "numbered_list_item") { previous = null; continue; }
      const place: NumberedPlace = { number: storedStart(block) ?? (previous ? previous.number + 1 : 1),
        format: numberedListStoredFormat(block) ?? previous?.format ?? null };
      places.set(block.id, place); previous = place;
    }
  }
  const result = { byId, starts, places }; runIndexes.set(blocks, result);
  return result;
}

/** The run of adjacent numbered items a block stands in: its first item, and how far after it the block is. */
export function numberedListRunStart(blocks: readonly GraphBlock[], block: GraphBlock) {
  return runIndex(blocks).starts.get(block.id) ?? { first: block, offset: 0 };
}

// A numbered item's marker column, as Notion's (seen 2026-11-11): 24px for
// a marker of up to three characters ("99.", "zz."), 32px for four, 42px for
// five and the marker's own width from six ("10000."). Roman numerals stand
// at the column's start, which is as wide as the widest numeral up to the
// item's number: 24px, and each time a numeral comes wider than that, its
// width and 14px more ("xiv." 38.52px, "xxviii." 53.88px) — by Notion's
// widths of the letters at 16px. Past a hundred thousand Leafen takes the
// numeral's own width and 14px, not counting every one before it.
const ROMAN_LETTER_WIDTHS: Readonly<Record<string, number>> = { i: 3.64, v: 8.36, x: 8.08, l: 3.73, c: 8.64, d: 9.52, m: 13.61 };
const ROMAN_COLUMN_COUNTED_TO = 100_000;
const romanColumns: number[] = [24];

function romanWidth(numeral: string) {
  let width = 4.44;
  for (const char of numeral) width += ROMAN_LETTER_WIDTHS[char] ?? 0;
  return width;
}

function romanColumn(value: number) {
  let column = romanColumns[romanColumns.length - 1];
  for (let next = romanColumns.length + 1; next <= Math.min(value, ROMAN_COLUMN_COUNTED_TO); next++) {
    const width = romanWidth(roman(next));
    if (width > column) column = width + 14;
    romanColumns.push(column);
  }
  return value <= romanColumns.length ? romanColumns[value - 1] : Math.max(column, romanWidth(roman(value)) + 14);
}

export type NumberedListMarker = { text: string; width: number | "auto"; roman: boolean };

export function numberedListMarkerColumn(blocks: readonly GraphBlock[], block: GraphBlock): NumberedListMarker {
  const { byId, places } = runIndex(blocks);
  const place = places.get(block.id) ?? { number: storedStart(block) ?? 1, format: numberedListStoredFormat(block) };
  let depth = 0;
  for (let parent = block.parentId ? byId.get(block.parentId) : undefined; parent;
    parent = parent.parentId ? byId.get(parent.parentId) : undefined) {
    if (parent.type === "numbered_list_item") depth += 1;
  }
  const format = place.format ?? DEPTH_FORMATS[depth % DEPTH_FORMATS.length] ?? "numbers";
  const text = `${formatNumberedListMarker(place.number, format)}.`;
  if (format === "roman" && place.number < ROMAN_DIGITS_FROM) return { text, width: romanColumn(place.number), roman: true };
  return { text, width: text.length <= 3 ? 24 : text.length === 4 ? 32 : text.length === 5 ? 42 : "auto", roman: false };
}
