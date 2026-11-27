"use client";

import { Fragment, memo, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import type { DbProperty, Page } from "@/lib/types";
import { ChevronDown, ChevronRight, DragHandleIcon, Plus } from "../icons";
import { AddHereTip, HandleTip, IconTipButton } from "../IconTipButton";
import { optionKeyName } from "@/lib/keyboardShortcuts";
import { PageIconGlyph } from "../page/PageIcon";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { OTHER_ROWS_LOOKUPS, PropertyCell, liveCellWays } from "./PropertyCell";
import { LOCAL_PROPERTY_CELL_TYPES, type PropertyCellEnvironment, type PropertyCellSnapshot } from "./propertyCellTypes";
import styles from "./databaseSheet";
import { TimelineGroupAddLane, TimelineGroupLaneHeader, TimelineNewGroupLane, type TimelineGroupActions } from "./TimelineGroupLanes";
import type { TimelineAddLane, TimelineGroupLane, TimelineLaneKind, TimelineSubitemAddLane, TimelineTableLane } from "./timelineLaneProjection";
import { PropertyIcon } from "./PropertyIcon";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { ArchivedRowTitle } from "./DatabaseArchivedRowsMode";
import { PeekHalo } from "./PeekHalo";
import { CardChoiceHalo, useChooseCard, useChooseCards } from "./cardSelection";
import type { TimelineRowReorder } from "./useTimelineRowReorder";
import { RowCommentCount } from "./RowCommentCount";
import { inlineEndKey, inlineSign } from "@/lib/readingDirection";
import { sameOwnRowValues, useLiveWays } from "./memoizedViewItems";
import { readsOtherRows } from "./otherRowsRead";
import { holdMeasure } from "./heldStyleWrites";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { TimelineTableCell, TimelineTableNameCell, type TimelineTableCellWays } from "./TimelineTableCell";
import { toolsCellKey, useCellToolsShown, type CellToolsShown } from "./cellToolsShown";
import { TableCellValueTools, shownValueText, type TableCellValueReading } from "./TableCellValueTools";
import { useKeysHideRowTools } from "./useKeysHideRowTools";
import { tablePart } from "./tableInteractionModel";
import { useTimelineTableRange, type TimelineTableCells } from "./useTimelineTableRange";

export type TimelineHierarchyRow = {
  row: Page;
  depth: number;
  hasChildren: boolean;
  childCount: number;
};

export type TimelineRenderedRow = TimelineHierarchyRow & {
  start: Date;
  end: Date;
  first: number;
  last: number;
  lane: number;
};

export type TimelineRowOpenMode = "default" | "new_tab" | "side" | "full";

type CommonProps = {
  childCountByParent: ReadonlyMap<string, number>;
  expandedRows: ReadonlySet<string>;
  flattenedSubtasks: boolean;
  hiddenSubtasks: boolean;
  openRow(rowId: string, mode?: TimelineRowOpenMode): void;
  openRowContextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  openRowContextMenuFromElement(rowId: string, element: HTMLElement): void;
  pagesById: Readonly<Record<string, Page>>;
  rowTitle(row: Page): string;
  t(key: string, values?: Record<string, unknown>): string;
  toggleExpanded(rowId: string): void;
};

export function TimelineSubitemControls({
  expandedRows,
  item,
  rowTitle,
  t,
  toggleExpanded,
}: {
  expandedRows: ReadonlySet<string>;
  item: TimelineHierarchyRow;
  rowTitle(row: Page): string;
  t(key: string, values?: Record<string, unknown>): string;
  toggleExpanded(rowId: string): void;
}) {
  const expanded = expandedRows.has(item.row.id);
  const title = rowTitle(item.row);
  return (
    <>
      {item.depth > 0 && (
        <span className={styles.timelineSubitemIndent} style={{ width: item.depth * 16 }} aria-hidden="true" />
      )}
      {item.hasChildren && (
        <button
          type="button"
          className={styles.subitemToggle}
          data-visible="true"
          style={{ alignSelf: "center" }}
          aria-expanded={expanded}
          aria-label={t(expanded ? "tableView:collapseSubitems" : "tableView:expandSubitems", { title })}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={() => toggleExpanded(item.row.id)}
        >
          {expanded
            ? <ChevronDown size="tiny" aria-hidden="true" />
            : <ChevronRight size="tiny" aria-hidden="true" />}
        </button>
      )}
    </>
  );
}

/** The title of a row just made here, ready for typing; `byKey` says Enter or Esc closed it, not a press elsewhere, `escaped` Esc. */
export function NewRowTitle({ row, label, onCommit }: { row: Page; label: string; onCommit(value: string, byKey: boolean, escaped: boolean): void }) {
  const [value, setValue] = useState(row.title), finished = useRef(false), typed = useRef(false);
  // Until typed into it shows the title as it is: opened again by ⌘Z, the title taken back arrives after it opens.
  if (!typed.current && value !== row.title) setValue(row.title);
  function finish(byKey: boolean, escaped = false) { if (!finished.current) { finished.current = true; onCommit(value, byKey, escaped); } }
  return <input autoFocus className={styles.timelineNewRowTitle} value={value} aria-label={label} placeholder={label}
    onChange={event => { typed.current = true; setValue(event.currentTarget.value); }} onBlur={() => finish(false)} onPointerDown={event => event.stopPropagation()}
    onKeyDown={event => { event.stopPropagation(); if (fieldEnter(event) || event.key === "Escape") { event.preventDefault(); finish(true, event.key === "Escape"); } }} />;
}

function openKey(
  rowId: string,
  event: ReactKeyboardEvent<HTMLElement>,
  openRowContextMenuFromElement: CommonProps["openRowContextMenuFromElement"],
) {
  const e = event;
  if (e.key !== "ContextMenu" && !(e.shiftKey && e.key === "F10")) return;
  event.preventDefault();
  event.stopPropagation();
  openRowContextMenuFromElement(rowId, event.currentTarget);
}

/** A shown column of the table panel; the title's (`title`) holds each row's name and fills what the others leave. */
export type TimelineTableColumn = { property: DbProperty; title?: boolean; width: number; wrap: boolean };

/** A table panel column's head, whose edge drags or arrows its width. */
function TimelineTableColumnHead({
  column,
  onResize,
  t,
}: {
  column: TimelineTableColumn;
  onResize?: (propertyId: string, width: number) => void;
  t: CommonProps["t"];
}) {
  const [preview, setPreview] = useState<number | null>(null);
  const dragged = useRef(column.width);
  const width = preview ?? column.width;
  function begin(event: ReactPointerEvent<HTMLSpanElement>) {
    if (!onResize) return;
    event.preventDefault();
    const startX = event.clientX, sign = inlineSign(event.currentTarget);
    dragged.current = column.width;
    const move = (next: PointerEvent) => {
      dragged.current = Math.max(60, Math.min(2000, column.width + (next.clientX - startX) * sign));
      setPreview(dragged.current);
    };
    const finish = () => {
      window.removeEventListener("pointermove", move);
      setPreview(null);
      if (dragged.current !== column.width) onResize(column.property.id, dragged.current);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", finish, { once: true });
  }
  return (
    <span className={`${styles.timelineTableCell} ${styles.timelineTableColumnHead}`} style={{ width, position: "relative" }}
      data-timeline-table-column={column.property.id}>
      {/* Its property's icon before its name, as a table's head and the title's here (Notion's, seen 2026-11-12: "# 금액"). */}
      <span className={styles.headGlyphIcon} aria-hidden="true"><PropertyIcon property={column.property} size="regular" /></span>
      <span className={styles.timelineTableColumnName}>{column.property.name}</span>
      {onResize && (
        <span className={styles.timelineTableColumnResize} role="separator" aria-orientation="vertical" tabIndex={0}
          aria-label={t("tableView:resizeColumnValue", { name: column.property.name, width: Math.round(width) })}
          data-timeline-table-resize={column.property.id}
          onPointerDown={begin}
          onKeyDown={(event) => {
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
            event.preventDefault();
            onResize(column.property.id, column.width + (event.key === inlineEndKey(event.currentTarget) ? 12 : -12) * (event.shiftKey ? 4 : 1));
          }} />
      )}
    </span>
  );
}

/** The table panel: each row's name and the table properties it shows. */
export function TimelineTableRows({
  columns,
  items,
  onResizeColumn,
  propertyCellEnvironment,
  cellsReadOnly = false,
  propertyValueContext,
  lanes,
  groupActions = {},
  toggleGroup = () => {},
  nameHeader,
  onLaneHeightsChange,
  onCreate,
  summary,
  reorder,
  openRowIn,
  onCloseRow,
  cells,
  ...props
}: CommonProps & {
  columns: readonly TimelineTableColumn[];
  items: readonly TimelineTableLane[];
  /** Every lane's kind; a grouped table's heads, columns' names, "+ 새로 만들기" and "+ 신규 그룹". */
  lanes: { kinds: readonly TimelineLaneKind[]; groups: readonly TimelineGroupLane[]; addLanes: readonly TimelineAddLane[]; newGroupLane?: number;
    subitemAdds: readonly TimelineSubitemAddLane[] };
  onCreateSubitem?(parentId: string): void;
  /** A sub-item just made with no date takes its title here. */
  newTableRowId?: string | null;
  /** A new row's title closed, `escaped` by Escape. */
  onCommitTitle?(row: Page, title: string, escaped?: boolean): void;
  /** A row's "+": a row of no date under it, over it with ⌥ (`above`); without it rows show no "+". */
  onAddBeside?(rowId: string, above: boolean): void;
  groupActions?: TimelineGroupActions;
  toggleGroup?(key: string): void;
  nameHeader?: string;
  onLaneHeightsChange?(heights: readonly number[]): void;
  /** "+ 새로 만들기" under the rows, beside the canvas's own, which the table covers. */
  onCreate?(): void;
  /** Its calculations' row under them, given its columns' grid (TableSummaryRow). */
  summary?(columns: string): ReactNode;
  /** Saves a column's new width; without it the columns keep theirs. */
  onResizeColumn?: (propertyId: string, width: number) => void;
  /** Edits the cells in place, as a table does; without it they only show. */
  propertyCellEnvironment?: PropertyCellEnvironment;
  /** The cells only show their values, drawn as when they edit (a locked page's; cellReadOnly.ts). */
  cellsReadOnly?: boolean;
  propertyValueContext: PropertyValueReadContext;
  /** Its rows move by their handles (useTimelineRowReorder). */
  reorder?: TimelineRowReorder;
  /** Where a row's "열기" opens it, and the row open in the side peek closed by its "닫기", as a table's. */
  openRowIn?: "side" | "center" | "full";
  onCloseRow?(): void;
  /** What its range does with its rows' values (useTimelineTableRange). */
  cells?: TimelineTableCells;
}) {
  const grouped = lanes.groups.length > 0;
  const moving = reorder?.moves ? reorder : undefined, choose = useChooseCard(), chooseLines = useChooseCards();
  // A grouped table's first head and its columns' names stand beside the
  // month and the days, held at the page's top with them as it scrolls, as
  // Notion's (seen 2026-11-15 at 1440: "▼ 시작 전" and "«" on the month's
  // line, "Aa 이름" on the days').
  const pinned = (lane: number) => grouped && (lane === 0 || (lane === 1 && lanes.kinds[1] === "columns"));
  const groupAt = new Map(lanes.groups.map(group => [group.lane, group]));
  const itemAt = new Map(items.map(item => [item.lane, item]));
  const addAt = new Map(lanes.addLanes.map(add => [add.lane, add]));
  const heads = () => <>
    {columns.map((column) => column.title ? (
      <span key={column.property.id} className={styles.timelineTableNameHead} data-timeline-table-column={column.property.id}>
        {/* Its icon sits as a table head's (DatabaseTableChrome.module.css), as Notion's (seen 2026-11-11). */}
        <span className={styles.headGlyphIcon} aria-hidden="true"><PropertyTypeIcon type="title" size="regular" /></span>
        {nameHeader || props.t("timelineView:table.nameHeader")}</span>
    ) : (
      <TimelineTableColumnHead key={column.property.id} column={column} t={props.t}
        onResize={propertyCellEnvironment && !cellsReadOnly ? onResizeColumn : undefined} />
    ))}
  </>;
  // The columns as last drawn while they read the same, and what a row's presses do by the table as last drawn (TimelineTableRow).
  const steadyColumns = useSteadyColumns(columns);
  // Its cells write with the table's latest ways, the same objects as rows not drawn again keep them (liveCellWays).
  const environmentNow = useRef(propertyCellEnvironment);
  environmentNow.current = propertyCellEnvironment;
  const [cellWays] = useState(() => liveCellWays(environmentNow as { readonly current: PropertyCellEnvironment }));
  // Its values' tools under the pointer, as a table's (cellToolsShown), put away by a key until the mouse moves.
  const rowsRef = useRef<HTMLDivElement>(null), cellTools = useCellToolsShown();
  useKeysHideRowTools(rowsRef, cellTools);
  // Each row's line in the table, as a table's rows have theirs (tableCellLine.ts): the keys go from cell to cell by it. A grouped
  // timeline's group is its own table to a chosen cell's keys and its range, as a table's (tablePart): its first line, the rows on lanes before its head.
  const ordered = [...items].sort((a, b) => a.lane - b.lane), lineOf = new Map(ordered.map((item, line) => [item.line ?? item.row.id, line]));
  const part = (line: number) => tablePart(lanes.groups.map((group) => items.filter((item) => item.lane < group.lane).length), line, items.length);
  const range = useTimelineTableRange({ rowsRef, part, items: ordered, propertyIds: columns.map((column) => column.property.id), cells, chooseLines: (lines) => chooseLines?.(lines) });
  const rowWays = useLiveWays<TimelineTableRowWays>({
    open: (rowId, mode) => props.openRow(rowId, mode),
    openKey: (rowId, event) => openKey(rowId, event, props.openRowContextMenuFromElement),
    contextMenu: (rowId, event) => props.openRowContextMenu(rowId, event),
    commitTitle: (row, value, escaped) => props.onCommitTitle?.(row, value, escaped),
    add: (rowId, above) => props.onAddBeside?.(rowId, above),
    subitemControls: (item) => <TimelineSubitemControls {...props} item={item} />,
    handle: (rowId) => reorder!.handle(rowId),
    // Pressed, the handle chooses its row and opens the row's menu beside it, as Notion's (seen 2026-11-24).
    handleMenu: (rowId, element) => { choose?.(rowId); props.openRowContextMenuFromElement(rowId, element); },
    menu: (rowId, element) => props.openRowContextMenuFromElement(rowId, element),
    clear: (row, property) => { const now = environmentNow.current; if (now) void now.operations.setValue(row.id, property, null, now.context); },
    chooseRow: (rowId) => choose?.(rowId), part, rangeKey: range.keyDown, rangeChosen: range.chosen,
    close: () => onCloseRow?.(),
    toolsShown: () => cellTools,
    reading: (row, property) => ({ valueContext: propertyValueContext, text: () => shownValueText(row, property, propertyValueContext) }),
    t: props.t,
  });
  useLayoutEffect(() => {
    const host = rowsRef.current;
    if (!host || !onLaneHeightsChange) return;
    let active = true;
    const measure = () => {
      if (!active) return;
      const heights: number[] = [];
      for (const child of host.children) {
        // "+ 새로 만들기" and the calculations' row hold no lane.
        if (!child.hasAttribute("data-timeline-lane")) continue;
        const lane = Number(child.getAttribute("data-timeline-lane"));
        const style = getComputedStyle(child);
        if (Number.isInteger(lane) && lane >= 0) heights[lane] = child.getBoundingClientRect().height + (parseFloat(style.marginTop) || 0) + (parseFloat(style.marginBottom) || 0);
      }
      onLaneHeightsChange(heights);
    };
    // First measured with the view's other first measures, once the task that drew it is done (heldStyleWrites):
    // measured here at once, the page was laid out for it alone, then again for them (2026-11-15).
    const dropFirst = holdMeasure(rowsRef, measure);
    if (typeof ResizeObserver === "undefined") return () => { active = false; dropFirst(); };
    const observer = new ResizeObserver(measure);
    for (const child of host.children) observer.observe(child);
    return () => { active = false; dropFirst(); observer.disconnect(); };
  }, [lanes, items, onLaneHeightsChange]);
  return (
    <div className={styles.timelineTablePane} data-timeline-table-pane="true" data-grouped={grouped ? "true" : undefined} aria-label={props.t("timelineView:table.ariaLabel")}>
      {grouped ? null : <div className={styles.timelineTableHead} data-sticky-head="">{heads()}</div>}
      <div ref={rowsRef} className={styles.timelineTableRows} role="grid" onPaste={range.paste}
        onPointerOver={(event) => { if (rowsRef.current?.dataset.keysPressed !== "true") cellTools.pointerOver(event.target); }}
        onPointerLeave={cellTools.pointerLeave} onFocus={(event) => cellTools.focusIn(event.target)}
        onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) { cellTools.focusIn(null); range.clear(); } }}>
        {lanes.kinds.map((kind, lane) => {
          const group = groupAt.get(lane), add = addAt.get(lane), item = itemAt.get(lane);
          if (group) return <TimelineGroupLaneHeader key={`head:${group.key}`} group={group} onToggle={toggleGroup} actions={groupActions} table pinned={pinned(lane)} />;
          if (add) return <TimelineGroupAddLane key={`${add.more ? "more" : "add"}:${add.key}`} add={add} actions={groupActions} table />;
          if (kind === "newGroup") return <TimelineNewGroupLane key="new-group" lane={lane} actions={groupActions} table />;
          if (kind === "columns") return <div key={`columns:${lane}`} className={styles.timelineTableHead} data-group-columns="true"
            data-sticky-head={pinned(lane) ? "" : undefined} data-timeline-lane={lane} style={{ gridRow: lane + 1 }}>{heads()}</div>;
          const subitemAdd = lanes.subitemAdds.find((candidate) => candidate.lane === lane);
          if (subitemAdd) return <button key={`subitem-add:${subitemAdd.parentId}`} type="button" className={styles.timelineTableCreate}
            data-timeline-subitem-add={subitemAdd.parentId} data-timeline-lane={lane} style={{ gridRow: lane + 1, paddingInlineStart: subitemAdd.depth * 16 + 24 }}
            disabled={!props.onCreateSubitem} onClick={() => props.onCreateSubitem?.(subitemAdd.parentId)}>
            <Plus size="small" aria-hidden="true" />{props.t("tableView:newSubitem")}
          </button>;
          if (!item) return <div key={`gap:${lane}`} aria-hidden="true" className={kind === "gap" ? styles.timelineTableGroupGap : undefined} data-timeline-lane={lane} style={{ gridRow: lane + 1, height: 36 }} />;
          return <TimelineTableRow key={item.line ?? item.row.id} item={item} title={props.rowTitle(item.row)} expanded={props.expandedRows.has(item.row.id)}
            editingNew={props.newTableRowId === item.row.id} columns={steadyColumns} cellSnapshot={propertyCellEnvironment?.snapshot} cellWays={cellWays}
            cellsReadOnly={cellsReadOnly} context={propertyValueContext} ways={rowWays} adds={Boolean(props.onAddBeside)} line={lineOf.get(item.line ?? item.row.id) ?? 0} openIn={openRowIn} closes={Boolean(onCloseRow)}
            moves={Boolean(moving)} rowRef={moving?.row(item.line ?? item.row.id).ref} dragging={moving?.dragging === (item.line ?? item.row.id)} />;
        })}
        {onCreate && !grouped ? <button type="button" className={styles.timelineTableCreate} style={{ gridRow: lanes.kinds.length + 1 }}
          onClick={onCreate}><Plus size="small" aria-hidden="true" />{props.t("timelineView:toolbar.createInCanvas")}</button> : null}
        {/* Under "+ 새로 만들기" a 36px row of its calculations, a title's "계산" at its end (Notion's, seen 2026-11-15). */}
        {summary && !grouped ? <div className={styles.timelineTableSummary} style={{ gridRow: lanes.kinds.length + (onCreate ? 2 : 1) }}>
          {summary(columns.map((column) => `${column.width}px`).join(" "))}</div> : null}
      </div>
    </div>
  );
}

