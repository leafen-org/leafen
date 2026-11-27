import { formulaAdd, formulaDivide, formulaMultiply, formulaPower, formulaRemainder, formulaSubtract } from "./formulaDecimal";
import { formulaCbrt, formulaExp, formulaLn, formulaLog10, formulaLog2, formulaPow } from "./formulaMath";
import { formulaCharacters, formulaPadding } from "./formulaCharacters";
import { formatText } from "./formulaDateText";
import { FormulaRunError, FormulaWorkError, unsafeFormulaPattern } from "./formulaRunError";
import { parseFormulaDate } from "./formulaDates";
import { formulaLanguage, type FormulaViewer } from "./formulaLocales";
import {
  joinedValue,
  linkValue,
  mapRichText,
  padRichText,
  repeatRichText,
  richLength,
  styleValue,
  trimRichText,
  unstyleValue,
} from "./formulaRichText";
import { formatNumberValue } from "./formulaFormats";
import { sortedFormulaList } from "./formulaSort";
import { FormulaDate, FormulaRichText, MAX_FORMULA_LIST_ITEMS, MAX_FORMULA_VALUE_TEXT, type FormulaValue } from "./formulaSyntax";
import {
  aggregateNumbers,
  finiteNumber,
  formulaReferenceText,
  isFormulaReference,
  listValue,
  reducerNumbers,
  requireList,
  roundValue,
  sameFormulaValue,
  toBoolean,
  toNumber,
  toText,
} from "./formulaValues";

// Text, number and list functions whose arguments are all evaluated first.
// The server's functions give the same results (database_formula_text.go,
// database_formula_list_values.go).

function toIndex(value: FormulaValue | undefined) {
  return Math.max(0, Math.trunc(finiteNumber(value ?? 0)));
}

function substringValue(args: FormulaValue[]) {
  const text = toText(args[0] ?? "");
  const from = toIndex(args[1]);
  const end = args[2];
  if (end === undefined || end === null || end === "") return text.slice(from);
  return text.slice(from, Math.max(from, toIndex(end)));
}

// A pattern is a regular expression; as in Notion, one that is not fails
// the formula, and so does one repeating a repetition, named as its call's
// failure (seen 2026-11-14: test("aaa", "a" + "(a+)+") names test()'s call
// at [0,26]; one written in the formula is a type problem, formulaTypes.ts).
function pattern(source: string, flags = "") {
  if (unsafeFormulaPattern(source)) throw new FormulaWorkError("A pattern repeating a repetition");
  try {
    return new RegExp(source, flags);
  } catch {
    throw new Error("Formula pattern is not a regular expression");
  }
}

function replaceValue(args: FormulaValue[], all: boolean) {
  const text = toText(args[0] ?? "");
  const source = toText(args[1] ?? "");
  const replacement = toText(args[2] ?? "");
  if (!source) return text;
  return text.replace(pattern(source, all ? "g" : ""), replacement);
}

function matchValue(args: FormulaValue[]) {
  const text = toText(args[0] ?? "");
  const source = toText(args[1] ?? "");
  if (!source) return [];
  return listValue(Array.from(text.match(pattern(source, "g")) ?? []).slice(0, MAX_FORMULA_LIST_ITEMS + 1));
}

// padStart() and padEnd() pad to at most this many characters, as Notion's
// (seen 2026-10-14).
const MAX_FORMULA_PAD_LENGTH = 10_000;
function padValue(args: FormulaValue[], start: boolean) {
  const target = Math.min(Math.trunc(finiteNumber(args[1] ?? 0)), MAX_FORMULA_PAD_LENGTH);
  const fill = args.length > 2 ? toText(args[2]) : " ";
  if (args[0] instanceof FormulaRichText) return padRichText(args[0], target, fill, start);
  const text = toText(args[0] ?? "");
  const missing = target - formulaCharacters(text).length;
  if (missing <= 0 || !fill) return text;
  const padding = formulaPadding(missing, fill);
  return start ? padding + text : text + padding;
}

// How many copies repeat() writes, as Notion counts them (seen 2026-10-13): a
// fraction counts up — 2.4 copies are 3 — nothing below one, and at most
// 10,000; the text is never cut.
const MAX_FORMULA_REPEAT = 10_000;
function formulaRepeatCount(number: number) {
  if (Number.isNaN(number) || number <= 0) return 0;
  return Math.min(MAX_FORMULA_REPEAT, Math.ceil(number));
}

// formulaToNumber reads a value as Notion's toNumber does (seen 2026-10-10):
// a date as its instant's milliseconds, true and false as 1 and 0, and text
// as the number it spells or begins with — "1e3" is 1000, "0x10" 16, "12abc"
// 12 and "1,234" 1 — and blank text and other text as empty; a list, an
// empty one too, fails the formula (seen 2026-11-14). Arithmetic still
// counts an empty value as 0.
function formulaToNumber(value: FormulaValue, timeZone: string): FormulaValue {
  if (Array.isArray(value)) throw new FormulaRunError("toNumberList");
  if (value == null) return null;
  if (typeof value === "number" || typeof value === "boolean") return toNumber(value);
  if (value instanceof FormulaDate) return parseFormulaDate(value, timeZone)?.instant ?? null;
  const text = toText(value).trim();
  if (!text) return null;
  const whole = Number(text);
  if (Number.isFinite(whole)) return whole;
  const leading = Number.parseFloat(text);
  return Number.isFinite(leading) ? leading : null;
}

