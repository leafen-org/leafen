import type { ProductStatusSink } from "@/lib/productStatus";
import { blockInPlace, compareGraphOrder, createGraphProperty, withDataSourceChange } from "@/contentGraph";
import type { Dispatch, SetStateAction } from "react";
import type {
  ContentGraph,
  GraphBlock,
  GraphDataSource,
  GraphDataSourceWrite,
  GraphOperation,
  GraphView,
  GraphViewType,
} from "@/contentGraph";
import { boardGroupDefault, newViewDefaults } from "@/components/database/viewLayoutDefaults";
import { createDefaultStatusConfig } from "@/lib/statusPropertyDefaults";
import { i18next } from "@/i18n";
import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { writeDatabaseViewChoice } from "@/lib/databaseViewChoice";
import { newId } from "@/lib/ids";
import { viewTakenAfter } from "@/lib/viewTakenAfter";
import { persistGraphOperationRuns, saveShownAtOnce } from "@/lib/nativeGraphAuthority";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { databaseBlockMembers, databaseLinksSource, withDatabaseBlockMembers } from "@/lib/nativeDataSourceManagement";
import { createNativeDataSourceManager, type NativeDataSourceManager } from "@/lib/nativeDataSourceManager";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import {
  dashboardLayoutOf,
  isDashboardWidget,
  placeDashboardWidget,
  removeDashboardWidget,
  withDashboardLayout,
  type DashboardLayout,
  type DashboardPlacement,
} from "@/lib/dashboardLayout";
import type { Session } from "@/types";
import type { DatabaseStarterBand } from "@/lib/databaseStarterBand";
import type { TextSpan } from "@/lib/types";
import { linkedDatabasePresentation, writtenBlockName } from "@/lib/inlineDatabaseFullPage";
import { wikiHomeOf, type WikiHome } from "@/lib/wikiHome";

export type DataSourceIcon = Required<Pick<GraphDataSource, "icon" | "iconType">>;

