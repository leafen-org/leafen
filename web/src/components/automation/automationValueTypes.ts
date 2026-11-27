import { useCallback } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { AutomationValueExpression, DbProperty } from "@/lib/types";
import { formulaStaticResult, type FormulaProblem, type FormulaScopeName } from "@/components/database/formulaEditorModel";

/**
 * The types a formula may give a property an automation sets, as Notion's
 * editor allows them (seen 2026-11-10): a date a date, a number a number, a
 * status a text or a list of texts — each beside "정의되지 않은 유형". Words
 * (a title, a text) take any; the types not seen take any here.
 */
const ALLOWED: Readonly<Record<string, readonly string[]>> = { date: ["date"], number: ["number"], status: ["text", "list:text"] };

/** A result as one kind: its type, or "list:text" for a list of texts. */
function resultKind(result: ReturnType<typeof formulaStaticResult>) {
  return result.type === "list" && result.items && result.items !== "unknown" ? `list:${result.items}` : result.type;
}

/**
 * A formula's result a property can't take: what it gives and what the
 * property allows, or null. An if() whose results differ gives "알 수 없음",
 * which none allows, as Notion's; one giving nothing but empty() gives
 * "정의되지 않은 유형", which each allows; a type this editor can't read lets it by.
 */
export function automationFormulaResultProblem(
  expression: string, propertyType: string, properties: readonly DbProperty[], names: readonly FormulaScopeName[] = [],
): { kind: string; allowed: readonly string[] } | null {
  const allowed = ALLOWED[propertyType];
  if (!allowed || !expression.trim()) return null;
  const result = formulaStaticResult(expression, properties, names);
  if (result.type === "unknown" && !result.mixed || result.type === "undefined") return null;
  const kind = resultKind(result);
  return allowed.includes(kind) ? null : { kind, allowed };
}

/** Whether a value set to a property is a formula of a type it can't take. */
export function automationValueTypeWrong(value: AutomationValueExpression, property: DbProperty | undefined, properties: readonly DbProperty[]) {
  return value.kind === "formula" && Boolean(property) && automationFormulaResultProblem(value.expression, property!.type, properties) !== null;
}

/**
 * That problem as the formula editor says it, over the whole formula:
 * "수식에는 숫자 반환 유형을 포함할 수 없습니다. 허용된 반환 유형은 날짜 및
 * 정의되지 않은 유형입니다. [0,14]" (Notion's, seen 2026-11-10).
 */
export function automationFormulaResultWords(
  t: TFunction, problem: { kind: string; allowed: readonly string[] }, length: number,
): FormulaProblem {
  const name = (kind: string) => kind.startsWith("list:") ? t(`formulaEditor:listTypes.${kind.slice(5)}`) : t(`formulaEditor:types.${kind}`);
  return { key: "resultType", type: name(problem.kind), name: problem.allowed.map(name).join(", "), start: 0, end: length };
}

/**
 * What a value's editor says of a formula its property can't take, every
 * editor an automation sets a value in (FormulaEditorDialog `resultProblem`):
 * "속성 편집"'s and a page line's alike (Notion's, seen 2026-11-10).
 */
export function useAutomationResultProblem(property: DbProperty | undefined, properties: readonly DbProperty[], names: readonly FormulaScopeName[]) {
  const { t } = useTranslation(["databaseView", "formulaEditor"]);
  return useCallback((formula: string) => {
    const problem = property ? automationFormulaResultProblem(formula, property.type, properties, names) : null;
    return problem ? automationFormulaResultWords(t, problem, formula.length) : null;
  }, [property, properties, names, t]);
}

/**
 * A page line's value its property can't take, as Notion says it under the
 * line (seen 2026-11-10): the editor's words without where.
 */
export function automationValueTypeWords(
  t: TFunction, value: AutomationValueExpression | undefined, property: DbProperty | undefined, properties: readonly DbProperty[],
) {
  if (value?.kind !== "formula" || !property) return null;
  const problem = automationFormulaResultProblem(value.expression, property.type, properties);
  if (!problem) return null;
  const words = automationFormulaResultWords(t, problem, 0);
  return t("formulaEditor:errors.resultType", { type: words.type, name: words.name });
}
