import { api } from "@/api";
import type { GraphOperation } from "@/contentGraph";
import { graphWireErrorCodes } from "@/contentGraphWire.generated";
import { applyDatabaseStarter } from "@/databaseStarterApi";
import { i18next } from "@/i18n";
import { changeReached } from "@/components/editor/editorPagesMadeHere";
import { contentGraphOutboxOf } from "./contentGraphSend";
import { databaseStartFollowed } from "./databaseStartRefusal";
import { recoverFailedDatabaseCreation, startChosenDatabase, withPageCursorRetry, type DatabaseStarterCreationChoice, type PageDatabaseChoice } from "./databaseStarterCreation";
import { readGraphAfter } from "./graphReadAhead";
import { newId } from "./ids";
import { keptChangeRoutes } from "./keptChangeRoutes";
import { keptChangeTaken } from "./keptChangeTaken";
import { rememberFullPageDatabase } from "./inlineDatabaseAddress";
import type { NativeDatabaseStart } from "./nativePageLaneEntries";
import { pageCreationHere } from "./pageCreationService";
import { pageLeaving } from "./pageLeaving";
import { knownPageName } from "./pageNames";
import { showPageMadeHere, withServerMadePage, type PageMadeHereOutbox } from "./pageMadeHere";
import { showPageAhead } from "./pagesShownAhead";
import type { ProductStatusSink } from "./productStatus";
import type { Page, Session } from "@/types";

// A database a page starts as that only the server makes — a template's
// content, a linked view of an existing source's rows, a form — kept in the
// outbox at the press, behind the page's changes made before it, as the
// databases this device makes first are (fullPageDatabase.ts): it went from
// the window alone once those changes were saved and the page read again, two
// round trips after the press, and a reload or a closed tab meanwhile lost it,
// the page empty with its "시작하기" again (2026-11-18).

/**
 * The start as kept: what it starts as, the empty lines it takes away, or the
 * empty database a template replaces; `replacesPage` for one started on a
 * page of the person's, which goes to the trash as it was (database_start_page_trash.go).
 */
export type DatabaseStartAsked = { choice: PageDatabaseChoice; lineIds: readonly string[]; replace?: NativeDatabaseStart["replace"]; replacesPage?: boolean };

/**
 * Keeps the page's database start in the outbox and sends it in turn, behind
 * every older change kept for the page; resolves once the server took it,
 * or once it was kept for a server out of reach. Refused, it throws, and is
 * gone. The empty lines it takes away go as its kept change's operations
 * only to be shown gone meanwhile; the start itself goes by its own route
 * (sendKeptDatabaseStart). Without an outbox it goes at once.
 */
async function sendDatabaseStart(session: Session, pageId: string, asked: DatabaseStartAsked) {
  const start: NativeDatabaseStart = {
    choice: asked.choice as NativeDatabaseStart["choice"], lineIds: [...asked.lineIds], ...(asked.replace ? { replace: asked.replace } : {}),
    ...(asked.replacesPage ? { replacesPage: true as const } : {}),
  };
  const operations = start.lineIds.map((blockId): GraphOperation => ({ kind: "deleteBlock", deletion: { blockId } }));
  const mutationId = newId(), outbox = contentGraphOutboxOf(session);
  if (!outbox) return sendKeptDatabaseStart(session, { mutationId, pageId, start });
  const delivery = await outbox.admitAndSendGraph({ mutationId, operations, pageId, start });
  return delivery.state === "committed" ? delivery.result : delivery.state;
}

/**
 * Whether a page's database start reached the server, once it has, whichever
 * turn of the outbox sent it (changeReached). Refused, the page stays the
 * page it was, at its own address, before its sender says so
 * (databaseStartRefusal.ts).
 */
export function databaseStartMade(session: Session, pageId: string, asked: DatabaseStartAsked): Promise<boolean> {
  return databaseStartFollowed(pageId, changeReached(session, pageId, () => sendDatabaseStart(session, pageId, asked).then(() => true))
    .catch(() => false), Boolean(asked.replace));
}

/**
 * Sends a kept database start (the outbox's route for it): on the page as it
 * stands now — every change kept before it gone first, its cursor and graph
 * revision read again — under the kept change's own ID. Taken before, its
 * answer lost to a reload, a closed tab or the network, the server knows the
 * ID and it is not made twice. Moved on meanwhile by someone else, the page is
 * read again and it goes once more; refused, it throws. As the page goes
 * there is no time to read it: the start goes at once, as the server holds
 * the page (asHeld), named by the name this device knows it by.
 */
