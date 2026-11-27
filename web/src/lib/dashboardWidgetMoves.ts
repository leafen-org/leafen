import { moveDashboardWidget, type DashboardLayout } from "@/lib/dashboardLayout";

type Step = -1 | 1;

/**
 * A move a widget's options offer in Edit mode, as Notion's (seen 2026-11-15
 * in the Business trial): along its row, one place before or after it
 * ("왼쪽으로 이동" / "오른쪽으로 이동", while it has a neighbour that side);
 * into the row above or below ("위 행으로 이동" / "아래 행으로 이동", while that
 * row has room); or out to a new row of its own above or below ("위에 새 행
 * 생성하기" / "아래에 새 행 생성하기", while its row holds another widget).
 */
export type DashboardWidgetMove = { kind: "along" | "row" | "newRow"; step: Step };

const STEPS: readonly Step[] = [-1, 1];

/** The moves a widget can make now, in its menu's order. */
export function dashboardWidgetMoves(layout: DashboardLayout, id: string): DashboardWidgetMove[] {
  const rowIndex = layout.findIndex((row) => row.widgets.some((widget) => widget.id === id));
  if (rowIndex < 0) return [];
  const row = layout[rowIndex].widgets, index = row.findIndex((widget) => widget.id === id);
  const along = STEPS.filter((step) => row[index + step]).map((step): DashboardWidgetMove => ({ kind: "along", step }));
  const rows = STEPS.filter((step) => layout[rowIndex + step] && moveDashboardWidget(layout, id, { rowIndex: rowIndex + step, newRow: false }))
    .map((step): DashboardWidgetMove => ({ kind: "row", step }));
  const newRows = row.length > 1 ? STEPS.map((step): DashboardWidgetMove => ({ kind: "newRow", step })) : [];
  return [...along, ...rows, ...newRows];
}

/** The layout after a move; null where it can't be made. */
export function moveDashboardWidgetBy(layout: DashboardLayout, id: string, move: DashboardWidgetMove): DashboardLayout | null {
  const rowIndex = layout.findIndex((row) => row.widgets.some((widget) => widget.id === id));
  if (rowIndex < 0) return null;
  if (move.kind === "row") return moveDashboardWidget(layout, id, { rowIndex: rowIndex + move.step, newRow: false });
  if (move.kind === "newRow") return moveDashboardWidget(layout, id, { rowIndex: move.step < 0 ? rowIndex : rowIndex + 1, newRow: true });
  // Within the row, an index past the widget counts the place it leaves.
  const index = layout[rowIndex].widgets.findIndex((widget) => widget.id === id);
  return moveDashboardWidget(layout, id, { rowIndex, newRow: false, index: move.step < 0 ? index - 1 : index + 2 });
}
