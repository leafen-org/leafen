import type { DbProperty } from "@/lib/types";
import {
  FORMULA_PAGE_FIELDS,
  FormulaDate,
  formulaPageField,
  formulaPageFieldProperty,
  isFormulaReference,
  resolveFormulaPropertyReference,
  tokenizeFormula,
  type FormulaToken,
  type FormulaValue,
} from "./formulaCore";
import {
  FORMULA_CATALOG, FORMULA_CATALOG_GROUPS, type FormulaCatalogEntry, type FormulaCatalogGroup, type FormulaValueKind,
} from "./formulaFunctionCatalog";
import { formulaPropertyValueType, formulaTypeProblems, formulaValueTypeOf, type FormulaValueType } from "./formulaTypes";
import { formulaPropertyTypes } from "./formulaPropertyTypes";
import { formulaScopeNameType, type FormulaScopeName } from "./formulaScopeNames";

// What the formula editor works out from the text being edited, as Notion's
// editor shows it: the result's type, what keeps the text from being read,
// the name being typed at the cursor, what choosing a suggestion writes, and
// which suggestions it offers where the cursor is.

export type FormulaResultType = "text" | "number" | "checkbox" | "date" | "list" | "page" | "person";

export function formulaResultType(value: FormulaValue): FormulaResultType | null {
  if (value == null || value === "") return null;
  if (typeof value === "number") return "number";
  if (typeof value === "boolean") return "checkbox";
  if (value instanceof FormulaDate) return "date";
  if (Array.isArray(value)) return "list";
  if (isFormulaReference(value)) return value.kind === "person" ? "person" : "page";
  return "text";
}

// formulaResultEmpty is whether a result has nothing in it — no value, empty
// text or an empty list — which the editor shows as "출력 값 없음" with the
// formula's own type, debug mode or not (Notion's, seen 2026-11-12).
export function formulaResultEmpty(value: FormulaValue) {
  return value == null || value === "" || (Array.isArray(value) && !value.length);
}

// formulaListItemType is the type every item of a list has, when they share
// one: Notion's editor names such a list by it ("숫자 목록", seen 2026-10-08),
// and a list of lists by what the inner lists hold ([[1], [2]] is "숫자 목록").
export function formulaListItemType(value: FormulaValue): FormulaResultType | null {
  if (!Array.isArray(value) || !value.length) return null;
  const types = new Set(value.map((item) => (Array.isArray(item) ? formulaListItemType(item) : formulaResultType(item))));
  const [type] = types;
  return types.size === 1 ? type : null;
}

// FormulaProblem is what the editor shows for a formula it can't read, or a
// name in it the formula doesn't know: the key of its words in the
// formulaEditor locale, the token or name they tell, and where it is in the
// formula as characters from its start, which Notion's editor writes after
// the words as "[start,end]".
export type FormulaProblem = {
  key: string; token?: string; name?: string; start?: number; end?: number;
  // A type problem's types, as its words name them.
  left?: string; right?: string; type?: string;
  // A call short of values: how many its function needs and how many it has.
  expected?: number; given?: number;
  // What its words name besides (a run error's places, unit or format).
  values?: Record<string, string>;
  // Its place is in another formula, written as it is there.
  raw?: boolean;
  // Where Notion's check comes to it: a type problem (formulaTypes.ts), a missing value (formulaArity.ts).
  at?: number;
};

