import { tokenizeFormula, type FormulaToken } from "./formulaSyntax";

function callArguments(tokens: FormulaToken[], openIndex: number): FormulaToken[][] {
  const args: FormulaToken[][] = [];
  let current: FormulaToken[] = [];
  let parenDepth = 0;
  let bracketDepth = 0;
  for (let index = openIndex + 1; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (token.type === "paren" && token.value === "(") {
      parenDepth += 1;
      current.push(token);
      continue;
    }
    if (token.type === "paren" && token.value === ")") {
      if (parenDepth === 0 && bracketDepth === 0) {
        if (current.length > 0 || args.length > 0) args.push(current);
        return args;
      }
      parenDepth -= 1;
      current.push(token);
      continue;
    }
    if (token.type === "bracket" && token.value === "[") bracketDepth += 1;
    if (token.type === "bracket" && token.value === "]") bracketDepth -= 1;
    if (token.type === "comma" && parenDepth === 0 && bracketDepth === 0) {
      args.push(current);
      current = [];
      continue;
    }
    current.push(token);
  }
  return args;
}

function variableNameFromArg(tokens: FormulaToken[]) {
  if (tokens.length !== 1) return "";
  const token = tokens[0];
  if (token.type !== "identifier" && token.type !== "string") return "";
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(token.value) ? token.value : "";
}

export function formulaVariableNames(tokens: FormulaToken[]) {
  const variables = new Set<string>();
  tokens.forEach((token, index) => {
    if (token.type !== "identifier" || (token.value !== "let" && token.value !== "lets")) return;
    if (tokens[index + 1]?.type !== "paren" || tokens[index + 1]?.value !== "(") return;
    const args = callArguments(tokens, index + 1);
    if (token.value === "let") {
      const variable = variableNameFromArg(args[0] ?? []);
      if (variable) variables.add(variable);
      return;
    }
    for (let argIndex = 0; argIndex + 2 < args.length; argIndex += 2) {
      const variable = variableNameFromArg(args[argIndex] ?? []);
      if (variable) variables.add(variable);
    }
  });
  return variables;
}

export function formulaPropertyReferences(input: string | FormulaToken[]) {
  const tokens = typeof input === "string" ? tokenizeFormula(input) : input;
  const refs = new Set<string>();
  tokens.forEach((token, index) => {
    if (token.type !== "identifier" || token.value !== "prop") return;
    if (tokens[index - 1]?.type === "dot") return;
    if (tokens[index + 1]?.type !== "paren" || tokens[index + 1]?.value !== "(") return;
    const property = tokens[index + 2];
    if (property?.type === "string" && property.value) refs.add(property.value);
  });
  return refs;
}

export type FormulaPropertyReferenceTarget = {
  id: string;
  name: string;
  config?: { formulaReferenceAliases?: unknown } | null;
};

function formulaReferenceAliases(property: FormulaPropertyReferenceTarget) {
  const aliases = property.config?.formulaReferenceAliases;
  if (!Array.isArray(aliases)) return [];
  return aliases.filter(
    (alias): alias is string => typeof alias === "string" && alias.length > 0,
  );
}

/**
 * A page's own created and edited fields: Notion's formulas name them as
 * prop("Created Time") and the rest even where no property holds them, and
 * its editor lists them after the database's properties (seen 2026-09-30).
 */
export const FORMULA_PAGE_FIELDS = [
  { name: "Created By", type: "created_by" },
  { name: "Created Time", type: "created_time" },
  { name: "Last Edited By", type: "last_edited_by" },
  { name: "Last Edited Time", type: "last_edited_time" },
] as const;

export type FormulaPageField = (typeof FORMULA_PAGE_FIELDS)[number];

/** The page field a prop() name names, when no property of the database does. */
export function formulaPageField(
  properties: readonly FormulaPropertyReferenceTarget[],
  reference: string,
): FormulaPageField | undefined {
  if (resolveFormulaPropertyReference(properties, reference)) return undefined;
  return FORMULA_PAGE_FIELDS.find((field) => field.name === reference);
}

