import { api } from "@/api";
import { emptyContentGraph } from "@/contentGraph";
import { graphWireErrorCodes } from "@/contentGraphWire.generated";
import { applyDatabaseStarter } from "@/databaseStarterApi";
import { i18next } from "@/i18n";
import { changeReached } from "@/components/editor/editorPagesMadeHere";
import { databaseStartFollowed } from "./databaseStartRefusal";
import { newId } from "./ids";
import { sendContentGraphChange } from "./contentGraphSend";
import { pageDatabaseStartHere, startFullPageDatabase, startLinkedFullPageDatabase, type FullPageDatabaseStart } from "./fullPageDatabase";
import type { DatabaseContainerSource } from "./databaseContainerPicker";
import type { DatabaseStarterSelection } from "./databaseStarterCatalog";
import { readGraphAfter } from "./graphReadAhead";
import { pageCreationHere } from "./pageCreationService";
import { showPageMadeHere, withServerMadePage, type PageMadeHereOutbox } from "./pageMadeHere";
import { showPageAhead } from "./pagesShownAhead";
import type { ProductStatusSink } from "./productStatus";
import type { Page, Session } from "@/types";

export type DatabaseStarterCreationChoice = { kind: "template"; selection: DatabaseStarterSelection } | { kind: "source"; source: DatabaseContainerSource };
export type AddRootPageDetails = { teamspaceId?: string; title?: string; databaseChoice?: DatabaseStarterCreationChoice };
/** What a page the server holds starts as on the server: a template's or a source's database, or a view's (a form's). */
export type PageDatabaseChoice = DatabaseStarterCreationChoice | { kind: "view"; view: FullPageDatabaseStart };

export async function startCreatedDatabasePage(session: Session, page: Page, choice?: DatabaseStarterCreationChoice) {
  if (!choice) return startFullPageDatabase(session, page.id, page.title, [], "blank");
  return startChosenDatabase(session, page.id, choice, { lineIds: [], basePageCursor: page.cursor, baseGraphRevision: 0, title: page.title });
}

/**
 * Starts the chosen database on a page the server holds, as one change the
 * server makes, fenced by the page's cursor and graph revision: a
 * template's content and a source's rows only it can make. A page renamed
 * meanwhile — from its top bar or the sidebar while its database was being
 * made — moved its cursor on: it is read again and the change sent once
 * more on it, the name kept, where the database failed and a new page went
 * to the trash (2026-11-13). Sent from the outbox, it goes under the kept
 * change's ID (`mutationId`, databaseStartKept.ts). Started on a page of
 * the person's (`replacesPage`), the database takes the page's place and the
 * page as it was goes to the trash, as Notion's (database_start_page_trash.go).
 * One sent as the page goes, with no time to read the page, goes as the
 * server holds it (`asHeld`, content_graph_as_held.go).
 */
export async function startChosenDatabase(session: Session, pageId: string, choice: PageDatabaseChoice,
  base: { lineIds: readonly string[]; basePageCursor: number; baseGraphRevision: number; title: string; mutationId?: string; replacesPage?: boolean;
    asHeld?: boolean },
  signal?: AbortSignal) {
  const send = (basePageCursor: number, title: string) => {
    const fence = { basePageCursor, baseGraphRevision: base.baseGraphRevision, ...(base.mutationId ? { mutationId: base.mutationId } : {}),
      ...(base.replacesPage ? { replacesPage: true } : {}), ...(base.asHeld ? { asHeld: true } : {}) };
    if (choice.kind === "template") return applyDatabaseStarter(session, pageId, { mutationId: newId(), ...choice.selection,
      language: i18next.resolvedLanguage ?? "en", ...fence }, signal).then(({ receipt }) => receipt);
    if (choice.kind === "source") return startLinkedFullPageDatabase(session, pageId, base.lineIds, choice.source, fence, signal);
    return startFullPageDatabase(session, pageId, title, base.lineIds, choice.view, signal, fence);
  };
  return withPageCursorRetry(session, pageId, signal, (cursor, title) => send(cursor ?? base.basePageCursor, title ?? base.title));
}

