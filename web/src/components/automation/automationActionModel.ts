import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";
import { isDatabaseAutomationListedProperty } from "@/components/database/automationPropertyKinds";
import { validAutomationRecipientFormula } from "./automationRecipientFormula";
import { automationValueTypeWrong } from "./automationValueTypes";
import { automationActionFormulaFault, automationCheckedNames } from "./automationFormulaFault";
import { earlierAutomationVariables } from "./automationFormulaNames";
import type { FormulaScopeName } from "@/components/database/formulaEditorModel";
import { publicWebAddress } from "@/lib/publicWebAddress";
import type {
  AutomationAction,
  AutomationValueExpression,
  DbProperty,
  DbView,
  Page,
} from "@/lib/types";
import {
  admitAutomationDocument,
  admitAutomationEditorInputs,
  type AutomationEditorBounds,
} from "./automationActionBudget";

export type AutomationEditorSurface =
  | "database_button"
  | "page_button"
  | "event_automation"
  | "schedule_automation";

export type AutomationActionType = AutomationAction["type"];

// Notion's own: five webhook actions to an automation, and twenty people
// picked by name to notify (seen in its help, 2026-10-13).
export const MAX_WEBHOOK_ACTIONS = 5;
export const MAX_RECIPIENTS = 20;
const WEBHOOK_HEADER_NAME = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const WEBHOOK_RESERVED_HEADERS = new Set([
  "connection",
  "content-length",
  "content-type",
  "host",
  "idempotency-key",
  "transfer-encoding",
  "x-leafen-automation-delivery",
]);

// In the order Notion lists them (seen 2026-10-06 for a button, 2026-10-24
// for a database's automation; "페이지 보관하기…" after "웹훅 보내기" in both,
// 2026-11-15; "페이지 아이콘 편집…" after "페이지 편집 위치" in both, 2026-11-21), Leafen's own last.
const BUTTON_ACTION_TYPES: AutomationActionType[] = [
  "edit_property",
  "add_page",
  "edit_pages",
  "set_page_icon",
  "send_notification",
  "send_email",
  "send_webhook",
  "archive_pages",
  "show_confirmation",
  "open_page",
  "open_form",
  "open_url",
  "send_slack",
  "define_variables",
];

const EVENT_ACTION_TYPES: AutomationActionType[] = [
  "edit_property",
  "add_page",
  "edit_pages",
  "set_page_icon",
  "send_notification",
  "send_email",
  "send_webhook",
  "archive_pages",
  "send_slack",
  "define_variables",
];

/** The steps that write the page an action ran for: its property ("속성 편집…") and its icon ("페이지 아이콘 편집…"). */
export const TRIGGER_PAGE_TYPES: ReadonlySet<AutomationActionType> = new Set(["edit_property", "set_page_icon"]);

export function automationActionTypesForSurface(surface: AutomationEditorSurface) {
  // A page's button and a schedule have no page of their own to edit or give an icon, as Notion's offer neither (2026-11-21).
  if (surface === "page_button") return ["insert_blocks" as const, ...BUTTON_ACTION_TYPES.filter((type) => !TRIGGER_PAGE_TYPES.has(type))];
  if (surface === "database_button") return BUTTON_ACTION_TYPES;
  if (surface === "schedule_automation") return EVENT_ACTION_TYPES.filter((type) => !TRIGGER_PAGE_TYPES.has(type));
  return EVENT_ACTION_TYPES;
}

export function defaultPropertyValue(property?: DbProperty): AutomationValueExpression {
  // A date starts empty, "Date 지우기" until a value is chosen (Notion's, seen 2026-11-10).
  if (property?.type === "date") return { kind: "literal", value: null };
  if (property?.type === "checkbox") return { kind: "literal", value: false };
  if (property?.type === "multi_select" || property?.type === "person" || property?.type === "relation") {
    return { kind: "literal", value: [] };
  }
  if (property?.type === "number" || property?.type === "select" || property?.type === "status" || property?.type === "place") {
    return { kind: "literal", value: null };
  }
  if (property?.type === "verification") {
    return { kind: "literal", value: { state: "unverified" } };
  }
  return { kind: "literal", value: "" };
}

/**
 * A page line's value as the line is added (a page added, pages edited): a
 * checkbox's set to nothing yet, its field saying "선택…" (Notion's, seen
 * 2026-11-10); any other's as an action's.
 */
