import { useEffect, useRef, useState, type CSSProperties, type PointerEvent } from "react";
import { stickyPad } from "./tableInteractionModel";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import tipStyles from "../IconTipButton.module.css";
import styles from "./TableFreezeEdge.module.css";
import { inlineEndX, inlineScrolled, inlineSign, inlineStartX, readsRightToLeft } from "@/lib/readingDirection";

const GRIP_HEIGHT = 28;

/**
 * The edge after a table's frozen columns, or before its first column, as
 * Notion's (seen 2026-11-03): an 8px strip down the rows that, pointed at,
 * shows a 2px line, a blue grip at the pointer and the tip "Drag
 * horizontally to freeze columns". Dragged sideways, it freezes the columns
 * up to the column edge nearest where it is let go, none before the first.
 * It sits at the head of the rows and moves as the row controls do, so it
 * sticks with frozen columns and scrolls with an unfrozen first one.
 */
export function TableFreezeEdge({ frozen, frozenWidth, stickyLeft, onFreeze }: {
  frozen: number;
  /** The frozen columns' width together. */
  frozenWidth: number;
  /** Where the row controls stick while columns are frozen. */
  stickyLeft?: CSSProperties["left"];
  onFreeze(count: number): void;
}) {
  const { t } = useTranslation("tableView");
  const holderRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(0);
  const [gripTop, setGripTop] = useState<number | null>(null);
  const [dragX, setDragX] = useState<number | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null);
  const [tipAnchor, setTipAnchor] = useState<PopoverRect | null>(null);
  const place = usePopoverPlacement(tipRef, tipAnchor ? () => ({ anchor: tipAnchor, placement: "right", gap: 14, options: {} }) : null,
    [tipAnchor], undefined, false);

  // As tall as the rows it stands at the head of, read where the page is laid
  // out already (a ResizeObserver's first answer comes once it is), never by
  // laying the page out in the task that drew it (2026-11-13: unlocking a
  // page laid out its tables in the press's frame for this edge, 151–156 ms).
  // It shows only once pointed at, by then measured.
  useEffect(() => {
    const rows = holderRef.current?.parentElement;
    if (!rows) return undefined;
    const observer = new ResizeObserver(([entry]) => {
      const box = entry?.borderBoxSize?.[0];
      setHeight(box ? box.blockSize : rows.getBoundingClientRect().height);
    });
    observer.observe(rows);
    return () => observer.disconnect();
  }, []);

  function point(clientY: number) {
    const strip = stripRef.current?.getBoundingClientRect();
    if (!strip) return;
    const top = Math.max(0, Math.min(strip.height - GRIP_HEIGHT, clientY - strip.top - GRIP_HEIGHT / 2));
    setGripTop(top);
    setTipAnchor({ left: strip.left, right: strip.right, top: strip.top + top, bottom: strip.top + top + GRIP_HEIGHT });
  }

  function leave() {
    if (dragX !== null) return;
    setGripTop(null);
    setTipAnchor(null);
  }

  function beginDrag(event: PointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return;
    event.preventDefault();
    const strip = event.currentTarget;
    strip.setPointerCapture(event.pointerId);
    scrollBackToColumns(strip, frozen, frozenWidth);
    const startX = event.clientX;
    setDragX(0);
    setTipAnchor(null);
    const move = (next: globalThis.PointerEvent) => {
      setDragX(next.clientX - startX);
      point(next.clientY);
    };
    const finish = (end: globalThis.PointerEvent) => {
      strip.removeEventListener("pointermove", move);
      strip.removeEventListener("pointerup", finish);
      strip.removeEventListener("pointercancel", finish);
      setDragX(null);
      setGripTop(null);
      if (end.type !== "pointerup") return;
      const count = nearestColumnEdge(strip, frozen, end.clientX);
      if (count !== null && count !== frozen) onFreeze(count);
    };
    strip.addEventListener("pointermove", move);
    strip.addEventListener("pointerup", finish);
    strip.addEventListener("pointercancel", finish);
  }

  const shown = gripTop !== null || dragX !== null;
  return (
    <div ref={holderRef} className={styles.holder} style={stickyLeft === undefined ? undefined : { ...stickyPad, position: "sticky", insetInlineStart: stickyLeft }}>
      <div className={styles.edge} data-shown={shown || undefined} data-dragging={dragX !== null || undefined}
        style={{ insetInlineStart: frozen > 0 ? `calc(var(--table-gutter-width) + ${frozenWidth - 1}px)` : "calc(var(--table-gutter-width) - 3px)",
          height, transform: `translateX(${dragX ?? 0}px)` }}>
        <div ref={stripRef} className={styles.strip} role="separator" aria-orientation="vertical" aria-label={t("freezeEdgeTip")}
          data-table-freeze-edge onPointerDown={beginDrag}
          onPointerMove={(event) => point(event.clientY)} onPointerLeave={leave}>
          <span className={styles.line} aria-hidden="true" />
          {gripTop !== null && <span className={styles.grip} style={{ top: gripTop }} aria-hidden="true" />}
        </div>
      </div>
      {tipAnchor && dragX === null && typeof document !== "undefined" ? createPortal(
        <span ref={tipRef} className={tipStyles.tip} style={popoverPlacementStyle(place)} role="tooltip">{t("freezeEdgeTip")}</span>,
        document.body,
      ) : null}
    </div>
  );
}