// formulaTypeProblemsOf is a formula's type problems as the editor names
// them, in Notion's order; a property's type is its values' type in a
// formula (a formula's or rollup's, formulaPropertyTypes), a page field's
// as the field's, and a name's as it runs with it (formulaScopeNameType).
export function formulaTypeProblemsOf(
  expression: string, properties: readonly DbProperty[], related?: Record<string, DbProperty[]>, names: readonly FormulaScopeName[] = [],
): FormulaProblem[] {
  return formulaTypeProblems(expression, formulaPropertyTypes(properties, true, related), formulaScopeNameType(names)).map((problem): FormulaProblem => {
    const place = { start: problem.start, end: problem.end, at: problem.at };
    switch (problem.kind) {
      case "pattern": case "nonBlock": case "property":
        return problem.kind === "property" ? { key: "unknownProperty", name: problem.name, ...place } : { key: problem.kind === "pattern" ? "unsafePattern" : "nonBlockProperty", ...place };
      case "argument":
        return { key: "typeArgument", type: problem.type, name: problem.name, ...place };
      case "target":
        return { key: "typeTarget", type: problem.type, name: problem.name, ...place };
      case "unary": case "called":
        return { key: problem.kind === "unary" ? "typeUnary" : "typeCalled", type: problem.type, ...place };
    }
    return { key: problem.kind === "operation" ? "typeOperation" : "typeComparison", left: problem.left, right: problem.right, ...place };
  });
}

// formulaProblemsInOrder puts a formula's name and call problems among its
// type problems where Notion's one check of the formula comes to them (seen
// 2026-11-14: (1 - "a") + foo names the math first, foo + (1 - "a") foo
// first, abs(2, 1 - "a") the extra value before the math in it): a value its
// function doesn't take before what is inside it, the others once what they
// are made of is checked; one with no place first.
export function formulaProblemsInOrder(named: readonly FormulaProblem[], types: readonly FormulaProblem[]): FormulaProblem[] {
  const at = (problem: FormulaProblem) => problem.at ?? (problem.key === "argumentExtra" ? problem.start ?? -1 : problem.end ?? -1);
  const waiting = [...named].sort((left, right) => at(left) - at(right));
  const ordered: FormulaProblem[] = [];
  for (const problem of types) {
    while (waiting.length && at(waiting[0]) < (problem.at ?? 0)) ordered.push(waiting.shift() as FormulaProblem);
    ordered.push(problem);
  }
  return [...ordered, ...waiting];
}

// formulaNumberSettings is which number settings a formula property's "속성
// 편집" offers, as Notion's (seen 2026-11-07): a number result takes the
// format, decimal places and display; a list of numbers the format and display
// with no decimal places; any other result, or none yet, only "수식 편집".
export function formulaNumberSettings(expression: string, properties: readonly DbProperty[]): "all" | "format display" | null {
  if (!expression.trim()) return null;
  const shape = formulaValueTypeOf(expression, formulaPropertyTypes(properties, true));
  if (shape?.type === "number") return "all";
  return shape?.type === "array" && shape.items === "number" ? "format display" : null;
}

// formulaProblemPlace is where a problem is, as the editor writes it after
// the problem's words, or "" when it has no one place.
export function formulaProblemPlace(problem: FormulaProblem) {
  return problem.start === undefined || problem.end === undefined ? "" : ` [${problem.start},${problem.end}]`;
}

// Notion's editor colours a formula's tokens as Prism names them, in its
// code blocks' colours (seen 2026-09-30, dark 2026-11-14 — formulaTokenTheme.ts):
// if and the word operators as keywords, other called
// names as functions, numbers and true or false alike, operators, strings,
// brackets and commas as punctuation, and a closed /* … */ as a comment
// (#998066 in dark, seen 2026-11-14). Other names and anything the tokens
// skip keep the text's colour.
export type FormulaHighlightKind = "keyword" | "function" | "number" | "boolean" | "operator" | "string" | "punctuation" | "comment";

const FORMULA_KEYWORDS = new Set(["if", "ifs", "let", "lets", "and", "or", "not"]);

// A formula as its editor writes it: its tokens coloured, and each
// prop("Name") naming a property or page field as one chip with the
// property's type and name, as Notion's editor shows it (seen 2026-09-30);
// a name no property has stays text. A chip keeps the text it stands for.
export type FormulaPiece = {
  text: string;
  kind?: FormulaHighlightKind;
  chip?: { name: string; type: string };
};