export function defaultLineValue(property?: DbProperty): AutomationValueExpression {
  return property?.type === "checkbox" ? { kind: "literal", value: null } : defaultPropertyValue(property);
}

/**
 * Where a property stands in an automation's property lists ("속성 편집...",
 * a page's "속성 편집 ⌄" and a line's property): the name first, then the
 * rest by name (Notion's, seen 2026-11-03 and 2026-11-10).
 */
export function automationPropertyOrder(a: DbProperty, b: DbProperty) {
  return Number(b.type === "title") - Number(a.type === "title") || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
}

export function databasePages(pages: readonly Page[]) {
  return pages.filter((page) => page.kind === "database" && !page.inTrash);
}

export function newAutomationEditorAction(
  type: AutomationActionType,
  {
    properties,
    pages,
    views,
    database,
  }: {
    properties: readonly DbProperty[];
    pages: readonly Page[];
    views: readonly DbView[];
    database?: Page; // the actions' own, as AutomationActionEditor decides it; none for a page's button
  },
  bounds: AutomationEditorBounds,
): AutomationAction {
  admitAutomationEditorInputs([], properties, pages, views, bounds);
  const property = properties.find(isDatabaseAutomationListedProperty);
  if (type === "edit_property") {
    return {
      id: newId(),
      type,
      target: "trigger_page",
      propertyId: property?.id ?? "",
      value: defaultPropertyValue(property),
    };
  }
  if (type === "insert_blocks") {
    return {
      id: newId(),
      type,
      target: "trigger_page",
      blocks: [{ type: "paragraph", content: { text: "" } }],
    };
  }
  // Where pages are added or edited starts unchosen, "데이터 소스 선택", as
  // Notion's in a database's automation, a button property and a page's
  // button (seen 2026-11-02).
  if (type === "add_page") {
    return {
      id: newId(),
      type,
      target: "database",
      databaseId: "",
      title: "",
    };
  }
  if (type === "edit_pages") {
    return {
      id: newId(),
      type,
      target: { type: "database", databaseId: "", filter: { conjunction: "and", filters: [], groups: [] }, timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone },
      changes: [],
    };
  }
  // "보관 [데이터 소스 선택 ⌄]" until its pages are named (Notion's, seen 2026-11-15).
  if (type === "archive_pages") {
    return {
      id: newId(),
      type,
      target: { type: "database", databaseId: "", filter: { conjunction: "and", filters: [], groups: [] }, timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone },
    };
  }
  if (type === "set_page_icon") return { id: newId(), type, target: "trigger_page", icon: "", iconType: "" }; // "아이콘 [☺]" until chosen (Notion's).
  if (type === "define_variables") {
    return {
      id: newId(),
      type,
      // "변수 1" in words, as Notion's new one (seen 2026-10-24).
      variables: [{ name: i18next.t("databaseView:variableDefaultName", { number: 1 }), value: { kind: "literal", value: "" } }],
    };
  }
  // Whom it notifies starts unchosen, "받는 사람 선택 ⌄" (Notion's database button, seen 2026-11-21 twice).
  if (type === "send_notification") return { id: newId(), type, recipientIds: [], message: "" };
  if (type === "send_email") {
    return { id: newId(), type, recipientEmail: "", subject: "", message: "" };
  }
  if (type === "send_webhook") {
    return { id: newId(), type, url: "", propertyIds: [], headers: [] };
  }
  if (type === "send_slack") {
    return { id: newId(), type, connectionId: "", channelId: "", message: "" };
  }
  if (type === "run_app_action") {
    return { id: newId(), type, appInstallId: "", appAction: "", appInput: {} };
  }
  if (type === "show_confirmation") {
    // In the reader's language, as Notion's "계속하시겠습니까?", "계속" and "취소".
    return {
      id: newId(),
      type,
      message: i18next.t("databaseView:confirmationDefaults.message"),
      confirmLabel: i18next.t("databaseView:confirmationDefaults.confirm"),
      cancelLabel: i18next.t("databaseView:confirmationDefaults.cancel"),
    };
  }
  if (type === "open_page") {
    return { id: newId(), type, pageId: pages.find((page) => !page.inTrash)?.id ?? "", openIn: "center_peek" };
  }
  if (type === "open_form") {
    // Its own database's form, as its list offers that database's first (AutomationOpenTargetPicker).
    const own = database ? views.filter((view) => view.databaseId === database.id) : [];
    return { id: newId(), type, databaseId: database?.id ?? "", viewId: (own.find((view) => view.type === "form") ?? own[0])?.id ?? "" };
  }
  return { id: newId(), type: "open_url", url: "" };
}

