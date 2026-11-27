import type { GraphDataSource, GraphProperty, GraphView } from "@/contentGraph";
import {
  databaseAutomationBounds,
  databaseAutomationScheduleFrequencies,
  databaseAutomationTriggerConditionOperators,
  type DatabaseAutomationScope,
  type DatabaseAutomationTrigger,
} from "@/databaseAutomationWire.generated";
import { triggerConditionIncomplete, type TriggerOperator } from "./automationTriggerConditions";
import { interactiveAutomationMaxDocumentBytes } from "@/interactiveAutomationWire.generated";
import { newId } from "@/lib/ids";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { PROPERTY_OPTION_CAPACITY } from "./queryBudget";
import type {
  AutomationAction,
  DatabaseAutomationDefinition,
  DbProperty,
  DbView,
  Page,
} from "@/lib/types";
import type { DatabaseAutomationDraft } from "@/databaseAutomationApi";
import type { AutomationEditorBounds } from "@/components/automation/AutomationActionEditor";
import { MAX_RECIPIENTS } from "@/components/automation/automationActionModel";
// What a condition or "속성 편집" offers lives in a leaf of its own, so the
// action model can read it without importing this model back (2026-11-10:
// the two imported each other and one load order read MAX_RECIPIENTS too soon).
export { isDatabaseAutomationEditableProperty, isDatabaseAutomationListedProperty } from "./automationPropertyKinds";

const MAX_EDITOR_VALUE_DEPTH = 20;

// The database's properties, pages and views are however many it holds, and
// an automation's actions, variables, blocks, changes and headers however many
// its settings document holds (64 KB); people picked by name stay at Notion's
// twenty.
export const databaseAutomationEditorBounds: AutomationEditorBounds = {
  maxActions: Number.MAX_SAFE_INTEGER,
  maxProperties: Number.MAX_SAFE_INTEGER,
  maxPages: Number.MAX_SAFE_INTEGER,
  maxViews: Number.MAX_SAFE_INTEGER,
  maxOptionsPerProperty: PROPERTY_OPTION_CAPACITY,
  maxVariablesPerAction: Number.MAX_SAFE_INTEGER,
  maxBlocksPerAction: Number.MAX_SAFE_INTEGER,
  maxChangesPerAction: Number.MAX_SAFE_INTEGER,
  maxRecipientsPerAction: MAX_RECIPIENTS,
  maxWebhookHeaders: Number.MAX_SAFE_INTEGER,
  maxWebhookProperties: Number.MAX_SAFE_INTEGER,
  maxValueNodes: interactiveAutomationMaxDocumentBytes,
  maxValueDepth: MAX_EDITOR_VALUE_DEPTH,
  maxContainerEntries: interactiveAutomationMaxDocumentBytes,
  maxScalarCharacters: interactiveAutomationMaxDocumentBytes,
  maxTotalCharacters: interactiveAutomationMaxDocumentBytes,
};



export type EventTriggerConditionDraft = {
  id: string;
  type: "row_added";
} | {
  id: string;
  type: "property_edited";
  /** "" for any property's edit ("편집된 모든 속성"). */
  propertyId: string;
  /** What the edit leaves in it (automationTriggerConditions.ts); none for any edit. */
  operator?: TriggerOperator;
  value?: unknown;
};

/**
 * A trigger's conditions in Notion's order: a page added first, however
 * late it was added, then the others as they were added (seen 2026-11-18 in
 * Chrome at 1440 and on a Galaxy A90 at 411: "페이지 추가 완료" added after
 * "Done" and "Count" went to the top of the editor's cards at once and led
 * the list's sentence, "페이지 추가, Done … 및 Count = 5의 경우", and above
 * "속성 편집" too; "Count", added after "Done", stayed after it). One saved
 * in another order reads the same.
 */
export function inTriggerConditionOrder(conditions: EventTriggerConditionDraft[]): EventTriggerConditionDraft[] {
  return [...conditions.filter((condition) => condition.type === "row_added"),
    ...conditions.filter((condition) => condition.type !== "row_added")];
}

export type AutomationTriggerDraft = {
  type: "events";
  mode: "any" | "all";
  conditions: EventTriggerConditionDraft[];
} | {
  type: "schedule";
  frequency: ScheduleFrequency;
  interval: string;
  time: string;
  timeZone: string;
  startsOn: string;
  endsOn: string;
  /** "실행 횟수": how many runs it ends after, "" for none (Notion's, seen 2026-10-24). */
  endsAfter?: string;
  /** Weekly: the chosen weekdays, 0 = Sunday. */
  weekdays: number[];
  /** Monthly: a day of the month, or the first to fourth or last weekday. */
  monthlyBy: "day" | "weekday";
  monthDay: string;
  monthWeek: string;
  monthWeekday: string;
  /** A phone's "매..." before its "주기" is picked, as Notion's: nothing to save yet. */
  pending?: boolean;
};

