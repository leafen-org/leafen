import { parseFormulaTokens, parseFormulaTokensRecovering, type FormulaNode } from "./formulaAst";
import { FORMULA_CATALOG, type FormulaReturnType } from "./formulaFunctionCatalog";
import { tokenizeFormula } from "./formulaSyntax";
import { unsafeFormulaPattern } from "./formulaRunError";

// The formula editor names every type problem as Notion's does (seen
// 2026-09-30, all of them 2026-11-14): an arithmetic operator on values that
// aren't both numbers, a comparison of two types, a minus on what isn't a
// number, an argument of a type its function doesn't take, or a method's
// target of such a type, each where it is written. + joins text with
// anything, so it has none. A value whose type the formula can't know (an
// if() whose results differ, items of more than one type; a formula's or
// rollup's property is of what it gives, formulaPropertyTypes.ts) takes any
// type, as do the arguments Notion turns into what its function takes
// (length(5), upper(true)). A value with a problem still has
// its type: (1 - "a") > "x" names both. empty() with nothing to test gives a
// value of no type, which Notion names "undefined" (seen 2026-11-20 through
// its API: empty() * 2 is math on undefined and number); an if() or ifs()
// giving it or another value is of the other's type (if(false, 1, empty()) >
// "a" compares number and text). A
// formula giving nothing but it is of that type, "정의되지 않은 유형" in the
// editor, and filters as text (notion_formula_undefined.go). A list knows its
// items where they all have one type, a let's name and current are of what
// they hold, a relation's page is "block" and a person "person" (seen
// 2026-11-21 in its editor and API: [1, 2].first() > "a" compares number and
// text, "a".first() is first() on a text target, first("a") a text value
// first() can't take; notion_formula_items.go): a list function takes
// a list, join() only as a method's; sum() and its kin a list of numbers; id()
// a page or a person, name() and email() a person; lists, pages and people
// don't compare.
export type FormulaValueType = "number" | "text" | "boolean" | "date" | "array" | "block" | "person" | "undefined" | "unknown";

/** A value's type and, for a list, its items' where they all have one; a page's source. */
export type FormulaShape = { type: FormulaValueType; item?: FormulaShape; source?: string };

/**
 * The type of a property a formula names, of its own database, or with source
 * of the database a page is of (null where that database hasn't it, undefined
 * where its properties aren't known).
 */
export type FormulaPropertyType = (name: string, source?: string) => FormulaValueType | FormulaShape | null | undefined;

/** The type of a name the formula runs with, as an automation's (formulaScopeNames.ts), with its list's items where known. */
export type FormulaNameType = (name: string) => FormulaValueType | FormulaShape;

// Each problem's at is where Notion's check comes to it, which orders it
// among the formula's other problems (formulaEditorModel.ts
// formulaProblemsInOrder): an operation's, comparison's, minus's or
// pattern's once what it is made of is checked, at its end; an argument's
// before what is inside the argument, at its start; a method's target's
// after its target, at the method's name's end.
export type FormulaTypeProblem = (
  | { kind: "operation" | "comparison"; left: FormulaValueType; right: FormulaValueType }
  | { kind: "argument" | "target"; type: FormulaValueType; name: string }
  | { kind: "unary" | "called"; type: FormulaValueType }
  | { kind: "pattern" | "nonBlock" }
  | { kind: "property"; name: string }
) & { start?: number; end?: number; at?: number };

// The functions whose second value is a pattern.
const PATTERNS = new Set(["test", "match", "replace", "replaceAll"]);

const RETURNS = new Map<string, FormulaReturnType>(
  FORMULA_CATALOG.filter((entry) => entry.group !== "builtins").map((entry) => [entry.key, entry.returns]),
);

const ARITHMETIC = new Set(["-", "*", "/", "%", "^"]);
const COMPARISON = new Set([">", ">=", "<", "<="]);

const numbers: readonly FormulaValueType[] = ["number"];
const numbersOrLists: readonly FormulaValueType[] = ["number", "array"];
const dates: readonly FormulaValueType[] = ["date"];
const texts: readonly FormulaValueType[] = ["text"];
const lists: readonly FormulaValueType[] = ["array"];
const pagesOrPeople: readonly FormulaValueType[] = ["block", "person"];
const people: readonly FormulaValueType[] = ["person"];