function expressionValid(value: AutomationValueExpression, knownVariables: ReadonlySet<string>) {
  if (value.kind === "formula") return Boolean(value.expression.trim());
  if (value.kind === "variable") return knownVariables.has(value.name);
  if (value.kind === "trigger_property") return Boolean(value.propertyId);
  return true;
}

export function automationEditorActionsValid(
  actions: readonly AutomationAction[],
  surface: AutomationEditorSurface,
  bounds: AutomationEditorBounds,
  properties: readonly DbProperty[] = [],
) {
  try {
    admitAutomationEditorInputs(actions, [], [], [], bounds);
  } catch {
    return false;
  }
  if (actions.length === 0 || actions.length > 20) return false;
  if (actions.filter((action) => action.type === "send_webhook").length > MAX_WEBHOOK_ACTIONS) {
    return false;
  }
  // An app's action may join any surface's actions (Leafen).
  const allowed = new Set<AutomationActionType>([...automationActionTypesForSurface(surface), "run_app_action"]);
  const knownVariables = new Set<string>();
  for (const action of actions) {
    if (!allowed.has(action.type)) return false;
    // A formula its editor names a problem in holds it back, as Notion's "활성화" (seen 2026-11-21).
    if (automationActionFormulaFault(action, properties, automationCheckedNames(surface, earlierAutomationVariables(actions, actions.indexOf(action)), properties))) return false;
    if (action.type === "define_variables") {
      if (!action.variables.length) return false;
      for (const variable of action.variables) {
        if (!variable.name.trim() || knownVariables.has(variable.name) || !expressionValid(variable.value, knownVariables)) {
          return false;
        }
        knownVariables.add(variable.name);
      }
      continue;
    }
    if (action.type === "edit_property") {
      if (!action.propertyId || !expressionValid(action.value, knownVariables)) return false;
      if (automationValueTypeWrong(action.value, properties.find((property) => property.id === action.propertyId), properties)) return false;
    } else if (action.type === "insert_blocks") {
      if (!action.blocks?.length && !action.sourceBlockId) return false;
    } else if (action.type === "add_page") {
      // A page may be added untitled: the database alone is asked for, as Notion's (seen 2026-11-02).
      const titleValid = typeof action.title === "string" || expressionValid(action.title, knownVariables);
      if (!action.databaseId || !titleValid) return false;
    } else if (action.type === "edit_pages") {
      if (!action.target.databaseId || Array.isArray(action.target.filter.rowIds) && (!action.target.limit || action.target.limit < 1 || action.target.limit > 100) || !action.changes.length) {
        return false;
      }
      if (action.changes.some((change) => !change.propertyId || !expressionValid(change.value, knownVariables))) {
        return false;
      }
    } else if (action.type === "archive_pages") {
      // A schedule sets off no page to archive (database_automation_validation.go).
      if (!action.target.databaseId || action.target.type === "this_page" && surface === "schedule_automation"
        || Array.isArray(action.target.filter.rowIds) && (!action.target.limit || action.target.limit < 1 || action.target.limit > 100)) {
        return false;
      }
    } else if (action.type === "set_page_icon") { if (!action.icon || !action.iconType) return false;
    } else if (action.type === "send_notification") {
      const count = action.recipientIds.length + (action.recipientSources?.length ?? 0);
      if (!count || count > 20 || !automationMessageWritten(action)) return false;
      if (action.recipientSources?.some(source =>
        (source.kind === "formula" && !validAutomationRecipientFormula(source.expression, properties))
        || (source.kind === "person_property" && !source.propertyId.trim()))) return false;
    } else if (action.type === "send_email") {
      if (!emailAddressValid(action.recipientEmail) || !action.subject.trim() || !automationMessageWritten(action)) {
        return false;
      }
    } else if (action.type === "send_webhook") {
      if (!publicWebAddress(action.url) || !webhookHeadersValid(action.headers)) return false;
      if (surface !== "event_automation" && action.propertyIds.length > 0) return false;
      if (new Set(action.propertyIds).size !== action.propertyIds.length) return false;
    } else if (action.type === "send_slack") {
      if (!action.connectionId.trim() || !action.channelId.trim() || !automationMessageWritten(action)) return false;
    } else if (action.type === "run_app_action") {
      if (!action.appInstallId || !action.appAction) return false;
    } else if (action.type === "show_confirmation") {
      if (
        (action.title !== undefined && !action.title.trim())
        || !automationMessageWritten(action)
        || !action.confirmLabel.trim()
        || !action.cancelLabel.trim()
      ) return false;
    } else if (action.type === "open_page") {
      if (!action.pageId && action.target !== "trigger_page") return false;
    } else if (action.type === "open_form") {
      if (!action.databaseId || !action.viewId) return false;
    } else if (!publicHttpUrl(action.url)) {
      return false;
    }
  }
  return true;
}

