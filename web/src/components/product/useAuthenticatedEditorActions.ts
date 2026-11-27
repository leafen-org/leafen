import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { useCallback, useEffect, useRef, type Dispatch, type SetStateAction } from "react";

import { api } from "@/api";
import type { DatabaseStarterSelection } from "@/lib/databaseStarterCatalog";
import type { BodyFocus } from "@/lib/pageTitleFocus";
import { notePageGraphChange } from "@/components/editor/pageUndo";
import {
  createLinkedInlineDatabase,
  createParagraph,
  type ContentGraph,
  type GraphBlock,
  type GraphOperation,
} from "@/contentGraph";
import { DEFAULT_DATABASE_TITLE } from "@/components/editor/databaseTitles";
import { moveEditorBlock, placeNewDatabase, type EditorDatabaseChoice } from "@/components/editor/editorGraphModel";
import type { EditorDatabaseStartActions, EditorPageStarterActions } from "@/components/editor/editorRuntimeTypes";
import { focusEditable, getEditable, selectEditableContents } from "@/components/editor/focus";
import { changeReached } from "@/components/editor/editorPagesMadeHere";
import { loadInlineDatabase } from "@/components/editor/InlineDatabaseBlock";
import { requestDatabaseStartPanel } from "@/lib/databaseStartPanel";
import { markFreshDatabase, markFreshForm } from "@/lib/freshDatabases";
import { databaseBlockMembers, databaseViewBlock, linkDataSourceOperations, linkSourceInPlaceOperations, linkViewInPlaceOperations, withDatabaseBlockMembers } from "@/lib/nativeDataSourceManagement";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { showPageAhead } from "@/lib/pagesShownAhead";
import { typedPageTitle } from "@/lib/pageNames";
import { sendTypedPageTitles } from "@/lib/pageKindChange";
import { createStartedDatabase, slashViewName } from "@/lib/databaseViewStart";
import { newDatabaseNames, pageDatabaseStartHere, type FullPageDatabaseStart } from "@/lib/fullPageDatabase";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import type { PageDatabaseChoice } from "@/lib/databaseStarterCreation";
import { databaseStartMade } from "@/lib/databaseStartKept";
import { databaseStartFollowed } from "@/lib/databaseStartRefusal";
import { overlayPendingGraphOperations } from "./pendingGraphOverlay";
import { noteSourceLinkStep, withLinkedSourceFrame } from "./sourceLinkStep";
import type { Bootstrap, Page, Session } from "@/types";

type EditorGraphWrites = {
  commit: (pageId: string, operations: GraphOperation[]) => Promise<boolean>;
  commitStepping: (pageId: string, operations: GraphOperation[]) => Promise<boolean>;
  queue: (pageId: string, operations: GraphOperation[]) => void;
  reread: (pageId: string) => Promise<boolean>;
};

/** The page edited: its kind and title where the host knows them. */
export type EditedPage = Pick<Page, "id" | "workspaceId"> & Partial<Pick<Page, "title" | "kind" | "parentId" | "teamspaceId">>;

