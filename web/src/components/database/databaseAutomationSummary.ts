import type { TFunction } from "i18next";
import { i18next } from "@/i18n";
import type {
  AutomationAction,
  AutomationValueExpression,
  DatabaseAutomationDefinition,
  DbProperty,
  DbView,
  Page,
} from "@/lib/types";
import { pageNamesNow } from "@/lib/pageNames";
import { personLabel } from "@/lib/peopleDirectory";
import type { InteractiveAutomationRecipientSource } from "@/interactiveAutomationWire.generated";
// The panel's model first: it reads the action model's bounds as it loads.
import {
  databaseAutomationEditorBounds,
  isDatabaseAutomationEditableProperty,
  triggerDraftFromAutomation,
  type AutomationTriggerDraft,
  type EventTriggerConditionDraft,
} from "./databaseAutomationPanelModel";
import { triggerPropertyOptions, triggerSentence, triggerSentenceText } from "./automationTriggerConditions";
import { automationDocumentActions, labelKey } from "@/components/automation/automationActionModel";

/** What an automation's trigger and actions name in its own database. */
export type DatabaseAutomationReferences = {
  databaseId: string;
  properties: readonly DbProperty[];
  views: readonly DbView[];
};

// An automation naming a property its database no longer has is invalid
// before anything runs, as Notion's list marks it the moment the property
// goes (seen 2026-10-15): a trigger on it, an action editing it, a value or
// recipient read from it, a webhook sending it. Another database's
// properties are that database's; a page added or edited there stays as it is.
function property(references: DatabaseAutomationReferences, propertyId: string) {
  return references.properties.find((candidate) => candidate.id === propertyId);
}

function readsGoneProperty(value: unknown, references: DatabaseAutomationReferences, depth = 0): boolean {
  if (!value || typeof value !== "object" || depth > 20) return false;
  if (Array.isArray(value)) return value.some((item) => readsGoneProperty(item, references, depth + 1));
  const record = value as Record<string, unknown>;
  if (record.kind === "trigger_property" && typeof record.propertyId === "string"
    && !property(references, record.propertyId)) return true;
  return Object.values(record).some((item) => readsGoneProperty(item, references, depth + 1));
}

function automationTriggerConditionBroken(
  condition: EventTriggerConditionDraft,
  references: DatabaseAutomationReferences,
) {
  return condition.type === "property_edited" && condition.propertyId !== "" && !property(references, condition.propertyId);
}

function automationActionBroken(action: AutomationAction, references: DatabaseAutomationReferences) {
  if (readsGoneProperty(action, references)) return true;
  const gone = (propertyId: string) => !property(references, propertyId);
  if (action.type === "edit_property") {
    const edited = property(references, action.propertyId);
    return !edited || !isDatabaseAutomationEditableProperty(edited);
  }
  if (action.type === "add_page") {
    return action.databaseId === references.databaseId && (action.properties ?? []).some((change) => gone(change.propertyId));
  }
  if (action.type === "edit_pages") {
    return action.target.databaseId === references.databaseId && action.changes.some((change) => gone(change.propertyId));
  }
  if (action.type === "send_notification") {
    return (action.recipientSources ?? []).some((source) => source.kind === "person_property" && gone(source.propertyId));
  }
  if (action.type === "send_webhook") return action.propertyIds.some(gone);
  return false;
}

/** Whether a draft or a saved automation names what its database no longer has. */
export function databaseAutomationDraftBroken(
  trigger: AutomationTriggerDraft,
  actions: readonly AutomationAction[],
  viewId: string | null,
  references: DatabaseAutomationReferences,
) {
  return (viewId !== null && !references.views.some((view) => view.id === viewId))
    || (trigger.type === "events" && trigger.conditions.some((condition) => automationTriggerConditionBroken(condition, references)))
    || actions.some((action) => automationActionBroken(action, references));
}

function storedActions(automation: DatabaseAutomationDefinition) {
  try {
    return automationDocumentActions(automation.actionDocument, databaseAutomationEditorBounds);
  } catch {
    return [];
  }
}

export function databaseAutomationBroken(
  automation: DatabaseAutomationDefinition,
  references: DatabaseAutomationReferences,
) {
  return databaseAutomationDraftBroken(
    triggerDraftFromAutomation(automation),
    storedActions(automation),
    automation.scopeType === "view" ? automation.viewId ?? "" : null,
    references,
  );
}

type Summary = { t: TFunction; references: DatabaseAutomationReferences; personName: (id: string) => string | null; event: boolean;
  /** A related database's pages, naming a relation condition's (AutomationRelationRowsContext). */
  relationRows?: (dataSourceId: string) => Page[] };

function optionName(target: DbProperty, id: unknown) {
  return typeof id === "string" ? target.config?.options?.find((option) => option.id === id)?.name ?? null : null;
}

