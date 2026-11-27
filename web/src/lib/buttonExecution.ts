import { i18next } from "@/i18n";
import { interactiveAutomationErrorCodes, type InteractiveAutomationExecuteResponse, type InteractiveAutomationOutcome } from "@/interactiveAutomationWire.generated";
import type {
  ButtonClientOutcome,
  ButtonConfirmationChallenge,
  ExecuteDatabaseButtonResult,
  ExecutePageButtonResult,
} from "./buttonExecutionContract";
import { LeafenApiError } from "@/apiTransport";
import { formPeekHref, inlineDatabasePeekHref, pagePeekHref } from "./pagePeek";
import { rememberInlineDatabaseAddress } from "./inlineDatabaseAddress";
import { formHref } from "./formViewModel";
import { pageHref } from "./navigation";

const MAX_BUTTON_CLIENT_OUTCOMES = 20;

export interface ButtonOutcomeDispatcher {
  navigate: (href: string) => void;
  focusBlock?: (outcome: ButtonClientOutcome) => void;
  openExternal?: (url: string) => void;
}

function openPublicHttpUrl(url: string) {
  let target: URL;
  try {
    target = new URL(url);
  } catch {
    return;
  }
  if (target.protocol !== "http:" && target.protocol !== "https:") return;
  window.open(target.toString(), "_blank", "noopener,noreferrer");
}

export function dispatchButtonClientOutcomes(
  outcomes: readonly ButtonClientOutcome[],
  dispatcher: ButtonOutcomeDispatcher,
) {
  if (outcomes.length > MAX_BUTTON_CLIENT_OUTCOMES) return;
  for (const outcome of outcomes) {
    if (outcome.type === "focus_block" && outcome.blockId) {
      dispatcher.focusBlock?.(outcome);
      continue;
    }
    if (outcome.type === "open_page" && outcome.pageId) {
      // Over the page unless it says otherwise, as Notion's button opens a
      // page (seen 2026-11-02), the button's page staying behind it; a
      // database a page holds (containerId) opens alone, at its own address
      // or over the page (seen 2026-11-24).
      const mode = outcome.openIn === "side_peek" ? "side" : "center";
      if (outcome.containerId) rememberInlineDatabaseAddress(outcome.containerId, outcome.pageId);
      dispatcher.navigate(outcome.containerId ? outcome.openIn === "full_page" ? pageHref(outcome.containerId)
        : inlineDatabasePeekHref(outcome.pageId, outcome.containerId, mode)
        : outcome.openIn === "full_page" ? pageHref(outcome.pageId) : pagePeekHref(outcome.pageId, mode));
      continue;
    }
    if (outcome.type === "open_form" && outcome.databaseId && outcome.viewId) {
      // Its answer page, as Notion's button opens a form (seen 2026-11-05):
      // beside the page, over it — the default — or in its place; one that
      // takes no answers of theirs says so over the page.
      dispatcher.navigate(outcome.openIn === "full_page" && outcome.formToken ? formHref(outcome.formToken)
        : formPeekHref(outcome.formToken ?? "", outcome.openIn === "side_peek" ? "side" : "center"));
      continue;
    }
    if (outcome.type === "open_url" && outcome.url) {
      (dispatcher.openExternal ?? openPublicHttpUrl)(outcome.url);
    }
  }
}

const WIRE_OUTCOME_TYPES: Record<InteractiveAutomationOutcome["type"], ButtonClientOutcome["type"]> = {
  openPage: "open_page", openForm: "open_form", openUrl: "open_url",
};

// A run that asks first: the server's question and the token that answers it.
function confirmationFromWire(response: InteractiveAutomationExecuteResponse, executionId: string): ButtonConfirmationChallenge | null {
  const asked = response.confirmation;
  return asked ? {
    executionId: response.executionId ?? executionId,
    replayed: response.receipt?.duplicate === true,
    insertedBlockIds: (response.effects?.blocks ?? []).map(({ id }) => id),
    createdPageIds: (response.effects?.pages ?? []).map(({ id }) => id),
    editedPageIds: (response.effects?.edited ?? []).map(({ id }) => id),
    tasks: response.effects?.tasks === true,
    clientOutcomes: outcomesFromWire(response.outcomes),
    canUndo: response.canUndo,
    confirmationRequired: true,
    confirmationToken: asked.token,
    confirmations: [{ actionId: "", title: asked.title, message: asked.message, messageSpans: asked.messageSpans,
      confirmLabel: asked.confirmLabel, cancelLabel: asked.cancelLabel }],
  } : null;
}

