import { formulaAdd, formulaDivide, formulaMultiply, formulaPower, formulaRemainder, formulaSubtract } from "./formulaDecimal";
import { EMPTY_FORMULA_NODE, parseFormulaTokens, type FormulaNode } from "./formulaAst";
import { formulaSpans, richValue } from "./formulaRichText";
import { dateFunction, formulaTimeZone, parseFormulaDate } from "./formulaDates";
import { formulaFunction } from "./formulaFunctions";
import { sortedFormulaList } from "./formulaSort";
import { FormulaRunError, FormulaWorkError } from "./formulaRunError";
import { formulaLanguage, formulaLocale, type FormulaViewer } from "./formulaLocales";
import { MAX_FORMULA_STEPS, type FormulaReference, type FormulaToken, type FormulaValue } from "./formulaSyntax";
import { type FormulaPropertyResult, formulaPropertyValue, formulaValueBudget, isBlankFormulaValue, isFormulaReference, isFormulaText, listValue, requireList, sameFormulaValue, toBoolean, toNumber, toText, wellFormedValue } from "./formulaValues";

export interface FormulaEvaluationOptions {
  now?: () => Date;
  // The IANA zone dates are read in; the viewer's zone when left out.
  timeZone?: string;
  // The language dates are written in as text; the page's when left out.
  language?: string;
  // The row the formula belongs to, which id() without an argument returns.
  pageId?: string;
  // Reads a property of a related page, for .prop() on it; left out, related
  // pages read as empty.
  resolveRelatedProp?: (page: FormulaReference, name: string) => FormulaValue | FormulaPropertyResult;
}

type Scope = Map<string, FormulaValue>;

const LAZY_LIST_FUNCTIONS = new Set(["map", "filter", "find", "findIndex", "some", "every", "count", "sort"]);
const VARIABLE_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
const DATE_SHAPE = /^\d{4}-\d{2}-\d{2}/;

// Parser evaluates a formula's tokens. The server evaluates formulas the same
// way (database_formula_evaluator.go), and the two must agree.
export class Parser {
  private steps = 0;
  private readonly viewer: FormulaViewer;
  private clock: Date | undefined;

  constructor(
    private readonly tokens: FormulaToken[],
    private readonly resolveProp: (name: string) => FormulaValue | FormulaPropertyResult,
    private readonly options: FormulaEvaluationOptions = {},
  ) {
    this.viewer = { timeZone: formulaTimeZone(options.timeZone), locale: formulaLocale(options.language) };
  }

  parse(): FormulaValue {
    return formulaValueBudget(this.evaluate(parseFormulaTokens(this.tokens), new Map()));
  }

  private now() {
    this.clock ??= this.options.now?.() ?? new Date();
    return this.clock;
  }

  private evaluate(node: FormulaNode, scope: Scope): FormulaValue {
    this.steps += 1;
    if (this.steps > MAX_FORMULA_STEPS) throw new Error("Formula evaluation is too long");
    switch (node.kind) {
      case "number":
      case "string":
        return node.value;
      case "identifier":
        if (node.name === "true") return true;
        if (node.name === "false") return false;
        if (node.name === "null") return null;
        return scope.has(node.name) ? (scope.get(node.name) ?? null) : "";
      case "list":
        return listValue(this.each(node.items.map((item) => () => this.evaluate(item, scope))));
      case "unary": {
        const value = this.evaluate(node.operand, scope);
        if (node.operator === "!") return !toBoolean(value);
        // Of an empty property it fails the formula, as Notion's (seen 2026-11-14: format(-empty) is no value).
        if (value == null) throw new Error("- takes only numbers");
        return -toNumber(value);
      }
      case "ternary":
        return toBoolean(this.evaluate(node.condition, scope))
          ? this.evaluate(node.whenTrue, scope)
          : this.evaluate(node.whenFalse, scope);
      case "binary":
        return this.binary(node.operator, node.left, node.right, scope);
      case "call":
        return formulaValueBudget(this.call(node.name, node.args, undefined, false, scope, node));
      case "method": {
        let receiver: FormulaValue;
        try {
          receiver = this.evaluate(node.receiver, scope);
        } catch (error) {
          // A value after it that fails too is the one named (each).
          const lazy = node.name === "prop" || LAZY_LIST_FUNCTIONS.has(node.name) || ["if", "ifs", "and", "or", "let", "lets"].includes(node.name);
          if (!(error instanceof RangeError) && !lazy) this.each(node.args.map((arg) => () => this.evaluate(arg, scope)));
          throw error;
        }
        if (node.name === "prop") {
          // Only a page has properties: page.prop("Name") reads a related page.
          if (!isFormulaReference(receiver) || receiver.kind !== "page") throw new Error("prop() needs a page");
          const name = toText(node.args[0] ? this.evaluate(node.args[0], scope) : "");
          if (!receiver.databaseId || !this.options.resolveRelatedProp) return "";
          return formulaValueBudget(formulaPropertyValue(this.options.resolveRelatedProp(receiver, name)));
        }
        return formulaValueBudget(this.call(node.name, node.args, receiver, true, scope, node));
      }
      // Only the editor's reading of a formula that doesn't read has it (formulaAst.ts).
      case "called":
        throw new Error("A value can't be called");
    }
  }

