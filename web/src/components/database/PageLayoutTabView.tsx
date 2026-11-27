import { Suspense, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { ContentGraph, GraphBlock, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { i18next } from "@/i18n";
import { collectAuthenticatedContentGraph } from "@/lib/authenticatedContentGraph";
import type { PageLayoutTab, PageLayoutTabView as TabView } from "@/lib/databasePageLayout";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import { DashboardFilteredContext } from "./dashboardWidgetHeight";
import { DatabaseFullPageLinkContext, DatabaseOwnPageContext } from "./databaseFullPageLink";
import { ViewNoResultsExtraContext } from "./DatabaseNoResults";
import { DatabasePreviewColumnsContext } from "./databaseRowsPreview";
import { NativeDatabaseViewMount } from "./NativeDatabaseViewMount";
import { PageLayoutTabLink } from "./PageLayoutTabLink";
import { NewPageExtraContext } from "./tableNewRowExtra";
import { keepViewOutsideGraph } from "@/lib/nativeRowMoveSteps";
import {
  PageLayoutTabActionsContext, PageLayoutTabDraftContext, PageLayoutTabFilterContext, PageLayoutTabModeContext, usePageLayoutTabEnvironment, usePageLayoutTabHolder, type PageLayoutTabActions, type PageLayoutTabEnvironment, type PageLayoutTabHolder,
  type PageLayoutTabMode,
} from "./pageLayoutTabEnvironment";
import { currentPageFilterValue, isCurrentPageFilterValue } from "./query";
import { FilterThisPageContext } from "./filterThisPage";
import { plainTitleOf } from "@/lib/closedMentions";
import { lazyScreen } from "@/lib/lazyScreen";

// The page editing a holder's read brings stays out of a published page's code.
const PageLayoutTabHolderRead = lazyScreen(() => import("./PageLayoutTabHolderRead"));

const relatedIds = (row: GraphRow | undefined, propertyId: string) => {
  const value = row?.values[propertyId]?.value;
  return Array.isArray(value) ? value.map(String) : [];
};

const TAB_SUBITEMS = { displayMode: "flattened", filterScope: "parents_and_subitems" };

/** Set once a tab's view keeps its relation's filter among its own (tabViewConfig). */
const RELATION_FILTER_KEPT = "relationFilterKept";

type Condition = { propertyId?: unknown; operator?: unknown; value?: unknown };
type Conditions = { filters?: unknown; groups?: unknown };

/**
 * Whether a view's filters, as its reader has them, keep a two-way relation
 * tab's pages to the open page: its pair "값을 포함하는 데이터 이 페이지",
 * among its pills or in its advanced filter.
 */
function keepsThisPage(config: Readonly<Record<string, unknown>>, pairId: string): boolean {
  const names = (condition: Condition) => condition?.propertyId === pairId && condition.operator === "contains"
    && Array.isArray(condition.value) && condition.value.some(isCurrentPageFilterValue);
  const inGroup = (group: Conditions | undefined): boolean => Boolean(group) && (
    (Array.isArray(group!.filters) && (group!.filters as Condition[]).some(names))
    || (Array.isArray(group!.groups) && (group!.groups as Conditions[]).some(inGroup)));
  return [config.quickFilters, config.filters].some((list) => Array.isArray(list) && (list as Condition[]).some(names))
    || inGroup(config.filterGroup as Conditions | undefined);
}

/** A two-way relation tab's pair: the relation's property on the tab's source, holding the open page. */
function relationPair(properties: readonly GraphProperty[], tab: PageLayoutTab) {
  const relation = tab.relationPropertyId;
  if (!relation || relation.startsWith("subitems-")) return undefined;
  const pairId = properties.find((candidate) => candidate.id === relation)?.config.relatedPropertyId;
  return typeof pairId === "string"
    ? properties.find((candidate) => candidate.id === pairId && candidate.dataSourceId === tab.dataSourceId) : undefined;
}

/**
 * A tab's view settings. A two-way relation tab's pages are its view's own
 * filter, as Notion's (seen 2026-11-12): its pair "값을 포함하는 데이터 이
 * 페이지", first among its pills, which the reader changes, empties, removes
 * or moves to the advanced filter as any other, for themselves until saved
 * for everyone. A tab starts with it until its view is kept with it.
 */
function tabViewConfig(saved: TabView | undefined, pairId: string | undefined): Record<string, unknown> {
  const config: Record<string, unknown> = { subtasks: TAB_SUBITEMS, rowOrder: [], ...saved?.config };
  if (!pairId || config[RELATION_FILTER_KEPT] === true) return config;
  const quick = (Array.isArray(config.quickFilters) ? config.quickFilters : []) as { propertyId?: unknown }[];
  return { ...config, [RELATION_FILTER_KEPT]: true, quickFilters: [
    { propertyId: pairId, operator: "contains", value: [currentPageFilterValue()] },
    ...quick.filter((filter) => filter?.propertyId !== pairId),
  ] };
}

/**
 * The environment with a just-connected source read from its own page, as
 * Notion's builder shows its pages at once (seen 2026-10-12), its view and
 * properties set as any tab's (Notion's, seen 2026-11-12: "설정 보기", the
 * timeline's "≫" and its columns' heads; a property renamed there waits with
 * the layout): their changes written to that page as the layout is applied
 * (usePageLayoutPropertyDraft), a board's new grouping property not made
 * before the database reads the source.
 */
function usePreviewEnvironment(environment: PageLayoutTabEnvironment | null, dataSourceId: string, pageId: string | undefined) {
  const missing = Boolean(environment && pageId && !environment.dataSources.some((source) => source.id === dataSourceId));
  const session = environment?.session ?? null;
  const [preview, setPreview] = useState<ContentGraph | null>(null);
  useEffect(() => {
    if (!missing || !session || !pageId) return undefined;
    let current = true;
    void collectAuthenticatedContentGraph(session, pageId, null, () => current)
      .then((read) => { if (current && read) setPreview(read.graph); }, () => {});
    return () => { current = false; };
  }, [missing, pageId, session]);
  return useMemo(() => !environment || !missing || !preview || !pageId ? environment : {
    ...environment,
    sourcePages: { ...environment.sourcePages, [dataSourceId]: preview.dataSources.find((source) => source.id === dataSourceId)?.pageId ?? pageId },
    boardAdapter: environment.boardAdapter && { ...environment.boardAdapter, addProperty: async () => null },
    dataSources: [...environment.dataSources, ...preview.dataSources.filter((source) => source.id === dataSourceId)],
    filterGraph: {
      properties: [...environment.filterGraph.properties, ...preview.properties.filter((property) => property.dataSourceId === dataSourceId)],
      rows: [...environment.filterGraph.rows, ...preview.rows.filter((row) => row.dataSourceId === dataSourceId)],
    },
  }, [dataSourceId, environment, missing, pageId, preview]);
}

// A data source's Sub-items and Parent item relate its rows by their nesting.
const subitemChildren = (propertyId: string | undefined) => propertyId?.startsWith("subitems-children-") === true;

const parentItem = (properties: readonly GraphProperty[], dataSourceId: string) => properties.find((property) =>
  property.dataSourceId === dataSourceId && property.id.startsWith("subitems-parent-"));

/**
 * The rows a tab shows on the open page, before its view's filters: a
 * two-way relation's are its view's filter's (tabViewConfig); one just
 * nested under it names it as its Parent item before it is read again.
 */
function tabRows(rows: readonly GraphRow[], properties: readonly GraphProperty[], tab: PageLayoutTab, current: GraphRow | undefined) {
  const own = rows.filter((candidate) => candidate.dataSourceId === tab.dataSourceId);
  const relation = tab.relationPropertyId;
  if (!relation || relationPair(properties, tab)) return own;
  if (subitemChildren(relation)) {
    const parent = parentItem(properties, tab.dataSourceId);
    return own.filter((candidate) => current && (candidate.subitemParentId === current.id
      || (parent && relatedIds(candidate, parent.id).includes(current.id))));
  }
  if (relation.startsWith("subitems-parent-")) return own.filter((candidate) => candidate.id === current?.subitemParentId);
  const related = relatedIds(current, relation);
  return own.filter((candidate) => related.includes(candidate.id));
}

type TabViewProps = {
  tab: PageLayoutTab;
  /** The open page, or the builder's preview page. */
  rowId: string | undefined;
  /** The layout's tabs, on the view's bar. */
  title: ReactNode;
  mode: PageLayoutTabMode;
  /** Keeps the tab's view, and its name as the view's settings name it. */
  onSaveView?: (view: TabView, name: string) => void | Promise<void>;
  /** The builder's "보기 복제 · 보기 삭제" for the tab. */
  actions?: PageLayoutTabActions | null;
  /** The page of a source the database's graph doesn't hold yet, read to show it before the layout is applied. */
  previewPageId?: string;
  fallback: ReactNode;
};

/**
 * The page a tab reads its source from where the environment it is given
 * doesn't hold it: the page of the open page's database — a view of it on
 * another page (a linked view) reads that page, not its database's — or,
 * with no environment, the holder's. None where it was read already.
 */
function holderToRead(given: PageLayoutTabEnvironment | null, holder: PageLayoutTabHolder | null, tab: PageLayoutTab,
  rowId: string | undefined): PageLayoutTabHolder | null {
  if (given?.dataSources.some((source) => source.id === tab.dataSourceId)) return null;
  const session = given?.session ?? holder?.session;
  const row = given?.filterGraph.rows.find((candidate) => candidate.id === rowId);
  const own = row ? given?.dataSources.find((source) => source.id === row.dataSourceId) : undefined;
  const pageId = own?.pageId ?? holder?.pageId;
  const workspaceId = given?.workspaceId ?? holder?.workspaceId;
  if (!session || !pageId || !workspaceId || pageId === given?.pageId) return null;
  return { session, pageId, workspaceId };
}

/**
 * A layout tab's view, as Notion's (seen 2026-10-12): the tab's data source
 * in a view of its own — a table until its layout is changed — of the pages
 * the open page relates to through the tab's relation, or all of them. A
 * page made in it is related to the open page. Its settings change the tab
 * for every page, or the builder's draft, where its pages are a preview to
 * read (Notion's, seen 2026-11-12: in every layout); `fallback` shows while
 * the graph holds no view of the source (one the reader can't see).
 */
export function PageLayoutTabView(props: TabViewProps) {
  const given = usePageLayoutTabEnvironment();
  const read = holderToRead(given, usePageLayoutTabHolder(), props.tab, props.rowId);
  if (!read) return <TabViewIn {...props} given={given} />;
  // While its source is read the tabs show alone, as Notion's do for a moment.
  return (
    <Suspense fallback={props.title}>
      <PageLayoutTabHolderRead key={read.pageId} holder={read}>
        {(environment, done) => done ? <TabViewIn {...props} given={environment} /> : props.title}
      </PageLayoutTabHolderRead>
    </Suspense>
  );
}

function TabViewIn({ tab, rowId, title, mode, onSaveView, actions = null, previewPageId, fallback, given }: TabViewProps & {
  given: PageLayoutTabEnvironment | null;
}) {
  // The builder's tabs read their sources through its draft, a just-connected one's with them.
  const draft = useContext(PageLayoutTabDraftContext);
  const previewed = usePreviewEnvironment(given, tab.dataSourceId, previewPageId);
  const environment = useMemo(() => previewed && draft ? draft(previewed) : previewed, [draft, previewed]);
  const source = environment?.dataSources.find((candidate) => candidate.id === tab.dataSourceId);
  const viewId = `layout-tab-${tab.id}`;
  const graph = environment?.filterGraph;
  const pair = useMemo(() => relationPair(graph?.properties ?? [], tab), [graph?.properties, tab]);
  const pairId = pair?.id;
  const view = useMemo<GraphView>(() => ({
    id: viewId, dataSourceId: tab.dataSourceId, containerId: viewId, type: tab.view?.type ?? "table",
    // The tab chooses its pages, so sub-items show among them, flat, until set otherwise. Its
    // order is its own, as a view made now keeps one (viewRowOrder.ts): the newest first until
    // its pages are moved in it, whatever order its database's views keep (Notion's, seen 2026-11-12).
    name: tab.name ?? "", position: "a0", config: tabViewConfig(tab.view, pairId),
  }), [pairId, tab.dataSourceId, tab.name, tab.view, viewId]);
  const block = useMemo<GraphBlock>(() => ({
    id: viewId, parentId: null, afterId: null, position: "a0", type: "inline_database",
    content: { dataSourceId: tab.dataSourceId, viewId, linkedDatabaseSource: true },
  }), [tab.dataSourceId, viewId]);
  const current = graph?.rows.find((candidate) => candidate.id === rowId);
  // A template's page isn't among its database's rows; its relation names the database.
  const ownSourceId = current?.dataSourceId ?? graph?.properties.find((candidate) => candidate.id === tab.relationPropertyId)?.dataSourceId;
  const thisPage = useMemo(() => rowId && ownSourceId ? { rowId, dataSourceId: ownSourceId } : null, [rowId, ownSourceId]);
  const rows = useMemo(() => tabRows(graph?.rows ?? [], graph?.properties ?? [], tab, current), [current, graph?.properties, graph?.rows, tab]);
  const properties = useMemo(() => (graph?.properties ?? []).filter((candidate) => candidate.dataSourceId === tab.dataSourceId),
    [graph?.properties, tab.dataSourceId]);
  // Writers read the latest so a page made here joins the open page's relation.
  const latest = useRef({ current, environment, onSaveView, tab });
  latest.current = { current, environment, onSaveView, tab };
  const adapters = useMemo(() => environment ? tabAdapters(viewId, environment, () => ({ ...latest.current, environment })) : null,
    [environment, viewId]);
  // Its view is the layout's, which a move's or a copy's order is written to in their ⌘Z step (nativeRowMoveSteps.ts).
  const shown = useRef({ view, rows });
  shown.current = { view, rows };
  useEffect(() => adapters ? keepViewOutsideGraph(viewId, { read: () => shown.current.view, rows: () => shown.current.rows, save: adapters.save })
    : undefined, [adapters, viewId]);
  if (!environment || !source || !adapters || !properties.some((property) => property.type === "title")) return <>{title}{fallback}</>;
  const relation = tab.relationPropertyId;
  const relationProperty = relation && !relation.startsWith("subitems-") ? graph?.properties.find((candidate) => candidate.id === relation) : undefined;
  const ownSource = current ? environment.dataSources.find((candidate) => candidate.id === current.dataSourceId) : undefined;
  const databaseName = plainTitleOf(ownSource) || environment.title?.trim() || "";
  // A one-way relation's pages, which no filter of its view can name, show as the layout's own pill.
  const filterLabel = !pair && relationProperty && databaseName
    ? { viewId, label: i18next.t("databaseView:layoutTabRelatedTo", { database: databaseName, property: relationProperty.name }) } : null;
  const linkable = mode === "page" && relation && !relation.startsWith("subitems-") && current && !environment.readOnly && environment.tableAdapter;
  // "↗ 기존 항목 연결" by a view's "+ 새 페이지" (NewPageExtraContext), while
  // its filters keep it to the open page, as Notion's (seen 2026-11-12: gone
  // with "이 페이지" emptied or the filter removed); "기존 항목 연결하기" in the
  // empty view's place while none is related.
  const linkWith = (outlined: boolean) => linkable ? <PageLayoutTabLink outlined={outlined}
    rows={(graph?.rows ?? []).filter((candidate) => candidate.dataSourceId === tab.dataSourceId && candidate.id !== current.id)}
    properties={properties} related={relatedIds(current, relation)}
    onLink={(rowId) => void environment.tableAdapter?.commitRows([{ ...current, values: { ...current.values,
      [relation]: { type: "relation", value: [...relatedIds(current, relation), rowId] } } }], () => true)} /> : null;
  const link = linkWith(false);
  const linkBy = (config: Readonly<Record<string, unknown>>) => !pairId || keepsThisPage(config, pairId) ? link : null;
  return (
    <PageLayoutTabActionsContext.Provider value={actions}>
    <PageLayoutTabModeContext.Provider value={mode}>
    <PageLayoutTabFilterContext.Provider value={filterLabel}>
    <FilterThisPageContext.Provider value={thisPage}>
    {/* Its relation narrows its pages as a filter: none related, it says so, as Notion's (seen 2026-11-12). */}
    <DashboardFilteredContext.Provider value={Boolean(filterLabel)}>
    <ViewNoResultsExtraContext.Provider value={linkWith(true)}>
      <DatabaseFullPageLinkContext.Provider value={null}>
        <DatabaseOwnPageContext.Provider value={false}>
          <NewPageExtraContext.Provider value={link ? linkBy : null}>
          <DatabasePreviewColumnsContext.Provider value={mode === "builder" && !environment.readOnly}>
          <NativeDatabaseViewMount block={block} source={source} displayTitle={tab.name || source.title} dataSources={environment.dataSources}
            filterGraph={environment.filterGraph} views={[view]} activeView={view} properties={properties} rows={rows}
            tableAdapter={adapters.table} boardAdapter={adapters.board} timelineAdapter={adapters.timeline}
            databaseViewAdapter={adapters.view} pageId={environment.pageId} workspaceId={environment.workspaceId}
            session={environment.session} readOnly={environment.readOnly} preview={mode === "builder"} title={title} />
          </DatabasePreviewColumnsContext.Provider>
          </NewPageExtraContext.Provider>
        </DatabaseOwnPageContext.Provider>
      </DatabaseFullPageLinkContext.Provider>
    </ViewNoResultsExtraContext.Provider>
    </DashboardFilteredContext.Provider>
    </FilterThisPageContext.Provider>
    </PageLayoutTabFilterContext.Provider>
    </PageLayoutTabModeContext.Provider>
    </PageLayoutTabActionsContext.Provider>
  );
}

type Latest = () => {
  current: GraphRow | undefined;
  environment: NonNullable<ReturnType<typeof usePageLayoutTabEnvironment>>;
  onSaveView?: (view: TabView, name: string) => void | Promise<void>;
  tab: PageLayoutTab;
};

/**
 * The database's writers, with the tab's view kept by the layout and a page
 * made in a relation tab related to the open page.
 */
function tabAdapters(viewId: string, environment: NonNullable<ReturnType<typeof usePageLayoutTabEnvironment>>, latest: Latest) {
  const save = async (view: GraphView) => {
    const config = { ...view.config };
    delete config.search;
    await latest().onSaveView?.({ type: view.type, config }, view.name.trim());
  };
  const relate = (row: GraphRow | null) => {
    const { current, tab, environment: now } = latest();
    if (!row || !tab.relationPropertyId || !current || !now.tableAdapter) return row;
    // A sub-items tab's page is nested under the open page through its Parent item.
    if (subitemChildren(tab.relationPropertyId)) {
      const parent = parentItem(now.filterGraph.properties, tab.dataSourceId);
      if (parent && row.subitemParentId !== current.id) {
        void now.tableAdapter.commitRows([{ ...row, values: { ...row.values, [parent.id]: { type: "relation", value: [current.id] } } }], () => true);
      }
      return row;
    }
    // A two-way relation tab's new page takes the open page from its filter, as long as it keeps it (tabViewConfig).
    if (tab.relationPropertyId.startsWith("subitems-") || relationPair(now.filterGraph.properties, tab)) return row;
    const ids = relatedIds(current, tab.relationPropertyId);
    if (!ids.includes(row.id)) {
      void now.tableAdapter.commitRows([{
        ...current, values: { ...current.values, [tab.relationPropertyId]: { type: "relation", value: [...ids, row.id] } },
      }], () => true);
    }
    return row;
  };
  const table: NativeTableProductAdapter | undefined = environment.tableAdapter && {
    ...environment.tableAdapter,
    // A sub-items tab nests the page it makes under the open page. It is made
    // on this device first as the database's own rows are, its place and its
    // page opening next carried through (rowMadeHere.ts).
    createRow: async (source, parentId, afterRowId, isCurrent, seeds, ...rest) => {
      const { current, tab } = latest();
      const parent = subitemChildren(tab.relationPropertyId) && current ? current.id : parentId;
      return relate(await environment.tableAdapter!.createRow(source, parent, afterRowId, isCurrent, seeds, ...rest));
    },
    publishCreatedRow: (row, isCurrent) => relate(environment.tableAdapter!.publishCreatedRow(row, isCurrent)),
    updateView: (view, isCurrent, step) => view.id === viewId ? save(view) : environment.tableAdapter!.updateView(view, isCurrent, step),
    commitProperties: async (properties, isCurrent, view) => {
      await environment.tableAdapter!.commitProperties(properties, isCurrent, view?.id === viewId ? undefined : view);
      if (view?.id === viewId) await save(view);
    },
    // Its own order, which a move or a copy writes in their ⌘Z step, is
    // kept by the layout (keepViewOutsideGraph), its pages' places in their
    // database staying as they are.
  };
  const board: NativeBoardProductAdapter | undefined = environment.boardAdapter && {
    ...environment.boardAdapter,
    createRow: async (...args) => relate(await environment.boardAdapter!.createRow(...args)),
    updateOptions: async (propertyId, options, isCurrent, view) => {
      await environment.boardAdapter!.updateOptions(propertyId, options, isCurrent, view?.id === viewId ? undefined : view);
      if (view?.id === viewId) await save(view);
    },
    updateView: (view, isCurrent, step) => view.id === viewId ? save(view) : environment.boardAdapter!.updateView(view, isCurrent, step),
  };
  const timeline: NativeTimelineProductAdapter | undefined = environment.timelineAdapter && {
    ...environment.timelineAdapter,
    createDatedRow: async (...args) => relate(await environment.timelineAdapter!.createDatedRow(...args)),
    updateView: (view, isCurrent, step) => view.id === viewId ? save(view) : environment.timelineAdapter!.updateView(view, isCurrent, step),
  };
  // A tab is one view: it isn't chosen, added to, removed or restored.
  const view: NativeDatabaseViewProductAdapter = {
    ...environment.databaseViewAdapter,
    selectView: () => {},
    createView: async () => null,
    deleteView: async () => {},
    restoreView: async () => {},
    updateViews: async (views, isCurrent, step) => {
      const own = views.find((candidate) => candidate.id === viewId);
      if (own) await save(own);
      const others = views.filter((candidate) => candidate.id !== viewId);
      if (others.length) await environment.databaseViewAdapter.updateViews(others, isCurrent, step);
    },
  };
  return { table, board, timeline, view, save };
}
