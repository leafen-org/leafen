import type { TFunction } from "i18next";
import { formulaPieces, type FormulaScopeName } from "@/components/database/formulaEditorModel";
import { FORMULA_PAGE_FIELDS, formulaPageFieldProperty } from "@/components/database/formulaReferences";
import type { AutomationAction, AutomationValueExpression, DbProperty } from "@/lib/types";
import type { AutomationEditorSurface } from "./automationActionModel";
import { automationFormulaFault } from "./automationFormulaCheck";
import { automationVariableKind, automationVariableScopeName, type AutomationVariableKind } from "./automationVariableKind";

// The names an action's formula runs with, as Notion's automation formulas
// list them under "속성" (seen 2026-10-23): who ran it, the page's maker,
// when and on what day, the page that triggered it — whose properties follow
// its "." — and the variables earlier actions define, each of the type its
// value gives. The server binds the same (interactive_automation_recipients.go
// contextScope, interactive_automation_formula.go), each variable's value as
// it was worked out, its type kept.

/**
 * A variable's name in a formula: its own where it is of the letters a-z and
 * A-Z, digits and _, else one made from it, as the server binds it
 * (automationVariableFormulaName) and as saved formulas hold it, though a
 * formula reads a name of any letters. Its chip shows the variable's own name.
 */
