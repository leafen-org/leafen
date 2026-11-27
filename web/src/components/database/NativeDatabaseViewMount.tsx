"use client";

import { usePageOnlyReader } from "@/lib/usePageOnlyReader";
import { databaseKindRowName } from "@/lib/untitledRowNames";
import { useShownViewFallback } from "./useShownViewFallback";
import { type ReactNode, useCallback, useContext, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { databaseBlockSourceName, databaseSourceInTrash } from "@/lib/nativeDataSourceManagement";
import { createPortal } from "react-dom";
import { DASHBOARD_WIDGET_SETTINGS_EVENT, DashboardFilteredContext, DashboardWidgetEditingContext } from "./dashboardWidgetHeight";
import { useDatabaseDocumentViewName } from "./useDatabaseDocumentViewName";
import { useViewNoResultsCommands } from "./viewNoResultsCommands";
import { usePageVisitRecorder } from "@/components/product/usePageVisitRecorder";
import { PageLayoutTabProviders } from "./PageLayoutTabProviders";
import { useFilterThisPage } from "./filterThisPage";
import { pageHref } from "@/lib/navigation";
import { requestDatabaseTemplate, useDatabaseTemplates } from "@/lib/databaseTemplateRequests";
import type { DatabaseSubitemsFeature, Page, TextSpan } from "@/lib/types";
import { toolbarSubitemsFeature } from "./subitemFilterScope";
import { useDefaultTemplateRows } from "./useDefaultTemplateRows";
import { useTemplateBodyRows } from "./templateBodyRows";
import { usePublicPageHref } from "@/lib/publicPageLinks";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import { DatabaseQuickFilterBar } from "./DatabaseQuickFilterBar";
import { DatabaseDashboardWidgets, dashboardWidgetRows } from "./DatabaseDashboardWidgets";
import { dashboardWidgetEditing } from "./dashboardWidgetEditing";
import { dashboardFilterProperties, dashboardFilterSources, DashboardFilterSourcesContext, dashboardKeptRowIds } from "./dashboardFilter";
import { DatabaseDocsRowsContext, DatabaseMeetingRowsContext, DatabaseOwnUntitledNameContext, DatabaseProjectRowsContext, DatabaseRowTemplateNameContext, DatabaseSprintRowsContext, DatabaseTaskRolePropertiesContext, DatabaseTaskRowsContext, databaseUntitledName } from "./untitledRowName";
import embeddedStyles from "./DatabaseDashboardWidgets.module.css";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { GraphBlock, GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { createDatabaseAutomationService } from "@/databaseAutomationApi";
import { copyText } from "@/lib/copyText";
import { splitArchivedDatabaseRows } from "@/lib/databaseRowArchive";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { useInlineDatabaseRequests } from "@/lib/inlineDatabaseRequests";
import { CardPageContentContext, createCardPageContent } from "@/lib/cardPageContent";
import { DatabaseNamesAddressContext, useDatabaseFullPageLink, useDatabaseOwnPage } from "./databaseFullPageLink";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { useTrashedAddressRow } from "./useTrashedAddressRow";
import { CellCommentsProvider } from "./CellComments";
import { DatabaseCommentAccessContext } from "./cellCommentsContext";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { nativeFormSurfaceBounds, type NativeFormService } from "@/lib/nativeFormService";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import { useProductToast } from "@/lib/productToast";
import { userErrorToast } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { BoardView } from "./BoardView";
import { useNativeBoardView } from "./useNativeBoardView";
import { RowPageWhileReading, useRowPageWhileReading } from "./rowPageWhileReading"; import { useRowPageReads } from "./useRowPageReads";
import { ViewSearchHighlight } from "./ViewSearchHighlight";
import { DatabaseView, type DatabaseViewOperationContext, type DatabaseViewOpenMode, type DatabaseViewSnapshot } from "./DatabaseView";
import { DatabaseViewRenderer, type DatabaseViewRenderers } from "./DatabaseViewRenderer";
import { FormView } from "./FormView";
import { NativeCardDatabaseViewMount } from "./NativeCardDatabaseViewMount";
import { NativeDatabaseToolbarMount } from "./NativeDatabaseToolbarMount";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { newRowPageProperty } from "./rowPagePropertyAdd";
import type { DatabaseToolbarCommands } from "./databaseToolbarModel";
import { DatabaseRowsPreviewContext } from "./databaseRowsPreview";
import previewStyles from "./databaseRowsPreview.module.css";
import { NativeTableViewMount } from "./NativeTableViewMount";
import { RELATION_CREATION_BOUNDS, relationDatabaseOptions } from "@/lib/relationPropertyCreationModel";
import { NativeTimelineViewMount } from "./NativeTimelineViewMount";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { DatabaseArchivedRowsContext } from "./databaseArchivedRowsContext";
import { ArchivedRowsBar, useArchivedRowsMode } from "./DatabaseArchivedRowsMode";
import { DatabaseSourceTrashedBanner } from "./databaseArchive";
import { BoardGroupsContext, createBoardGroupsStore } from "./boardGroupSettings";
import { databaseAutomationEditorPages, databaseAutomationEditorProperties, databaseAutomationEditorViews } from "./databaseAutomationPanelModel";
import { databaseFilterGraphIdentity, graphRowsIdentity, nativeDatabaseFilterSnapshot, nativeDatabaseHydratedRelationTargetIds, type DatabaseFilterGraph } from "./databaseFilterSnapshot";
import { createPageAnalyticsPanelService } from "@/lib/pageAnalyticsPanelService";
import { CommentsPanel } from "../CommentsPanel";
import type { OpenCommentsDetail } from "../comments/commentPanelEvents";
import { PageAnalyticsObserver } from "../page/PageAnalyticsObserver";
import { PublicDatabaseRowPage } from "./PublicDatabaseRowPage";
import { RowPageUnavailable } from "./RowPageUnavailable";
import { databaseViewLink, databaseViewLinkId, effectiveOpenPageIn } from "./databaseViewShared";
import { galleryPresentationPage } from "./galleryViewModel";
import type { PropertyValueReadContext } from "./PropValue";
import { DatabaseViewControlsContext } from "./DatabaseViewControls";
import { DatabasePropertySettingsContext } from "./databasePropertySettings";
import { useDatabaseViewController } from "./useDatabaseViewController";
import { useDatabasePageLayoutBuilder } from "./useDatabasePageLayoutBuilder";
import { usePropertyAccessDialog } from "./usePropertyAccessDialog";
import { OpenDatabasePagesContext, RowsOpenInContext } from "./databaseRowOpeners";
import { RollupPageOpenContext } from "./RollupPageLinks";
import { RelatedPageOpenContext } from "./relatedPageOpen";
import { useNativePropertyCells } from "./useNativePropertyCells";
import { useViewMountRowMenu } from "./useViewMountRowMenu";
import { useDatabasePersonalQueries } from "./useDatabasePersonalQueries";
import { useSaveViewAsNew } from "./useSaveViewAsNew";
import { DatabaseFilterBarContext, useDatabaseFilterBarState } from "./databaseFilterBar";
import { useRelativeSprintsVersion } from "./databaseSprintContext";
import { serverUrl } from "@/lib/serverOrigin";
import { requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { DatabaseRowCommentKey } from "./DatabaseRowCommentKey";
import { useRowPeekSiblings, viewFirstShownRowId } from "./rowPeekSiblings";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { saveHeldRowTitle, useRowPageRead } from "./useRowPageRead";
import { rowPageMadeHere } from "@/lib/rowMadeHere";
import { useRowBodyEdits } from "@/lib/rowBodyEdits"; import type { AutomationSurface } from "./DatabaseToolbarMenus";
import { useWikiSourceShape } from "./wikiSourceShape"; import { useRowPageWithDatabaseUnlocked } from "./rowPageDatabaseUnlockedForMe"; import { DatabaseSourceRules, OutsideDatabaseSourceRules } from "./databaseSourceRules";

const boardSchemaReadOwner = Object.freeze({ request: () => false, retire: () => {} });

export type NativeDatabaseViewMountProps = {
  block: GraphBlock;
  source: GraphDataSource;
  displayTitle: string;
  dataSources: GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  views: GraphView[];
  dashboardWidgets?: GraphView[];
  activeView: GraphView;
  properties: GraphProperty[];
  rows: GraphRow[];
  boardAdapter?: NativeBoardProductAdapter;
  databaseViewAdapter: NativeDatabaseViewProductAdapter;
  tableAdapter?: NativeTableProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
  pageId: string;
  workspaceId: string;
  session: Session | null;
  formService?: NativeFormService;
  onAddProperty?: (source: GraphDataSource, type: GraphProperty["type"], name: string, config?: Record<string, unknown>) => Promise<GraphProperty | null>;
  onConfiguredView?: (view: GraphView) => void;
  configurationReady?: boolean;
  readOnly?: boolean;
  /**
   * Its pages shown to read only, its view still set: a layout builder's tab,
   * as Notion's preview (seen 2026-11-12: no page made, opened, moved or
   * edited in any layout, its filter, sort and settings at work).
   */
  preview?: boolean;
  /** Its pages read only, still opened, while its views are written: People for its owner (PeopleDatabaseView). */
  rowsOnlyRead?: boolean;
  staticReadOnlySemantics?: boolean;
  /** A dashboard widget (or Home's "내 작업") draws only its view, without tabs, toolbar or filter bar. */
  embedded?: boolean;
  /** A widget's head, where its tools go: a dashboard's in its mode, Home's "내 작업" ("home"). */
  widgetHead?: { slot: HTMLElement; mode: "view" | "edit" | "home" } | null;
  /** An inline database's title row, over its views. */
  title?: ReactNode;
  /** Opens a row itself, as People opens a person's profile, in place of its page. */
  onOpenRow?: (rowId: string, mode: DatabaseViewOpenMode) => void;
  /** A list that is no data source of the workspace (People, or "tasks" for My tasks' rows): its view links its own address, and no automations but its own `listAutomations` (People's). */
  listOnly?: boolean | "tasks"; listAutomations?: AutomationSurface | null;
};

export function NativeDatabaseViewMount({
  block,
  source,
  displayTitle,
  dataSources,
  filterGraph,
  views,
  dashboardWidgets = [],
  activeView,
  properties: storedProperties,
  rows: storedRows,
  boardAdapter: boardWriter,
  databaseViewAdapter,
  tableAdapter: tableWriter,
  timelineAdapter: timelineWriter,
  pageId: containerPageId,
  workspaceId,
  session,
  formService,
  onAddProperty,
  onConfiguredView,
  configurationReady = true,
  readOnly: readOnlyHere = false,
  preview = false, rowsOnlyRead = false,
  staticReadOnlySemantics = false,
  embedded = false,
  widgetHead = null,
  title,
  onOpenRow,
  listOnly = false, listAutomations = null,
}: NativeDatabaseViewMountProps) {
	const pageId = source.pageId ?? containerPageId;
  // What its filters and new rows take as "this page": a layout tab's open page (FilterThisPageContext), else the page it sits in; in a template's body none: no row holds a template in a relation, so its views keep no row by it and one made there takes none, as Notion's shows them (seen 2026-11-20: its row there made, its relation shown empty; a refused one went, with two errors) — the template takes it on a two-way relation's other side instead (templateBodyRows.ts).
  const thisPage = useFilterThisPage(), filterPageId = thisPage?.template ? "" : thisPage?.rowId ?? containerPageId;
  // A source in the trash where it stands, or with its page, shows to read only (DatabaseSourceTrashedBanner).
  const sourceInTrash = databaseSourceInTrash(block, source), sourcePageInTrash = source.sourcePageInTrash === true;
  // Its page locked or unlocked, each cell, card and control turns read only
  // or back behind the press's frame, in pieces a newer press passes, not in
  // it (2026-11-13: three tables of 140 rows held "페이지 잠금" 420–620 ms).
  const readOnly = useDeferredValue(readOnlyHere) || sourcePageInTrash || sourceInTrash;
  // What its pages allow: nothing in a preview or where only its views are written, its view still set as `readOnly` lets it.
  const rowsReadOnly = readOnly || preview || rowsOnlyRead;
  const { t } = useTranslation(["rowMenu", "pageTreeItem", "databaseView", "tableView", "pageArchive"]);
  // A locked database keeps its rows editable but not its views and properties.
  const structureLocked = useDatabaseStructureLocked();
  // The toolbar reads the source's task features; the table keeps a toggle's room while sub-items are on, a list nests them and cards follow their view's option.
  const [loadedSubitems, setSubitems] = useState<DatabaseSubitemsFeature | null>(null);
  // A wiki's views show its pages flat, without the pair that nests them (wikiSourceShape).
  const { rows: sourceRows, properties, subitems } = useWikiSourceShape(source.id, storedRows, storedProperties, loadedSubitems);
  // Views show the rows that are not archived, or only the archived ones
  // while "보관된 페이지 보기" is on for the view; archived rows stay openable.
  const { active: activeRows, archived: archivedRows } = useMemo(() => splitArchivedDatabaseRows(sourceRows), [sourceRows]);
  const archivedMode = useArchivedRowsMode(activeView.id);
  const rows = archivedMode.viewing && !embedded ? archivedRows : activeRows;
  const { notify } = useProductToast();
  // A change the server didn't get waits in the page's outbox, which the
  // connection bar says once; anything else that failed is said here.
  const viewFailed = (error?: unknown) => {
    const text = userErrorToast(error, t("databaseView:toast.couldntSaveChanges"));
    if (text) notify(text, "error");
  };
  const [toolbarSearch, setToolbarSearch] = useState("");
  const [viewControlsSlot, setViewControlsSlot] = useState<HTMLElement | null>(null);
  const [formBarSlot, setFormBarSlot] = useState<HTMLElement | null>(null);
  const [taskSettings, setTaskSettings] = useState<boolean | null>(null), [taskRoles, setTaskRoles] = useState<ReadonlySet<string>>(() => new Set());
  const subitemsOn = subitems !== null, taskDatabase = taskSettings ?? source.taskDatabase === true; // settings unread (offline): the read's mark
  const cardPageContent = useMemo(() => session ? createCardPageContent(session) : null, [session]);
  // An inline database is a piece of its pages' path, as Notion's.
  const ownPage = useDatabaseOwnPage(), fullPageHref = useDatabaseFullPageLink();
  // Its own page's title, which alone of the page list draws it again (productPagesStore.tsx): a database page's; an inline one shown alone (a peek, "전체 페이지로 열기", a home's pin) is its block's name, as Notion's builder (seen 2026-11-18).
  const ownPageTitle = useProductPagesSelect((pages) => (ownPage ? pages?.pages.find((page) => page.id === pageId && page.kind === "database") : undefined)?.title);
  const pathDatabase = useMemo(() => !ownPage && fullPageHref ? {
    blockId: block.id, title: displayTitle, href: fullPageHref, titleSpans: source.titleSpans as TextSpan[] | undefined,
    icon: { icon: source.icon ?? "", iconType: source.iconType ?? "none" },
  } : undefined, [block.id, displayTitle, fullPageHref, ownPage, source.icon, source.iconType, source.titleSpans]);
  // The filters and sorts a viewer sets apply for them alone, by view, until
  // an editor saves them for everyone; a dashboard widget's changes save.
  const savedViews = useMemo(() => views.some((view) => view.id === activeView.id) ? views : [...views, activeView],
    [activeView, views]);
  // Only where others can open the database: on a page the reader alone opens, and on My tasks, the person's own, a change saves
  // to the view, as Notion's (seen 2026-11-02; My tasks 2026-11-24 in two tabs: no "모두에게 저장", a filter changed stayed after a
  // reload) — but a locked one's views take no change, so its filters and sorts stay the reader's own, kept here, there too.
  const onlyReader = usePageOnlyReader(session, listOnly ? undefined : containerPageId ?? pageId) || listOnly === "tasks";
  const personal = useTemplateBodyRows(thisPage, source, activeView, properties, useDatabasePersonalQueries({ // A row its views make in a template's body is the template's too (templateBodyRows.ts).
    views: savedViews, enabled: !embedded && (onlyReader !== true || structureLocked), userId: session?.user.id,
    tableAdapter: tableWriter, boardAdapter: boardWriter, timelineAdapter: timelineWriter, onStatus: notify,
  }));
  const { tableAdapter, boardAdapter, timelineAdapter } = useDefaultTemplateRows({ session, source, view: activeView, rows, properties, ...personal });
  // Its pages without a name go by the name it keeps for them, else by its untitled default template's (the live one its toolbar offers, else the read's), else by its kind's word, as its rows' paths name them (untitledRowNames.ts).
  const templatesShown = useDatabaseTemplates(source.id);
  const rowTemplateName = source.untitledName?.trim() || databaseUntitledName(templatesShown) || source.untitledRowName?.trim() || databaseKindRowName({ ...source, taskDatabase });
  // Once shown, a board, calendar or timeline keeps the default or fallback it draws.
  useShownViewFallback(activeView, properties, readOnly || structureLocked ? undefined : tableWriter?.viewShown);
  const { latest: latestRowPage, set: setRowPageSnapshot, of: rowPageOf } = useRowPageReads();
  const analyticsService = useMemo(() => (session ? createPageAnalyticsPanelService(session) : null), [session]);
  const [commentsRow, setCommentsRow] = useState<{ rowId: string; detail?: OpenCommentsDetail } | null>(null);
  // A table header's "⋯" opens the toolbar's properties menu beside itself.
  const toolbarCommands = useRef<DatabaseToolbarCommands | null>(null);
  // The area dialog's "하위 항목 켜기" opens the view settings at sub-items, a start band's "맞춤 설정" at their head.
  useInlineDatabaseRequests(`settings:${block.id}`, (request) => {
    if (request === "subitems" || request === "view") toolbarCommands.current?.openViewSettings(request === "view" ? undefined : request);
  }, { blockId: block.id, alone: ownPage });
  // A column menu's "filter", or a property picked from the toolbar's, opens
  // that property's pill in the filter bar, which the viewer can hide; a
  // phone has no bar and opens it in the toolbar's filter sheet.
  const barState = useDatabaseFilterBarState(activeView, toolbarCommands, personal.hasOwnQuery(activeView.id)), filterBarSurface = barState.surface;
  // A sprint relation's "현재" and its groups read the sprints the toolbar learns after.
  const sprintsVersion = useRelativeSprintsVersion();
  const snapshotIdentity = useMemo(() => JSON.stringify([source, views, properties, sprintsVersion]) + graphRowsIdentity(rows), [properties, rows, source, sprintsVersion, views]);
  const contextIdentity = `${session?.user.id ?? "anonymous"}:${workspaceId}:${pageId}:${source.id}:${block.id}`;
  const propertyValueSnapshotIdentity = useMemo(
    () => databaseFilterGraphIdentity({ properties: filterGraph.properties, rows: filterGraph.rows }),
    [filterGraph.properties, filterGraph.rows],
  );
  const propertyValueSnapshot = useMemo(() => nativeDatabaseFilterSnapshot({
    currentUserId: session?.user.id ?? "",
    graph: filterGraph,
    snapshotIdentity: propertyValueSnapshotIdentity,
    workspaceId,
  }), [filterGraph, propertyValueSnapshotIdentity, session?.user.id, workspaceId]);
  const hydratedRelationTargetIds = useMemo(
    () => nativeDatabaseHydratedRelationTargetIds(filterGraph),
    [filterGraph],
  );
  const rowPreviewPropertyValueContext = useMemo<PropertyValueReadContext>(() => ({
    bounds: nativeBoardQueryBounds.formula,
    contextIdentity,
    hydratedRelationTargetIds,
    pagesById: propertyValueSnapshot.pagesById as PropertyValueReadContext["pagesById"],
    propsByDatabaseId: propertyValueSnapshot.propertiesByDatabase as PropertyValueReadContext["propsByDatabaseId"],
    schemaReadOwner: boardSchemaReadOwner,
    snapshotIdentity: propertyValueSnapshot.snapshotIdentity,
    userId: session?.user.id,
  }), [contextIdentity, hydratedRelationTargetIds, propertyValueSnapshot, session?.user.id]);
  const generation = useRef({ identity: "", value: 0 });
  if (generation.current.identity !== snapshotIdentity) {
    generation.current = { identity: snapshotIdentity, value: generation.current.value + 1 };
  }
  // A database page opened from the view edits its properties as the
  // view's cells do.
  const { propertyCellEnvironment: rowPageCellEnvironment } = useNativePropertyCells({
    adapter: rowsReadOnly ? undefined : tableAdapter, contextIdentity, currentUserId: session?.user.id, dataSource: source, dataSources,
    filterGraph, pageId, properties, requestGeneration: generation.current.value, rows, workspaceId,
  });
  const trashedRowIds = useTrashedAddressRow(session, source.id, sourceRows, !onOpenRow);
  const snapshot: DatabaseViewSnapshot = {
    snapshotIdentity, contextIdentity, requestGeneration: generation.current.value, placement: "inline", block, dataSource: source,
    layoutPageId: pageId, holderPageId: containerPageId ?? pageId, displayTitle, views, activeView, properties, rows, archivedRows, trashedRowIds, readOnly: rowsReadOnly,
  };
  // A widget opens rows for itself; a dashboard showing widgets opens none.
  const showsWidgets = !embedded && dashboardWidgetRows(activeView).length > 0;
  // A published page opens its database's pages as public pages.
  const publicHref = usePublicPageHref();
  const publicRows = !session && publicHref ? publicHref : undefined;
  const controller = useDatabaseViewController(snapshot, databaseViewAdapter, viewFailed,
    embedded || showsWidgets ? String(activeView.config.dashboardViewId || activeView.id) : undefined, publicRows, embedded, Boolean(onOpenRow));
  // The views inside the database page this view opened don't open it again.
  const openPages = useContext(OpenDatabasePagesContext);
  const commentAccess = useContext(DatabaseCommentAccessContext);
  const openedRowId = controller.rowSelection?.rowId, rowPageSnapshot = openedRowId ? rowPageOf(openedRowId) : latestRowPage;
  const openedPages = useMemo(() => openedRowId ? [...openPages, openedRowId] : openPages, [openPages, openedRowId]);
  // A view that opens its rows itself neither opens nor reads a row page for the one in the address (useDatabaseViewController).
  const opensRowsItself = Boolean(onOpenRow);
  const rowPageService = useMemo(
    () => session && !opensRowsItself ? createDatabaseRowPageService(session, setRowPageSnapshot) : null,
    [opensRowsItself, session],
  );
  useEffect(() => () => rowPageService?.dispose(), [rowPageService]);
  useEffect(() => setToolbarSearch(""), [activeView.id]);
  const rowPageUnanswered = useRowPageRead(controller.rowSelection?.rowId, rowPageService, rows, () => setRowPageSnapshot(null), () => {
    controller.closeRow();
    notify(t("tableView:rowLoadErrorTitle"), "error");
  }, (rowId, text, rowsNow) => saveHeldRowTitle(rowsReadOnly ? undefined : tableAdapter, rowsNow,
    properties.find((property) => property.dataSourceId === source.id && property.type === "title")?.id, rowId, text, () => true),
  // A row made here opens at once, before the server has it (rowMadeHere.ts).
  (row) => { if (session) setRowPageSnapshot(rowPageMadeHere(session, row, properties, source.pageId ?? pageId, workspaceId)); });
  // A page that was open may have changed: its cards' "페이지 콘텐츠" reads it again.
  useEffect(() => {
    if (!openedRowId || !cardPageContent) return;
    return () => cardPageContent.changed(openedRowId);
  }, [cardPageContent, openedRowId]);
  // An opened row page is a visit, as a page is: the library's "최근" lists it.
  usePageVisitRecorder(session, rowPageSnapshot?.row.id === openedRowId ? openedRowId ?? null : null);

  const operationContext = (requestKey: string): DatabaseViewOperationContext => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    requestGeneration: snapshot.requestGeneration,
    requestKey,
    signal: controller.lifecycle.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.activeView.id,
  });
  // A row made just now opens once the view has drawn it: by the view as it is then.
  const openRowNow = useRef((_rowId: string, _mode: DatabaseViewOpenMode, _fromMenu?: boolean) => {});
  openRowNow.current = (rowId, mode, fromMenu) => preview ? undefined : onOpenRow ? onOpenRow(rowId, mode)
    : controller.operations.openRow(rowId, mode, operationContext(`row:open:${rowId}:${mode}`), fromMenu);
  const openRow = (rowId: string, mode: DatabaseViewOpenMode = "side", fromMenu = false) => openRowNow.current(rowId, mode, fromMenu);
  const openRowAtSide = useCallback((rowId: string) => openRowNow.current(rowId, "side"), []);
  // A relation's page pressed on the opened row's page opens in its peek, in its way (relatedPageOpen.ts).
  const rowPeekMode = useRef<DatabaseViewOpenMode>("side");
  rowPeekMode.current = controller.rowSelection?.mode === "center" ? "center" : "side";
  const openRowInPeek = useCallback((rowId: string) => openRowNow.current(rowId, rowPeekMode.current), []);
  const selectedRowId = controller.rowSelection?.rowId ?? rowPageSnapshot?.row.id ?? "";
  const selectedRow = selectedRowId ? sourceRows.find(({ id }) => id === selectedRowId) : undefined;
  const titleProperty = properties.find(({ type }) => type === "title");
  const selectedRowIndex = selectedRowId ? sourceRows.findIndex(({ id }) => id === selectedRowId) : -1;
  // A row in trash is out of the views; its page shows what the row page read.
  const trashedRow = rowPageSnapshot?.row.id === selectedRowId && rowPageSnapshot.row.trashedAt
    ? rowPageSnapshot.row : null;
  const presentedRow = selectedRow ?? (trashedRow ? { ...trashedRow, values: trashedRow.values as GraphRow["values"] } : undefined);
  // The same page while its row is, so the opened page's cells are drawn again only for their row's changes.
  const selectedPresentationRow = useMemo(() => presentedRow ? galleryPresentationPage({
    row: presentedRow, titleProperty, workspaceId, position: Math.max(0, selectedRowIndex),
  }) : undefined, [presentedRow, selectedRowIndex, titleProperty, workspaceId]);
  const selectedValueRow = propertyValueSnapshot.pagesById[selectedRowId] ?? selectedPresentationRow;
  const shownRowPage = useRowPageWhileReading(rowPageSnapshot, controller.rowSelection, selectedRow, properties, source.pageId ?? pageId, workspaceId, rowPageUnanswered), drawnRowPage = useRowPageWithDatabaseUnlocked(shownRowPage);
  const rowTitle = titleProperty && selectedRow
    ? String(selectedRow.values[titleProperty.id]?.value ?? "")
    : "";
  // What a view its search or filters leave empty offers: its filters and clearing the search.
  const clearToolbarSearch = useCallback(() => setToolbarSearch(""), []);
  const noResults = useViewNoResultsCommands(toolbarCommands, clearToolbarSearch, embedded ? widgetHead?.slot ?? null : null, filterBarSurface);
  const openPropertySettings = useCallback((propertyId: string) => toolbarCommands.current?.openPropertySettings(propertyId), []);
  // "새 보기로 저장": the viewer's own filters and sorts as a new view.
  const saveViewAsNew = useSaveViewAsNew({ snapshot, operations: controller.operations, signal: controller.lifecycle.signal, effectiveView: personal.effectiveView,
    resetOwnQuery: personal.resetOwnQuery, setOwnQuery: personal.setOwnQuery, openSettings: () => toolbarCommands.current?.openViewSettings() });
  // A relation a table's header makes finds its databases by name.
  const searchRelationDatabases = useMemo(
    () => session && !readOnly ? createRelationDatabaseSearch(session, workspaceId) : undefined,
    [readOnly, session, workspaceId],
  );
  const { rowActions, rowMenu, rowWritable, analyticsDocked, sidePanel, rowPageWritable, rowPageReadOnly } = useViewMountRowMenu({ session, service: rowPageService, reread: () => databaseViewAdapter.dataSources?.reread(),
    rowPageSnapshot, menuRowPage: latestRowPage, rowPageOf, tableAdapter, closeRow: controller.closeRow, closeRowAtPress: controller.closeRowAtPress, analyticsService, pathDatabase,
    workspaceId, pageId, source, dataSources, readOnly, rowsReadOnly, structureLocked, listOnly, selectedRowId, rowTitle, rows, properties, titleProperty,
    activeView, cellEnvironment: rowPageCellEnvironment, toolbarCommands, openRow, rowPageFull: controller.rowSelection?.mode === "full", rowPageAll: { open: Boolean(commentsRow?.detail?.all), close: () => setCommentsRow((open) => open?.detail?.all ? null : open) }, holderPageId: containerPageId, readOnlyHere, rowPageHeld: preview || rowsOnlyRead || staticReadOnlySemantics || !session || sourcePageInTrash || sourceInTrash });
  const { setArchived: setRowArchived, changePresentation: changeRowPresentation, changeLock: changeRowLock, changeTrash: changeRowTrash,
    deleteForever: deleteRowForever, copyLink: copyRowLink, setFavorite: setRowFavorite } = rowActions;
  const boardGroups = useMemo(createBoardGroupsStore, []);

  const { effectiveView } = personal;
  const toolbarView = useMemo(() => {
    const seen = effectiveView(activeView);
    return { ...seen, config: { ...seen.config, search: toolbarSearch || undefined } };
  }, [activeView, effectiveView, toolbarSearch]);
  useDatabaseDocumentViewName({ block, views, dashboardWidgets, activeView, ownPage, listOnly });
  const peekSiblings = useRowPeekSiblings({ rowId: controller.rowSelection?.rowId, rows, properties, view: toolbarView,
    currentUserId: session?.user.id, pageId: filterPageId ?? pageId, workspaceId, subitems: subitemsOn, lendTo: onOpenRow && openRow });
  // A signed-in reader reads the automations; only an editor changes them.
  const automationService = useMemo(() => session && !listOnly
    ? createDatabaseAutomationService(api, session, pageId, source.id)
    : null, [listOnly, pageId, session, source.id]);
  const automation = listAutomations ?? (automationService ? {
    readOnly,
    databaseId: source.id,
    automationCount: source.automationCount,
    editorPages: databaseAutomationEditorPages(workspaceId, dataSources),
    properties: databaseAutomationEditorProperties(properties),
    // A relation's value names rows its database's view already reads, as its cell offers them.
    relationRows: (dataSourceId: string) => Object.values(rowPageCellEnvironment?.snapshot.pagesById ?? {})
      .filter((page) => page.parentType === "database" && page.parentId === dataSourceId && !page.inTrash && !page.archivedAt)
      .sort((left, right) => left.position - right.position),
    service: automationService, currentUserId: session?.user.id,
    views: databaseAutomationEditorViews(views),
  } : null);

  const { snapshot: boardSnapshot, operations: boardOperations } = useNativeBoardView({ block, source, activeView, view: toolbarView, properties, rows,
    snapshotIdentity, contextIdentity, requestGeneration: snapshot.requestGeneration, workspaceId, containerPageId: filterPageId, currentUserId: session?.user.id,
    subitems, propertyValueContext: rowPreviewPropertyValueContext, filterSnapshot: propertyValueSnapshot, cellEnvironment: rowPageCellEnvironment,
    readOnly: Boolean(rowsReadOnly), boardAdapter, tableAdapter, noResults, rowMenu, toolbarCommands, openRow, changeRowTrash, onError: viewFailed });

  const cardView = <NativeCardDatabaseViewMount snapshot={{ ...snapshot, activeView: toolbarView }}
    session={session} currentUserId={session?.user.id} workspaceId={workspaceId} pageId={pageId} contextPageId={filterPageId} tableAdapter={tableAdapter}
    relationValueContext={rowPreviewPropertyValueContext} subitems={subitems}
    onAddProperty={onAddProperty ? (type, name, config) => onAddProperty(source, type, name, config) : undefined}
    onError={viewFailed} onOpenRow={openRow}
    openRowContextMenu={rowMenu.openRowContextMenu} openRowContextMenuFromElement={rowMenu.openRowContextMenuFromElement}
    rowContextMenu={rowMenu.rowContextMenu} noResults={noResults}
    onOpenGroupSettings={() => toolbarCommands.current?.openViewSettings("group")}
    trashRows={rowWritable ? async (rowIds) => { await changeRowTrash(rowIds, true); } : undefined}
    duplicateRows={rowWritable && tableAdapter ? (rowIds, place) => tableAdapter.duplicateRows(rowIds, () => true, activeView, place) : undefined} propertyCellEnvironment={rowsReadOnly ? undefined : rowPageCellEnvironment}
    chartRows={{ filterSnapshot: propertyValueSnapshot, sourceContainerId: boardSnapshot.sourceContainerId ?? block.id }} />;
  const quickFilterAdapter = tableAdapter ?? boardAdapter ?? timelineAdapter;
  const viewWritable = !readOnly && !structureLocked && Boolean(session && quickFilterAdapter);
  const ownQuery = !structureLocked && personal.hasOwnQuery(activeView.id) ? { saved: activeView, onReset: () => personal.resetOwnQuery(activeView.id),
    onSaveForEveryone: viewWritable ? () => personal.saveForEveryone(activeView.id) : undefined, onSaveAsNewView: viewWritable ? () => void saveViewAsNew(activeView) : undefined } : null;
  const tableView = <NativeTableViewMount noResults={noResults} adapter={tableAdapter} currentUserId={session?.user.id} subitemsEnabled={subitemsOn} taskDatabase={taskDatabase}
    subitemPair={subitems ? { parentPropertyId: subitems.parentPropertyId, childrenPropertyId: subitems.childrenPropertyId, onTitle: subitems.showToggleOnTitle === true } : undefined}
    trashRows={rowWritable ? async (rowIds) => { await changeRowTrash(rowIds, true); } : undefined}
    dataSource={source} dataSources={dataSources} filterGraph={filterGraph}
    onAddProperty={onAddProperty ? (type, name, config) => onAddProperty(source, type, name, config) : undefined}
    searchRelationDatabases={searchRelationDatabases}
    // A table head's "⋯" opens the view settings at "속성 표시 여부", docked at the side, as Notion's (seen 2026-11-06).
    onOpenProperties={() => toolbarCommands.current?.openViewSettings("properties")}
    onOpenGroupSettings={() => toolbarCommands.current?.openViewSettings("group")}
    onFilterProperty={viewWritable ? filterBarSurface.add : undefined}
    onError={viewFailed} onOpenRow={(rowId, mode) => openRow(rowId, mode === "default" ? effectiveOpenPageIn(toolbarView) : mode)} onCloseRow={controller.closeRowAtPress}
    openRowContextMenu={rowMenu.openRowContextMenu} openRowContextMenuFromElement={rowMenu.openRowContextMenuFromElement}
    openRowsContextMenuAt={rowMenu.openRowsContextMenuAt}
    pageId={pageId} contextPageId={filterPageId} properties={properties} readOnly={rowsReadOnly} rowContextMenu={rowMenu.rowContextMenu} rows={rows}
    staticReadOnlySemantics={staticReadOnlySemantics} view={toolbarView} workspaceId={workspaceId} />;
  const timelineView = <NativeTimelineViewMount adapter={timelineAdapter} cellAdapter={tableAdapter} currentUserId={session?.user.id}
    dataSource={source} dataSources={dataSources} filterGraph={filterGraph}
    onOpenGroupSettings={() => toolbarCommands.current?.openViewSettings("group")}
    trashRows={rowWritable ? async (rowIds) => { await changeRowTrash(rowIds, true); } : undefined}
    duplicateRows={rowWritable && tableAdapter ? (rowIds, place) => tableAdapter.duplicateRows(rowIds, () => true, activeView, place) : undefined}
    onError={viewFailed} onOpenRow={(rowId, mode) => openRow(rowId, mode === "default" ? effectiveOpenPageIn(toolbarView) : mode)} onCloseRow={controller.closeRowAtPress}
    openRowContextMenu={rowMenu.openRowContextMenu} openRowContextMenuFromElement={rowMenu.openRowContextMenuFromElement}
    pageId={pageId} contextPageId={filterPageId} properties={properties} readOnly={rowsReadOnly} rowContextMenu={rowMenu.rowContextMenu}
    rows={rows} subitems={subitems} view={toolbarView} workspaceId={workspaceId} />;
  const formView = <FormView bounds={nativeFormSurfaceBounds} dataSource={source} formService={formService}
    onAddProperty={onAddProperty} onConfiguredView={onConfiguredView} pageId={pageId} properties={properties}
    onUpdateProperty={tableAdapter && !readOnly && !structureLocked ? (property) => {
      const owner = generation.current.value;
      tableAdapter.commitProperties([property], () => generation.current.value === owner)
        .catch(viewFailed);
    } : undefined}
    readOnly={readOnly || structureLocked || !session} view={toolbarView} barSlot={embedded ? null : formBarSlot}
    respondent={session ? { id: session.user.id, name: session.user.name } : undefined}
    propertyEditing={!readOnly && !structureLocked && session && tableAdapter ? {
      contextIdentity, dataSource: source, dataSources, properties, snapshotIdentity, view: toolbarView, writable: true,
      propertiesByDataSourceId: rowPageCellEnvironment?.snapshot.propertiesByDataSourceId,
      onCommitProperties: (next) => persistWithGeneration((isCurrent) => tableAdapter.commitProperties(next, isCurrent)),
      // A question turned back to the title lets the property it leaves go (FormView).
      onDeleteProperty: (propertyId) => persistWithGeneration((isCurrent) => tableAdapter.deleteProperties([propertyId], isCurrent)),
    } : undefined} />;
  // A dashboard lays out its widgets, each drawn as its own view of its source.
  const dashboardEditing = !listOnly && !readOnly && !structureLocked && session ? dashboardWidgetEditing({
    block, dashboard: activeView, dataSources, views: [...views, ...dashboardWidgets], adapter: databaseViewAdapter }) : undefined;
  // The dashboard's own filters pick its widgets' sources' properties (dashboardFilterSources.ts) and narrow each widget of the source they filter (dashboardFilter.ts).
  const dashboardSources = useMemo(() => !embedded && activeView.type === "dashboard" ? dashboardFilterSources(block, dashboardWidgets, dataSources, filterGraph.properties) : null, [activeView, block, dashboardWidgets, dataSources, embedded, filterGraph.properties]);
  const dashboardKept = useMemo(() => new Map((dashboardSources ?? []).map(({ source: shown }) => [shown.id, dashboardKeptRowIds(toolbarView, shown.id === source.id ? rows : filterGraph.rows.filter((row) => row.dataSourceId === shown.id), propertyValueSnapshot, containerPageId ?? pageId, shown.id)])),
  [containerPageId, dashboardSources, filterGraph.rows, pageId, propertyValueSnapshot, rows, source.id, toolbarView]);
  const renderWidget = (widget: GraphView, head?: { slot: HTMLElement; mode: "view" | "edit" } | null) => {
    const widgetSource = dataSources.find((candidate) => candidate.id === widget.dataSourceId);
    if (!widgetSource) return null;
    const kept = dashboardKept.get(widget.dataSourceId) ?? null, narrowed = Boolean(kept);
    return <DashboardFilteredContext.Provider value={narrowed}><NativeDatabaseViewMount embedded block={block} source={widgetSource} displayTitle={widget.name}
      dataSources={dataSources} filterGraph={filterGraph} views={[widget]} activeView={widget}
      properties={filterGraph.properties.filter((property) => property.dataSourceId === widget.dataSourceId)}
      rows={filterGraph.rows.filter((row) => row.dataSourceId === widget.dataSourceId
        && (!kept || kept.has(row.id)))}
      boardAdapter={boardWriter} databaseViewAdapter={databaseViewAdapter} tableAdapter={tableWriter}
      timelineAdapter={timelineWriter} pageId={containerPageId} workspaceId={workspaceId} session={session}
      formService={formService} onAddProperty={onAddProperty} readOnly={readOnly}
      staticReadOnlySemantics={staticReadOnlySemantics} widgetHead={head ?? null} /></DashboardFilteredContext.Provider>;
  };
  const dashboardView = !embedded ? <DatabaseDashboardWidgets key={activeView.id} dashboard={activeView} widgets={dashboardWidgets}
    editing={dashboardEditing} barSlot={formBarSlot} holderPageId={containerPageId ?? pageId} renderWidget={renderWidget} /> : cardView;
  const renderers: DatabaseViewRenderers = {
    table: tableView, board: <BoardView snapshot={boardSnapshot} operations={boardOperations} readOnly={rowsReadOnly || !boardAdapter} />,
    list: cardView, calendar: cardView, gallery: cardView, chart: cardView, dashboard: dashboardView, feed: cardView, map: cardView,
    timeline: timelineView, form: formView,
    TableView: tableView, BoardView: <BoardView snapshot={boardSnapshot} operations={boardOperations} readOnly={rowsReadOnly || !boardAdapter} />,
    GalleryView: cardView, ListView: cardView, CalendarView: cardView, TimelineView: timelineView,
    ChartView: cardView, FormView: formView, DashboardView: dashboardView, FeedView: cardView, MapView: cardView,
  };
  const copyViewLink = async (viewId: string) => {
    if (await copyText(listOnly ? `${serverUrl(listOnly === "tasks" ? "/my-tasks" : "/people")}?v=${encodeURIComponent(viewId)}` : databaseViewLink(databaseViewLinkId({ pageId: containerPageId, blockId: block.id, ownPage }), viewId))) notify(t("databaseView:copiedViewLink"), "success");
    else notify(t("databaseView:copyViewLinkFailed"), "error");
  };
  // A database's own page has no "»" to fold its tools (Notion's, seen 2026-11-02).
  // A dashboard asks a widget for its settings, "보기 편집" or a widget just given its layout ("새 보기").
  const widgetSlot = embedded ? widgetHead?.slot ?? null : null;
  useEffect(() => {
    if (!widgetSlot) return;
    const open = (event: Event) => {
      const menu = (event as CustomEvent<"view" | "newView">).detail === "newView" ? "newView" : "view";
      const trigger = widgetSlot.querySelector<HTMLElement>("[data-database-settings-trigger]") ?? widgetSlot;
      toolbarCommands.current?.openMenu(menu, trigger);
    };
    widgetSlot.addEventListener(DASHBOARD_WIDGET_SETTINGS_EVENT, open);
    return () => widgetSlot.removeEventListener(DASHBOARD_WIDGET_SETTINGS_EVENT, open);
  }, [widgetSlot]);
  const databaseToolbar = <NativeDatabaseToolbarMount widget={embedded ? widgetHead?.mode : undefined} commandsRef={toolbarCommands} blockId={block.id} activeView={toolbarView} automation={automation} boardAdapter={boardAdapter}
    configurationReady={configurationReady} formBarSlotRef={setFormBarSlot} viewControlsSlotRef={setViewControlsSlot}
    dataSources={dataSources} filterGraph={filterGraph} pageId={pageId} contextPageId={filterPageId} properties={properties} readOnly={readOnly}
    rows={rows} search={toolbarSearch} session={session} source={source} tableAdapter={personal.tableAdapter} timelineAdapter={timelineAdapter}
    views={views} workspaceId={workspaceId} onCreateProperty={onAddProperty ? (type, name, config) => onAddProperty(source, type, name, config) : undefined}
    onError={viewFailed} onSearchChange={setToolbarSearch}
    onOwnQueryChange={personal.setOwnQuery} ownQuery={ownQuery} onTaskFeatures={(features) => { setSubitems(toolbarSubitemsFeature(features)); setTaskSettings(features ? features.tasks?.enabled === true : null); setTaskRoles(new Set<string>(features?.tasks?.enabled ? [features.tasks.statusPropertyId, features.tasks.assigneePropertyId, features.tasks.dueDatePropertyId].filter((id): id is string => Boolean(id)) : [])); }}
    onResetOwnQuery={structureLocked && personal.hasOwnQuery(activeView.id) ? () => personal.resetOwnQuery(activeView.id) : undefined}
    onRowCreated={(rowId, row) => { const way = effectiveOpenPageIn(toolbarView); requestPageTitleFocus(rowId); if (row && session && way !== "full") setRowPageSnapshot(rowPageMadeHere(session, row, properties, source.pageId ?? pageId, workspaceId)); openRow(rowId, way); }}
    onCopyViewLink={copyViewLink} {...(dashboardEditing ? {
      onTurnIntoDashboard: (view: GraphView) => databaseViewAdapter.turnViewIntoDashboard(block, view, () => true),
      onLeaveDashboard: (dashboard: GraphView, next: GraphView) => databaseViewAdapter.turnDashboardInto(dashboard, next, () => true),
    } : {})} />;
  // The open database page's lock, as its … menu and its bar's "Locked" change it; under a locked page, its own alone (rowPageUnderPageLock.ts).
  const lockedRow = rowPageSnapshot?.row, rowPage = { readOnly: rowPageReadOnly, writable: rowPageWritable };
  const rowLock = lockedRow && rowPage.writable && !trashedRow ? {
    locked: lockedRow.locked, unlockedForMe: lockedRow.unlockedForMe,
    onLock: (locked: boolean) => changeRowLock(() => rowPageService?.setLock(lockedRow.id, locked),
      t("toast.couldntChangePageLock")),
    onLockForMe: (unlocked: boolean) => void changeRowLock(() => rowPageService?.setLockForMe(lockedRow.id, unlocked, rowPageSnapshot),
      t("toast.couldntChangeLockForMe")),
  } : undefined;
  // A database page opened from the view changes the database's properties
  // as the view's settings do, under a locked page too (rowPageUnderPageLock.ts).
  const persistWithGeneration = (run: (isCurrent: () => boolean) => Promise<void>) => {
    const owner = generation.current.value;
    return run(() => generation.current.value === owner).catch(viewFailed);
  };
  useRowBodyEdits((rowId) => tableAdapter?.rowEditedHere?.(rowId));
  const rowPagePropertyEditing: DatabasePropertyEditing | undefined = !rowPage.readOnly && !structureLocked && session && tableAdapter ? {
    contextIdentity, dataSource: source, dataSources, properties, snapshotIdentity, view: activeView, writable: true,
    pagesById: rowPageCellEnvironment?.snapshot.pagesById,
    propertiesByDataSourceId: rowPageCellEnvironment?.snapshot.propertiesByDataSourceId,
    onCommitProperties: (next) => persistWithGeneration((isCurrent) => tableAdapter.commitProperties(next, isCurrent)),
    onUpdateView: (view) => persistWithGeneration((isCurrent) => tableAdapter.updateView(view, isCurrent)),
    onDeleteProperty: (propertyId) => persistWithGeneration((isCurrent) => tableAdapter.deleteProperties([propertyId], isCurrent)),
    // "속성 추가" makes a relation as the table's "+" does.
    relationCreation: onAddProperty ? {
      snapshot: { databases: relationDatabaseOptions(dataSources), snapshotIdentity },
      context: {
        bounds: RELATION_CREATION_BOUNDS, identity: contextIdentity, searchDatabases: searchRelationDatabases,
        notifyFailure: viewFailed,
        commit: (command) => onAddProperty(source, "relation", command.name, command.propertyConfig),
      },
    } : undefined,
  } : undefined;
  const rowPreviewContent = controller.rowSelection && publicRows && presentedRow && selectedValueRow
    ? <PublicDatabaseRowPage mode={controller.rowSelection.mode} row={presentedRow} properties={properties}
      containerPageId={pageId} containerTitle={source.title} workspaceId={workspaceId} valueRow={selectedValueRow}
      propertyValueContext={rowPreviewPropertyValueContext} onClose={controller.closeRow}
      onOpenMode={(mode) => openRow(presentedRow.id, mode)} />
    : controller.rowSelection && shownRowPage?.row.id === controller.rowSelection.rowId
    && selectedValueRow
    ? <>
      {session && rowPageSnapshot === shownRowPage ? <PageAnalyticsObserver input={{
        kind: "member", workspaceId, pageId: rowPageSnapshot.row.containerPageId, rowId: rowPageSnapshot.row.id, inTrash: false,
      }} /> : null}
      <RowPageWhileReading mode={controller.rowSelection.mode} containerTitle={source.title} pathDatabase={pathDatabase} properties={properties} snapshot={drawnRowPage!} sidePanelShown={analyticsDocked}
        propertyValueContext={rowPreviewPropertyValueContext} valueRow={selectedValueRow}
        editableRow={!rowPage.readOnly && tableAdapter && !trashedRow ? selectedPresentationRow : undefined}
        propertyCellEnvironment={!rowPage.readOnly && !trashedRow ? rowPageCellEnvironment : undefined}
        propertyEditing={!trashedRow ? rowPagePropertyEditing : undefined}
        onAddProperty={!trashedRow && rowPagePropertyEditing
          ? (type, name, icon) => {
            const property = newRowPageProperty(type, source.id, properties, name, icon);
            void rowPagePropertyEditing.onCommitProperties([property]);
            return property.id;
          }
          : undefined}
        onClose={controller.closeRow} onCreateTemplate={!readOnly ? () => void requestDatabaseTemplate(source.id) : undefined}
        onRichTextChange={!rowPage.readOnly && tableAdapter && selectedRow && !trashedRow ? (property, value, spans, read) => {
          const ownerGeneration = generation.current.value, isCurrent = () => generation.current.value === ownerGeneration;
          const row = { ...selectedRow, values: { ...selectedRow.values, [property.id]: { type: property.type, value, ...(spans?.length ? { spans } : {}) } } };
          // A title goes through the row's page, over the one it was typed on (rowValuesKept.ts); a text typed goes once its typing ends.
          if (property.type === "title" && tableAdapter.commitRowTitleAsRead) return tableAdapter.commitRowTitleAsRead(row, property.id, read ?? null, isCurrent);
          void tableAdapter.commitRows([row], isCurrent, true);
        } : undefined}
        onOpenMode={(mode) => openRow(shownRowPage.row.id, mode)}
        topBar={session ? {
          onChangeMode: (mode) => openRow(shownRowPage.row.id, mode),
          onChangeDefaultMode: viewWritable ? () => toolbarCommands.current?.openViewSettings("layout", true) : undefined,
          onCopyLink: () => void copyRowLink(shownRowPage.row.id),
          onFavorite: rowPageService ? (favorite) => void setRowFavorite(shownRowPage.row.id, favorite) : undefined,
          ...rowMenu.topBarUpdates(shownRowPage.row.id, analyticsDocked), onClosePanel: sidePanel.onClosePanel, discussionsDocked: sidePanel.discussionsDocked,
          lock: rowLock, siblings: { ...peekSiblings, onOpen: (rowId) => openRow(rowId, controller.rowSelection!.mode) },
        } : undefined}
        onOpenComments={session ? (detail) => setCommentsRow({ rowId: shownRowPage.row.id, detail }) : undefined}
        onOpenActions={session
          ? (trigger, extras) => rowMenu.openRowPageActions(shownRowPage.row.id, trigger, extras, rowLock)
          : undefined}
        onPresentationChange={rowPage.writable && !trashedRow ? (change) => changeRowPresentation(shownRowPage.row.id, change) : undefined}
        onHistoryRestored={() => {
          void rowPageService?.refresh(shownRowPage.row.id).then(() => databaseViewAdapter.dataSources?.reread()).catch(() => {});
        }}
        onBodyContentChange={(hasContent) => databaseViewAdapter.showRowContent(shownRowPage.row.id, hasContent)}
        session={session ?? undefined} workspaceId={workspaceId} bodyReadOnly={rowPage.readOnly || Boolean(trashedRow)}
        currentUserId={session?.user.id}
        onUnarchive={rowPage.writable && !trashedRow ? () => setRowArchived(shownRowPage.row.id, false) : undefined}
        onRestore={rowWritable && trashedRow ? () => changeRowTrash(trashedRow.id, false) : undefined}
        onDeleteForever={rowWritable && trashedRow ? () => deleteRowForever(trashedRow.id) : undefined} />
      {rowMenu.rowPageActionsMenu}
      {session && rowPageSnapshot && commentsRow?.rowId === rowPageSnapshot.row.id ? (
        <CommentsPanel key={`${commentsRow.rowId}:${commentsRow.detail?.all ? "all" : commentsRow.detail?.blockId ?? ""}:${commentsRow.detail?.activeCommentId ?? ""}:${commentsRow.detail?.compose ? "new" : ""}`}
          pageId={rowPageSnapshot.row.containerPageId} rowId={commentsRow.rowId} blockId={commentsRow.detail?.blockId} compose={commentsRow.detail?.compose}
          all={commentsRow.detail?.all} docked={sidePanel.discussionsDocked} workspaceId={workspaceId}
          activeCommentId={commentsRow.detail?.activeCommentId} initialQuote={commentsRow.detail?.initialQuote}
          threadIds={commentsRow.detail?.threadIds} initialQuoteStart={commentsRow.detail?.initialQuoteStart} initialQuoteEnd={commentsRow.detail?.initialQuoteEnd}
          initialQuotePlaces={commentsRow.detail?.initialQuotePlaces}
          session={session} canEditPage={rowPageSnapshot.accessRole === "edit" || rowPageSnapshot.accessRole === "full_access"}
          canComment={rowPageSnapshot.accessRole !== "view" && !rowPageSnapshot.row.trashedAt}
          onClose={() => setCommentsRow(null)} onStatus={(message) => notify(message, "error")} />
      ) : null}
    </>
    // A row whose page the server wasn't there to read opens as a page then says.
    : controller.rowSelection && rowPageUnanswered === controller.rowSelection.rowId
    ? <RowPageUnavailable mode={controller.rowSelection.mode} onClose={controller.closeRow}
      title={String(selectedRow && titleProperty ? selectedRow.values[titleProperty.id]?.value ?? "" : "")} />
    : undefined;
  // Its pages' layout tabs show their views with this graph and these writers.
  const tabEnvironment = useMemo(() => ({ dataSources, filterGraph, databaseViewAdapter, tableAdapter: tableWriter, boardAdapter: boardWriter,
    timelineAdapter: timelineWriter, pageId, workspaceId, session, readOnly, title: displayTitle }),
  [boardWriter, dataSources, databaseViewAdapter, displayTitle, filterGraph, pageId, readOnly, session, tableWriter, timelineWriter, workspaceId]);
  // A rollup's page pressed on the opened row's page opens beside it, in this view's peek (RollupPageLinks); its views open theirs their own way, named in the address.
  const rowPreview = rowPreviewContent ? <OutsideDatabaseSourceRules><OpenDatabasePagesContext.Provider value={openedPages}><RowsOpenInContext.Provider value={undefined}><DatabaseNamesAddressContext.Provider value>
    <RollupPageOpenContext.Provider value={openRowAtSide}><RelatedPageOpenContext.Provider value={openRowInPeek}>
      <PageLayoutTabProviders environment={tabEnvironment}>{rowPreviewContent}</PageLayoutTabProviders>
    </RelatedPageOpenContext.Provider></RollupPageOpenContext.Provider></DatabaseNamesAddressContext.Provider></RowsOpenInContext.Provider></OpenDatabasePagesContext.Provider></OutsideDatabaseSourceRules> : undefined;
  // "레이아웃 사용자 지정" lays out this database's pages on them (Notion's, seen 2026-10-12).
  // A Sprints database's status is kept as a task database's own properties are: its type stays and it isn't deleted (Notion's, seen 2026-11-07).
  const lockedRoles = useMemo(() => source.sprintStatusPropertyId ? new Set([...taskRoles, source.sprintStatusPropertyId]) : taskRoles,
    [source.sprintStatusPropertyId, taskRoles]);
  const builder = useDatabasePageLayoutBuilder(session && !readOnly && !rowsOnlyRead ? {
    session, pageId, dataSourceId: source.id, properties, propertyValueContext: rowPreviewPropertyValueContext, title: databaseBlockSourceName(block, source, ownPageTitle || displayTitle),
    previewRows: () => rows.flatMap((row) => propertyValueSnapshot.pagesById[row.id] ?? []) as Page[], editing: rowPagePropertyEditing,
    firstShown: () => viewFirstShownRowId({ rows, properties, view: toolbarView, currentUserId: session?.user.id, pageId: filterPageId ?? pageId, workspaceId, subitems: subitemsOn ? { faded: new Set() } : undefined }),
  } : null);
  const layoutBuilder = builder ? <PageLayoutTabProviders environment={tabEnvironment}>{builder}</PageLayoutTabProviders> : null;
  const accessDialog = usePropertyAccessDialog({ session, workspaceId, source, properties,
    onSaved: () => void databaseViewAdapter.dataSources?.reread() });
  // A locked database's own filters and sorts stay in its settings, as
  // Notion's (seen 2026-10-01): its bar shows only the view's own.
  // A reader changes the pills for themselves, as Notion's viewers do, and
  // so does a visitor to its published page, for this visit.
  const readerPills = !viewWritable && readOnly && !structureLocked && !embedded;
  const filterBar = <DatabaseQuickFilterBar view={structureLocked ? activeView : toolbarView} properties={dashboardSources ? dashboardFilterProperties(dashboardSources) : properties} snapshot={propertyValueSnapshot}
    hidden={barState.hidden} kept={barState.kept} onPopulated={barState.keep} writable={viewWritable || readerPills} request={barState.filterRequest} onRequestDone={barState.clearFilterRequest} sortRequest={barState.sortRequest} advancedRequest={barState.advancedRequest}
    ownQuery={ownQuery} toolbarSheet
    onUpdateView={(view) => readerPills ? personal.setOwnQuery(view) : void quickFilterAdapter?.updateView(view, () => true)} />;
  const cellComments = { session, containerPageId: listOnly ? "" : pageId, workspaceId, canEditPage: !readOnly, canComment: !readOnly || commentAccess };
  if (embedded) {
    // A widget's filters are its own, its "필터" no way to the dashboard's bar.
    return <DatabaseSourceRules source={source}><DatabaseFilterBarContext.Provider value={null}><DashboardFilterSourcesContext.Provider value={null}><DashboardWidgetEditingContext.Provider value={widgetHead?.mode === "edit"}><CellCommentsProvider {...cellComments}><DatabaseTaskRowsContext.Provider value={taskDatabase || listOnly === "tasks"}><DatabaseTaskRolePropertiesContext.Provider value={lockedRoles}><DatabaseSprintRowsContext.Provider value={Boolean(source.sprintStatusPropertyId)}><DatabaseProjectRowsContext.Provider value={Boolean(source.projectDatabase)}><DatabaseMeetingRowsContext.Provider value={Boolean(source.meetingDatabase)}><DatabaseDocsRowsContext.Provider value={Boolean(source.docsDatabase)}><DatabaseRowTemplateNameContext.Provider value={rowTemplateName}><DatabaseOwnUntitledNameContext.Provider value={source.untitledName?.trim() ?? ""}><CardPageContentContext.Provider value={cardPageContent}>
      <div className={embeddedStyles.embedded} data-database-view-embedded={activeView.id}>
        {/* Its tools in the widget's head, as Notion's (seen 2026-11-05). */}
        {widgetHead ? createPortal(databaseToolbar, widgetHead.slot) : null}
        <DatabaseRowCommentKey onComment={rowMenu.commentOnRow}><DatabaseViewRenderer activeType={activeView.type} renderers={renderers} /></DatabaseRowCommentKey>{rowPreview}{layoutBuilder}{accessDialog}
      </div>
    </CardPageContentContext.Provider></DatabaseOwnUntitledNameContext.Provider></DatabaseRowTemplateNameContext.Provider></DatabaseDocsRowsContext.Provider></DatabaseMeetingRowsContext.Provider></DatabaseProjectRowsContext.Provider></DatabaseSprintRowsContext.Provider></DatabaseTaskRolePropertiesContext.Provider></DatabaseTaskRowsContext.Provider></CellCommentsProvider></DashboardWidgetEditingContext.Provider></DashboardFilterSourcesContext.Provider></DatabaseFilterBarContext.Provider></DatabaseSourceRules>;
  }
  // The view's cells open its settings at a property; a row opened beside it keeps its own.
  return <DatabaseSourceRules source={source}><CellCommentsProvider {...cellComments}><DatabaseTaskRowsContext.Provider value={taskDatabase || listOnly === "tasks"}><DatabaseTaskRolePropertiesContext.Provider value={lockedRoles}><DatabaseSprintRowsContext.Provider value={Boolean(source.sprintStatusPropertyId)}><DatabaseProjectRowsContext.Provider value={Boolean(source.projectDatabase)}><DatabaseMeetingRowsContext.Provider value={Boolean(source.meetingDatabase)}><DatabaseDocsRowsContext.Provider value={Boolean(source.docsDatabase)}><DatabaseRowTemplateNameContext.Provider value={rowTemplateName}><DatabaseOwnUntitledNameContext.Provider value={source.untitledName?.trim() ?? ""}><CardPageContentContext.Provider value={cardPageContent}><BoardGroupsContext.Provider value={boardGroups}><DatabaseArchivedRowsContext.Provider value={archivedMode}>
    <DatabaseFilterBarContext.Provider value={filterBarSurface}><DashboardFilterSourcesContext.Provider value={dashboardSources}>{layoutBuilder}{accessDialog}
      <DatabasePropertySettingsContext.Provider value={openPropertySettings}>
      <DatabaseViewControlsContext.Provider value={viewControlsSlot}><DatabaseView snapshot={snapshot} title={title} operations={controller.operations} onNewViewSettings={trigger => toolbarCommands.current?.openMenu("newView", trigger)}
        onViewSettings={trigger => toolbarCommands.current?.openMenu("view", trigger)} onCopyViewLink={copyViewLink} databaseToolbar={databaseToolbar} filterBar={filterBar}
        archivedBar={archivedMode.viewing && !embedded ? <ArchivedRowsBar onExit={archivedMode.exit} /> : null}
        sourceTrashedBar={(sourceInTrash || sourcePageInTrash) && !ownPage && !embedded ? <DatabaseSourceTrashedBanner href={sourcePageInTrash && source.pageId ? pageHref(source.pageId) : fullPageHref} /> : null}
        renderer={<DatabaseRowCommentKey onComment={rowMenu.commentOnRow}><ViewSearchHighlight query={toolbarSearch}><DatabaseRowsPreviewContext.Provider value={preview}>
          {preview ? <div className={previewStyles.preview} data-rows-preview><DatabaseViewRenderer activeType={activeView.type} renderers={renderers} /></div>
            : <DatabaseViewRenderer activeType={activeView.type} renderers={renderers} />}</DatabaseRowsPreviewContext.Provider></ViewSearchHighlight></DatabaseRowCommentKey>}
        rowPreview={<DatabasePropertySettingsContext.Provider value={null}>{rowPreview}</DatabasePropertySettingsContext.Provider>} /></DatabaseViewControlsContext.Provider>
      </DatabasePropertySettingsContext.Provider>
    </DashboardFilterSourcesContext.Provider></DatabaseFilterBarContext.Provider>
  </DatabaseArchivedRowsContext.Provider></BoardGroupsContext.Provider></CardPageContentContext.Provider></DatabaseOwnUntitledNameContext.Provider></DatabaseRowTemplateNameContext.Provider></DatabaseDocsRowsContext.Provider></DatabaseMeetingRowsContext.Provider></DatabaseProjectRowsContext.Provider></DatabaseSprintRowsContext.Provider></DatabaseTaskRolePropertiesContext.Provider></DatabaseTaskRowsContext.Provider></CellCommentsProvider></DatabaseSourceRules>;
}
