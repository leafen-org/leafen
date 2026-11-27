import type { DbProperty } from "@/lib/types";
import { parseFormulaTokens, type FormulaNode } from "./formulaAst";
import type { FormulaPiece } from "./formulaEditorModel";
import { resolveFormulaPropertyReference } from "./formulaReferences";
import { tokenizeFormula } from "./formulaSyntax";

// Notion's formula editor in debug mode warns of each function called on a
// value that may be empty, in orange under the result with the call's place
// ([0,20]; seen 2026-11-12, its type checker's CalledFunctionOnEmptyValue;
// formulaEditor:emptyValueCall here). A number's or a date's property may be empty,
// and so may a formula property whose formula may give nothing, a let's name
// for such a value, and an if(), ifs() or "? :" one of whose results may be;
// every other property has a value (text is "", a list []), and so has what a
// function or an operator gives. A call is warned of once, for its receiver
// or first value, else for any other — not if(), ifs(), empty(), not(), and(),
// or(), the list functions, nor add(), subtract(), multiply(), divide() and
// sum(), which take an empty value as 0; operators and let() are no calls.
// Where empty() or .empty() rules a property or name out — the other branch
// of if(empty(x), …), ifs() after it, "!empty(x) && …" — it has its value.

const UNCHECKED = new Set([
  "empty", "not", "and", "or", "ifs", "if", "let", "lets",
  "at", "first", "last", "slice", "reverse", "sort", "unique", "includes", "map", "filter", "find", "findIndex", "some",
  "every", "flat", "count", "length", "concat",
  "add", "subtract", "multiply", "divide", "sum",
]);

export type FormulaEmptyCall = { start: number; end: number };

type Known = ReadonlySet<string>;
type Names = ReadonlyMap<string, boolean>;
// What a condition tells of the values it rules out: those that have one when it holds, and when it doesn't.
type Ruled = { held: Known; failed: Known };
const NOTHING: Known = new Set();
const NO_RULE: Ruled = { held: NOTHING, failed: NOTHING };
const both = (left: Known, right: Known): Known => (right.size ? new Set([...left, ...right]) : left);
const shared = (left: Known, right: Known): Known => new Set([...left].filter((key) => right.has(key)));

class EmptyCalls {
  readonly calls: FormulaEmptyCall[] = [];

  constructor(private readonly property: (name: string) => { key: string; mayBeEmpty: boolean }) {}

  // Whether `node` may give nothing, noting the calls in it that are given what may be empty.
  mayBeEmpty(node: FormulaNode, known: Known, names: Names): boolean {
    switch (node.kind) {
      case "number": case "string":
        return false;
      case "list":
        for (const item of node.items) this.mayBeEmpty(item, known, names);
        return false;
      case "identifier":
        return names.get(node.name) === true && !known.has(`name:${node.name}`);
      case "unary":
        this.mayBeEmpty(node.operand, known, names);
        return false;
      case "binary": {
        this.mayBeEmpty(node.left, known, names);
        const ruled = this.ruled(node.left);
        const right = node.operator === "&&" ? both(known, ruled.held) : node.operator === "||" ? both(known, ruled.failed) : known;
        this.mayBeEmpty(node.right, right, names);
        return false;
      }
      case "ternary": {
        this.mayBeEmpty(node.condition, known, names);
        const ruled = this.ruled(node.condition);
        const whenTrue = this.mayBeEmpty(node.whenTrue, both(known, ruled.held), names);
        return this.mayBeEmpty(node.whenFalse, both(known, ruled.failed), names) || whenTrue;
      }
      case "call":
        return this.call(node, node.name, node.args, known, names);
      case "method":
        // A related page's property (current.prop("…")) is read where it is.
        if (node.name === "prop") {
          this.mayBeEmpty(node.receiver, known, names);
          return false;
        }
        return this.call(node, node.name, [node.receiver, ...node.args], known, names);
      case "called":
        for (const part of [node.callee, ...node.args]) this.mayBeEmpty(part, known, names);
        return false;
    }
  }

  private call(node: FormulaNode, name: string, args: readonly FormulaNode[], known: Known, names: Names): boolean {
    if (name === "prop" && args[0]?.kind === "string") {
      const { key, mayBeEmpty } = this.property(args[0].value);
      return mayBeEmpty && !known.has(key);
    }
    if (name === "if" && args.length === 3) return this.branches([args[0], args[1]], args[2], known, names);
    if (name === "ifs" && args.length % 2 === 1) {
      const pairs: [FormulaNode, FormulaNode][] = [];
      for (let index = 0; index + 1 < args.length; index += 2) pairs.push([args[index], args[index + 1]]);
      return this.branches(pairs.flat(), args[args.length - 1], known, names);
    }
    if ((name === "let" || name === "lets") && args.length % 2 === 1) {
      const bound = new Map(names);
      let inner = known;
      for (let index = 0; index + 1 < args.length; index += 2) {
        const named = args[index];
        const value = this.mayBeEmpty(args[index + 1], inner, bound);
        if (named.kind !== "identifier") continue;
        bound.set(named.name, value);
        inner = new Set([...inner].filter((key) => key !== `name:${named.name}`));
      }
      return this.mayBeEmpty(args[args.length - 1], inner, bound);
    }
    const empty = args.map((arg) => this.mayBeEmpty(arg, known, names));
    if (!UNCHECKED.has(name) && empty.some(Boolean) && node.start !== undefined && node.end !== undefined) {
      this.calls.push({ start: node.start, end: node.end });
    }
    return false;
  }

