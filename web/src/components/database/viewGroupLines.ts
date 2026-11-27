import { rowAfterInRun } from "@/lib/dragReorderOwnPlace";

/**
 * A grouped view's line of a row: the row under one of its groups. A row
 * whose value holds several members — tags, people, related pages — stands
 * under each member's group, as Notion's tables, lists, galleries, boards and
 * timelines show it (tableGroupsOf; seen 2026-11-24 in Chrome at 1685, dpr 1:
 * tags x and y under x and under y), so a drag names the line it moves: the
 * group's key and the row's id. Where the view is not grouped a line is its
 * row's id alone.
 */
const MARK = "\u001f";

export function groupLine(group: string | undefined, rowId: string) {
  return group === undefined ? rowId : `${group}${MARK}${rowId}`;
}

export function lineRowId(line: string) {
  return line.slice(line.lastIndexOf(MARK) + 1);
}

export function lineGroup(line: string) {
  const at = line.lastIndexOf(MARK);
  return at < 0 ? undefined : line.slice(0, at);
}

/**
 * The rows a view's choice holds, each once: each chosen line's, and each row
 * chosen whole — copies just made, rows brought back, a row a menu chose.
 * A row stands chosen on all its lines, as Notion's grouped views tint every
 * line of a row one of whose lines is chosen (seen 2026-11-24).
 */
export function chosenRowsOf(chosen: Iterable<string>): ReadonlySet<string> {
  return new Set(Array.from(chosen, lineRowId));
}

/**
 * A view's choice read on its lines (`lines`, in order), as Notion's grouped
 * table chooses: by the line pressed (seen 2026-11-24 in two tabs: 나, tagged
 * x and y, ticked under x read "1개 선택됨" with its line under y tinted, not
 * ticked; ticked under y too, "2개 선택됨"); a row chosen whole stands for its
 * first line (the copies of 나's two lines came chosen under x, their first
 * group), and so does a chosen line no longer shown, its row's value having
 * moved it. A chosen row the view doesn't show (a filter hides it) stays as
 * it is. The same set while each is a line shown.
 */
export function chosenAsLines(chosen: ReadonlySet<string>, lines: readonly string[]): ReadonlySet<string> {
  const shown = new Set(lines);
  if ([...chosen].every((id) => shown.has(id))) return chosen;
  const first = new Map<string, string>();
  for (const line of lines) if (!first.has(lineRowId(line))) first.set(lineRowId(line), line);
  return new Set([...chosen].map((id) => shown.has(id) ? id : first.get(lineRowId(id)) ?? id));
}

/**
 * The lines a drag of `source` carries among a view's lines in their order:
 * where it or its row is chosen, each chosen line under its own parent, in
 * that order — a row chosen whole by its line in the source's group where it
 * stands there too, else its first — as Notion's views carry their chosen
 * rows (chosenCarried); else it alone. Notion carries a row chosen under
 * another group from that group (seen 2026-11-24: 나 chosen under y and 가
 * dragged from x to z left 나 x and z).
 */
export function chosenLinesCarried(lines: readonly { line: string; parentId?: string | null }[], chosen: ReadonlySet<string>, source: string): string[] {
  if (!chosen.has(source) && !chosen.has(lineRowId(source))) return [source];
  const group = lineGroup(source), parentId = lines.find(({ line }) => line === source)?.parentId ?? null;
  const kept = new Set<string>(), whole = new Map<string, string>();
  for (const { line, parentId: parent } of lines) {
    const rowId = lineRowId(line);
    if ((parent ?? null) !== parentId) continue;
    if (chosen.has(line)) kept.add(line);
    else if (chosen.has(rowId) && (!whole.has(rowId) || (lineGroup(line) === group && lineGroup(whole.get(rowId)!) !== group))) whole.set(rowId, line);
  }
  for (const line of whole.values()) kept.add(line);
  const carried = lines.flatMap(({ line }) => kept.has(line) ? [line] : []);
  return carried.includes(source) ? carried : [source];
}

