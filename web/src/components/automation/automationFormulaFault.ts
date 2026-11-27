import type { TFunction } from "i18next";
import type { AutomationAction, AutomationValueExpression, DbProperty } from "@/lib/types";
import type { FormulaScopeName } from "@/components/database/formulaEditorModel";
import { automationFormulaFault, type AutomationFormulaFault } from "./automationFormulaCheck";
import { automationFormulaNames, type AutomationVariableList } from "./automationFormulaNames";
import type { AutomationEditorSurface } from "./automationActionModel";

export { VALUE_FORMULA_BOUNDS, automationFormulaFault, type AutomationFormulaFault } from "./automationFormulaCheck";

/**
 * That problem in the editor's words, without where it is ("[1,2]"), as
 * Notion says it under a variable's or a page line's formula (seen
 * 2026-11-21); after a page name's "." a property may follow too.
 */
export function automationFormulaFaultWords(t: TFunction, fault: AutomationFormulaFault, expression: string, names: readonly FormulaScopeName[]) {
  const { problem } = fault;
  const afterPage = problem.key === "method" && names.some((name) => name.page
    && new RegExp(`(?:^|[^A-Za-z0-9_])${name.name}\\s*\\.\\s*$`, "u").test(expression));
  return t(`formulaEditor:errors.${afterPage ? "pageMethod" : problem.key}`, {
    token: problem.token, name: problem.name, left: problem.left, right: problem.right, type: problem.type,
    expected: problem.expected, given: problem.given, ...problem.values, postProcess: [],
  });
}

/** What a value's formula can't run for, in the editor's words without where; null for one that runs or isn't a formula. */
export function automationValueFormulaWords(
  t: TFunction, value: AutomationValueExpression | undefined, properties: readonly DbProperty[], names: readonly FormulaScopeName[],
) {
  if (value?.kind !== "formula") return null;
  const fault = automationFormulaFault(value.expression, properties, names);
  return fault ? automationFormulaFaultWords(t, fault, value.expression, names) : null;
}

// The names alone, for a check that says nothing in words.
const NAMES_ONLY = ((key: string) => key) as unknown as TFunction;

/**
 * The names a step's formulas run with, for checking them (automationFormulaNames without their words): the
 * variables of the steps before it typed from the trigger database's properties where they are given.
 */
export function automationCheckedNames(
  surface: AutomationEditorSurface | undefined, variables: AutomationVariableList, properties: readonly DbProperty[] = [],
) {
  return automationFormulaNames(NAMES_ONLY, { surface, variables, properties });
}

/**
 * The first formula of a step that can't run as written: a variable's, the
 * value "속성 편집" sets, a line of a page added or edited, or a message's —
 * each read against the trigger database's properties, as its editor reads it.
 */
export function automationActionFormulaFault(
  action: AutomationAction, properties: readonly DbProperty[], names: readonly FormulaScopeName[],
): AutomationFormulaFault | null {
  const formulas: string[] = [];
  if (action.type === "define_variables") {
    for (const variable of action.variables) if (variable.value.kind === "formula") formulas.push(variable.value.expression);
  } else if (action.type === "edit_property") {
    if (action.value.kind === "formula") formulas.push(action.value.expression);
  } else if (action.type === "add_page" || action.type === "edit_pages") {
    const lines = action.type === "add_page" ? action.properties ?? [] : action.changes;
    for (const line of lines) if (line.value.kind === "formula") formulas.push(line.value.expression);
    if (action.type === "add_page" && typeof action.title !== "string" && action.title.kind === "formula") formulas.push(action.title.expression);
  }
  if ("messageFormula" in action && action.messageFormula) formulas.push(action.messageFormula);
  for (const formula of formulas) {
    const fault = automationFormulaFault(formula, properties, names);
    if (fault) return fault;
  }
  return null;
}
