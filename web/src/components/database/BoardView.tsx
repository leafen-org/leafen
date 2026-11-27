import { popoverAnchorOf, type PopoverRect } from "@/lib/popoverPlacement";
"use client";
import { copiesPlace, viewKeepsOwnRowOrder, viewRowOrderPlacing, type CopiesPlace } from "./viewRowOrder"; import { groupLine, lineRowId } from "./viewGroupLines";
import { useCommittedOperationScope } from "../product/useCommittedOperationScope";
import { boardCellTakesCard } from "./boardCellTakesCard";
import { newId } from "@/lib/ids";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { OwnPageViewBar } from "./OwnPageViewBar";
import { useOptionalProductToast } from "@/lib/productToast";
import { useCallback, useEffect, useId, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { usePageWideTable } from "./usePageWideTable";
import { useStickyViewHeads } from "./useStickyViewHeads";
import { useOwnPageViewFill } from "./useOwnPageViewFill";
import { useBoardSpan } from "./useBoardSpan";
import { announce } from "@atlaskit/pragmatic-drag-and-drop-live-region";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "../../contentGraph";
import type { PropertyValueReadContext } from "./PropValue";
import type { DbView, Page, ViewConfig } from "@/lib/types";
import { usePageNames } from "@/lib/pageNames";
import { BoardColumn } from "./BoardColumn";
import { useBoardCardDrafts } from "./useBoardCardDrafts";
import { CardFullLineContext, compactFullLineIds } from "./cardFullLine";
import { newRowOpensItsPage } from "@/lib/pageTitleFocus";
import { makeRowThatOpens, newRowSeedsShow } from "./newViewRow";
import { DatabaseNoResults, viewQuery } from "./DatabaseNoResults";
import { BoardLanes } from "./BoardLanes";
import { ViewGroupAdd } from "./ViewGroupAdd";
import { useViewAddsGroups, viewGroupAddedKey, viewGroupAddKind } from "./viewGroupAddKind";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { GroupOptionsMenu } from "./GroupOptionsMenu";
import { BoardGroupMenu } from "./BoardGroupMenu";
import { GroupRename } from "./GroupNameField";
import { optionNamed } from "./optionNameText";
import type { PageContentPreviewTransport, WorkspaceFileHook } from "./CardPreviewMedia";
import {
  boardGroupMode,
  boardGroupReplacementValue,
  hasEditableBoardGroupOptions,
  isBoardMainGroupProperty,
  type BoardGroupingBounds,
  type BoardMainGroupProperty,
} from "./boardGrouping";
import { boardBlockNeighbours } from "./boardCardOwnPlace"; import { boardListSwap } from "./viewGroupMove";
import {
  boardMoveGroupValue,
  reorderBoardOptionsById,
  type BoardBucket,
  type BoardOption,
} from "./boardViewModel";
import { isBoardValueGroupProperty } from "./boardValueGrouping";
import { boardColumns } from "./boardColumns";
import { createListOperationCoordinator } from "./listViewModel";
import { galleryPresentationProperty, type GalleryPagePresentation } from "./galleryViewModel";
import { pagesOver, presentedPagesById, rowPagesInOrder } from "./presentedPages";
import { conditionalColorResolver } from "./conditionalColorModel";
import { projectCardViewSubitems, visibleViewProperties, type QueryBounds } from "./query";
import { projectRows, rowsAsNow, useDeferredPlacing } from "./viewRowProjection";
import { viewFilterSeedValues } from "./querySeeds";
import { cardPreviewMode, hasCardPreview } from "./cardPreviewModePolicy";
import { isStatusDefaultOption } from "./propertyStatusPolicy";
import { tellDefaultOptionKept } from "./PropertySelectCellView";
import styles from "./databaseSheet";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { useDatabaseIcon } from "./databaseOpenIcon";
import { useDatabaseOpenHref } from "./databaseOpenHref";
import { useNewRowTemplate } from "./useDefaultTemplateRows";
import { boardCardLoads, tableInitialLoadLimit } from "./queryProjection";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { boardSubgroupMode } from "./boardSubgroupModel";
import { chosenOptionColor } from "./colors";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { tableConfigAfterGroupKept, tableConfigAfterGroupMoved, tableConfigAfterGroupShown, tableHiddenGroupKeys } from "./tableGroupSettingsModel";
import { boardGroupPropertyId } from "./viewLayoutDefaults";
import { arrangeBoardColumns, boardConfigAfterGroupPinned, boardConfigWithGroupCounts, boardGroupCountHidden, BoardGroupsPublisher, boardGroupsSurface, boardPinnedGroupKeys,
  boardStatusKeepsViewOrder } from "./boardGroupSettings";
import { BoardSideGroups } from "./BoardSideGroups";
import { boardGroupPreviewFilters } from "./boardGroupPreviewModel";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { rowValuesLocked } from "./rowValuesLocked";
import { useBoardLanes } from "./useBoardLanes";
import { CardSelectionProvider, cardLine, useCardSelection } from "./cardSelection";

// A coloured column's width and the space between, and "+ 신규 그룹" after them.
const BOARD_COLUMN_WIDTH = 276;
const BOARD_COLUMN_WIDTH_OF_SIZE: Record<string, number> = { small: 196, large: 336 };
const BOARD_COLUMN_GAP = 12;
const BOARD_ADD_GROUP_WIDTH = 120;

export type BoardRowOpenMode = "default" | "new_tab" | "side" | "center" | "full";

export interface BoardViewSnapshot {
  snapshotIdentity: string;
  contextIdentity: string;
  requestGeneration: number;
  workspaceId: string;
  dataSource: GraphDataSource;
  view: GraphView;
  properties: readonly GraphProperty[];
  rows: readonly GraphRow[];
  pagesById: Readonly<Record<string, GraphRow>>;
  /** Whether its rows are every row the view has read, its cards' pages made from them alone (presentedPages.ts). */
  rowsAreAll?: boolean;
  pagePresentationById: Readonly<Record<string, GalleryPagePresentation>>;
  pageContentPreviews: Readonly<Record<string, { previewUrl?: string; revision: number }>>;
  propertyValueContext: PropertyValueReadContext;
  queryBounds: QueryBounds;
  groupingBounds: BoardGroupingBounds;
  useWorkspaceFile: WorkspaceFileHook;
  currentUserId?: string;
  contextPageId?: string;
  databaseFeatures?: Page["databaseFeatures"];
  /** What a pinned group's preview filters with, and the database's own container a view saved from it links. */
  filterSnapshot?: DatabaseFilterSnapshot;
  /** Edits a value in place: the property slots of a card being made (BoardColumn's draft card). */
  propertyCellEnvironment?: PropertyCellEnvironment;
  sourceContainerId?: string;
}

export interface BoardViewOperationContext {
  contextIdentity: string;
  dataSourceId: string;
  requestGeneration: number;
  requestKey: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  viewId: string;
}

/** Where a card moved goes and the values it takes there. */
export type BoardCardChange = { propertyId: string; value: unknown; beforeId: string | null; afterId: string | null; subgroupPropertyId?: string; subgroupValue?: unknown; keepPosition?: boolean; view?: GraphView };
export interface BoardViewOperations {
  openGroupSettings?(trigger: HTMLElement, subgroup?: boolean): void;
  openFilterSettings?(trigger: HTMLElement): void;
  clearSearch?(): void;
  trashRows?(rowIds: readonly string[], context: BoardViewOperationContext): Promise<void>;
  duplicateRows?(rowIds: readonly string[], context: BoardViewOperationContext, place?: CopiesPlace): Promise<readonly string[] | void>;
  createGroupProperty(type: "select" | "status", context: BoardViewOperationContext): Promise<GraphProperty | null>;
  setRowProperty(rowId: string, propertyId: string, value: unknown, context: BoardViewOperationContext): Promise<void>;
  createRowInGroup(propertyId: string, value: unknown, seeds: { title?: string; properties: Record<string, unknown>; beforeRowId?: string }, context: BoardViewOperationContext): Promise<GraphRow | null>;
  updateGroupOptions(propertyId: string, options: readonly BoardOption[], context: BoardViewOperationContext, view?: GraphView): Promise<void>;
  /** `view` is the view with the card in its place, where it keeps its own order; moveCards moves several cards in one step of ⌘Z, each after the one before. */
  moveRow(rowId: string, change: BoardCardChange, context: BoardViewOperationContext): Promise<void>;
  moveCards?(moves: readonly { rowId: string; change: BoardCardChange }[], context: BoardViewOperationContext): Promise<void>;
  /** A person's change to the view (`step`) is a step of the page's ⌘Z; one written for another change, as a new card's place, is not. */
  updateView(view: GraphView, context: BoardViewOperationContext, step?: boolean): Promise<void>;
  isCurrent(context: BoardViewOperationContext): boolean;
  onError(error: unknown, context: BoardViewOperationContext): void;
  openRow(rowId: string, context: BoardViewOperationContext): void;
  openRowIn(rowId: string, mode: BoardRowOpenMode, context: BoardViewOperationContext): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>, duplicate?: () => void): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement, duplicate?: () => void): void;
  rowContextMenu: ReactNode;
}

