import type { TextSpan } from "@/lib/types";

export type FormulaReference = {
  kind: "page" | "person";
  id: string;
  name: string;
  email?: string;
  /** The database a related page is a row of, so .prop() can read it. */
  databaseId?: string;
};

// A date: its ISO text, a day ("2026-01-02"), a time
// ("2026-01-02T03:04:05Z", a wall-clock time, or with its own offset), or a
// range of them joined by "/"; zone names the time zone its times keep, as
// a date property's time zone does. A function that reads text reads that
// text.
export class FormulaDate {
  readonly text: string;
  readonly zone: string;

  constructor(text: string, zone = "") {
    this.text = text;
    this.zone = zone;
  }

  toString() {
    return this.text;
  }

  toJSON() {
    return this.text;
  }
}

// Rich text: text with formatting, links and mentions, as the spans a title
// or text cell keeps (formulaRichText.ts). A function that reads text reads
// its plain text.
export class FormulaRichText {
  readonly spans: TextSpan[];

  constructor(spans: TextSpan[]) {
    this.spans = spans;
  }

  toJSON() {
    return { rich_text: this.spans };
  }
}

export type FormulaValue = string | number | boolean | null | FormulaDate | FormulaRichText | FormulaReference | FormulaValue[];

export const MAX_FORMULA_LIST_ITEMS = 1_000;
export const MAX_FORMULA_LIST_DEPTH = 10;
// A value's text, in UTF-16 units: Notion's table showed a 10,000,000
// character value whole; its Public API counted 100,000,000 but its screen
// did not live through one (seen 2026-10-14). Past it a formula fails here.
export const MAX_FORMULA_VALUE_TEXT = 10_000_000;

export type FormulaTokenType =
  | "number"
  | "string"
  | "identifier"
  | "operator"
  | "paren"
  | "bracket"
  | "dot"
  | "comma"
  | "question"
  | "colon"
  | "comment";

export type FormulaToken = {
  type: FormulaTokenType;
  value: string;
  // Where the token is written in the formula, as characters from its start
  // up to its end.
  start?: number;
  end?: number;
};

// FormulaSyntaxError is what keeps a formula from being read, with the token
// the reading stopped at and the token it waited for, when there is one.
export class FormulaSyntaxError extends Error {
  readonly start?: number;
  readonly end?: number;
  readonly expected?: string;
  /** Whether the formula ended before the reading could go on. */
  readonly atEnd: boolean;

  constructor(message: string, start?: number, end?: number, expected?: string, atEnd = false) {
    super(message);
    this.start = start;
    this.end = end;
    this.expected = expected;
    this.atEnd = atEnd;
  }
}

export const MAX_FORMULA_EXPRESSION_CHARACTERS = 4_096;
// Notion's parser runs out of room at about 256 parentheses inside one
// another, 254 minuses each with its parentheses, 224 calls and 230 lists —
// around there it fails or not from one request to the next (seen
// 2026-11-12) — and Leafen's spends a fixed share of the same room on each:
// an expression 1000, a call's or method's values 142 more, a list's 112
// more, a minus or "not" 8, as the server's parser does
// (database_formula_syntax.go).
export const FORMULA_NESTING_ROOM = 257_500;
export const FORMULA_NESTING_COST = { expression: 1000, call: 142, list: 112, unary: 8 } as const;
export const MAX_FORMULA_STEPS = 100_000;

// The functions the formula evaluator knows. The server's evaluator knows
// the same functions and must give the same results.
export const FORMULA_FUNCTIONS = new Set([
  "prop", "id", "if", "ifs", "let", "lets", "and", "or", "not", "empty", "equal", "unequal",
  "concat", "repeat", "format", "formatNumber", "toNumber", "lower", "upper", "trim",
  "startsWith", "endsWith", "contains", "substring", "replace", "replaceAll", "test", "match",
  "split", "join", "length", "padStart", "padEnd", "link", "style", "unstyle", "name", "email",
  "add", "subtract", "multiply", "divide", "mod", "pow", "min", "max", "sum", "mean", "median",
  "sqrt", "cbrt", "exp", "ln", "log10", "log2", "sign", "pi", "e", "round", "floor", "ceil", "abs",
  "now", "today", "dateAdd", "dateSubtract", "dateBetween", "dateRange", "parseDate", "dateStart",
  "dateEnd", "timestamp", "fromTimestamp", "formatDate", "year", "month", "day", "date", "week",
  "hour", "minute", "nextWorkday", "addWorkdays",
  "at", "first", "last", "slice", "sort", "reverse", "unique", "includes", "find", "findIndex",
  "filter", "some", "every", "map", "flat", "splice", "count",
]);

export const FORMULA_LITERALS = new Set(["true", "false", "null", "current", "index", "and", "or", "not"]);

/**
 * What a backslash and the next character write in a string, as Notion's
 * (seen 2026-11-04): the quote, a backslash, a new line and a tab; anything
 * else keeps its backslash, so "\\d" reads as a pattern's digit.
 */