// A literal value as the list's sentence reads it ("Count 속성을 5 값으로
// 설정"); one it can't put in words leaves the sentence at "편집".
function literalText(target: DbProperty, value: unknown, summary: Summary): string | null {
  const { t } = summary;
  if (value === null || value === undefined || value === "") return null;
  if (target.type === "checkbox") return t(value === true ? "databaseView:automationSummary.checkedValue" : "databaseView:automationSummary.unchecked");
  if (target.type === "select" || target.type === "status") return optionName(target, value);
  if (target.type === "person") return peopleText(value, summary);
  // Options in the language's list, as the card says them ("빨강 및 파랑", Notion's, seen 2026-11-11).
  if (target.type === "multi_select") {
    if (!Array.isArray(value) || !value.length) return null;
    const names = value.map((id) => optionName(target, id));
    return names.every((name): name is string => name !== null)
      ? new Intl.ListFormat(i18next.language, { type: "conjunction" }).format(names) : null;
  }
  if (typeof value === "number") return String(value);
  if (typeof value === "string") return target.type === "date" ? value.slice(0, 10) : value;
  return null;
}

// People as the card lists them (AutomationPersonValue): each by name, then
// who runs it and the page's creator, in the language's list.
function peopleText(value: unknown, summary: Summary): string | null {
  if (!Array.isArray(value) || !value.length) return null;
  const names = value.filter((item): item is string => typeof item === "string").map((id) => summary.personName(id));
  // Who runs it and the page's creator by what they are, as the card's tip says them (AutomationPersonValue).
  for (const source of ["execution_actor", "page_creator"] as const) {
    if (value.some((item) => item && typeof item === "object" && (item as { source?: string }).source === source)) {
      names.push(source === "page_creator" || summary.event
        ? summary.t(`databaseView:personSourceTips.${source}`) : recipientName({ kind: source }, summary));
    }
  }
  if (!names.length || !names.every((name): name is string => name !== null)) return null;
  return new Intl.ListFormat(i18next.language, { type: "conjunction" }).format(names);
}

function valueText(target: DbProperty, value: AutomationValueExpression, summary: Summary) {
  if (value.kind === "execution_time") return summary.t("databaseView:messageNames.executionTime");
  if (value.kind === "execution_date") return summary.t("databaseView:messageNames.executionDate");
  return value.kind === "literal" ? literalText(target, value.value, summary) : null;
}

// Notion's list (seen 2026-11-02): a value left empty clears the property
// ("Name 지우기"); a choice, people or pages replace what it held
// ("Pick 속성을 One 값으로 바꾸기"); anything else is set.
const REPLACED = new Set(["select", "status", "multi_select", "person", "relation"]);
function clears(value: AutomationValueExpression) {
  return value.kind === "literal" && (value.value === null || value.value === undefined || value.value === ""
    || (Array.isArray(value.value) && value.value.length === 0));
}

function recipientName(source: InteractiveAutomationRecipientSource, summary: Summary) {
  const { t } = summary;
  if (source.kind === "person_property") {
    return property(summary.references, source.propertyId)?.name ?? t("databaseView:recipientUnavailableProperty");
  }
  if (source.kind === "execution_actor" && summary.event) return t("databaseView:recipientEventActor");
  return t(`databaseView:recipientSource_${source.kind}`);
}

function actionText(action: AutomationAction, summary: Summary) {
  const { t, references } = summary;
  if (action.type === "edit_property") {
    const edited = property(references, action.propertyId);
    if (!edited) return t("databaseView:automationSummary.deletedPropertyEdit");
    if (clears(action.value) && edited.type !== "checkbox") return t("databaseView:automationSummary.clearProperty", { name: edited.name });
    const value = valueText(edited, action.value, summary);
    // People or options added, taken out or toggled say so, as the card does ("태그 속성에서 빨강 및 파랑 토글",
    // Notion's, seen 2026-11-11).
    const mode = (edited.type === "person" || edited.type === "multi_select") && action.value.kind === "literal" ? action.value.mode : undefined;
    if (mode && value !== null) return t(`databaseView:automationSummary.people.${mode}`, { name: edited.name, value });
    return value === null
      ? t("databaseView:automationSummary.editProperty", { name: edited.name })
      : t(REPLACED.has(edited.type) ? "databaseView:automationSummary.replaceProperty" : "databaseView:automationSummary.setProperty",
        { name: edited.name, value });
  }
  if (action.type === "add_page") {
    return t(action.databaseId === references.databaseId ? "databaseView:automationSummary.addPageHere" : "databaseView:automationSummary.addPageElsewhere");
  }
  if (action.type === "edit_pages") {
    if (action.target.type === "this_page") return t("databaseView:automationSummary.editThisPage");
    return t(action.target.databaseId === references.databaseId ? "databaseView:automationSummary.editPagesHere" : "databaseView:automationSummary.editPagesElsewhere");
  }
  // Archived pages read alike wherever they are, as Notion's "페이지 보관" (seen 2026-11-15).
  if (action.type === "archive_pages") return t("databaseView:automationSummary.archivePages");
  // "페이지 아이콘 편집", whatever icon (Notion's, seen 2026-11-21).
  if (action.type === "set_page_icon") return t("databaseView:automationSummary.setPageIcon");
  if (action.type === "send_notification") {
    // A person property's says its name, people picked how many they are, who
    // runs it among them (Notion's, seen 2026-11-08: "담당님에게 알림을
    // 보냅니다.", "사용자 2명에게 알림 보내기").
    const sources = action.recipientSources ?? [];
    if (!action.recipientIds.length && sources.length && sources.every((source) => source.kind === "person_property")) {
      return t("databaseView:automationSummary.notify", { names: sources.map((source) => recipientName(source, summary)).join(", ") });
    }
    const count = action.recipientIds.length + sources.length;
    return count ? t("databaseView:automationSummary.notifyUsers", { count }) : t("databaseView:automationSummary.notifyNobody");
  }
  if (action.type === "define_variables") return t("databaseView:automationSummary.defineVariables", { count: action.variables.length });
  if (action.type === "send_email" || action.type === "send_webhook" || action.type === "send_slack" || action.type === "run_app_action") {
    return t(`databaseView:automationSummary.${action.type}`);
  }
  return t(labelKey(action.type));
}

