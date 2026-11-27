import { useEffect, useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import type {
  DatabaseToolbarTemplateMetadataWire,
  DatabaseToolbarTemplateRepeatWire,
} from "@/databaseToolbarConfigWire.generated";
import { chosenTimeZone } from "@/lib/dateReminders";
import { followPageDatabaseCopies } from "@/lib/pageDatabaseCopies";
import type { Session } from "@/types";

/**
 * A database page's empty body offers "템플릿을 생성하세요" (seen
 * 2026-10-12): it asks the database's own toolbar, which holds its
 * templates, to make a new one and open it.
 */
const CREATE_DATABASE_TEMPLATE_EVENT = "leafen:create-database-template";
type CreateDatabaseTemplateDetail = { dataSourceId: string };

/** True when a view of the database took the request. */
export function requestDatabaseTemplate(dataSourceId: string): boolean {
  return !window.dispatchEvent(new CustomEvent<CreateDatabaseTemplateDetail>(CREATE_DATABASE_TEMPLATE_EVENT, {
    detail: { dataSourceId }, cancelable: true,
  }));
}

/** Hears requests for a new template of the data source; the first view that hears one takes it. */
export function listenForDatabaseTemplateRequests(dataSourceId: string, create: () => void): () => void {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<CreateDatabaseTemplateDetail>).detail;
    if (event.defaultPrevented || detail?.dataSourceId !== dataSourceId) return;
    event.preventDefault();
    create();
  };
  window.addEventListener(CREATE_DATABASE_TEMPLATE_EVENT, listener);
  return () => window.removeEventListener(CREATE_DATABASE_TEMPLATE_EVENT, listener);
}

/**
 * A database template its pages' empty bodies offer (Notion's "템플릿을
 * 선택하세요", seen 2026-10-12), named by its page's title or the name it was
 * made with; `titled` while its page has a title of its own; `hasContent`
 * while its body holds a block, which its icon shows, as its menu's does.
 */
export type DatabaseTemplateChoice = { id: string; name: string; titled?: boolean; icon: string; repeating: boolean; isDefault?: boolean; hasContent?: boolean };

const offered = new Map<string, readonly DatabaseTemplateChoice[]>();
const offerListeners = new Set<() => void>();
const NO_TEMPLATES: readonly DatabaseTemplateChoice[] = [];

/** A database's toolbar names its live templates while it shows; the pages opened from it list them. */
export function offerDatabaseTemplates(dataSourceId: string, templates: readonly DatabaseTemplateChoice[]): () => void {
  offered.set(dataSourceId, templates);
  for (const listener of offerListeners) listener();
  return () => {
    if (offered.get(dataSourceId) !== templates) return;
    offered.delete(dataSourceId);
    for (const listener of offerListeners) listener();
  };
}

export function useDatabaseTemplates(dataSourceId: string | undefined): readonly DatabaseTemplateChoice[] {
  return useSyncExternalStore(
    (listener) => { offerListeners.add(listener); return () => { offerListeners.delete(listener); }; },
    () => (dataSourceId ? offered.get(dataSourceId) ?? NO_TEMPLATES : NO_TEMPLATES),
  );
}

/**
 * What a database's toolbar does to its templates, which its pages' empty
 * bodies offer on a template's "···" (Notion's, seen 2026-10-13): its repeat,
 * editing, duplicating and deleting it.
 */
export type DatabaseTemplateActions = {
  template(templateId: string): DatabaseToolbarTemplateMetadataWire | undefined;
  /** The template a view's new pages start from (newPageTemplate, viewDefaultTemplate.ts), null for the empty page. */
  defaultFor?(view: { config: Record<string, unknown> }): { templateId?: string; orDefault?: true } | null;
  edit(templateId: string): void;
  duplicate(templateId: string): void;
  remove(templateId: string): void;
  setRepeat(templateId: string, repeat: DatabaseToolbarTemplateRepeatWire | null): void;
};

const actionsBySource = new Map<string, DatabaseTemplateActions>();

