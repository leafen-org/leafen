import type { TFunction } from "i18next";
import type { AutomationValueExpression, DbProperty } from "@/lib/types";
import { formulaStaticResult, type FormulaScopeName } from "@/components/database/formulaEditorModel";
import type { FormulaShape, FormulaValueType } from "@/components/database/formulaTypes";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { TextPropertyIcon } from "@/icons/leafen/databaseIcons";
import { ListResultIcon, QuestionMarkIcon, WarningFillIcon } from "@/icons/leafen/automationIcons";

/**
 * What a "변수 정의" variable holds, as Notion names it at the head of its
 * line and the foot of its menu (seen 2026-11-21): words are text; a formula
 * is the type its editor reads it to give ("1" a number, "true" a checkbox,
 * "now()" a date, "[1,2]" a list of numbers, the run's person a person, the
 * triggering page a block); one whose results differ ("if(true, 1, "a")") or
 * that its editor names a problem in ("1.", "foo + 1") can't be told, and
 * one giving nothing ("empty()") is of no type — both marked "?". So can't
 * one whose words were cleared — erased, or by "변수 지우기" (seen
 * 2026-11-21: "?" and "유형: 알 수 없음"; a new one's empty words are text).
 */
export type AutomationVariableKind = { type: string; items?: string };

export function automationVariableKind(
  value: AutomationValueExpression, properties: readonly DbProperty[], names: readonly FormulaScopeName[], faulty: boolean,
): AutomationVariableKind {
  if (value.kind === "literal" && value.value === null) return { type: "unknown" };
  if (value.kind !== "formula") return { type: "text" };
  if (faulty) return { type: "unknown" };
  const result = formulaStaticResult(value.expression, properties, names);
  if (result.mixed) return { type: "unknown" };
  const items = result.items && result.items !== "unknown" && result.items !== "undefined" ? result.items : undefined;
  return result.type === "list" && items ? { type: "list", items } : { type: result.type };
}

/** That kind's name, "숫자", "숫자 목록" or "정의되지 않은 유형". */
function automationVariableKindName(t: TFunction, kind: AutomationVariableKind) {
  return kind.type === "list" && kind.items ? t(`formulaEditor:listTypes.${kind.items}`) : t(`formulaEditor:types.${kind.type}`);
}

/** That kind in words, "유형: 숫자", "유형: 숫자 목록" or "유형: 정의되지 않은 유형", as the variable's menu says it at its foot. */
export function automationVariableKindWords(t: TFunction, kind: AutomationVariableKind) {
  return t("databaseView:variableKindOf", { type: automationVariableKindName(t, kind) });
}

// A kind as a formula reads it, a list's items too.
const VALUE_TYPE: Record<string, FormulaValueType> = {
  text: "text", number: "number", checkbox: "boolean", date: "date", person: "person", page: "block", block: "block", list: "array", undefined: "undefined",
};
// The property kind whose mark it is listed by: a list's bullets, Σ for a kind that can't be told.
const LISTED_AS: Record<string, DbProperty["type"]> = {
  text: "rich_text", number: "number", checkbox: "checkbox", date: "date", person: "person", page: "relation", block: "relation", list: "multi_select",
};

/**
 * A variable as a later formula runs with it, as Notion's editor lists it
 * (seen 2026-11-21): marked by its kind — a number's "#", a date's, words'
 * lines, a list's bullets, Σ for one that can't be told — its chips a list's
 * "[ ]" and that one's "?"; "변수 유형은 숫자입니다." and its kind's examples,
 * none for one that can't be told; read as what it holds.
 */
export function automationVariableScopeName(t: TFunction, name: string, label: string, kind: AutomationVariableKind): FormulaScopeName {
  const known = kind.type in LISTED_AS;
  const item = kind.items ? VALUE_TYPE[kind.items] : undefined;
  const shape: FormulaShape | undefined = kind.type === "page" || kind.type === "block" ? undefined
    : kind.type === "list" ? { type: "array", ...(item ? { item: { type: item } } : {}) } : { type: VALUE_TYPE[kind.type] ?? "unknown" };
  const examples: Record<string, readonly string[]> = {
    text: [name, `upper(${name})`, `${name}.style("b", "red")`],
    number: [name, `${name} / 2`, `pi() * ${name} ^ 2`],
    date: [name, `${name} > now()`, `${name}.dateAdd(1, "weeks")`, `dateBetween(${name}, now(), "days")`],
    list: [name, `${name}.length()`, `${name}.at(0).format()`],
  };
  return {
    name, label, type: LISTED_AS[kind.type] ?? "formula", description: t("databaseView:messageNames.variableDetailOf", { type: automationVariableKindName(t, kind) }),
    examples: known ? examples[kind.type] : [], ...(kind.type === "page" || kind.type === "block" ? { page: true } : {}),
    ...(shape ? { shape } : {}), ...(kind.type === "list" ? { chip: "value_list" as const } : known ? {} : { chip: "value_unknown" as const }),
  };
}

/**
 * The mark at the head of a variable's line: its kind's, as Notion draws the
 * property kind of the same values (a number's "#", words' lines, a
 * checkbox, a date, a person's filled circle, a block's relation arrow), "[ ]"
 * for a list and "?" for a kind that can't be told or none — or, while its
 * formula has a problem, the filled warning at 20 (Notion's, seen 2026-11-21
 * in two tabs).
 */
export function AutomationVariableMark({ kind, warn }: { kind: AutomationVariableKind; warn: boolean }) {
  if (warn) return <WarningFillIcon size="regular" aria-hidden="true" />;
  switch (kind.type) {
    case "text": return <TextPropertyIcon size="small" aria-hidden="true" />;
    case "number": return <PropertyTypeIcon type="number" />;
    case "checkbox": return <PropertyTypeIcon type="checkbox" />;
    case "date": return <PropertyTypeIcon type="date" />;
    case "person": return <PropertyTypeIcon type="created_by" />;
    case "page":
    case "block": return <PropertyTypeIcon type="relation" />;
    case "list": return <ListResultIcon size="small" aria-hidden="true" />;
    default: return <QuestionMarkIcon size="small" aria-hidden="true" />;
  }
}
