import type { PointerEvent } from "react";
import { ArrowDown, ArrowUp, DragHandleIcon, FitRowHeightIcon } from "@/icons/leafen";
import { databaseViewLabels } from "./databaseViewLabels";
import widgetStyles from "./DatabaseDashboardWidgets.module.css";
import styles from "./DashboardRowSide.module.css";

/**
 * A row's buttons at its left in Edit mode, as Notion's (seen 2026-11-15 at
 * 1440, a row of each kind): centred on the row, its moves over "화면 높이에
 * 맞게 조정" 12 apart — one move a 24 round button, both joined in one 24×44
 * pill of two 20 round buttons, up over down — and its "드래그" a 24×30 pill
 * over their middle, shown under the pointer. A row fitted to its content
 * has no "화면 높이에 맞게 조정" (read 2026-11-15).
 */
export function DashboardRowSide({ rowIndex, steps, fitted, onDragStart, onMove, onFit }: {
  rowIndex: number;
  /** The neighbouring rows it can trade places with: -1 above, 1 below. */
  steps: readonly (-1 | 1)[];
  fitted: boolean;
  onDragStart(event: PointerEvent<HTMLButtonElement>): void;
  onMove(step: -1 | 1): void;
  onFit(): void;
}) {
  const labels = databaseViewLabels();
  const move = (step: -1 | 1, className: string) => (
    <button key={step} type="button" className={className} data-dashboard-row-move={step}
      aria-label={step < 0 ? labels.moveDashboardRowUp : labels.moveDashboardRowDown} onClick={() => onMove(step)}>
      {step < 0 ? <ArrowUp size="tiny" /> : <ArrowDown size="tiny" />}
    </button>
  );
  return (
    <div className={styles.side}>
      <button type="button" className={styles.drag} aria-label={labels.dragDashboardRow}
        data-dashboard-row-drag={rowIndex} onPointerDown={onDragStart}>
        <DragHandleIcon size="small" />
      </button>
      {steps.length === 2 ? (
        <span className={styles.movePair}>{steps.map((step) => move(step, styles.pairButton))}</span>
      ) : steps.map((step) => move(step, widgetStyles.roundButton))}
      {!fitted ? (
        <button type="button" className={widgetStyles.roundButton} aria-label={labels.fitDashboardRow} title={labels.fitDashboardRow}
          data-dashboard-row-fit={rowIndex} onClick={onFit}>
          <FitRowHeightIcon size="tiny" />
        </button>
      ) : null}
    </div>
  );
}