export type ScheduleFrequency = typeof databaseAutomationScheduleFrequencies[number];

/** Notion's "자정 전후": midnight to 3am, which the server runs it in once (database_automation_schedule.go). */
export const SCHEDULE_MIDNIGHT = "midnight";

const scheduleIntervalLimits: Record<ScheduleFrequency, number> = {
  daily: databaseAutomationBounds.dailyIntervalDays,
  weekly: databaseAutomationBounds.weeklyIntervalWeeks,
  monthly: databaseAutomationBounds.monthlyIntervalMonths,
  yearly: databaseAutomationBounds.yearlyIntervalYears,
};

export function scheduleIntervalLimit(frequency: ScheduleFrequency) {
  return scheduleIntervalLimits[frequency];
}

export type EventTriggerDraft = Extract<AutomationTriggerDraft, { type: "events" }>;
export type ScheduleTriggerDraft = Extract<AutomationTriggerDraft, { type: "schedule" }>;

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function localDateInputValue(date = new Date()) {
  return [date.getFullYear(), date.getMonth() + 1, date.getDate()]
    .map((part, index) => String(part).padStart(index === 0 ? 4 : 2, "0"))
    .join("-");
}

function defaultTimeZone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

export function defaultEventTriggerDraft(): EventTriggerDraft {
  return {
    type: "events",
    mode: "any",
    conditions: [{ id: newId(), type: "row_added" }],
  };
}

export function defaultScheduleTriggerDraft(): ScheduleTriggerDraft {
  return {
    type: "schedule",
    frequency: "daily",
    interval: "1",
    time: "09:00",
    timeZone: defaultTimeZone(),
    startsOn: localDateInputValue(),
    endsOn: "",
    weekdays: [],
    monthlyBy: "day",
    monthDay: "",
    monthWeek: "",
    monthWeekday: "",
  };
}

export function triggerDraftFromAutomation(
  automation: DatabaseAutomationDefinition,
): AutomationTriggerDraft {
  const trigger = recordValue(automation.trigger);
  if (automation.triggerType === "schedule" && trigger?.type === "schedule") {
    const weekdays = Array.isArray(trigger.weekdays)
      ? trigger.weekdays.filter((day): day is number => Number.isInteger(day) && day >= 0 && day <= 6)
      : [];
    const frequency = databaseAutomationScheduleFrequencies.find((value) => value === trigger.frequency) ?? "daily";
    const monthWeek = typeof trigger.monthWeek === "number" ? trigger.monthWeek : 0;
    return {
      type: "schedule",
      frequency,
      interval: typeof trigger.interval === "number" ? String(trigger.interval) : "",
      time: typeof trigger.time === "string" ? trigger.time : "",
      timeZone: typeof trigger.timeZone === "string" ? trigger.timeZone : "",
      startsOn: typeof trigger.startsOn === "string" ? trigger.startsOn : "",
      endsOn: typeof trigger.endsOn === "string" ? trigger.endsOn : "",
      // The server sends 0 for no count and no day of the month, which the draft leaves empty.
      endsAfter: typeof trigger.endsAfter === "number" && trigger.endsAfter > 0 ? String(trigger.endsAfter) : "",
      weekdays: frequency === "weekly" ? weekdays : [],
      monthlyBy: monthWeek > 0 ? "weekday" : "day",
      monthDay: typeof trigger.monthDay === "number" && trigger.monthDay > 0 ? String(trigger.monthDay) : "",
      monthWeek: monthWeek > 0 ? String(monthWeek) : "",
      monthWeekday: monthWeek > 0 && weekdays.length === 1 ? String(weekdays[0]) : "",
    };
  }
  const rawConditions = Array.isArray(trigger?.conditions) ? trigger.conditions : [];
  const conditions = rawConditions.flatMap((value): EventTriggerConditionDraft[] => {
    const condition = recordValue(value);
    if (condition?.type === "row_added") return [{ id: newId(), type: "row_added" }];
    if (condition?.type === "property_edited" && typeof condition.propertyId === "string") {
      const operator = (databaseAutomationTriggerConditionOperators as readonly string[]).includes(String(condition.operator))
        ? condition.operator as TriggerOperator : undefined;
      return [{ id: newId(), type: "property_edited", propertyId: condition.propertyId,
        ...(operator ? { operator } : {}), ...(operator && condition.value !== undefined && condition.value !== null ? { value: condition.value } : {}) }];
    }
    return [];
  });
  return {
    type: "events",
    mode: trigger?.mode === "all" ? "all" : "any",
    conditions: conditions.length > 0 ? inTriggerConditionOrder(conditions) : defaultEventTriggerDraft().conditions,
  };
}