function textFunction(name: string, args: FormulaValue[], viewer: FormulaViewer): { value: FormulaValue } | undefined {
  const text = (index: number) => toText(args[index] ?? "");
  switch (name) {
    case "format": return { value: formatText(args[0] ?? null, viewer) };
    // It takes numbers only: an empty one fails it, as Notion's (seen
    // 2026-11-14: empty(formatNumber(prop("n"), "number")) of an empty n is
    // no value), as the functions of numbers (NUMBERS_ONLY).
    case "formatNumber":
      if (args.some((value) => value == null)) throw new Error("formatNumber() takes only numbers");
      return { value: formatNumberValue(args, formulaLanguage()) };
    case "toNumber": return { value: args[0] == null ? null : formulaToNumber(args[0], viewer.timeZone) };
    case "lower":
    case "upper": {
      const write = (value: string) => (name === "upper" ? value.toUpperCase() : value.toLowerCase());
      return { value: args[0] instanceof FormulaRichText ? mapRichText(args[0], write) : write(text(0)) };
    }
    case "trim": return { value: args[0] instanceof FormulaRichText ? trimRichText(args[0]) : text(0).trim() };
    case "startsWith": return { value: text(0).startsWith(text(1)) };
    case "endsWith": return { value: text(0).endsWith(text(1)) };
    case "contains": return { value: text(0).includes(text(1)) };
    case "substring": return { value: substringValue(args) };
    case "repeat": {
      const count = formulaRepeatCount(finiteNumber(args[1] ?? 0));
      if (args[0] instanceof FormulaRichText) return { value: repeatRichText(args[0], count) };
      // Measured before it is written: past a value's text the formula fails.
      if (text(0).length * count > MAX_FORMULA_VALUE_TEXT) throw new Error("Formula text is too long");
      return { value: text(0).repeat(count) };
    }
    case "padStart":
    case "padEnd":
      return { value: padValue(args, name === "padStart") };
    case "replace":
    case "replaceAll":
      return { value: replaceValue(args, name === "replaceAll") };
    case "test": {
      const source = text(1);
      return { value: source ? pattern(source).test(text(0)) : false };
    }
    case "match": return { value: matchValue(args) };
    case "split": return { value: listValue(text(0).split(text(1))) };
    case "length": {
      if (Array.isArray(args[0])) return { value: args[0].length };
      return { value: args[0] instanceof FormulaRichText ? richLength(args[0]) : formulaCharacters(text(0)).length };
    }
    case "link": return { value: linkValue(args[0], args[1], viewer) };
    case "style": return { value: styleValue(args, viewer) };
    case "unstyle": return { value: unstyleValue(args, viewer) };
    case "name": return { value: isFormulaReference(args[0]) ? formulaReferenceText(args[0]) : text(0) };
    case "email": return { value: isFormulaReference(args[0]) ? args[0].email ?? "" : "" };
    case "equal": return { value: sameFormulaValue(args[0] ?? null, args[1] ?? null) };
    case "unequal": return { value: !sameFormulaValue(args[0] ?? null, args[1] ?? null) };
    case "empty":
      // With nothing to test it comes to nothing, as Notion's (seen 2026-11-04);
      // false is empty, as 0 and "" are (seen 2026-11-15 through its API and on
      // screen: empty(false), empty(1 == 2) and an unchecked checkbox's
      // empty(prop("c")) are true, empty([false]) false), as the server's
      // (database_formula_text.go).
      if (args.length === 0) return { value: null };
      return {
        value: Array.isArray(args[0]) ? args[0].length === 0
          : args[0] == null || args[0] === "" || args[0] === 0 || args[0] === false,
      };
    case "not": return { value: !toBoolean(args[0] ?? null) };
  }
  return undefined;
}

function relativeIndex(value: FormulaValue | undefined, length: number) {
  const index = Math.trunc(finiteNumber(value ?? 0));
  return index < 0 ? Math.max(0, length + index) : Math.min(index, length);
}