// The types a function's arguments take, by position; a function in
// VARIADIC takes its one set for every argument.
const ARGUMENTS: Record<string, readonly (readonly FormulaValueType[])[]> = {
  abs: [numbers], round: [numbers, numbers], ceil: [numbers], floor: [numbers], sqrt: [numbers], cbrt: [numbers],
  exp: [numbers], ln: [numbers], sign: [numbers], pow: [numbers, numbers], mod: [numbers, numbers],
  subtract: [numbers, numbers], multiply: [numbers, numbers], divide: [numbers, numbers],
  dateAdd: [dates, numbers, texts], dateSubtract: [dates, numbers, texts], dateBetween: [dates, dates, texts],
  dateRange: [dates, dates], dateStart: [dates], dateEnd: [dates], formatDate: [dates, texts], timestamp: [dates],
  year: [dates], month: [dates], week: [dates], date: [dates], day: [dates], hour: [dates], minute: [dates],
  nextWorkday: [dates, texts], addWorkdays: [dates, numbers, texts], id: [pagesOrPeople], name: [people], email: [people],
  ...Object.fromEntries(["first", "last", "at", "find", "findIndex", "filter", "some", "every", "map", "sort", "reverse", "unique",
    "includes", "flat", "count", "slice", "splice"].map((name) => [name, [lists]])),
};
const VARIADIC: Record<string, readonly FormulaValueType[]> = {
  sum: numbersOrLists, min: numbersOrLists, max: numbersOrLists, mean: numbersOrLists, median: numbersOrLists, concat: lists,
};
// What a method's own value must be where the function's first value as
// written may be anything.
const TARGETS: Record<string, readonly FormulaValueType[]> = { join: lists };
// The functions whose last value runs for each item with current and index.
const EACH_ITEM = new Set(["map", "filter", "find", "findIndex", "some", "every", "count", "sort"]);

/** The type a property's values have in a formula, with a list's items'. */
export function formulaPropertyShape(type: string | undefined, source?: string): FormulaShape {
  switch (type) {
    case "relation":
      return { type: "array", item: { type: "block", ...(source ? { source } : {}) } };
    case "person":
      return { type: "array", item: { type: "person" } };
    case "multi_select": case "files":
      return { type: "array", item: { type: "text" } };
    case "created_by": case "last_edited_by":
      return { type: "person" };
    default:
      return { type: formulaPropertyValueType(type) };
  }
}

/** The type a property's values have in a formula. */
export function formulaPropertyValueType(type: string | undefined): FormulaValueType {
  switch (type) {
    case "number":
      return "number";
    case "checkbox":
      return "boolean";
    case "date": case "created_time": case "last_edited_time":
      return "date";
    case "multi_select": case "relation": case "person":
      return "array";
    case "title": case "rich_text": case "url": case "email": case "phone": case "select": case "status": case "files":
      return "text";
    default:
      return "unknown";
  }
}

function returned(name: string): FormulaValueType {
  const type = RETURNS.get(name);
  if (type === "list") return "array";
  return type === "number" || type === "text" || type === "boolean" || type === "date" ? type : "unknown";
}

// item is the type every item of a list has, when they share one; mixed,
// that an if()'s results are of types known to differ.
type Checked = { type: FormulaValueType; item?: Checked; source?: string; mixed?: true };

const shape = (found: FormulaValueType | FormulaShape | null | undefined): Checked =>
  (typeof found === "string" ? { type: found } : found ?? { type: "unknown" });
const listOf = (item: Checked | undefined): Checked => ({ type: "array", ...(item && item.type !== "unknown" ? { item } : {}) });
const UNCOMPARABLE = new Set<FormulaValueType>(["array", "block", "person"]);

class FormulaTypeChecker {
  // The problems found, in the order Notion's check names them.
  readonly problems: FormulaTypeProblem[] = [];

  // The names let() and the each-item functions give, innermost last.
  private readonly scopes: Map<string, Checked>[] = [];
  // The database of each page property the formula names, by its name's place;
  // where the first property it names that no database has is (lost).
  readonly sources = new Map<number, string>();
  lostAt: number | undefined;

  constructor(
    private readonly propertyType: FormulaPropertyType,
    private readonly nameType: FormulaNameType = () => "unknown",
  ) {}

  private named(name: string): Checked {
    for (let index = this.scopes.length - 1; index >= 0; index -= 1) {
      const bound = this.scopes[index].get(name);
      if (bound) return bound;
    }
    return shape(this.nameType(name));
  }

  private within<T>(names: Map<string, Checked>, read: () => T): T {
    this.scopes.push(names);
    try {
      return read();
    } finally {
      this.scopes.pop();
    }
  }