/**
 * A drag of lines (`carried`) let go beside `target`'s: the rows it moves,
 * the row it lands beside, the group each stood in or lands in — the line's
 * own, which a row under several takes its value from (groupDropValue: tags
 * x and y dragged from y to z are x and z, seen 2026-11-24) — and the lines
 * then chosen, each row's in the group it lands in.
 */
export function linesMoved(carried: readonly string[], target: string) {
  const groups = new Map<string, string | undefined>();
  for (const line of carried) if (!groups.has(lineRowId(line))) groups.set(lineRowId(line), lineGroup(line));
  const to = lineGroup(target), rowIds = [...groups.keys()];
  return { rowIds, targetId: lineRowId(target), to, groupOf: (rowId: string) => groups.get(rowId), landed: rowIds.map((rowId) => groupLine(to, rowId)) };
}

/**
 * How a view's rows are dragged by their lines — a table's, a list's: `rowOf`
 * reads each of its lines (`lines`, in order) that is a row's, its line's
 * name, parent and depth. A chosen row's line carries a line of each chosen
 * row (chosenLinesCarried), none of whose lines it lands on (`takes`); the
 * row right after one in its run takes them after itself (rowAfterInRun).
 * `shown` are the rows' lines in order.
 */
export function viewLinesDrag<Line extends { kind: string; depth?: number }>(lines: readonly Line[],
  rowOf: (line: Line, index: number) => { line: string; parentId: string | null; depth: number } | undefined, chosen: ReadonlySet<string>) {
  const read = lines.map(rowOf), rows = read.flatMap((row) => row ? [row] : []);
  const run = lines.map((line, index) => { const row = read[index]; return row ? { kind: "row", item: { row: { id: row.line }, depth: row.depth } } : line; });
  let asked: { source: string; rows: ReadonlySet<string> } | null = null;
  const carry = (source: string) => chosenLinesCarried(rows, chosen, source);
  return {
    shown: rows.map(({ line }) => line),
    /** The line at `index` among the view's lines, where it is a row's. */
    lineAt: (index: number) => read[index]?.line,
    next: (source: string) => rowAfterInRun(run, source),
    carry,
    /** Whether a dragged line may land beside a line of row `rowId`: not one of the rows it carries. */
    takes(source: string, rowId: string) {
      if (asked?.source !== source) asked = { source, rows: new Set(carry(source).map(lineRowId)) };
      return !asked.rows.has(rowId);
    },
    moved: linesMoved,
    rowOf: lineRowId,
  };
}

/** A view's lines in order: each group's rows under it where it is `grouped`, else its rows. */
export function viewLinesOf(groups: readonly { key: string; rows: readonly { row: { id: string } }[] }[], grouped: boolean) {
  return groups.flatMap((group) => group.rows.map(({ row }) => groupLine(grouped ? group.key : undefined, row.id)));
}

/**
 * The lines a Shift+press on `line` chooses from the one pressed before
 * (`anchor`), among a view's lines in order: those between them where both
 * stand in one group, else it alone, as Notion's grouped table (seen
 * 2026-11-24: 바 to 나 under y chose 바, 다 and 나; 나 under x, then 다 under
 * y, chose 다 alone besides).
 */
export function linesBetween(lines: readonly string[], anchor: string, line: string) {
  const from = lines.indexOf(anchor), to = lines.indexOf(line);
  return from < 0 || to < 0 || lineGroup(anchor) !== lineGroup(line) ? [line] : lines.slice(Math.min(from, to), Math.max(from, to) + 1);
}

/** Rows each as many times as a line of it is chosen (or it is, whole): what Notion's menu for several counts (seen 2026-11-24: 나 ticked twice, "2개의 단어"). */
export function rowsPerLine<Row extends { id: string }>(rows: readonly Row[], chosen: ReadonlySet<string>): Row[] {
  const lines = new Map<string, number>();
  for (const id of chosen) lines.set(lineRowId(id), (lines.get(lineRowId(id)) ?? 0) + 1);
  return rows.flatMap((row) => Array.from({ length: lines.get(row.id) ?? 0 }, () => row));
}