function automationVariableFormulaName(name: string) {
  if (/^[A-Za-z_][A-Za-z0-9_]*$/u.test(name)) return name;
  return "var_" + Array.from(new TextEncoder().encode(name), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/** A variable an earlier action defines: its name, its value and the action's place. */
export type AutomationEarlierVariable = { name: string; value: AutomationValueExpression; step: number };

/** Those variables, or their names alone where their values aren't at hand (of a type that can't be told). */
export type AutomationVariableList = readonly (AutomationEarlierVariable | string)[];

/** The variables the actions before one define, in order. */
export function earlierAutomationVariables(actions: readonly AutomationAction[], index: number): AutomationEarlierVariable[] {
  const seen = new Set<string>();
  return actions.slice(0, index).flatMap((action, step) => action.type !== "define_variables" ? [] : action.variables.flatMap((variable) => {
    const name = variable.name.trim();
    if (!name || seen.has(name)) return [];
    seen.add(name);
    return [{ name, value: variable.value, step }];
  }));
}

/** What a memo of the names keeps to: each variable's name and value. */
export function automationVariablesKey(variables: AutomationVariableList) {
  return JSON.stringify(variables);
}

/**
 * Where a run has a page of its own: the row that triggered a database's
 * automation or a button on its rows, or the page a page button is on.
 */
function automationHasTriggerPage(surface: AutomationEditorSurface | undefined) {
  return surface === "event_automation" || surface === "database_button" || surface === "page_button";
}

/**
 * The names an action's formulas and mentions run with. A button's run is
 * whoever clicked it — "클릭한 모든 사람", "이 버튼을 클릭한 사용자" — and a
 * page button's page is "이 페이지", "이 버튼이 포함된 페이지", listed after
 * the run's day and before the page's maker, as Notion's (seen 2026-10-25).
 */
export function automationFormulaNames(
  t: TFunction,
  options: { surface: AutomationEditorSurface | undefined; variables: AutomationVariableList; properties?: readonly DbProperty[] },
): FormulaScopeName[] {
  const label = (key: string) => t(`databaseView:messageNames.${key}`);
  const button = options.surface === "page_button" || options.surface === "database_button";
  const actor: FormulaScopeName = { name: "executionActor", label: label(button ? "clickedBy" : "executionActor"), type: "person",
    description: label(button ? "clickedByDetail" : "executionActorDetail"),
    examples: ["executionActor", "executionActor.email()", `executionActor.name() == "Alice"`] };
  const creator: FormulaScopeName = { name: "pageCreator", label: label("pageCreator"), type: "person", description: label("pageCreatorDetail") };
  const times: FormulaScopeName[] = [
    { name: "executionTime", label: label("executionTime"), type: "date", description: label("executionTimeDetail") },
    { name: "executionDate", label: label("executionDate"), type: "date", description: label("executionDateDetail"),
      examples: ["executionDate", "executionDate > now()", `executionDate.dateAdd(1, "weeks")`, `dateBetween(executionDate, now(), "days")`] },
  ];
  const own = options.surface === "page_button";
  // A database's run page is a row of the database its properties are of; a page button's page has its own fields.
  const database = options.properties?.find((property) => property.databaseId)?.databaseId;
  const page: FormulaScopeName[] = automationHasTriggerPage(options.surface) ? [{ name: "triggerPage",
    label: label(own ? "thisPage" : "triggerPage"), type: "relation", page: true, description: label(own ? "thisPageDetail" : "triggerPageDetail"),
    examples: ["triggerPage", "triggerPage.format()", `triggerPage.prop("Created By")`],
    ...(own ? { fields: automationOwnPageFields(t) } : database ? { database } : {}) }] : [];
  const names: FormulaScopeName[] = own ? [actor, ...times, ...page, creator] : [actor, creator, ...times, ...page];
  // Each variable is of what its value gives, read with the names before its own step's (Notion's list in a variable's
  // formula leaves its step's others out, seen 2026-11-21), as its line's mark says it (automationVariableKind).
  const properties = options.properties ?? [];
  const steps: number[] = names.map(() => -1);
  for (const variable of options.variables) {
    if (typeof variable === "string") {
      names.push(automationVariableScopeName(t, automationVariableFormulaName(variable), variable, { type: "unknown" }));
      steps.push(Number.MAX_SAFE_INTEGER);
      continue;
    }
    const kind = earlierVariableKind(variable.value, properties, names.filter((_, at) => steps[at] < variable.step));
    names.push(automationVariableScopeName(t, automationVariableFormulaName(variable.name), variable.name, kind));
    steps.push(variable.step);
  }
  return names;
}

// The kinds of the formulas already read, by the properties they were read with: the editor asks again on every press.
const FORMULA_KINDS = new WeakMap<readonly DbProperty[], Map<string, AutomationVariableKind>>();

/** What a variable's value gives, read with the names before it, as its line's mark says (automationVariableKind). */
function earlierVariableKind(value: AutomationValueExpression, properties: readonly DbProperty[], before: readonly FormulaScopeName[]) {
  if (value.kind !== "formula") return automationVariableKind(value, properties, before, false);
  const known = FORMULA_KINDS.get(properties) ?? new Map<string, AutomationVariableKind>();
  if (known.size > 500) known.clear();
  FORMULA_KINDS.set(properties, known);
  const key = JSON.stringify([value.expression, before.map((name) => [name.name, name.shape, name.page])]);
  const kind = known.get(key) ?? automationVariableKind(value, properties, before, automationFormulaFault(value.expression, properties, before) !== null);
  known.set(key, kind);
  return kind;
}

/** A formula as its editor shows it: properties and the names it runs with by their own names, as its chips say them. */
export function automationFormulaShown(expression: string, names: readonly FormulaScopeName[], properties: readonly DbProperty[]) {
  return formulaPieces(expression, (name) => {
    const property = properties.find((candidate) => candidate.name === name);
    return property ? { name: property.name, type: property.type } : undefined;
  }, names).map((piece) => piece.chip ? piece.chip.name : piece.text).join("");
}

/**
 * A page button's page's own fields, as Notion's "‹ 이 페이지의 속성" lists
 * them (seen 2026-10-25): "제목" — "Page Name" in a formula — then its maker
 * and when, its last editor and when, the server reading the same names
 * (interactive_automation_button_page.go).
 */
function automationOwnPageFields(t: TFunction): NonNullable<FormulaScopeName["fields"]> {
  const field = (key: string) => t(`databaseView:thisPageFields.${key}`);
  const fields: NonNullable<FormulaScopeName["fields"]>[number][] = [{
    property: { id: "page:title", databaseId: "", name: "Page Name", type: "title", config: {}, position: 0 }, label: field("pageName") }];
  const keys: Record<(typeof FORMULA_PAGE_FIELDS)[number]["type"], string> = {
    created_by: "createdBy", created_time: "createdTime", last_edited_by: "lastEditedBy", last_edited_time: "lastEditedTime",
  };
  for (const pageField of FORMULA_PAGE_FIELDS) fields.push({ property: formulaPageFieldProperty(pageField, ""), label: field(keys[pageField.type]) });
  return fields;
}

