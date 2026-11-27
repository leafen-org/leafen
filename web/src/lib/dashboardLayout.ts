import type { GraphView } from "@/contentGraph";

// A dashboard view keeps its widget IDs row by row in dashboardRows, each row's
// height in pixels in dashboardRowHeights (0 is the default height, -1 its
// widgets' content) and each widget's share of its row in
// dashboardWidgetWidths. The sizes follow the shape of the rows; the server
// rejects any other shape.

/** A view shown as a widget inside a dashboard view, not as a tab. */
export function isDashboardWidget(view: GraphView) {
  return typeof view.config.dashboardViewId === "string" && view.config.dashboardViewId !== "";
}

export const DASHBOARD_WIDGETS_PER_ROW = 4;
export const DASHBOARD_WIDGETS = 12;
/** A row's height: 320 by default, 240–2140 dragged, as Notion's (read 2026-11-15: a fixed row 240–2140, default 320). */
export const DASHBOARD_ROW_HEIGHT = { standard: 320, min: 240, max: 2140 };
/**
 * A row "화면 높이에 맞게 조정" fitted: it follows its widgets' content, as
 * Notion's "Snap to view height" (read 2026-11-15: the row's heightConfig
 * becomes "content" and its widgets 240–2140 high), until it is dragged again.
 */
export const DASHBOARD_ROW_FITS_CONTENT = -1;

export type DashboardLayoutRow = { height: number; widgets: { id: string; width: number }[] };
export type DashboardLayout = DashboardLayoutRow[];
/** A new row at rowIndex, or a place in the row at rowIndex (the end when index is absent). */
export type DashboardPlacement = { rowIndex: number; newRow: boolean; index?: number };