export type NativeDatabaseViewProductAdapter = {
  /**
   * Names the database: with its formatting, or with none given the name
   * keeps what it has while it stays the same words; answers once it is saved.
   */
  changeTitle(source: GraphDataSource, title: string, spans?: GraphDataSource["titleSpans"]): void | Promise<boolean>;
  /** Sets or takes away the database's icon, which its title shows wherever it is. */
  changeIcon(source: GraphDataSource, icon: DataSourceIcon): void;
  /** Writes the database's description, or hides or shows it. */
  changeDescription(source: GraphDataSource, change: Pick<GraphDataSource, "description" | "descriptionSpans" | "descriptionHidden">): void;
  /** Sets, moves or takes away the cover over the database opened alone. */
  changeCover(source: GraphDataSource, change: Pick<GraphDataSource, "cover" | "coverPosition">): void;
  /** Shows whether a row's page holds any block, as its body now does. */
  showRowContent(rowId: string, hasContent: boolean): void;
  selectView(block: GraphBlock, view: GraphView): void;
  /**
   * Writes the name of a full-page database that its page's title names to
   * its block, with its formatting or none, and the page takes its words
   * (database_page_name_spans.go).
   */
  setPageNameSpans?(block: GraphBlock, spans: GraphDataSource["titleSpans"]): void | Promise<boolean>;
  /** Locks or unlocks an inline database for everyone. */
  setLocked(block: GraphBlock, locked: boolean): void;
  /**
   * Names a linked database, with its formatting as changeTitle takes it, or
   * gives it an icon of its own, its source's staying as they are.
   */
  setLinkedPresentation(block: GraphBlock, change: { title?: string; titleSpans?: GraphDataSource["titleSpans"]; icon?: DataSourceIcon }): void;
  /** Archives an inline database in place, or brings it back. */
  setArchived(block: GraphBlock, archived: boolean): void;
  /** Keeps a template database page's "시작하기" band as it now is, or closes it for good (null). */
  setStarterBand?(block: GraphBlock, band: DatabaseStarterBand | null): void;
  /** Keeps a wiki's Home's own name or place on its database block, for everyone (wikiHome.ts). */
  setWikiHome?(block: GraphBlock, change: Partial<WikiHome>): void;
  /**
   * `config` joins its first settings, as a linked view's waiting mark
   * ("linkPending"). The view shows at once, `shown` hearing of it in the
   * same task, so its tab is chosen on the press's own frame; it answers the
   * view once saved, or null.
   */
  createView(block: GraphBlock, source: GraphDataSource, type: GraphViewType, isCurrent: () => boolean, from?: GraphView,
    config?: Record<string, unknown>, shown?: (view: GraphView) => void): Promise<GraphView | null>;
  /** A person's change to the views (`step`), as tabs reordered or a title hidden, is a step of the page's ⌘Z; one made for another change is not. */
  updateViews(views: readonly GraphView[], isCurrent: () => boolean, step?: boolean): Promise<void>;
  deleteView(view: GraphView, isCurrent: () => boolean): Promise<void>;
  restoreView(view: GraphView, isCurrent: () => boolean): Promise<void>;
  /** Present when the editor can change which sources a database shows. */
  dataSources?: NativeDataSourceManager;
  addDashboardWidget(
    block: GraphBlock, dashboard: GraphView, dataSourceId: string, type: GraphViewType,
    placement: DashboardPlacement, isCurrent: () => boolean,
  ): Promise<GraphView | null>;
  /** Links another database's source and puts a chart or other widget of it at `placement`, the page read again after. */
  addLinkedDashboardWidget?(
    block: GraphBlock, dashboard: GraphView, candidate: DatabaseContainerSource, type: GraphViewType,
    placement: DashboardPlacement, isCurrent: () => boolean,
  ): Promise<GraphView | null>;
  duplicateDashboardWidget(
    block: GraphBlock, dashboard: GraphView, widget: GraphView, isCurrent: () => boolean,
  ): Promise<GraphView | null>;
  /** Turns a widget into a new view of another kind, with the properties that kind makes. */
  retypeDashboardWidget(widget: GraphView, type: GraphViewType, isCurrent: () => boolean): Promise<void>;
  /** The dashboard as written without the widget, which a write made next builds on; null when not saved. */
  removeDashboardWidget(dashboard: GraphView, widgetId: string, isCurrent: () => boolean): Promise<GraphView | null>;
  arrangeDashboard(dashboard: GraphView, layout: DashboardLayout, isCurrent: () => boolean): Promise<void>;
  /** Turns a view into a dashboard whose first widget is the view as it was. */
  turnViewIntoDashboard(block: GraphBlock, view: GraphView, isCurrent: () => boolean): Promise<boolean>;
  /** Turns a dashboard into another layout, its widgets gone. */
  turnDashboardInto(dashboard: GraphView, next: GraphView, isCurrent: () => boolean): Promise<boolean>;
};