export function formulaPieces(
  expression: string,
  chipOf: (name: string, at?: number) => { name: string; type: string } | undefined,
  names: readonly FormulaScopeName[] = [], anyPage = false,
): FormulaPiece[] {
  const pieces: FormulaPiece[] = [];
  const tokens = tokenizeFormula(expression, true);
  const named = (token: FormulaToken | undefined) => token?.type === "identifier" ? names.find((name) => name.name === token.value) : undefined;
  let at = 0;
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    const start = token.start ?? at;
    const [open, reference, close] = tokens.slice(index + 1, index + 4);
    // A property after a page name's "." is that page's: triggerPage.prop("Name"), read from its own fields where it has them.
    const page = tokens[index - 1]?.type === "dot" ? named(tokens[index - 2]) : undefined;
    const ofPage = page?.page === true;
    const field = (name: string) => page?.fields?.find(({ property }) => property.name === name)?.property;
    // So is one after "current." in map() or filter(), a chip as Notion's (seen 2026-11-04), and with anyPage any page's (formulaEditorPieces.ts).
    const ofCurrent = anyPage || tokens[index - 1]?.type === "dot" && tokens[index - 2]?.type === "identifier" && tokens[index - 2]?.value === "current";
    const chip = token.type === "identifier" && token.value === "prop" && (tokens[index - 1]?.type !== "dot" || ofPage || ofCurrent) &&
      open?.type === "paren" && open.value === "(" && reference?.type === "string" &&
      close?.type === "paren" && close.value === ")" ? field(reference.value) ?? chipOf(reference.value, reference.start) : undefined;
    // A name the formula runs with is a chip of its own, as Notion's "페이지 실행".
    const name = !chip && tokens[index - 1]?.type !== "dot" && !(open?.type === "paren" && open.value === "(") ? named(token) : undefined;
    const end = Math.min((chip ? close!.end : token.end) ?? start, expression.length);
    if (start > at) pieces.push({ text: expression.slice(at, start) });
    pieces.push(chip
      ? { text: expression.slice(start, end), chip }
      : name ? { text: expression.slice(start, end), chip: { name: name.label, type: name.chip ?? name.type } }
      : { text: expression.slice(start, end), kind: formulaTokenKind(token, tokens[index + 1]) });
    at = end;
    if (chip) index += 3;
  }
  if (at < expression.length) pieces.push({ text: expression.slice(at) });
  return pieces;
}

// formulaShownOffset is where a place in the formula's text falls as its
// editor shows it, where a chip is one letter, as Notion counts a problem's
// place.
export function formulaShownOffset(pieces: readonly FormulaPiece[], offset: number) {
  let text = 0;
  let shown = 0;
  for (const piece of pieces) {
    if (text >= offset) break;
    if (piece.chip) {
      shown += 1;
    } else {
      shown += Math.min(piece.text.length, offset - text);
    }
    text += piece.text.length;
  }
  return shown;
}

function formulaTokenKind(token: FormulaToken, next: FormulaToken | undefined): FormulaHighlightKind | undefined {
  switch (token.type) {
    case "number":
    case "string":
    case "comment":
      return token.type;
    case "operator":
    case "question":
    case "colon":
      return "operator";
    case "identifier":
      if (FORMULA_KEYWORDS.has(token.value)) return "keyword";
      if (next?.type === "paren" && next.value === "(") return "function";
      return token.value === "true" || token.value === "false" ? "boolean" : undefined;
    default:
      return "punctuation";
  }
}

// formulaErrorKey names an evaluator error in the formulaEditor locale.
export function formulaErrorKey(message: string): string {
  switch (message) {
    case "Unexpected formula token": return "unexpected";
    case "Formula nesting is too deep": return "nesting";
    case "Formula condition is missing its ':'": return "condition";
    case "Formula parentheses are not balanced": return "parentheses";
    case "Formula method name is missing": return "method";
    case "Formula list or call is not closed": return "unclosed";
    case "Formula expression is too long": return "tooLong";
    case "Formula value is missing": return "missing";
  }
  return "invalid";
}