function numberAt(values: unknown, index: number): number | undefined {
  const value = Array.isArray(values) ? values[index] : undefined;
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

/** The dashboard's rows, keeping only the widgets `live` accepts. */
export function dashboardLayoutOf(dashboard: GraphView, live: (id: string) => boolean = () => true): DashboardLayout {
  const rows = dashboard.config.dashboardRows;
  const heights = dashboard.config.dashboardRowHeights;
  const widths = dashboard.config.dashboardWidgetWidths;
  if (!Array.isArray(rows)) return [];
  return rows.flatMap((row: unknown, rowIndex) => {
    if (!Array.isArray(row)) return [];
    const rowWidths = Array.isArray(widths) ? widths[rowIndex] : undefined;
    const widgets = row.flatMap((id: unknown, index) => typeof id === "string" && id !== "" && live(id)
      ? [{ id, width: numberAt(rowWidths, index) ?? 1 }]
      : []);
    return widgets.length ? [{ height: numberAt(heights, rowIndex) ?? 0, widgets }] : [];
  });
}

export function dashboardWidgetCount(layout: DashboardLayout) {
  return layout.reduce((count, row) => count + row.widgets.length, 0);
}

export function withDashboardLayout(dashboard: GraphView, layout: DashboardLayout): GraphView {
  const config = { ...dashboard.config };
  delete config.dashboardRows;
  delete config.dashboardRowHeights;
  delete config.dashboardWidgetWidths;
  if (layout.length) {
    config.dashboardRows = layout.map((row) => row.widgets.map(({ id }) => id));
    if (layout.some((row) => row.height !== 0)) config.dashboardRowHeights = layout.map((row) => row.height);
    if (layout.some((row) => row.widgets.some(({ width }) => width !== 1))) {
      config.dashboardWidgetWidths = layout.map((row) => row.widgets.map(({ width }) => width));
    }
  }
  return { ...dashboard, config };
}

function cloneLayout(layout: DashboardLayout): DashboardLayout {
  return layout.map((row) => ({ height: row.height, widgets: row.widgets.map((widget) => ({ ...widget })) }));
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

/** Places a new widget; null when the dashboard or the row is full. */
export function placeDashboardWidget(layout: DashboardLayout, id: string, placement: DashboardPlacement): DashboardLayout | null {
  if (dashboardWidgetCount(layout) >= DASHBOARD_WIDGETS) return null;
  const next = cloneLayout(layout);
  if (placement.newRow) {
    next.splice(clamp(placement.rowIndex, 0, next.length), 0, { height: 0, widgets: [{ id, width: 1 }] });
    return next;
  }
  const row = next[placement.rowIndex];
  if (!row || row.widgets.length >= DASHBOARD_WIDGETS_PER_ROW) return null;
  row.widgets.splice(clamp(placement.index ?? row.widgets.length, 0, row.widgets.length), 0, { id, width: 1 });
  return next;
}

/** Takes a widget out, and an emptied row with it. */
export function removeDashboardWidget(layout: DashboardLayout, id: string): DashboardLayout {
  return cloneLayout(layout)
    .map((row) => ({ ...row, widgets: row.widgets.filter((widget) => widget.id !== id) }))
    .filter((row) => row.widgets.length > 0);
}

/** Moves a widget within its row or to another row; null when the target row is full. */
export function moveDashboardWidget(layout: DashboardLayout, id: string, placement: DashboardPlacement): DashboardLayout | null {
  const rowIndex = layout.findIndex((row) => row.widgets.some((widget) => widget.id === id));
  if (rowIndex < 0) return null;
  const index = layout[rowIndex].widgets.findIndex((widget) => widget.id === id);
  if (!placement.newRow && placement.rowIndex === rowIndex) {
    const next = cloneLayout(layout);
    const [moved] = next[rowIndex].widgets.splice(index, 1);
    const target = clamp(placement.index ?? next[rowIndex].widgets.length + 1, 0, next[rowIndex].widgets.length + 1);
    next[rowIndex].widgets.splice(target > index ? target - 1 : target, 0, moved);
    return next;
  }
  // The widget leaves its row; an emptied row before the target shifts it up.
  const emptied = layout[rowIndex].widgets.length === 1;
  const targetRow = emptied && rowIndex < placement.rowIndex ? placement.rowIndex - 1 : placement.rowIndex;
  return placeDashboardWidget(removeDashboardWidget(layout, id), id, { ...placement, rowIndex: targetRow });
}

/** Moves a whole row up (-1) or down (1); null at the end it would leave. */
export function moveDashboardRow(layout: DashboardLayout, rowIndex: number, step: -1 | 1): DashboardLayout | null {
  const target = rowIndex + step;
  if (!layout[rowIndex] || !layout[target]) return null;
  const next = cloneLayout(layout);
  [next[rowIndex], next[target]] = [next[target], next[rowIndex]];
  return next;
}

/**
 * A row is twelve columns, as Notion's: a divider between two widgets moves
 * by whole columns, and each widget keeps at least the columns 240px takes
 * of the row, two at the least (read 2026-11-15: minColumns max(2, ceil(240
 * / rowWidth × 12)) — at a 704 row five, a widget 287 wide at the least).
 */
const ROW_COLUMNS = 12;
const WIDGET_MIN_PX = 240;

export function dashboardMinColumns(rowWidth: number) {
  return rowWidth > 0 ? Math.max(2, Math.ceil(WIDGET_MIN_PX / rowWidth * ROW_COLUMNS)) : 2;
}

/**
 * How many widgets a line of a row holds: as many 240px widgets with their
 * 12px gaps as its width takes, one at the least, as Notion's (read
 * 2026-11-15: floor((width + 12) / 252); seen 2026-11-15 at a 704 row: three
 * widgets drawn two to a line, the third alone under them, 16 apart).
 */
export function dashboardWidgetsPerLine(rowWidth: number) {
  return rowWidth > 0 ? Math.max(1, Math.floor((rowWidth + 12) / (WIDGET_MIN_PX + 12))) : DASHBOARD_WIDGETS_PER_ROW;
}

/**
 * The line of its row a widget is drawn on — full lines first, the rest on
 * the last — and its columns of that line's twelve, the line's widgets
 * sharing them as a row of their own does (seen 2026-11-15: a wrapped 704
 * row's two at calc(50% − 6px) each, the one under them at 100%).
 */
export function dashboardWidgetLine(row: DashboardLayoutRow, index: number, perLine: number, minColumns: number) {
  const start = index - index % Math.max(1, perLine), end = Math.min(row.widgets.length, start + Math.max(1, perLine));
  const columns = dashboardShownColumns({ ...row, widgets: row.widgets.slice(start, end) }, minColumns);
  return { start, end, columns: columns[index - start], before: columns.slice(0, index - start).reduce((sum, value) => sum + value, 0) };
}

/**
 * A widget's box in its row, a wrapping flex row 16 between its lines and
 * 12 between widgets: its columns of its line, the gaps shared alike —
 * Notion's calc(columns / 12 − 12(n − 1) / n) of the n on its line (read
 * 2026-11-15), which fills the line, its grow then idle — and the row's
 * height, each line's own; a row fitted to its content sets none.
 */
export function dashboardWidgetBox(row: DashboardLayoutRow, index: number, perLine: number, minColumns: number) {
  const { start, end, columns } = dashboardWidgetLine(row, index, perLine, minColumns);
  return {
    flexGrow: row.widgets[index].width,
    flexBasis: `calc(${columns / ROW_COLUMNS * 100}% - ${12 * (end - start - 1) / (end - start)}px)`,
    height: row.height === DASHBOARD_ROW_FITS_CONTENT ? undefined : row.height || DASHBOARD_ROW_HEIGHT.standard,
  };
}

/** Each widget's whole columns of its row's twelve, its shares rounded so they still make twelve. */
export function dashboardRowColumns(row: DashboardLayoutRow): number[] {
  const total = row.widgets.reduce((sum, { width }) => sum + width, 0) || row.widgets.length;
  const exact = row.widgets.map(({ width }) => width / total * ROW_COLUMNS);
  const columns = exact.map(Math.floor);
  const order = exact.map((value, index) => [value - columns[index], index]).sort((a, b) => b[0] - a[0]);
  for (let left = ROW_COLUMNS - columns.reduce((sum, value) => sum + value, 0), at = 0; left > 0; left -= 1, at += 1) {
    columns[order[at % order.length][1]] += 1;
  }
  return columns;
}

/**
 * The columns a row's widgets show where each keeps `minColumns`, as Notion
 * draws a row narrower than its widths were set at (read 2026-11-15: snapped
 * to whole columns with the minimum kept, else 12/n each): a narrow one takes
 * what it lacks from the widest.
 */
export function dashboardShownColumns(row: DashboardLayoutRow, minColumns: number): number[] {
  const columns = dashboardRowColumns(row);
  if (columns.length * minColumns > ROW_COLUMNS) return columns.map(() => Math.floor(ROW_COLUMNS / columns.length));
  let lacking = 0;
  const shown = columns.map((value) => value < minColumns ? (lacking += minColumns - value, minColumns) : value);
  for (; lacking > 0; lacking -= 1) shown[shown.indexOf(Math.max(...shown))] -= 1;
  return shown;
}

/**
 * Moves the divider after the widget at leftIndex to `column` of its row's
 * twelve, to the nearest whole column, from the columns its widgets show,
 * each of the two widgets keeping `minColumns`; two that together have less
 * than twice that stay as they are. In a row wrapped into lines of
 * `perLine` the divider moves within its line, the two widgets sharing
 * their part of the row as before; there is none between two lines.
 */
export function placeDashboardDivider(
  layout: DashboardLayout, rowIndex: number, leftIndex: number, column: number, minColumns: number, perLine = DASHBOARD_WIDGETS_PER_ROW,
): DashboardLayout {
  const row = layout[rowIndex];
  if (!row?.widgets[leftIndex + 1] || !Number.isFinite(column)) return layout;
  const { start, end } = dashboardWidgetLine(row, leftIndex, perLine, minColumns);
  if (leftIndex + 1 >= end) return layout;
  if (end - start < row.widgets.length) {
    const line = [{ ...row, widgets: row.widgets.slice(start, end) }];
    const placed = placeDashboardDivider(line, 0, leftIndex - start, column, minColumns);
    if (placed === line) return layout;
    // The line's widgets keep their part of the row: their shares scaled to what they had together.
    const scale = line[0].widgets.reduce((sum, { width }) => sum + width, 0) / line[0].widgets.length;
    const next = cloneLayout(layout);
    placed[0].widgets.forEach(({ width }, offset) => { next[rowIndex].widgets[start + offset].width = Math.round(width * scale * 1000) / 1000; });
    return next;
  }
  const columns = dashboardShownColumns(row, minColumns);
  const before = columns.slice(0, leftIndex).reduce((sum, value) => sum + value, 0);
  const pair = columns[leftIndex] + columns[leftIndex + 1];
  if (pair < 2 * minColumns) return layout;
  const left = clamp(Math.round(column - before), minColumns, pair - minColumns);
  columns[leftIndex] = left;
  columns[leftIndex + 1] = pair - left;
  const next = cloneLayout(layout);
  // Shares of the row: a widget of 12/n columns keeps 1.
  next[rowIndex].widgets.forEach((widget, index) => {
    widget.width = Math.round(columns[index] * row.widgets.length / ROW_COLUMNS * 1000) / 1000;
  });
  return next;
}

export function resizeDashboardRow(layout: DashboardLayout, rowIndex: number, height: number): DashboardLayout {
  if (!layout[rowIndex] || !Number.isFinite(height)) return layout;
  const next = cloneLayout(layout);
  next[rowIndex].height = height === DASHBOARD_ROW_FITS_CONTENT ? height
    : Math.round(clamp(height, DASHBOARD_ROW_HEIGHT.min, DASHBOARD_ROW_HEIGHT.max));
  return next;
}
