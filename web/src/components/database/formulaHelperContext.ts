import { parseFormulaTokensRecovering, type FormulaNode } from "./formulaAst";
import { formulaBlankInvalidCharacters, formulaNameCalled, tokenizeFormula, type FormulaToken } from "./formulaSyntax";
import type { FormulaValueKind } from "./formulaFunctionCatalog";
import { formulaPropertyValueType, formulaValueTypeOf, type FormulaValueType } from "./formulaTypes";
import { formulaScopeNameType, type FormulaScopeName } from "./formulaScopeNames";

// Where the caret stands, as the formula editor's helper tells it apart to
// choose its suggestions (formulaEditorModel.ts formulaSuggestions), as
// Notion's does (seen 2026-10-08, and the after-a-value kinds measured
// 2026-11-27 in Chrome, two tabs, below).

/**
 * Where an expression starts (nothing yet, or after "(", "," or "["), after
 * an operator, or after a value — right after it, after a space, or after its
 * "." — with the kinds of value the operators offered take (`operators`) and
 * the kinds the functions offered take (`of`); null where the value's type is
 * not known, which takes no operator and only the functions that take any
 * value. `spaced` offers the properties first and every function but let()
 * and lets(); `operatorsOnly` offers the operators alone; `unchained` names
 * the comparisons left out because one of their kind is still open there.
 */
export type FormulaHelperContext =
  | { kind: "none" }
  | { kind: "start" }
  | { kind: "operand" }
  | {
    kind: "value"; spaced: boolean; dotted: boolean; operatorsOnly?: true;
    of: readonly FormulaValueKind[] | null; operators: readonly FormulaValueKind[] | null;
    unchained: readonly string[];
  };

/**
 * The context of the text before the name being typed (`before`). A
 * character no token starts with is passed over as a space is ("1 + @" is
 * after an operator, as Notion's). Right after a value (no space) Notion
 * reads the deepest part of the formula ending at the caret (seen 2026-11-27
 * in Chrome at 1440, two tabs):
 *  - a call, a method's too: the functions that take what it gives, and the
 *    operators that take its last value — dateBetween(…, "days") offers "+"
 *    first and the number functions, length("ab") the text operators and the
 *    number functions, format(1) the number operators and the text
 *    functions; a call of no values ("ab".length(), now()) offers both by
 *    what it gives;
 *  - two values an operator joins, the last in parentheses (1 + (2),
 *    "a" + (1)): only the operators that take that last value;
 *  - nothing ending there — a value in parentheses at the start, after "-"
 *    or "not" ((1), -(1), not (true)) — as after a value and a space: the
 *    properties, the operators that take it, every function but let() and
 *    lets();
 *  - anything else (a value, a list, a call left open): by what it gives.
 * Where what comes before can't be read but for its end — a value or close
 * still to come — there is nothing ("none": "1 2", "1 2 + ", "abs(1 2",
 * "1 + * 2" are "결과 없음", seen 2026-11-27 in Chrome). After a value or a
 * space, a comparison still open there leaves its own kind out
 * (formulaOpenComparisons).
 */