  check(node: FormulaNode): Checked {
    switch (node.kind) {
      case "number":
        return { type: "number" };
      case "string":
        return { type: "text" };
      case "identifier":
        return node.name === "true" || node.name === "false" ? { type: "boolean" } : this.named(node.name);
      case "list":
        return listOf(alike(node.items.map((item) => this.check(item))));
      case "unary": {
        const operand = this.check(node.operand);
        if (node.operator !== "-") return { type: "boolean" };
        if (operand.type !== "unknown" && operand.type !== "number") {
          this.problems.push({ kind: "unary", type: operand.type, start: node.start, end: node.end, at: node.end });
        }
        return { type: "number" };
      }
      case "ternary": {
        const parts = [node.condition, node.whenTrue, node.whenFalse].map((part) => this.check(part));
        return results(parts.slice(1));
      }
      case "binary":
        return this.binary(node);
      case "call":
        return this.call(node.name, node.args, node.end);
      case "method":
        return this.method(node);
      case "called":
        return this.called(node);
    }
  }

  // A value of a known type can't be called: named after what the value is
  // made of and before its values, over the value and its parentheses, as
  // Notion's (seen 2026-11-15: "abs(1)(foo)" names a number value called
  // [0,11], then foo); what it gives is not known.
  private called(node: Extract<FormulaNode, { kind: "called" }>): Checked {
    const callee = this.check(node.callee);
    if (callee.type !== "unknown") {
      this.problems.push({ kind: "called", type: callee.type, start: node.start, end: node.end, at: (node.callee.end ?? 0) + 0.5 });
    }
    for (const arg of node.args) this.check(arg);
    return { type: "unknown" };
  }

  private binary(node: Extract<FormulaNode, { kind: "binary" }>): Checked {
    const left = this.check(node.left);
    const right = this.check(node.right);
    const known = left.type !== "unknown" && right.type !== "unknown";
    const place = { start: node.start, end: node.end, at: node.end };
    if (ARITHMETIC.has(node.operator)) {
      if (known && (left.type !== "number" || right.type !== "number")) {
        this.problems.push({ kind: "operation", left: left.type, right: right.type, ...place });
      }
      return { type: "number" };
    }
    if (COMPARISON.has(node.operator)) {
      if (known && (left.type !== right.type || UNCOMPARABLE.has(left.type))) {
        this.problems.push({ kind: "comparison", left: left.type, right: right.type, ...place });
      }
      return { type: "boolean" };
    }
    // + adds two numbers and joins anything else as text, where neither is of
    // a type that can't be told (Notion's, seen 2026-11-21 in its automation
    // and database formula editors: now() + 1, true + 1, [1] + 1, empty() + 1
    // and a date property + 1 are text, if(true, 1, "a") + 1 can't be told).
    if (node.operator === "+") {
      const type = left.type === "text" || right.type === "text" ? "text"
        : left.type === "unknown" || right.type === "unknown" ? "unknown"
          : left.type === "number" && right.type === "number" ? "number" : "text";
      return { type };
    }
    return { type: "boolean" };
  }

  // A method's target is checked first, its problem after what is inside
  // it and named over the target and the method's name, as Notion's (seen
  // 2026-11-14: "a".abs() is abs() on a text target [0,7]); its other values
  // are a call's.
  private method(node: Extract<FormulaNode, { kind: "method" }>): Checked {
    const target = this.check(node.receiver);
    if (node.name === "prop") return this.pageProperty(node, target);
    const allowed = TARGETS[node.name] ?? VARIADIC[node.name] ?? ARGUMENTS[node.name]?.[0];
    if (allowed && target.type !== "unknown" && !this.fits(node.name, allowed, target)) {
      const end = node.nameEnd ?? node.end;
      this.problems.push({ kind: "target", type: target.type, name: node.name, start: node.start, end, at: end });
    }
    return this.call(node.name, node.args, node.end, target);
  }