/** What a table panel row's presses do, by the table as last drawn (useLiveWays). */
type TimelineTableRowWays = TimelineTableCellWays & {
  openKey(rowId: string, event: ReactKeyboardEvent<HTMLElement>): void;
  contextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  commitTitle(row: Page, value: string, escaped: boolean): void;
  add(rowId: string, above: boolean): void;
  subitemControls(item: TimelineTableLane): ReactNode;
  handle(rowId: string): ReturnType<TimelineRowReorder["handle"]>;
  handleMenu(rowId: string, element: HTMLElement): void;
  close(): void;
  toolsShown(): CellToolsShown;
  reading(row: Page, property: DbProperty): TableCellValueReading;
  t(key: string, values?: Record<string, unknown>): string;
};

type TimelineTableRowProps = {
  item: TimelineTableLane;
  title: string;
  /** Its sub-items shown under it. */
  expanded: boolean;
  /** Just made here with no date: its title open for typing. */
  editingNew: boolean;
  columns: readonly TimelineTableColumn[];
  /** What its cells read, where they edit in place; their ways are the table's latest. */
  cellSnapshot: PropertyCellSnapshot | undefined;
  cellWays: Pick<PropertyCellEnvironment, "context" | "operations">;
  cellsReadOnly: boolean;
  context: PropertyValueReadContext;
  ways: TimelineTableRowWays;
  /** It shows its "+". */
  adds: boolean;
  /** It moves by its handle, its element the drop target `rowRef` registers. */
  moves: boolean;
  rowRef?: (element: HTMLElement | null) => void;
  dragging: boolean;
  /** Its line among the table's rows. */
  line: number;
  openIn?: "side" | "center" | "full";
  /** Its "열기" reads "닫기" while its row shows in the side peek, and closes it. */
  closes: boolean;
};

