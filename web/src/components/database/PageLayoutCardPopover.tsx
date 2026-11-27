import { useLayoutEffect, useRef, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import styles from "./PageLayoutCardPopover.module.css";

const GAP = 8;

/**
 * A layout card's settings, open beside the card while it is chosen, as
 * Notion's (seen 2026-11-01): 8px after it with its top on the card's, or
 * before it where the builder's canvas has no room after it — a panel
 * card's opens on its left, clear of the page settings (seen 2026-11-12) —
 * kept in the window. The builder's right column keeps the page's settings
 * meanwhile. The content tab's opens centred 22px under it. Its first move
 * takes the focus as it opens, unless a field in it has, so its first Escape
 * closes it wherever it was opened from — a card's press or a property just
 * added from a "+" menu — and the keyboard starts from that move, marked
 * while the keyboard is in use (seen 2026-11-12: Notion's focus goes into its
 * menu, "페이지로 옮기기" ringed after a key). A press outside it or Escape
 * closes it, the card no longer chosen.
 */
export function PageLayoutCardPopover({ card, under = false, menu = false, label, children, onClose }: {
  card: HTMLElement | null;
  under?: boolean;
  /**
   * A card without settings of its own, its moves alone: a menu, as Notion's
   * (seen 2026-11-12: "아래로 이동 · 페이지 토론 숨기기" for "페이지"). The
   * menu itself takes the focus as it opens, no move marked, and ↓ ↑ go
   * through its moves, round from the last to the first.
   */
  menu?: boolean;
  label: string;
  children: ReactNode;
  onClose(): void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const placed = popoverPlacementStyle(usePopoverPlacement(ref, card ? (size, viewport) => {
    const anchor: PopoverRect = popoverRectOf(card) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    return under ? { anchor, placement: "bottom", gap: 22, options: {} }
      : placeBeside(anchor, size, viewport, { gap: GAP, prefer: roomAfterInCanvas(card, size.width) ? "right" : "left" });
  } : null, [card, under], undefined, { kind: "picker", restoreFocus: false, onEscape: onClose, onOutsidePress: onClose, inside: () => [card] }));
  // Before a "+" menu closing under it gives the focus back to its "+".
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root || root.contains(document.activeElement)) return;
    (menu ? root : root.querySelector<HTMLElement>("[data-layout-module-action]") ?? root).focus({ preventScroll: true });
    // Opened as it is, a menu or not.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const step = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!menu || (event.key !== "ArrowDown" && event.key !== "ArrowUp")) return;
    const moves = [...event.currentTarget.querySelectorAll<HTMLElement>("[data-layout-module-action]")];
    if (moves.length === 0) return;
    event.preventDefault();
    const at = moves.indexOf(document.activeElement as HTMLElement);
    const next = event.key === "ArrowDown" ? (at + 1) % moves.length : at <= 0 ? moves.length - 1 : at - 1;
    moves[next].focus({ preventScroll: true });
  };
  return createPortal(
    <div ref={ref} role={menu ? "menu" : "dialog"} aria-label={label} tabIndex={-1} className={styles.popover} style={placed}
      onKeyDown={step} data-layout-card-popover={menu ? "menu" : ""}>
      {children}
    </div>,
    document.body,
  );
}

/** Whether a popover this wide fits after the card, the way the screen reads, inside the builder's canvas. */
export function roomAfterInCanvas(card: HTMLElement, width: number) {
  const canvas = card.closest("[data-layout-canvas]")?.getBoundingClientRect();
  if (!canvas) return true;
  const at = card.getBoundingClientRect();
  const room = readsRightToLeft(card) ? at.left - canvas.left : canvas.right - at.right;
  return room >= width + GAP;
}