function formulaStringEscape(character: string, quote: string) {
  if (character === quote || character === '"' || character === "\\") return character;
  if (character === "n") return "\n";
  if (character === "t") return "\t";
  return `\\${character}`;
}

// Where a comment opened at `start` ("/*") ends as Notion's parser reads it
// (seen 2026-11-14): at the first "*/" after its opening, a "*" with
// something after it read with that character ("1 /** x **/ + 2" runs on
// past "**/" to the end and is 1), at the formula's end when it never
// closes ("1 /* x" is 1), or -1 where a lone "*" ends the formula ("1 /* a *"
// reads "/" and "*" and does not read at all).
export function formulaCommentEnd(input: string, start: number) {
  if (input[start] !== "/" || input[start + 1] !== "*") return -1;
  let i = start + 2;
  while (i < input.length) {
    if (input[i] !== "*") i += 1;
    else if (i + 1 >= input.length) return -1;
    else if (input[i + 1] === "/") return i + 2;
    else i += 2;
  }
  return input.length;
}

// Where a comment opened at `start` ends as Notion's editor colours it: at
// the first "*/" after its opening ("/** x **/" whole), or -1 when it never
// closes, its "/" and "*" then coloured as operators (seen 2026-11-14).
function shownFormulaCommentEnd(input: string, start: number) {
  if (input[start] !== "/" || input[start + 1] !== "*") return -1;
  const close = input.indexOf("*/", start + 2);
  return close < 0 ? -1 : close + 2;
}

// Formulas written only of comments, which read as nothing to Notion's
// parser ("Expression expected") rather than as an empty formula.
const onlyComments = new WeakSet<FormulaToken[]>();

/** Whether a formula's tokens are none because it is only comments. */
export const formulaOnlyComments = (tokens: FormulaToken[]) => onlyComments.has(tokens);

/**
 * A character no token starts with, which keeps a formula from being read,
 * as Notion's reading refuses it (seen 2026-11-27 in Chrome, two tabs: "1 # 2"
 * names "#" at [2,3], then the end it wanted at [4,5]; "'a' + 1" each quote):
 * one for a run of such characters ("1 #@ 2" names "#" alone), its place the
 * run's first character.
 */
export type FormulaInvalidCharacter = { character: string; start: number; end: number; /** Where its run ends. */ through: number };
const invalidCharacters = new WeakMap<FormulaToken[], FormulaInvalidCharacter[]>();

/** The characters of a formula its tokens leave out because none starts with them. */
export const formulaInvalidCharacters = (tokens: FormulaToken[]): readonly FormulaInvalidCharacter[] => invalidCharacters.get(tokens) ?? [];

/** The text with each character no token starts with written as a space, for what reads it as a space (the helper's place). */
export function formulaBlankInvalidCharacters(input: string) {
  const runs = formulaInvalidCharacters(tokenizeFormula(input));
  if (!runs.length) return input;
  const characters = [...input];
  let text = "", at = 0;
  for (const character of characters) {
    const blank = runs.some((run) => at >= run.start && at < run.through);
    text += blank ? " ".repeat(character.length) : character;
    at += character.length;
  }
  return text;
}

/**
 * A name is any letters, "_" and, after its first, digits 0-9, as Notion's
 * parser reads one: a Korean name typed bare, as dateBetween(마감일, now(),
 * "days"), is a name not defined at [12,15] (seen 2026-11-27 in Chrome; its
 * API read names so on 2026-11-12). The server reads them the same
 * (databaseFormulaNameStart).
 */
export const FORMULA_NAME_START = /[\p{L}_]/u, FORMULA_NAME_PART = /[\p{L}_0-9]/u;
/** A whole name, as a variable a let() names. */
export const FORMULA_NAME = /^[\p{L}_][\p{L}_0-9]*$/u;

const FORMULA_WORD_OPERATORS = new Set(["and", "or", "not"]);

/**
 * Whether the name at `index` is called: a parenthesis follows it, right
 * after it where the name is and, or or not, which before a space and a
 * parenthesis are their operators, as Notion's reading takes them ("not
 * (true)" is not before a value in parentheses, "not (true, false)" wants ")"
 * at [9,10], "true and (false)" reads and "and (true, false)" wants a value
 * at [0,1], while "abs (1)" is a call; seen 2026-11-28 in Chrome, two tabs).
 * The server reads them the same (databaseFormulaSyntax.keyword).
 */
export function formulaNameCalled(tokens: readonly FormulaToken[], index: number) {
  const name = tokens[index], next = tokens[index + 1];
  return name?.type === "identifier" && next?.type === "paren" && next.value === "(" &&
    (!FORMULA_WORD_OPERATORS.has(name.value) || next.start === name.end);
}

/**
 * A formula's tokens. Comments are left out, as Notion's parser reads them;
 * `shown` keeps them as "comment" tokens where its editor colours them.
 */
