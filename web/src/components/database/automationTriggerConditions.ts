import type { TFunction } from "i18next";
import { databaseAutomationTriggerConditionOperators } from "@/databaseAutomationWire.generated";
import type { DbProperty } from "@/lib/types";
import { statusOptionsInGroupOrder } from "./propertyStatusPolicy";
import { statusConditionItems } from "./automationStatusCondition";

/**
 * What a property trigger may say the edit left in the property, as Notion's
 * (seen 2026-10-24 at 1280px on the synthetic "Color cards"): a select's or
 * status's "다음과 동일함"/"다음과 동일하지 않음" some options, a number's =
 * ≠ > < ≥ ≤, words' "다음 값으로 설정됨"/"…되지 않음", "다음을 포함"/"…하지
 * 않음", "다음 값으로 시작함"/"끝남", people's and a multi-select's
 * "다음을 포함"/"…하지 않음", each also "편집됨" (any edit, stored as no
 * operator) and "초기화됨"; a checkbox checked or unchecked ("is" one of
 * them, both any edit); a date only edited; a relation's "다음을 포함"/"…하지
 * 않음" some of its database's pages (seen 2026-11-17 in Chrome at 1440 and on
 * a Galaxy A90 at 411). The server reads the same (database_automation_trigger_values.go).
 */
export type TriggerOperator = typeof databaseAutomationTriggerConditionOperators[number];
/** An operator as the editor offers it: "edited" is any edit, stored as none. */
export type TriggerOperatorChoice = TriggerOperator | "edited";
export type TriggerConditionKind = "options" | "list" | "pages" | "number" | "text" | "checkbox" | "none";

const TEXT_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);
const LIST_TYPES = new Set(["multi_select", "person"]);

export function triggerConditionKind(property: Pick<DbProperty, "type"> | undefined): TriggerConditionKind {
  if (!property) return "none";
  if (property.type === "select" || property.type === "status") return "options";
  if (LIST_TYPES.has(property.type)) return "list";
  if (property.type === "relation") return "pages";
  if (property.type === "number") return "number";
  if (TEXT_TYPES.has(property.type)) return "text";
  if (property.type === "checkbox") return "checkbox";
  return "none";
}

/** The operators a property's trigger offers, in Notion's order. */
export function triggerOperatorChoices(property: Pick<DbProperty, "type"> | undefined): TriggerOperatorChoice[] {
  switch (triggerConditionKind(property)) {
  case "options": return ["is", "is_not", "edited", "cleared"];
  case "list": case "pages": return ["contains", "does_not_contain", "edited", "cleared"];
  case "number": return ["is", "is_not", "greater_than", "less_than", "greater_or_equal", "less_or_equal", "edited", "cleared"];
  case "text": return ["is", "is_not", "contains", "does_not_contain", "starts_with", "ends_with", "edited", "cleared"];
  default: return [];
  }
}

/**
 * A property's new condition, as Notion starts it: an option list's all
 * options, a number's "=", words' "다음 값으로 설정됨" with an empty "값 입력"
 * (a title and plain words alike, seen 2026-11-17 in Chrome at 1440 in three
 * tabs and on a Galaxy A90 at 411; left empty it settles to any edit,
 * settledTriggerCondition), a status (all its options ticked) any edit.
 */
export function defaultTriggerCondition(property: Pick<DbProperty, "type"> | undefined): { operator?: TriggerOperator; value?: unknown } {
  switch (triggerConditionKind(property)) {
  case "options": return property?.type === "status" ? {} : { operator: "is" };
  case "list": case "pages": return { operator: "contains" };
  case "number": case "text": return { operator: "is" };
  default: return {};
  }
}

/**
 * A condition as leaving its window or screen keeps it ("완료", Escape, a
 * press outside or a card's "←"): a relation's pages, all taken back, leave
 * it "초기화됨" whichever of its two it was (the relation cleared), there
 * being no "모든 옵션" of pages; a number's comparison with no number "초기화됨"
 * (the number cleared), words' with no words any edit ("이름 속성 편집"),
 * whatever their operator (Notion's, seen 2026-11-17 in Chrome at 1440 in two
 * tabs: "=" and "다음 값으로 설정됨" left empty both by "완료" and a press
 * outside, ">" and "다음을 포함" by "완료").
 */