// The columns as last drawn, the same list while each reads the same: made anew for each drawing of the timeline.
function useSteadyColumns(columns: readonly TimelineTableColumn[]) {
  const kept = useRef(columns);
  const was = kept.current;
  if (was !== columns && (was.length !== columns.length || columns.some((column, index) => column.property !== was[index].property
    || column.title !== was[index].title || column.width !== was[index].width || column.wrap !== was[index].wrap))) kept.current = columns;
  return kept.current;
}

// Whether a row's cells read the same of two snapshots: the same, or one whose lookups of other
// rows alone were made anew as any row changed, while no cell shown reads other rows and a formula's
// database's properties are the same (PropertyCell draws such a cell again for those alone; otherRowsRead.ts).
function sameCellSnapshot(before: PropertyCellSnapshot | undefined, after: PropertyCellSnapshot | undefined, columns: readonly TimelineTableColumn[]) {
  if (before === after || columns.every(({ title }) => title)) return true;
  if (!before || !after) return false;
  const was = before as unknown as Record<string, unknown>, now = after as unknown as Record<string, unknown>;
  if (Object.keys(now).some((key) => !OTHER_ROWS_LOOKUPS.has(key) && was[key] !== now[key])) return false;
  return columns.every(({ property, title }) => {
    const properties = after.propertiesByDataSourceId[property.databaseId];
    return title || (!readsOtherRows(property, properties)
      && (property.type !== "formula" || before.propertiesByDataSourceId[property.databaseId] === properties));
  });
}