function outcomesFromWire(outcomes: readonly InteractiveAutomationOutcome[] = []): ButtonClientOutcome[] {
  return outcomes.flatMap((outcome) => {
    const type = WIRE_OUTCOME_TYPES[outcome.type];
    return type ? [{ actionId: "", type, pageId: outcome.pageId, databaseId: outcome.databaseId, viewId: outcome.viewId, containerId: outcome.containerId, url: outcome.url, openIn: outcome.openIn, formToken: outcome.formToken }] : [];
  });
}

/** A database button's run as its cell takes it: a question to answer, or done with what to open. */
export function databaseButtonResultFromWire(response: InteractiveAutomationExecuteResponse, executionId: string): ExecuteDatabaseButtonResult {
  return confirmationFromWire(response, executionId) ?? {
    executionId: response.executionId ?? executionId,
    replayed: response.receipt?.duplicate === true,
    createdPages: [],
    updatedPages: [],
    clientOutcomes: outcomesFromWire(response.outcomes),
    notice: {
      createdPageIds: (response.effects?.pages ?? []).map(({ id }) => id),
      editedPageIds: (response.effects?.edited ?? []).map(({ id }) => id),
      tasks: response.effects?.tasks === true,
      canUndo: response.canUndo === true,
    },
  };
}

/** A page button's run as its block takes it: a question to answer, or done with what it added and what to open. */
export function pageButtonResultFromWire(response: InteractiveAutomationExecuteResponse, executionId: string): ExecutePageButtonResult {
  return confirmationFromWire(response, executionId) ?? {
    executionId: response.executionId ?? executionId,
    replayed: response.receipt?.duplicate === true,
    insertedBlockIds: (response.effects?.blocks ?? []).map(({ id }) => id),
    createdPageIds: (response.effects?.pages ?? []).map(({ id }) => id),
    editedPageIds: (response.effects?.edited ?? []).map(({ id }) => id),
    tasks: response.effects?.tasks === true,
    clientOutcomes: outcomesFromWire(response.outcomes),
    canUndo: response.canUndo,
  };
}

/** A definite rejection allows a new click; an uncertain delivery keeps its ID. */
/** Whether the server would not run a button's actions as they are set: one to fix in its settings. */
export function buttonActionsRefused(error: unknown): boolean {
  return error instanceof LeafenApiError && error.code === interactiveAutomationErrorCodes.invalid;
}

/** Whether what a button's action names — a database, a page, a template —
 * is not here: one brought back alone from an archive points outside it
 * (2026-11-13: it said only that the button didn't run). Fixed in its settings. */
export function buttonTargetMissing(error: unknown): boolean {
  return error instanceof LeafenApiError && error.code === interactiveAutomationErrorCodes.notFound;
}

export function buttonExecutionCanRestart(error: unknown): boolean {
  return error instanceof LeafenApiError && error.status >= 400 && error.status < 500;
}

/**
 * What a button's run made and changed, as Notion says it (seen 2026-11-02):
 * "1개의 작업이 생성되었습니다. 및 25개의 작업이 편집되었습니다." — pages
 * where they are not all tasks — or nothing when it touched no page.
 */
export function buttonRunNoticeText(created: number, edited: number, tasks: boolean) {
  const noun = tasks ? "Tasks" : "Pages";
  const parts = [
    created ? i18next.t(`blockItem:button.created${noun}`, { count: created }) : "",
    edited ? i18next.t(`blockItem:button.edited${noun}`, { count: edited }) : "",
  ].filter(Boolean);
  return parts.length ? new Intl.ListFormat(i18next.language, { type: "conjunction" }).format(parts) : "";
}
