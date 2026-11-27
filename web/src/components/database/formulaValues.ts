import { shownNumberText } from "@/lib/numberText";
import { formulaMean, formulaMiddle, formulaSum } from "./formulaDecimal";
import { FormulaRunError } from "./formulaRunError";
import { FormulaDate, FormulaRichText, MAX_FORMULA_LIST_DEPTH, MAX_FORMULA_LIST_ITEMS, MAX_FORMULA_VALUE_TEXT, type FormulaReference, type FormulaValue } from "./formulaSyntax";

// Formula values and their conversions. A date reads as its ISO text
// wherever text is read. The server converts values the same way
// (database_formula_values.go).

export function isFormulaReference(value: FormulaValue | undefined): value is FormulaReference {
  return !!value
    && typeof value === "object"
    && !Array.isArray(value)
    && !(value instanceof FormulaDate)
    && !(value instanceof FormulaRichText)
    && (value.kind === "page" || value.kind === "person")
    && typeof value.id === "string"
    && typeof value.name === "string";
}

// An untitled page has no name, never its ID; a display passes its own word
// for untitled.
export function formulaReferenceText(value: FormulaReference, untitled = "") {
  return value.name || (value.kind === "page" ? untitled : value.id);
}

// toNumber is a value as arithmetic and the number functions read it: a
// number as it is, an infinite one or one that is no number too, as Notion's
// (seen 2026-11-14: round(1e308 * 10) and abs(-1e308 * 10) are Infinity,
// round(sqrt(-1)) no number); true and false as 1 and 0; anything else as
// the number its text spells, or 0.
export function toNumber(value: FormulaValue | undefined) {
  if (typeof value === "number") return value;
  if (typeof value === "boolean") return value ? 1 : 0;
  const n = Number(toText(value ?? 0));
  return Number.isFinite(n) ? n : 0;
}

// finiteNumber is a count or a place read as a number, one that is not
// finite as 0.
export function finiteNumber(value: FormulaValue | undefined) {
  const n = toNumber(value);
  return Number.isFinite(n) ? n : 0;
}

// richPlain is rich text as functions read it: a date or person mention
// without its "@".
export function richPlain(value: FormulaRichText) {
  return value.spans.map((span) => (span.mention === "date" || span.mention === "person" ? span.text.replace(/^@/, "") : span.text)).join("");
}

// richShown is rich text as the cell and the Public API show it: every
// mention with its "@".
export function richShown(value: FormulaRichText) {
  return value.spans.map((span) => (span.mention === "page" ? `@${span.text}` : span.text)).join("");
}

export function toText(value: FormulaValue | undefined): string {
  if (value == null) return "";
  if (value instanceof FormulaDate) return value.text;
  if (value instanceof FormulaRichText) return richPlain(value);
  if (Array.isArray(value)) return value.map((item) => toText(item)).join(",");
  if (isFormulaReference(value)) return formulaReferenceText(value);
  // A number that is no number reads as no text, as Notion's.
  if (typeof value === "number" && Number.isNaN(value)) return "";
  return String(value);
}

export function toBoolean(value: FormulaValue | undefined) {
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  if (Array.isArray(value)) return value.length > 0;
  if (value instanceof FormulaDate) return value.text !== "";
  if (value instanceof FormulaRichText) return richPlain(value) !== "";
  if (isFormulaReference(value)) return true;
  return value !== null && value !== undefined && value !== "";
}

// isBlankFormulaValue is an empty property's value (prop() reads one as
// null). Text that is empty, written "" or made, is text like any other, as
// Notion's formulas take it (seen 2026-11-09): the server reads it the same
// way (database_formula_values.go databaseFormulaBlank).
export function isBlankFormulaValue(value: FormulaValue | undefined) {
  return value == null;
}

// FormulaPropertyResult is another formula's result as prop() reads it: as
// it is, empty text it made too, as Notion's (seen 2026-11-14:
// prop("format(1 / 0)") == "" is true).
export class FormulaPropertyResult {
  constructor(readonly value: FormulaValue) {}
}

// formulaPropertyValue is what prop() reads: a property with nothing in it is
// empty whatever its type, as Notion's formulas read it, so adding to it counts
// it as 0 and most functions of it are empty too; a formula's result is as it
// is. The server reads it the same way (database_formula_values.go).
export function formulaPropertyValue(value: FormulaValue | FormulaPropertyResult): FormulaValue {
  if (value instanceof FormulaPropertyResult) return value.value;
  return value === "" ? null : value;
}