const PROBLEM = "databaseView:actionProblems";

/**
 * The first thing an action lacks, as Notion's button names it beside its red
 * mark (its messages, read 2026-11-01; "수신자가 없습니다." seen the same day):
 * a locale key, or null when nothing Notion would name is missing.
 */
export function automationActionProblem(
  action: AutomationAction, properties?: readonly DbProperty[],
  /** The names its formulas run with (automationCheckedNames): given, a formula that can't run is named too. */
  names?: readonly FormulaScopeName[],
): string | null {
  // A formula its editor names a problem in (Notion's, seen 2026-11-21): "속성 편집"'s says it can't be read or has a
  // type error, a variable's, a page line's or a message's says so where it stands, its card red once asked for.
  const fault = properties && names ? automationActionFormulaFault(action, properties, names) : null;
  const faulty = fault ? `${PROBLEM}.formula` : null;
  switch (action.type) {
    case "send_notification":
      if (!action.recipientIds.length && !(action.recipientSources ?? []).length) return `${PROBLEM}.recipients`;
      return automationMessageWritten(action) ? faulty : `${PROBLEM}.message`;
    case "send_email":
      if (!emailAddressValid(action.recipientEmail)) return `${PROBLEM}.recipients`;
      if (!action.subject.trim()) return `${PROBLEM}.emailSubject`;
      return automationMessageWritten(action) ? faulty : `${PROBLEM}.emailBody`;
    case "send_webhook":
      // What the server won't send to stops the button (2026-11-03: it failed saying nothing).
      if (!publicWebAddress(action.url)) return `${PROBLEM}.webhookUrl`;
      return webhookHeadersValid(action.headers) ? null : `${PROBLEM}.webhookHeader`;
    case "send_slack":
      // Its channel unchosen, as Notion's red "Slack 채널 선택" (seen 2026-11-15), then its words.
      if (!action.connectionId.trim() || !action.channelId.trim()) return `${PROBLEM}.slackChannel`;
      return automationMessageWritten(action) ? faulty : `${PROBLEM}.message`;
    case "define_variables":
      return faulty;
    case "add_page":
      if (!action.databaseId) return `${PROBLEM}.addPageTarget`;
      return linesTypeWrong(action.properties ?? [], properties) ? `${PROBLEM}.formulaType` : faulty;
    case "archive_pages":
      return action.target.databaseId ? null : `${PROBLEM}.archivePagesTarget`;
    case "set_page_icon": return action.icon && action.iconType ? null : `${PROBLEM}.pageIcon`;
    case "edit_pages":
      if (!action.target.databaseId) return `${PROBLEM}.editPagesTarget`;
      // A line's formula of a type its property can't take, as "속성 편집"'s (Notion's, seen 2026-11-10).
      return linesTypeWrong(action.changes, properties) ? `${PROBLEM}.formulaType` : faulty;
    case "edit_property":
      if (!action.propertyId) return `${PROBLEM}.property`;
      if (fault?.parse) return `${PROBLEM}.formulaParse`;
      // A formula of a type its property can't take, or one naming what it doesn't know: "수식에 유형 오류가
      // 있습니다." (Notion's, seen 2026-11-10 and 2026-11-21 — "foo" set to a text property).
      return fault || properties && automationValueTypeWrong(action.value, properties.find((property) => property.id === action.propertyId), properties)
        ? `${PROBLEM}.formulaType` : null;
    case "open_page":
      return action.pageId || action.target === "trigger_page" ? null : `${PROBLEM}.openTarget`;
    case "open_form":
      return action.databaseId && action.viewId ? null : `${PROBLEM}.openTarget`;
    case "open_url":
      return publicHttpUrl(action.url) ? null : `${PROBLEM}.openTarget`;
    default:
      return null;
  }
}