function validDateInput(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return false;
  const instant = new Date(Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3])));
  return instant.getUTCFullYear() === Number(match[1])
    && instant.getUTCMonth() + 1 === Number(match[2])
    && instant.getUTCDate() === Number(match[3]);
}

function validTimeZone(value: string) {
  if (!value || value.length > 160) return false;
  try {
    Intl.DateTimeFormat("en", { timeZone: value }).format(0);
    return true;
  } catch {
    return false;
  }
}

function integerIn(value: string, min: number, max: number) {
  const number = Number(value);
  return Number.isInteger(number) && number >= min && number <= max ? number : null;
}

/** The weekday and month-day choices each frequency stores, or null when incomplete. */
function scheduleRepeat(draft: ScheduleTriggerDraft): { weekdays?: number[]; monthDay?: number; monthWeek?: number } | null {
  if (draft.frequency === "weekly") {
    const weekdays = Array.from(new Set(draft.weekdays)).filter((day) => day >= 0 && day <= 6).sort((a, b) => a - b);
    return weekdays.length > 0 ? { weekdays } : null;
  }
  if (draft.frequency === "monthly") {
    if (draft.monthlyBy === "day") {
      const monthDay = integerIn(draft.monthDay, 1, 31);
      return monthDay === null ? null : { monthDay };
    }
    const monthWeek = integerIn(draft.monthWeek, 1, 5);
    const weekday = integerIn(draft.monthWeekday, 0, 6);
    return monthWeek === null || weekday === null ? null : { monthWeek, weekdays: [weekday] };
  }
  return {};
}

export function storedTrigger(
  draft: AutomationTriggerDraft,
  properties: readonly DbProperty[],
): DatabaseAutomationTrigger | null {
  if (draft.type === "schedule") {
    if (draft.pending) return null;
    const interval = Number(draft.interval);
    // "자정 전후" is a window the server runs it in, as Notion's.
    const timeMatch = draft.time === SCHEDULE_MIDNIGHT ? ["", "00", "00"] : /^(\d{2}):(\d{2})$/.exec(draft.time);
    const endsAfter = draft.endsAfter ? Number(draft.endsAfter) : 0;
    const repeat = scheduleRepeat(draft);
    if (
      !repeat
      || !Number.isSafeInteger(interval)
      || interval < 1
      || interval > scheduleIntervalLimit(draft.frequency)
      || !timeMatch
      || Number(timeMatch[1]) > 23
      || Number(timeMatch[2]) > 59
      || !validTimeZone(draft.timeZone)
      || !validDateInput(draft.startsOn)
      || (draft.endsOn !== "" && (!validDateInput(draft.endsOn) || draft.endsOn < draft.startsOn))
      || !Number.isSafeInteger(endsAfter) || endsAfter < 0 || endsAfter > databaseAutomationBounds.scheduleMaxRuns
      || (endsAfter > 0 && draft.endsOn !== "")
    ) return null;
    return {
      type: "schedule",
      frequency: draft.frequency,
      interval,
      time: draft.time,
      timeZone: draft.timeZone,
      startsOn: draft.startsOn,
      ...(draft.endsOn ? { endsOn: draft.endsOn } : {}),
      ...(endsAfter ? { endsAfter } : {}),
      ...repeat,
    };
  }
  // "모든" may hold a page added, as Notion's (seen 2026-10-24): then only a page added holding the rest runs it.
  if (draft.conditions.length < 1) return null;
  const propertyIds = new Set(properties.map((property) => property.id));
  const conditionKeys = new Set<string>();
  const conditions: Array<{ type: "row_added" } | { type: "property_edited"; propertyId: string; operator?: TriggerOperator; value?: unknown }> = [];
  for (const condition of draft.conditions) {
    const key = condition.type === "row_added" ? "row_added" : `property:${condition.propertyId}`;
    if (
      conditionKeys.has(key)
      || (condition.type === "property_edited" && condition.propertyId !== "" && !propertyIds.has(condition.propertyId))
      || (condition.type === "property_edited" && condition.propertyId === "" && condition.operator)
      || (condition.type === "property_edited" && triggerConditionIncomplete(condition.operator, condition.value))
    ) return null;
    conditionKeys.add(key);
    conditions.push(condition.type === "row_added"
      ? { type: "row_added" }
      : { type: "property_edited", propertyId: condition.propertyId,
        ...(condition.operator ? { operator: condition.operator } : {}),
        ...(condition.operator && condition.value !== undefined && condition.value !== null && condition.value !== "" ? { value: condition.value } : {}) });
  }
  return { type: "events", mode: draft.mode, conditions };
}

