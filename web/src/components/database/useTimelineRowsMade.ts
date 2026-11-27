"use client";

import { useRef, type MutableRefObject } from "react";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, DbView } from "@/lib/types";
import { holdTypingForField } from "@/lib/pageTitleFocus";
import { viewFilterSeedValues, type ViewFilterSeedValues } from "./query";
import { dateKey } from "./dateUtils";
import { filledFromTemplate } from "./freshRows";
import { timelineColumnStart } from "./timelineViewModel";
import { dateTimeValue } from "./timelineDateValues";
import { tableGroupSeedValue } from "./tableGrouping";
import { lineGroup, lineRowId } from "./viewGroupLines";
import { makeRowThatOpens, newRowSeedsShow } from "./newViewRow";
import type { TimelineTableLane, TimelineGroupLane } from "./timelineLaneProjection";
import type { TimelineViewOperationContext, TimelineViewOperations, TimelineViewSnapshot } from "./TimelineView";

type Run = <Result>(requestKey: string, perform: (context: TimelineViewOperationContext) => Promise<Result>,
  publish?: (result: Result, context: TimelineViewOperationContext) => void, write?: boolean) => Promise<void>;

/**
 * The rows a timeline makes: a bar where its canvas is pressed or a group's
 * head "+" (from today, four days), its table's "+ 새로 만들기" (no date), a
 * sub-item, and a table row's "+" — a row with no date right under it, over it
 * with ⌥, as Notion's (seen 2026-11-24 in Chrome at 1685, dpr 1, two tabs at
 * different times: the new row's title open, a letter typed after the press
 * going into it, judged after the press with no script in the page; under a
 * group it takes that group's value; "날짜 없음 (N)" counts it; Escape in its
 * title showing no words takes it away and chooses the row under it, a title
 * typed keeps it). That row is made as a table's row "+" makes one, by the
 * table writer's createRow beside the row (NativeTimelineViewMount): a step
 * of ⌘Z, its title typed another, Notion's two too (seen once). What every
 * new row starts with: the values the view's filters ask for and, made in a
 * group, that group's.
 */