  // if()'s and ifs()'s results: each value where its condition holds and the ones before it don't, the last where none does.
  private branches(pairs: readonly FormulaNode[], otherwise: FormulaNode, known: Known, names: Names) {
    let empty = false, failed = known;
    for (let index = 0; index + 1 < pairs.length; index += 2) {
      this.mayBeEmpty(pairs[index], failed, names);
      const ruled = this.ruled(pairs[index]);
      empty = this.mayBeEmpty(pairs[index + 1], both(failed, ruled.held), names) || empty;
      failed = both(failed, ruled.failed);
    }
    return this.mayBeEmpty(otherwise, failed, names) || empty;
  }

  // What a condition rules out: empty(x) or x.empty() that x has a value where it doesn't hold, negated or joined by and/or.
  private ruled(node: FormulaNode): Ruled {
    if (node.kind === "unary" && node.operator === "!") return this.flipped(this.ruled(node.operand));
    if (node.kind === "call" && node.name === "not" && node.args.length === 1) return this.flipped(this.ruled(node.args[0]));
    const joined = node.kind === "binary" && (node.operator === "&&" || node.operator === "||") ? { operator: node.operator, parts: [node.left, node.right] }
      : node.kind === "call" && (node.name === "and" || node.name === "or") && node.args.length === 2 ? { operator: node.name === "and" ? "&&" : "||", parts: node.args }
        : null;
    if (joined) {
      const [left, right] = joined.parts.map((part) => this.ruled(part));
      return joined.operator === "&&" ? { held: both(left.held, right.held), failed: shared(left.failed, right.failed) }
        : { held: shared(left.held, right.held), failed: both(left.failed, right.failed) };
    }
    const value = node.kind === "call" && node.name === "empty" && node.args.length === 1 ? node.args[0]
      : node.kind === "method" && node.name === "empty" && node.args.length === 0 ? node.receiver : null;
    const key = value ? this.keyOf(value) : null;
    return key ? { held: NOTHING, failed: new Set([key]) } : NO_RULE;
  }

  private flipped(ruled: Ruled): Ruled {
    return { held: ruled.failed, failed: ruled.held };
  }

  private keyOf(node: FormulaNode): string | null {
    if (node.kind === "identifier") return `name:${node.name}`;
    if (node.kind === "call" && node.name === "prop" && node.args[0]?.kind === "string") return this.property(node.args[0].value).key;
    return null;
  }
}

function formulaTree(expression: string): FormulaNode | null {
  if (!expression.trim()) return null;
  try {
    return parseFormulaTokens(tokenizeFormula(expression));
  } catch {
    return null;
  }
}

/**
 * The calls in a formula given a value that may be empty, where each is
 * written in it, in the order Notion names them (a call's values before it);
 * none when it can't be read.
 */
export function formulaEmptyValueCalls(expression: string, properties: readonly DbProperty[]): FormulaEmptyCall[] {
  const tree = formulaTree(expression);
  if (!tree) return [];
  const reading = new Set<string>();
  const property = (name: string) => {
    const found = resolveFormulaPropertyReference(properties, name);
    if (!found) return { key: `prop:${name}`, mayBeEmpty: false };
    const key = `prop:${found.id}`;
    if (found.type === "number" || found.type === "date") return { key, mayBeEmpty: true };
    if (found.type !== "formula" || reading.has(found.id)) return { key, mayBeEmpty: false };
    const formula = formulaTree(String((found.config as { formula?: unknown } | undefined)?.formula ?? ""));
    reading.add(found.id);
    try {
      return { key, mayBeEmpty: formula ? new EmptyCalls(property).mayBeEmpty(formula, NOTHING, new Map()) : false };
    } finally {
      reading.delete(found.id);
    }
  };
  const found = new EmptyCalls(property);
  found.mayBeEmpty(tree, NOTHING, new Map());
  return found.calls;
}

/**
 * Where Notion ends a call's mark: after its ")", but before it when a
 * property's chip comes right before it ("format(수)" is [0,8], "max(1, 수 )"
 * [0,10], seen 2026-11-12).
 */
export function formulaEmptyCallEnd(expression: string, pieces: readonly FormulaPiece[], end: number) {
  if (expression[end - 1] !== ")") return end;
  let at = 0;
  for (const piece of pieces) {
    at += piece.text.length;
    if (at === end - 1) return piece.chip ? end - 1 : end;
    if (at > end - 1) return end;
  }
  return end;
}