  // each works out every value, and when some fail the last one's failure is
  // theirs, as Notion's editor names it (seen 2026-11-14: round(2, 0.5) +
  // round(3, 13), add() of them and a list of them name the second, a chain
  // of three its last); a bound passed stops at once.
  private each(steps: readonly (() => FormulaValue)[]): FormulaValue[] {
    let failed = false, last: unknown;
    const values = steps.map((step) => {
      try {
        return step();
      } catch (error) {
        if (error instanceof RangeError) throw error;
        failed = true;
        last = error;
        return null;
      }
    });
    if (failed) throw last;
    return values;
  }

  private binary(operator: string, leftNode: FormulaNode, rightNode: FormulaNode, scope: Scope): FormulaValue {
    if (operator === "&&" || operator === "||") {
      const left = this.evaluate(leftNode, scope);
      if (operator === "&&" && !toBoolean(left)) return false;
      if (operator === "||" && toBoolean(left)) return true;
      return toBoolean(this.evaluate(rightNode, scope));
    }
    const [left, right] = this.each([() => this.evaluate(leftNode, scope), () => this.evaluate(rightNode, scope)]);
    switch (operator) {
      case "==": return sameFormulaValue(left, right);
      case "!=": return !sameFormulaValue(left, right);
      case ">": return this.compare(left, right) === 1;
      case ">=": return [0, 1].includes(this.compare(left, right));
      case "<": return this.compare(left, right) === -1;
      case "<=": return [0, -1].includes(this.compare(left, right));
      case "+": return this.add(left, right);
      // Arithmetic as Notion's (formulaDecimal.ts).
      case "-": return formulaSubtract(toNumber(left), toNumber(right));
      case "*": return formulaMultiply(toNumber(left), toNumber(right));
      case "/": return left == null && right == null ? 0 : formulaDivide(toNumber(left), toNumber(right));
      case "%": return formulaRemainder(toNumber(left), toNumber(right));
      case "^": return left == null || right == null ? null : formulaPower(toNumber(left), toNumber(right));
    }
    throw new Error("Unknown formula operator");
  }

  // add joins text, a date as a mention of it, and otherwise adds numbers.
  private add(left: FormulaValue, right: FormulaValue): FormulaValue {
    const joins = (value: FormulaValue) => isFormulaText(value) || Array.isArray(value) || isFormulaReference(value);
    if (joins(left) || joins(right)) return richValue([...formulaSpans(left, this.viewer), ...formulaSpans(right, this.viewer)]);
    return formulaAdd(toNumber(left), toNumber(right));
  }

  // compare orders dates by time, text by character, and anything else by
  // number. NaN and an empty property are unordered (2), even with each other
  // (seen 2026-11-04: 5 >= empty, 0 <= empty and empty >= empty are all false);
  // empty text is text.
  private compare(left: FormulaValue, right: FormulaValue): number {
    if (isBlankFormulaValue(left) || isBlankFormulaValue(right)) return 2;
    if (isFormulaText(left) && isFormulaText(right)) {
      const [leftText, rightText] = [toText(left), toText(right)];
      if (DATE_SHAPE.test(leftText) && DATE_SHAPE.test(rightText)) {
        const leftDate = parseFormulaDate(leftText, this.viewer.timeZone);
        const rightDate = parseFormulaDate(rightText, this.viewer.timeZone);
        if (leftDate && rightDate) return order(leftDate.instant, rightDate.instant);
      }
      if (Number.isNaN(Number(leftText)) || Number.isNaN(Number(rightText))) {
        return leftText < rightText ? -1 : leftText > rightText ? 1 : 0;
      }
    }
    return order(toNumber(left), toNumber(right));
  }