export function tokenizeFormula(input: string, shown = false): FormulaToken[] {
  const tokens: FormulaToken[] = [];
  let commented = false;
  let i = 0;
  const invalid: FormulaInvalidCharacter[] = [];
  const push = (token: FormulaToken, start: number) => tokens.push({ ...token, start, end: i });
  while (i < input.length) {
    const ch = input[i];
    const start = i;
    if (/\s/.test(ch)) {
      i += 1;
      continue;
    }
    // Only a double quote starts text: Notion's editor names a single one an
    // invalid character ("'a'" is "'" at [0,1], a at [1,2] not defined and "'"
    // at [2,3], seen 2026-11-27 and 2026-11-28 in Chrome).
    if (ch === "\"") {
      const quote = ch;
      let value = "";
      i += 1;
      while (i < input.length && input[i] !== quote) {
        if (input[i] === "\\" && i + 1 < input.length) {
          value += formulaStringEscape(input[i + 1], quote);
          i += 2;
        } else {
          value += input[i];
          i += 1;
        }
      }
      // Text left open is not text where the editor colours it: its quote
      // keeps the text's colour and what follows is read on, as Notion's
      // highlighter does ('"abc + 1' colours + and 1, seen 2026-11-28 in
      // Chrome, two tabs); its reading names it (formulaSyntaxProblems.ts).
      if (shown && i >= input.length) {
        i = start + 1;
        continue;
      }
      i += 1;
      push({ type: "string", value }, start);
      continue;
    }
    // A number starts with a digit and takes one point with a digit after it: a point before a name is a
    // method's, as in 1.equal(1) (seen 2026-11-12), and one with no digit after it ("1.", ".5") is a point
    // waiting for a property or function (Notion's editor, seen 2026-11-15: "1." [1,2], ".5" [0,1] and [1,2]).
    if (/[0-9]/.test(ch)) {
      let value = ch;
      let seenDot = false;
      i += 1;
      while (i < input.length && (/[0-9]/.test(input[i]) || (input[i] === "." && !seenDot && /[0-9]/.test(input[i + 1] ?? "")))) {
        if (input[i] === ".") seenDot = true;
        value += input[i];
        i += 1;
      }
      // An exponent, as in 1e21 or 2.5E-3.
      const exponent = /^[eE][+-]?[0-9]+/.exec(input.slice(i));
      if (exponent) {
        value += exponent[0];
        i += exponent[0].length;
      }
      push({ type: "number", value }, start);
      continue;
    }
    if (FORMULA_NAME_START.test(ch)) {
      let value = ch;
      i += 1;
      while (i < input.length && FORMULA_NAME_PART.test(input[i])) {
        value += input[i];
        i += 1;
      }
      push({ type: "identifier", value }, start);
      continue;
    }
    const commentEnd = shown ? shownFormulaCommentEnd(input, i) : formulaCommentEnd(input, i);
    if (commentEnd >= 0) {
      i = commentEnd;
      if (shown) push({ type: "comment", value: input.slice(start, i) }, start);
      commented = true;
      continue;
    }
    const two = input.slice(i, i + 2);
    if ([">=", "<=", "==", "!=", "&&", "||"].includes(two)) {
      i += 2;
      push({ type: "operator", value: two }, start);
      continue;
    }
    if ("+-*/%^><!".includes(ch)) {
      i += 1;
      push({ type: "operator", value: ch }, start);
      continue;
    }
    if (ch === "(" || ch === ")") {
      i += 1;
      push({ type: "paren", value: ch }, start);
      continue;
    }
    if (ch === "[" || ch === "]") {
      i += 1;
      push({ type: "bracket", value: ch }, start);
      continue;
    }
    if (ch === ".") {
      i += 1;
      push({ type: "dot", value: ch }, start);
      continue;
    }
    if (ch === ",") {
      i += 1;
      push({ type: "comma", value: ch }, start);
      continue;
    }
    if (ch === "?" || ch === ":") {
      i += 1;
      push({ type: ch === "?" ? "question" : "colon", value: ch }, start);
      continue;
    }
    // No token starts here: one invalid character for the run it starts or goes on with.
    const character = String.fromCodePoint(input.codePointAt(i) ?? 0);
    const run = invalid[invalid.length - 1];
    if (run?.through === i) run.through += character.length;
    else invalid.push({ character, start: i, end: i + character.length, through: i + character.length });
    i += character.length;
  }
  if (commented && tokens.length === 0) onlyComments.add(tokens);
  if (invalid.length) invalidCharacters.set(tokens, invalid);
  return tokens;
}

/** Whether `at` stands in a text value of `input`, its closing quote not before it ("da|ys" and an unclosed "da|). */
export function formulaInText(input: string, at: number) {
  const tokens = tokenizeFormula(input.slice(0, at)), last = tokens[tokens.length - 1];
  return last?.type === "string" && (last.end ?? 0) > at;
}
