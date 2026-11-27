"use client";

import type {
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
} from "react";
import { Fragment, memo, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import { drawArrivalsWhole } from "@/lib/arrivingWhole";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import { attachClosestEdge, extractClosestEdge } from "@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge";
import { useTranslation } from "react-i18next";
import type { GraphRow } from "../../contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { CardSubitemMeta, CardSubitemParent, rowCardProperties } from "./CardSubitemMeta";
import {
  CardPreviewMedia,
  type PageContentPreviewTransport,
  type WorkspaceFileHook,
} from "./CardPreviewMedia";
import { CardValueTip } from "./CardValueTip";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { chipStyle } from "./colors";
import { GroupPageGlyph } from "./GroupPageGlyph";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PersonBadge } from "./PersonBadge";
import { statusGroupTitle } from "./StatusGroupLabel";
import { conditionalColorBackground, type ConditionalColorMatch } from "./conditionalColorModel";
import { DotsHorizontal, LayoutIcon, Plus } from "../icons";
import { FileBlank } from "@/icons/leafen";
import { CardPropertySlots, fromWithinCard } from "./CardPropertySlots";
import { CardValuePress } from "./CardValuePress";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { BoardBucket, BoardOption } from "./boardViewModel";
import { columnCards } from "./boardColumns"; import { boardCardSide, boardCardsCarried, cardCarried, carriedCardCopies, carriedCardsData } from "./boardCardOwnPlace"; import { useBoardColumnDrop } from "./boardColumnDrop"; import { carryChosen } from "@/lib/carriedChoice";
import styles from "./databaseSheet";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { CardRowTitle } from "./CardRowTitle";
import titleStyles from "./CardRowTitle.module.css";
import { CardTitleIcon } from "./CardTitleIcon";
import { useNearScrollEnd } from "@/lib/useNearScrollEnd";
import { CardsLoadMore, type CardsLoadMoreControl } from "./CardsLoadMore";
import { BOARD_NEAR_END_DISTANCE, type CardViewSubitemPresentation } from "./queryProjection";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";
import { PeekHalo } from "./PeekHalo";
import { CardChoiceHalo } from "./cardSelection";
import { RowCommentCount } from "./RowCommentCount";
import { useCardFullLine } from "./cardFullLine";
import { useRowDisplayTitle, useUntitledRowName } from "./untitledRowName";
import { TemplateIconGlyph } from "./databaseViewShared";
import { arrowAlongRow } from "@/lib/readingDirection";
import { useNewPageExtra } from "./tableNewRowExtra";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

type SubitemPresentation = Pick<CardViewSubitemPresentation, "childCountByParent" | "childrenPropertyId" | "mode" | "scope">;

/** A card being made in place, its title starting as given. */
export type BoardDraft = {
  /** Each card made in turn is a fresh one. */
  serial: number;
  /** The card it sits before: the one first in its group as the head's "+" was pressed; none, at the end. */
  before?: string;
  /** The row it stands for, once made. */
  rowId?: string;
  /** Makes its row now, for a value set before its name (a card Enter started is made only once named). */
  ensureRow?(): Promise<string | null>;
  title?: string;
  /** The title its row was made with, its template's, until something is typed. */
  madeTitle?: string;
  /** The icon of the template its row starts from (useNewRowTemplate); none, the empty page's. */
  templateIcon?: string;
  /** The first's row is made already; Enter names it and starts the next (next), leaving names it — empty, the first stays, a next goes. */
  /** `escaped`: left with Escape, which chooses the card made (cardSelection.tsx). */
  onDone(title: string, next: boolean, escaped?: boolean): void;
  onOpen(title: string): void;
  onMenu(title: string, element: HTMLElement): void;
};

