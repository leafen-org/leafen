import type { DbProperty } from "@/lib/types";
import { parseFormulaTokens, parseFormulaTokensRecovering, type FormulaNode } from "./formulaAst";
import { formulaPageField, formulaVariableNames, resolveFormulaPropertyReference } from "./formulaReferences";
import { FORMULA_FUNCTIONS, FORMULA_LITERALS, tokenizeFormula } from "./formulaSyntax";

// Notion's formula editor in debug mode warns, in amber under the result, of
// each function given a value whose type it can't know where the function
// takes one kind (seen 2026-11-12, its type checker's
// CallingFunctionWithUnknownTarget and CallingFunctionWithUnknownArgument;
// formulaEditor:unknownTargetCall and unknownArgumentCall here): one for a
// method's receiver, marked from it to the method's name ([0,16]), and one
// for an argument, marked over the whole call — once for each such value,
// a call's own before those inside it. Its type can't be known for an item of
// an empty list or of one whose items differ ([].first(), [1, "a"].first()),
// for what if(), ifs() or "? :" give when their results differ, for "+" on
// such a value and a number, and for a let's name or current holding one.
// Notion names the type the function takes, the first where it takes more
// (length() "text"); one that takes anything (format(), empty(), toNumber(),
// equal(), and(), …, a condition after ifs()'s first) is never warned of. A
// property's type is its own, a formula's what its formula gives; a rollup's
// and anything else Leafen can't tell are taken as fitting. A name, a
// function or a property the formula doesn't know is of a type not known
// too, so a call given one is warned of beside the problem naming it, as
// Notion's (seen 2026-11-14: abs(foo), abs(zz(1)) and abs(prop("zz")) each
// warn of abs() under the problem naming the unknown name or property).

export type FormulaUnknownCall = { kind: "target" | "argument"; name: string; type: string; start: number; end: number };

type Base = "number" | "text" | "boolean" | "date" | "person" | "page" | "array";
type Type = "any" | "unknown" | { type: Base; items?: Type };

const T = "text", N = "number", D = "date", L = "array", P = "person", B = "boolean";
// What each Notion function takes, by place: the type it names, or null for anything.
const PARAMETERS: Record<string, readonly (string | null)[]> = {
  if: [B, null, null], ifs: [B], length: [T], substring: [T, N, N], contains: [T, T], test: [T, T], match: [T, T],
  replace: [T, T, T], replaceAll: [T, T, T], lower: [T], upper: [T], trim: [T], repeat: [T, N], padStart: [T, N, T],
  padEnd: [T, N, T], link: [T, T], formatNumber: [N, T, N], add: [N, N], subtract: [N, N], multiply: [N, N], mod: [N, N],
  pow: [N, N], divide: [N, N], abs: [N], round: [N, N], ceil: [N], floor: [N], sqrt: [N], cbrt: [N], exp: [N], ln: [N],
  log10: [N], log2: [N], sign: [N], minute: [D], hour: [D], day: [D], date: [D], week: [D], month: [D], year: [D],
  dateAdd: [D, N, T], dateSubtract: [D, N, T], dateBetween: [D, D, T], dateRange: [D, D], dateStart: [D], dateEnd: [D],
  timestamp: [D], fromTimestamp: [N], formatDate: [D, T, T], parseDate: [T], name: [P], email: [P], id: [P],
  at: [L, N], first: [L], last: [L], slice: [L, N, N], splice: [L, N, N], sort: [L, null], reverse: [L], join: [L, T],
  split: [T, T], unique: [L], includes: [L, null], find: [L, null], findIndex: [L, null], filter: [L, null],
  some: [L, null], every: [L, null], map: [L, null], flat: [L], count: [L, null],
};
// Functions taking any number of values, each of one type.
const EVERY: Record<string, string> = { style: T, unstyle: T, min: N, max: N, sum: N, median: N, mean: N, concat: L };
const LAMBDAS = new Set(["sort", "find", "findIndex", "filter", "some", "every", "map", "count"]);
const NUMBERS = new Set(["length", "findIndex", "count", "abs", "round", "ceil", "floor", "sqrt", "cbrt", "exp", "ln", "log10",
  "log2", "sign", "pi", "e", "toNumber", "add", "subtract", "multiply", "mod", "pow", "divide", "min", "max", "sum",
  "median", "mean", "minute", "hour", "day", "date", "week", "month", "year", "dateBetween", "timestamp"]);