/** A page field as the property the evaluator and the editor read it as. */
export function formulaPageFieldProperty(field: FormulaPageField, databaseId: string) {
  return {
    id: `page:${field.type}`, databaseId, name: field.name, type: field.type, config: {}, position: Number.MAX_SAFE_INTEGER,
  };
}

/**
 * Resolve one prop() token against the current schema plus the temporary alias
 * used while a bounded property-rename lifecycle rewrites dependent formulas.
 * Exact current names/ids win; an alias is accepted only when it identifies one
 * property, so malformed or colliding transitional state fails closed.
 */
export function resolveFormulaPropertyReference<T extends FormulaPropertyReferenceTarget>(
  properties: readonly T[],
  reference: string,
): T | undefined {
  const exact = properties.filter(
    (property) => property.id === reference || property.name === reference,
  );
  if (exact.length === 1) return exact[0];
  if (exact.length > 1) return undefined;
  const aliased = properties.filter((property) => (
    formulaReferenceAliases(property).includes(reference)
  ));
  return aliased.length === 1 ? aliased[0] : undefined;
}

function formulaStringLiteral(
  expression: string,
  start: number,
): { end: number; quote: string; value: string } | undefined {
  const quote = expression[start];
  if (quote !== "\"" && quote !== "'") return undefined;
  let value = "";
  let index = start + 1;
  while (index < expression.length) {
    const char = expression[index];
    if (char === quote) return { end: index + 1, quote, value };
    if (char === "\\" && index + 1 < expression.length) {
      value += expression[index + 1];
      index += 2;
      continue;
    }
    value += char;
    index += 1;
  }
  return undefined;
}

function escapedFormulaStringLiteral(value: string, quote: string) {
  return value
    .replaceAll("\\", "\\\\")
    .replaceAll(quote, `\\${quote}`);
}

/**
 * Rewrite only complete prop("name") string arguments while preserving every
 * other byte of the user expression, including quote style and whitespace.
 * `replace` returns the new reference, or undefined to keep one.
 */
export function rewriteFormulaPropertyReferences(
  expression: string,
  replace: (reference: string) => string | undefined,
): { expression: string; replacements: number } {
  let output = "";
  let cursor = 0;
  let index = 0;
  let replacements = 0;
  while (index < expression.length) {
    const char = expression[index];
    if (char === "\"" || char === "'") {
      const literal = formulaStringLiteral(expression, index);
      index = literal?.end ?? expression.length;
      continue;
    }
    if (!/[A-Za-z_]/.test(char)) {
      index += 1;
      continue;
    }
    const identifierStart = index;
    index += 1;
    while (index < expression.length && /[A-Za-z0-9_]/.test(expression[index])) {
      index += 1;
    }
    if (expression.slice(identifierStart, index) !== "prop") continue;
    let previous = identifierStart - 1;
    while (previous >= 0 && /\s/.test(expression[previous])) previous -= 1;
    if (expression[previous] === ".") continue;
    let open = index;
    while (open < expression.length && /\s/.test(expression[open])) open += 1;
    if (expression[open] !== "(") continue;
    let argument = open + 1;
    while (argument < expression.length && /\s/.test(expression[argument])) argument += 1;
    const literal = formulaStringLiteral(expression, argument);
    if (!literal) continue;
    let close = literal.end;
    while (close < expression.length && /\s/.test(expression[close])) close += 1;
    if (expression[close] !== ")") continue;
    index = close + 1;
    const next = replace(literal.value);
    if (next === undefined || next === literal.value) continue;
    output += expression.slice(cursor, argument + 1);
    output += escapedFormulaStringLiteral(next, literal.quote);
    cursor = literal.end - 1;
    replacements += 1;
  }
  if (replacements === 0) return { expression, replacements };
  output += expression.slice(cursor);
  return { expression: output, replacements };
}