export function formulaHelperContext(
  before: string, propertyType: (name: string) => string | undefined, names: readonly FormulaScopeName[] = [],
): FormulaHelperContext {
  const text = formulaBlankInvalidCharacters(before);
  const trimmed = text.trimEnd();
  const reading = formulaReading(trimmed);
  if (reading === null || reading.faults.some((fault) => !fault.ended)) return { kind: "none" };
  if (trimmed === "" || /[(,[]$/.test(trimmed)) return { kind: "start" };
  if (/[-+*/%^=!<>&|?:]$/.test(trimmed) || /\b(?:and|or|not)$/.test(trimmed)) return { kind: "operand" };
  const kinds = (source: string, inside = false) => formulaTrailingKinds(source, propertyType, names, inside);
  if (trimmed.endsWith(".")) {
    const of = kinds(trimmed.slice(0, -1));
    return { kind: "value", spaced: false, dotted: true, of, operators: of, unchained: [] };
  }
  const of = kinds(trimmed);
  const unchained = formulaOpenComparisons(reading.node, trimmed.length);
  if (trimmed.length < text.length) {
    const joined = formulaJoinedInParentheses(reading.node, trimmed.length);
    const operators = joined ? formulaOperandKinds(trimmed.slice(joined.start ?? 0, joined.end), propertyType, names) : kinds(trimmed, true);
    return { kind: "value", spaced: true, dotted: false, of, operators, unchained };
  }
  const node = formulaNodeEndingAt(reading.node, trimmed.length);
  if (!node) return { kind: "value", spaced: true, dotted: false, of, operators: kinds(trimmed, true), unchained };
  const slice = (part: FormulaNode) => kinds(trimmed.slice(part.start ?? 0, part.end ?? trimmed.length));
  if (node.kind === "binary") {
    return { kind: "value", spaced: false, dotted: false, operatorsOnly: true, of: null, operators: slice(node.right), unchained };
  }
  // A property read by prop("Name") is its value, not a call (Notion's chip).
  if ((node.kind === "call" || node.kind === "method") && node.name !== "prop" && node.args.length) {
    return { kind: "value", spaced: false, dotted: false, of, operators: slice(node.args[node.args.length - 1]), unchained };
  }
  return { kind: "value", spaced: false, dotted: false, of, operators: of, unchained };
}

// Comparisons don't chain, as Notion's reading takes them (formulaAst.ts):
// "1 > 2 > 3" and "true == false == true" stop at the second. So
// where the formula ends in the right side of a comparison, that comparison's
// kind is not offered after it — 1 > 2, 1 > -2, 1 > 2 + 3, 1 > (2), abs(1 > 2
// and true ? 1 : 2 > 3 offer == and != but not > >= < <=; true == false,
// "a" == "b" and 1 < 2 == true not == and != — while 1 + 2, true and false
// and (1 > 2) offer them all (seen 2026-11-28 in Chrome at 1440, after the
// value and after a space alike).
const COMPARISON_KINDS: readonly (readonly string[])[] = [[">", ">=", "<", "<="], ["==", "!="]];

function formulaOpenComparisons(root: FormulaNode, length: number): string[] {
  const open: string[] = [];
  for (let node: FormulaNode | undefined = root; node && node.missingAt === undefined && formulaNodeEnd(node) === length;) {
    if (node.kind === "binary") {
      const { operator } = node;
      open.push(...COMPARISON_KINDS.find((kind) => kind.includes(operator)) ?? []);
    }
    node = formulaNodeChildren(node).at(-1);
  }
  return open;
}

// After a space, a value in parentheses an operator joins at the end is read
// with the operation: the operators take what the operation gives, as
// Notion's (seen 2026-11-28 in Chrome, two tabs at different times): "1 +
// (2 > 3) ", "1 + (true) ", "(1) + (2 > 3) " and "now() + (1) " offer the text
// operators, "1 - (2 > 3) " and "1 + (2 + 3) " the number ones, "true and
// (2 > 3) " the true or false ones, "1 == (2 > 3) " and and or. One at the
// start, or after "-" or "not", is still read by the last value in it
// ("(1 > 2) ", "not (2 > 3) " offer the number operators), and right after
// the parenthesis it is that last value ("1 + (true)" the true or false ones).
function formulaJoinedInParentheses(root: FormulaNode, length: number): FormulaNode | null {
  for (let node: FormulaNode | undefined = root; node && node.missingAt === undefined && node.end === length;) {
    if (node.kind === "binary" && node.right.missingAt === undefined && formulaNodeEnd(node.right) !== length) return node;
    node = formulaNodeChildren(node).at(-1);
  }
  return null;
}

// The text read on past what it can't read, or null when it is too deep to read.
function formulaReading(text: string) {
  try {
    return parseFormulaTokensRecovering(tokenizeFormula(text), text.length);
  } catch {
    return null;
  }
}

// The deepest part of a formula (read on past what it can't read) that ends
// at `length`, or null. A minus or "not" ends where its value does, its
// closing parenthesis left out, as Notion places one; a value in parentheses
// keeps its own place without them (formulaAst.ts).
function formulaNodeEndingAt(root: FormulaNode, length: number): FormulaNode | null {
  const deepest = (node: FormulaNode): FormulaNode | null => {
    for (const child of formulaNodeChildren(node)) {
      const found = child.missingAt === undefined ? deepest(child) : null;
      if (found) return found;
    }
    return node.missingAt === undefined && formulaNodeEnd(node) === length ? node : null;
  };
  return deepest(root);
}

const formulaNodeEnd = (node: FormulaNode): number | undefined => node.kind === "unary" ? formulaNodeEnd(node.operand) : node.end;

function formulaNodeChildren(node: FormulaNode): FormulaNode[] {
  switch (node.kind) {
    case "list": return node.items;
    case "call": return node.args;
    case "method": return [node.receiver, ...node.args];
    case "called": return [node.callee, ...node.args];
    case "unary": return [node.operand];
    case "binary": return [node.left, node.right];
    case "ternary": return [node.condition, node.whenTrue, node.whenFalse];
    default: return [];
  }
}

const KIND_OF_TYPE: Partial<Record<FormulaValueType, FormulaValueKind>> = {
  number: "number", text: "text", boolean: "boolean", date: "date", array: "list", block: "page", person: "person",
};

// The kinds of the value the text ends with: the last operand, with the
// calls and methods written on it. `inside` reads a last operand in
// parentheses by the last operand in them, as Notion's operators after a
// space or after one at the start do ((1 > 2), ((1 > 2)), ("a" + 1) and
// (1 > 2) and a space offer the number operators, (format(1)) the text ones;
// seen 2026-11-28 in Chrome, two tabs) where its functions after a "." take
// the whole ((1 > 2). offers the true or false ones).
function formulaTrailingKinds(
  text: string, propertyType: (name: string) => string | undefined, names: readonly FormulaScopeName[] = [], inside = false,
): FormulaValueKind[] | null {
  let tokens: FormulaToken[];
  try {
    tokens = tokenizeFormula(text);
  } catch {
    return null;
  }
  let index = tokens.length - 1;
  // Where the parenthesis the text ends with opens.
  let lastOpen = -1;
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
      if (lastOpen < 0 && token === tokens[tokens.length - 1] && token.value === ")") lastOpen = index;
      index -= 1;
      if (formulaNameCalled(tokens, index)) index -= 1;
    } else if (token.type === "number" || token.type === "string" || token.type === "identifier") {
      index -= 1;
    } else {
      return null;
    }
    if (tokens[index]?.type !== "dot") break;
    index -= 1;
  }
  const operand = text.slice(tokens[index + 1]?.start ?? 0);
  if (inside && lastOpen >= 0 && index + 1 === lastOpen) {
    return formulaTrailingKinds(text.slice(tokens[lastOpen].end ?? 0, tokens[tokens.length - 1].start), propertyType, names, true);
  }
  return formulaOperandKinds(operand, propertyType, names);
}

// The kinds of a value written as `operand`.
function formulaOperandKinds(
  operand: string, propertyType: (name: string) => string | undefined, names: readonly FormulaScopeName[],
): FormulaValueKind[] | null {
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

/**
 * The function whose parentheses the caret is in, the innermost, or "": its
 * help stays beside an empty list, as Notion's (seen 2026-11-27 in Chrome:
 * lower(" shows lower()'s help, upper(lower("x lower()'s, dateBetween(1, "da
 * dateBetween()'s).
 */
export function formulaEnclosingCall(expression: string, cursor: number): string {
  const tokens = tokenizeFormula(expression.slice(0, cursor));
  const open: string[] = [];
  tokens.forEach((token, index) => {
    if (token.type === "paren" || token.type === "bracket") {
      if (token.value === "(" || token.value === "[") {
        open.push(token.value === "(" && formulaNameCalled(tokens, index - 1) ? tokens[index - 1].value : "");
      } else open.pop();
    }
  });
  return open.reverse().find(Boolean) ?? "";
}
