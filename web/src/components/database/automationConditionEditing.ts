import type { TFunction } from "i18next";
import { newId } from "@/lib/ids";
import { inTriggerConditionOrder, type AutomationTriggerDraft, type EventTriggerConditionDraft } from "./databaseAutomationPanelModel";
import type { TriggerConditionValue } from "./PhoneAutomationCondition";

/** A condition to add: a page added, or a property's edit (none for any), with what it leaves. */
export type ConditionKind = { type: "row_added" } | { type: "property_edited"; propertyId: string } & TriggerConditionValue;

export function triggerConditions(trigger: AutomationTriggerDraft) {
  return trigger.type === "events" ? trigger.conditions : [];
}

/** Whether a trigger already holds a condition: a page added, or that property's edit. */
export function hasTriggerCondition(trigger: AutomationTriggerDraft, condition: ConditionKind) {
  return triggerConditions(trigger).some((candidate) => candidate.type === condition.type
    && (condition.type !== "property_edited" || (candidate.type === "property_edited" && candidate.propertyId === condition.propertyId)));
}

/**
 * A condition added: a property's takes the place of any property's edit, as Notion's (seen 2026-10-24); a page added
 * goes first (inTriggerConditionOrder).
 */
export function withTriggerCondition(trigger: AutomationTriggerDraft, condition: ConditionKind): AutomationTriggerDraft {
  return trigger.type === "events"
    ? { ...trigger, conditions: inTriggerConditionOrder([...trigger.conditions.filter((candidate) => condition.type !== "property_edited"
      || candidate.type !== "property_edited" || candidate.propertyId), { id: newId(), ...condition } as EventTriggerConditionDraft]) }
    : { type: "events", mode: "all", conditions: [{ id: newId(), ...condition } as EventTriggerConditionDraft] };
}

export function withoutTriggerCondition(trigger: AutomationTriggerDraft, id: string): AutomationTriggerDraft {
  return trigger.type === "events"
    ? { ...trigger, conditions: trigger.conditions.filter((condition) => condition.id !== id) }
    : { type: "events", mode: "all", conditions: [] };
}

/** Any property's edit, in place of each property's (a page added stays). */
export function withAnyPropertyEdited(trigger: AutomationTriggerDraft): AutomationTriggerDraft {
  return trigger.type === "events" ? { ...trigger, conditions: [
    ...trigger.conditions.filter((condition) => condition.type === "row_added"),
    { id: newId(), type: "property_edited", propertyId: "" },
  ] } : trigger;
}

/** Whether any property but "any property" is edited in it: choosing any asks to replace them first. */
export function triggerNamesProperties(trigger: AutomationTriggerDraft) {
  return triggerConditions(trigger).some((condition) => condition.type === "property_edited" && condition.propertyId);
}

/** A property's condition with what its edit leaves, in place of the one it was or added. */
export function withConditionValue(trigger: AutomationTriggerDraft, id: string | undefined, propertyId: string,
  value: TriggerConditionValue): AutomationTriggerDraft {
  const condition = { type: "property_edited" as const, propertyId, ...(value.operator ? { operator: value.operator } : {}),
    ...(value.operator && value.value !== undefined ? { value: value.value } : {}) };
  if (!id) return withTriggerCondition(trigger, condition);
  return trigger.type === "events" ? { ...trigger, conditions: trigger.conditions.map((candidate) => (
    candidate.id === id ? { id, ...condition } : candidate)) } : trigger;
}

/**
 * Whether "다음 경우에 사용:" reads "〈모든|하나라도 ⌄〉 조건 발생 시": while a
 * condition names a property, whether or not it names what the edit leaves,
 * for the chip picks how the named properties' conditions join; a page added
 * and any property's edit name none, alone or together (Notion's, seen
 * 2026-10-25 with Stage set to Lead alone, 2026-11-10
 * with "날짜 속성 편집" alone, 2026-11-18 in Chrome at 1440 and on a Galaxy
 * A90 with "페이지 추가" and "속성 편집" keeping "다음 경우에 사용:" and
 * "페이지 추가" with Stage's showing the chip; its builder counts the named
 * properties).
 */
export function triggerSaysMode(trigger: AutomationTriggerDraft) {
  return triggerNamesProperties(trigger);
}

/**
 * The words before and after the mode chip: "〈chip〉 조건 발생 시", "When
 * 〈chip〉 triggers occur" (Notion's titleWithOperator); words without the
 * chip's place go after it.
 */
export function modeHeadWords(t: TFunction): [string, string] {
  const parts = t("databaseView:automationModeHead", { mode: "\u0001" }).split("\u0001");
  return parts.length > 1 ? [parts[0], parts.slice(1).join("")] : ["", parts[0]];
}