/** Where a stored automation applies: its database, or one of its views. */
export function automationScopeOf(automation: DatabaseAutomationDefinition): DatabaseAutomationScope {
  return automation.scopeType === "view" && automation.viewId ? { type: "view", viewId: automation.viewId } : { type: "database" };
}

export function storedDefinitionDraft(
  automation: DatabaseAutomationDefinition,
  name: string,
  trigger: DatabaseAutomationTrigger,
  actions: AutomationAction[],
  scope: DatabaseAutomationScope = automationScopeOf(automation),
): DatabaseAutomationDraft {
  return {
    name,
    scope,
    trigger: structuredClone(trigger),
    actionDocument: {
      version: 1,
      label: name,
      actions: structuredClone(actions),
    },
  };
}

export function newDefinitionDraft(
  name: string,
  trigger: DatabaseAutomationTrigger,
  actions: AutomationAction[],
  scope: DatabaseAutomationScope = { type: "database" },
): DatabaseAutomationDraft {
  return {
    name,
    scope,
    trigger: structuredClone(trigger),
    actionDocument: { version: 1, label: name, actions: structuredClone(actions) },
  };
}

export function databaseAutomationEditorProperties(
  properties: readonly GraphProperty[],
): DbProperty[] {
  return properties.map((property, index) => ({
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    type: property.type,
    config: property.config,
    position: index,
  } as DbProperty));
}



export function databaseAutomationEditorViews(views: readonly GraphView[]): DbView[] {
  return views.map((view, index) => ({
    id: view.id,
    databaseId: view.dataSourceId,
    name: view.name,
    type: view.type,
    config: view.config,
    position: index,
  } as DbView));
}

/** The page's databases an action offers, one in the trash marked so its lists leave it out (dataSourcePage). */
export function databaseAutomationEditorPages(
  workspaceId: string,
  dataSources: readonly GraphDataSource[],
): Page[] {
  return dataSources.map((source, index) => dataSourcePage(source, workspaceId, index));
}

export type DatabaseAutomationPanelOperation = {
  identity: string;
  signal: AbortSignal;
  isCurrent(): boolean;
  retire(): void;
};

export function createDatabaseAutomationPanelLifecycle() {
  let generation = 0;
  let current: { generation: number; controller: AbortController } | null = null;
  return {
    begin(identity: string): DatabaseAutomationPanelOperation {
      current?.controller.abort();
      const controller = new AbortController();
      const ownGeneration = ++generation;
      current = { generation: ownGeneration, controller };
      return {
        identity,
        signal: controller.signal,
        isCurrent: () => (
          current?.generation === ownGeneration
          && current.controller === controller
          && !controller.signal.aborted
        ),
        retire() {
          if (current?.generation !== ownGeneration || current.controller !== controller) return;
          generation += 1;
          controller.abort();
          current = null;
        },
      };
    },
  };
}

function viewFilterTreeCount(value: unknown): number {
  if (!value || typeof value !== "object") return 0;
  const group = value as { filters?: unknown[]; groups?: unknown[] };
  return (Array.isArray(group.filters) ? group.filters.length : 0)
    + (Array.isArray(group.groups) ? group.groups.reduce<number>((total, child) => total + viewFilterTreeCount(child), 0) : 0);
}

/**
 * How many filters a view holds, as Notion's "적용할 페이지 위치" says under
 * it ("필터 1개", People's two views, seen 2026-11-14; "이 보기에 있는
 * 페이지" for one with none): its filter pills and its advanced filters.
 */
export function automationViewFilterCount(view: Pick<DbView, "config">): number {
  const config = (view.config ?? {}) as Record<string, unknown>;
  const pills = Array.isArray(config.quickFilters) ? config.quickFilters.length : 0;
  const advanced = config.filterGroup ? viewFilterTreeCount(config.filterGroup)
    : Array.isArray(config.filters) ? config.filters.length : 0;
  return pills + advanced;
}
