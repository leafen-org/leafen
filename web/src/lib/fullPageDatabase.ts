import { readDatabaseToolbarConfig } from "@/databaseToolbarConfigApi";
import {
  heldDefaultTemplate, madeFromTemplate, noteDefaultRowTemplate, rowTemplateToMakeFrom, rowTemplateValues, rowWithTemplateIcon,
} from "@/lib/rowTemplateHere";
import {
  compareGraphOrder,
  createLinkedInlineDatabase,
  type ContentGraph,
  type GraphBlock,
  type GraphOperation,
  type GraphRow,
  type GraphViewType,
  type InlineDatabaseNames,
} from "@/contentGraph";
import { i18next } from "@/i18n";
import { collectAuthenticatedContentGraph, collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import type { BlockMoveService } from "@/lib/blockMoveService";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { createPageStarterForm } from "@/lib/pageStarterDatabase";
import { databaseStartFollowed } from "@/lib/databaseStartRefusal";
import { beginLinkedViewMaking, linkedStarterViewConfig } from "@/lib/databaseStarterLinked";
import { markFreshForm } from "@/lib/freshDatabases";
import { newId } from "@/lib/ids";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { createNativePage, pageCreationHere, sendPageCreationHere } from "@/lib/pageCreationService";
import type { NativePageCreation } from "@/lib/nativePageLaneEntries";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsShown } from "@/lib/rowsShownAtPress";
import { inlineDatabaseDrawn } from "@/lib/databaseTurnedBackHere";
import { forgetShownGraph, holdShownGraph } from "@/lib/shownGraphs";
import { showPageAhead } from "@/lib/pagesShownAhead";
import { newRowStart } from "@/lib/newRowStart";
import { createStartedDatabase } from "@/lib/databaseViewStart";
import { linkedDatabaseName } from "@/components/editor/databaseTitles";
import { changeReached, pagesInBodyOrder, positionAmong, type PagesMadeHere } from "@/components/editor/editorPagesMadeHere";
import type { EditorMoveContext } from "@/components/editor/editorRuntimeTypes";
import { requestDatabaseStartPanel } from "@/lib/databaseStartPanel";
import { markFreshDatabase } from "@/lib/freshDatabases";
import { openPageBeforeMade } from "@/lib/pageMadeHere";
import { linkedDatabasePresentation } from "@/lib/inlineDatabaseFullPage";
import { rememberFullPageDatabase, rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import { contentGraphOutboxOf, sendContentGraphChange, sendContentGraphChangeNow } from "@/lib/contentGraphSend";
import { readGraphAfter } from "@/lib/graphReadAhead";
import { localFirstProductCache } from "@/lib/localFirstProductCache";
import { makeRowHere, rowPageMadeHere, tellRowMadeHere } from "@/lib/rowMadeHere";
import { serverIdentity } from "@/lib/serverOrigin";
import { answeredUnlessAway } from "@/lib/serverReach";
import type { Page, Session } from "@/types";
import type { WikiDatabaseNames } from "@/wikiWire.generated";

type PlacedBlock = { id: string; parentId: string | null };

/** The name a new database's title property starts with, in the reader's language. */
export function newDatabaseNames(): InlineDatabaseNames {
  return {
    name: i18next.t("common:generated.propertyNames.name"),
  };
}

/** A new wiki's database names, in the language the person making it reads. */
export function newWikiDatabaseNames(): WikiDatabaseNames {
  const name = (key: keyof WikiDatabaseNames) => i18next.t(`common:generated.wikiNames.${key}`);
  return {
    page: name("page"),
    owner: name("owner"),
    tags: name("tags"),
    verification: name("verification"),
    lastEditedTime: name("lastEditedTime"),
    allPages: name("allPages"),
    pagesIOwn: name("pagesIOwn"),
    parentItem: name("parentItem"),
    subItems: name("subItems"),
  };
}

/** The view a full-page database starts as, or "blank": Notion's "빈 데이터베이스". */
export type FullPageDatabaseStart = GraphViewType | "blank";

/**
 * Makes a page a full-page database, as a page in Notion can start as one:
 * one change removes the page's empty lines, gives it its database, named
 * after the page and first shown as the chosen view, and makes the page a
 * full-page database. "빈 데이터베이스" starts as a table that doesn't wrap,
 * where one started as a view or from "/" wraps (Notion's, seen 2026-11-11).
 */
export function startFullPageDatabase(
  session: Session,
  pageId: string,
  title: string,
  lineIds: readonly string[],
  start?: FullPageDatabaseStart,
  signal?: AbortSignal,
  base?: { basePageCursor: number; baseGraphRevision: number; mutationId?: string; replacesPage?: boolean; asHeld?: boolean },
) {
  const { database, operations: made, pageTitle } = fullPageDatabaseStart(pageId, title, start);
  const operations: GraphOperation[] = [
    ...lineIds.map((blockId): GraphOperation => ({ kind: "deleteBlock", deletion: { blockId } })),
    ...made,
  ];
  return sendContentGraphChangeNow(session, pageId, { mutationId: newId(), ...base, operations, pageKind: "database",
    ...(pageTitle ? { pageTitle } : {}) }, signal)
    .then((result) => Object.assign(result, {
      databaseBlockId: database?.block.id ?? "",
      dataSourceId: database?.dataSource.id ?? "",
      databaseProperties: database?.properties.length ?? 0,
    }));
}

/**
 * A page starting as a full-page database on this device first (user
 * decision 2026-11-12: a press answers on the next frame — the empty page's
 * "데이터베이스" and its views waited for the page to be read, the database
 * made and the page list read again, ~0.9 s on a NAS): the change that
 * removes its empty lines and makes its database, and its graph so changed,
 * to show while the change goes to the server, which makes the page a
 * full-page database with it (sendContentGraphChange's pageKind). Null for
 * a form, which the server starts first.
 */
export function pageDatabaseStartHere(pageId: string, title: string, start: FullPageDatabaseStart, lineIds: readonly string[]) {
  const { database, operations: made } = fullPageDatabaseStart(pageId, title, start);
  if (!database) return null;
  const gone = new Set(lineIds);
  const operations: GraphOperation[] = [
    ...lineIds.map((blockId): GraphOperation => ({ kind: "deleteBlock", deletion: { blockId } })),
    ...made,
  ];
  const show = (graph: ContentGraph): ContentGraph => ({
    ...graph,
    blocks: [...graph.blocks.filter(({ id }) => !gone.has(id)), database.block],
    databaseContainers: [...graph.databaseContainers, database.container],
    dataSources: [...graph.dataSources, database.dataSource],
    views: [...graph.views, ...database.views],
    properties: [...graph.properties, ...database.properties],
    rows: [...graph.rows, ...database.rows],
  });
  return { operations, show };
}

/** What starts a page as a full-page database: its database (none for a form), the change making it and the page's name for a form. */
function fullPageDatabaseStart(pageId: string, title: string, start?: FullPageDatabaseStart) {
  const view = start === "blank" ? "table" : start;
  const database = view === "form" ? null : createStartedDatabase(
    [], title.trim(), pageId, newDatabaseNames(),
    view ? { type: view, name: "", ...(start === "blank" ? { config: { wrap: false } } : {}) } : undefined,
  );
  // A form started on an untitled page names its page and its database "새
  // 폼" alike, as Notion's (seen 2026-11-07 and 2026-11-08, the sidebar too).
  const formTitle = database ? "" : title.trim() || i18next.t("editor:starter.formDefaults.newForm");
  const form = database ? null : createPageStarterForm(pageId, formTitle);
  const created = database ?? form!;
  // Its database's ID is its address from now on (inlineDatabaseAddress.ts).
  if (database) rememberFullPageDatabase(database.block.id, pageId);
  // Its title takes what is typed first, as a form made as a new view does.
  if (form) markFreshForm(form.formViewId);
  return { database, operations: created.operations, pageTitle: form && !title.trim() ? formTitle : "" };
}

/**
 * A full-page database a line becomes ("/데이터베이스 - 전체 페이지")
 * shown at once, as Notion opens it (user decision 2026-11-12: a press
 * answers on the next frame — it waited for the line's change, the
 * database's start and the page list, 870 ms on a NAS): listed under its
 * page as a database and opening from what this device keeps, the database
 * as made with its start panel. The server makes the page with the line's
 * change; the database, kept in the outbox behind it at once with the page's
 * becoming a full-page database (one change, pageKind), goes to it once that
 * change has made the page; its page is read with that (readGraphAfter) and
 * the page list is read again. Its becoming one went as a second write the
 * outbox didn't keep, and a reload or a closed tab before it went left a
 * page holding its database, its address not found till the outbox sent the
 * database (2026-11-18). A form starts on the
 * server first (null). Sent beside the line's change, the database took the
 * outbox's turn first, sent that change and then waited in that turn for its
 * page to be made, which the line's own send, waiting for the turn, was to tell: six seconds till the turn gave up
 * (2026-11-14, NAS). It draws from what it was made with, held in this tab (shownGraphs.ts), and every read
 * of its page waits till the server holds the database (readGraphAfter): read once the page alone was made,
 * it stayed blank — no name, table or start panel — and drawn from this device's copy alone it opened
 * blank while the copy was being written (2026-11-16).
 */
export function showFullPageDatabaseHere(input: {
  session: Session; context: EditorMoveContext; parentId: string; workspaceId: string; pageId: string;
  start: { title: string; view?: GraphViewType }; flush: () => Promise<boolean>; refresh: () => Promise<unknown>;
}): PagesMadeHere | null {
  const { session, context, pageId } = input;
  const { database, operations } = input.start.view === "form" ? { database: null, operations: [] }
    : fullPageDatabaseStart(pageId, input.start.title, input.start.view);
  if (!database) return null;
  const parent = context.pages.find((page) => page.id === input.parentId);
  const workspaceId = input.workspaceId || parent?.workspaceId || "";
  const page: Page = { ...pageCreationHere(session, { siblings: context.pages, parentId: input.parentId, title: input.start.title, workspaceId }).page,
    id: pageId, teamspaceId: parent?.teamspaceId ?? "", kind: "database" };
  const reached = changeReached(session, input.parentId, input.flush);
  const mutationId = newId();
  void contentGraphOutboxOf(session)?.keepGraph({ mutationId, operations, pageId, pageKind: "database" }).catch(() => undefined);
  // The line refused made no page: the database goes too, refused, and leaves the outbox.
  const started = databaseStartFollowed(pageId, reached.then(async (made) => {
    const saved = await changeReached(session, pageId, () => sendContentGraphChange(session, pageId, operations, { mutationId, pageKind: "database" })
      .then(() => true));
    return made && saved;
  }).catch(() => false));
  readGraphAfter(session, pageId, started);
  // A page list read meanwhile lists it as the database it will be.
  showPageAhead(pageId, { kind: "database" }, started);
  void started.then((made) => {
    if (!made) context.onStatus?.(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
    return input.refresh();
  }).catch(() => {});
  // Its page opens with Notion's "새 데이터베이스" panel, this once, its toolbar only settings and "새로 만들기" till it first changes.
  requestDatabaseStartPanel(database.block.id);
  markFreshDatabase(database.dataSource.id, database.properties.length);
  const graph: ContentGraph = { blocks: [database.block], databaseContainers: [database.container], dataSources: [database.dataSource],
    views: database.views, properties: database.properties, rows: database.rows };
  const value = { bootstrapRevision: 0, graphRevision: 0, graph, canEdit: true, canSuggest: false };
  holdShownGraph(session.user.id, pageId, value);
  const opening = openPageBeforeMade(session, page, reached, false).then(() => localFirstProductCache.graph.writeCache({
    complete: true, key: pageId, principalId: session.user.id, serverKey: serverIdentity(), version: 1, value })).then(() => {}, () => {});
  context.onPageChange(page);
  return { reached, opening: new Map([[pageId, opening]]) };
}

/** A linked full-page database owns its view and keeps the chosen source. */
export function startLinkedFullPageDatabase(
  session: Session, pageId: string, lineIds: readonly string[], source: DatabaseContainerSource,
  base: { basePageCursor: number; baseGraphRevision: number; mutationId?: string; replacesPage?: boolean; asHeld?: boolean }, signal?: AbortSignal,
) {
  const created = createLinkedInlineDatabase([], pageId, {
    containerId: source.id, dataSourceId: source.dataSourceId, viewType: "table",
    // Unnamed as stored: "새 보기" only where it asks for its view (askingLinkedViewHere).
    viewName: "", viewConfig: linkedStarterViewConfig(source),
  });
  beginLinkedViewMaking(created.view.id);
  rememberFullPageDatabase(created.block.id, pageId);
  const generatedTitle = i18next.t("editor:starter.linkedTitle", { title: source.title });
  created.block.content.generatedPageTitle = generatedTitle;
  return sendContentGraphChangeNow(session, pageId, {
    mutationId: newId(), ...base, pageKind: "database",
    pageTitle: generatedTitle,
    operations: [...lineIds.map((blockId): GraphOperation => ({ kind: "deleteBlock", deletion: { blockId } })),
      ...created.operations],
  }, signal);
}

/**
 * Adds an empty row to a full-page database, as Notion's sidebar adds a page
 * to a database: the row comes after the database's other top rows.
 * Returns the row's ID, which is its page's.
 */
export function addFullPageDatabaseRow(session: Session, database: Pick<Page, "id" | "workspaceId">, signal?: AbortSignal) {
  return addDatabaseBlockRow(session, database.id, database.workspaceId, (block) => !block.parentId, signal);
}

/**
 * Adds an empty row, the same way, to an inline database of a page, by its
 * block: what Notion's sidebar "+" on the database's row does (seen
 * 2026-11-02).
 */
export function addInlineDatabaseRow(session: Session, pageId: string, blockId: string, workspaceId: string, signal?: AbortSignal) {
  return addDatabaseBlockRow(session, pageId, workspaceId, (block) => block.id === blockId, signal);
}

/**
 * The row is made on this device first, as a view's are (rowMadeHere.ts):
 * from the database as this device keeps it, else as the server reads it;
 * its page opens at once from what is kept here, and it goes to the server
 * behind, the server filling it from the database's default template as it
 * makes it (2026-11-12: on a stalled network the sidebar's "+" waited for the server
 * to read the database and to make the row, and its page never opened).
 * One this device never kept can't be made while the server is out of reach,
 * its properties unknown here: the read fails as unanswered at once when the
 * app knows it (answeredUnlessAway), which the "+" tells as offline (addFailedNotice).
 */
async function addDatabaseBlockRow(session: Session, pageId: string, workspaceId: string,
  isDatabase: (block: ContentGraph["blocks"][number]) => boolean, signal?: AbortSignal) {
  const kept = await localFirstProductCache.graph.readCache({ key: pageId, principalId: session.user.id, serverKey: serverIdentity() })
    .catch(() => null);
  const current = kept?.value.graph
    ?? (await answeredUnlessAway(collectAuthenticatedContentGraph(session, pageId, null, () => !signal?.aborted)))?.graph ?? null;
  const block = current?.blocks.find((candidate) => candidate.type === "inline_database" && isDatabase(candidate));
  const dataSourceId = typeof block?.content.dataSourceId === "string" ? block.content.dataSourceId : "";
  const title = current?.properties.find((property) => property.dataSourceId === dataSourceId && property.type === "title");
  if (!current || !dataSourceId || !title) throw new Error("The database could not be read.");
  const last = current.rows
    .filter((row) => row.dataSourceId === dataSourceId && !row.subitemParentId)
    .sort(compareGraphOrder)
    .at(-1);
  const start = newRowStart(current.dataSources.find(({ id }) => id === dataSourceId), title.id, current.properties);
  // It starts from the database's default template, as Notion's sidebar "+"
  // does (seen 2026-11-05), made with its values as held here (rowTemplateHere.ts).
  const template = heldDefaultTemplate(session, dataSourceId);
  const templateValues = Object.entries(template ? rowTemplateValues(session, template.template, current.properties, dataSourceId) : {})
    .flatMap(([id, value]) => {
      const property = current.properties.find((candidate) => candidate.id === id);
      return property ? [[id, { type: property.type, value }] as const] : [];
    });
  const row: GraphRow = {
    id: newId(),
    dataSourceId,
    subitemParentId: null,
    position: graphPositionBetween(last?.position, undefined),
    // Its statuses read as their default without keeping it, as Notion's new page (seen 2026-11-08).
    values: { ...start ? start.values : { [title.id]: { type: "title" as const, value: "" } }, ...Object.fromEntries(templateValues) },
  };
  const sourcePageId = current.dataSources.find(({ id }) => id === dataSourceId)?.pageId ?? pageId;
  // Its making names the template it starts from, which the server fills it from as it makes it.
  const making = rowTemplateToMakeFrom(session, template?.templateId, { isDefault: true });
  const { pageReady } = makeRowHere(row, session.user.id,
    () => sendContentGraphChange(session, pageId, [{ kind: "upsertRow", row }], { signal }).then(() => true), () => {}, making.template);
  const made = madeFromTemplate(session, row.id, making);
  const shown = rowWithTemplateIcon(row, template?.template);
  rowPageMadeHere(session, shown, current.properties, sourcePageId, workspaceId);
  // The page holding the database, open, shows it at once; its page opens over it.
  tellRowMadeHere(pageId, shown);
  void readDefaultTemplateWhenMade(session, sourcePageId, dataSourceId, made);
  await Promise.all([pageReady, made.shown]);
  return row.id;
}

/**
 * A row made here from its database's default template (rowTemplateToMakeFrom
 * isDefault) is filled by the server as it makes it: once it is, the default
 * the server names is read ahead for the next row, unless the row took it
 * copied here. Settles then, or once it won't be.
 */
export function readDefaultTemplateWhenMade(session: Session, sourcePageId: string, dataSourceId: string,
  made: { filled: Promise<boolean>; copied: boolean }): Promise<void> {
  return made.filled.then(async (filled) => {
    if (!filled || made.copied) return;
    const { templates } = await readDatabaseToolbarConfig(session, sourcePageId, dataSourceId);
    noteDefaultRowTemplate(session, dataSourceId, templates.find(({ isDefault }) => isDefault));
  }).catch(() => undefined);
}

/** The name of the page an inline database turns into: its own, or a linked one's "〈source〉 보기". */
function turnedPageTitle(database: Partial<GraphBlock>, title: string, sourceTitle = "") {
  const linked = database.content?.linkedDatabaseSource === true ? linkedDatabasePresentation(database as GraphBlock) : null;
  return linked ? linkedDatabaseName(linked.title, sourceTitle) : title.trim();
}

/**
 * An inline database turned into a page, shown so at once (user decision
 * 2026-11-12: a press answers on the next frame — the turn showed once its
 * four server steps and two reads were done, ~2 s on a NAS): the page made
 * here stands in its place, a page block chosen, listed under the page as a
 * full-page database, and opens from the database as the page shows it
 * (its reads waiting until the server made it). The open page shows the
 * change until `release` (as moved, or refused: the database back in its
 * place); the turn goes to the server with `made` (turnInlineDatabaseIntoPage).
 */
export function showInlineDatabaseTurnedHere(input: {
  session: Session; context: EditorMoveContext; store: PageGraphStore; pageId: string; workspaceId: string;
  database: GraphBlock; title: string; sourceTitle?: string;
}) {
  const { session, context, database, pageId } = input;
  const graph = input.store.get();
  if (!graph.blocks.some(({ id }) => id === database.id)
    || graph.databaseContainers.find(({ blockId }) => blockId === database.id)?.pageId !== pageId) return null;
  const parent = context.pages.find((page) => page.id === pageId);
  const title = turnedPageTitle(database, input.title, input.sourceTitle);
  const made = pageCreationHere(session, { siblings: context.pages, parentId: pageId, title,
    workspaceId: input.workspaceId || parent?.workspaceId || "" });
  const pageBlock: GraphBlock = { id: made.pageId, parentId: database.parentId, afterId: database.afterId, position: database.position,
    type: "child_page", content: { childPageId: made.pageId, childPageTitle: title } };
  // The open page's graph store holds whichever page is open: the change is
  // this page's alone, not the new page's own graph opened meanwhile (where
  // the database stands at its root, its container that page's).
  const onThisPage = (now: ContentGraph) => now.databaseContainers.find(({ blockId }) => blockId === database.id)?.pageId === pageId;
  const turned = (now: ContentGraph) => onThisPage(now) && now.blocks.some(({ id }) => id === database.id)
    ? { ...now, blocks: [...now.blocks.filter(({ id }) => id !== database.id && id !== pageBlock.id), pageBlock] } : now;
  const back = (now: ContentGraph) => !now.blocks.some(({ id }) => id === pageBlock.id) || now.blocks.some(({ id }) => id === database.id) ? now
    : { ...now, blocks: [...now.blocks.filter(({ id }) => id !== pageBlock.id), database] };
  // The sidebar lists it where its block stands among the page's sub-pages.
  const order = pagesInBodyOrder(turned(graph), isTurnedPageBlock);
  const positions = new Map(context.pages.filter((page) => page.parentId === pageId).map((page) => [page.id, page.position]));
  const page: Page = { ...made.page, kind: "database", teamspaceId: parent?.teamspaceId ?? "",
    position: positionAmong(order, positions, made.pageId) ?? made.page.position };
  // Its own page holds the database alone, the source's records with it.
  const { databaseContainers, dataSources, views, properties, rows } = inlineDatabaseDrawn(graph, database);
  const owns = databaseContainers[0]?.kind !== "linked";
  const copy: ContentGraph = {
    blocks: [{ ...database, parentId: null, afterId: null }],
    databaseContainers: databaseContainers.map((container) => ({ ...container, pageId: made.pageId, parentBlockId: null })),
    dataSources: dataSources.map((source) => owns ? { ...source, pageId: made.pageId } : source),
    views, properties, rows,
  };
  let answer: (made: boolean) => void = () => {};
  const madeThere = new Promise<boolean>((resolve) => { answer = resolve; });
  const value = { bootstrapRevision: 0, graphRevision: 0, graph: copy, canEdit: true, canSuggest: false };
  holdShownGraph(session.user.id, made.pageId, value);
  void openPageBeforeMade(session, page, madeThere, false).then(() => localFirstProductCache.graph.writeCache({
    complete: true, key: made.pageId, principalId: session.user.id, serverKey: serverIdentity(), version: 1, value,
  })).catch(() => undefined);
  const release = holdRowsShown(input.store, turned, back);
  // Its ID opens its page now, as Notion's one address does.
  rememberFullPageDatabase(database.id, made.pageId);
  let ended: () => void = () => {};
  showPageAhead(made.pageId, { kind: "database", title }, new Promise<void>((resolve) => { ended = resolve; }));
  context.onPageChange(page);
  return {
    made,
    /** The server made the page (or couldn't): what asks about it goes now. */
    pageMade: (made: boolean) => answer(made),
    release: (refused = false) => {
      answer(false);
      ended();
      release(refused);
      if (!refused) return;
      forgetShownGraph(session.user.id, made.pageId);
      rememberInlineDatabaseAddress(database.id, pageId, true);
    },
  };
}

function isTurnedPageBlock(block: GraphBlock) {
  return block.type === "child_page" && block.content?.childPageId === block.id;
}

// Each step of turning a database into a page of its own, or back, is taken
// by the server before the next one.
function changeContent(session: Session, pageId: string, operations: GraphOperation[], pageKind?: "page" | "database") {
  return sendContentGraphChangeNow(session, pageId, { mutationId: newId(), operations, ...(pageKind ? { pageKind } : {}) });
}

/**
 * Turns an inline database into a full-page database in its place, as
 * Notion's block menu does: a new page named after the database takes the
 * place right after it, the database moves into that page, and the page
 * becomes a full-page database. A linked database's page is made with its
 * own name; without one it is called "〈source〉 보기" with no large title, as
 * Notion's (seen 2026-10-15). The page-kind change gives the page that
 * name's formatting and the database's icon, and keeps a made name as made
 * (database_turn_linked_name.go). A page the database couldn't move into
 * goes to the trash. Returns the new page's ID.
 */
export async function turnInlineDatabaseIntoPage(input: {
  session: Session;
  pageId: string;
  workspaceId: string;
  database: PlacedBlock & Partial<GraphBlock>;
  title: string;
  moves: Pick<BlockMoveService, "move">;
  /** The source's title, which names a linked database without a name of its own. */
  sourceTitle?: string;
  /** The page as made on this device first (showInlineDatabaseTurnedHere), which the server makes under its ID. */
  made?: NativePageCreation;
  /** Told once the server made the page, or couldn't. */
  onPageMade?(made: boolean): void;
}) {
  const { session, pageId, database } = input;
  const title = turnedPageTitle(database, input.title, input.sourceTitle);
  const page = input.made ? await sendPageCreationHere(session, input.made).then(({ page: made }) => made, (error: unknown) => {
    input.onPageMade?.(false);
    throw error;
  }) : await createNativePage(session, { parentId: pageId, title, workspaceId: input.workspaceId });
  input.onPageMade?.(true);
  await changeContent(session, pageId, [{
    kind: "moveBlock", move: { blockId: page.id, parentId: database.parentId, afterId: database.id },
  }]);
  try {
    await input.moves.move({ sourcePageId: pageId, blockId: database.id, destinationPageId: page.id });
  } catch (error) {
    await changeContent(session, pageId, [{ kind: "deleteBlock", deletion: { blockId: page.id } }]).catch(() => {});
    throw error;
  }
  await changeContent(session, page.id, [], "database");
  // Its ID opens its page now, as Notion's one address does.
  rememberFullPageDatabase(database.id, page.id);
  return page.id;
}

/**
 * Turns a full-page database inline where its page's block is, as Notion's
 * "Turn into inline" does: the page becomes a page again, its database moves
 * into the parent page right after the page's block, and the page's block
 * goes, taking the emptied page to the trash. A linked database takes its
 * page's name, with its formatting, and icon as its own, as the page-kind
 * change gives them (database_turn_linked_name.go).
 */
export async function turnFullPageDatabaseInline(input: {
  session: Session;
  pageId: string;
  pageBlock: PlacedBlock;
  moves: Pick<BlockMoveService, "move">;
}) {
  const { session, pageId, pageBlock } = input;
  // Its block, without the database's rows.
  const read = await collectContentGraphStructure(session, pageBlock.id);
  const database = read?.graph.blocks.find((block) => !block.parentId && block.type === "inline_database");
  if (!database) throw new Error("The full-page database could not be read.");
  await changeContent(session, pageBlock.id, [], "page");
  await input.moves.move({ sourcePageId: pageBlock.id, blockId: database.id, destinationPageId: pageId });
  await changeContent(session, pageId, [
    { kind: "moveBlock", move: { blockId: database.id, parentId: pageBlock.parentId, afterId: pageBlock.id } },
    { kind: "deleteBlock", deletion: { blockId: pageBlock.id } },
  ]);
  // Its ID opens it alone in this page now, as its page's does (useDatabasePageSignpost).
  rememberInlineDatabaseAddress(database.id, pageId, true);
  return database.id;
}