export type BoardColumnProps = {
  /** Edits a value in place: a card being made's property slots. */
  cellEnvironment?: PropertyCellEnvironment;
  /** The property the board groups by, whose value a card made in this column takes. */
  groupPropertyId?: string;
  /** The conditional color a card's page takes, if any. */
  cardColor?: (page: Page) => ConditionalColorMatch | null;
  cardProps: DbProperty[];
  cardSize: string;
  column: BoardBucket<GraphRow>;
  /** How many of its cards the column shows, when it shows only its first. */
  cardLimit?: number;
  /** Cards made here, which show whatever the limit. */
  madeRowIds?: ReadonlySet<string>;
  /** Its first cards and how to show more in every column. */
  loadMore?: CardsLoadMoreControl;
  /** Shows more in every column as this one's end comes `nearEndDistance` px near the window's foot, in place of "더 불러오기" (a database page's board). */
  onNearEnd?: () => void; nearEndDistance?: number;
  context: PropertyValueReadContext;
  /** The board's cards and columns drag within it only. */
  dragScope: string;
  draggingId: string | null;
  fitImage: boolean;
  groupEditable: boolean;
  countHidden?: boolean;
  /** Its groups are people: each named after their avatar, with no chip, as Notion's (seen 2026-11-12). */
  personGroups?: boolean;
  pagesById: Record<string, Page>;
  presentation: SubitemPresentation;
  previewTransport: PageContentPreviewTransport;
  props: DbProperty[];
  readOnly: boolean;
  rowBusy: boolean;
  showPreview: boolean;
  /** On a board split by a sub-group, the column's head on the top row, or one
   * sub-group's cell of it ("laneKey") under that sub-group's name. */
  part?: "head" | "cell";
  laneKey?: string;
  useWorkspaceFile: WorkspaceFileHook;
  wrap: boolean;
  /** "페이지 아이콘 표시": a page's own icon before its card's title. */
  pageIcons?: boolean;
  coverProperty?: string;
  titleIdentity: string;
  onCommitTitle(rowId: string, title: string): Promise<unknown>;
  onAddRow: (groupKey: string, laneKey?: string) => void;
  /** The head's "+": a row made in the group and opened. */
  onAddOpenRow: (groupKey: string) => void;
  /** A card being made in this column, in any of its cells, which its count takes in. */
  drafting?: boolean;
  draft?: BoardDraft;
  onCardDragEnd: () => void;
  onCardDragStart: (rowId: string, groupKey: string, carried?: readonly string[]) => void;
  /** Whether the card dragged now lands in this column (or a sub-group's cell): one that won't take it shows no drop line (boardCellTakesCard). */
  canTakeCard?: (groupKey: string, laneKey?: string) => boolean;
  onDropAt: (groupKey: string, index: number, subgroupKey?: string) => void;
  onDropGroup: (targetGroupId: string) => void;
  onGroupDragStart: (groupId: string) => void;
  onGroupKeyDown: (groupId: string, direction: -1 | 1) => void;
  onGroupMenu: (option: BoardOption, element: HTMLElement) => void;
  onMoveCardByKeyboard: (rowId: string, groupKey: string, index: number, direction: "up" | "down" | "left" | "right", laneKey?: string) => void;
  onOpenRow: (rowId: string, mode?: "default" | "new_tab" | "side") => void;
  onOpenRowContextMenu: (rowId: string, event: ReactMouseEvent<HTMLElement>) => void;
  onOpenRowContextMenuFromElement: (rowId: string, element: HTMLElement) => void;
  /** The board's settings as shown, for what sits under "+ 새 페이지" (useNewPageExtra). */
  viewConfig?: Readonly<Record<string, unknown>>;
};

/** What a card draws from: it draws again only when one of these changes. */
type BoardCardProps = Pick<BoardColumnProps, "titleIdentity" | "cellEnvironment" | "cardColor" | "cardProps" | "cardSize" | "context" | "dragScope"
  | "draggingId" | "fitImage" | "pagesById" | "presentation" | "previewTransport" | "props" | "readOnly" | "rowBusy" | "showPreview"
  | "useWorkspaceFile" | "wrap" | "pageIcons" | "coverProperty" | "onCommitTitle" | "onCardDragEnd" | "onCardDragStart" | "onDropAt"
  | "onMoveCardByKeyboard" | "onOpenRow" | "onOpenRowContextMenu" | "onOpenRowContextMenuFromElement"> & {
  index: number;
  row: GraphRow;
  canTake?: () => boolean;
  /** The side a card dragged over the column away from its cards lands on this one (boardColumnDrop.ts). */
  landing?: "before" | "after" | null;
};

