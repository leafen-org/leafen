import { linkedDatabaseName } from "@/components/editor/databaseTitles";
import { createLinkedInlineDatabase, type ContentGraph } from "@/contentGraph";
import { i18next } from "@/i18n";
import { recoverFailedDatabaseCreation } from "./databaseStarterCreation";
import { readGraphAfter } from "./graphReadAhead";
import { newId } from "./ids";
import { createNativePage, pageCreationHere } from "./pageCreationService";
import { showPageMadeHere, withServerMadePage, type PageMadeHereOutbox } from "./pageMadeHere";
import { sendContentGraphChangeNow } from "./contentGraphSend";
import type { ProductStatusSink } from "./productStatus";
import { showPageAhead } from "./pagesShownAhead";
import type { Page, Session } from "@/types";
import { serverAway } from "./serverAnswers";

type SavedViewSource = { containerId: string; dataSourceId: string; title: string };

/** The database a saved view's page is: the source linked as a table with the view's filters, and the page's name. */
function savedViewDatabase(pageId: string, source: SavedViewSource, config: Record<string, unknown>) {
  const created = createLinkedInlineDatabase([], pageId, {
    containerId: source.containerId, dataSourceId: source.dataSourceId, viewType: "table",
    viewName: "", viewConfig: config,
  });
  const pageTitle = linkedDatabaseName("", source.title);
  created.block.content.generatedPageTitle = pageTitle;
  return { created, pageTitle };
}

/**
 * Saves a view on a page, as Notion's group preview "다음에 보기로 저장"
 * does (seen 2026-10-11): a new page under the chosen one, named
 * "〈database〉 보기" ("새 데이터베이스 보기" for an untitled one, seen
 * 2026-11-11), is the database linked as a table with the view's filters —
 * a name it has until it is given its own, with no large title meanwhile
 * (seen 2026-10-15). The table has no name, so each reader sees its
 * layout's name in their own language (seen 2026-11-11: an empty name field,
 * its tab "표"). A page whose database couldn't be made goes to the trash.
 * Returns the new page.
 */
export async function createSavedViewPage(session: Session, input: {
  workspaceId: string;
  parentPageId: string;
  source: SavedViewSource;
  config: Record<string, unknown>;
}) {
  const page = await createNativePage(session, { parentId: input.parentPageId, title: "", workspaceId: input.workspaceId });
  await makeSavedView(session, page, input.source, input.config);
  return page;
}

// The page made, its database and name go in one change, on the page as made.
async function makeSavedView(session: Session, page: Page, source: SavedViewSource, config: Record<string, unknown>,
  made = savedViewDatabase(page.id, source, config)) {
  try {
    await sendContentGraphChangeNow(session, page.id, {
      mutationId: newId(), basePageCursor: page.cursor, baseGraphRevision: 0, pageKind: "database",
      pageTitle: made.pageTitle, operations: made.created.operations,
    });
  } catch (error) {
    await recoverFailedDatabaseCreation(session, page).catch(() => null);
    throw error;
  }
}

/**
 * The same, shown at once (user decision 2026-11-12: a press answers on the
 * next frame — the page opened once the server had made it, made its
 * database and the page list was read again, ~1 s on a NAS): the page is made
 * on this device first, listed under the chosen page as the database it
 * will be, and opens from what the view showed — the source's properties and
 * rows as the open page holds them. The server makes the page in turn
 * (pageMadeHere), then its database and name in one change; the page's read
 * waits for that (readGraphAfter), and the list is read again. Refused, it
 * says so, the list shows what the server holds and `refused` is told.
 * Null where this device can't show it — no outbox, a database or wiki page
 * chosen, a source the open page doesn't hold: the server makes it first.
 */
export function showSavedViewPageHere(session: Session, input: {
  workspaceId: string;
  parentPageId: string;
  source: SavedViewSource;
  config: Record<string, unknown>;
  outbox: PageMadeHereOutbox | undefined;
  pages: readonly Page[];
  /** The open page's graph, which holds the source the view shows. */
  shown: ContentGraph | null;
  onPageChange(change: (pages: readonly Page[]) => Page[]): void;
  refresh(): Promise<unknown>;
  announce: ProductStatusSink;
  refused(): void;
}): Page | null {
  const { outbox, source } = input;
  const parent = input.pages.find((page) => page.id === input.parentPageId);
  const dataSource = input.shown?.dataSources.find(({ id }) => id === source.dataSourceId);
  if (!outbox || serverAway() || !parent || parent.kind === "database" || parent.isWiki || parent.wikiRootId
    || !dataSource || !input.shown) return null;
  const creation = pageCreationHere(session, { siblings: input.pages, parentId: parent.id, teamspaceId: parent.teamspaceId, title: "",
    workspaceId: input.workspaceId || parent.workspaceId });
  const made = savedViewDatabase(creation.pageId, source, input.config);
  const page: Page = { ...creation.page, teamspaceId: parent.teamspaceId ?? "", kind: "database", title: made.pageTitle };
  const graph: ContentGraph = {
    blocks: [made.created.block], databaseContainers: [made.created.container], dataSources: [dataSource],
    views: [made.created.view], properties: input.shown.properties.filter(({ dataSourceId }) => dataSourceId === dataSource.id),
    rows: input.shown.rows.filter(({ dataSourceId }) => dataSourceId === dataSource.id),
  };
  const copy = { bootstrapRevision: 0, graphRevision: 0, graph, canEdit: true, canSuggest: false };
  input.onPageChange((pages) => [...pages.filter(({ id }) => id !== page.id), page]);
  const fail = () => {
    input.announce(i18next.t("databaseView:couldntSaveView"), "error");
    input.refused();
    void input.refresh().catch(() => undefined);
  };
  let answer: (made: Page | null) => void = () => {};
  const serverPage = new Promise<Page | null>((resolve) => { answer = resolve; });
  const done = serverPage.then(async (server) => {
    if (!server) return false;
    await makeSavedView(session, server, source, input.config, made);
    return true;
  }).catch(() => false);
  // Its read waits for its database, not the empty page the server makes first,
  // and a page list read meanwhile lists it as the database it will be.
  readGraphAfter(session, page.id, done);
  showPageAhead(page.id, { kind: "database", title: made.pageTitle }, done);
  void done.then((saved) => {
    if (!saved) { fail(); return; }
    void input.refresh().catch(() => undefined);
  });
  // It draws in the frame it opens in, from what the view showed, its copy
  // here following; what asks the server about it waits until the server
  // made it (showPageMadeHere — 2026-11-13: it asked before it was kept).
  void showPageMadeHere(outbox, session, creation, {
    announce: input.announce,
    made: (server) => {
      input.onPageChange((pages) => withServerMadePage(pages, server));
      answer(server);
    },
    refused: () => answer(null),
  }, copy);
  return page;
}