export async function sendKeptDatabaseStart(session: Session, input: { mutationId: string; pageId: string; start: NativeDatabaseStart },
  signal?: AbortSignal): Promise<unknown> {
  const { mutationId, pageId, start } = input;
  const choice = start.choice as PageDatabaseChoice;
  if (pageLeaving()) return startAsHeld(session, input, signal);
  for (let tries = 1; ; tries += 1) {
    const [page, graph] = await Promise.all([api.readPage(session, pageId, signal), api.contentGraph(session, pageId, null, { rows: false, signal })]);
    const baseGraphRevision = graph.snapshotRevision;
    try {
      if (!start.replace) {
        return await startChosenDatabase(session, pageId, choice, { lineIds: start.lineIds, basePageCursor: page.cursor, baseGraphRevision,
          title: page.title, mutationId, ...(start.replacesPage ? { replacesPage: true } : {}) }, signal);
      }
      if (choice.kind !== "template") throw Object.assign(new Error("Only a template takes an empty database's place."), { status: 400 });
      const { blockId, dataSourceId } = start.replace;
      return await withPageCursorRetry(session, pageId, signal, (cursor = page.cursor) => applyDatabaseStarter(session, pageId, {
        mutationId, ...choice.selection, language: i18next.resolvedLanguage ?? "en", replaceBlockId: blockId,
        ...(dataSourceId ? { replaceDataSourceId: dataSourceId } : {}), basePageCursor: cursor, baseGraphRevision,
      }, signal));
    } catch (error) {
      const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
      // Its ID is this start's alone: known to the server, it was taken
      // already (keptChangeTaken.ts). The page's database is then the one
      // just read, whose ID is its address, not the one this try made up.
      if (keptChangeTaken(error)) {
        const held = start.replace ? undefined : graph.items.find((item) => item.kind === "block" && item.value.type === "inline_database" && !item.value.parentId);
        if (held) rememberFullPageDatabase(held.value.id, pageId);
        return { taken: true };
      }
      if ((code !== graphWireErrorCodes.revisionConflict && code !== graphWireErrorCodes.pageCursorConflict) || tries >= 3 || signal?.aborted) throw error;
    }
  }
}

/**
 * A new page made a database from a template or an existing source — the
 * add window's templates and "기존 데이터 소스" — shown in the press's own
 * frame (user decision 2026-11-12: a press answers on the next frame; the
 * window stayed up while the server made the page and its database, some
 * 0.6–0.9 s on a NAS): the page is listed as the database it will be and
 * opens at once with its name over an empty body that takes nothing yet. It
 * is made here first and kept (showPageMadeHere), its database start kept
 * behind it (sendDatabaseStart): the server makes the page, then its
 * database, which only it can make, and across a reload too (2026-11-18:
 * both went from the window alone, a reload right after the press leaving no
 * page or a blank one). The page's read follows its database
 * (readGraphAfter) and the list is read again. Refused, it says why and the
 * list shows what the server holds; a page whose database couldn't be made
 * goes to the trash, as before. Null without an outbox: the server makes it first.
 */
export function startChosenDatabaseHere(session: Session, outbox: PageMadeHereOutbox | undefined, choice: DatabaseStarterCreationChoice, input: {
  siblings: readonly Page[]; teamspaceId?: string; title: string; workspaceId: string;
  announce: ProductStatusSink; isCurrent: () => boolean; refresh: () => Promise<unknown>;
  /** Changes the pages listed. */
  listed: (change: (pages: Page[]) => Page[]) => void;
}): Page | null {
  const { announce, isCurrent } = input;
  if (!outbox || !contentGraphOutboxOf(session)) return null;
  const creation = pageCreationHere(session, input);
  const page: Page = { ...creation.page, kind: "database" };
  let pageRefused = false;
  const pageMade = showPageMadeHere(outbox, session, creation, {
    announce, isCurrent,
    made: (made) => input.listed((pages) => withServerMadePage(pages, made)),
    refused: () => { pageRefused = true; void input.refresh().catch(() => undefined); },
  });
  const made = databaseStartMade(session, page.id, { choice, lineIds: [] });
  readGraphAfter(session, page.id, made);
  // A page list read meanwhile lists it as the database it will be.
  showPageAhead(page.id, { kind: "database", title: page.title }, made);
  void Promise.all([pageMade, made]).then(async ([shownMade, done]) => {
    // A page refused has said why already, kept here first or not.
    if (shownMade && !done && !pageRefused) {
      if (isCurrent()) announce(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
      await recoverFailedDatabaseCreation(session, page).catch(() => null);
    }
    await input.refresh();
  }).catch(() => undefined);
  if (isCurrent()) input.listed((pages) => [...pages.filter(({ id }) => id !== page.id), page]);
  return page;
}

// A start sent as its page goes: one request, the page and its body as the
// server holds them (content_graph_as_held.go) — read first, the reads never
// came back and nothing went until Leafen was opened again (2026-11-27).
function startAsHeld(session: Session, { mutationId, pageId, start }: { mutationId: string; pageId: string; start: NativeDatabaseStart },
  signal?: AbortSignal) {
  const choice = start.choice as PageDatabaseChoice;
  const held = { basePageCursor: 0, baseGraphRevision: 0, mutationId, asHeld: true };
  if (!start.replace) {
    return startChosenDatabase(session, pageId, choice, { ...held, lineIds: start.lineIds, title: knownPageName(pageId)?.title ?? "",
      ...(start.replacesPage ? { replacesPage: true } : {}) }, signal);
  }
  if (choice.kind !== "template") return Promise.reject(Object.assign(new Error("Only a template takes an empty database's place."), { status: 400 }));
  const { blockId, dataSourceId } = start.replace;
  return applyDatabaseStarter(session, pageId, { ...held, ...choice.selection, language: i18next.resolvedLanguage ?? "en", replaceBlockId: blockId,
    ...(dataSourceId ? { replaceDataSourceId: dataSourceId } : {}) }, signal);
}

// At hand for the outbox as the page goes (keptChangeRoutes.ts).
keptChangeRoutes.start = sendKeptDatabaseStart;