/** Whether a page's line sets a formula of a type its property can't take. */
function linesTypeWrong(changes: readonly { propertyId: string; value: AutomationValueExpression }[], properties?: readonly DbProperty[]) {
  return Boolean(properties) && changes.some((change) => automationValueTypeWrong(change.value,
    properties!.find((property) => property.id === change.propertyId), properties!));
}

// An address alone, as the server reads one (mail.ParseAddress giving back
// what was written): no name, brackets, quotes or second address around it.
function emailAddressValid(value: string) {
  return /^[^\s@<>()",;:\\[\]]+@[^\s@<>()",;:\\[\]]+\.[^\s@<>()",;:\\[\]]+$/u.test(value.trim());
}

/** Whether a webhook's headers are ones the server sends as given (interactive_automation_action_delivery.go). */
function webhookHeadersValid(headers: readonly { name: string; value: string }[]) {
  const names = new Set<string>();
  for (const header of headers) {
    const name = header.name.trim();
    const value = header.value.trim();
    const normalizedName = name.toLowerCase();
    if (
      !name
      || name.length > 128
      || !WEBHOOK_HEADER_NAME.test(name)
      || !value
      || value.length > 2_048
      || value.includes("\r")
      || value.includes("\n")
      || WEBHOOK_RESERVED_HEADERS.has(normalizedName)
      || names.has(normalizedName)
    ) return false;
    names.add(normalizedName);
  }
  return true;
}

/**
 * Whether a webhook's field takes an address: an https one, as Notion's editor
 * takes (seen 2026-11-04: an http, a scheme-less or a localhost address is
 * "유효하지 않은 URL"), that the server sends to — not this computer or a
 * private network, which Notion's editor lets by.
 */
export function webhookUrlValid(value: string) {
  return /^https:/iu.test(value.trim()) && publicWebAddress(value);
}

export function publicHttpUrl(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

export function labelKey(type: AutomationActionType) {
  return `databaseView:actionTypes.${type}`;
}

/** An action's words in the list it is added from, as Notion's ("속성 편집...", "다음으로 알림 전송..."). */
export function menuLabelKey(type: AutomationActionType) {
  return `databaseView:actionMenuTypes.${type}`;
}

// An action's words are written out, or a formula (AutomationMessageField.tsx).
function automationMessageWritten(action: { message: string; messageFormula?: string }) {
  return Boolean(action.message.trim() || action.messageFormula?.trim());
}

const MESSAGE_ACTIONS = new Set(["send_notification", "send_email", "send_slack"]);

export function automationDocumentActions(
  value: unknown,
  bounds: AutomationEditorBounds,
): AutomationAction[] {
  if (!value || typeof value !== "object" || !Array.isArray((value as { actions?: unknown }).actions)) return [];
  admitAutomationDocument(value, bounds);
  return structuredClone((value as { actions: unknown[] }).actions).map((action) => {
    // Words kept as a formula come back with none written out.
    if (action && typeof action === "object" && MESSAGE_ACTIONS.has(String((action as { type?: unknown }).type))
      && typeof (action as { message?: unknown }).message !== "string") {
      action = { ...action, message: "" };
    }
    // A notification saved to a person property alone comes back without its
    // empty list of people, as the server's document leaves it out.
    if (action && typeof action === "object" && (action as { type?: unknown }).type === "send_notification") {
      const notification = action as Record<string, unknown>;
      return { ...notification, recipientIds: Array.isArray(notification.recipientIds) ? notification.recipientIds : [] } as unknown as AutomationAction;
    }
    if (!action || typeof action !== "object" || (action as { type?: unknown }).type !== "send_webhook") {
      return action as AutomationAction;
    }
    const {
      body: _body,
      ...webhook
    } = action as Record<string, unknown>;
    return {
      ...webhook,
      propertyIds: Array.isArray(webhook.propertyIds) ? webhook.propertyIds : [],
      headers: Array.isArray(webhook.headers) ? webhook.headers : [],
    } as unknown as AutomationAction;
  });
}

