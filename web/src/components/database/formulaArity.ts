import type { FormulaNode } from "./formulaAst";
import { FORMULA_FUNCTIONS } from "./formulaSyntax";

// How many values each function takes, as Notion's editor checks a call
// (seen 2026-11-03 for each function it has, calling it with none and with
// seven): the fewest and the most, Infinity where any more may follow.
// "call" is a function Notion reads with none as a failed call instead
// (formulaEditor:errors.callFailed here), taking any number after. A method's
// value counts as its first ("a".upper() is upper("a")). startsWith,
// endsWith, nextWorkday and addWorkdays are Leafen's own, as their help
// names them.
const ANY = Infinity;
const FORMULA_ARITY: Readonly<Record<string, readonly [number | "call", number]>> = {
  if: [3, 3], ifs: [2, ANY], and: [0, ANY], or: [0, ANY], not: ["call", ANY], empty: [0, 1], length: [1, 1],
  format: [1, 1], equal: [2, 2], unequal: [2, 2], let: ["call", ANY], lets: ["call", ANY],
  substring: [2, 3], contains: [2, 2], test: [2, 2], match: [2, 2], replace: [2, 3], replaceAll: [2, 3],
  lower: [1, 1], upper: [1, 1], trim: [1, 1], repeat: [2, 2], padStart: [3, 3], padEnd: [3, 3], link: [2, 2],
  style: [1, ANY], unstyle: [1, ANY], concat: [0, ANY], join: [2, 2], split: [1, 2], startsWith: [2, 2],
  endsWith: [2, 2], formatNumber: [1, 3],
  add: [2, 2], subtract: [2, 2], multiply: [2, 2], mod: [2, 2], pow: [2, 2], divide: [2, 2], min: [0, ANY],
  max: [0, ANY], sum: [0, ANY], median: [0, ANY], mean: [0, ANY], abs: [1, 1], round: ["call", ANY], ceil: [1, 1],
  floor: [1, 1], sqrt: [1, 1], cbrt: [1, 1], exp: [1, 1], ln: [1, 1], log10: [1, 1], log2: [1, 1], sign: [1, 1],
  pi: [0, 0], e: [0, 0], toNumber: [1, 1],
  now: [0, 0], today: [0, 0], minute: [1, 1], hour: [1, 1], day: [1, 1], date: [1, 1], week: [1, 1], month: [1, 1],
  year: [1, 1], dateAdd: [3, 3], dateSubtract: [3, 3], dateBetween: [3, 3], dateRange: [2, 2], dateStart: [1, 1],
  dateEnd: [1, 1], timestamp: [1, 1], fromTimestamp: [1, 1], formatDate: [2, 3], parseDate: [1, 1],
  nextWorkday: [1, 2], addWorkdays: [2, 3], name: [1, 1], email: [1, 1],
  at: [2, 2], first: [1, 1], last: [1, 1], slice: [2, 3], splice: [2, ANY], sort: [1, 2], reverse: [1, 1],
  unique: [1, 1], includes: [2, 2], find: [2, 2], findIndex: [2, 2], filter: [2, 2], some: [2, 2], every: [2, 2],
  map: [2, 2], flat: [1, 1], count: [1, 2], id: [0, 1],
};

export type FormulaCallProblem =
  | { code: "unknownMethod"; name: string; start?: number; end?: number }
  | { code: "argumentsMissing"; name: string; expected: number; given: number; start?: number; end?: number }
  // A missing value's place is Notion's [0,1]; `at` is where it is missing, which orders it among the problems.
  | { code: "argumentExtra"; name: string; start?: number; end?: number; at?: number }
  | { code: "callFailed"; name: string; start?: number; end?: number };

/**
 * A formula's calls Notion's editor refuses, in the order they're written:
 * a method it has no function for, from its value to its name; a call with
 * too few values, the whole call; each value past the most a function
 * takes; and a "call" function called with none.
 */
export function formulaCallProblems(node: FormulaNode): FormulaCallProblem[] {
  const problems: FormulaCallProblem[] = [];
  const visit = (current: FormulaNode) => {
    switch (current.kind) {
      case "call":
      case "method": {
        const method = current.kind === "method";
        if (method) visit(current.receiver);
        if (method && !FORMULA_FUNCTIONS.has(current.name)) {
          problems.push({ code: "unknownMethod", name: current.name, start: current.start, end: current.nameEnd });
        } else {
          checkArity(current.name, current.args, method ? 1 : 0, current, problems);
        }
        current.args.forEach(visit);
        return;
      }
      case "list": current.items.forEach(visit); return;
      case "unary": visit(current.operand); return;
      case "binary": visit(current.left); visit(current.right); return;
      case "ternary": visit(current.condition); visit(current.whenTrue); visit(current.whenFalse); return;
      case "called": visit(current.callee); current.args.forEach(visit); return;
      default: return;
    }
  };
  visit(node);
  return problems;
}

function checkArity(name: string, args: readonly FormulaNode[], before: number, call: FormulaNode, problems: FormulaCallProblem[]) {
  const arity = FORMULA_ARITY[name];
  if (!arity) return;
  const [fewest, most] = arity;
  const given = args.length + before;
  if (fewest === "call") {
    if (given === 0) problems.push({ code: "callFailed", name, start: call.start, end: call.end });
    return;
  }
  if (given < fewest) {
    problems.push({ code: "argumentsMissing", name, expected: fewest, given, start: call.start, end: call.end });
    return;
  }
  args.slice(Math.max(0, most - before)).forEach((arg) => problems.push({ code: "argumentExtra", name, start: arg.start, end: arg.end,
    ...(arg.missingAt === undefined ? {} : { at: arg.missingAt }) }));
}