function listFunction(name: string, args: FormulaValue[], viewer: FormulaViewer): { value: FormulaValue } | undefined {
  if (name === "concat") {
    if (!args.some(Array.isArray)) return { value: joinedValue(args, "", viewer) };
    return { value: listValue(args.flatMap((value) => (Array.isArray(value) ? value : [value]))) };
  }
  if (!["at", "first", "last", "slice", "splice", "sort", "reverse", "join", "unique", "includes", "flat"].includes(name)) {
    return undefined;
  }
  const list = requireList(args[0], name);
  switch (name) {
    // An item that isn't there is an empty value, which a list then leaves
    // out, and at() counts a negative index from the end, as Notion's (seen
    // 2026-11-12 through its API: first([]) null, at([1, 2], -1) 2).
    case "at": {
      const given = Math.trunc(finiteNumber(args[1] ?? 0));
      const index = given < 0 ? given + list.length : given;
      return { value: index >= 0 && index < list.length ? list[index] : null };
    }
    case "first": return { value: list.length ? list[0] : null };
    case "last": return { value: list.length ? list[list.length - 1] : null };
    case "slice": {
      const start = relativeIndex(args[1], list.length);
      const end = args.length > 2 ? relativeIndex(args[2], list.length) : list.length;
      return { value: listValue(list.slice(start, Math.max(start, end))) };
    }
    // As Notion's (seen 2026-10-08): nothing is removed unless a count is given,
    // and a start past the end adds at the end.
    case "splice": {
      const start = relativeIndex(args[1], list.length);
      const removed = Math.max(0, Math.trunc(finiteNumber(args[2] ?? 0)) || 0);
      const spliced = list.slice();
      spliced.splice(start, removed, ...args.slice(3));
      return { value: listValue(spliced) };
    }
    // The evaluator orders a sort() by its expression (formulaParser.ts eachItem).
    case "sort": return { value: listValue(sortedFormulaList(list, list, viewer.timeZone, formulaLanguage())) };
    case "reverse": return { value: listValue(list.slice().reverse()) };
    case "join": return { value: joinedValue(list, toText(args[1] ?? ""), viewer) };
    case "unique": {
      const values: FormulaValue[] = [];
      for (const value of list) {
        if (!values.some((candidate) => sameFormulaValue(candidate, value))) values.push(value);
      }
      return { value: listValue(values) };
    }
    case "includes": return { value: list.some((value) => sameFormulaValue(value, args[1] ?? null)) };
  }
  return { value: listValue(list.flatMap((value) => (Array.isArray(value) ? value : [value]))) };
}

function exactLog(value: number, base: number, logarithm: (value: number) => number) {
  const result = logarithm(value);
  const rounded = Math.round(result);
  return Math.abs(result - rounded) < 1e-9 && formulaPow(base, rounded) === value ? rounded : result;
}

// These take numbers only: an empty property given to one fails the
// formula, as Notion's (its code read 2026-11-14; seen: empty(abs(empty)),
// format(round(empty)), pow(2, empty) and round(2, empty) are no value, where
// empty(empty) is true). The server's are the same
// (database_formula_list_values.go databaseFormulaNumbersOnly).
const NUMBERS_ONLY = new Set(["pow", "mod", "sqrt", "cbrt", "exp", "ln", "log10", "log2", "sign", "abs", "floor", "ceil", "round"]);

function numberFunction(name: string, args: FormulaValue[]): { value: FormulaValue } | undefined {
  const number = (index: number) => toNumber(args[index] ?? null);
  if (NUMBERS_ONLY.has(name) && args.some((value) => value == null)) throw new Error(`${name}() takes only numbers`);
  switch (name) {
    case "min":
    case "max":
    case "sum":
    case "mean":
    case "median": {
      const numbers = reducerNumbers(args);
      // Only empty properties have no smallest, largest or middle; they add up to 0.
      if (!numbers.length && name !== "sum" && args.every((value) => value == null)) return { value: null };
      return { value: aggregateNumbers(name, numbers) };
    }
    // Arithmetic as Notion's (formulaDecimal.ts).
    case "add": return { value: args.reduce<number>((sum, _value, index) => formulaAdd(sum, number(index)), 0) };
    case "multiply": return { value: args.length ? args.reduce<number>((product, _value, index) => formulaMultiply(product, number(index)), 1) : 0 };
    case "subtract": return { value: formulaSubtract(number(0), number(1)) };
    case "divide": return { value: formulaDivide(number(0), number(1)) };
    case "mod": return { value: formulaRemainder(number(0), number(1)) };
    case "pow": return { value: formulaPower(number(0), number(1)) };
    case "sqrt": return { value: Math.sqrt(number(0)) };
    case "cbrt": return { value: formulaCbrt(number(0)) };
    case "exp": return { value: formulaExp(number(0)) };
    case "ln": return { value: formulaLn(number(0)) };
    case "log10": return { value: exactLog(number(0), 10, formulaLog10) };
    case "log2": return { value: exactLog(number(0), 2, formulaLog2) };
    case "sign": return { value: Math.sign(number(0)) };
    case "abs": return { value: Math.abs(number(0)) };
    case "floor": return { value: Math.floor(number(0)) };
    case "ceil": return { value: Math.ceil(number(0)) };
    case "round": return { value: roundValue(args[0], args[1]) };
    case "pi": return { value: Math.PI };
    case "e": return { value: Math.E };
  }
  return undefined;
}

// formulaFunction runs a function with its arguments; a date written as
// text is written as the viewer sees it.
export function formulaFunction(name: string, args: FormulaValue[], viewer: FormulaViewer): { value: FormulaValue } | undefined {
  return listFunction(name, args, viewer) ?? numberFunction(name, args) ?? textFunction(name, args, viewer);
}