/**
 * Runs a change fenced by a page's cursor; refused because the page moved
 * on (renamed meanwhile), runs it once more on the page as read again.
 */
export async function withPageCursorRetry<T>(session: Session, pageId: string, signal: AbortSignal | undefined,
  send: (cursor?: number, title?: string) => Promise<T>): Promise<T> {
  try {
    return await send();
  } catch (error) {
    const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
    if (code !== graphWireErrorCodes.pageCursorConflict || signal?.aborted) throw error;
    const current = await api.readPage(session, pageId, signal);
    return send(current.cursor, current.title);
  }
}

/**
 * A new page made a blank full-page database on this device first — the add
 * window's "빈 데이터베이스", a sidebar shortcut's new database — as the
 * empty page's own "데이터베이스" is (user decision 2026-11-12: a press
 * answers on the next frame; it waited for the server to make the page,
 * start its database and read the page list again, 777 ms on a NAS, and then
 * for the page to be kept here): the page is listed and shows in the press's
 * own frame as the database it will be, drawn from the database as made, and
 * is kept here behind it (showPageMadeHere); the database goes to the server
 * as the page's own change, behind the page's making and ahead of anything
 * done in it, kept with the page's becoming a full-page database (one
 * change, pageKind: a second write after it was lost to a reload in between,
 * 2026-11-18), and the list is read again. Refused, the list shows what the
 * server holds and says why. Null without an outbox: the server makes it first.
 */
export function startRootDatabaseHere(session: Session, outbox: PageMadeHereOutbox | undefined, input: {
  siblings: readonly Page[]; teamspaceId?: string; title: string; workspaceId: string;
  announce: ProductStatusSink; isCurrent: () => boolean; refresh: () => Promise<unknown>;
  /** Changes the pages listed. */
  listed: (change: (pages: Page[]) => Page[]) => void;
}): Page | null {
  const { announce, isCurrent } = input;
  const creation = pageCreationHere(session, input);
  const started = pageDatabaseStartHere(creation.pageId, creation.page.title, "blank", []);
  if (!outbox || !started) return null;
  const page: Page = { ...creation.page, kind: "database" };
  const shown = { bootstrapRevision: 0, graphRevision: 0, graph: started.show(emptyContentGraph), canEdit: true, canSuggest: false };
  let pageRefused = false;
  const pageMade = showPageMadeHere(outbox, session, creation, {
    announce, isCurrent,
    made: (made) => input.listed((pages) => withServerMadePage(pages, made)),
    refused: () => { pageRefused = true; void input.refresh().catch(() => undefined); },
  }, shown);
  const made = databaseStartFollowed(page.id, changeReached(session, page.id, () => sendContentGraphChange(session, page.id, started.operations,
    { pageKind: "database" }).then(() => true)).catch(() => false));
  // Its body is read with its database, not as the empty page the server holds in between (which drew nothing till a reload).
  readGraphAfter(session, page.id, made);
  // A page list read meanwhile lists it as the database it will be.
  showPageAhead(page.id, { kind: "database" }, made);
  void Promise.all([pageMade, made]).then(async ([shownMade, done]) => {
    // A page refused has said why already, kept here first or not.
    if (shownMade && !done && !pageRefused && isCurrent()) announce(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
    await input.refresh();
  }).catch(() => undefined);
  if (isCurrent()) input.listed((pages) => [...pages.filter(({ id }) => id !== page.id), page]);
  return page;
}

/** Keep a failed fresh page recoverable, using its latest metadata cursor. */
export async function recoverFailedDatabaseCreation(session: Session, page: Page) {
  const current = await api.readPage(session, page.id);
  // A lost success response must not discard a database that was committed.
  if (current.kind === "database") return current;
  try {
    await api.updatePageMetadata(session, page.id, { title: current.title, parentId: current.parentId, baseCursor: current.cursor, mutationId: newId(), inTrash: true });
  } catch (error) {
    // The database may have committed between the read and the cleanup's
    // cursor fence. A lost cleanup response may already have put it in trash.
    const latest = await api.readPage(session, page.id);
    if (latest.kind === "database" && !latest.inTrash) return latest;
    if (!latest.inTrash) throw error;
  }
  return null;
}