const TEXTS = new Set(["format", "substring", "replace", "replaceAll", "lower", "upper", "trim", "repeat", "padStart", "padEnd",
  "link", "style", "unstyle", "join", "formatNumber", "formatDate", "name", "email", "id"]);
const BOOLEANS = new Set(["and", "or", "not", "empty", "equal", "unequal", "contains", "test", "includes", "some", "every",
  "startsWith", "endsWith"]);
const DATES = new Set(["now", "today", "dateAdd", "dateSubtract", "dateRange", "dateStart", "dateEnd", "fromTimestamp", "parseDate"]);
const KEEPS_ITEMS = new Set(["filter", "sort", "reverse", "slice", "splice", "unique"]);

const known = (type: Base, items?: Type): Type => (items ? { type, items } : { type });
const same = (left: Type, right: Type): boolean => left === right
  || typeof left === "object" && typeof right === "object" && left.type === right.type
    && (left.items === undefined && right.items === undefined || left.items !== undefined && right.items !== undefined && same(left.items, right.items));
// What values of these types together are: their one type, anything where one may be, else not known.
function joined(types: readonly Type[]): Type {
  if (types.length === 0 || types.some((type) => type === "any")) return types.length ? "any" : "unknown";
  return types.every((type) => same(type, types[0])) ? types[0] : "unknown";
}
const itemsOf = (type: Type): Type => (type === "any" || type === "unknown" ? type : type.items ?? "any");

function propertyValueType(type: string | undefined): Type {
  switch (type) {
    case "title": case "rich_text": case "url": case "email": case "phone_number": case "phone": case "select": case "status":
      return known("text");
    case "number": return known("number");
    case "checkbox": return known("boolean");
    case "date": case "created_time": case "last_edited_time": return known("date");
    case "multi_select": return known("array", known("text"));
    case "person": return known("array", known("person"));
    case "created_by": case "last_edited_by": return known("person");
    case "relation": return known("array", known("page"));
    default: return "any";
  }
}

class UnknownCalls {
  readonly calls: FormulaUnknownCall[] = [];

  constructor(private readonly property: (name: string) => Type, private readonly named: (name: string) => boolean) {}

  type(node: FormulaNode, names: ReadonlyMap<string, Type>): Type {
    switch (node.kind) {
      case "number": return known("number");
      case "string": return known("text");
      case "identifier":
        return node.name === "true" || node.name === "false" ? known("boolean")
          : names.get(node.name) ?? (this.named(node.name) ? "any" : "unknown");
      case "list": return known("array", joined(node.items.map((item) => this.type(item, names))));
      case "unary":
        this.type(node.operand, names);
        return known(node.operator === "-" ? "number" : "boolean");
      case "binary": {
        const left = this.type(node.left, names), right = this.type(node.right, names);
        if (node.operator !== "+") return known(["-", "*", "/", "%", "^"].includes(node.operator) ? "number" : "boolean");
        const kinds = [left, right].map((type) => (typeof type === "object" ? type.type : type));
        return kinds.includes("text") ? known("text") : kinds.every((kind) => kind === "number") ? known("number")
          : kinds.includes("unknown") ? "unknown" : "any";
      }
      case "ternary":
        this.type(node.condition, names);
        return joined([this.type(node.whenTrue, names), this.type(node.whenFalse, names)]);
      case "call":
        return this.call(node, node.name, node.args, names);
      case "method":
        if (node.name === "prop") {
          this.type(node.receiver, names);
          return "any";
        }
        return this.call(node, node.name, [node.receiver, ...node.args], names, true);
      case "called":
        for (const part of [node.callee, ...node.args]) this.type(part, names);
        return "unknown";
    }
  }