export function formulaValueBudget(value: FormulaValue) {
  let itemCount = 0;
  let textCount = 0;

  const visit = (current: FormulaValue, depth: number) => {
    if (depth > MAX_FORMULA_LIST_DEPTH) {
      throw new Error(`Formula lists may be nested at most ${MAX_FORMULA_LIST_DEPTH} levels`);
    }
    if (Array.isArray(current)) {
      itemCount += current.length;
      if (itemCount > MAX_FORMULA_LIST_ITEMS) {
        throw new Error(`Formula lists may contain at most ${MAX_FORMULA_LIST_ITEMS} items`);
      }
      for (const item of current) visit(item, depth + 1);
      return;
    }
    if (typeof current === "string") textCount += current.length;
    else if (current instanceof FormulaDate) textCount += current.text.length;
    else if (current instanceof FormulaRichText) {
      for (const span of current.spans) textCount += span.text.length + (span.link?.length ?? 0);
    }
    else if (isFormulaReference(current)) textCount += current.id.length + current.name.length;
    if (textCount > MAX_FORMULA_VALUE_TEXT) {
      throw new Error(`Formula values may contain at most ${MAX_FORMULA_VALUE_TEXT} text characters`);
    }
  };

  visit(value, 0);
  return value;
}

export function listValue(values: FormulaValue[]) {
  return formulaValueBudget(values) as FormulaValue[];
}

// requireList is a list function's list; an empty value, as first() of an
// empty list gives, is an empty list (database_formula_list_values.go).
export function requireList(value: FormulaValue | undefined, functionName: string) {
  if (value == null) return [];
  if (!Array.isArray(value)) throw new Error(`${functionName} requires a list`);
  return value;
}

export function sameFormulaValue(left: FormulaValue, right: FormulaValue) {
  // An empty property is the same as another, not as empty text.
  if ((left == null) !== (right == null)) return false;
  // Two numbers are the same as JavaScript's ===: no number is the same as
  // none, not even itself, as Notion's (seen 2026-11-14: sqrt(-1) ==
  // sqrt(-1), equal() and includes() of it false, unique() keeps both);
  // Infinity is itself.
  if (typeof left === "number" && typeof right === "number") return left === right;
  if (isFormulaReference(left) && isFormulaReference(right)) {
    return left.kind === right.kind && left.id === right.id;
  }
  // A number is compared with anything else by its own text, NaN's "NaN" too.
  const text = (value: FormulaValue) => typeof value === "number" ? String(value) : toText(value);
  return text(left) === text(right);
}

const LONE_SURROGATE = /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/g;

// wellFormedValue is a function's result with each lone half of a surrogate
// pair, as cutting text between the halves of an emoji leaves, written as
// U+FFFD, as the server writes it.
export function wellFormedValue(value: FormulaValue): FormulaValue {
  if (typeof value === "string") return value.replace(LONE_SURROGATE, "\ufffd");
  if (Array.isArray(value)) return value.map(wellFormedValue);
  if (value instanceof FormulaRichText) {
    return new FormulaRichText(value.spans.map((span) => ({ ...span, text: span.text.replace(LONE_SURROGATE, "\ufffd") })));
  }
  return value;
}

// isFormulaText is whether a value is text, or a date or rich text, which
// read as text.
export function isFormulaText(value: FormulaValue | undefined): value is string | FormulaDate | FormulaRichText {
  return typeof value === "string" || value instanceof FormulaDate || value instanceof FormulaRichText;
}

// A list given to a numeric function must hold only numbers, an infinite
// one or one that is no number too ([1e308 * 10, 2].max() is Infinity,
// [sqrt(-1), 2].min() no number, as Notion's), and empty items, which it
// leaves out ([1 / 0, 2].sum() is 2); other arguments are read as numbers.
export function reducerNumbers(values: FormulaValue[]) {
  const numbers: number[] = [];
  for (const value of values) {
    if (!Array.isArray(value)) {
      // An empty property isn't one of the numbers.
      if (value != null) numbers.push(toNumber(value));
      continue;
    }
    for (const item of value) {
      if (item == null) continue;
      if (typeof item !== "number") {
        throw new Error("Numeric list functions require only numbers");
      }
      numbers.push(item);
    }
  }
  return numbers;
}

// A list's sum, mean and middle are worked out on the numbers' digits, as
// Notion's (formulaDecimal.ts).
export function aggregateNumbers(name: string, numbers: number[]) {
  if (!numbers.length) return 0;
  if (name === "min") return Math.min(...numbers);
  if (name === "max") return Math.max(...numbers);
  if (name === "median") {
    const sorted = numbers.slice().sort((a, b) => a - b);
    const middle = Math.floor(sorted.length / 2);
    return sorted.length % 2 === 1 ? sorted[middle] : formulaMiddle(sorted[middle - 1], sorted[middle]);
  }
  return name === "mean" ? formulaMean(numbers) : formulaSum(numbers);
}

// The most decimal places round() takes either way, as Notion's.
const MAX_ROUND_PLACES = 12;

