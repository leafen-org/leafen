import { statusGroupTitle } from "./StatusGroupLabel";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CaretRightFill, DotsHorizontal, Plus } from "../icons";
import { keptItems } from "@/lib/rowsReplaced";
import type { BoardBucket } from "./boardViewModel";
import { chipStyle } from "./colors";
import { GroupPageGlyph } from "./GroupPageGlyph";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";
import styles from "./databaseSheet";
import laneStyles from "./BoardLanes.module.css";

const cellsKept = new WeakMap<object, Map<string, WeakMap<readonly { id: string }[], readonly { id: string }[]>>>();
const idOf = (row: { id: string }) => row.id;

/**
 * A column's cards in a sub-group (`laneKey`, among those `keysOf` says a
 * card is in), in the column's order: kept for the column's cards and, when
 * an edit that arrives changes some, followed for those alone
 * (lib/rowsReplaced.ts keptItems), not every card looked up again each time
 * the board draws.
 */
export function laneCell<Row extends { id: string }>(columnRows: readonly Row[], laneKey: string, keysOf: (row: Row) => readonly string[]): Row[] {
  let byLane = cellsKept.get(keysOf);
  if (!byLane) cellsKept.set(keysOf, byLane = new Map());
  let kept = byLane.get(laneKey);
  if (!kept) byLane.set(laneKey, kept = new WeakMap());
  return keptItems(columnRows, kept as WeakMap<readonly Row[], readonly Row[]>, (row) => keysOf(row).includes(laneKey), idOf) as Row[];
}

/**
 * A board split by a second property, as Notion's (seen 2026-10-10,
 * 2026-10-11): the columns' heads once on top, then each sub-group as a row
 * across every column — its name, which folds it, the fold kept with the
 * view — and under it a cell per column with that column's cards of the
 * sub-group and "새 페이지"; last, "+ 신규 그룹" (`addLane`, ViewGroupAdd by
 * what the sub-groups are grouped by), at their start. Pointed at, a sub-group's head shows "⋯", its
 * menu, and "+", a page made in it (Notion's, seen 2026-11-03, 11-04), after
 * its name and the count its menu shows.
 */
export function BoardLanes<Row extends { id: string }>({ columns, lanes, laneKeysOf, folded, onFold, addLane, counted, onLaneMenu, onLaneAdd, renderColumn }: {
  columns: readonly BoardBucket<Row>[];
  lanes: readonly BoardBucket<Row>[];
  /** The keys of the sub-groups a card is in. */
  laneKeysOf(row: Row): readonly string[];
  folded: ReadonlySet<string>;
  onFold(key: string, folded: boolean): void;
  /** "+ 신규 그룹", given the class that places it. */
  addLane?: (className: string) => ReactNode;
  /** The sub-groups whose count shows, hidden until their menu shows it. */
  counted?: ReadonlySet<string>;
  /** A sub-group's "⋯", or its name chip (`naming`): an option's sub-group renamed in place (useBoardLanes). */
  onLaneMenu?: (lane: BoardBucket<Row>, element: HTMLElement, naming?: boolean) => void;
  onLaneAdd?: (laneKey: string) => void;
  renderColumn(column: BoardBucket<Row>, part: "head" | "cell", laneKey?: string): ReactNode;
}) {
  const { t } = useTranslation(["pageTreeItem", "boardView", "propValue"]);
  const offersNewRows = useViewOffersNewRows();
  return (
    <div className={laneStyles.lanes} data-board-lanes>
      <div className={laneStyles.row}>{columns.map((column) => renderColumn(column, "head"))}</div>
      {lanes.map((lane) => {
        const open = !folded.has(lane.key);
        return (
          <section key={lane.key} className={laneStyles.lane} data-board-lane={lane.key}>
            <header className={laneStyles.laneHead}>
              <button type="button" className={laneStyles.fold} aria-expanded={open}
                aria-label={t(open ? "pageTreeItem:collapse" : "pageTreeItem:expand", { title: lane.label })} onClick={() => onFold(lane.key, open)}>
                <CaretRightFill aria-hidden="true" />
              </button>
              {/* An unnamed, disabled checkbox and the property's name, as Notion's (seen 2026-11-19). */}
              {lane.checked !== undefined ? (
                <span className={styles.boardCheckboxGroup}>
                  <span className={styles.cardCheckbox} data-checked={lane.checked ? "true" : undefined}
                    role="checkbox" aria-checked={lane.checked} aria-disabled="true">
                  </span>
                  {lane.label}
                </span>
              ) : onLaneMenu && !lane.statusStage && (lane.statusOption || lane.color) && lane.key !== "__none" ? (
                // An option's chip is pressed to rename it in place, as a column's (BoardColumn; Notion's, seen 2026-11-28).
                <button type="button" className={`${styles.boardGroupBtn} ${laneStyles.laneName}`} data-lane-name onClick={(event) => onLaneMenu(lane, event.currentTarget, true)}>
                  {laneName(lane)}</button>
              ) : laneName(lane)}
              {counted?.has(lane.key) ? <span className={laneStyles.count}>{lane.rows.length}</span> : null}
              {onLaneMenu ? <button type="button" className={styles.boardGroupMore} data-lane-action aria-label={t("boardView:groupActions", { groupName: lane.label })}
                onClick={(event) => onLaneMenu(lane, event.currentTarget)}><DotsHorizontal size="small" aria-hidden="true" /></button> : null}
              {onLaneAdd && offersNewRows ? <button type="button" className={styles.boardGroupMore} data-lane-action aria-label={t("boardView:addToGroup", { groupName: lane.label })}
                onClick={() => onLaneAdd(lane.key)}><Plus size="small" aria-hidden="true" /></button> : null}
            </header>
            {open ? (
              <div className={laneStyles.row}>
                {columns.map((column) => renderColumn({ ...column, rows: laneCell(column.rows, lane.key, laneKeysOf) }, "cell", lane.key))}
              </div>
            ) : null}
          </section>
        );
      })}
      {addLane?.(laneStyles.addLane)}
    </div>
  );
}

/** A sub-group's name as its head shows it: a status's mark or option, an option's chip, no value's words, or its value's. */
function laneName<Row>(lane: BoardBucket<Row>) {
  return statusGroupTitle(lane, lane.label) ?? (lane.color ? <span className={styles.chip} style={chipStyle(lane.color)}><span className={styles.chipText}>{lane.label}</span></span>
    : lane.key === "__none" ? <span className={styles.boardNoGroup}>{lane.label}</span>
    : <span className={styles.boardValueGroup}><GroupPageGlyph page={lane.page} pixels={18} />{lane.label}</span>);
}