/**
 * Pressed, a table scrolled along goes back to its start, so the columns it
 * had under the frozen ones have edges to drop on: 8 short of where its first
 * column sits unscrolled, a smaller scroll staying as it is, as Notion's (seen
 * 2026-11-05: 600 → 96 with the column at 104; 40 stayed). A change from a
 * column's menu leaves the scroll alone there too.
 */
function scrollBackToColumns(from: HTMLElement, frozen: number, frozenWidth: number) {
  const scroller = from.closest<HTMLElement>("[data-table-scroll]");
  const unfrozen = scroller?.querySelectorAll<HTMLElement>("[data-table-head] > [role='columnheader'][data-property-id]")[frozen];
  if (!scroller || !unfrozen) return;
  // Counted from the table's start, as far along as it is scrolled (lib/readingDirection).
  const sign = inlineSign(scroller), scrolled = inlineScrolled(scroller);
  const start = (inlineStartX(unfrozen.getBoundingClientRect(), 0, scroller) - inlineStartX(scroller.getBoundingClientRect(), 0, scroller)) * sign + scrolled - frozenWidth;
  scroller.scrollLeft = sign * Math.min(scrolled, Math.max(0, start - 8));
}

/**
 * How many columns end at or before the shown column edge nearest x: the
 * first column's start edge or a column's end one (its left and right, the
 * other way round where the table reads from the right). A column scrolled
 * under the frozen ones has no edge to find.
 */
function nearestColumnEdge(from: HTMLElement, frozen: number, x: number) {
  const scroller = from.closest<HTMLElement>("[data-table-scroll]");
  const head = scroller?.querySelector("[data-table-head]");
  if (!scroller || !head) return null;
  const cells = Array.from(head.querySelectorAll<HTMLElement>(":scope > [role='columnheader'][data-property-id]")).map((cell) => cell.getBoundingClientRect());
  if (cells.length === 0) return null;
  const box = scroller.getBoundingClientRect();
  const rtl = readsRightToLeft(scroller), sign = rtl ? -1 : 1;
  const frozenEnd = frozen > 0 ? inlineEndX(cells[frozen - 1], 0, scroller) : -Infinity * sign;
  const edges = [{ count: 0, at: inlineStartX(cells[0], 0, scroller) }, ...cells.map((cell, index) => ({ count: index + 1, at: inlineEndX(cell, 0, scroller) }))]
    .filter(({ count, at }) => (count <= frozen || (at - frozenEnd) * sign > 0) && at >= box.left && at <= box.right);
  return edges.reduce<{ count: number; at: number } | null>((best, edge) =>
    !best || Math.abs(edge.at - x) < Math.abs(best.at - x) ? edge : best, null)?.count ?? null;
}