  // Each value is named when it doesn't fit before what is inside it, value
  // by value, as Notion's (seen 2026-11-14: dateAdd(abs("a"), 1, "days")
  // names dateAdd's value, then abs's); a pattern after them all.
  private call(name: string, args: readonly FormulaNode[], end: number | undefined, target?: Checked): Checked {
    const offset = target ? 1 : 0;
    const types: Checked[] = target ? [target] : [];
    // let()'s and lets()' names are of their values in what follows.
    const bound = name === "let" || name === "lets" ? new Map<string, Checked>() : undefined;
    for (const [index, arg] of args.entries()) {
      const before = this.problems.length;
      const each = EACH_ITEM.has(name) && index + offset === 1
        ? new Map<string, Checked>([["current", types[0]?.item ?? { type: "unknown" }], ["index", { type: "number" }]]) : undefined;
      const checked = this.within(each ?? bound ?? new Map(), () => this.check(arg));
      types.push(checked);
      const named = args[index - 1];
      if (bound && index % 2 === 1 && index < args.length - 1 && named?.kind === "identifier") bound.set(named.name, checked);
      const allowed = VARIADIC[name] ?? ARGUMENTS[name]?.[index + offset];
      if (allowed && checked.type !== "unknown" && !this.fits(name, allowed, checked)) {
        this.problems.splice(before, 0, { kind: "argument", type: checked.type, name, start: arg.start, end: arg.end, at: arg.start });
      }
    }
    if (name === "prop") {
      const [reference] = args;
      if (reference?.kind !== "string" || this.lostAt !== undefined) return { type: "unknown" };
      const found = this.propertyType(reference.value);
      if (found === null) this.lostAt = reference.start ?? 0;
      return shape(found);
    }
    // A pattern written in the formula that repeats a repetition is named
    // where it is written, whether or not it runs, as Notion's (seen
    // 2026-11-14: if(false, test("aaa", "(a+)+"), false) is [22,29]; its
    // API refuses the formula, notion_formula_type_check.go).
    const pattern = PATTERNS.has(name) ? args[1 - offset] : undefined;
    if (pattern?.kind === "string" && unsafeFormulaPattern(pattern.value)) {
      this.problems.push({ kind: "pattern", start: pattern.start, end: pattern.end, at: end });
    }
    if (name === "if" || name === "ifs") return this.branches(name, types);
    if (name === "empty" && !target && args.length === 0) return { type: "undefined" };
    if (bound) return types[types.length - 1] ?? { type: "unknown" };
    return listResult(name, types) ?? { type: returned(name) };
  }

  // A page's prop() is of its database's property, as Notion's editor reads
  // it (seen 2026-11-21: through a relation to another database,
  // prop("Rel2").first().prop("Text2") * 2 is math on text and number,
  // .prop("Nope") names Nope as no property, a value that isn't a page is
  // refused as one — a list of pages too, once its name is valid — each over
  // the whole call).
  // Once a property it names isn't found, those it names after it aren't
  // looked up: no type, no chip, no problem (seen 2026-11-21 twice:
  // prop("Nope") + prop("Text") * 2 names Nope alone, Text not a chip).
  // What isn't a page reads its name in its own database first ("a".prop("Text2")
  // names Text2, "a".prop("Text") the non-block).
  private pageProperty(node: Extract<FormulaNode, { kind: "method" }>, target: Checked): Checked {
    for (const arg of node.args) this.check(arg);
    const [reference] = node.args;
    const place = { start: node.start, end: node.end, at: node.end };
    const pages = target.type === "array" && target.item?.type === "block" ? target.item : target;
    if (target.type === "unknown" || reference?.kind !== "string" || this.lostAt !== undefined) return { type: "unknown" };
    const notPage = pages.type !== "block" || target.type === "array" && !pages.source;
    if (!notPage && !pages.source) return { type: "unknown" };
    if (!notPage) this.sources.set(reference.start ?? 0, pages.source!);
    const found = this.propertyType(reference.value, notPage ? undefined : pages.source);
    if (found === null) {
      this.lostAt = reference.start ?? 0;
      this.problems.push({ kind: "property", name: reference.value, ...place });
    } else if (notPage || target.type === "array") this.problems.push({ kind: "nonBlock", ...place });
    return notPage || target.type === "array" ? { type: "unknown" } : shape(found);
  }

  // Whether a value fits what a function takes: sum() and its kin a list of
  // numbers, or one whose items aren't known.
  private fits(name: string, allowed: readonly FormulaValueType[], checked: Checked) {
    if (!allowed.includes(checked.type)) return false;
    return !(VARIADIC[name] === numbersOrLists && checked.type === "array" && checked.item && checked.item.type !== "number");
  }

  // What if() and ifs() give is the type all their results share, as
  // Notion's names it (seen 2026-11-09: if(…, now(), parseDate("")) is a
  // date); results of two types leave it open, as does an if() without its
  // three values (2026-11-10: if(true, now()) gives "알 수 없음").
  private branches(name: string, types: readonly Checked[]): Checked {
    if (name === "if" && types.length !== 3) return { type: "unknown", mixed: true };
    return results(name === "if" ? types.slice(1) : types.filter((_, index) => index % 2 === 1 || index === types.length - 1));
  }
}