  private call(node: FormulaNode, name: string, args: readonly FormulaNode[], names: ReadonlyMap<string, Type>, method = false): Type {
    if (name === "prop" && !method) return args[0]?.kind === "string" ? this.property(args[0].value) : "any";
    if ((name === "let" || name === "lets") && args.length % 2 === 1) {
      const bound = new Map(names);
      for (let index = 0; index + 1 < args.length; index += 2) {
        const value = this.type(args[index + 1], bound), named = args[index];
        if (named.kind === "identifier") bound.set(named.name, value);
      }
      return this.type(args[args.length - 1], bound);
    }
    const expected = (index: number) => EVERY[name] ?? (name in PARAMETERS ? PARAMETERS[name][index] ?? null : null);
    const types: Type[] = [];
    for (const [index, arg] of args.entries()) {
      const inner = this.calls.length;
      const lambda = index === 1 && LAMBDAS.has(name);
      const type = lambda ? this.type(arg, new Map(names).set("current", itemsOf(types[0])).set("index", known("number")))
        : this.type(arg, names);
      types.push(type);
      const wanted = lambda ? null : expected(index);
      if (type !== "unknown" || !wanted || node.start === undefined || node.end === undefined) continue;
      // A receiver is marked when read, after what is in it; an argument before what is in it.
      const target = method && index === 0;
      const call = { kind: target ? "target" as const : "argument" as const, name, type: wanted, start: node.start,
        end: target && node.kind === "method" && node.nameEnd !== undefined ? node.nameEnd : node.end };
      if (target) this.calls.push(call);
      else this.calls.splice(inner, 0, call);
    }
    return !method && !FORMULA_FUNCTIONS.has(name) ? "unknown" : this.result(name, types);
  }

  private result(name: string, types: readonly Type[]): Type {
    const [list] = types;
    if (name === "if") return joined(types.slice(1, 3));
    if (name === "ifs") return joined(types.filter((_, index) => index % 2 === 1 || index === types.length - 1));
    if (name === "first" || name === "last" || name === "at" || name === "find") return list === undefined ? "any" : itemsOf(list);
    if (KEEPS_ITEMS.has(name)) return known("array", list === undefined ? "any" : itemsOf(list));
    if (name === "map") return known("array", types[1] ?? "any");
    if (name === "flat") {
      const items = list === undefined ? "any" : itemsOf(list);
      return known("array", typeof items === "object" && items.type === "array" ? itemsOf(items) : items);
    }
    if (name === "concat") return known("array", joined(types.map(itemsOf)));
    if (name === "split" || name === "match") return known("array", known("text"));
    if (NUMBERS.has(name)) return known("number");
    if (TEXTS.has(name)) return known("text");
    if (BOOLEANS.has(name)) return known("boolean");
    if (DATES.has(name)) return known("date");
    return "any";
  }
}

// formulaTree is a formula read, or what of it reads where it can't be read
// whole (formulaAst.ts parseFormulaTokensRecovering): Notion warns of the
// calls in that too (seen 2026-11-15: "abs(foo) +" warns of abs() [0,8]).
function formulaTree(expression: string, recover = false): FormulaNode | null {
  if (!expression.trim()) return null;
  try {
    const tokens = tokenizeFormula(expression);
    return recover ? parseFormulaTokensRecovering(tokens, expression.length).node : parseFormulaTokens(tokens);
  } catch {
    return null;
  }
}

/**
 * The calls in a formula given a value whose type can't be known where they
 * take one type, in the order Notion names them; none when it can't be read.
 */
export function formulaUnknownTypeCalls(expression: string, properties: readonly DbProperty[],
  /** Names the formula may use where it runs (formulaWarnings' `known`). */
  scope: ReadonlySet<string> = new Set()): FormulaUnknownCall[] {
  const tree = formulaTree(expression, true);
  if (!tree) return [];
  // A name it knows as formulaWarnings does: one it names no problem of.
  const variables = formulaVariableNames(tokenizeFormula(expression));
  const named = (name: string) => FORMULA_LITERALS.has(name) || variables.has(name) || scope.has(name);
  const reading = new Set<string>();
  const property = (name: string): Type => {
    const found = resolveFormulaPropertyReference(properties, name);
    if (!found) return formulaPageField(properties, name) ? "any" : "unknown";
    if (found.type !== "formula") return propertyValueType(found.type);
    const formula = reading.has(found.id) ? null : formulaTree(String((found.config as { formula?: unknown } | undefined)?.formula ?? ""));
    if (!formula) return "any";
    reading.add(found.id);
    try {
      return new UnknownCalls(property, named).type(formula, new Map());
    } finally {
      reading.delete(found.id);
    }
  };
  const found = new UnknownCalls(property, named);
  found.type(tree, new Map());
  return found.calls;
}