/** A column's cards stay drawn as more come, so showing 20 more draws those 20. */
const ShownBoardCard = memo(BoardCard);

function BoardCard({
  titleIdentity, onCommitTitle, cellEnvironment, cardColor, cardProps, cardSize, context, dragScope, draggingId, fitImage, index, pagesById,
  presentation, previewTransport, props, readOnly: surfaceReadOnly, rowBusy, row, showPreview, useWorkspaceFile, wrap, pageIcons = true,
  coverProperty, onCardDragEnd, onCardDragStart, onDropAt, canTake, onMoveCardByKeyboard, onOpenRow, onOpenRowContextMenu, onOpenRowContextMenuFromElement, landing,
}: BoardCardProps) {
  const { t } = useTranslation("boardView");
  const rowTitle = useRowDisplayTitle();
  const fullLine = useCardFullLine();
  const [drop, setDrop] = useState<"before" | "after" | null>(null);
  // While a card saves its name stays put; cards still move, one after another (BoardView's queueMove).
  const readOnly = surfaceReadOnly || rowBusy;
  const cardRef = useRef<HTMLElement>(null);
  // Its name being typed, it shows its property slots; one's editor open, the name stays typed.
  const [titleEditing, setTitleEditing] = useState(false);
  const slotHold = useRef(false);
  const hasPage = Boolean(pagesById[row.id]);
  const latest = useRef({ index, onCardDragEnd, onCardDragStart, onDropAt, canTake });
  latest.current = { index, onCardDragEnd, onCardDragStart, onDropAt, canTake };
  // A card lands above or below the card it is dropped on, in a column that takes it (canTake) — the card right after it takes it after itself (boardCardSide); on itself it stays. A mouse drags it by the pointer's events (pointerDrag), a chosen card the other chosen ones along (boardCardsCarried), which wear no chosen look while they go (carriedChoice.ts).
  useEffect(() => {
    const element = cardRef.current;
    if (!element) return;
    const kind = `board-card:${dragScope}`;
    const sideOf = (dragged: Record<string | symbol, unknown>, data: Record<string | symbol, unknown>) => boardCardSide(dragged, element, extractClosestEdge(data) === "bottom");
    const show = (dragged: Record<string | symbol, unknown>, data: Record<string | symbol, unknown>) => {
      const side = cardCarried(dragged, row.id) ? null : sideOf(dragged, data);
      setDrop((current) => current === side ? current : side);
    };
    const target = dropTargetByPointer({
      element,
      canDrop: ({ source }) => source.data.kind === kind && latest.current.canTake?.() !== false,
      getData: ({ input, element: self }) => attachClosestEdge({}, { input, element: self, allowedEdges: ["top", "bottom"] }),
      onDragEnter: ({ source, self }) => show(source.data, self.data),
      onDrag: ({ source, self }) => show(source.data, self.data),
      onDragLeave: () => setDrop(null),
      onDrop: ({ source, self }) => {
        setDrop(null);
        if (!cardCarried(source.data, row.id)) latest.current.onDropAt("", latest.current.index + (sideOf(source.data, self.data) === "after" ? 1 : 0));
      },
    });
    if (surfaceReadOnly) return target;
    return combine(target, draggableByPointer({
      element,
      canDrag: () => !element.querySelector("[data-row-title-editor]"),
      getInitialData: () => ({ kind, id: row.id, ...carriedCardsData(boardCardsCarried(element)) }),
      onDragStart: ({ source }) => { carryChosen(source.data.carried as readonly string[]); latest.current.onCardDragStart(row.id, "", source.data.carried as readonly string[]); },
      onDrop: () => { carryChosen([]); latest.current.onCardDragEnd(); },
    }, { carry: () => carriedCardCopies(element) }));
  }, [dragScope, hasPage, surfaceReadOnly, row.id]);
  const page = pagesById[row.id];
  if (!page) return null;
  const title = rowTitle(page);
  const tint = cardColor?.(page);
  // A rollup's page on the card opens as the board opens its cards (RollupPageLinks).
  const openPage = (pageId: string) => onOpenRow(pageId, "default");

  function openKey(e: ReactKeyboardEvent<HTMLElement>) {
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      e.preventDefault();
      onOpenRowContextMenuFromElement(row.id, e.currentTarget);
      return;
    }
    if (!surfaceReadOnly && e.altKey && !e.metaKey && !e.ctrlKey && ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(e.key)) {
      e.preventDefault();
      // Left and right as the board's columns run: the other way round read from the right.
      onMoveCardByKeyboard(row.id, "", index, arrowAlongRow(e.key, e.currentTarget).slice(5).toLowerCase() as "up" | "down" | "left" | "right");
      return;
    }
    if (!fieldEnter(e) && e.key !== " ") return;
    e.preventDefault();
    onOpenRow(row.id, e.metaKey || e.ctrlKey ? "new_tab" : "default");
  }

  return (
    <div className={styles.boardCardSlot}>
      {(drop ?? landing) === "before" && <div className={styles.boardDropLine} data-side="before" aria-hidden="true" />}
      <article
        ref={cardRef}
        data-row-card
        data-row-id={row.id}
        data-card-choice={row.id}
        data-card-title={title}
        className={styles.card}
        role="button"
        tabIndex={0}
        aria-label={t("openRow", { title })}
        aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown Alt+ArrowLeft Alt+ArrowRight"
        data-size={cardSize}
        data-wrap={wrap ? "true" : undefined}
        data-conditional-color={tint?.color}
        style={tint ? { background: conditionalColorBackground(tint.color) } : undefined}
        data-dragging={draggingId === row.id ? "true" : undefined}
        onClick={(event) => { if (fromWithinCard(event)) onOpenRow(row.id, rowOpenModeOfClick(event)); }}
        onKeyDown={(event) => { if (fromWithinCard(event)) openKey(event); }}
        onAuxClick={(e) => {
          if (e.button === 1 && fromWithinCard(e)) {
            e.preventDefault();
            onOpenRow(row.id, "new_tab");
          }
        }}
        onContextMenu={(event) => { if (fromWithinCard(event)) onOpenRowContextMenu(row.id, event); }}
      >
        <PeekHalo itemId={row.id} />
        <CardChoiceHalo itemId={row.id} />
        {showPreview && (
          <CardPreviewMedia
            row={page}
            props={props}
            coverProperty={coverProperty}
            fitImage={fitImage}
            coverClassName={styles.cardCover}
            iconClassName={styles.cardIcon}
            iconSize={18}
            collapseEmptyPageContent
            collapseEmptyCover
            getPageContentPreview={previewTransport}
            useWorkspaceFile={useWorkspaceFile}
          />
        )}
        <CardSubitemParent row={page} pagesById={pagesById} presentation={presentation} />
        <CardRowTitle key={titleIdentity} row={page} className={styles.cardTitle} readOnly={readOnly}
          icon={<CardTitleIcon row={page} show={pageIcons} size={18} className={styles.cardTitleIcon} />}
          editingIcon={<span className={`${styles.cardTitleIcon} ${styles.cardEditingIcon}`} aria-hidden="true"><FileBlank pixels={18} /></span>}
          onOpenSide={() => onOpenRow(row.id, "side")}
          onEditingChange={setTitleEditing} holdRef={slotHold}
          onCommit={value => onCommitTitle(row.id, value)}
          onMenu={element => onOpenRowContextMenuFromElement(row.id, element)} />
        {titleEditing && cellEnvironment && !readOnly ? <CardPropertySlots className={styles.cardMeta} cardProps={cardProps} context={context}
          page={page} rowId={page.id} environment={cellEnvironment} holdRef={slotHold} onClosed={() => undefined} />
        : <div className={styles.cardMeta}>
          {rowCardProperties(page, cardProps, presentation).map((property) => fullLine.has(property.id) ? (
            <CardValueTip key={property.id} name={property.name}><CardValuePress className={styles.cardFullLine} data-card-full-line="true" line
              property={property} page={page} environment={readOnly ? undefined : cellEnvironment}>
              <PropValue context={context} row={page} prop={property} interactive={false} linksOpen checkboxName onOpenPage={openPage} />
            </CardValuePress></CardValueTip>
          ) : (
            <CardValueTip key={property.id} name={property.name}><CardValuePress className={styles.cardLine} line property={property} page={page}
              environment={readOnly ? undefined : cellEnvironment}><PropValue context={context} row={page} prop={property} interactive={false} linksOpen checkboxName onOpenPage={openPage} /></CardValuePress></CardValueTip>
          ))}
          <CardSubitemMeta row={page} presentation={presentation} />
        </div>}
        <RowCommentCount rowId={row.id} line />
      </article>
      {(drop ?? landing) === "after" && <div className={styles.boardDropLine} data-side="after" aria-hidden="true" />}
    </div>
  );
}