export function useAuthenticatedEditorActions({
  bodyLoading,
  changeText,
  readGraph,
  graphWrites,
  markToolbarDataSourcePending,
  readText,
  refreshPages,
  selectedPage,
  selectedPageId,
  session,
  setData,
  setDatabasePickerOpen,
  setGraph,
  setMessage,
}: {
  /** The open page's body is still being read, with no line to go to yet. */
  bodyLoading: boolean;
  changeText: (blockId: string, text: string) => void;
  /** The page's graph as drawn, which each action reads as it begins. */
  readGraph: () => ContentGraph;
  graphWrites: EditorGraphWrites;
  markToolbarDataSourcePending: (dataSourceId: string) => void;
  readText: (blockId: string) => string;
  refreshPages?: () => Promise<unknown>;
  selectedPage: EditedPage | null;
  selectedPageId: string | null;
  session: Session;
  /** Keeps a page's change in the page list until the list is read again. */
  setData?: Dispatch<SetStateAction<Bootstrap>>;
  setDatabasePickerOpen: Dispatch<SetStateAction<boolean>>;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
}) {
  function focusParagraph(blockId: string, selection: BodyFocus) {
    const focused = focusEditable(blockId, "start");
    if (selection !== "select") return;
    let remaining = 12;
    const select = () => {
      const editable = getEditable(blockId);
      if (editable) {
        selectEditableContents(editable);
        return;
      }
      remaining -= 1;
      if (remaining > 0) window.requestAnimationFrame(select);
    };
    if (focused) select();
    else window.requestAnimationFrame(select);
  }

  function addParagraph(selection: BodyFocus = "start") {
    const graph = readGraph();
    if (!selectedPage) return;
    const created = createParagraph(graph.blocks);
    setGraph((current) => ({ ...current, blocks: [...current.blocks, created.block] }));
    graphWrites.queue(selectedPage.id, created.operations);
    focusParagraph(created.block.id, selection);
    return created.block.id;
  }

  // A title's Enter starts the body with a new first line, above the lines
  // already there, as Notion's (seen 2026-11-01): with the caret, but in a body kept unseen (BodyFocus "unseen").
  function addFirstParagraph(page: EditedPage, caret = true) {
    const graph = readGraph();
    const created = createParagraph(graph.blocks);
    const withLine = (current: ContentGraph) => {
      const added = { ...current, blocks: [...current.blocks, created.block] };
      return moveEditorBlock(added, created.block.id, null, null)?.graph ?? added;
    };
    setGraph(withLine);
    const operations: GraphOperation[] = [...created.operations,
      { kind: "moveBlock", move: { blockId: created.block.id, parentId: null, afterId: null } }];
    graphWrites.queue(page.id, operations);
    // A step of the page's ⌘Z, between what is written under it and the title's words (Notion's, seen 2026-11-04).
    notePageGraphChange({ pageId: page.id, before: graph, after: withLine(graph), operations, titleLine: created.block.id });
    if (caret) { focusParagraph(created.block.id, "start"); keepCaretThroughRebuild(created.block.id); }
    return created.block.id;
  }

  // The first line a title's Enter made, which that Enter asking again goes back to.
  const titleEnterLine = useRef<{ pageId: string; blockId: string } | null>(null);

  // A fresh page's body is built again as its document arrives, a moment
  // after its first line (2026-11-01: in a center peek the window then took
  // the caret to its first button): the line takes it back, once, if its
  // editable was replaced.
  function keepCaretThroughRebuild(blockId: string) {
    let editable: HTMLElement | undefined, frames = 0;
    const watch = () => {
      const now = getEditable(blockId);
      if (editable && !editable.isConnected && now && now !== editable) {
        focusEditable(blockId, "end");
        return;
      }
      editable ??= now;
      if (++frames < 90) window.requestAnimationFrame(watch);
    };
    window.requestAnimationFrame(watch);
  }

  function focusFirstBody(selection: BodyFocus) {
    const graph = readGraph();
    // A full-page database's body is its database, with no line to go to,
    // and a body still being read has none yet (the title holds what is
    // typed until it has, PageHeader).
    if (selection === "new") titleEnterLine.current = null;
    if (!selectedPage || selectedPage.kind === "database" || bodyLoading) return;
    const made = titleEnterLine.current;
    if (selection === "again" && made?.pageId === selectedPage.id) {
      focusParagraph(made.blockId, "start");
      return;
    }
    if (selection === "new" || selection === "again" || selection === "unseen") {
      titleEnterLine.current = { pageId: selectedPage.id, blockId: addFirstParagraph(selectedPage, selection !== "unseen") };
      return;
    }
    // An empty body keeps its "시작하기" row while the caret is in it, as
    // Notion's does (seen 2026-10-31); a typed letter hides it.
    const firstParagraph = graph.blocks.find((block) => block.type === "paragraph");
    if (firstParagraph) {
      focusParagraph(firstParagraph.id, selection);
      return;
    }
    addParagraph(selection);
  }

  // The line a menu chose the database for, while the chooser is open.
  const databaseChoice = useRef<EditorDatabaseChoice | null>(null);

  function takeDatabaseChoice() {
    const choice = databaseChoice.current;
    databaseChoice.current = null;
    return choice;
  }

  function createDatabase() {
    const graph = readGraph();
    if (!selectedPage) return;
    const choice = takeDatabaseChoice();
    const created = createStartedDatabase(
      graph.blocks, DEFAULT_DATABASE_TITLE, selectedPage.id, newDatabaseNames(),
      choice?.view ? { type: choice.view, name: choice.link ? "" : slashViewName(choice.view), config: choice.viewConfig } : undefined,
    );
    const placed = choice ? placeNewDatabase(graph, created, choice) : null;
    markToolbarDataSourcePending(created.dataSource.id);
    // A database made from "/" opens Notion's "새 데이터베이스" start panel and
    // a linked view its chooser, a page's first as every later one, as on the
    // person's own device (2026-11-14: a page's first opened it in their
    // Chrome and Notion app, its first field taking the focus; a worker's
    // Chrome opened none for a page's first, which is not followed). The
    // panel's search field takes the focus here, Notion's AI field before it
    // being an approved difference; never at once on a phone
    // (InlineDatabaseBlock); a form, a dashboard, a view block, none (seen
    // 2026-11-09). Put away, the panel leaves the caret at the page title's
    // start (DatabaseStartPanel).
    if (choice?.link) requestDatabaseStartPanel(created.block.id, { link: true, made: true, slash: true });
    else if (choice?.view === "form") markFreshForm(created.views[0].id);
    // A dashboard "/" makes opens out of Edit mode, its empty view saying "차트, 표, 목록 추가"
    // over "대시보드 편집", the caret at the page title's start, as Notion's (seen 2026-11-16 in
    // Chrome at 1440, a Business workspace, a new page's first line, two tabs at two times, no
    // script before the key; a page's second, seen once in Edit mode on 2026-11-12, not again).
    // A tab's "+" opens one in Edit mode (DatabaseViewTabs).
    else if (choice && !databaseViewBlock(created.block, created.views)) {
      markFreshDatabase(created.dataSource.id, created.properties.length);
      requestDatabaseStartPanel(created.block.id, { slash: true });
    }
    const withDatabase = (current: ContentGraph) => {
      const here = choice ? placeNewDatabase(current, created, choice) : null;
      return {
        blocks: here?.blocks ?? [...current.blocks, created.block],
        databaseContainers: [...current.databaseContainers, here?.container ?? created.container],
        dataSources: [...current.dataSources, created.dataSource],
        views: [...current.views, ...created.views],
        properties: [...current.properties, ...created.properties],
        rows: [...current.rows, ...created.rows],
      };
    };
    setGraph(withDatabase);
    // A new database has no rows; as Notion's, nothing in it takes the caret.
    const operations = placed?.operations ?? created.operations;
    graphWrites.queue(selectedPage.id, operations);
    // Made, it is a step of the page's ⌘Z.
    choice?.made?.(graph, withDatabase(graph), operations);
  }

  function addDatabase(choice?: EditorDatabaseChoice) {
    if (!selectedPage) return;
    databaseChoice.current = choice ?? null;
    void loadInlineDatabase().catch(() => undefined);
    if (choice?.create) {
      createDatabase();
      return;
    }
    setDatabasePickerOpen(true);
  }

  // Async work belongs to the page and the person that opened it (their sign-in handed over or renewed keeps it).
  // An old closure must not publish a graph into the newly selected page or create it a second time.
  const currentPage = useRef(selectedPageId);
  currentPage.current = selectedPageId;
  const signedIn = useRef(session);
  signedIn.current = session;
  const starterWork = useRef<AbortController | null>(null);
  useEffect(() => () => { starterWork.current?.abort(); starterWork.current = null; }, [selectedPageId, session.user.id]);

  function startPageDatabase(view: FullPageDatabaseStart, source?: DatabaseContainerSource, signal?: AbortSignal, template?: DatabaseStarterSelection, templateName?: string) {
    const graph = readGraph();
    if (!selectedPage || selectedPage.kind === "database" || signal?.aborted) return false;
    const roots = graph.blocks.filter((block) => !block.parentId);
    if (roots.length > 1 || graph.blocks.length !== roots.length || roots.some(block =>
      block.type !== "paragraph" || readText(block.id).length !== 0)) return false;
    const lineIds = roots.map(block => block.id);
    // A blank database or one of its views shows at once (fullPageDatabase.ts).
    if (!template && !source && view !== "form") return startPageDatabaseHere(view, lineIds);
    return startChosenPageDatabase(template ? { kind: "template", selection: template } : source ? { kind: "source", source }
      : { kind: "view", view }, lineIds, templateName);
  }

  // A template's, a source's or a form's database shows at once too (user
  // decision 2026-11-12: a press answers on the next frame — the starter's
  // window stayed up and the page empty while the server made it, ~0.6–0.9 s
  // on a NAS): the page is listed as the database it will be and its empty
  // line put away, its body taking nothing meanwhile (a full-page database's
  // body offers no starter), while the server makes the database, which only
  // it can (a template's content, a source's rows; the server checks the
  // empty line again under its room lock before deleting it, so a concurrent
  // writer's text is kept). The page's changes collected so far go first,
  // its start kept in the outbox behind them at once (databaseStartKept.ts:
  // sent from the window alone after two round trips, a reload right after
  // the press lost it, 2026-11-18). Once it went, the list and the page are
  // read again; refused, the page is a page again, as the server holds it,
  // and says why.
  function startChosenPageDatabase(choice: PageDatabaseChoice, lineIds: string[], templateName?: string) {
    if (!selectedPage) return false;
    const pageId = selectedPage.id, kind = selectedPage.kind ?? "page";
    // Its title typed just before goes first, kept ahead of its start (lib/pageKindChange.ts).
    const titleBefore = typedPageTitle(pageId, selectedPage.title ?? "") ?? selectedPage.title ?? "";
    sendTypedPageTitles();
    const here = () => currentPage.current === pageId && signedIn.current.user.id === session.user.id;
    // A source's page goes by "〈source〉 보기", a template's by its name over the page's (Notion's, seen 2026-11-18), an
    // untitled form's by "새 폼" (fullPageDatabase.ts). The page as it was goes to the trash (database_start_page_trash.go).
    const named: { title?: string } = choice.kind === "source" ? { title: i18next.t("editor:starter.linkedTitle", { title: choice.source.title }) }
      : choice.kind === "template" && templateName ? { title: templateName }
      : choice.kind === "view" && choice.view === "form" && !titleBefore.trim() ? { title: i18next.t("editor:starter.formDefaults.newForm") } : {};
    const listed = (change: (page: Page) => Page) =>
      setData?.(current => ({ ...current, pages: current.pages.map(item => item.id === pageId ? change(item) : item) }));
    const gone = new Set(lineIds);
    listed(item => ({ ...item, kind: "database", ...named }));
    setGraph(current => ({ ...current, blocks: current.blocks.filter(({ id }) => !gone.has(id)) }));
    void graphWrites.commit(pageId, []);
    const made = databaseStartMade(session, pageId, { choice, lineIds, replacesPage: true });
    // A page list read meanwhile lists it as the database it will be.
    showPageAhead(pageId, { kind: "database", ...named }, made);
    void made.then(async (done) => {
      if (!done && here()) setMessage(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
      if (!done) listed(item => ({ ...item, kind, ...(named.title && item.title === named.title ? { title: titleBefore } : {}) }));
      await refreshPages?.();
      if (here()) await graphWrites.reread(pageId);
    }).catch(() => undefined);
    return true;
  }

  // The page is the database at once: listed so, drawn from its graph as
  // changed; the page's changes collected so far go first, its title typed
  // just before too, then its database, named so, with its becoming a
  // full-page database, one change kept in the outbox at once (a second write
  // after it was lost to a reload, 2026-11-18), and the list is read again.
  // Refused, the page shows what the server holds and says why.
  function startPageDatabaseHere(view: FullPageDatabaseStart, lineIds: string[]) {
    if (!selectedPage) return false;
    const pageId = selectedPage.id, title = typedPageTitle(pageId, selectedPage.title ?? "") ?? selectedPage.title ?? "";
    sendTypedPageTitles();
    const started = pageDatabaseStartHere(pageId, title, view, lineIds);
    if (!started) return false;
    setData?.(current => ({ ...current, pages: current.pages.map(item => item.id === pageId ? { ...item, kind: "database" } : item) }));
    setGraph(started.show);
    void graphWrites.commit(pageId, []);
    // Kept for a server out of reach, it becomes one once the outbox sent the change (changeReached).
    const made = databaseStartFollowed(pageId, changeReached(session, pageId, () => sendContentGraphChange(session, pageId, started.operations,
      { pageKind: "database", replacesPage: true }).then(() => true)).catch(() => false));
    showPageAhead(pageId, { kind: "database" }, made);
    void made.then(async (done) => {
      if (!done && currentPage.current === pageId) setMessage(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
      await refreshPages?.();
      if (!done && currentPage.current === pageId) await graphWrites.reread(pageId);
    }).catch(() => undefined);
    return true;
  }

  const startTemplate = (selection: DatabaseStarterSelection, signal: AbortSignal, name?: string) =>
    Promise.resolve(startPageDatabase("table", undefined, signal, selection, name));
  const startDatabase = (view: FullPageDatabaseStart = "table", signal?: AbortSignal) => Promise.resolve(startPageDatabase(view, undefined, signal));
  const startLinkedDatabase = (source: DatabaseContainerSource, signal: AbortSignal) => Promise.resolve(startPageDatabase(source.viewType, source, signal));
  const loadDatabaseSourcePage = useCallback(async (query: string, cursor: string | undefined, signal: AbortSignal, limit?: number, first?: readonly string[]) => {
    if (!selectedPage) return { items: [] };
    return api.databaseContainerSources(session, selectedPage.workspaceId, query, { cursor, limit: limit ?? (cursor || query ? 25 : 4), signal, first });
  }, [selectedPage, session]);

  const loadDatabaseSources = useCallback(async (
    query: string,
    signal: AbortSignal,
  ) => {
    if (!selectedPage) return [];
    const response = await api.databaseContainerSources(
      session, selectedPage.workspaceId, query, { limit: 25, signal },
    );
    return response.items;
  }, [selectedPage, session]);

  // A database linked from the page's database chooser stands where it goes
  // at once (linkShownHere).
  function linkDatabase(source: DatabaseContainerSource) {
    const graph = readGraph();
    if (!selectedPage) return;
    const pageId = selectedPage.id;
    const choice = takeDatabaseChoice();
    const created = createLinkedInlineDatabase(graph.blocks, pageId, {
      containerId: source.id,
      dataSourceId: source.dataSourceId,
      viewType: source.viewType,
      viewName: source.viewName,
      viewConfig: source.viewConfig,
    });
    const placed = choice ? placeNewDatabase(graph, created, choice) : null;
    setDatabasePickerOpen(false);
    linkShownHere(pageId, placed?.operations ?? created.operations, source, (current) => {
      const here = choice ? placeNewDatabase(current, created, choice) : null;
      return {
        ...current, blocks: here?.blocks ?? [...current.blocks, created.block],
        databaseContainers: [...current.databaseContainers, here?.container ?? created.container], views: [...current.views, created.view],
      };
    }, () => setMessage(i18next.t("syncStatusBadge:status.linkedDatabaseAdded"), "default"));
  }

  /**
   * A source linked here shows in the press's own frame (user decision
   * 2026-11-12: a press answers on the next frame — the chooser or the start
   * panel stayed, and the page as it was, until the server took the link and
   * the page was read again, 0.6–0.7 s on a NAS): the linked view stands
   * where it goes, its source drawn from what the page holds, or from another
   * page as its frame (withLinkedSourceFrame). The link goes as the page's
   * own changes do, made against the page as drawn before it showed; once
   * the server has it the page is read again, the source's rows with it.
   * Refused, the writes say why and draw the page as the server holds it.
   */
  function linkShownHere(pageId: string, operations: GraphOperation[], source: DatabaseContainerSource,
    shown: (graph: ContentGraph) => ContentGraph, linked?: () => void) {
    const saved = graphWrites.commit(pageId, operations);
    setGraph((current) => withLinkedSourceFrame(shown(current), source));
    void saved.then(async (taken) => {
      // Read once the page's writes are saved, a change made meanwhile with them (rereadGraph).
      if (taken && currentPage.current === pageId && await graphWrites.reread(pageId) && currentPage.current === pageId) linked?.();
    }).catch(() => undefined);
    return saved;
  }

  /**
   * The start panel's suggestion: the template's database takes the place of
   * the new, still empty one (the server refuses one with rows or columns).
   * The panel closes at the press (user decision 2026-11-12: a press answers
   * on the next frame — it stayed up while the server made it); the
   * template's content, which only the server makes, comes as the page is
   * read again, its start kept in the outbox behind the page's changes, as an
   * empty page's is. Refused, it says why and the empty database stays.
   */
  function applyTemplateInPlace(blockId: string, selection: DatabaseStarterSelection, signal: AbortSignal, dataSourceId?: string) {
    if (!selectedPage || starterWork.current || signal.aborted) return Promise.resolve(false);
    const pageId = selectedPage.id;
    const controller = new AbortController();
    starterWork.current = controller;
    const here = () => currentPage.current === pageId && signedIn.current.user.id === session.user.id;
    void graphWrites.commit(pageId, []);
    const replace = { blockId, ...(dataSourceId ? { dataSourceId } : {}) };
    void databaseStartMade(session, pageId, { choice: { kind: "template", selection }, lineIds: [], replace }).then(async (done) => {
      if (!done && here()) setMessage(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
      if (here()) await graphWrites.reread(pageId);
    }).catch(() => undefined).finally(() => {
      if (starterWork.current === controller) starterWork.current = null;
    });
    return Promise.resolve(true);
  }

  /**
   * The start panel's "기존 데이터 소스 연결하기": the new, still empty
   * database becomes a linked view of the chosen source in place, the same
   * block, which then asks for its view, as Notion's (seen 2026-10-30); the
   * empty data source goes with it. A view waiting for its source, or back
   * from its "새 보기", is linked alone, the block's other views staying
   * (linkViewInPlaceOperations).
   */
  function linkSourceInPlace(blockId: string, source: DatabaseContainerSource, dataSourceId?: string, viewId?: string) {
    const graph = readGraph();
    if (!selectedPage) return Promise.resolve(false);
    const pageId = selectedPage.id;
    const block = graph.blocks.find((candidate) => candidate.id === blockId);
    if (!block) return Promise.resolve(false);
    let operations: GraphOperation[] | null = null;
    if (dataSourceId) {
      // A source just added: the chosen one is linked in its place.
      const { own, linked } = databaseBlockMembers(block);
      const linking = linkDataSourceOperations(withDatabaseBlockMembers(block, own.filter((id) => id !== dataSourceId), linked), graph, source);
      operations = linking && [...linking, { kind: "deleteDataSource", dataSourceDeletion: { id: dataSourceId } }];
    } else {
      operations = (viewId ? linkViewInPlaceOperations(block, graph, viewId, source) : null)
        ?? linkSourceInPlaceOperations(block, graph, source);
    }
    if (!operations) return Promise.resolve(false);
    // The panel closes at the press, the linked view in the block's place (linkShownHere), a step of ⌘Z once saved.
    const linking = operations;
    void linkShownHere(pageId, linking, source, (current) => overlayPendingGraphOperations(current, linking)).then((taken) => {
      if (taken && currentPage.current === pageId) noteSourceLinkStep(pageId, source, graph, linking, { graph: readGraph, setGraph,
        commit: (writes) => graphWrites.commitStepping(pageId, writes), reread: () => graphWrites.reread(pageId) });
    }, () => undefined);
    return Promise.resolve(true);
  }

  function changeParagraph(block: GraphBlock, text: string) {
    changeText(block.id, text);
  }

  return {
    addDatabase,
    addParagraph,
    changeParagraph,
    createDatabase,
    focusFirstBody,
    linkDatabase,
    loadDatabaseSources,
    startDatabase,
    startLinkedDatabase,
    startTemplate,
    loadDatabaseSourcePage,
    // What an empty page's starter and a new database's start panel do.
    pageStarter: {
      previewAuthor: session.user, onStartDatabase: startDatabase, onStartLinkedDatabase: startLinkedDatabase,
      loadDatabaseSources: loadDatabaseSourcePage, onStartTemplate: startTemplate,
    } satisfies EditorPageStarterActions,
    databaseStart: {
      previewAuthor: session.user, applyTemplate: applyTemplateInPlace, linkSource: linkSourceInPlace,
      loadDatabaseSources: loadDatabaseSourcePage,
    } satisfies EditorDatabaseStartActions,
  };
}