export function offerDatabaseTemplateActions(dataSourceId: string, actions: DatabaseTemplateActions): () => void {
  actionsBySource.set(dataSourceId, actions);
  return () => {
    if (actionsBySource.get(dataSourceId) === actions) actionsBySource.delete(dataSourceId);
  };
}

/** Read when used: the toolbar showing the database offers them. */
export function databaseTemplateActions(dataSourceId: string): DatabaseTemplateActions | undefined {
  return actionsBySource.get(dataSourceId);
}

// Rows made here from a template whose body the server copies into them (none
// copied here, rowTemplateHere.ts), till their page shows it: it offers
// neither the templates nor its starter meanwhile, as a page made from a
// template never offers them in Notion (seen 2026-11-16 in Chrome at 1440: a
// side peek from "새로 만들기 ⌄"'s template opened with its body, and no
// template list at any 15 ms sample; drawn here, the list stood ~100 ms over
// the empty body). "coming" while the server copies it, "drawn" once it has
// one for the page to draw, a moment at most.
const takingTemplate = new Map<string, "coming" | "drawn">();
const takingListeners = new Set<() => void>();
const takingChanged = () => { for (const listener of [...takingListeners]) listener(); };
const followTaking = (listener: () => void) => { takingListeners.add(listener); return () => { takingListeners.delete(listener); }; };
const DRAWN_AT_MOST_MS = 3_000;

function stopTaking(rowId: string) {
  if (takingTemplate.delete(rowId)) takingChanged();
}

/**
 * Notes that the row's body is its template's, coming from the server, till
 * `until` settles: true when it gave the row a body, which its page then
 * draws (useRowTakingTemplate).
 */
export function noteRowTakingTemplate(rowId: string, until: Promise<boolean | void>) {
  takingTemplate.set(rowId, "coming");
  takingChanged();
  void until.then((bodied) => {
    if (bodied !== true || takingTemplate.get(rowId) !== "coming") return stopTaking(rowId);
    takingTemplate.set(rowId, "drawn");
    takingChanged();
    window.setTimeout(() => { if (takingTemplate.get(rowId) === "drawn") stopTaking(rowId); }, DRAWN_AT_MOST_MS);
  }, () => stopTaking(rowId));
}

/** Whether the row's page waits for its template's body from the server (noteRowTakingTemplate), till `bodyEmpty` is no more. */
export function useRowTakingTemplate(rowId: string, bodyEmpty: boolean): boolean {
  const taking = useSyncExternalStore(followTaking, () => takingTemplate.get(rowId));
  useEffect(() => { if (taking === "drawn" && !bodyEmpty) stopTaking(rowId); }, [bodyEmpty, rowId, taking]);
  return taking !== undefined;
}

/** Sent when the server filled a row from a template, its id in `detail`: its open page reads it again (useRowPageRead, useRowPageReadAgain). */
export const ROW_FILLED_FROM_TEMPLATE_EVENT = "leafen:row-filled-from-template";

/**
 * Applies a template to a database page that already exists: values the page
 * leaves empty, an icon, its body after the page's — but for a row made here
 * that took the template's body copied here (rowTemplateHere.ts), `body: false`.
 * Its open page is told once the server filled it, whichever way the row was
 * made (2026-11-25: a task made in "내 작업", read as the server had made it,
 * before its template's icon, kept no icon in its side peek till opened again;
 * the sidebar's "+" fills its rows the same way).
 */
export async function applyDatabaseRowTemplate(session: Session, rowId: string, templateId: string, { body = true } = {}) {
  let applied: { copyingDatabases?: boolean } | undefined;
  try {
    applied = await request<{ copyingDatabases?: boolean } | undefined>(`/database-rows/${encodeURIComponent(rowId)}/apply-template`, {
      method: "POST", body: JSON.stringify({ templateId, timeZone: chosenTimeZone(), ...(body ? {} : { withoutBody: true }) }),
    }, session);
    window.dispatchEvent(new CustomEvent(ROW_FILLED_FROM_TEMPLATE_EVENT, { detail: rowId }));
  } finally {
    // The body's own databases come after the rest (lib/pageDatabaseCopies.ts).
    followPageDatabaseCopies(session, rowId, body && applied?.copyingDatabases === true);
  }
}