/** A card being made, as Notion's "+ 새 페이지" (seen 2026-11-03): a card at
 * once, counted in its column, with its template's icon or else the empty
 * page's (seen 2026-11-08: the task tracker's ✅ on Enter's next one too), "이름을 입력하세요"
 * and the side peek and "⋯" buttons, standing for the row made as "+" was
 * pressed. Enter names it, empty or not, and opens the next; Escape or leaving
 * names it too, and an empty one stays (seen 2026-11-04) — but a next one
 * left empty goes (seen 2026-11-06). */
type DraftSlots = { cardProps: DbProperty[]; context: PropertyValueReadContext; page?: Page; environment?: PropertyCellEnvironment;
  /** Before its row is made, the grouping property's slot shows the column's own value. */
  group?: { propertyId: string; value: ReactNode } };

function BoardDraftCard({ draft, cardSize, slots }: { draft: BoardDraft; cardSize?: string; slots: DraftSlots }) {
  const { t } = useTranslation(["boardView", "tableView"]);
  const done = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  // A slot's editor open over the card: the title's leaving does not end it.
  const editingRef = useRef(false);
  useEffect(() => { if (draft.madeTitle && input.current && !input.current.value) input.current.value = draft.madeTitle; }, [draft.madeTitle]);
  function hand(act: (title: string) => void) {
    if (done.current) return;
    done.current = true;
    act(input.current?.value.trim() ?? "");
  }
  return (
    <div className={styles.boardCardSlot}>
      <div className={styles.card} data-board-draft-card data-size={cardSize}>
        {/* Its buttons keep the title being typed until they act on it. */}
        <span className={titleStyles.cardTools} data-row-card-tools onMouseDown={(event) => event.preventDefault()}>
          <button type="button" aria-label={t("tableView:openInSidePeek")} title={t("tableView:openInSidePeek")}
            onClick={() => hand(draft.onOpen)}><LayoutIcon size="tiny" aria-hidden="true" /></button>
          <button type="button" aria-label={t("tableView:rowActions")} title={t("tableView:rowActions")}
            onClick={(event) => { const element = event.currentTarget; hand((title) => draft.onMenu(title, element)); }}>
            <DotsHorizontal size="small" aria-hidden="true" /></button>
        </span>
        <span className={`${styles.cardTitle} ${titleStyles.title}`} data-board-draft-title>
          {draft.templateIcon?.trim() ? <span className={styles.cardTitleIcon} aria-hidden="true"><TemplateIconGlyph icon={draft.templateIcon} pixels={18} /></span>
            : <span className={`${styles.cardTitleIcon} ${styles.cardEditingIcon}`} aria-hidden="true"><FileBlank pixels={18} /></span>}
          <input ref={input} className={styles.boardDraftTitle} autoFocus placeholder={t("draftTitle")} aria-label={t("draftTitle")} defaultValue={draft.title}
            onBlur={() => { if (!editingRef.current) hand((title) => draft.onDone(title, false)); }}
            onKeyDown={(event) => {
              if (imeComposing(event) || (!fieldEnter(event) && event.key !== "Escape")) return;
              event.preventDefault(); event.stopPropagation();
              const next = fieldEnter(event);
              hand((title) => draft.onDone(title, next, event.key === "Escape"));
            }} />
        </span>
        <CardPropertySlots className={styles.cardMeta} data-board-draft-slots="" cardProps={slots.cardProps} context={slots.context}
          page={slots.page} rowId={draft.rowId} environment={slots.environment} group={slots.group} holdRef={editingRef}
          ensureRow={draft.ensureRow} onClosed={() => input.current?.focus()} />
      </div>
    </div>
  );
}

