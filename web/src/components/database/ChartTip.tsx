import { useLayoutEffect, useRef, useState } from "react";
import { i18next } from "@/i18n";
import { ListIcon } from "@/icons/leafen";
import chartStyles from "./chartView.module.css";

/** One line of a chart's tip: its group's swatch, name and value. */
export type ChartTipRow = { key: string; label: string; value: string; fill: string; shade?: number };

/**
 * Where a tip goes by the point it tells of, in the chart's own pixels:
 * "above" it, its middle over the point and its foot `gap` over it, or
 * below where there is no room; "side", to its left with its middle at
 * the point, or to its right where there is no room.
 */
export type ChartTipAnchor = { x: number; y: number; place: "above" | "side"; gap: number };

/**
 * The tip over a chart's hovered group, as Notion's (seen 2026-11-11, an
 * HTML box over its chart): 8px corners on the menu's color with a 1px
 * ring; the group's swatch (8px, 3px in), its name and its value in 12px
 * lines 18 high, 10px 12px around — or, when its Y axis stacks, its name
 * over each stacked group's line, 12px around; then, where the group's rows
 * open, a line and "☰ 데이터를 보려면 클릭하세요." 12px in. It keeps
 * inside the chart.
 */
export function ChartTip({ anchor, title, rows, opens, width, height }: {
  anchor: ChartTipAnchor; title?: string; rows: readonly ChartTipRow[]; opens: boolean; width: number; height: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null);
  useLayoutEffect(() => {
    const box = ref.current;
    if (!box) return;
    const w = box.offsetWidth, h = box.offsetHeight;
    let left: number, top: number;
    if (anchor.place === "above") {
      left = anchor.x - w / 2;
      top = anchor.y - anchor.gap - h;
      if (top < 0) top = anchor.y + anchor.gap;
    } else {
      left = anchor.x - anchor.gap - w;
      if (left < 0) left = anchor.x + anchor.gap;
      top = anchor.y - h / 2;
    }
    left = Math.max(0, Math.min(width - w, left));
    // The chart's pixels run from its left; a right-to-left page counts its start from the right.
    const next = { left: getComputedStyle(box).direction === "rtl" ? width - w - left : left, top: Math.max(0, Math.min(height - h, top)) };
    setPlace((current) => current && current.left === next.left && current.top === next.top ? current : next);
  }, [anchor.x, anchor.y, anchor.place, anchor.gap, width, height, title, rows]);
  return (
    <div ref={ref} className={chartStyles.tip} role="tooltip" data-chart-tip
      style={place ? { insetInlineStart: place.left, top: place.top } : { visibility: "hidden" }}>
      <div className={title === undefined ? chartStyles.tipBody : chartStyles.tipBodyTitled}>
        {title !== undefined && <div className={chartStyles.tipTitle}>{title}</div>}
        {rows.map((row) => (
          <div key={row.key} className={chartStyles.tipRow}>
            <span className={chartStyles.tipName}>
              <span className={chartStyles.tipSwatch} style={{ background: row.fill, opacity: row.shade }} aria-hidden="true" />
              <span>{row.label}</span>
            </span>
            <span className={chartStyles.tipValue}>{row.value}</span>
          </div>
        ))}
      </div>
      {opens && <>
        <hr className={chartStyles.tipRule} />
        <div className={chartStyles.tipHint}>
          <ListIcon pixels={14} aria-hidden="true" />
          <span>{i18next.t("chartView:clickToView")}</span>
        </div>
      </>}
    </div>
  );
}