export function settledTriggerCondition(property: Pick<DbProperty, "type"> | undefined,
  condition: { operator?: TriggerOperator; value?: unknown }): { operator?: TriggerOperator; value?: unknown } {
  const kind = triggerConditionKind(property);
  const listed = condition.operator === "contains" || condition.operator === "does_not_contain";
  if (kind === "pages" && listed && !(triggerChosen(condition.value) ?? []).length) return { operator: "cleared" };
  const compares = (kind === "number" || kind === "text") && condition.operator && condition.operator !== "cleared";
  const empty = condition.value === undefined || condition.value === null || condition.value === "";
  if (compares && empty) return kind === "number" ? { operator: "cleared" } : {};
  return condition;
}

/** Whether a condition needs a value it lacks: a comparison of a number or words with nothing to compare. */
export function triggerConditionIncomplete(operator: TriggerOperator | undefined, value: unknown) {
  if (!operator || operator === "cleared") return false;
  // Every option unticked leaves nothing to look for.
  if (Array.isArray(value) && !value.length) return true;
  if (["greater_than", "less_than", "greater_or_equal", "less_or_equal", "starts_with", "ends_with"].includes(operator)) {
    return value === undefined || value === null || value === "";
  }
  return value === "";
}

const TRIGGER_OPERATOR_KINDS = ["options", "number", "text", "list"] as const;

/**
 * An operator's words for its property's kind. One kept from before the
 * property's type changed (a number's "<" on what is now a select) reads in
 * the words of the kind that has it, not as its key.
 */
export function triggerOperatorLabel(t: TFunction, property: Pick<DbProperty, "type"> | undefined, operator: TriggerOperatorChoice) {
  const kind = triggerConditionKind(property);
  const own = kind === "number" || kind === "text" || kind === "list" ? kind : kind === "pages" ? "list" : "options";
  return t([own, ...TRIGGER_OPERATOR_KINDS.filter((other) => other !== own)].map((group) => `databaseView:automationTriggerOperators.${group}.${operator}`));
}

type Option = { id: string; name: string; color?: string };

export function triggerPropertyOptions(property: DbProperty): Option[] {
  const options = property.type === "status" ? statusOptionsInGroupOrder(property.config) : property.config?.options ?? [];
  return options.filter((option) => typeof option?.id === "string").map(({ id, name, color }) => ({ id, name: name ?? "", color }));
}

/** A condition's chosen items: option, person or page IDs, or a checkbox's states; none for all of them. */
export function triggerChosen(value: unknown): Array<string | boolean> | null {
  return Array.isArray(value) ? value.filter((item): item is string | boolean => typeof item === "string" || typeof item === "boolean") : null;
}

/** "A, B 또는 C", as Notion joins a condition's options; pages "A 및 B" (`joint`). */
function triggerJoin(t: TFunction, names: readonly string[], joint = "databaseView:automationTriggerOr") {
  if (names.length < 2) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")}${t(joint)}${names.at(-1)}`;
}

/**
 * What a property trigger says, as Notion's card and list read it (seen
 * 2026-10-24), in the sentences of databaseView's automationTrigger* keys:
 * Stage set to Lead, Won or Lost, Tags holding Red or Blue, "Amount > 15",
 * "Note 다음을 포함 urgent", Owner cleared, "Done 속성 편집". A relation's
 * pages (seen 2026-11-17 in Chrome at 1440 and on a Galaxy A90 at 411): one
 * held or not held, more joined with "및" as one of them held or none of
 * them held (`many`); people picked the same (seen 2026-11-18 in Chrome at 1440). The
 * values come as the caller names them (pills on a card, words in the list).
 */
export type TriggerSentence =
  | { form: "edited"; name: string }
  | { form: "cleared"; name: string }
  /**
   * A checkbox's carries its states (`states`, in its values' order), for its pills' marks and its own words; a
   * status's says so (`status`) for its own English words.
   */
  | { form: "set"; name: string; values: string[]; states?: boolean[]; status?: true }
  | { form: "holds"; name: string; values: string[]; many?: boolean }
  | { form: "lacks"; name: string; values: string[]; many?: boolean }
  | { form: "compare"; name: string; operator: string; values: string[] };