// roundValue is round(): to whole numbers without places or with 0, else to
// a whole number of places up to 12 either way by 10 to that power, the
// number nearest it (1e-11, not pow's last bit off); other places — a
// fraction, more than 12 — fail the formula, as Notion's (its code read and
// seen 2026-11-14: round(6.02e23, -11) is 6.0200000000000006e+23;
// round(3.14159, 0.5) and round(3.14159, 13) are no value), as an empty
// property does (formulaFunctions.ts NUMBERS_ONLY). The server rounds the
// same way (database_formula_list_values.go).
export function roundValue(value: FormulaValue | undefined, precisionValue?: FormulaValue) {
  const number = toNumber(value ?? null);
  if (precisionValue == null) return Math.round(number);
  // Read as it is: places that are no number or infinite are no whole number either (round(2, 1e308 * 10)).
  const precision = typeof precisionValue === "number" ? precisionValue : toNumber(precisionValue);
  if (precision === 0) return Math.round(number);
  if (!Number.isInteger(precision)) throw new FormulaRunError("roundNonInteger", { precision: String(precision) });
  if (Math.abs(precision) > MAX_ROUND_PLACES) throw new FormulaRunError("roundTooManyPlaces", { precision: String(precision) });
  const factor = Number(`1e${precision}`);
  return Math.round(number * factor) / factor;
}

// formatFormulaValue is a value as a formula's cell writes it: a number as
// a cell without a number format shows one (numberText.ts shownNumberText:
// a formula's 1/3, kept as 0.333333333333, shows so, 3.33333e-7 0.0000003),
// an infinite one as format() writes it, "Infinity" and "-Infinity", and one
// that is no number as nothing, as Notion's cell (seen 2026-11-14), as the
// server writes it (database_formula_values.go databaseFormulaFormat).
export function formatFormulaValue(value: FormulaValue, untitled = ""): string {
  if (value == null || value === "") return "";
  if (Array.isArray(value)) return value.map((item) => formatFormulaValue(item, untitled)).join(", ");
  if (isFormulaReference(value)) return formulaReferenceText(value, untitled);
  if (value instanceof FormulaDate) return value.text;
  if (value instanceof FormulaRichText) return richShown(value);
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") {
    return Number.isFinite(value) ? shownNumberText(value) : toText(value);
  }
  return value;
}

// The decimal places a formula property's number keeps.
const KEPT_DECIMALS = 12;

// A number rounded to them as Notion's does, by moving the decimal point of
// its shortest text (lodash's round(); seen 2026-11-14: 1/3 is
// 0.333333333333, 5e-13 1e-12, 999999999999.9999 999999999999.9998); past
// 1e296 the moved number is infinite and the result no number.
function keptFormulaNumber(value: number) {
  if (!Number.isFinite(value)) return value;
  const [mantissa, exponent = "0"] = String(value).split("e");
  const moved = Math.round(Number(`${mantissa}e${Number(exponent) + KEPT_DECIMALS}`));
  if (!Number.isFinite(moved)) return Number.NaN;
  const [movedMantissa, movedExponent = "0"] = String(moved).split("e");
  return Number(`${movedMantissa}e${Number(movedExponent) - KEPT_DECIMALS}`) + 0;
}

// keptFormulaValue is a formula property's result as Notion keeps it, as the
// server keeps it (database_formula_values.go databaseFormulaKept): a number,
// and each number a list holds itself, rounded to 12 decimal places; one
// that can't be is empty alone and an empty item in a list.
export function keptFormulaValue(value: FormulaValue): FormulaValue {
  if (typeof value === "number") return keptFormulaNumber(value);
  if (!Array.isArray(value)) return value;
  return value.map((item) => {
    if (typeof item !== "number" || !Number.isFinite(item)) return item;
    const kept = keptFormulaNumber(item);
    return Number.isNaN(kept) ? "" : kept;
  });
}

// A value as the server stores it: a number that is not finite is empty at
// the top and null inside a list.
export function formulaOutputValue(value: FormulaValue, top = true): FormulaValue {
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return top ? "" : null;
    return value + 0;
  }
  if (Array.isArray(value)) return value.map((item) => formulaOutputValue(item, false));
  return value;
}

// flatFormulaItems is a value's items as Notion's formula cell and Public API
// take a list (seen 2026-11-12 under 한지테스트페이지): lists within it flat,
// and its empty items — an empty property, an empty list — left out; a value
// that is no list is its one item.
export function flatFormulaItems(value: FormulaValue | undefined): FormulaValue[] {
  if (value == null) return [];
  return Array.isArray(value) ? value.flatMap(flatFormulaItems) : [value];
}

// shownFormulaListText is a list as Notion's formula cell, page and rollup
// show it: its items' texts between commas, those with no text left out (seen
// 2026-11-19: ["a", "b", ""] shows "a, b", ["", ""] nothing, ["a", " ", "b"]
// "a,  , b"). A copy of the cell and the API keep them ("a,b,"), as Notion's.
export function shownFormulaListText(texts: readonly string[]): string {
  return texts.filter((text) => text !== "").join(", ");
}

// formulaReferencesOf is the people, or the pages, the items are when they are
// people alone or pages alone, which Notion shows as a person or relation
// property shows them, and its Public API names between spaces; null else.
export function formulaReferencesOf(items: readonly FormulaValue[]): FormulaReference[] | null {
  const kind = isFormulaReference(items[0]) ? items[0].kind : null;
  return kind && items.every((item) => isFormulaReference(item) && item.kind === kind) ? items as FormulaReference[] : null;
}