type AdapterOptions = {
  graph: ContentGraph;
  pageId: string | null;
  persistGraphOperations(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  /**
   * Holds a write with the page's other typing and sends it once the typing
   * pauses, writes to one record waiting together making one (the write
   * coordinator's queue), answering whether it was saved; without it, it is sent at once.
   */
  queueGraphOperations?(pageId: string, operations: GraphOperation[]): void | Promise<boolean>;
  /** Saves a person's change to views as one step of the page's ⌘Z (useContentGraphWrites commitViewStep). */
  persistViewStep?(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  rereadGraph?(pageId: string): Promise<boolean>;
  session?: Session;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
  workspaceId?: string | null;
};

// A new form is "양식 작성기", as Notion's (seen 2026-11-02); the others' is their layout's name.
function viewName(type: GraphViewType) {
  return type === "form" ? i18next.t("databaseView:newFormName") : i18next.t(`databaseView:viewTypes.${type}`);
}


const DASHBOARD_KEYS = ["dashboardRows", "dashboardRowHeights", "dashboardWidgetWidths"] as const;

/** A dashboard's write from the dashboard it was made from (saveDashboard). */
function dashboardUpdate(from: GraphView, next: GraphView): GraphOperation {
  return { kind: "updateView", viewUpdate: { base: from, view: next } };
}

export function createNativeDatabaseViewProductAdapter({
  graph,
  pageId,
  persistGraphOperations,
  queueGraphOperations,
  persistViewStep,
  rereadGraph,
  session,
  setGraph,
  setMessage,
  workspaceId,
}: AdapterOptions): NativeDatabaseViewProductAdapter {
  async function persist(operations: GraphOperation[]) {
    return persistGraphOperationRuns(graph, pageId, operations, persistGraphOperations);
  }

  // A name is written as it is typed: its words and formatting alone, held with the page's typing until it pauses
  // (the full-page title, an inline database's or one opened alone, its rename box).
  function changeTitle(source: GraphDataSource, title: string, spans?: GraphDataSource["titleSpans"]) {
    return changeSource(source, { id: source.id, title, ...(spans ? { titleSpans: spans } : {}) }, true);
  }

  function changeIcon(source: GraphDataSource, icon: DataSourceIcon) {
    changeSource(source, icon);
  }

  function changeDescription(source: GraphDataSource, change: Pick<GraphDataSource, "description" | "descriptionSpans" | "descriptionHidden">) {
    changeSource(source, change);
  }

  function changeCover(source: GraphDataSource, change: Pick<GraphDataSource, "cover" | "coverPosition">) {
    changeSource(source, change);
  }

  // A row's content is read with it; its open page's body tells the views at once.
  function showRowContent(rowId: string, hasContent: boolean) {
    setGraph((current) => current.rows.some((row) => row.id === rowId && row.hasContent !== hasContent)
      ? { ...current, rows: current.rows.map((row) => row.id === rowId ? { ...row, hasContent } : row) }
      : current);
  }

  // A change to a database writes the fields it changes alone, its name only when it is one of them; the rest stay
  // as stored, others' changes to them too, a name given meanwhile as well (withDataSourceChange).
  function changeSource(source: GraphDataSource, change: Partial<GraphDataSource>, typed = false): void | Promise<boolean> {
    const authorityPageId = source.pageId ?? pageId;
    if (!authorityPageId) return;
    const written: GraphDataSourceWrite = { id: source.id, ...(source.pageId ? { pageId: source.pageId } : {}), ...change };
    setGraph((current) => ({
      ...current,
      dataSources: current.dataSources.map((item) => item.id === source.id ? withDataSourceChange(item, written) : item),
    }));
    const operations: GraphOperation[] = [{ kind: "upsertDataSource", dataSource: written }];
    if (typed && queueGraphOperations) return queueGraphOperations(authorityPageId, operations);
    void persistGraphOperations(authorityPageId, operations);
  }

  // A block's name typed is written as its other typing is: what changed
  // from the block as this screen read it, the server keeping the rest as it
  // stands (content_graph_block_merge.go).
  function writeBlockName(block: GraphBlock, next: GraphBlock): void | Promise<boolean> {
    if (!pageId) return;
    const operations: GraphOperation[] = [{ kind: "upsertBlock", block: { ...blockInPlace(next), baseContent: block.content } }];
    if (queueGraphOperations) return queueGraphOperations(pageId, operations);
    void persistGraphOperations(pageId, operations);
  }

  // A chosen view is this person's here, as Notion's; the block's saved view stays.
  function selectView(block: GraphBlock, view: GraphView) {
    writeDatabaseViewChoice(session?.user.id, block.id, view.id);
  }

  function setPageNameSpans(block: GraphBlock, spans: GraphDataSource["titleSpans"]) {
    if (!pageId) return;
    const content = { ...block.content, pageTitleSpans: spans ?? [] };
    const next = { ...block, content };
    setGraph((current) => ({ ...current, blocks: current.blocks.map((item) => item.id === block.id ? next : item) }));
    return writeBlockName(block, next);
  }

  function setLocked(block: GraphBlock, locked: boolean) {
    setBlockFlag(block, "locked", locked);
  }

  function setArchived(block: GraphBlock, archived: boolean) {
    setBlockFlag(block, "archived", archived);
  }

  // A linked database's own name, its formatting beside it, and icon are its block's; empty, it has none. So is the
  // icon of a database of several sources of its own, "none" kept for one taken away (database_own_icon.go).
  function setLinkedPresentation(block: GraphBlock, change: { title?: string; titleSpans?: GraphDataSource["titleSpans"]; icon?: DataSourceIcon }) {
    if (!pageId) return;
    const content = { ...block.content };
    if (change.title !== undefined) {
      const named = writtenBlockName(change.title, change.titleSpans as TextSpan[] | undefined,
        { title: linkedDatabasePresentation(block).title, spans: block.content.linkedTitleSpans });
      if (named.title) content.linkedTitle = named.title;
      else delete content.linkedTitle;
      if (named.spans) content.linkedTitleSpans = named.spans;
      else delete content.linkedTitleSpans;
    }
    if (change.icon && block.content.linkedDatabaseSource !== true) {
      Object.assign(content, change.icon.icon ? { databaseIcon: change.icon.icon, databaseIconType: change.icon.iconType } : { databaseIcon: "", databaseIconType: "none" });
    } else if (change.icon) {
      if (change.icon.iconType !== "none" && change.icon.icon) {
        content.linkedIcon = change.icon.icon;
        content.linkedIconType = change.icon.iconType;
      } else {
        delete content.linkedIcon;
        delete content.linkedIconType;
      }
    }
    const next = { ...block, content };
    setGraph((current) => ({
      ...current,
      blocks: current.blocks.map((item) => item.id === block.id ? next : item),
    }));
    if (change.title !== undefined && !change.icon) writeBlockName(block, next);
    else void persistGraphOperations(pageId, [{ kind: "upsertBlock", block: blockInPlace(next) }]);
  }

  // An inline database's own lock and archive are its block's flags.
  function setBlockFlag(block: GraphBlock, flag: "locked" | "archived" | "starterBand", on: boolean | DatabaseStarterBand) {
    if (!pageId) return;
    const content = { ...block.content };
    if (on) content[flag] = on;
    else delete content[flag];
    const next = { ...block, content };
    setGraph((current) => ({
      ...current,
      blocks: current.blocks.map((item) => item.id === block.id ? next : item),
    }));
    void persistGraphOperations(pageId, [{ kind: "upsertBlock", block: blockInPlace(next) }]);
  }

  // Written as a block's name is, what changed from the block as read, so a
  // change made meanwhile elsewhere on the block stays.
  function setWikiHome(block: GraphBlock, change: Partial<WikiHome>) {
    if (!pageId) return;
    const next = { ...block, content: { ...block.content, wikiHome: { ...wikiHomeOf(block), ...change } } };
    setGraph((current) => ({ ...current, blocks: current.blocks.map((item) => item.id === block.id ? next : item) }));
    void persistGraphOperations(pageId, [{ kind: "upsertBlock", block: { ...blockInPlace(next), baseContent: block.content } }]);
  }

  async function updateViews(given: readonly GraphView[], isCurrent: () => boolean, step?: boolean) {
    const { views, copies } = withOwnWidgets(given);
    const changed = new Map(views.map((view) => [view.id, view]));
    const save = step && persistViewStep ? (operations: GraphOperation[]) => persistGraphOperationRuns(graph, pageId, operations, persistViewStep) : persist;
    if (!views.length || !await saveShownAtOnce(graph, pageId, [...views, ...copies].map((view) => ({ kind: "upsertView", view })), save, isCurrent,
      () => setGraph((current) => ({ ...current, views: [...current.views.map((view) => changed.get(view.id) ?? view), ...copies] })))) return;
    setMessage(i18next.t("databaseView:status.saved"));
  }

  // A dashboard's rows name its own widgets: one written naming another
  // dashboard's — its copy, "보기 복제" — takes a copy of each, its own view
  // with the same layout, name and settings, the copy's rows naming them, as
  // Notion's copy keeps its widgets (seen 2026-11-12), the two then apart.
  function withOwnWidgets(given: readonly GraphView[]) {
    const copies: GraphView[] = [];
    const last = new Map<string, string | undefined>();
    const views = given.map((view) => {
      const before = copies.length;
      if (view.type !== "dashboard") return view;
      const layout = dashboardLayoutOf(view).map((row) => ({ ...row, widgets: row.widgets.map((cell) => {
        const widget = graph.views.find(({ id }) => id === cell.id);
        if (!widget || !isDashboardWidget(widget) || widget.config.dashboardViewId === view.id) return cell;
        const owner = `${widget.dataSourceId}:${widget.containerId ?? ""}`;
        const after = last.has(owner) ? last.get(owner) : graph.views
          .filter((other) => other.dataSourceId === widget.dataSourceId && (other.containerId ?? "") === (widget.containerId ?? ""))
          .sort(compareGraphOrder).at(-1)?.position;
        const copy: GraphView = { ...widget, id: newId(), position: graphPositionBetween(after, undefined),
          config: { ...structuredClone(widget.config), dashboardViewId: view.id } };
        last.set(owner, copy.position);
        copies.push(copy);
        return { ...cell, id: copy.id };
      }) }));
      return copies.length > before ? withDashboardLayout(view, layout) : view;
    });
    return { views, copies };
  }

  // A new view's first settings, and the properties it makes for them: a
  // view that places pages by a property makes one where the source has
  // none and places them by it, as Notion's does — a map "위치" (seen
  // 2026-10-05), a calendar or timeline "날짜" (seen 2026-10-06); a board
  // with nothing to group by makes a status "상태" (its rows "시작 전") and
  // groups by it (seen 2026-10-06). A new board or gallery starts with the
  // title alone, a list with one property beside it, a gallery previewing
  // its pages' content.
  function newViewParts(dataSourceId: string, type: GraphViewType, context?: Parameters<typeof newViewDefaults>[2]) {
    const sourceProperties = graph.properties.filter((property) => property.dataSourceId === dataSourceId);
    const placing = viewPlacingProperty(type);
    const place = placing && !sourceProperties.some((property) => property.type === placing.type)
      ? createGraphProperty(sourceProperties, dataSourceId, placing.type, placing.name())
      : null;
    const groupBy = boardGroupingProperty(type, sourceProperties);
    const grouping = groupBy ? createGraphProperty(sourceProperties, dataSourceId, groupBy.type, groupBy.name, groupBy.config) : null;
    return {
      config: {
        ...newViewDefaults(type, sourceProperties, context),
        ...(place && placing ? { [placing.configKey]: place.property.id } : {}),
        ...(grouping ? { groupBy: grouping.property.id } : {}),
      } as Record<string, unknown>,
      operations: [...(place?.operations ?? []), ...(grouping?.operations ?? [])] as GraphOperation[],
      apply: (current: ContentGraph) => ({
        properties: [...current.properties, ...(place ? [place.property] : []), ...(grouping ? [grouping.property] : [])],
      }),
    };
  }

  // A widget of a linked source belongs to the database showing it.
  function widgetOwner(
    block: GraphBlock, dataSourceId: string, linked = databaseLinksSource(block, dataSourceId),
  ): Pick<GraphView, "id" | "dataSourceId" | "containerId" | "position"> {
    const siblings = graph.views
      .filter((view) => view.dataSourceId === dataSourceId && (linked ? view.containerId === block.id : !view.containerId))
      .sort(compareGraphOrder);
    return {
      id: newId(), dataSourceId, ...(linked ? { containerId: block.id } : {}),
      position: graphPositionBetween(siblings.at(-1)?.position, undefined),
    };
  }

  // Every write of a dashboard's layout names the dashboard it was made from
  // as its base, and shows what it sends at once, as a setting does
  // (saveShownAtOnce): the server keeps what others changed meanwhile, and a
  // write made next — chained right after it, or by the next press — builds
  // on it. The dashboard this screen last drew is no base: it lags behind a
  // write until the screen draws again (2026-11-11: a widget of another
  // database's source, added right after the chart it replaced was taken
  // out, named the dashboard with the chart still in it and was refused as
  // a conflict, every time). It answers the dashboard as written, or null.
  async function saveDashboard(
    from: GraphView, layout: DashboardLayout, isCurrent: () => boolean,
    change: { before?: GraphOperation[]; after?: GraphOperation[]; show?: (current: ContentGraph, views: GraphView[]) => Partial<ContentGraph> } = {},
  ) {
    const next = withDashboardLayout(from, layout);
    const saved = await saveShownAtOnce(graph, pageId, [
      ...change.before ?? [], dashboardUpdate(from, next), ...change.after ?? [],
    ], persist, isCurrent, () => setGraph((current) => {
      const views = current.views.map((view) => view.id === from.id ? next : view);
      return { ...current, views, ...change.show?.(current, views) };
    }));
    return saved ? next : null;
  }

  async function addWidget(
    dashboard: GraphView, widget: GraphView, placement: DashboardPlacement, isCurrent: () => boolean,
    parts?: ReturnType<typeof newViewParts>, linking?: GraphBlock,
  ) {
    const layout = placeDashboardWidget(dashboardLayoutOf(dashboard), widget.id, placement);
    if (!layout || !await saveDashboard(dashboard, layout, isCurrent, {
      before: parts?.operations,
      after: [{ kind: "upsertView", view: widget }, ...linking ? [{ kind: "upsertBlock" as const, block: blockInPlace(linking) }] : []],
      show: (current, views) => ({
        ...parts?.apply(current), views: [...views, widget],
        ...linking ? { blocks: current.blocks.map((item) => item.id === linking.id ? linking : item) } : {},
      }),
    })) return null;
    setMessage(i18next.t("databaseView:status.added"));
    return widget;
  }

  const dataSources = pageId && workspaceId && session && rereadGraph ? createNativeDataSourceManager({
    graph, pageId, workspaceId, session, persistGraphOperations, rereadGraph, setMessage,
  }) : undefined;

  return {
    changeTitle,
    changeIcon,
    changeDescription,
    changeCover,
    showRowContent,
    selectView,
    setLocked,
    setLinkedPresentation,
    setPageNameSpans,
    setArchived,
    setStarterBand: (block, band) => setBlockFlag(block, "starterBand", band ?? false),
    setWikiHome,
    dataSources,
    async createView(block, source, type, isCurrent, from, config, shown) {
      // A view of a linked source belongs to the database showing it and leaves
      // the original alone; forms stay in the original database, as in Notion.
      const linkedSource = databaseLinksSource(block, source.id);
      if ((linkedSource && type === "form") || !isCurrent()) return null;
      const containerId = linkedSource ? block.id : undefined;
      // It goes after every tab of the database, whichever source each shows, as Notion's (seen 2026-11-02).
      const own = databaseBlockMembers(block).own;
      const siblings = graph.views
        .filter((view) => view.containerId === block.id
          || (!view.containerId && (view.dataSourceId === source.id || own.includes(view.dataSourceId))))
        .sort(compareGraphOrder);
      const parts = newViewParts(source.id, type, { from, siblings: siblings.filter((view) => view.dataSourceId === source.id) });
      // A new view has no name of its own, so each reader sees its layout's
      // name in their own language, as Notion leaves a view added from its
      // menus (seen 2026-11-11: "Untitled" through its API for a board, a
      // chart and a dashboard); a form is "양식 작성기", as Notion names it.
      const view: GraphView = {
        id: newId(),
        dataSourceId: source.id,
        ...(containerId ? { containerId } : {}),
        type,
        name: type === "form" ? viewName(type) : "",
        position: graphPositionBetween(siblings.at(-1)?.position, undefined),
        config: config ? { ...parts.config, ...config } : parts.config,
      };
      // A view added shows at once and is saved behind it, as a setting is
      // (saveShownAtOnce; user decision 2026-11-12: a press answers on the
      // next frame), a step of the page's ⌘Z, a new board's "상태" with it.
      // One the server refuses goes as the page is read again. Shown only
      // after the server answered, a new tab took 290–315 ms on a NAS's 100
      // ms round trip (2026-11-13).
      if (!await saveShownAtOnce(graph, pageId, [...parts.operations, { kind: "upsertView", view }],
        (operations) => persistGraphOperationRuns(graph, pageId, operations, persistViewStep ?? persistGraphOperations), () => true, () => {
          setGraph((current) => ({
            ...current,
            ...parts.apply(current),
            views: [...current.views, view],
          }));
          shown?.(view);
        })) return null;
      setMessage(i18next.t("databaseView:status.added"));
      return view;
    },
    updateViews,
    // A dashboard widget is a view of one of the database's sources that
    // names its dashboard; the dashboard's rows place it.
    // A widget starts with a new view's settings of its kind, never named as a new view.
    async addDashboardWidget(block, dashboard, dataSourceId, type, placement, isCurrent) {
      const parts = newViewParts(dataSourceId, type);
      const widget: GraphView = {
        ...widgetOwner(block, dataSourceId), type, name: "", config: { ...parts.config, dashboardViewId: dashboard.id },
      };
      return addWidget(dashboard, widget, placement, isCurrent, parts);
    },
    // A widget of another database's source links it in the same write, the
    // widget the database's own (Notion's first widget of a dashboard, its
    // source asked first, seen 2026-11-12); the page is read again for the
    // source's properties and rows.
    async addLinkedDashboardWidget(block, dashboard, candidate, type, placement, isCurrent) {
      const { own, linked } = databaseBlockMembers(block);
      const parts = newViewParts(candidate.dataSourceId, type);
      const widget: GraphView = {
        ...widgetOwner(block, candidate.dataSourceId, true), type, name: "",
        config: { ...parts.config, dashboardViewId: dashboard.id },
      };
      const linking = withDatabaseBlockMembers(block, own, [...linked, { dataSourceId: candidate.dataSourceId, sourceContainerId: candidate.id }]);
      const added = await addWidget(dashboard, widget, placement, isCurrent, parts, linking);
      if (added && pageId && rereadGraph) await rereadGraph(pageId);
      return added;
    },
    // A new widget turned to another kind starts over as a new view of it.
    async retypeDashboardWidget(widget, type, isCurrent) {
      const parts = newViewParts(widget.dataSourceId, type);
      const next: GraphView = { ...widget, type, name: "", config: { ...parts.config, dashboardViewId: widget.config.dashboardViewId } };
      if (!await persist([...parts.operations, { kind: "upsertView", view: next }]) || !isCurrent()) return;
      setGraph((current) => ({
        ...current,
        ...parts.apply(current),
        views: current.views.map((view) => view.id === widget.id ? next : view),
      }));
      setMessage(i18next.t("databaseView:status.saved"));
    },
    // A copy sits beside its widget, or in a new row below when that row is full.
    async duplicateDashboardWidget(block, dashboard, widget, isCurrent) {
      const layout = dashboardLayoutOf(dashboard);
      const rowIndex = layout.findIndex((row) => row.widgets.some(({ id }) => id === widget.id));
      if (rowIndex < 0) return null;
      const index = layout[rowIndex].widgets.findIndex(({ id }) => id === widget.id);
      const copy: GraphView = {
        ...widgetOwner(block, widget.dataSourceId), type: widget.type,
        name: i18next.t("databaseView:copyName", { name: widget.name || viewName(widget.type) }), config: structuredClone(widget.config),
      };
      const beside = layout[rowIndex].widgets.length < 4;
      return addWidget(dashboard, copy, beside
        ? { rowIndex, newRow: false, index: index + 1 }
        : { rowIndex: rowIndex + 1, newRow: true }, isCurrent);
    },
    async removeDashboardWidget(dashboard, widgetId, isCurrent) {
      const written = await saveDashboard(dashboard, removeDashboardWidget(dashboardLayoutOf(dashboard), widgetId), isCurrent, {
        after: [{ kind: "deleteView", viewDeletion: { id: widgetId } }],
        show: (_current, views) => ({ views: views.filter((view) => view.id !== widgetId) }),
      });
      if (written) setMessage(i18next.t("databaseView:status.deleted"));
      return written;
    },
    async arrangeDashboard(dashboard, layout, isCurrent) {
      if (await saveDashboard(dashboard, layout, isCurrent)) setMessage(i18next.t("databaseView:status.saved"));
    },
    // A view turned into a dashboard keeps its place, tab, name and settings
    // as the dashboard (a view keeps its name through any change of layout),
    // and a copy of it as it was becomes the dashboard's first widget, taking
    // after it as any view copy does (viewTakenAfter: its layout, its name as
    // typed with its spaces, its settings), as Notion's (2026-10-11, 11-18).
    async turnViewIntoDashboard(block, view, isCurrent) {
      if (view.type === "dashboard" || isDashboardWidget(view)) return false;
      const taken = viewTakenAfter({ ...widgetOwner(block, view.dataSourceId), type: view.type, name: "", config: {} }, view);
      const widget: GraphView = { ...taken, config: { ...structuredClone(taken.config), dashboardViewId: view.id } };
      const dashboard = withDashboardLayout({ ...view, type: "dashboard" },
        [{ height: 0, widgets: [{ id: widget.id, width: 1 }] }]);
      if (!await persist([{ kind: "upsertView", view: dashboard }, { kind: "upsertView", view: widget }]) || !isCurrent()) return false;
      setGraph((current) => ({
        ...current,
        views: [...current.views.map((item) => item.id === view.id ? dashboard : item), widget],
      }));
      setMessage(i18next.t("databaseView:status.saved"));
      return true;
    },
    // A dashboard turned into another layout keeps its settings and loses its
    // widgets, without asking, as Notion's (seen 2026-10-11).
    async turnDashboardInto(dashboard, next, isCurrent) {
      const widgets = graph.views.filter((view) => view.config.dashboardViewId === dashboard.id);
      const config = { ...next.config };
      for (const key of DASHBOARD_KEYS) delete config[key];
      const view: GraphView = { ...next, config };
      if (!await persist([
        { kind: "upsertView", view }, ...widgets.map((widget): GraphOperation => ({ kind: "deleteView", viewDeletion: { id: widget.id } })),
      ]) || !isCurrent()) return false;
      const gone = new Set(widgets.map((widget) => widget.id));
      setGraph((current) => ({
        ...current,
        views: current.views.flatMap((item) => gone.has(item.id) ? [] : [item.id === dashboard.id ? view : item]),
      }));
      setMessage(i18next.t("databaseView:status.saved"));
      return true;
    },
    async deleteView(view, isCurrent) {
      // A database block that showed the view shows the next one of its
      // database instead, in the same change, so the block stays whole.
      const next = graph.views
        .filter((candidate) => candidate.id !== view.id && candidate.dataSourceId === view.dataSourceId
          && (candidate.containerId ?? "") === (view.containerId ?? "") && !isDashboardWidget(candidate))
        .sort(compareGraphOrder)[0];
      const moved = next ? graph.blocks
        .filter((block) => block.type === "inline_database" && block.content.viewId === view.id)
        .map((block) => ({ ...block, content: { ...block.content, viewId: next.id } })) : [];
      if (!await persist([{ kind: "deleteView", viewDeletion: { id: view.id } },
        ...moved.map((block) => ({ kind: "upsertBlock" as const, block }))]) || !isCurrent()) return;
      const movedById = new Map(moved.map((block) => [block.id, block]));
      setGraph((current) => ({
        ...current,
        views: current.views.filter((candidate) => candidate.id !== view.id),
        blocks: movedById.size ? current.blocks.map((block) => movedById.get(block.id) ?? block) : current.blocks,
      }));
      setMessage(i18next.t("databaseView:status.deleted"));
    },
    async restoreView(view, isCurrent) {
      if (!await persist([{ kind: "upsertView", view }]) || !isCurrent()) return;
      setGraph((current) => ({
        ...current,
        views: [...current.views.filter((candidate) => candidate.id !== view.id), view],
      }));
      setMessage(i18next.t("databaseView:status.restored"));
    },
  };
}

/**
 * The status a board makes where its source has nothing it groups by, as
 * Notion's new board (seen 2026-10-06) and a view turned into a board (seen
 * 2026-11-11, from its layout screen): "상태", its rows "시작 전", the board
 * grouped by it.
 */
export function boardGroupingProperty(type: GraphViewType, properties: Parameters<typeof boardGroupDefault>[0]) {
  if (type !== "board" || boardGroupDefault(properties)) return null;
  return { type: "status" as const, name: activePersistentGeneratedLabels().propertyNames.status, config: createDefaultStatusConfig(newId, i18next.language) };
}

/**
 * The property a view places its pages by, which a new view of that type
 * makes where its source has none, and the view setting naming it.
 */
export function viewPlacingProperty(type: GraphViewType) {
  if (type === "map") return { type: "place" as const, configKey: "mapBy", name: () => i18next.t("databaseView:placePropertyName") };
  if (type === "calendar" || type === "timeline") {
    return { type: "date" as const, configKey: type === "calendar" ? "calendarBy" : "timelineBy",
      name: () => activePersistentGeneratedLabels().propertyNames.date };
  }
  return null;
}