export function useTimelineRowsMade({ operations, run, readOnly, offersNewRows, snapshot, properties, view, startProperty, endProperty, groupProperty,
  anchor, columnStep, columnsPerDay, hourly, newRowMade, setNewRowId, setNewTableRowId, groups, table, choose }: {
  operations: TimelineViewOperations;
  run: Run;
  readOnly: boolean;
  offersNewRows: boolean;
  snapshot: Pick<TimelineViewSnapshot, "contextPageId" | "currentUserId" | "queryBounds" | "view" | "properties" | "workspaceId">;
  properties: DbProperty[];
  view: DbView;
  startProperty?: DbProperty;
  endProperty?: DbProperty;
  groupProperty?: GraphProperty;
  anchor: Date;
  columnStep: number | false;
  columnsPerDay: number;
  hourly: boolean;
  /** A bar just made, its title opened as it is first drawn. */
  newRowMade: MutableRefObject<string | null>;
  setNewRowId(rowId: string | null): void;
  setNewTableRowId(rowId: string | null): void;
  groups: readonly TimelineGroupLane[];
  /** The table's rows, by their lanes. */
  table: readonly TimelineTableLane[];
  choose(rowId: string): void;
}) {
  // The row a table row's "+" just made, and the row under it then.
  const made = useRef<{ id: string; under: string | undefined } | null>(null);
  const seeds = (groupKey?: string): ViewFilterSeedValues => {
    const filtered = viewFilterSeedValues(properties, view, startProperty ? [startProperty.id] : [],
      { currentPageId: snapshot.contextPageId, currentUserId: snapshot.currentUserId }, snapshot.queryBounds);
    const seed = groupKey !== undefined && groupProperty ? tableGroupSeedValue(groupProperty, groupKey, snapshot.view.config) : undefined;
    return seed === undefined ? filtered : { ...filtered, properties: { ...filtered.properties, [groupProperty!.id]: seed } };
  };

  const createAt = (first: number | null, duration: number, groupKey?: string) => {
    // By a rollup, or from the table (`first` null), "+ 새로 만들기" makes a page of no date, as Notion's (seen 2026-11-06; 2026-11-15 twice).
    if (readOnly || !startProperty) return;
    const start = timelineColumnStart(anchor, first ?? 0, columnStep), end = timelineColumnStart(anchor, (first ?? 0) + duration, columnStep);
    const value = first === null ? "" : hourly ? `${dateTimeValue(start)}/${dateTimeValue(end)}` : duration ? `${dateKey(start)}/${dateKey(end)}` : dateKey(start);
    const given = seeds(groupKey), [from, to] = value.split("/");
    // One the view would leave out — in a template's body, by a view kept to "this page" — opens at its title instead,
    // as Notion's timeline (seen 2026-11-27 twice: a bar pressed on its canvas and its "+ 새로 만들기"; newViewRow.ts).
    if (!newRowSeedsShow(value ? { ...given, properties: { ...given.properties, [startProperty.id]: to ? { start: from, end: to } : { start: from } } } : given, snapshot)) {
      const made: { row: GraphRow | null; context?: TimelineViewOperationContext } = { row: null };
      void makeRowThatOpens((opened) => run("row:create", async (context) => { made.context = context; made.row = await operations.createDatedRow(value, given, context, opened); }).then(() => made.row),
        (rowId) => { if (made.context) operations.openRow(rowId, made.context); });
      return;
    }
    // What is typed before the bar's title opens goes into it.
    void holdTypingForField(async () => {
      let done = false;
      // Its title's editor comes with the bar, drawn as it shows.
      await run("row:create", context => operations.createDatedRow(value, given, context, (row) => { if (first !== null) newRowMade.current = row.id; else setNewTableRowId(row.id); }),
        row => { if (row) { (first === null ? setNewTableRowId : setNewRowId)(row.id); done = true; } });
      return done;
    });
  };

  // On the canvas a sub-item is a bar where pressed, as long as a new row's; in the table it has no date yet.
  const createSubitem = !readOnly && offersNewRows && operations.createSubitem ? (parentId: string, first?: number) => {
    const values: Record<string, unknown> = {};
    if (first !== undefined && startProperty) {
      const start = timelineColumnStart(anchor, first, columnStep), end = timelineColumnStart(anchor, first + 4 * columnsPerDay, columnStep);
      const [from, to] = hourly ? [dateTimeValue(start), dateTimeValue(end)] : [dateKey(start), dateKey(end)];
      if (endProperty) { values[startProperty.id] = from; values[endProperty.id] = to; } else values[startProperty.id] = `${from}/${to}`;
    }
    void holdTypingForField(async () => {
      let done = false;
      await run(`row:subitem:${parentId}`, context => operations.createSubitem!(parentId, values, context), row => {
        if (row) { (first === undefined ? setNewTableRowId : setNewRowId)(row.id); done = true; }
      });
      return done;
    });
  } : undefined;

  // A table row's "+": a row of no date under it (over it, `above`), its title open as it shows — not
  // while a sorted view asks whether its sort goes, nor once answered (sortedViewDrop.ts). Pressed on a
  // row's line under one of its groups, the row takes that group's value, as Notion's (viewGroupLines.ts).
  const addBeside = !readOnly && offersNewRows && startProperty && operations.createRowBeside ? (line: string, above: boolean) => {
    const rowId = lineRowId(line), lanes = [...table].sort((a, b) => a.lane - b.lane), at = lanes.findIndex((item) => (item.line ?? item.row.id) === line);
    const given = seeds(lineGroup(line) ?? groups.find((group) => group.rowIds.includes(rowId))?.key);
    const under = above ? rowId : lanes[at + 1]?.row.id;
    void holdTypingForField(async () => {
      let done = false, asked = false;
      await run(`row:beside:${rowId}`, context => operations.createRowBeside!({ id: rowId, side: above ? "before" : "after" }, given, context, (row, asking) => {
        asked = Boolean(asking);
        if (asked) return;
        made.current = { id: row.id, under };
        setNewTableRowId(row.id);
      }), row => { if (row && !asked) { setNewTableRowId(row.id); done = true; } });
      return done;
    });
  } : undefined;

  // A paste's rows its table fills and its lines past the table's part: rows of no date after the part's last line, in its group with
  // its value, each with its line's values, in the lines' order, one step of ⌘Z with the values pasted (tablePasteWrite.ts pastedRowsMade).
  const pasteRows = !readOnly && offersNewRows && operations.pasteRows ? (changed: readonly GraphRow[], lines: readonly GraphRow["values"][], after: string) => {
    const given = seeds(lineGroup(after) ?? groups.find((group) => group.rowIds.includes(lineRowId(after)))?.key);
    void run(`row:paste:${lineRowId(after)}`, (context) => operations.pasteRows!(changed, { after: lineRowId(after), seeds: given.properties, rows: lines }, context), undefined, true);
  } : undefined;

  return {
    createAt,
    createSubitem,
    addBeside,
    pasteRows,
    /**
     * A new row's title closed: true when Escape left the "+"'s row with no
     * words, and it was taken away — not one a template fills, as a table's
     * (freshRows.ts filledFromTemplate).
     */
    leftUnnamed(rowId: string, title: string, escaped: boolean) {
      const fresh = made.current;
      if (fresh?.id === rowId) made.current = null;
      if (!escaped || fresh?.id !== rowId || title.trim() || !operations.discardRow || filledFromTemplate(rowId)) return false;
      operations.discardRow(rowId);
      setNewTableRowId(null);
      if (fresh.under) choose(fresh.under);
      return true;
    },
  };
}