// Three or more go by commas, the last after the joint ("A, B 및 C"), as
// Notion's list says both its conditions and its actions (seen 2026-11-02
// for actions, 2026-11-18 at 1440 for three conditions).
function joinSentences(texts: string[], joint: string, t: TFunction) {
  return texts.length > 2
    ? `${texts.slice(0, -1).join(t("databaseView:automationSummary.comma"))}${joint}${texts[texts.length - 1]}`
    : texts.join(joint);
}

function triggerText(trigger: AutomationTriggerDraft, summary: Summary) {
  const { t, references } = summary;
  if (trigger.type === "schedule") {
    const interval = Number(trigger.interval) || 1;
    return interval === 1
      ? t(`databaseView:automationSummary.everyOne.${trigger.frequency}`)
      : t(`databaseView:automationSummary.everyMany.${trigger.frequency}`, { count: interval });
  }
  return joinSentences(trigger.conditions.map((condition) => {
    if (condition.type === "row_added") return t("databaseView:automationSummary.pageAdded");
    if (!condition.propertyId) return t("databaseView:automationSummary.anyPropertyEdited");
    const edited = property(references, condition.propertyId);
    if (!edited) return t("databaseView:automationSummary.deletedPropertyEdited");
    // What it names the edit leaves, as its card says (automationTriggerConditions.ts): people by their names, the reader's
    // own too, not "나", joined with "및" (Notion's list, seen 2026-11-18 in Chrome at 1440).
    const options = triggerPropertyOptions(edited as DbProperty);
    const pages = edited.type === "relation" ? summary.relationRows?.(edited.config?.relationDatabaseId ?? edited.databaseId) ?? [] : null;
    const pageName = (id: string) => { const page = pages?.find((candidate) => candidate.id === id); return page ? pageNamesNow.title(page) : t("databaseView:untitled"); };
    return triggerSentenceText(t, triggerSentence(t, edited as DbProperty, condition.operator, condition.value,
      (id) => pages ? pageName(id) : options.find((option) => option.id === id)?.name ?? (personLabel(id, undefined, false) || summary.personName(id) || id)));
  }), t(trigger.mode === "all" ? "databaseView:automationSummary.and" : "databaseView:automationSummary.or"), t);
}

/**
 * The line under an automation's name in its list, Notion's sentence (seen
 * 2026-10-15): "〈트리거〉의 경우, 〈작업〉 및 〈작업〉 실행", or for a schedule
 * "〈일정〉 〈작업〉"; what is gone reads "삭제된 속성". Three actions or
 * conditions or more go by commas, the last after "및" (joinSentences).
 */
export function databaseAutomationSummary(
  automation: DatabaseAutomationDefinition,
  references: DatabaseAutomationReferences,
  t: TFunction,
  personName: (id: string) => string | null = () => null,
  relationRows?: (dataSourceId: string) => Page[],
) {
  const trigger = triggerDraftFromAutomation(automation);
  const summary: Summary = { t, references, personName, event: trigger.type === "events", relationRows };
  const actions = joinSentences(storedActions(automation).map((action) => actionText(action, summary)), t("databaseView:automationSummary.and"), t);
  return t(trigger.type === "schedule" ? "databaseView:automationSummary.schedule" : "databaseView:automationSummary.event",
    { trigger: triggerText(trigger, summary), actions });
}