  private call(name: string, args: FormulaNode[], receiver: FormulaValue | undefined, hasReceiver: boolean, scope: Scope, at: FormulaNode): FormulaValue {
    const count = args.length + (hasReceiver ? 1 : 0);
    const argument = (index: number): FormulaValue => {
      if (hasReceiver) {
        if (index === 0) return receiver ?? null;
        index -= 1;
      }
      return index < args.length ? this.evaluate(args[index], scope) : "";
    };
    if (!hasReceiver && (name === "let" || name === "lets")) return this.let(args, name === "lets", scope);
    if (name === "if") return toBoolean(argument(0)) ? argument(1) : argument(2);
    if (name === "ifs") {
      for (let index = 0; index + 1 < count; index += 2) {
        if (toBoolean(argument(index))) return argument(index + 1);
      }
      // With no condition met and nothing else to give, nothing (Notion's,
      // seen 2026-11-12: ifs(false, 1) null).
      return count % 2 === 1 ? argument(count - 1) : null;
    }
    if (name === "and" || name === "or") {
      for (let index = 0; index < count; index += 1) {
        if (toBoolean(argument(index)) === (name === "or")) return name === "or";
      }
      return name === "and";
    }
    // count() counts every item, or with a condition the items it holds for.
    if (name === "count" && !args[hasReceiver ? 0 : 1]) return requireList(argument(0), name).length;
    if (LAZY_LIST_FUNCTIONS.has(name)) {
      const expression = args[hasReceiver ? 0 : 1] ?? EMPTY_FORMULA_NODE;
      return this.eachItem(name, argument(0), expression, scope);
    }
    const values = this.each(Array.from({ length: count }, (_, index) => () => argument(index)));
    if (name === "prop") return formulaPropertyValue(this.resolveProp(toText(values[0] ?? "")));
    if (name === "id") {
      if (!values.length) return this.options.pageId ?? "";
      return isFormulaReference(values[0]) ? values[0].id : "";
    }
    let result: { value: FormulaValue } | undefined;
    try {
      result = dateFunction(name, values, this.now(), this.viewer) ?? formulaFunction(name, values, this.viewer);
    } catch (error) {
      throw placedRunError(error, name, at);
    }
    return result ? wellFormedValue(result.value) : "";
  }

  private let(args: FormulaNode[], multiple: boolean, scope: Scope): FormulaValue {
    if (args.length < 3 || (multiple && args.length % 2 === 0)) return "";
    const pairs = multiple ? (args.length - 1) / 2 : 1;
    let bound = scope;
    for (let index = 0; index < pairs; index += 1) {
      const nameNode = args[index * 2];
      const name = nameNode.kind === "identifier" ? nameNode.name : nameNode.kind === "string" ? nameNode.value : "";
      if (!VARIABLE_NAME.test(name)) return "";
      const value = this.evaluate(args[index * 2 + 1], bound);
      bound = new Map(bound).set(name, value);
    }
    return this.evaluate(multiple ? args[args.length - 1] : args[2], bound);
  }

  private eachItem(name: string, value: FormulaValue, expression: FormulaNode, scope: Scope): FormulaValue {
    const items = requireList(value, name);
    const results: FormulaValue[] = [];
    for (let index = 0; index < items.length; index += 1) {
      const item = items[index];
      // sort() without an expression orders the items themselves (formulaSort.ts).
      const result = name === "sort" && expression === EMPTY_FORMULA_NODE ? item
        : this.evaluate(expression, new Map(scope).set("current", item).set("index", index));
      const matched = toBoolean(result);
      if (name === "map" || name === "sort") results.push(result);
      else if ((name === "filter" || name === "count") && matched) results.push(item);
      else if (name === "find" && matched) return item;
      else if (name === "findIndex" && matched) return index;
      else if (name === "some" && matched) return true;
      else if (name === "every" && !matched) return false;
    }
    // No item found is nothing, as Notion's (seen 2026-11-12).
    if (name === "find") return null;
    if (name === "findIndex") return -1;
    if (name === "some") return false;
    if (name === "every") return true;
    if (name === "count") return results.length;
    if (name === "sort") return listValue(sortedFormulaList(items, results, this.viewer.timeZone, this.options.language ?? formulaLanguage()));
    return listValue(results);
  }
}

// placedRunError is a failure in a function's work as the editor names it:
// one of its own, or any other its work met, as the call's failure, placed
// on the call (formulaRunError.ts; seen 2026-11-14: 1 + round(2, 0.5) is [4,17]).
function placedRunError(error: unknown, name: string, at: FormulaNode) {
  const named = error instanceof FormulaWorkError ? new FormulaRunError("libraryCall", { name }) : error;
  if (!(named instanceof FormulaRunError) || named.start !== undefined) return named;
  named.start = at.start;
  named.end = at.end;
  return named;
}

function order(left: number, right: number) {
  if (Number.isNaN(left) || Number.isNaN(right)) return 2;
  return left < right ? -1 : left > right ? 1 : 0;
}
