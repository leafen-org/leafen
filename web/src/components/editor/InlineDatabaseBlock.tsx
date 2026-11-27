import { Suspense, useContext, useEffect, useState } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { lazyScreen, withLoadDeadline } from "@/lib/lazyScreen";
import { DatabaseCommentAccessContext } from "@/components/database/cellCommentsContext";
import { graphReadBy, ofDataSource } from "@/components/database/graphSourceSlices";
import { compareGraphOrder, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import { chosenDatabaseView, useDatabaseViewChoices } from "@/lib/databaseViewChoice";
import { inlineDatabaseFullPageHref, linkedDatabasePresentation } from "@/lib/inlineDatabaseFullPage";
import { madeHereBy } from "@/lib/pageMadeHere";
import { notePeekedDatabaseIn } from "@/lib/pagePeek";
import { clearDatabaseStartPanel, databaseStartPanelRequest, requestDatabaseStartPanel, subscribeDatabaseStartPanel } from "@/lib/databaseStartPanel";
import { databaseStarterBand } from "@/lib/databaseStarterBand";
import { requestInlineDatabase } from "@/lib/inlineDatabaseRequests";
import { shownBlockCopy, useShownBlockCopy } from "@/lib/shownBlockCopy";
import { usePageBusy } from "@/lib/pageBusy";
import { databaseBlockMembers, databaseNamingSourceIds, databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import type { BlockItemProps } from "./editorRuntimeTypes";
import { blockItemText } from "./blockItemLabels";
import { useShareInlineDatabaseAlone } from "./inlineDatabaseAlone";
import { SuggestingModeContext } from "./suggestingMode";

// A database's views (tables, boards, calendars, charts and their menus) come
// apart from the editor, once the app is idle: a database made from "/" is
// drawn on its press's own frame, as Notion's (user decision 2026-11-12; its
// code asked for at the press, the table showed 330 ms after the Enter on a
// NAS — Notion's 55 — React holding back a part whose code comes late).
let inlineDatabaseModule: Promise<typeof import("@/components/database/InlineDatabase")> | null = null;
function importInlineDatabase() {
  return inlineDatabaseModule ??= withLoadDeadline(import("@/components/database/InlineDatabase")).catch((error: unknown) => {
    inlineDatabaseModule = null;
    throw error;
  });
}
const InlineDatabase = lazyScreen(() => importInlineDatabase().then((module) => ({ default: module.InlineDatabase })), { drawnOnPress: true });
/** Gets a database's views' code here, so they draw in the pass that first shows one. */
export function loadInlineDatabase() {
  return InlineDatabase.preload();
}
const DatabaseLinkPending = lazyScreen(() => import("@/components/database/DatabaseLinkPending").then((module) => ({ default: module.DatabaseLinkPending })));
const DatabaseLinkPendingSettings = lazyScreen(() => import("@/components/database/DatabaseLinkPendingSettings")
  .then((module) => ({ default: module.DatabaseLinkPendingSettings })));
// The start panel takes typing as it opens, its code fetched once the app is
// idle: fetched on the first "/" database's Enter, it opened 336 ms after the
// key on a NAS's 100 ms round trip (2026-11-13; Notion's 167).
const DatabaseStartPanel = lazyScreen(() => import("@/components/database/DatabaseStartPanel").then((module) => ({ default: module.DatabaseStartPanel })),
  { typedOnOpen: true });
const DatabaseStartBand = lazyScreen(() => import("@/components/database/DatabaseStartBand").then((module) => ({ default: module.DatabaseStartBand })));

function inlineDatabaseMemberIds(block: GraphBlock) {
  const additional = Array.isArray(block.content.additionalDataSourceIds)
    ? block.content.additionalDataSourceIds.filter((id): id is string => typeof id === "string" && id !== "")
    : [];
  return [String(block.content.dataSourceId ?? ""), ...additional];
}

// Kept for each list of views and block as read, as their rows are (graphSourceSlices.ts).
const blockViews = new WeakMap<ContentGraph["views"], WeakMap<GraphBlock, ReturnType<typeof readInlineDatabaseViews>>>();

/** An inline database's views, in tab order, and its dashboards' widget views. */
export function inlineDatabaseViews(graph: ContentGraph, block: GraphBlock) {
  let byBlock = blockViews.get(graph.views);
  if (!byBlock) {
    byBlock = new WeakMap();
    blockViews.set(graph.views, byBlock);
  }
  let views = byBlock.get(block);
  if (!views) {
    views = steadyViews(block.id, readInlineDatabaseViews(graph, block));
    byBlock.set(block, views);
  }
  return views;
}

// The views each block last showed, which a new list of the page's views
// holding the same ones for it gives again: a view of another database
// changed (a row made in its own order) drew this one's again (2026-11-14).
const shownViews = new Map<string, ReturnType<typeof readInlineDatabaseViews>>();
const sameItems = <T,>(left: readonly T[], right: readonly T[]) => left.length === right.length && left.every((item, index) => item === right[index]);

function steadyViews(blockId: string, read: ReturnType<typeof readInlineDatabaseViews>) {
  const shown = shownViews.get(blockId);
  if (shown && sameItems(shown.memberIds, read.memberIds) && sameItems(shown.views, read.views)
    && sameItems(shown.dashboardWidgets, read.dashboardWidgets)) return shown;
  shownViews.set(blockId, read);
  return read;
}

function readInlineDatabaseViews(graph: ContentGraph, block: GraphBlock) {
  // Views follow the database's ordered member sources; the active view decides
  // which source's schema and rows are shown, while the first member titles it.
  // The database owns the views of the sources it links; a linked database
  // shows only those.
  const memberIds = inlineDatabaseMemberIds(block);
  // Dashboard widgets show inside their dashboard, not as tabs.
  const dashboardWidgets = graph.views.filter((candidate) => isDashboardWidget(candidate) && (candidate.containerId === block.id
    || (!candidate.containerId && block.content.linkedDatabaseSource !== true && memberIds.includes(candidate.dataSourceId))));
  // Tabs follow their saved order, also right after one is moved.
  const ownedViews = graph.views.filter((candidate) => candidate.containerId === block.id && !isDashboardWidget(candidate))
    .sort(compareGraphOrder);
  // One row of tabs whichever source each shows, as Notion's (seen
  // 2026-11-02: a view added last stays last when it turns to another
  // source); views at the same place keep their sources' order.
  const all = block.content.linkedDatabaseSource === true
    ? ownedViews
    : [...memberIds.flatMap((memberId) => graph.views
      .filter((candidate) => candidate.dataSourceId === memberId && !candidate.containerId && !isDashboardWidget(candidate))
      .sort(compareGraphOrder)), ...ownedViews]
      .sort((left, right) => left.position < right.position ? -1 : left.position > right.position ? 1 : 0);
  // A source sent to the trash from 데이터 소스 관리 took its views along, as
  // Notion's (seen 2026-11-11), unless they are all the database has to show
  // (the server's database_source_trash_in_place.go).
  const gone = Array.isArray(block.content.trashedWithViewsDataSourceIds) ? block.content.trashedWithViewsDataSourceIds : [];
  const kept = all.filter((view) => view.containerId || !gone.includes(view.dataSourceId));
  const views = kept.length > 0 ? kept : all;
  return { memberIds, views, dashboardWidgets };
}

export function InlineDatabaseBlock({ block, props }: { block: GraphBlock; props: BlockItemProps }) {
  const { adapter, staticReadOnlySemantics } = props;
  // A page being suggested leaves its databases as they are: their rows,
  // cells and menus take edits at once, as Notion's (seen 2026-11-13: a cell
  // typed in while suggesting changed for all, ⌘/ and "⋯" on a row opened
  // their full menus).
  const readOnly = adapter.readOnly && !adapter.database.writableWhileSuggesting;
  const { memberIds, views, dashboardWidgets } = inlineDatabaseViews(adapter.graph, block);
  // A copy's top shows its body before its own databases are copied into it
  // (page_duplication_top_first.go): one still to come says it is coming, as
  // a copy's place does ("불러오는 중...", nativeTiptapCopyIndicator.ts).
  const copying = usePageBusy(adapter.pageId) === "copying";
  // One of several left with one by the trash goes by that one, its first gone or not (databaseNamingSourceIds).
  const naming = databaseNamingSourceIds(block);
  const titleSource = adapter.graph.dataSources.find((candidate) => candidate.id === (naming.length === 1 ? naming[0] : memberIds[0]));
  // The view this person last chose here, else the block's saved one; in a
  // peek the address's view doesn't choose it (chosenDatabaseView).
  useDatabaseViewChoices();
  const inPeek = useContext(SuggestingModeContext).inPeek === true;
  const activeView = chosenDatabaseView(views, adapter.session?.user.id, block.id, block.content.viewId, !inPeek);
  // Opened alone in a peek it is drawn on the page under it too: the copy
  // that shows takes what is asked of the block (shownBlockCopy).
  const alone = adapter.fixedBody === true;
  useShownBlockCopy(block.id, alone);
  // A peek showing it alone over this page by its own ID learns where it is (notePeekedDatabaseIn).
  useEffect(() => { if (!alone && !inPeek) notePeekedDatabaseIn(block.id, adapter.pageId); }, [adapter.pageId, alone, block.id, inPeek]);
  // A database just made from "/", or a source just added, shows Notion's start panel once.
  // A linked view just made on a phone shows none: in a page made on this device in this
  // window it waits for its "데이터 소스 연결"; in one opened otherwise — from its address,
  // or reloaded — its settings sheet opens at once, the keyboard down (Notion's, seen
  // 2026-11-18 on a Galaxy A90's Chrome: a page made by hand waited, the same page opened
  // in a new tab and reloaded in its own opened "설정", no script before the press).
  const { phone } = useSidebarLayout(true);
  const shown = (request: ReturnType<typeof databaseStartPanelRequest>) => request?.made && phone ? null : request;
  const [starting, setStarting] = useState(() => shownBlockCopy(block.id, alone) ? shown(databaseStartPanelRequest(block.id)) : null);
  const [pendingSettings, setPendingSettings] = useState(false);
  useEffect(() => {
    const take = () => {
      const request = shownBlockCopy(block.id, alone) ? databaseStartPanelRequest(block.id) : null;
      if (!request) return;
      clearDatabaseStartPanel(block.id);
      setStarting(shown(request));
      if (request.made && phone && !(adapter.session && madeHereBy(adapter.pageId, adapter.session.user.id))) setPendingSettings(true);
    };
    take();
    return subscribeDatabaseStartPanel(take);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [alone, block.id, phone]);
  const dataSourceId = activeView?.dataSourceId ?? memberIds[0];
  const source = adapter.graph.dataSources.find((candidate) => candidate.id === dataSourceId);
  const properties = ofDataSource(adapter.graph.properties, dataSourceId);
  const rows = ofDataSource(adapter.graph.rows, dataSourceId);
  const titleProperty = properties.find((candidate) => candidate.type === "title");
  // Opened alone, its page's "⋯" menu works on it, not on the page holding it.
  useShareInlineDatabaseAlone(adapter, block, { alone, views: databaseViewBlock(block, adapter.graph.views), dataSourceId: dataSourceId ?? "", title: titleSource?.title ?? "" });
  // A linked view whose source left everyone's sight, in its 30 days after a
  // permanent deletion or gone for good, is an empty link: its views wait for
  // a source as a linked view not yet linked does, and it moves, goes and
  // links anew as one (the server reads them waiting, database_gone_links_read.go).
  if (!source && activeView?.config.linkPending === true && block.content.linkedDatabaseSource === true && adapter.database.databaseViewAdapter) {
    const viewAdapter = adapter.database.databaseViewAdapter;
    const inGraph = new Set(adapter.graph.dataSources.map(({ id }) => id));
    const existing = databaseBlockMembers(block).linked.map(({ dataSourceId: id }) => id).filter((id) => inGraph.has(id));
    return <>{starting && adapter.database.startPanel && adapter.session && !readOnly
      ? <Suspense fallback={null}><DatabaseStartPanel session={adapter.session} workspaceId={adapter.workspaceId} pageId={adapter.pageId}
        pageTitle="" blockId={block.id} dataSourceId="" viewId={activeView.id} addedSource={false} link existing={existing}
        actions={adapter.database.startPanel} docked={adapter.fixedBody} chart={activeView.type === "chart"} onClose={() => setStarting(null)} /></Suspense> : null}
      <Suspense fallback={null}><DatabaseLinkPending view={activeView} views={views} readOnly={readOnly}
        onSelect={(view) => viewAdapter.selectView(block, view)}
        onDeleteView={(view) => void viewAdapter.deleteView(view, () => true)}
        onConnect={() => requestDatabaseStartPanel(block.id, { link: true })} /></Suspense></>;
  }
  if (!source || !titleSource || !activeView || !titleProperty || !adapter.database.databaseViewAdapter) {
    return <div data-inline-database-missing role={copying ? "status" : undefined}>{blockItemText(copying ? "database.copying" : "database.unavailable")}</div>;
  }
  // A linked view still waiting for its source shows its tab and "데이터
  // 소스 연결" in place of the table; "비어 있는 새 데이터 소스" keeps its own
  // empty source, as Notion's (seen 2026-11-02), "새 '〈search〉' 데이터 소스"
  // naming it by the search (seen 2026-11-08), as it names a database just
  // made from its start panel's search (seen 2026-11-10).
  const linkPending = activeView.config.linkPending === true, band = databaseStarterBand(block);
  // A view waiting among linked ones stands on a source the database links,
  // one it may link again (linkViewInPlaceOperations).
  const waitsOnLinked = linkPending && block.content.linkedDatabaseSource === true;
  // One among other views offers the sources alone, those the block links
  // first, with no empty source of its own (Notion's, seen 2026-11-11).
  const existing = linkPending && (views.length > 1 || waitsOnLinked) ? databaseBlockMembers(block).linked.map(({ dataSourceId }) => dataSourceId) : undefined;
  const viewAdapter = adapter.database.databaseViewAdapter;
  const keepOwnSource = (name?: string) => {
    const { linkPending: _pending, ...config } = activeView.config;
    void viewAdapter.updateViews([{ ...activeView, config }], () => true);
    if (name) viewAdapter.changeTitle(source, name);
  };
  const start = starting && adapter.database.startPanel && adapter.session && !readOnly
    ? <Suspense fallback={null}><DatabaseStartPanel session={adapter.session} workspaceId={adapter.workspaceId} pageId={adapter.pageId}
      pageTitle={titleSource.title} blockId={block.id} dataSourceId={starting.dataSourceId ?? (waitsOnLinked ? "" : source.id)} viewId={activeView.id} addedSource={Boolean(starting.dataSourceId)}
      link={Boolean(starting.link) && linkPending} existing={existing} relink={Boolean(starting.relink)} sources={Boolean(starting.sources)} onBlank={linkPending ? keepOwnSource : (name) => { if (name) viewAdapter.changeTitle(source, name); }}
      actions={adapter.database.startPanel} docked={adapter.fixedBody} chart={activeView.type === "chart"} slash={Boolean(starting.slash)} onClose={() => setStarting(null)} /></Suspense> : null;
  if (linkPending) {
    // Opened alone (its own page, a side peek) it offers the sources each
    // time it opens, under its tab row once that is drawn, as Notion's (seen 2026-11-09).
    const offerOnOpen = adapter.fixedBody && !readOnly && Boolean(adapter.database.startPanel);
    // All its views are its tabs, those linked to a source among those
    // waiting: one chosen here, another added in a layout then linked alone,
    // or deleted (Notion's, seen 2026-11-11). Once one is linked the block is
    // a linked database, named as one.
    const linkedName = waitsOnLinked ? linkedDatabasePresentation(block) : null;
    const settings = pendingSettings && phone && !readOnly ? <Suspense fallback={null}><DatabaseLinkPendingSettings view={activeView}
      pageId={adapter.pageId} blockId={block.id} locked={block.content.locked === true}
      fullPageHref={adapter.fixedBody || !adapter.session ? undefined : inlineDatabaseFullPageHref(adapter.pageId, block.id, activeView.id)}
      onUpdateView={(view) => void viewAdapter.updateViews([view], () => true, true)} onLock={(locked) => viewAdapter.setLocked(block, locked)}
      onConnect={() => requestDatabaseStartPanel(block.id, { link: true })} onClose={() => setPendingSettings(false)} /></Suspense> : null;
    return <>{start}{settings}<Suspense fallback={null}><DatabaseLinkPending view={activeView} views={views} readOnly={readOnly}
      onSelect={(view) => viewAdapter.selectView(block, view)}
      // The view added shows, chosen, with its chooser on the press's own frame.
      onAddView={(type) => void viewAdapter.createView(block, source, type, () => true, activeView, { linkPending: true }, (made) => {
        viewAdapter.selectView(block, made);
        requestDatabaseStartPanel(block.id, { link: true });
      })}
      onDeleteView={(view) => void viewAdapter.deleteView(view, () => true)}
      onConnect={() => requestDatabaseStartPanel(block.id, { link: true })}
      onDrawn={offerOnOpen ? () => setStarting((current) => current ?? { link: true }) : undefined}
      name={linkedName ? { blockId: block.id, alone, workspaceId: adapter.workspaceId, pageId: adapter.pageId, source: { ...source, ...linkedName },
        onTitleChange: (title) => viewAdapter.setLinkedPresentation(block, { title }),
        onIconChange: (icon, iconType) => viewAdapter.setLinkedPresentation(block, { icon: { icon, iconType } }) }
        : { blockId: block.id, alone, workspaceId: adapter.workspaceId, pageId: adapter.pageId, source,
          onTitleChange: (title) => viewAdapter.changeTitle(source, title),
          onIconChange: (icon, iconType) => viewAdapter.changeIcon(source, { icon, iconType }) }} /></Suspense></>;
  }
  return <>{start}<DatabaseCommentAccessContext.Provider value={adapter.database.canComment ?? !readOnly}><Suspense fallback={<div data-inline-database-loading aria-busy="true" contentEditable={false}
    style={{ minHeight: 120 }} />}><InlineDatabase block={block} source={source} titleSource={titleSource} dataSources={adapter.graph.dataSources}
    filterGraph={graphReadBy(adapter.graph, [...memberIds, ...views.map((view) => view.dataSourceId), ...dashboardWidgets.map((view) => view.dataSourceId)])} views={views} dashboardWidgets={dashboardWidgets} activeView={activeView} properties={properties} rows={rows}
    boardAdapter={adapter.database.boardAdapter} databaseViewAdapter={adapter.database.databaseViewAdapter}
    tableAdapter={adapter.database.tableAdapter} timelineAdapter={adapter.database.timelineAdapter}
    pageId={adapter.pageId} workspaceId={adapter.workspaceId} session={adapter.session}
    formService={adapter.database.formService} onAddProperty={adapter.database.onAddProperty}
    onConfiguredView={adapter.database.onConfiguredView} readOnly={readOnly} lockableUnderPageLock={readOnly && adapter.database.lockableUnderPageLock === true}
    structureLocked={adapter.database.structureLocked} staticReadOnlySemantics={staticReadOnlySemantics}
    configurationReady={!adapter.database.pendingToolbarDataSourceIds.has(source.id)}
    fullPageHref={adapter.fixedBody || staticReadOnlySemantics || !adapter.session
      ? undefined : inlineDatabaseFullPageHref(adapter.pageId, block.id, activeView.id)}
    ownPage={adapter.fixedBody} pageLock={adapter.fixedBody ? adapter.database.pageLock : undefined} /></Suspense></DatabaseCommentAccessContext.Provider>
    {band && adapter.fixedBody && !readOnly && adapter.session && viewAdapter.setStarterBand ? <Suspense fallback={null}>
      <DatabaseStartBand session={adapter.session} band={band} rowIds={rows.map(({ id }) => id)}
        onChange={(next) => viewAdapter.setStarterBand?.(block, next)} onReread={async () => { await viewAdapter.dataSources?.reread(); }}
        onCustomize={() => { requestInlineDatabase(`settings:${block.id}`, "view"); }} /></Suspense> : null}</>;
}
