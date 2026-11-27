import { type ReactNode, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { placeAround, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import styles from "./AutomationEditProperty.module.css";

/**
 * Where a desktop's "속성 편집" value opens: a window right under the "+ 새
 * 작업" that asked for it, or under its card, over the dialog rather than in
 * the card (Notion's, seen 2026-11-02, no gap 2026-11-10); too tall for either
 * side, it shows whole over its card (2026-11-10: a date's days, 442 high at
 * 28..470 over a card at 366..412 in a 779 window). From "+ 작업 추가" it
 * opens where the action list did, over it when the list opened over it
 * (2026-11-10: a text's 74 high window over the button with 164 under it).
 * Once open it keeps its side as it shrinks or grows, turning over only when
 * it no longer fits there (Notion's, seen 2026-11-11: a line's people window
 * over its field stays on it, its foot on the field, as a search shortens it).
 * A line's window is 260 wide, 1px out from its field's start; a card's and
 * "+ 작업 추가"'s 266, an option list's 260; "날짜 선택"'s days 248 (Notion's,
 * seen 2026-11-10 at 1975×779).
 */
export function AutomationValuePopover({ anchor, align, above, wide, line, onClose, children }: {
  anchor: DOMRect; align?: "end"; above?: boolean;
  /** A card's or "+ 작업 추가"'s value other than an option list's, 266 wide. */
  wide?: boolean;
  /** A line's field opened it: the window starts 1px out from the field's start. */
  line?: boolean;
  onClose(): void; children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // Notion's line window, 260 wide under a 259 wide field, ends where the field does.
  const out = line ? 1 : 0;
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(
    { top: anchor.top, bottom: anchor.bottom, left: anchor.left - out, right: anchor.right }, size, viewport,
    { gap: 0, align, whole: true, roomier: true, holdSide: true, ...(above ? { prefer: "above" as const } : {}) }),
    [anchor, align, above, out], undefined,
    { kind: "picker", onEscape: onClose, onOutsidePress: onClose, initialFocus: "first" });
  // The value takes the keys, not "←", as Notion's (seen 2026-11-02): its field, or a list's first line (2026-11-10).
  // Words wait for a press: their window takes the keys itself, the field unfocused (Notion's, seen 2026-11-10).
  // Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: "속성 편집" › a select, "z" went into its option
  // search (Leafen by code, not seen).
  useEffect(() => {
    if (!place) return;
    const first = ref.current?.querySelector<HTMLElement>("input:not([type=checkbox]):not([type=radio]), textarea, [role=menu] > [role^=menuitem]");
    (first ?? ref.current)?.focus({ preventScroll: true });
  }, [Boolean(place)]);
  if (typeof document === "undefined") return null;
  return createPortal(<div ref={ref} className={styles.popover} tabIndex={-1} data-automation-value-popover data-wide={wide ? "true" : undefined}
    style={popoverPlacementStyle(place)}>{children}</div>,
    document.body);
}