// formulaWordAt is the name being typed right before the cursor. A number
// is not a name: an operator chosen after one is written after it. Nor is
// a written true or false, which Notion takes as the value it is.
export function formulaWordAt(expression: string, cursor: number): { start: number; word: string } {
  let start = cursor;
  // Any letter counts, so a name shown in Korean is found by it (Notion's "페이지" finds "페이지 실행", seen 2026-10-24).
  while (start > 0 && /[\p{L}\p{N}_]/u.test(expression[start - 1])) start -= 1;
  while (start < cursor && /[0-9]/.test(expression[start])) start += 1;
  const word = expression.slice(start, cursor);
  return word === "true" || word === "false" ? { start: cursor, word: "" } : { start, word };
}

// formulaInsert writes a suggestion in place of the name being typed, and
// where the cursor goes after it: at the suggestion's "|", else its end. An
// operator's space before it is left out where a space is already written.
export function formulaInsert(expression: string, cursor: number, insert: string) {
  const { start } = formulaWordAt(expression, cursor);
  const spaced = insert.startsWith(" ") && (start === 0 || /\s/.test(expression[start - 1]));
  const text = spaced ? insert.slice(1) : insert;
  const marker = text.indexOf("|");
  const written = text.replace("|", "");
  return {
    text: expression.slice(0, start) + written + expression.slice(cursor),
    cursor: start + (marker < 0 ? written.length : marker),
  };
}

// formulaPropertyInsert is what choosing a property writes.
export function formulaPropertyInsert(property: Pick<DbProperty, "name">) {
  return `prop(${JSON.stringify(property.name)})|`;
}

export type FormulaHelperContext =
  | { kind: "start" }
  | { kind: "operand" }
  | { kind: "value"; spaced: boolean; dotted: boolean; of: readonly FormulaValueKind[] | null };

