"use client";

import { memo, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import type { DbProperty, Page } from "@/lib/types";
import type { PropertyValueReadContext } from "./PropValue";
import { PropValue } from "./PropValue";
import { PersonValueLookArea } from "./PersonBadge";
import { conditionalColorBackground } from "./conditionalColorModel";
import type { ColorName } from "./colors";
import type { TimelineInteractionMode } from "./timelineInteractionModel";
import { NewRowTitle, type TimelineRenderedRow, type TimelineRowOpenMode } from "./TimelineRows";
import styles from "./databaseSheet";
import depStyles from "./timelineDeps.module.css";
import { CardTitleIcon } from "./CardTitleIcon";
import { ArchivedRowTitle } from "./DatabaseArchivedRowsMode";
import { PeekHalo } from "./PeekHalo";
import { CardChoiceHalo } from "./cardSelection";
import { sameOwnRowValues } from "./memoizedViewItems";
import { TimelineResizeEdge } from "./TimelineResizeEdge";

/** What every bar of a timeline draws alike, the same object while none of it changes (useSteadyFrame). */
export type TimelineBarFrame = {
  /** The values a bar shows before its title, in the view's order, and those after it. */
  before: DbProperty[];
  after: DbProperty[];
  /** "페이지 아이콘 표시": a page's own icon before its title. */
  pageIcons: boolean;
  /** Its title shown among its values; hidden, a bar shows its values alone (viewTitleFree). */
  titled: boolean;
  readOnly: boolean;
  /** Its bars placed by a rollup's dates, which open but don't move. */
  datesFixed: boolean;
  /** Its bars' ends drag, where it has an end date and they move. */
  resizes: boolean;
  /** Its columns' length in ms where they are hours, else null (TimelineResizeEdge). */
  hourColumns: number | null;
  /** Its bars make dependencies by their handles. */
  dependencies: boolean;
  flattenedSubtasks: boolean;
  hiddenSubtasks: boolean;
};

/** What a bar's presses do, by the timeline as last drawn (useLiveWays). */
export type TimelineBarWays = {
  begin(event: ReactPointerEvent<HTMLElement>, item: TimelineRenderedRow, mode: TimelineInteractionMode): void;
  pointerUp(): void;
  key(item: TimelineRenderedRow, event: ReactKeyboardEvent<HTMLElement>): void;
  click(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  open(rowId: string, mode: TimelineRowOpenMode): void;
  contextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  dependencyStart(rowId: string): void;
  dependencyToggle(rowId: string): void;
  dependencyDrop(rowId: string): void;
  dependencyPick(rowId: string): void;
  /** `byKey`: closed with Enter or Esc, which leaves its bar chosen (useTimelineNewBarChoice). */
  commitTitle(row: Page, value: string, byKey?: boolean): void;
  subitemControls(item: TimelineRenderedRow): ReactNode;
  t(key: string, values?: Record<string, unknown>): string;
  titleOf(rowId: string): string;
  /** Whether a press on the bar opens it: not right after a drag. */
  opens(): boolean;
};

type TimelineBarProps = {
  /** Its row, and its line where the timeline is grouped: the line the view chooses, a row under several groups having one in each (viewGroupLines.ts). */
  item: TimelineRenderedRow & { line?: string };
  title: string;
  dateStart: string;
  dateEnd: string;
  /** Too short for its title and values, which run on past its end (contentFits). */
  narrow: boolean;
  /** Moved "whole" with the pointer, or stretched by its start or end (drawn over the days it would take). */
  moving: false | "whole" | "start" | "end";
  tint: ColorName | undefined;
  /** A flattened timeline's sub-item's parent's title. */
  parentTitle: string;
  /** Its sub-items shown under it. */
  expanded: boolean;
  /** Just made here: its title open for typing. */
  editingNew: boolean;
  /** The bar a dependency is being made from, if any. */
  dependencyFrom: string | null;
  barRef: (element: HTMLElement | null) => void;
  frame: TimelineBarFrame;
  context: PropertyValueReadContext;
  ways: TimelineBarWays;
};

function sameItem(left: TimelineRenderedRow, right: TimelineRenderedRow) {
  return left === right || (left.row === right.row && left.depth === right.depth && left.hasChildren === right.hasChildren
    && left.childCount === right.childCount && left.first === right.first && left.last === right.last && left.lane === right.lane
    && left.start.getTime() === right.start.getTime() && left.end.getTime() === right.end.getTime());
}

/**
 * One bar of a timeline, drawn again only when what it shows changed (its
 * row's page, place, title, dates, color, or the timeline's frame); the
 * timeline made its items anew for any row, and drew every bar for one row's
 * value (2026-11-15: 31 bars, ~780 parts, NAS + CPU ×2). The value context,
 * made anew as any row comes or goes, counts only where a value it shows
 * reads other rows (sameOwnRowValues).
 */
export const TimelineBar = memo(function TimelineBar({ item, title, dateStart, dateEnd, narrow, moving, tint, parentTitle, editingNew,
  dependencyFrom, barRef, frame, context, ways }: TimelineBarProps) {
  const id = item.row.id, line = item.line ?? id;
  const values = (run: readonly DbProperty[]) => run.length ? <span className={styles.timelineBarMeta} data-timeline-bar-values>
    {/* A bar's people show as their faces alone, as Notion's. */}
    <PersonValueLookArea look="faces">{run.map((property) => (
      <span key={property.id} className={styles.timelineBarProp}><PropValue context={context} row={item.row} prop={property} interactive={false} /></span>
    ))}</PersonValueLookArea>
  </span> : null;
  return (
    <div
      ref={barRef}
      className={styles.timelineBar}
      data-timeline-row-id={id}
      data-timeline-row-title={title}
      data-card-choice={line}
      data-card-title={title}
      data-subitem-depth={item.depth > 0 ? item.depth : undefined}
      data-narrow={narrow ? "true" : undefined}
      data-subitem-toggle={item.hasChildren ? "true" : undefined}
      data-moving={moving || undefined}
      data-dates-fixed={frame.datesFixed ? "true" : undefined}
      // One who only reads it (a published site's visitor) points at a bar
      // that opens and doesn't move, and pointing doesn't tint it: Notion's
      // public timeline changes nothing under the pointer (seen 2026-11-24).
      data-read-only={frame.readOnly ? "true" : undefined}
      // A moment under the hour zooms covers no column: an 11px stub at its start (timelineHourSpan).
      data-moment={item.last < item.first ? "true" : undefined}
      data-conditional-color={tint}
      title={`${title} · ${dateStart}${dateEnd !== dateStart ? ` – ${dateEnd}` : ""}`}
      style={{ gridColumn: `${item.first + 1} / span ${Math.max(1, item.last - item.first + 1)}`, gridRow: item.lane + 1, ...(tint ? { background: conditionalColorBackground(tint) } : {}) }}
      onPointerDown={(event) => ways.begin(event, item, "move")}
      onPointerUp={() => ways.pointerUp()}
    >
      <PeekHalo itemId={id} />
      <CardChoiceHalo itemId={line} inset={2} />
      {ways.subitemControls(item)}
      {frame.dependencies && (
        <>
          <button
            type="button"
            className={depStyles.depSourceHandle}
            data-active={dependencyFrom === id ? "true" : undefined}
            aria-label={ways.t("timelineView:dependencies.startFrom", { title })}
            onPointerDown={(event) => {
              event.preventDefault();
              event.stopPropagation();
              ways.dependencyStart(id);
            }}
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              if (event.detail === 0) ways.dependencyToggle(id);
            }}
          />
          {dependencyFrom && dependencyFrom !== id && (
            <button
              type="button"
              className={depStyles.depTargetHandle}
              aria-label={ways.t("timelineView:dependencies.makeDependOn", { predecessor: ways.titleOf(dependencyFrom), title })}
              onPointerDown={(event) => { event.preventDefault(); event.stopPropagation(); }}
              onPointerUp={(event) => {
                event.preventDefault();
                event.stopPropagation();
                ways.dependencyDrop(id);
              }}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                if (event.detail === 0) ways.dependencyPick(id);
              }}
            />
          )}
        </>
      )}
      {editingNew && !frame.readOnly ? <NewRowTitle row={item.row} label={ways.t("timelineView:bar.newTitle")}
        onCommit={(value, byKey) => ways.commitTitle(item.row, value, byKey)} /> : <button
        type="button"
        className={styles.timelineBarOpen}
        aria-label={ways.t("timelineView:bar.openAriaLabelWithDates", { title, start: dateStart, end: dateEnd })}
        onKeyDown={(event) => ways.key(item, event)}
        onClick={(event) => ways.click(id, event)}
        onAuxClick={(event) => {
          if (event.button === 1 && ways.opens()) {
            event.preventDefault();
            ways.open(id, "new_tab");
          }
        }}
        onContextMenu={(event) => ways.contextMenu(id, event)}
      >
        {frame.resizes && <TimelineResizeEdge side="start" at={item.start} hour={frame.hourColumns} stretching={moving === "start"} onPointerDown={(event) => ways.begin(event, item, "resize-start")} />}
        <span className={styles.timelineBarLabel}>
          {values(frame.before)}
          {frame.titled && <>
            <CardTitleIcon row={item.row} show={frame.pageIcons} size={14} className={styles.cardTitleIcon} ownOnly />
            <span className={styles.timelineBarTitle}><ArchivedRowTitle archived={Boolean(item.row.archivedAt)}>{title}</ArchivedRowTitle></span>
          </>}
          {frame.flattenedSubtasks && item.row.subitemParentId && (
            <span className={styles.subitemParentIndicator}>{ways.t("tableView:subitemOf", { title: parentTitle })}</span>
          )}
          {frame.hiddenSubtasks && item.childCount > 0 && (
            <span className={styles.subitemCount}>{ways.t("tableView:subitemCount", { count: item.childCount })}</span>
          )}
          {values(frame.after)}
        </span>
        {frame.resizes && <TimelineResizeEdge side="end" at={item.end} hour={frame.hourColumns} stretching={moving === "end"} onPointerDown={(event) => ways.begin(event, item, "resize-end")} />}
      </button>}
    </div>
  );
}, (previous, next) => {
  for (const key of Object.keys(next) as (keyof TimelineBarProps)[]) {
    if (key === "context") continue;
    if (key === "item" ? !sameItem(previous.item, next.item) : previous[key] !== next[key]) return false;
  }
  return sameOwnRowValues(previous.context, next.context, [...next.frame.before, ...next.frame.after]);
});