function presentationView(view: GraphView): DbView {
  return { id: view.id, databaseId: view.dataSourceId, name: view.name, type: view.type as DbView["type"], config: view.config as ViewConfig, position: 0 };
}

const BOARD_GROUP_STEP = 10;
// A board's cards show their content's preview as of its revision.
const BOARD_PAGES = { previews: true };
/**
 * A board with no property to group by asks for one; given one, it draws its
 * columns. The two are drawn apart, so making the property (its "상태" or
 * "선택") starts the columns afresh rather than adding their hooks to the
 * prompt's (2026-11-08: choosing "상태" broke the page).
 */
export function BoardView(props: { snapshot: BoardViewSnapshot; operations: BoardViewOperations; readOnly?: boolean }) {
  const id = boardGroupPropertyId(props.snapshot.view.config, props.snapshot.properties);
  const index = props.snapshot.properties.findIndex((property) => property.id === id);
  const shown = index >= 0 ? galleryPresentationProperty(props.snapshot.properties[index], index) : undefined;
  const grouped = isBoardMainGroupProperty(shown) || isBoardValueGroupProperty(shown);
  return <GroupedBoardView key={grouped ? "grouped" : "ungrouped"} {...props} />;
}

function GroupedBoardView({ snapshot, operations, readOnly = false }: { snapshot: BoardViewSnapshot; operations: BoardViewOperations; readOnly?: boolean }) {
  const { t } = useTranslation(["boardView", "common", "databaseView", "propertyTypes"]), pageNames = usePageNames();
  const toast = useOptionalProductToast();
  // A locked database keeps its cards movable but not its groups and view.
  const structureLocked = useDatabaseStructureLocked();
  const [draggingId, setDraggingId] = useState<string | null>(null);
  // The card (and its column) or column being dragged, read as it drops: a drag let go before the board draws again still lands.
  const dragged = useRef<{ card?: string; from?: string; group?: string; carried?: readonly string[] }>({});
  const moveQueue = useRef<Promise<void> | null>(null);
  // The move a queued card makes is this render's, read when its turn comes.
  const moveCardRef = useRef(moveCard);
  moveCardRef.current = moveCard;
  const dragScope = useId();
  const [editGroupMenu, setEditGroupMenu] = useState(false);
  const [menu, setMenu] = useState<{ option: BoardOption; position: PopoverRect; returnTo: HTMLElement; naming?: boolean } | null>(null);
  const [laneMenu, setLaneMenu] = useState<{ lane: BoardBucket<GraphRow>; position: PopoverRect; returnTo: HTMLElement } | null>(null);
  // An inline board, or a sub-grouped one anywhere, first shows 25 cards in each column (or cell), or as many
  // as its view says, and 20 more at each "더 불러오기" (Notion's, seen 2026-10-01, 2026-11-15); a card
  // made here shows at once. A database's own page first shows 25 too and 20 more in every column as a
  // column's end nears the window's foot, keeping those drawn (seen 2026-11-07); a phone's own page 10 and 10 more (boardCardLoads).
  const ownPage = useDatabaseOwnPage(), { byButton, first: initialCards, more: moreCards, nearEnd: nearEndDistance } = boardCardLoads(snapshot.view, ownPage, useSidebarLayout(true).phone);
  const openHref = useDatabaseOpenHref(snapshot.dataSource.id), openIcon = useDatabaseIcon(snapshot.dataSource);
  const cardWindowKey = `${snapshot.view.id}:${initialCards}`;
  const [cardWindow, setCardWindow] = useState<{ key: string; count: number; made: ReadonlySet<string> }>({ key: "", count: 0, made: new Set() });
  const currentCardWindow = cardWindow.key === cardWindowKey ? cardWindow : { key: cardWindowKey, count: initialCards, made: new Set<string>() };
  const [pendingMutationKeys, setPendingMutationKeys] = useState<ReadonlySet<string>>(new Set());
  const pendingMutationKeysRef = useRef(new Set<string>());
  const lifecycleController = useCommittedOperationScope([snapshot.contextIdentity, snapshot.requestGeneration, snapshot.snapshotIdentity]);
  const coordinator = useMemo(() => createListOperationCoordinator(), []);
  // Where the view can't be changed, sub-groups fold for this viewer alone.
  const [ownFolds, setOwnFolds] = useState<ReadonlySet<string> | null>(null);

  useEffect(() => { setMenu(null); setDraggingId(null); dragged.current = {}; }, [lifecycleController]);

  const baseContext = useMemo(() => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    requestGeneration: snapshot.requestGeneration,
    signal: lifecycleController.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.view.id,
  }), [lifecycleController, snapshot]);

  const handlers = { isCurrent: (context: BoardViewOperationContext) => operations.isCurrent(context),
    onError: (error: unknown, context: BoardViewOperationContext) => operations.onError(error, context), publish: () => {} };
  // A press that makes something (the group property, a card) runs once, its buttons showing it is making it.
  async function runMaking<Result>(requestKey: string, perform: (context: BoardViewOperationContext) => Promise<Result>) {
    if (readOnly || pendingMutationKeysRef.current.has(requestKey)) return false;
    pendingMutationKeysRef.current.add(requestKey);
    setPendingMutationKeys(new Set(pendingMutationKeysRef.current));
    try {
      return await coordinator.run({ ...baseContext, requestKey }, { ...handlers, perform });
    } finally {
      pendingMutationKeysRef.current.delete(requestKey);
      setPendingMutationKeys(new Set(pendingMutationKeysRef.current));
    }
  }
  // A write goes as made, never refused nor held for another of its key still saving (listViewModel.ts).
  async function runMutation<Result>(requestKey: string, perform: (context: BoardViewOperationContext) => Promise<Result>) {
    if (readOnly) return false;
    return coordinator.write({ ...baseContext, requestKey }, { ...handlers, perform });
  }
  // A person's change to the view is a step of ⌘Z, as Notion's (seen 2026-11-12: a group hidden, its count shown, a column moved); a sub-group folded is not, nor what is written for another change (`step` false).
  const writeView = (config: GraphView["config"], step = true) => runMutation(`view-write:${snapshot.view.id}`, (context) => operations.updateView({ ...snapshot.view, config }, context, step));

  const props = useMemo(() => snapshot.properties.map(galleryPresentationProperty), [snapshot.properties]);
  const titleProperty = snapshot.properties.find((property) => property.type === "title");
  // Its rows' pages, made again for the rows changed alone (presentedPages.ts).
  const pagesById = useMemo(() => presentedPagesById(snapshot, titleProperty, BOARD_PAGES), [snapshot, titleProperty]);
  const rows = useMemo(() => rowPagesInOrder(snapshot.rows, pagesById) as Page[] | undefined ?? snapshot.rows.flatMap((row) => pagesById[row.id] ?? []), [pagesById, snapshot.rows]);
  const view = useMemo(() => presentationView(snapshot.view), [snapshot.view]);
  const newRowTemplate = useNewRowTemplate(snapshot.dataSource.id, snapshot.view);
  const cardColor = useMemo(() => conditionalColorResolver(view.config as Record<string, unknown>, props, {
    pagesById, currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId,
  }, snapshot.queryBounds), [pagesById, props, snapshot.contextPageId, snapshot.currentUserId, snapshot.queryBounds, view.config]);
  const database = useMemo(() => dataSourcePage(snapshot.dataSource, snapshot.workspaceId, 0, { databaseFeatures: snapshot.databaseFeatures }), [snapshot]);
  // Its rows its filters keep in its sorts' order, or its own: an edit that arrives places the rows it changed alone (viewRowProjection.ts), and a value picked shows on its card,
  // in its new group, at once, the order worked out behind the press (useDeferredPlacing; a card made, moved or gone is placed at once).
  const placing = useDeferredPlacing(snapshot.rows, snapshot.view), placedFrom = useMemo(() => projectRows({ currentUserId: snapshot.currentUserId, pageId: snapshot.contextPageId ?? "", properties: snapshot.properties, rows: placing.rows, view: placing.view, workspaceId: snapshot.workspaceId }), [placing, snapshot.contextPageId, snapshot.currentUserId, snapshot.properties, snapshot.workspaceId]);
  const placed = useMemo(() => rowsAsNow(placedFrom, placing.rows, snapshot.rows), [placedFrom, placing.rows, snapshot.rows]);
  const shownRows = useMemo(() => rowPagesInOrder(placed, pagesById) as Page[] | undefined ?? placed.flatMap((row) => pagesById[row.id] ?? []), [pagesById, placed]);
  const presentation = useMemo(() => projectCardViewSubitems(database, view, shownRows, snapshot.queryBounds), [database, shownRows, snapshot.queryBounds, view]);
  const cardRows = useMemo(() => {
    if (presentation.rows === shownRows) return placed;
    const byId = new Map(placed.map((row) => [row.id, row]));
    return presentation.rows.flatMap((row) => byId.get(row.id) ?? []);
  }, [placed, presentation.rows, shownRows]);
  const visible = useMemo(() => visibleViewProperties(props, view, snapshot.queryBounds), [props, snapshot.queryBounds, view]);
  const groupPropertyId = boardGroupPropertyId(snapshot.view.config, snapshot.properties);
  const shownGroupProperty = props.find((property) => property.id === groupPropertyId);
  // By a title, text, number, date, formula or relation, its groups are what its cards hold, as a table's.
  const groupProperty = isBoardMainGroupProperty(shownGroupProperty) || isBoardValueGroupProperty(shownGroupProperty)
    ? shownGroupProperty as BoardMainGroupProperty : undefined;

  async function createGroupProperty(type: "select" | "status") {
    await runMaking(`schema-view:${snapshot.dataSource.id}`, async (context) => {
      return operations.createGroupProperty(type, context);
    });
  }

  if (!groupProperty) {
    return (
      <div className={styles.viewEmpty}>
        <div className={styles.viewEmptyTitle}>{t("boardView:groupThisBoard")}</div>
        <div className={styles.viewEmptyDesc}>{t("boardView:groupThisBoardDesc")}</div>
        {!readOnly && !structureLocked && <div><button type="button" className={styles.viewEmptyAction} disabled={pendingMutationKeys.has(`schema-view:${snapshot.dataSource.id}`)} onClick={() => void createGroupProperty("status")}>{t("propertyTypes:labels.status")}</button><button type="button" className={styles.viewEmptyAction} disabled={pendingMutationKeys.has(`schema-view:${snapshot.dataSource.id}`)} onClick={() => void createGroupProperty("select")}>{t("propertyTypes:labels.select")}</button></div>}
      </div>
    );
  }

  const activeGroupProperty = groupProperty;
  const groupMode = boardGroupMode(view.config, activeGroupProperty, snapshot.groupingBounds);
  // Its columns, kept, and followed for the cards an edit that arrives changes (boardColumns.ts). A filter on the grouped property
  // leaves its other groups on the board, empty, as Notion's (seen 2026-11-05: "상태: Done" kept Not started and In progress).
  const { valueGroups, options: allGroupOptions, columns: groupColumns, keysOf: columnKeysOf } = boardColumns({ property: activeGroupProperty,
    graphProperty: snapshot.properties.find((candidate) => candidate.id === activeGroupProperty.id)!, rows: cardRows, pages: presentation.rows,
    config: view.config as Record<string, unknown>, properties: snapshot.properties, pagesById: snapshot.propertyValueContext.pagesById,
    propsByDatabaseId: snapshot.propertyValueContext.propsByDatabaseId,
    currentUserId: snapshot.currentUserId, bounds: snapshot.groupingBounds, groupMode,
    emptyLabel: t("boardView:noValue", { groupName: activeGroupProperty.name }) });
  // A card made under a title's group starts titled with it, as Notion's (seen 2026-10-11).
  const titleGroups = (activeGroupProperty.type as string) === "title";
  const checkboxGroups = activeGroupProperty.type === "checkbox";
  const columns = groupColumns.map((column) => checkboxGroups ? { ...column, checked: column.key === "true" } : column);
  // Hidden groups leave the board, in the order and with the empty ones its settings say; pinned ones, hidden or empty too, wait after it.
  const hiddenGroupKeys = tableHiddenGroupKeys({ ...view.config });
  const statusColumns = activeGroupProperty.type === "status" && !boardStatusKeepsViewOrder(view.config ?? {});
  const arranged = arrangeBoardColumns(columns, view.config ?? {}, Boolean(valueGroups), activeGroupProperty.type);
  const shownColumns = arranged.filter((column) => !hiddenGroupKeys.has(column.key));
  // Ten groups at first and ten more at a time, by "⋯ 그룹 더 불러오기" after the last, as Notion's board (seen 2026-11-05).
  const groupWindowKey = `${view.id}\n${activeGroupProperty.id}`;
  const [groupWindow, setGroupWindow] = useState({ key: groupWindowKey, count: BOARD_GROUP_STEP });
  const groupCount = groupWindow.key === groupWindowKey ? groupWindow.count : BOARD_GROUP_STEP;
  const windowColumns = shownColumns.slice(0, groupCount);
  // Wider than the page's text, an inline board scrolls across the page's
  // margins to the window's edge, as its table does (Notion's, seen
  // 2026-11-02: 276px columns 12px apart running to 1264 at 1280px).
  const boardRef = useRef<HTMLDivElement>(null);
  usePageWideTable(boardRef, shownColumns.length * ((BOARD_COLUMN_WIDTH_OF_SIZE[String(view.config?.cardSize)] ?? BOARD_COLUMN_WIDTH) + BOARD_COLUMN_GAP) + BOARD_ADD_GROUP_WIDTH);
  useOwnPageViewFill(boardRef);
  useBoardSpan(boardRef);
  useStickyViewHeads(boardRef, { gap: 8, shape: shownColumns.length });
  const pinnedKeys = boardPinnedGroupKeys(view.config ?? {});
  const pinnedColumns = arrangeBoardColumns(columns, { ...view.config, hideEmptyGroups: false }, Boolean(valueGroups), activeGroupProperty.type).filter((column) => pinnedKeys.has(column.key));
  function setGroupHidden(key: string, hidden: boolean) {
    void writeView(tableConfigAfterGroupShown(snapshot.view.config ?? {}, key, !hidden));
  }
  const countHidden = (key: string) => boardGroupCountHidden(view.config ?? {}, key);
  function toggleGroupCount(key: string) {
    if (readOnly || structureLocked) return;
    void writeView(boardConfigWithGroupCounts(snapshot.view.config ?? {}, countHidden(key)));
  }
  const groupEditable = !structureLocked && !valueGroups && hasEditableBoardGroupOptions(activeGroupProperty, snapshot.groupingBounds, groupMode);
  // "+ 신규 그룹" adds what its grouping takes (viewGroupAddKind): an option asks its name first, as Notion's board does (seen
  // 2026-10-30); a status's groups are fixed and have none (2026-11-04); a person, text or related page is kept as a column.
  const groupAdd = useViewAddsGroups("board"), addsGroups = groupAdd === true && !readOnly && !structureLocked, inertAdd = groupAdd === "inert" && !structureLocked;
  const addKind = addsGroups || inertAdd ? viewGroupAddKind(activeGroupProperty, (view.config ?? {}) as Record<string, unknown>) : undefined;
  const boardAddKind = addKind === "option" && !groupEditable || addKind === "person" && valueGroups ? undefined : addKind;
  const addedKey = (value: string) => boardAddKind === "person" || boardAddKind === "option" || !boardAddKind ? value : viewGroupAddedKey(boardAddKind, value);
  function addBoardGroup(name: string) {
    if (boardAddKind !== "option") {
      void writeView(tableConfigAfterGroupKept(snapshot.view.config ?? {}, addedKey(name)));
      return;
    }
    // Added by hand, it goes last, after the group for no value, as Notion's (seen 2026-10-30): a board in no order of its own yet
    // (arrangeBoardColumns) keeps the one it shows, written with the new option so the new column shows there at once.
    const keepsShown = !(view.config as Record<string, unknown> | undefined)?.groupSort && typeof view.config?.noValueAt !== "number"
      && !Array.isArray((view.config as Record<string, unknown> | undefined)?.groupOrder);
    void runMutation(`group-options:${activeGroupProperty.id}`, (context) => operations.updateGroupOptions(activeGroupProperty.id,
      [...allGroupOptions, { id: newId(), name, color: "gray" }], context,
      keepsShown ? { ...snapshot.view, config: { ...snapshot.view.config, groupOrder: orderKeys() } } : undefined));
  }
  const { subgroupProperty, subgroup, subLanes, subValues, ownSubgroup, laneMembership, lanes, laneFolds, foldLane, laneOptions, laneAdd, laneKeyOf,
    addLane, laneCounts, laneMenuElement, openLaneMenu, laneRows } = useBoardLanes({ props, view, snapshot, t, activeGroupProperty, cardRows, columns, shownColumns,
    columnKeysOf, ownFolds, setOwnFolds, readOnly, structureLocked, addsGroups, laneMenu, setLaneMenu, writeView, runMutation, operations });
  // What every card draws from stays the same while it is, so a card drawn stays drawn (BoardColumn).
  const cardProps = useMemo(() => visible.filter((property) => property.type !== "title"), [visible]);
  const cardSize = view.config?.cardSize ?? "medium";
  const coverProperty = cardPreviewMode(view.config?.coverProperty, "board");
  const showPreview = hasCardPreview(coverProperty, "board");
  // Related pages the view has read stay beside this board's own cards, so a
  // relation shows its pages' titles as the table's does.
  const propertyValueContext = useMemo(() => ({ ...snapshot.propertyValueContext, contextIdentity: snapshot.contextIdentity, pagesById: pagesOver(snapshot.propertyValueContext.pagesById, pagesById), propsByDatabaseId: { ...snapshot.propertyValueContext.propsByDatabaseId, [snapshot.dataSource.id]: props }, snapshotIdentity: snapshot.snapshotIdentity }),
    [pagesById, props, snapshot.contextIdentity, snapshot.dataSource.id, snapshot.propertyValueContext, snapshot.snapshotIdentity]);
  const previewTransport = useCallback<PageContentPreviewTransport>(async (pageId) => ({ pageId, previewUrl: snapshot.pageContentPreviews[pageId]?.previewUrl, revision: snapshot.pageContentPreviews[pageId]?.revision ?? 0 }), [snapshot.pageContentPreviews]);

  function contextForOpen() { return { ...baseContext, requestKey: "open-row" }; }
  function openRow(rowId: string, mode: BoardRowOpenMode = "default") {
    const context = contextForOpen();
    if (!operations.isCurrent(context) || context.signal.aborted) return;
    if (mode === "default") operations.openRow(rowId, context); else operations.openRowIn(rowId, mode, context);
  }
  function closeMenu() { setMenu(null); setEditGroupMenu(false); }
  // Its name chip renames an option's group in place (GroupRename); a group of no option of its own, or a locked one, opens nothing there.
  const groupGraphProperty = snapshot.properties.find((candidate) => candidate.id === activeGroupProperty.id);
  function openGroupMenu(option: BoardOption, element: HTMLElement, naming = false) {
    if (readOnly || naming && !(groupEditable && optionNamed(groupGraphProperty?.config, option.id))) return;
    const position = popoverAnchorOf(element);
    setEditGroupMenu(false); setMenu({ option, position, returnTo: element, naming });
  }
  function fullOptionsWithPatch(option: BoardOption, patch: Partial<BoardOption>) { return allGroupOptions.map((candidate) => candidate.id === option.id ? { ...candidate, ...patch } : candidate); }
  async function writeOptions(options: readonly BoardOption[], _reason: string) { await runMutation(`group-options:${activeGroupProperty.id}`, (context) => operations.updateGroupOptions(activeGroupProperty.id, options, context)); }
  // Deleting a group removes its option; the cards keep their value, so the
  // undo writes the option list back and the cards return to the group.
  function deleteGroup(option: BoardOption) {
    // A status's default stays, as its settings tell (tellDefaultOptionKept).
    if (activeGroupProperty.type === "status" && isStatusDefaultOption(activeGroupProperty.config, option.id)) {
      void tellDefaultOptionKept();
      return;
    }
    const previous = allGroupOptions;
    void writeOptions(previous.filter((candidate) => candidate.id !== option.id), `group-delete:${option.id}`).then(() => {
      toast?.notify(t("boardView:deletedGroup", { groupName: option.name }), "success", {
        label: t("boardView:undo"),
        onClick: () => writeOptions(previous, `group-restore:${option.id}`)
          .then(() => toast.notify(t("boardView:restoredGroup"), "success"), () => toast.notify(t("boardView:restoreGroupFailed"), "error")),
      });
    });
  }
  async function addRow(groupKey: string, title: string, laneKey?: string, atTop = false, before?: string): Promise<GraphRow | null> {
    const target = groupKey === "__none" ? null : groupKey;
    const value = valueGroups ? valueGroups.valueFor(target) : boardGroupReplacementValue(activeGroupProperty, target, snapshot.groupingBounds, groupMode);
    const lane = laneKey !== undefined ? subgroupProperty : undefined, filtered = cardSeeds(laneKey);
    // A card made in a sub-group's cell takes that sub-group too.
    const laneTarget = laneKey === "__none" ? null : laneKey ?? null;
    const laneValue = subValues ? subValues.valueFor(laneTarget) : lane ? boardGroupReplacementValue(lane, laneTarget, snapshot.groupingBounds, boardSubgroupMode(subgroup)) : undefined;
    const inLane = lane && !ownSubgroup && laneValue !== undefined
      ? { ...filtered, properties: { ...filtered.properties, [lane.id]: laneValue } } : filtered;
    const first = before ?? (atTop ? laneRows(groupKey, undefined)[0]?.id : undefined);
    const titled = title ? { ...inLane, title } : inLane;
    const seeds = first ? { ...titled, beforeRowId: first } : titled;
    const made: { row: GraphRow | null } = { row: null };
    await runMaking(`row-write:${snapshot.dataSource.id}`, async (context) => {
      made.row = await operations.createRowInGroup(activeGroupProperty.id, titleGroups ? undefined : value, seeds, context);
      return made.row;
    });
    const row = made.row;
    if (row) setCardWindow((current) => {
      const shown = current.key === cardWindowKey ? current : currentCardWindow;
      return { ...shown, made: new Set([...shown.made, row.id]) };
    });
    // A view keeping its own order keeps the card where it was made: its group's end, or top.
    if (row && viewKeepsOwnRowOrder(snapshot.view.config)) {
      void writeView({ ...snapshot.view.config, rowOrder: viewRowOrderPlacing(snapshot.view.config, shownRows.map(({ id }) => id), [row.id], first ? { id: first, side: "before" } : null) }, false);
    }
    return row;
  }
  // What a card moved to a column is given (undefined: a group of no one value, a status its own database lacks — statusTakesValue; its tags or people the one swapped, boardListSwap), and
  // whether the column, or a sub-group's cell, takes it: one decision for the move and for the drop line shown over it (boardCellTakesCard).
  const valueFor = (rowId: string, from: string | null, groupKey: string, target = groupKey === "__none" ? null : groupKey) => valueGroups ? valueGroups.valueFor(target, snapshot.rows.find((row) => row.id === rowId), from === "__none" ? null : from)
    : boardListSwap(activeGroupProperty.type, snapshot.rows.find((row) => row.id === rowId)?.values[activeGroupProperty.id]?.value, from === "__none" ? null : from, boardGroupReplacementValue(activeGroupProperty, target, snapshot.groupingBounds, groupMode, rowId));
  const takesCard = (rowId: string, from: string | null, groupKey: string, laneKey?: string) => boardCellTakesCard({ sameColumn: from === groupKey,
    value: valueFor(rowId, from, groupKey), cellRows: laneRows(groupKey, laneKey), config: snapshot.view.config });
  async function moveCard(rowId: string, sourceGroupKey: string | null, groupKey: string, index: number, subgroupKey?: string, carried: readonly string[] = [rowId]) {
    const column = columns.find((candidate) => candidate.key === groupKey);
    if (!column || !takesCard(rowId, sourceGroupKey, groupKey, subgroupKey)) return;
    // Each card carried (a chosen card drags the other chosen ones, BoardColumn) takes what it is given from the column it stands in, the dragged one's where it was dragged from.
    const fromOf = (id: string) => id === rowId ? sourceGroupKey : columns.find((candidate) => candidate.rows.some((row) => row.id === id))?.key ?? null;
    const moving = carried.flatMap((id) => {
      const row = snapshot.rows.find((candidate) => candidate.id === id), from = fromOf(id);
      const value = boardMoveGroupValue({ currentValue: row?.values[activeGroupProperty.id]?.value, replacementValue: valueFor(id, from, groupKey), sourceGroupId: from, targetGroupId: groupKey });
      // A sub-group of a value its property no longer has can't be given.
      const subgroupValue = subgroupProperty && !ownSubgroup && subgroupKey !== undefined && !subgroupKey.startsWith("unknown:")
        && !laneMembership(row ?? { values: {} } as GraphRow).includes(subgroupKey)
        ? subValues ? subValues.valueFor(subgroupKey === "__none" ? null : subgroupKey, row, null)
          : boardGroupReplacementValue(subgroupProperty, subgroupKey === "__none" ? null : subgroupKey, snapshot.groupingBounds, boardSubgroupMode(subgroup))
        : undefined;
      // A card whose database page is locked, or that the reader may only view, keeps its values: it moves only within its group, and not across sub-groups.
      if (!rowValuesLocked(row) || (from === groupKey && subgroupValue === undefined)) return [{ id, value, subgroupValue }];
      announce(t(row?.lockedForMe ? "boardView:lockedCardStays" : "boardView:readOnlyCardStays", { title: pagesById[id] ? pageNames.title(pagesById[id]) : "" }));
      return [];
    });
    if (!moving.length) return;
    const ids = moving.map(({ id }) => id), neighbours = boardBlockNeighbours(laneRows(groupKey, subgroupKey), ids, index);
    // A view keeping its own order places the cards in it alone (viewRowOrder.ts), saved with the move as one ⌘Z step.
    const ownOrder = viewKeepsOwnRowOrder(snapshot.view.config);
    const place = neighbours.afterId ? { id: neighbours.afterId, side: "before" as const }
      : neighbours.beforeId ? { id: neighbours.beforeId, side: "after" as const } : null;
    const view = ownOrder ? { ...snapshot.view, config: { ...snapshot.view.config, rowOrder: viewRowOrderPlacing(snapshot.view.config, shownRows.map(({ id }) => id), ids, place) } } : undefined;
    const changes = moving.map(({ id, value, subgroupValue }, at) => ({ rowId: id, change: { propertyId: activeGroupProperty.id, value, beforeId: at ? ids[at - 1]! : neighbours.beforeId,
      afterId: neighbours.afterId, subgroupPropertyId: subgroupValue !== undefined ? subgroupProperty?.id : undefined, subgroupValue, keepPosition: ownOrder, view } }));
    // Saved in the order made: one still saving drops none after it (runMutation).
    await runMutation(`row-move:${rowId}`, (context) => changes.length > 1 && operations.moveCards ? operations.moveCards(changes, context) : operations.moveRow(changes[0]!.rowId, changes[0]!.change, context));
    announce(t("boardView:cardMovedToGroup", { title: pagesById[rowId] ? pageNames.title(pagesById[rowId]) : "", groupName: column.label }));
  }
  // A card moves in its press's own frame, dragged or by keys. One moved while the move before it is still to be
  // drawn starts from the board that move draws, a task on, not from its save: none is dropped (2026-11-06: a second
  // card dragged at once stayed put; a second view write from the view before the first's was refused, 409), nor
  // waits for the server (2026-11-12: on a 100 ms network a second ⌥→ came a round trip late, or not at all).
  function queueMove(rowId: string, sourceGroupKey: string | null, groupKey: string, index: number, subgroupKey?: string, carried?: readonly string[]) {
    const before = moveQueue.current, run = () => moveCardRef.current(rowId, sourceGroupKey, groupKey, index, subgroupKey, carried);
    const moved = before ? before.then(run) : run();
    const drawn = (before ?? Promise.resolve()).then(() => new Promise<void>((resolve) => { window.setTimeout(resolve, 0); }));
    moveQueue.current = drawn;
    void drawn.then(() => { if (moveQueue.current === drawn) moveQueue.current = null; });
    return moved;
  }
  async function dropCard(groupKey: string, index: number, subgroupKey?: string) {
    const { card: rowId, from: sourceGroup = null, carried } = dragged.current;
    if (!rowId) return;
    dragged.current = {}; setDraggingId(null);
    // The cards dropped are chosen, and stay chosen through ⌘Z and ⌘⇧Z (Notion's, seen 2026-11-12; several 2026-11-24).
    selection.chooseMany(carried?.length ? carried : [rowId]);
    await queueMove(rowId, sourceGroup, groupKey, index, subgroupKey, carried);
  }
  // An option board's columns move in the view's own order, as Notion's
  // (seen 2026-10-30), not its property's options; a status's reorder them.
  const orderKeys = () => arrangeBoardColumns(columns, { ...view.config, hideEmptyGroups: false }, false, activeGroupProperty.type).map((column) => column.key);
  async function moveInView(sourceId: string, targetId: string, side: "before" | "after") {
    const next = tableConfigAfterGroupMoved({ ...view.config, groupSort: activeGroupProperty.type === "status" ? "manual" : undefined }, orderKeys(), sourceId, targetId, side);
    if (next) await writeView(next);
  }
  async function dropGroup(targetId: string) {
    const groupId = dragged.current.group;
    dragged.current = {};
    if (!groupId || valueGroups || targetId.startsWith("unknown:")) return;
    if (!statusColumns) { await moveInView(groupId, targetId, "before"); return; }
    if (targetId === "__none") return;
    await writeOptions(reorderBoardOptionsById(allGroupOptions, groupId, targetId), `group-order:${activeGroupProperty.id}`);
  }
  async function moveGroupByKeyboard(sourceId: string, direction: -1 | 1) {
    if (valueGroups) return;
    if (!statusColumns) {
      const keys = shownColumns.map((column) => column.key), at = keys.indexOf(sourceId), target = keys[at + direction];
      if (at < 0 || !target) return;
      await moveInView(sourceId, target, direction < 0 ? "before" : "after");
      announce(t("boardView:groupMoved", { groupName: shownColumns[at]?.label, position: at + direction + 1 }));
      return;
    }
    const sourceIndex = allGroupOptions.findIndex((option) => option.id === sourceId);
    const targetIndex = sourceIndex + direction;
    if (sourceIndex < 0 || targetIndex < 0 || targetIndex >= allGroupOptions.length) return;
    const next = allGroupOptions.map((option) => ({ ...option }));
    [next[sourceIndex], next[targetIndex]] = [next[targetIndex], next[sourceIndex]];
    await writeOptions(next, `group-order:${activeGroupProperty.id}`);
    announce(t("boardView:groupMoved", { groupName: next[targetIndex]?.name, position: targetIndex + 1 }));
  }
  async function moveCardByKeyboard(rowId: string, groupKey: string, index: number, direction: "up" | "down" | "left" | "right", laneKey?: string) {
    const columnIndex = shownColumns.findIndex((candidate) => candidate.key === groupKey);
    if (columnIndex < 0) return;
    if (direction === "up") {
      if (index === 0) return;
      await queueMove(rowId, groupKey, groupKey, index - 1, laneKey);
      return;
    }
    if (direction === "down") {
      if (index >= laneRows(groupKey, laneKey).length - 1) return;
      await queueMove(rowId, groupKey, groupKey, index + 2, laneKey);
      return;
    }
    const target = shownColumns[columnIndex + (direction === "left" ? -1 : 1)];
    if (!target) return;
    await queueMove(rowId, groupKey, target.key, laneRows(target.key, laneKey).length, laneKey);
  }

  // Its "⋯" checks the view's value, or 50 until one is chosen, as Notion's
  // does while its board shows 25.
  const nearEnd = byButton ? undefined : () => setCardWindow({ ...currentCardWindow, count: currentCardWindow.count + moreCards });
  const loadMore = !byButton ? undefined : {
    limit: tableInitialLoadLimit(snapshot.view),
    onLoadMore: () => setCardWindow({ ...currentCardWindow, count: currentCardWindow.count + moreCards }),
    onLimit: readOnly || structureLocked ? undefined : (limit: number) => void writeView({ ...snapshot.view.config, initialLoadLimit: limit }),
  };
  // A card made and left with Escape is chosen; Delete moves the chosen ones to the trash (Notion's, seen 2026-11-03).
  const selection = useCardSelection({ keep: snapshot.view.id, onMenu: operations.openRowContextMenuFromElement, onOpen: openRow, onTrash: readOnly || !operations.trashRows ? undefined
    : (ids) => void runMutation("cards-trash", (context) => operations.trashRows!(ids, context)),
    onDuplicate: readOnly || !operations.duplicateRows ? undefined : (ids) => {
      // A board's columns are its groups (viewRowOrder.ts copiesPlace): a chosen card or its menu copies it from its column's line (cardLine), a row chosen whole from each column it shows in.
      const lines = ids.some((id) => lineRowId(id) !== id), { rowIds, place } = copiesPlace(shownColumns.map((column) => column.rows.map(({ id }) => lines ? groupLine(column.key, id) : id)), ids, shownRows.map(({ id }) => id));
      let copies: readonly string[] | void;
      return runMutation("cards-duplicate", async (context) => { copies = await operations.duplicateRows!(rowIds, context, place); }).then(() => copies);
    } });
  // Cards being made in place, named in them (useBoardCardDrafts).
  const { draftGroup, draftRowId, draftKey, startDraft, draftFor } = useBoardCardDrafts({ addRow, laneRows, titlePropertyId: titleProperty?.id,
    draftTitle: (groupKey) => titleGroups ? String(valueGroups?.valueFor(groupKey === "__none" ? null : groupKey) ?? "") : undefined,
    writeTitle: (rowId, title) => { if (titleProperty) void runMutation(`row-title:${rowId}`, (context) => operations.setRowProperty(rowId, titleProperty.id, title, context)); },
    templateIcon: newRowTemplate?.icon, choose: (rowId) => selection.choose(rowId), openRow: (rowId) => openRow(rowId),
    openRowMenu: (rowId, element) => operations.openRowContextMenuFromElement(rowId, element) });
  // A sub-grouped board's head "+" and a sub-group's make the row at the
  // group's top and open it, its title ready for typing; it stays though left
  // empty (Notion's, seen 2026-10-25 and 2026-11-03) — a plain board's head
  // "+" makes its card in place there instead (startDraft, 2026-11-06), but
  // on a touch screen opens its page as well, and so does a column's "+ 새
  // 페이지" at the column's end (newRowOpensItsPage; seen 2026-11-12 at 402pt
  // and 2026-11-14 at 834), one whose card the view would leave out, and the
  // "+ 새 페이지" of a board showing nothing (newViewRow.ts). A sub-group's
  // "+" gives only its value, the column's being the property's default.
  const cardSeeds = (laneKey?: string) => viewFilterSeedValues(props, view, laneKey !== undefined && subgroupProperty ? [activeGroupProperty.id, subgroupProperty.id] : [activeGroupProperty.id],
    { currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId }, snapshot.queryBounds);
  const opensCard = (laneKey?: string) => newRowOpensItsPage() || !newRowSeedsShow(cardSeeds(laneKey), snapshot);
  function addOpenRow(groupKey: string, laneKey?: string, atTop = true) {
    void makeRowThatOpens(() => addRow(groupKey, "", laneKey, atTop), (rowId) => operations.openRow(rowId, contextForOpen()));
  }
  const fullLine = useMemo(() => compactFullLineIds(view.config), [view.config]);
  const columnElement = (column: typeof shownColumns[number], part?: "head" | "cell", laneKey?: string) => (
    <CardFullLineContext.Provider key={laneKey === undefined ? column.key : `${column.key}\n${laneKey}`} value={fullLine}>
    <BoardColumn column={draftRowId ? { ...column, rows: column.rows.filter((row) => row.id !== draftRowId) } : column} part={part} laneKey={laneKey} viewConfig={snapshot.view.config}
        cellEnvironment={readOnly ? undefined : snapshot.propertyCellEnvironment}
        groupPropertyId={valueGroups ? undefined : activeGroupProperty.id} personGroups={activeGroupProperty.type === "person"}
        titleIdentity={snapshot.contextIdentity} onCommitTitle={(rowId, title) => titleProperty ? runMutation(`row-title:${rowId}`, context => operations.setRowProperty(rowId, titleProperty.id, title, context)) : Promise.resolve(false)}
        countHidden={countHidden(column.key)} cardLimit={currentCardWindow.count} madeRowIds={currentCardWindow.made}
        loadMore={loadMore} onNearEnd={nearEnd} nearEndDistance={nearEndDistance} cardColor={cardColor} cardProps={cardProps} cardSize={cardSize} context={propertyValueContext} dragScope={dragScope} draggingId={draggingId} fitImage={!!view.config?.fitImage} groupEditable={groupEditable} pagesById={pagesById} presentation={presentation} previewTransport={previewTransport} props={props} readOnly={readOnly} rowBusy={pendingMutationKeys.has(`row-write:${snapshot.dataSource.id}`)} showPreview={showPreview} useWorkspaceFile={snapshot.useWorkspaceFile} wrap={!!view.config?.wrap} pageIcons={viewShowsPageIcons(view.config ?? {})} coverProperty={coverProperty} onAddRow={(groupKey, laneKey) => opensCard(laneKey) ? addOpenRow(groupKey, laneKey, false) : startDraft(groupKey, laneKey)} onAddOpenRow={(groupKey) => lanes || opensCard() ? addOpenRow(groupKey) : startDraft(groupKey, undefined, true)}
        drafting={draftGroup !== null && (draftGroup === column.key || draftGroup.startsWith(`${column.key}\n`))} canTakeCard={(key, lane) => { const { card, from = null } = dragged.current; return !card || takesCard(card, from, key, lane); }}
        draft={draftGroup === draftKey(column.key, laneKey) ? draftFor(column.key, laneKey) : undefined} onCardDragEnd={() => { setDraggingId(null); dragged.current = {}; }} onCardDragStart={(rowId, key, carried) => { dragged.current = { card: rowId, from: key, carried }; setDraggingId(rowId); }} onDropAt={(key, index, subgroupKey) => void dropCard(key, index, subgroupKey)} onDropGroup={(targetId) => void dropGroup(targetId)} onGroupDragStart={(groupId) => { dragged.current = { group: groupId }; }} onGroupKeyDown={(groupId, direction) => void moveGroupByKeyboard(groupId, direction)} onGroupMenu={openGroupMenu} onMoveCardByKeyboard={(rowId, groupKey, index, direction, lane) => void moveCardByKeyboard(rowId, groupKey, index, direction, lane)} onOpenRow={openRow} onOpenRowContextMenu={(rowId, event) => operations.openRowContextMenu(rowId, event, selection.copyHere(event.currentTarget, rowId))} onOpenRowContextMenuFromElement={(rowId, element) => { selection.choose(cardLine(element) ?? rowId); operations.openRowContextMenuFromElement(rowId, element, selection.copyHere(element, rowId)); }} />
    </CardFullLineContext.Provider>
  );
  // No column to show — every group or sub-group hidden, or the empty ones
  // hidden while a search or filter leaves none — and the board says so in
  // their place; with its empty groups shown, Notion keeps them (seen 2026-10-11).
  const { searching, queried } = viewQuery(view, props, snapshot.queryBounds);
  const nothingShown = (shownColumns.length === 0 && (columns.length > 0 || snapshot.rows.length > 0)) || lanes?.length === 0;
  const board = nothingShown ? (
    <div className={styles.board} data-board-scroll>
      <DatabaseNoResults searching={searching} onEditFilters={operations.openFilterSettings} onEditGroups={queried || lanes?.length === 0 ? undefined : operations.openGroupSettings} onClearSearch={operations.clearSearch}
        onAddRow={readOnly ? undefined : () => addOpenRow("__none", undefined, false)} />
      {operations.rowContextMenu}
    </div>
  ) : (
    // A person's columns have no wash, as Notion's (seen 2026-11-12).
    <div ref={boardRef} className={styles.board} data-board-scroll data-cards-carry={operations.moveCards ? "" : undefined} data-column-colors={valueGroups || activeGroupProperty.type === "person" ? undefined : String(view.config?.boardColumnColors !== false)} data-size={cardSize} data-card-layout={view.config?.cardLayout === "compact" ? "compact" : undefined}>
      {lanes && subLanes ? <BoardLanes columns={windowColumns} lanes={lanes} laneKeysOf={subLanes.keysOf} folded={laneFolds} onFold={foldLane} counted={laneCounts}
        onLaneMenu={readOnly ? undefined : openLaneMenu}
        onLaneAdd={readOnly ? undefined : (laneKey) => addOpenRow(ownSubgroup ? laneKey : "__none", laneKey)}
        addLane={laneAdd && ((className) => <ViewGroupAdd kind={laneAdd} property={subgroupProperty!} environment={snapshot.propertyCellEnvironment} className={className} marker="board-add-lane"
          taken={laneOptions?.map((option) => option.name) ?? subLanes.lanes.map((lane) => lane.label)} has={(value) => subLanes.lanes.some((lane) => lane.key === laneKeyOf(value))} onAdd={addLane} />)} renderColumn={columnElement} />
        : windowColumns.map((column) => columnElement(column))}
      <BoardSideGroups pinned={pinnedColumns} onMoreGroups={shownColumns.length > groupCount
        ? () => setGroupWindow({ key: groupWindowKey, count: groupCount + BOARD_GROUP_STEP }) : undefined} preview={{ properties: props, context: propertyValueContext, pagesById, databaseTitle: snapshot.dataSource.title,
        databaseSourceId: snapshot.dataSource.id, databaseIcon: openIcon,
        databaseHref: openHref, onOpenRow: (rowId) => openRow(rowId, "center"), groupPropertyId: activeGroupProperty.id,
        filters: boardGroupPreviewFilters(snapshot, activeGroupProperty, Boolean(valueGroups), rows, pagesById) }} addGroup={boardAddKind && <ViewGroupAdd kind={boardAddKind} property={activeGroupProperty} environment={snapshot.propertyCellEnvironment}
          className={styles.boardAddGroup} inert={inertAdd}
          taken={allGroupOptions.map((option) => option.name)} has={(value: string) => allGroupOptions.some((option) => option.id === addedKey(value))} onAdd={addBoardGroup} />}
        onUnpin={readOnly || structureLocked ? undefined : (key) => void writeView(boardConfigAfterGroupPinned(snapshot.view.config ?? {}, key, false))} />
      {operations.rowContextMenu}
      {laneMenuElement}
      {menu?.naming && groupGraphProperty && <GroupRename property={groupGraphProperty} optionId={menu.option.id} name={menu.option.name} returnTo={menu.returnTo}
        onRename={(options) => void writeOptions(options, `group-option:${menu.option.id}`)} onClose={closeMenu} />}
      {menu && !menu.naming && !editGroupMenu && <GroupOptionsMenu name={menu.option.name} position={menu.position} returnTo={menu.returnTo}
        countShown={!countHidden(menu.option.id)} structureLocked={structureLocked}
        color={chosenOptionColor(menu.option)}
        onColor={groupEditable && activeGroupProperty.type !== "status" && allGroupOptions.some((option) => option.id === menu.option.id)
          ? (color) => { void writeOptions(fullOptionsWithPatch(menu.option, { color, colorFromGroup: undefined }), `group-option:${menu.option.id}`); closeMenu(); } : undefined}
        onClose={closeMenu} onEdit={() => { if (operations.openGroupSettings) { const trigger = menu.returnTo; closeMenu(); operations.openGroupSettings(trigger); }
          else if (groupEditable) setEditGroupMenu(true); }} onToggleCount={() => { toggleGroupCount(menu.option.id); closeMenu(); }}
        onHide={() => { if (!structureLocked) setGroupHidden(menu.option.id, true); closeMenu(); }}
        onTrash={operations.trashRows ? () => { const ids = columns.find(column => column.key === menu.option.id)?.rows.map(row => row.id) ?? []; closeMenu();
          if (ids.length) void runMutation(`group-trash:${menu.option.id}`, context => operations.trashRows!(ids, context)); } : undefined} />}
      {menu && editGroupMenu && groupEditable && <BoardGroupMenu returnTo={menu.returnTo} option={menu.option} position={menu.position} onClose={closeMenu} onChange={(patch) => { const next = fullOptionsWithPatch(menu.option, patch); setMenu({ ...menu, option: { ...menu.option, ...patch } }); void writeOptions(next, `group-option:${menu.option.id}`); }} onDelete={() => { deleteGroup(menu.option); closeMenu(); }} onHide={readOnly || structureLocked ? undefined : () => { setGroupHidden(menu.option.id, true); closeMenu(); }} />}
    </div>
  );

  return <><CardSelectionProvider value={selection.value}>{board}<OwnPageViewBar scrollRef={boardRef} /></CardSelectionProvider><BoardGroupsPublisher surface={boardGroupsSurface({ viewId: view.id, columns, hidden: hiddenGroupKeys, valueGroups: Boolean(valueGroups),
    propertyType: activeGroupProperty.type, options: allGroupOptions, config: view.config ?? {}, readOnly: readOnly || structureLocked, setHidden: setGroupHidden,
    writeOptions: (next) => void writeOptions(next, `group-order:${activeGroupProperty.id}`),
    replaceConfig: (config) => void writeView(config) })} /></>;
}