/**
 * One row of the table panel, drawn again only when what it shows changed (its
 * row's page, lane, title, the columns, or the ways its cells read): the
 * timeline drew every row of its table again for one row's value, twice
 * inside the press as its lanes were measured (2026-11-15, NAS + CPU ×2).
 */
const TimelineTableRow = memo(function TimelineTableRow({ item, title, editingNew, columns, cellSnapshot, cellWays, cellsReadOnly, context, ways, adds, moves, rowRef, dragging, line, openIn, closes }: TimelineTableRowProps) {
  return (
    // The row stands for its item among the view's chosen ones (cardSelection), its bar being the item: Shift and a
    // press on its name or a cell chooses it as on its bar, as a table's row (Notion's timeline table, seen 2026-11-24).
    <div ref={rowRef} className={styles.timelineTableTreeRow} data-timeline-table-row-id={item.row.id} data-timeline-lane={item.lane}
      role="row" aria-rowindex={line + 2} data-timeline-table-line={item.line ?? item.row.id}
      data-card-choice-for={item.line ?? item.row.id}
      data-subitem-depth={item.depth > 0 ? item.depth : undefined} data-dragging={dragging ? "true" : undefined} style={{ gridRow: item.lane + 1 }}>
      {/* Chosen, the whole row is tinted, its shown values too, 2px in over a 32px line (Notion's, seen 2026-11-24: 676×32 over a 680px row). */}
      <CardChoiceHalo itemId={item.row.id} inset="2px 2px 1px" />
      {/* Its "+" 44px before it and its ⠿ 20px before it, named and tipped as a table row's (Notion's, seen 2026-11-24 in two tabs:
          24×24 and 18×24, 3px down its first line); the "+" makes a row of no date under it, over it with ⌥ (useTimelineRowsMade). */}
      {adds ? <IconTipButton className={styles.timelineRowAdd} data-timeline-row-add label={ways.t("blockHandle:buttons.addBlockBelow", { key: optionKeyName() })}
        tip={<AddHereTip />} placement="bottom" wait="handle" onClick={(event) => ways.add(item.line ?? item.row.id, event.altKey)}><Plus size="regular" aria-hidden="true" /></IconTipButton> : null}
      {moves ? <IconTipButton tabIndex={-1} wait="handle" className={styles.timelineRowHandle} label={ways.t("blockHandle:buttons.dragHint")} tip={<HandleTip drag />}
        placement="bottom" data-row-menu-handle data-card-handle-for={item.row.id} {...ways.handle(item.line ?? item.row.id)}
        onClick={(event) => ways.handleMenu(item.row.id, event.currentTarget)}><DragHandleIcon size="regular" aria-hidden="true" /></IconTipButton> : null}
      {columns.map((column, index) => column.title ? <Fragment key={column.property.id}>
        {/* Written in place as a table's title cell, opened from its "열기" (TimelineTableNameCell); it only opens where nothing is written here. */}
        {cellSnapshot && !editingNew ? <TimelineTableNameCell row={item.row} property={column.property} column={index} snapshot={cellSnapshot}
          cellWays={cellWays} readOnly={cellsReadOnly} context={context} ways={ways} openIn={openIn} closes={closes}
          before={<>{ways.subitemControls(item)}<PeekHalo itemId={item.row.id} style={{ borderRadius: 6 }} /></>} /> : <>{ways.subitemControls(item)}{editingNew ? <NewRowTitle row={item.row} label={ways.t("timelineView:bar.newTitle")}
          onCommit={(value, _byKey, escaped) => ways.commitTitle(item.row, value, escaped)} /> : <button
          type="button"
          className={styles.timelineTableRow}
          data-row-id={item.row.id}
          title={title}
          onKeyDown={(event) => ways.openKey(item.row.id, event)}
          onClick={(event) => ways.open(item.row.id, rowOpenModeOfClick(event))}
          onAuxClick={(e) => {
            if (e.button === 1) {
              e.preventDefault();
              ways.open(item.row.id, "new_tab");
            }
          }}
          onContextMenu={(event) => ways.contextMenu(item.row.id, event)}
        >
          <PeekHalo itemId={item.row.id} style={{ borderRadius: 6 }} />
          <PageIconGlyph page={{ icon: item.row.icon ?? "", iconType: item.row.iconType, workspaceId: item.row.workspaceId,
            kind: item.row.kind, notionIcon: item.row.notionIcon, hasContent: item.row.hasContent }} size={18} />
          <span><ArchivedRowTitle archived={Boolean(item.row.archivedAt)}>{title}</ArchivedRowTitle></span>
          {/* Its open discussions after its name, as a table's title (Notion's timeline table, seen 2026-11-15). */}
          <RowCommentCount rowId={item.row.id} insideButton />
        </button>}</>}
      </Fragment> : cellSnapshot ? (
        // Written in place as a table's cell (TimelineTableCell).
        <TimelineTableCell key={column.property.id} row={item.row} property={column.property} column={index} width={column.width} wrap={column.wrap}
          editable={!cellsReadOnly && !item.row.isLocked && LOCAL_PROPERTY_CELL_TYPES.has(column.property.type)} readOnly={cellsReadOnly} ways={ways}
          after={<TableCellValueTools shown={ways.toolsShown()} cell={toolsCellKey(item.line ?? item.row.id, index)} page={item.row} property={column.property}
            read={() => ways.reading(item.row, column.property)} />}>
          {!item.row.isLocked && LOCAL_PROPERTY_CELL_TYPES.has(column.property.type) ? (
            <PropertyCell snapshot={cellSnapshot} {...cellWays} row={item.row} prop={column.property} readOnly={cellsReadOnly}
              onOpenPage={(id) => ways.open(id)} />
          ) : <PropValue context={context} row={item.row} prop={column.property} interactive={!cellsReadOnly} />}
        </TimelineTableCell>
      ) : (
        <div key={column.property.id} className={styles.timelineTableCell} style={{ width: column.width }}
          data-timeline-table-cell={column.property.id} data-wrap={column.wrap ? "true" : undefined}>
          <PropValue context={context} row={item.row} prop={column.property} interactive={false} />
        </div>
      ))}
    </div>
  );
}, (previous, next) => {
  for (const key of Object.keys(next) as (keyof TimelineTableRowProps)[]) {
    if (key === "context" || key === "cellSnapshot") continue;
    if (key === "item" ? previous.item !== next.item && (previous.item.row !== next.item.row || previous.item.lane !== next.item.lane
      || previous.item.depth !== next.item.depth || previous.item.hasChildren !== next.item.hasChildren || previous.item.childCount !== next.item.childCount)
      : previous[key] !== next[key]) return false;
  }
  const values = next.columns.filter((column) => !column.title).map((column) => column.property);
  return sameCellSnapshot(previous.cellSnapshot, next.cellSnapshot, next.columns) && sameOwnRowValues(previous.context, next.context, values);
});
