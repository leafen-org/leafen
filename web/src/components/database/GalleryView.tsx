"use client";

import { useCommittedOperationScope } from "../product/useCommittedOperationScope";

import {
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import type {
  GraphDataSource,
  GraphProperty,
  GraphRow,
  GraphView,
} from "../../contentGraph";
import type { PropertyValueReadContext } from "@/components/database/PropValue";
import type { DbView, Page, ViewConfig } from "@/lib/types";
import { CardFullLineContext, compactFullLineIds } from "./cardFullLine";
import { useDragReorder } from "@/lib/useDragReorder";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { CardSelectionProvider, useCardSelection } from "./cardSelection";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { GalleryCard, type GalleryCardFrame, type GalleryCardWays } from "./GalleryCard";
import { liveEnvironment } from "./ListViewRow";
import { useLiveWays, useSteadyFrame } from "./memoizedViewItems";
import { conditionalColorResolver } from "./conditionalColorModel";
import {
  type PageContentPreviewTransport,
  type WorkspaceFileHook,
} from "./CardPreviewMedia";
import { newNamedOption } from "./optionNameText";
import { drawArrivalsWhole } from "@/lib/arrivingWhole";
import { useGalleryCoverHeight } from "./useGalleryCoverHeight";
import { CARD_PREVIEW_PAGE_CONTENT, hasCardPreview } from "./cardPreviewModePolicy";
import {
  projectCardViewSubitems,
  propertySearchText,
  visibleViewProperties,
  type QueryBounds,
} from "./query";
import {
  createGalleryRowCoordinator,
  galleryPresentationPage,
  galleryPresentationProperty,
  type GalleryPagePresentation,
} from "./galleryViewModel";
import { GroupHeadContent } from "./GroupHeadContent";
import { GALLERY_WINDOW_THRESHOLD, GalleryWindow } from "./GalleryWindow";
import { pagesOver, presentedPagesById, rowPagesInOrder } from "./presentedPages";
import { listGroups } from "./listGroups";
import { tableGroupSeedValue } from "./tableGrouping";
import { groupMoveValues } from "./viewGroupMove";
import { groupLine, viewLinesDrag } from "./viewGroupLines";
import { ViewGroupOptions, type ViewGroupMenuAt } from "./ViewGroupOptions";
import { groupOptionNames } from "./TableGroupAddRow";
import { ViewGroupAdd } from "./ViewGroupAdd";
import { useViewAddsGroups, viewGroupAddedKey, viewGroupAddKind } from "./viewGroupAddKind";
import { tableConfigAfterGroupKept } from "./tableGroupSettingsModel";
import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { Plus } from "../icons";
import styles from "./databaseSheet";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { CardsLoadMore, type CardsLoadMoreControl } from "./CardsLoadMore";
import { holdTypingWhileRowOpens, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { useViewingArchivedRows, useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { DatabaseNoResults, viewNothingToAdd, viewQuery, type ViewNoResultsCommands } from "./DatabaseNoResults";
import { useKeepNewGroup } from "./useKeepNewGroup";
import { useRowDisplayTitle } from "./untitledRowName";
import { useCollapsedGroups } from "./useCollapsedGroups";
import { useListGroupLoads } from "./ListGroupLoads";
import { copiesPlace, type CopiesPlace } from "./viewRowOrder";
import { useNewPageExtra } from "./tableNewRowExtra";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

export type GalleryRowOpenMode = "default" | "new_tab" | "side" | "full";

export interface GalleryViewSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  workspaceId: string;
  /** The page holding the view and who reads it, for "this page" and "me" in its conditional colors. */
  contextPageId?: string;
  currentUserId?: string;
  dataSource: GraphDataSource;
  view: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  pagesById: Readonly<Record<string, GraphRow>>;
  pagePresentationById: Readonly<Record<string, GalleryPagePresentation>>;
  databaseFeatures?: Page["databaseFeatures"];
  pageContentPreviews: Readonly<Record<string, { previewUrl?: string; revision: number }>>;
  propertyValueContext: PropertyValueReadContext;
  queryBounds: QueryBounds;
  useWorkspaceFile: WorkspaceFileHook;
  /** Edits a value in place: a card's property slots while its name is typed. */
  propertyCellEnvironment?: PropertyCellEnvironment;
}

export interface GalleryViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  requestGeneration: number;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

export interface GalleryViewOperations extends ViewNoResultsCommands {
  /** Moves rows before or after another in the view, with the values they take there by row. */
  moveRows?(rowIds: readonly string[], targetId: string, side: "before" | "after", context: GalleryViewOperationContext, values?: Readonly<Record<string, Readonly<Record<string, unknown>>>>): Promise<void>;
  /** A new card, last or beside another, starting with the values given (a group's); `alongside` runs as it shows, drawn with it. */
  createRow(context: GalleryViewOperationContext, seeds?: Readonly<Record<string, unknown>>,
    place?: { beforeRowId?: string; afterRowId?: string }, alongside?: (row: GraphRow) => void): Promise<GraphRow | null>;
  /** What a group's "⋯" changes, as a list's: the view's settings (a person's change, `step`, a step of the page's ⌘Z), its property's options, its pages to the trash, and the grouping settings. */
  updateViewConfig?(config: Record<string, unknown>, context: GalleryViewOperationContext, step?: boolean): Promise<void>;
  commitProperty?(property: GraphProperty, context: GalleryViewOperationContext): Promise<void>;
  trashRows?(rowIds: readonly string[]): Promise<void>;
  duplicateRows?(rowIds: readonly string[], place?: CopiesPlace): Promise<readonly string[] | void>;
  openGroupSettings?(trigger: HTMLElement): void;
  setRowProperty(rowId: string, propertyId: string, value: unknown, context: GalleryViewOperationContext): Promise<void>;
  /** Its first cards and how to show more, when the gallery shows only its first. */
  /** The cards made here since it opened, shown past a group's first ones (useListGroupLoads). */ madeRowIds?: ReadonlySet<string>; loadMoreCards?: CardsLoadMoreControl; setCardLoadLimit?(limit: number): void;
  isCurrent(context: GalleryViewOperationContext): boolean;
  onError(error: unknown, context: GalleryViewOperationContext): void;
  openRow(rowId: string, context: GalleryViewOperationContext): void;
  openRowIn(rowId: string, mode: GalleryRowOpenMode, context: GalleryViewOperationContext): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  rowContextMenu: ReactNode;
}

// A gallery's cards show their content's preview as of its revision.
const GALLERY_PAGES = { previews: true };

export function GalleryView({
  snapshot,
  operations,
  readOnly = false,
}: {
  snapshot: GalleryViewSnapshot;
  operations: GalleryViewOperations;
  readOnly?: boolean;
}) {
  const { t } = useTranslation(["galleryView", "boardView", "common"]);
  const rowTitle = useRowDisplayTitle();
  const newItemText = t("galleryView:newInline");
  const [collapsedGroups, setCollapsedGroups] = useCollapsedGroups(snapshot.view.id), groupLoads = useListGroupLoads(snapshot.view, operations.madeRowIds, operations.setCardLoadLimit);
  const [groupMenu, setGroupMenu] = useState<ViewGroupMenuAt | null>(null);
  // A "+ 새 페이지" pressed while its page is still being made makes no
  // other (the coordinator's own), its button not shut meanwhile: shut and
  // opened again as its own state, it drew every card of the gallery twice
  // for the press (2026-11-15: 2,565 components each, CPU ×2), as a table's
  // new row no longer shuts its button (TableView).
  const createCoordinator = useMemo(
    () => createGalleryRowCoordinator<GalleryViewOperationContext, GraphRow>(),
    [],
  );
  const lifecycleController = useCommittedOperationScope([
    snapshot.contextIdentity,
    snapshot.requestGeneration,
    snapshot.snapshotIdentity,
  ]);
  const context = useMemo<GalleryViewOperationContext>(() => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    requestGeneration: snapshot.requestGeneration,
    signal: lifecycleController.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.view.id,
  }), [lifecycleController, snapshot]);

  const props = useMemo(
    () => snapshot.properties.map(galleryPresentationProperty),
    [snapshot.properties],
  );
  const titleProperty = snapshot.properties.find((property) => property.type === "title");
  // Its rows' pages, made again for the rows changed alone (presentedPages.ts).
  const pagesById = useMemo(() => presentedPagesById({ pagesById: snapshot.pagesById, rows: snapshot.rows, workspaceId: snapshot.workspaceId,
    pagePresentationById: snapshot.pagePresentationById, pageContentPreviews: snapshot.pageContentPreviews }, titleProperty, GALLERY_PAGES),
  [snapshot.pageContentPreviews, snapshot.pagePresentationById, snapshot.pagesById, snapshot.rows, snapshot.workspaceId, titleProperty]);
  const rows = useMemo(
    () => rowPagesInOrder(snapshot.rows, pagesById) as Page[] | undefined ?? snapshot.rows.map((row, index) => pagesById[row.id] ?? galleryPresentationPage({
      row,
      titleProperty,
      workspaceId: snapshot.workspaceId,
      previewRevision: snapshot.pageContentPreviews[row.id]?.revision,
      presentation: snapshot.pagePresentationById[row.id],
      position: index,
    })),
    [pagesById, snapshot.pageContentPreviews, snapshot.pagePresentationById, snapshot.rows, snapshot.workspaceId, titleProperty],
  );
  const view = useMemo<DbView>(() => ({
    id: snapshot.view.id,
    databaseId: snapshot.view.dataSourceId,
    name: snapshot.view.name,
    type: snapshot.view.type as DbView["type"],
    config: snapshot.view.config as ViewConfig,
    position: 0,
  }), [snapshot.view]);
  const database = useMemo(() => dataSourcePage(snapshot.dataSource, snapshot.workspaceId, 0, { databaseFeatures: snapshot.databaseFeatures }),
    [snapshot.dataSource, snapshot.databaseFeatures, snapshot.workspaceId]);
  // A card its conditional colors match takes the color whole, as Notion's
  // gallery does (2026-10-24): its preview and title alike.
  const cardColor = useMemo(() => conditionalColorResolver(view.config as Record<string, unknown>, props, {
    pagesById, currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId,
  }, snapshot.queryBounds), [pagesById, props, snapshot.contextPageId, snapshot.currentUserId, snapshot.queryBounds, view.config]);
  const presentation = useMemo(
    () => projectCardViewSubitems(database, view, rows, snapshot.queryBounds),
    [database, rows, snapshot.queryBounds, view],
  );
  // Its cards: every row it shows, unless its sub-items' setting leaves some out.
  const cardRows = useMemo(() => {
    if (presentation.rows === rows) return snapshot.rows;
    const shown = new Set(presentation.rows.map((row) => row.id));
    return snapshot.rows.filter((row) => shown.has(row.id));
  }, [presentation.rows, rows, snapshot.rows]);
  const visible = useMemo(
    () => visibleViewProperties(props, view, snapshot.queryBounds),
    [props, snapshot.queryBounds, view],
  );
  const others = useMemo(
    () => visible.filter((property) => property.type !== "title"),
    [visible],
  );
  const cardSize = view.config?.cardSize ?? "medium";
  const cover = useGalleryCoverHeight(cardSize);
  // A card new to the drawn gallery paints in its first frame (lib/arrivingWhole.ts).
  useLayoutEffect(() => drawArrivalsWhole(cover.ref.current, cover.ref.current?.querySelectorAll<HTMLElement>("[data-row-card]") ?? []));
  const coverProperty = view.config?.coverProperty;
  const showPreview = hasCardPreview(coverProperty);
  // A card without a preview shows its title even when the title is hidden,
  // as Notion's gallery does (seen 2026-11-12, made through its API).
  const showTitle = !titleProperty || !showPreview
    || !(view.config?.hiddenProperties ?? []).includes(titleProperty.id);
  const wrap = !!view.config?.wrap;
  const cardLayout = view.config?.cardLayout === "compact" ? "compact" : undefined;
  const fullLine = useMemo(() => compactFullLineIds(view.config), [view.config]);
  const fitImage = !!view.config?.fitImage;
  const newPageLabel = t("galleryView:newPageIn", { dbTitle: snapshot.dataSource.title });
  // What sits under "+ 새 페이지", a relation tab's "↗ 기존 항목 연결", centred under its tile (Notion's, seen 2026-11-12).
  const newPageExtra = useNewPageExtra(snapshot.view.config);
  const withExtra = (button: ReactNode) => newPageExtra ? <div className={styles.galleryNewCell}>{button}{newPageExtra}</div> : button;
  const viewingArchived = useViewingArchivedRows(), offersNewRows = useViewOffersNewRows();
  // Grouped by any property, each group under a table's head with its own
  // "+ 새 페이지" card, as Notion's gallery (seen 2026-10-25).
  const groupBy = snapshot.view.config?.groupBy;
  const groupProperty = typeof groupBy === "string" ? snapshot.properties.find((property) => property.id === groupBy) : undefined;
  const viewConfig = snapshot.view.config as Record<string, unknown>;
  const galleryGroups = useMemo(() => groupProperty ? listGroups({ property: groupProperty, properties: snapshot.properties,
    config: viewConfig, currentUserId: snapshot.currentUserId, rows: cardRows, graphRows: snapshot.pagesById,
    pagesById: snapshot.propertyValueContext.pagesById, propsByDatabaseId: snapshot.propertyValueContext.propsByDatabaseId }) : null,
  [cardRows, groupProperty, snapshot.currentUserId, snapshot.pagesById, snapshot.properties, snapshot.propertyValueContext, viewConfig]);
  // A card moves by dragging it; into another group it takes that group's value (Notion's, seen 2026-11-05).
  const moves = !readOnly && !viewingArchived && Boolean(operations.moveRows);
  // The cards dropped are chosen, as Notion's (seen 2026-11-12), a chosen one carrying the other chosen ones (2026-11-24); into another group they take its value in the move's one step of ⌘Z (moveShownRows, groupMoveValues), a card under several groups by its line there (viewLinesDrag).
  const cardReorder = useDragReorder(`gallery-card:${view.id}`, (_sourceId, targetId, side, carried) => { const moved = drag.moved(carried, targetId); selection.chooseMany(moved.landed);
    void operations.moveRows?.(moved.rowIds, moved.targetId, side, context, groupMoveValues(groupProperty, moved.rowIds, moved.to, moved.groupOf, viewConfig, snapshot.rows)).catch((error) => operations.onError(error, context));
  }, { axis: "horizontal", gaps: true, carry: (sourceId) => drag.carry(sourceId) });
  // "+ 신규 그룹" after the last group adds what its grouping takes (viewGroupAddKind), as a list's.
  const addKind = !useViewAddsGroups("gallery") || readOnly || !operations.commitProperty || !operations.updateViewConfig ? undefined : viewGroupAddKind(groupProperty, viewConfig);
  const keepGroup = useKeepNewGroup((key) => void operations.updateViewConfig?.(tableConfigAfterGroupKept(viewConfig, key), context));
  function addGroup(name: string) {
    if (!groupProperty) return;
    if (addKind && addKind !== "option") return void operations.updateViewConfig?.(tableConfigAfterGroupKept(viewConfig, viewGroupAddedKey(addKind, name),
      addKind === "person" ? (galleryGroups ?? []).map((group) => group.key) : undefined), context, true);
    const config = groupProperty.config as Record<string, unknown>;
    const options = Array.isArray(config.options) ? config.options : [];
    const option = newNamedOption(name);
    void operations.commitProperty?.({ ...groupProperty, config: { ...config, options: [...options, option] } } as GraphProperty, context)
      .then(() => keepGroup(option.id));
  }
  const propertyValueContext = useMemo<PropertyValueReadContext>(() => ({
    ...snapshot.propertyValueContext,
    contextIdentity: snapshot.contextIdentity,
    // The related pages the view has read stay beside its own cards.
    pagesById: pagesOver(snapshot.propertyValueContext.pagesById, pagesById),
    propsByDatabaseId: {
      ...snapshot.propertyValueContext.propsByDatabaseId,
      [snapshot.dataSource.id]: props,
    },
    snapshotIdentity: snapshot.snapshotIdentity,
  }), [pagesById, props, snapshot]);
  // Page find reads a long gallery's cards from their data, drawn or not (GalleryWindow.tsx).
  const searchContext = useMemo(() => ({ props, pagesById, currentPageId: snapshot.contextPageId, bounds: snapshot.queryBounds }),
    [pagesById, props, snapshot.contextPageId, snapshot.queryBounds]);
  const findText = (row: GraphRow) => {
    const page = pagesById[row.id];
    return !page ? "" : [showTitle ? rowTitle(page) : "", ...others.map((property) => propertySearchText(page, property, searchContext))].join("\n");
  };
  // A card reads its preview as the gallery has it now; its page's revision says when to read it again.
  const previewsNow = useRef(snapshot.pageContentPreviews);
  previewsNow.current = snapshot.pageContentPreviews;
  const [previewTransport] = useState<PageContentPreviewTransport>(() => async (pageId: string) => {
    const preview = previewsNow.current[pageId];
    return { pageId, previewUrl: preview?.previewUrl, revision: preview?.revision ?? 0 };
  });
  const { openRowContextMenu, openRowContextMenuFromElement, rowContextMenu } = operations;

  function openRow(rowId: string, mode: GalleryRowOpenMode = "default") {
    if (!operations.isCurrent(context) || context.signal.aborted) return;
    if (mode === "default") operations.openRow(rowId, context);
    else operations.openRowIn(rowId, mode, context);
  }

  const selection = useCardSelection({ keep: snapshot.view.id, onMenu: openRowContextMenuFromElement, onOpen: openRow,
    onTrash: readOnly || !operations.trashRows ? undefined : (ids) => void operations.trashRows?.(ids),
    onDuplicate: readOnly || !operations.duplicateRows ? undefined : (ids) => {
      const { rowIds, place } = copiesPlace((galleryGroups ?? [{ key: undefined, rows }]).map((group) => group.rows.map(({ id }) => groupLine(group.key, id))), ids, rows.map(({ id }) => id));
      return operations.duplicateRows?.(rowIds, place);
    } });
  const lineOf = (group: string | undefined, rowId: string) => groupLine(galleryGroups ? group : undefined, rowId);
  const drag = viewLinesDrag((galleryGroups ?? [{ key: undefined, rows: cardRows }]).flatMap((group) => group.rows.map(({ id }) => ({ kind: "row", line: lineOf(group.key, id) }))),
    (card) => ({ line: card.line, parentId: null, depth: 0 }), selection.value.chosen);

  function openRowClick(rowId: string, e: ReactMouseEvent<HTMLElement>) {
    openRow(rowId, rowOpenModeOfClick(e));
  }

  function openRowKey(rowId: string, e: ReactKeyboardEvent<HTMLElement>) {
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      e.preventDefault();
      e.stopPropagation();
      openRowContextMenuFromElement(rowId, e.currentTarget, selection.copyHere(e.currentTarget, rowId));
      return;
    }
    if (!fieldEnter(e) && e.key !== " ") return;
    e.preventDefault(); openRow(rowId, e.metaKey || e.ctrlKey ? "new_tab" : "default");
  }

  async function createRow(groupKey?: string, place?: { beforeRowId?: string; afterRowId?: string }) {
    if (readOnly) return;
    const seed = groupKey !== undefined && groupProperty ? tableGroupSeedValue(groupProperty, groupKey, viewConfig) : undefined;
    const seeds = seed === undefined ? undefined : { [groupProperty!.id]: seed };
    // Its page opens as the card shows, drawn with it: opened once the
    // making settled, the card drawn first, it waited for the card's drawing
    // (2026-11-15: the page opened 160–190 ms after the press, NAS + CPU ×2).
    let opened: string | null = null;
    const open = (row: GraphRow, context: GalleryViewOperationContext) => {
      opened = row.id;
      requestPageTitleFocus(row.id);
      operations.openRow(row.id, context);
    };
    await holdTypingWhileRowOpens(() => createCoordinator.run(context, {
      createRow: (context) => operations.createRow(context, seeds, place, (row) => {
        if (operations.isCurrent(context) && !context.signal.aborted) open(row, context);
      }),
      isCurrent: (context) => operations.isCurrent(context),
      onError: (error, context) => operations.onError(error, context),
      openRow: (row, context) => { if (opened !== row.id) open(row, context); },
    }));
  }

  // What every card draws alike, and what its presses do by the gallery as last drawn (GalleryCard).
  // Its values' editors take the gallery's latest ways, the same object as the cards' own drawings stay (liveEnvironment).
  const environmentNow = useRef(snapshot.propertyCellEnvironment);
  environmentNow.current = snapshot.propertyCellEnvironment;
  const [latestEnvironment] = useState(() => liveEnvironment(environmentNow));
  const cardFrame = useSteadyFrame<GalleryCardFrame>({
    props, others, coverProperty, showPreview, showTitle, fitImage, wrap, pageIcons: viewShowsPageIcons(view.config ?? {}),
    readOnly: readOnly || !titleProperty, environment: readOnly || !snapshot.propertyCellEnvironment ? undefined : latestEnvironment,
    useWorkspaceFile: snapshot.useWorkspaceFile, previewTransport, contextIdentity: snapshot.contextIdentity,
    subitems: { mode: presentation.mode, scope: presentation.scope, childrenPropertyId: presentation.childrenPropertyId },
  });
  const cardWays = useLiveWays<GalleryCardWays>({
    open: openRow,
    openClick: openRowClick,
    openKey: openRowKey,
    contextMenu: (rowId, event) => { selection.menuChoose(event.currentTarget); openRowContextMenu(rowId, event, selection.copyHere(event.currentTarget, rowId)); },
    menu: (rowId, element) => { selection.choose(element.closest<HTMLElement>("[data-card-choice]")?.dataset.cardChoice ?? rowId); openRowContextMenuFromElement(rowId, element, selection.copyHere(element, rowId)); },
    commitTitle: async (rowId, title) => {
      if (!titleProperty || readOnly || !operations.isCurrent(context)) return;
      try { await operations.setRowProperty(rowId, titleProperty.id, title, context); }
      catch (error) { if (operations.isCurrent(context)) operations.onError(error, context); }
    },
  });

  function renderCard(row: GraphRow, group?: string) {
    const page = pagesById[row.id], line = lineOf(group, row.id); if (!page) return null;
    const reorder = moves ? cardReorder.row(line, (source) => drag.takes(source, row.id), true) : undefined;
    return (
      <CardFullLineContext.Provider key={line} value={fullLine}>
      <GalleryCard row={page} title={rowTitle(page)} tint={cardColor(page)?.color ?? undefined}
        childCount={presentation.childCountByParent.get(row.id) ?? 0}
        parent={presentation.mode === "flattened" && page.subitemParentId ? pagesById[page.subitemParentId] : undefined}
        rowRef={reorder?.ref} dragging={reorder?.["data-dragging"]} dropSide={reorder?.["data-drop-side"]}
        frame={cardFrame} context={propertyValueContext} ways={cardWays} line={line} />
      </CardFullLineContext.Provider>
    );
  }

  const contentPreview = coverProperty === CARD_PREVIEW_PAGE_CONTENT;
  if (galleryGroups && groupProperty) {
    // No group to show — each hidden, or the empty ones while its filters leave none — and the gallery says so in their place with a board's actions, as Notion's (seen 2026-11-27 twice), its "+ 새 페이지" opening the card's page as a gallery's does.
    const { searching, queried } = viewQuery(view, props, snapshot.queryBounds);
    if (!galleryGroups.length) return <div className={styles.galleryGroups} data-gallery-groups><DatabaseNoResults searching={searching} onEditFilters={operations.openFilterSettings} onEditGroups={queried ? undefined : operations.openGroupSettings} onClearSearch={operations.clearSearch} onAddRow={readOnly ? undefined : () => void createRow()} />{rowContextMenu}</div>;
    const adds = !readOnly && offersNewRows;
    const head = (group: (typeof galleryGroups)[number], collapsed: boolean) => (
      <div className={`${styles.listGroupHead} ${styles.galleryGroupHead}`} data-gallery-group-head>
        <GroupHeadContent group={group} collapsed={collapsed}
          onToggle={() => setCollapsedGroups((current) => {
            const next = new Set(current);
            if (next.has(group.key)) next.delete(group.key); else next.add(group.key);
            return next;
          })}
          count={viewConfig?.showGroupCounts === true ? group.rows.length : undefined}
          onMenu={readOnly || !operations.updateViewConfig ? undefined : (element, naming) => setGroupMenu({
            group: { ...group, rows: group.rows.map((row) => ({ row })) }, position: popoverAnchorOf(element), returnTo: element, naming })}
          onAdd={adds ? () => void createRow(group.key, { beforeRowId: group.rows[0]?.id }) : undefined} />
      </div>
    );
    const tile = (group: (typeof galleryGroups)[number]) => !adds ? undefined : withExtra(
      <button type="button" className={styles.galleryNew} title={newPageLabel} aria-label={newPageLabel}
        onClick={() => void createRow(group.key, { afterRowId: group.rows.at(-1)?.id })} data-gallery-group-add={group.key}>
        <Plus size="small" aria-hidden="true" /> {newItemText}
      </button>,
    );
    const shown = (group: (typeof galleryGroups)[number]) => groupLoads.cut(group.key, group.rows), tiles = (group: (typeof galleryGroups)[number]) => shown(group).more ? <>{tile(group)}<CardsLoadMore placement="view" limit={groupLoads.step} onLimit={groupLoads.onLimit} onLoadMore={() => groupLoads.loadMore(group.key)} /></> : tile(group);
    const places = galleryGroups.reduce((sum, group) => sum + 1 + (collapsedGroups.has(group.key) ? 0 : shown(group).rows.length + 1), 0);
    return (
      <CardSelectionProvider value={selection.value}>
      <div ref={cover.ref} style={cover.style} className={styles.galleryGroups} data-gallery-groups>
        {places > GALLERY_WINDOW_THRESHOLD ? (
          <GalleryWindow size={cardSize} layout={cardLayout} contentPreview={contentPreview} renderCard={renderCard}
            dragging={cardReorder.dragging} findText={findText}
            sections={galleryGroups.map((group) => ({ key: group.key, head: head(group, collapsedGroups.has(group.key)), rows: shown(group).rows,
              tile: tiles(group), collapsed: collapsedGroups.has(group.key) }))} />
        ) : galleryGroups.map((group) => {
          const collapsed = collapsedGroups.has(group.key);
          return (
            <section key={group.key} className={styles.galleryGroup} data-gallery-group data-gallery-group-key={group.key}
              data-gallery-group-label={group.label} data-gallery-group-count={group.rows.length}>
              {head(group, collapsed)}
              {collapsed ? null : (
                <div className={`${styles.gallery} ${styles.galleryGroupGrid}`} data-gallery-grid data-size={cardSize} data-card-layout={cardLayout}
                  data-page-content-preview={contentPreview ? "true" : undefined}>
                  {shown(group).rows.map((row) => renderCard(row, group.key))}
                  {tiles(group)}
                </div>
              )}
            </section>
          );
        })}
        {addKind ? (
          <div className={styles.galleryNewGroupRow}>
            <ViewGroupAdd kind={addKind} property={groupProperty} environment={snapshot.propertyCellEnvironment}
              className={styles.listNewGroup} marker="gallery-new-group"
              taken={addKind === "option" ? groupOptionNames(groupProperty) : galleryGroups.map((group) => group.label ?? "")}
              has={(value) => addKind !== "option" && galleryGroups.some((group) => group.key === viewGroupAddedKey(addKind, value))} onAdd={addGroup} />
          </div>
        ) : null}
        {groupMenu ? <ViewGroupOptions at={groupMenu} property={groupProperty} config={viewConfig}
          structureLocked={readOnly} onClose={() => setGroupMenu(null)}
          onConfig={(next) => void operations.updateViewConfig?.(next, context, true)}
          onProperty={(next) => void operations.commitProperty?.(next, context)}
          onTrash={operations.trashRows ? (ids) => void operations.trashRows?.(ids) : undefined}
          onOpenSettings={operations.openGroupSettings} /> : null}
        {rowContextMenu}
      </div>
      </CardSelectionProvider>
    );
  }

  // With no card, from no page or a search or filter, only its "+ 새 페이지" tile shows, as Notion's (seen 2026-10-11); with none to one who may not add, "결과 없음" (viewNothingToAdd).
  const tile = readOnly || !offersNewRows ? viewNothingToAdd(cardRows.length, viewQuery(view, props, snapshot.queryBounds).queried, readOnly) : withExtra(
    <button type="button" className={styles.galleryNew} title={newPageLabel} aria-label={newPageLabel}
      onClick={() => void createRow()}>
      <Plus size="small" aria-hidden="true" /> {newItemText}
    </button>,
  );
  const after = <>{operations.loadMoreCards ? <CardsLoadMore placement="view" {...operations.loadMoreCards} /> : null}{rowContextMenu}</>;
  return (
    <CardSelectionProvider value={selection.value}>
    {cardRows.length + 1 > GALLERY_WINDOW_THRESHOLD ? (
      <div ref={cover.ref} style={cover.style} data-gallery-windowed>
        <GalleryWindow size={cardSize} layout={cardLayout} contentPreview={contentPreview} renderCard={renderCard}
          dragging={cardReorder.dragging} findText={findText} sections={[{ key: "", rows: cardRows, tile }]} />
        {after}
      </div>
    ) : (
      <div ref={cover.ref} style={cover.style} className={styles.gallery} data-gallery-grid data-size={cardSize} data-card-layout={cardLayout}
        data-page-content-preview={contentPreview ? "true" : undefined}>
        {cardRows.map((row) => renderCard(row))}
        {tile}
        {after}
      </div>
    )}
    </CardSelectionProvider>
  );
}