// formulaHelperContext is where the cursor stands, as Notion's helper tells
// it apart (seen 2026-10-08): where an expression starts (nothing yet, or
// after "(", "," or "["), after an operator, or after a value — right
// after it, after a space, or after its "." — with the kinds that value is
// (null when the formula can't tell).
function formulaHelperContext(
  expression: string, cursor: number, propertyType: (name: string) => string | undefined,
  names: readonly FormulaScopeName[] = [],
): FormulaHelperContext {
  const before = expression.slice(0, formulaWordAt(expression, cursor).start);
  const trimmed = before.trimEnd();
  if (trimmed === "" || /[(,[]$/.test(trimmed)) return { kind: "start" };
  if (/[-+*/%^=!<>&|?:]$/.test(trimmed) || /\b(?:and|or|not)$/.test(trimmed)) return { kind: "operand" };
  const dotted = trimmed.endsWith(".");
  const value = dotted ? trimmed.slice(0, -1) : trimmed;
  return { kind: "value", spaced: !dotted && trimmed.length < before.length, dotted, of: formulaTrailingKinds(value, propertyType, names) };
}

const KIND_OF_TYPE: Partial<Record<FormulaValueType, FormulaValueKind>> = {
  number: "number", text: "text", boolean: "boolean", date: "date", array: "list", block: "page", person: "person",
};

// The kinds of the value the text ends with: the last operand, with the
// calls and methods written on it.
function formulaTrailingKinds(
  text: string, propertyType: (name: string) => string | undefined, names: readonly FormulaScopeName[] = [],
): FormulaValueKind[] | null {
  let tokens: FormulaToken[];
  try {
    tokens = tokenizeFormula(text);
  } catch {
    return null;
  }
  let index = tokens.length - 1;
  for (;;) {
    const token = tokens[index];
    if (!token) return null;
    if (token.type === "paren" && token.value === ")" || token.type === "bracket" && token.value === "]") {
      let depth = 0;
      for (; index >= 0; index -= 1) {
        const { type, value } = tokens[index];
        if (type === "paren" || type === "bracket") depth += value === ")" || value === "]" ? 1 : -1;
        if (depth === 0) break;
      }
      if (index < 0) return null;
      index -= 1;
      if (tokens[index]?.type === "identifier" && tokens[index + 1].value === "(") index -= 1;
    } else if (token.type === "number" || token.type === "string" || token.type === "identifier") {
      index -= 1;
    } else {
      return null;
    }
    if (tokens[index]?.type !== "dot") break;
    index -= 1;
  }
  const operand = text.slice(tokens[index + 1]?.start ?? 0);
  const name = names.find((candidate) => candidate.name === operand.trim());
  if (name && !name.shape) return name.page ? ["page"] : name.type === "person" ? ["person"] : propertyKind(name.type);
  const named = /^prop\(("(?:[^"\\]|\\.)*")\)$/.exec(operand.trim());
  if (named) {
    const kind = propertyKind(propertyType(JSON.parse(named[1]) as string));
    if (kind) return kind;
  }
  // A name holding what its type doesn't say (an automation's variable) is read as it holds it.
  const shape = formulaValueTypeOf(operand, (name) => formulaPropertyValueType(propertyType(name)), formulaScopeNameType(names));
  if (!shape) return null;
  const kind = KIND_OF_TYPE[shape.type];
  if (!kind) return null;
  return kind === "list" && shape.items === "number" ? ["list", "numbers"] : [kind];
}

// A property's values as the helper tells them apart: a created or edited
// by is one person, where a people property is a list of them.
function propertyKind(type: string | undefined): FormulaValueKind[] | null {
  switch (type) {
    case "created_by": case "last_edited_by": return ["person"];
    case "number": return ["number"];
    case "checkbox": return ["boolean"];
    case "date": case "created_time": case "last_edited_time": return ["date"];
    case "multi_select": case "relation": case "person": return ["list"];
    case "title": case "rich_text": case "url": case "email": case "phone": case "select": case "status": case "files":
      return ["text"];
    default: return null;
  }
}

// formulaPropertyChoices are the properties a formula's editor offers: all
// but the formula itself, the buttons, which hold no value, and the files, as
// Notion's list leaves them out (buttons seen 2026-11-02, files 2026-11-03),
// in the source's own order, then the page's created and edited fields no
// property is named for.
export function formulaPropertyChoices(
  properties: readonly DbProperty[], formula: Pick<DbProperty, "id" | "databaseId">,
): DbProperty[] {
  const fields = FORMULA_PAGE_FIELDS.filter((field) => formulaPageField(properties, field.name))
    .map((field) => formulaPageFieldProperty(field, formula.databaseId));
  return [...properties.filter((property) => property.id !== formula.id && property.type !== "button" && property.type !== "files"), ...fields];
}

export type FormulaSuggestionGroup =
  | { group: "names"; names: FormulaScopeName[] }
  | { group: "properties"; properties: DbProperty[] }
  | { group: FormulaCatalogGroup; entries: FormulaCatalogEntry[] };

export type { FormulaScopeName } from "./formulaScopeNames";

// formulaSuggestions lists what the editor offers at the cursor, by group,
// as Notion's helper does (seen 2026-10-08): where a value is expected the
// properties, then where an expression starts not, true and false, then the
// functions; right after a value only the operators and functions that take
// it; after a value and a space the properties, the operators that take it
// and the functions but let() and lets(). A typed name keeps whatever has
// its letters in order (Notion's "abc" finds dateSubtract), those starting
// with it first, then those holding it, in the same groups (seen again
// 2026-11-15 in Chrome: "da", "dn", "pi", "abc" gave Notion's lists in
// Notion's order). Leafen's own nextWorkday() and addWorkdays() are found
// by the same rule: "to" finds nextWorkday() beside toNumber() and today().
export function formulaSuggestions(
  expression: string, cursor: number, properties: readonly DbProperty[], names: readonly FormulaScopeName[] = [],
): FormulaSuggestionGroup[] {
  const needle = formulaWordAt(expression, cursor).word.toLowerCase();
  const typeOf = (name: string) => (formulaPageField(properties, name) ?? resolveFormulaPropertyReference(properties, name))?.type;
  const context = formulaHelperContext(expression, cursor, typeOf, names);
  // Where a page name stands for the page (an automation's "페이지 실행"), its
  // properties are offered only after its ".", first, as Notion's are (seen
  // 2026-10-23); the names alone elsewhere.
  const paged = names.some((name) => name.page);
  const afterPage = paged && context.kind === "value" && context.dotted && (context.of?.includes("page") ?? false);
  const takes = (entry: FormulaCatalogEntry) => context.kind === "value" && context.of
    ? entry.after?.some((kind) => context.of?.includes(kind)) ?? false
    : entry.after !== undefined;
  const offered = (entry: FormulaCatalogEntry) => {
    if (entry.unlisted) return false;
    if (needle) return true;
    if (context.kind === "start") return entry.group !== "builtins" || entry.start === true;
    if (context.kind === "operand") return entry.group !== "builtins";
    if (context.dotted) return entry.group !== "builtins" && takes(entry);
    if (context.spaced) return entry.group === "builtins" ? takes(entry) : !entry.names;
    return takes(entry);
  };
  const ranked = <T,>(items: readonly T[], name: (item: T) => string) => {
    if (!needle) return [...items];
    const rank = (item: T) => {
      const text = name(item).toLowerCase();
      if (text.startsWith(needle)) return 0;
      if (text.includes(needle)) return 1;
      let at = 0;
      for (const letter of text) if (letter === needle[at]) at += 1;
      return at === needle.length ? 2 : 3;
    };
    return items.map((item) => ({ item, rank: rank(item) })).filter(({ rank }) => rank < 3)
      .sort((left, right) => left.rank - right.rank).map(({ item }) => item);
  };
  const groups: FormulaSuggestionGroup[] = [];
  for (const group of FORMULA_CATALOG_GROUPS) {
    const entries = ranked(FORMULA_CATALOG.filter((entry) => entry.group === group && offered(entry)), (entry) => entry.label);
    if (entries.length) groups.push({ group, entries });
  }
  const valueFirst = context.kind === "value" && !context.spaced;
  const pageFields = names.find((name) => name.page && name.fields)?.fields?.map(({ property }) => property);
  const shown = afterPage ? ranked(pageFields ?? properties, (property) => property.name)
    : paged || (valueFirst && !needle) ? [] : ranked(properties, (property) => property.name);
  const named = afterPage || (valueFirst && !needle) ? [] : ranked(names, (name) => `${name.label} ${name.name}`);
  const valueGroups: FormulaSuggestionGroup[] = [
    ...(named.length ? [{ group: "names" as const, names: named }] : []),
    ...(shown.length ? [{ group: "properties" as const, properties: shown }] : []),
  ];
  return valueFirst && !afterPage ? [...groups, ...valueGroups] : [...valueGroups, ...groups];
}

// The help for a property, as Notion's (seen 2026-11-03, a database of every
// kind): its kind's line, the property itself, then Notion's examples for the
// kind — some written with the property, some with the sample names Notion
// uses ("Priority", "Tags"...), which show as plain chips.
export function formulaPropertyExamples(property: Pick<DbProperty, "name" | "type">): string[] {
  const p = `prop(${JSON.stringify(property.name)})`;
  switch (property.type) {
    case "title": return [`${p}.style("b")`, `prop("Full Name").split(" ").at(0)`];
    case "rich_text": return [`upper(${p})`, `${p}.style("b", "red")`];
    case "number": return [`${p} / 2`, `pi() * ${p} ^ 2`];
    case "select": return [`prop("Priority") == "High"`, `["Feature", "Bug"].includes(prop("Task Type"))`];
    case "status": return [`prop("Status") == "Done"`, `["Not started", "In progress"].includes(prop("Status"))`];
    case "multi_select": return [`${p}.length()`, `prop("Tags").includes("Finance")`];
    case "date": return [`${p} > now()`, `${p}.dateAdd(1, "weeks")`, `dateBetween(${p}, now(), "days")`];
    case "person": return [`${p}.length()`, `${p}.at(0)`, `${p}.map(current.email())`, `${p}.some(current.name() == "Alice")`];
    // Notion's middle name is a word the screen words keep for encryption (docs/screen-words.md).
    case "checkbox": return [`not ${p}`, `prop("Signed") and prop("Paid") and prop("Delivered")`];
    case "url": return [`${p} ? link("Link", ${p}).style("green") : ""`];
    case "email": return [`${p} ? link("Email", "mailto:" + ${p}).style("blue") : ""`];
    case "phone": return [`${p} ? link("Call", "tel:" + ${p}).style("blue") : ""`];
    case "relation": return [`${p}.length()`, `prop("Tasks").filter(current.Status != "Done")`];
    case "rollup": return [`prop("Average cost") * 12`, `prop("Sum of customers") > 100`];
    case "unique_id": return [`${p}.split("-").first() /* Prefix */`, `${p}.split("-").last().toNumber() /* ID */`];
    case "created_time": return [`${p}.dateAdd(1, "weeks")`, `${p} > dateSubtract(now(), 1, "years")`];
    case "last_edited_time": return [`${p}.dateAdd(1, "weeks")`, `${p} > dateSubtract(now(), 1, "month")`];
    case "created_by": case "last_edited_by": return [`${p}.name()`, `${p}.email()`];
    default: return [];
  }
}

/** The kinds whose help line Notion words itself (formulaEditor:propertyHelp). */
export const FORMULA_PROPERTY_HELP_KINDS = new Set([
  "title", "rich_text", "number", "select", "multi_select", "status", "date", "person", "checkbox", "url", "email",
  "phone", "relation", "rollup", "unique_id", "created_time", "created_by", "last_edited_time", "last_edited_by",
]);

const STATIC_RESULT: Record<FormulaValueType, FormulaResultType | "block" | "undefined" | "unknown"> = {
  number: "number", text: "text", boolean: "checkbox", date: "date", array: "list", block: "block", person: "person", undefined: "undefined", unknown: "unknown",
};

// formulaStaticResultType is a formula's type as an automation's editor names
// it, with no page to run it on (Notion's "유형:텍스트", seen 2026-10-23): read
// from its text, its names' types among it.
export function formulaStaticResultType(
  expression: string, properties: readonly DbProperty[], names: readonly FormulaScopeName[],
): FormulaResultType | "block" | "undefined" | "unknown" {
  return formulaStaticResult(expression, properties, names).type;
}

// formulaStaticResult is that type with its list's items' type, and whether its results are of types known to
// differ (an if() of a date and a text); a run's page alone is a block (Notion's "유형:블록", seen 2026-11-21).
export function formulaStaticResult(
  expression: string, properties: readonly DbProperty[], names: readonly FormulaScopeName[], related?: Record<string, DbProperty[]>,
): { type: FormulaResultType | "block" | "undefined" | "unknown"; items?: FormulaResultType | "block" | "undefined" | "unknown"; mixed?: true } {
  const trimmed = expression.trim();
  const alone = names.find((name) => name.name === trimmed && !name.shape);
  if (alone) return { type: alone.page ? "block" : alone.type === "person" ? "person" : alone.type === "date" ? "date" : "text" };
  if (!trimmed) return { type: "unknown" };
  const shape = formulaValueTypeOf(trimmed, formulaPropertyTypes(properties, true, related), formulaScopeNameType(names));
  return { type: STATIC_RESULT[shape?.type ?? "unknown"], ...(shape?.items ? { items: STATIC_RESULT[shape.items] } : {}),
    ...(shape?.mixed ? { mixed: shape.mixed } : {}) };
}

// formulaLoneListItems is what a property read alone holds as a list, which
// its type tells when the list is empty: people, pages, or a multi-select's
// words (Notion's empty people property is "사람 목록", seen 2026-11-12).
export function formulaLoneListItems(expression: string, properties: readonly DbProperty[]): FormulaResultType | undefined {
  const quoted = /^prop\(\s*("(?:[^"\\]|\\.)*")\s*\)$/u.exec(expression.trim())?.[1];
  let name = "";
  try { name = quoted ? String(JSON.parse(quoted)) : ""; } catch { return undefined; }
  const type = name ? (formulaPageField(properties, name) ?? resolveFormulaPropertyReference(properties, name))?.type : undefined;
  return type === "person" ? "person" : type === "relation" ? "page" : type === "multi_select" ? "text" : undefined;
}
