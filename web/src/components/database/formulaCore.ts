import { Parser, type FormulaEvaluationOptions } from "./formulaParser";
import { MAX_FORMULA_EXPRESSION_CHARACTERS, tokenizeFormula, type FormulaValue } from "./formulaSyntax";
import type { FormulaPropertyResult } from "./formulaValues";

export * from "./formulaSyntax";
export * from "./formulaValues";
export * from "./formulaReferences";
export * from "./formulaParser";

export function evaluateFormulaExpression(
  expression: string,
  resolveProp: (name: string) => FormulaValue | FormulaPropertyResult,
  options: FormulaEvaluationOptions = {},
): FormulaValue {
  return evaluateFormulaExpressionResult(expression, resolveProp, options).value;
}

export type FormulaEvaluationResult = {
  value: FormulaValue;
  error?: string;
  /** What kept it from working out, as it was thrown. */
  failure?: unknown;
};

export function evaluateFormulaExpressionResult(
  expression: string,
  resolveProp: (name: string) => FormulaValue | FormulaPropertyResult,
  options: FormulaEvaluationOptions = {},
): FormulaEvaluationResult {
  const trimmed = expression.trim();
  if (!trimmed) return { value: "" };
  if (trimmed.length > MAX_FORMULA_EXPRESSION_CHARACTERS) {
    return { value: "", error: "Formula expression is too long" };
  }
  try {
    return { value: new Parser(tokenizeFormula(trimmed), resolveProp, options).parse() };
  } catch (error) {
    return {
      value: "",
      error: error instanceof Error ? error.message : "Formula evaluation failed",
      failure: error,
    };
  }
}