export function BoardColumn(props: BoardColumnProps) {
  const {
    column,
    dragScope,
    groupEditable,
    readOnly,
    rowBusy,
    part,
    laneKey,
    draft,
    onAddRow,
    onDropAt,
    onDropGroup,
    onGroupDragStart,
    onGroupKeyDown,
    onGroupMenu,
  } = props;
  const { t } = useTranslation(["boardView", "propValue"]);
  const newPageLabel = useUntitledRowName();
  // Under each column's "+ 새 페이지", a relation tab's "↗ 기존 항목 연결" (Notion's, seen 2026-11-12).
  const newPageExtra = useNewPageExtra(props.viewConfig);
  const offersNewRows = useViewOffersNewRows();
  // A checkbox has no editable option or option order; its group actions still apply.
  const option = column.key === "__none" || column.key.startsWith("unknown:") || column.checked !== undefined
    ? null
    : { id: column.key, name: column.label, color: column.color };
  // Its first cards, and those made here; "더 불러오기" follows when more wait.
  const shownRows = columnCards(column.rows, props.cardLimit, props.madeRowIds);
  const hasMore = shownRows.length < column.rows.length;
  // A card being made sits before the card it was made before, or at the end.
  const draftBefore = !draft || readOnly ? undefined : draft.before && shownRows.some((row) => row.id === draft.before) ? draft.before : null;
  const draftSlots: DraftSlots = { cardProps: props.cardProps, context: props.context, environment: props.cellEnvironment,
    page: draft?.rowId ? props.pagesById[draft.rowId] : undefined,
    group: props.groupPropertyId && option ? { propertyId: props.groupPropertyId,
      value: props.personGroups ? <PersonBadge id={option.id} /> : statusGroupTitle(column, option.name) ?? (option.color ? <span className={styles.chip} style={chipStyle(option.color)}><span className={styles.chipText}>{option.name}</span></span> : option.name) } : undefined };
  let renderedIndex = 0;
  // A card's actions stay the same functions, reaching what the board last drew.
  const latestColumn = useRef(props);
  latestColumn.current = props;
  const [cardActions] = useState(() => ({
    onCommitTitle: (rowId: string, title: string) => latestColumn.current.onCommitTitle(rowId, title),
    onCardDragEnd: () => latestColumn.current.onCardDragEnd(),
    onCardDragStart: (rowId: string, _key: string, carried?: readonly string[]) => latestColumn.current.onCardDragStart(rowId, latestColumn.current.column.key, carried),
    onDropAt: (_groupKey: string, targetIndex: number) => latestColumn.current.onDropAt(latestColumn.current.column.key, targetIndex, latestColumn.current.laneKey),
    canTake: () => latestColumn.current.canTakeCard?.(latestColumn.current.column.key, latestColumn.current.laneKey) !== false,
    onMoveCardByKeyboard: (rowId: string, _groupKey: string, targetIndex: number, direction: "up" | "down" | "left" | "right") =>
      latestColumn.current.onMoveCardByKeyboard(rowId, latestColumn.current.column.key, targetIndex, direction, latestColumn.current.laneKey),
    onOpenRow: (rowId: string, mode?: "default" | "new_tab" | "side") => latestColumn.current.onOpenRow(rowId, mode),
    onOpenRowContextMenu: (rowId: string, event: ReactMouseEvent<HTMLElement>) => latestColumn.current.onOpenRowContextMenu(rowId, event),
    onOpenRowContextMenuFromElement: (rowId: string, element: HTMLElement) => latestColumn.current.onOpenRowContextMenuFromElement(rowId, element),
  }));
  // A database page's board draws more in each column once a column's last card is 550px from the window's foot,
  // as Notion's (seen 2026-11-07); a phone's once it reaches the foot (boardCardLoads).
  const endRef = useRef<HTMLSpanElement>(null);
  useNearScrollEnd(endRef, hasMore ? props.onNearEnd : undefined, props.nearEndDistance ?? BOARD_NEAR_END_DISTANCE, shownRows.length);
  const columnRef = useRef<HTMLElement>(null);
  // A card new to the drawn column paints in its first frame (lib/arrivingWhole.ts).
  useLayoutEffect(() => drawArrivalsWhole(columnRef.current, columnRef.current?.querySelectorAll<HTMLElement>("[data-row-card]") ?? []));
  const headRef = useRef<HTMLElement>(null);
  // A card over the column's own part, not over one of its cards, lands beside the nearer card, or last: "end" draws the line there (boardColumnDrop.ts).
  const over = useBoardColumnDrop(columnRef, column.key, dragScope, { count: column.rows.length, rowIds: shownRows.map(({ id }) => id), laneKey, onDropAt, onDropGroup,
    canTakeCard: props.canTakeCard });
  const [groupDragging, setGroupDragging] = useState(false);
  const latest = useRef({ onGroupDragStart });
  latest.current = { onGroupDragStart };
  const groupDraggable = !readOnly && groupEditable && !!option && part !== "cell";
  useEffect(() => {
    const element = headRef.current;
    if (!element || !groupDraggable) return;
    return draggableByPointer({
      element,
      getInitialData: () => ({ kind: `board-group:${dragScope}`, id: column.key }),
      onDragStart: () => { setGroupDragging(true); latest.current.onGroupDragStart(column.key); },
      onDrop: () => setGroupDragging(false),
    });
  }, [column.key, dragScope, groupDraggable]);

  function card(row: GraphRow) {
    const index = renderedIndex;
    renderedIndex += 1;
    return (
      <ShownBoardCard key={row.id} row={row} index={index} titleIdentity={props.titleIdentity} cellEnvironment={props.cellEnvironment}
        cardColor={props.cardColor} cardProps={props.cardProps} cardSize={props.cardSize} context={props.context} dragScope={props.dragScope}
        draggingId={props.draggingId} fitImage={props.fitImage} pagesById={props.pagesById} presentation={props.presentation}
        previewTransport={props.previewTransport} props={props.props} readOnly={readOnly} rowBusy={rowBusy} showPreview={props.showPreview}
        useWorkspaceFile={props.useWorkspaceFile} wrap={props.wrap} pageIcons={props.pageIcons} coverProperty={props.coverProperty} {...cardActions}
        landing={typeof over === "object" && over?.card === row.id ? over.side : null} />
    );
  }

  return (
    <section
      ref={columnRef}
      className={styles.boardCol}
      data-group-color={column.colorFromGroup ? "default" : column.color}
      data-drag-over={over && over !== "group" ? "true" : undefined}
      data-column-drag-over={over === "group" ? "true" : undefined}
      data-board-part={part} data-card-group={column.key}
      data-subgroup-id={laneKey}
    >
      {part !== "cell" && <header
        ref={headRef}
        className={styles.boardColHead}
        data-sticky-head=""
        data-group-dragging={groupDragging ? "true" : undefined}
      >
        {option ? (
          <button
            type="button"
            className={styles.boardGroupBtn}
            aria-keyshortcuts="Alt+ArrowLeft Alt+ArrowRight"
            disabled={readOnly}
            onClick={(event) => onGroupMenu(option, event.currentTarget)}
            onKeyDown={(event) => {
              if (!event.altKey || event.metaKey || event.ctrlKey) return;
              if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
              event.preventDefault();
              onGroupKeyDown(option.id, arrowAlongRow(event.key, event.currentTarget) === "ArrowLeft" ? -1 : 1);
            }}
          >
            {/* A group of what cards hold has no color: a title's text, a related page's with its glyph (Notion's, seen 2026-10-11),
                its name on one line, cut with "…" before the head's "⋯ +" (seen 2026-11-12). */}
            {props.personGroups ? <span className={styles.boardValueGroup}><PersonAvatar className={styles.personAvatar} userId={option.id} name={option.name} size={20} /><span className={styles.boardColName}>{option.name}</span></span>
              : statusGroupTitle(column, option.name) ?? (option.color ? <span className={styles.chip} style={chipStyle(option.color)}><span className={styles.chipText}>{option.name}</span></span>
              : <span className={styles.boardValueGroup}><GroupPageGlyph page={column.page} pixels={18} /><span className={styles.boardColName}>{option.name}</span></span>)}
          </button>
        ) : column.checked !== undefined ? (
          <span className={styles.boardCheckboxGroup} data-board-checkbox-group={column.checked ? "checked" : "unchecked"}>
            <span className={styles.cardCheckbox} data-checked={column.checked ? "true" : undefined}
              role="checkbox" aria-checked={column.checked} aria-disabled="true">
            </span>
            <span className={styles.boardColName}>{column.label}</span>
          </span>
        ) : (
          <span className={styles.boardNoGroup}>{column.label}</span>
        )}
        {!props.countHidden && <span className={styles.boardCount}>{column.rows.length + (props.drafting ? 1 : 0)}</span>}
        {/* At the head's end "⋯" and then "+", as Notion's board (seen 2026-10-25). */}
        {!readOnly && (
          <button type="button" className={styles.boardGroupMore} data-board-group-actions aria-label={t("groupActions", { groupName: column.label })} onClick={(event) => onGroupMenu({ id: column.key, name: column.label, color: column.color }, event.currentTarget)}><DotsHorizontal size="small" aria-hidden="true" /></button>
        )}
        {!readOnly && offersNewRows && <button type="button" className={styles.boardGroupMore} aria-label={newPageLabel} disabled={rowBusy}
          onClick={() => props.onAddOpenRow(column.key)}><Plus size="small" aria-hidden="true" /></button>}
      </header>}
      {part !== "head" && <div className={styles.boardCards}>
        {shownRows.map((row) => row.id === draftBefore
          ? <Fragment key={row.id}><BoardDraftCard key={draft!.serial} draft={draft!} cardSize={props.cardSize} slots={draftSlots} />{card(row)}</Fragment> : card(row))}
        {hasMore && props.loadMore ? <CardsLoadMore placement="column" {...props.loadMore} /> : null}
        {hasMore && props.onNearEnd ? <span ref={endRef} className={styles.boardCardsEnd} aria-hidden="true" /> : null}
        {draft && draftBefore === null && !readOnly ? <BoardDraftCard key={draft.serial} draft={draft} cardSize={props.cardSize} slots={draftSlots} /> : null}
        {over === "end" ? <div className={styles.boardDropEnd} aria-hidden="true"><div className={styles.boardDropLine} data-side="after" /></div> : null}
        {!readOnly && offersNewRows && (
          <button type="button" className={styles.boardAdd} disabled={rowBusy} onClick={() => onAddRow(column.key, laneKey)}>
            <Plus size="small" aria-hidden="true" /> {newPageLabel}
          </button>
        )}
        {!readOnly && offersNewRows && newPageExtra ? <div className={styles.boardAddLink}>{newPageExtra}</div> : null}
      </div>}
    </section>
  );
}