export function triggerSentence(t: TFunction, property: DbProperty, operator: TriggerOperator | undefined, value: unknown,
  nameOf: (id: string) => string): TriggerSentence {
  const kind = triggerConditionKind(property);
  const name = property.name;
  if (!operator) return { form: "edited", name };
  // A checkbox's state in the words of Notion's automation trigger, not its window's rows ("선택됨"·"선택 취소됨",
  // seen 2026-11-18 in Chrome at 1440 on the card and in the list).
  if (kind === "checkbox") {
    const states = (triggerChosen(value) ?? []).map((state) => state === true);
    return { form: "set", name, states,
      values: states.map((state) => t(state ? "databaseView:automationSummary.checkedValue" : "databaseView:automationSummary.unchecked")) };
  }
  if (operator === "cleared") return { form: "cleared", name };
  if (property.type === "status") {
    // A status's whole groups by their names, the other options by theirs (automationStatusCondition.ts); all of them any edit.
    const chosen = triggerChosen(value);
    if (!chosen && operator === "is") return { form: "edited", name };
    const names = statusConditionItems(property, chosen ?? []).map((item) => item.name);
    return operator === "is" ? { form: "set", name, values: names, status: true }
      : { form: "compare", name, operator: triggerOperatorLabel(t, property, operator), values: names };
  }
  if (kind === "pages" && (operator === "contains" || operator === "does_not_contain")) {
    const names = (triggerChosen(value) ?? []).map((id) => nameOf(String(id)));
    // None chosen holds any page (stored so only outside the editor, which settles it "초기화됨").
    if (!names.length) return operator === "contains" ? { form: "holds", name, values: [t("databaseView:automationTriggerAllOptions")] } : { form: "cleared", name };
    return { form: operator === "contains" ? "holds" : "lacks", name, values: names, many: names.length > 1 };
  }
  // People picked read as a relation's pages do; none picked, as an option list's.
  const people = property.type === "person" && (operator === "contains" || operator === "does_not_contain")
    ? (triggerChosen(value) ?? []).map((id) => nameOf(String(id))) : [];
  if (people.length) return { form: operator === "contains" ? "holds" : "lacks", name, values: people, many: people.length > 1 };
  if (kind === "options" || kind === "list") {
    const chosen = triggerChosen(value);
    // None chosen is all of them: a select's or multi-select's every option, people or pages "모든 옵션".
    const names = chosen ? chosen.map((id) => nameOf(String(id)))
      : kind === "options" || property.type === "multi_select" ? triggerPropertyOptions(property).map((option) => option.name) : [];
    // Set to nothing reads as cleared, as Notion's does for a select with no option (seen 2026-11-15).
    if (operator === "is") return names.length ? { form: "set", name, values: names } : { form: "cleared", name };
    if (operator === "contains") return { form: "holds", name, values: names.length ? names : [t("databaseView:automationTriggerAllOptions")] };
    return { form: "compare", name, operator: triggerOperatorLabel(t, property, operator), values: names };
  }
  return { form: "compare", name, operator: triggerOperatorLabel(t, property, operator), values: value === undefined || value === null ? [] : [String(value)] };
}

/** The sentence as words, for an automation's list. */
export function triggerSentenceText(t: TFunction, sentence: TriggerSentence) {
  switch (sentence.form) {
  case "edited": return t("databaseView:automationSummary.propertyEdited", { name: sentence.name });
  case "cleared": return t("databaseView:automationTriggerCleared", { name: sentence.name });
  case "set": return t(triggerSetKey(sentence), { name: sentence.name, values: triggerJoin(t, sentence.values) });
  case "holds": case "lacks": return t(triggerHoldsKey(sentence), { name: sentence.name,
    values: triggerJoin(t, sentence.values, sentence.many !== undefined ? "databaseView:automationTriggerAnd" : undefined) });
  case "compare": return [sentence.name, sentence.operator, ...sentence.values].filter(Boolean).join(" ");
  }
}

/** The words of a condition holding values or not: one page or option's, or pages' "값 중 하나가"·"중 어느 값도". */
export function triggerHoldsKey(sentence: Extract<TriggerSentence, { form: "holds" | "lacks" }>) {
  if (sentence.form === "lacks") return sentence.many ? "databaseView:automationTriggerLacksAll" : "databaseView:automationTriggerLacks";
  return sentence.many ? "databaseView:automationTriggerHoldsOneOf" : "databaseView:automationTriggerHolds";
}

/**
 * The words of a condition setting values, each Notion's own: a checkbox's ("Done set to checked", its
 * checkboxPropertySet), a status's ("Stage set to Done", statusPropertySet) and a select's ("Stage is set to Won",
 * selectPropertyIs) — one Korean sentence for all three.
 */
export function triggerSetKey(sentence: Extract<TriggerSentence, { form: "set" }>) {
  return sentence.states ? "databaseView:automationTriggerCheckbox.set"
    : sentence.status ? "databaseView:automationTriggerStatusSet" : "databaseView:automationTriggerSet";
}