// Whether two types are the same, lists by their items.
const same = (left: Checked | undefined, right: Checked | undefined): boolean =>
  left?.type === right?.type && left?.source === right?.source && (left?.type !== "array" || same(left?.item, right?.item));

// The type a list's items all have, none where they differ — an undefined one
// among numbers too ([1, empty()].first() > "a" is taken, 2026-11-21).
function alike(items: readonly Checked[]): Checked | undefined {
  const [first] = items;
  return first && items.every((item) => same(item, first)) ? first : undefined;
}

// The type results share, undefined ones aside, or none, marked mixed when
// each is known and they differ; lists share theirs where their items do.
function results(all: readonly Checked[]): Checked {
  const checked = all.filter((result) => result.type !== "undefined");
  if (!checked.length && all.length) return { type: "undefined" };
  const [first] = checked;
  if (first && checked.every((result) => same(result, first))) return { type: first.type, ...(first.item ? { item: first.item } : {}) };
  // Lists of items of different types give one of no type known (Notion's
  // if(true, [1], ["a"]) * 2 is taken, 2026-11-21).
  if (first?.type === "array" && checked.every((result) => result.type === "array")) return { type: "unknown" };
  return checked.every((result) => result.type !== "unknown") ? { type: "unknown", mixed: true } : { type: "unknown" };
}

// What a list function gives, where the list it reads tells it (a method's
// own value its first): an item of its items' type, the list itself, the
// list map() makes of what it gives.
function listResult(name: string, types: readonly Checked[]): Checked | undefined {
  const [list] = types;
  switch (name) {
    case "first": case "last": case "at": case "find":
      return list?.item ?? { type: "unknown" };
    case "filter": case "sort": case "reverse": case "slice": case "unique": case "splice":
      return list?.type === "array" ? list : undefined;
    case "map":
      return types.length > 1 ? listOf(types[types.length - 1]) : undefined;
    case "flat":
      return list?.type !== "array" ? undefined : list.item?.type === "array" ? list.item : list.item ? list : listOf(undefined);
    case "concat": {
      const shared = results(types);
      return shared.type === "array" ? shared : undefined;
    }
    case "split": case "match":
      return listOf({ type: "text" });
  }
  return undefined;
}

/**
 * A formula's type problems in the order Notion's editor names them, none
 * when it has none or is too deep to be read, of what it reads where it
 * can't be read whole (formulaAst.ts parseFormulaTokensRecovering);
 * propertyType gives the type of a property the formula names.
 */
export function formulaTypeProblems(
  expression: string,
  propertyType: FormulaPropertyType,
  nameType?: FormulaNameType,
): FormulaTypeProblem[] {
  if (!expression.trim()) return [];
  let tree: FormulaNode;
  try {
    tree = parseFormulaTokensRecovering(tokenizeFormula(expression), expression.length).node;
  } catch {
    return [];
  }
  const checker = new FormulaTypeChecker(propertyType, nameType);
  checker.check(tree);
  return checker.problems;
}

/**
 * Where a formula's page properties are of (their names' places, by
 * database) and where the first property it names that isn't found is.
 */
export function formulaPropertyReading(expression: string, propertyType: FormulaPropertyType) {
  const checker = new FormulaTypeChecker(propertyType);
  try {
    checker.check(parseFormulaTokensRecovering(tokenizeFormula(expression), expression.length).node);
  } catch {
    // Too deep to be read: nothing found.
  }
  return { sources: checker.sources, lostAt: checker.lostAt };
}

/**
 * The type of what a formula gives, and of its list's items when they share
 * one; null when it can't be read.
 */
export function formulaValueTypeOf(
  expression: string,
  propertyType: FormulaPropertyType,
  nameType?: FormulaNameType,
): { type: FormulaValueType; items?: FormulaValueType; item?: FormulaShape; mixed?: true } | null {
  let tree: FormulaNode;
  try {
    tree = parseFormulaTokens(tokenizeFormula(expression));
  } catch {
    return null;
  }
  const { type, item, mixed } = new FormulaTypeChecker(propertyType, nameType).check(tree);
  const items = item?.type;
  return { type, ...(items && items !== "undefined" ? { items } : {}), ...(item ? { item } : {}), ...(mixed ? { mixed } : {}) };
}
