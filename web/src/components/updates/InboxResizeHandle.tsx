import { useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { clampInboxWidth, INBOX_MAX_WIDTH, INBOX_MIN_WIDTH, INBOX_WIDTH_STEP } from "./inboxDock";
import styles from "../UpdatesPanelChrome.module.css";
import tipStyles from "../IconTipButton.module.css";
import { inlineSign, inlineStartKey, readsRightToLeft } from "@/lib/readingDirection";

// A press that moves less than this is a click: it closes the inbox.
const CLICK_SLOP = 3;
// The tip stands 10px past the pointer, its middle at the pointer's height (Notion's, seen 2026-11-13).
const TIP_GAP = 10;
const TIP_MARGIN = 8;

/**
 * The inbox's right edge, as Notion's (seen 2026-10-07, 2026-11-13): a thin
 * line between the inbox and what lies beside it, twice as thick and darker
 * under the pointer; dragged, it makes the inbox wider or narrower, clicked
 * it closes it, and pointed at it says so in a tip by the pointer. The left
 * and right arrow keys move it 10px.
 */
export function InboxResizeHandle({ width, onWidthChange, onClose }: {
  width: number;
  onWidthChange: (width: number, keep: boolean) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("updatesPanel");
  const [tip, setTip] = useState<{ x: number; y: number; rtl: boolean } | null>(null);

  function startDrag(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0) return;
    event.preventDefault();
    setTip(null);
    const startX = event.clientX;
    // Away from the sidebar it stands beside widens it, rightward left to right.
    const sign = inlineSign(event.currentTarget);
    let next = width;
    let moved = false;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    const move = (pointer: PointerEvent) => {
      if (!moved && Math.abs(pointer.clientX - startX) < CLICK_SLOP) return;
      moved = true;
      next = clampInboxWidth(width + (pointer.clientX - startX) * sign);
      onWidthChange(next, false);
    };
    const end = (pointer: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      if (moved) onWidthChange(next, true);
      else if (pointer.type === "pointerup") onClose();
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
  }

  function onKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    onWidthChange(clampInboxWidth(width + (event.key === inlineStartKey(event.currentTarget) ? -1 : 1) * INBOX_WIDTH_STEP), true);
  }

  return (
    <>
      <div className={styles.resizeHandle} role="separator" aria-orientation="vertical" tabIndex={0}
        aria-label={t("inbox.resize")} aria-valuenow={width} aria-valuemin={INBOX_MIN_WIDTH} aria-valuemax={INBOX_MAX_WIDTH}
        data-inbox-resize onPointerDown={startDrag} onKeyDown={onKeyDown}
        // Where the pointer comes onto the edge the tip stays, as Notion's does.
        onPointerEnter={(event) => {
          if (event.pointerType !== "touch" && event.buttons === 0) {
            setTip({ x: event.clientX, y: event.clientY, rtl: readsRightToLeft(event.currentTarget) });
          }
        }}
        onPointerLeave={() => setTip(null)} />
      {tip ? createPortal(<EdgeTip {...tip} />, document.body) : null}
    </>
  );
}

function EdgeTip({ x, y, rtl }: { x: number; y: number; rtl: boolean }) {
  const { t } = useTranslation("updatesPanel");
  const dim = { dim: <span className={tipStyles.tipNote} /> };
  return (
    <span role="tooltip" className={tipStyles.tip} data-inbox-edge-tip
      ref={(node) => {
        if (!node) return;
        const box = node.getBoundingClientRect();
        const left = rtl ? x - TIP_GAP - box.width : x + TIP_GAP;
        const top = Math.min(Math.max(TIP_MARGIN, y - box.height / 2), window.innerHeight - TIP_MARGIN - box.height);
        node.style.left = `${Math.round(left)}px`;
        node.style.top = `${Math.round(top)}px`;
        node.style.visibility = "";
      }}
      style={{ visibility: "hidden" }}>
      <span className={tipStyles.tipLine}><Trans t={t} i18nKey="inbox.edgeTip.close" components={dim} /></span>
      <span className={tipStyles.tipLine}><Trans t={t} i18nKey="inbox.edgeTip.resize" components={dim} /></span>
    </span>
  );
}
