import { type CSSProperties, type MouseEvent, type PointerEventHandler, type ReactNode, type RefObject } from "react";
import { placeAround, placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, popoverRectAt, popoverRectOf, usePopoverPlacement,
  type PopoverViewport } from "@/lib/popoverPlacement";
import { keepLayerKeys, useOverlayLayer } from "@/lib/overlayLayer";
import { PopoverLayer } from "@/lib/popoverLayer";
import { accessHeldStyle, usePageAccessHold } from "@/lib/pageActionAccessContext";
import styles from "./RowMenu.module.css";

/**
 * How high a page's "…" menu may be: 80% of the window, its search scrolling
 * with its rows — Notion's, from the page's bar and from a database page's or
 * its peek's alike (seen 2026-11-16 twice: max-height 578.4 in a 723 high
 * window, 338.4 in a 423 one, where its 569 scrolled under it from 44).
 */
export function pageMenuCap(viewport: PopoverViewport) {
  return viewport.height * 0.8;
}

/**
 * How high a sidebar row's "…" menu may be: 70% of the window, the whole
 * menu scrolling — Notion's menus' own cap where one names none, as its
 * sidebar's does (its web client read 2026-11-16: a popup menu's maxHeight
 * defaults to 70% of the viewport; seen 506.1 on the menu's box in a 723
 * high window, 2026-11-16 twice).
 */
export function sidebarMenuCap(viewport: PopoverViewport) {
  return viewport.height * 0.7;
}

/**
 * A page's "…" menu's box and the backdrop under it (RowMenu), placed and
 * layered by their own state: opened at the pointer, it stays on screen as
 * it grows; from a bar's "…" it opens 8px under the button, its end on the
 * button's and 12px in from the window's at most, as Notion's (seen
 * 2026-11-01: top 44, right edge 1268 at 1280 and 548 at 560), and stays
 * under it, ending above the window's foot and scrolling inside: raised, it
 * went over the bar and lost its search (2026-11-05). A page's is at most
 * 80% of the window high (pageMenuCap), a sidebar row's 70% (sidebarMenuCap). Placing it, lifting it
 * into its layer and its settling draw this box alone, not the rows it holds,
 * which each drew again with React Aria's list of them (some 115 parts two or
 * three times after the menu was drawn, NAS condition, CPU ×2, 2026-11-13),
 * as a database row's menu keeps its box (DatabaseRowMenu). One drawn ahead
 * (TopBarPageMenu) takes no layer and no focus until it shows. Drawn on the
 * page's body, over whatever holds it (PopoverLayer): from a bar's "…" too,
 * over a panel docked under the bar.
 */
export function PlacedPageMenu({ menuRef, anchor, hangsFrom, ahead, onClose, initialFocus, backdropLabel, label, variant, accessOf, onPointerMove, after, children }: {
  menuRef: RefObject<HTMLDivElement | null>;
  anchor?: { x: number; y: number } | null;
  /** The bar's "…" it hangs from where it opens at no point. */
  hangsFrom: () => HTMLElement | null;
  ahead: boolean;
  onClose: () => void;
  initialFocus: RefObject<HTMLInputElement | null>;
  backdropLabel: string;
  label: string;
  variant: "page" | "sidebar";
  /** The page whose access its rows follow, while that access is unknown: it shows once they are known (usePageAccessHold). */
  accessOf?: string;
  onPointerMove?: PointerEventHandler<HTMLDivElement>;
  /** What opens beside the menu, outside its box. */
  after?: ReactNode;
  children: ReactNode;
}) {
  const cap = (viewport: PopoverViewport) => (variant === "page" ? pageMenuCap(viewport) : sidebarMenuCap(viewport));
  // Waiting for its rows' access, it is open and placed as it stands, unseen (usePageAccessHold).
  const held = usePageAccessHold(accessOf);
  const place = usePopoverPlacement(menuRef, anchor ? (size, viewport) => placeAtPoint(anchor, size, viewport, { cap: cap(viewport) })
    : (size, viewport) => placeAround(popoverRectOf(hangsFrom()) ?? popoverRectAt({ top: 0, bottom: 0, left: viewport.width }), size, viewport,
      { align: "end", gap: 8, keepSide: true, cap: cap(viewport) }), [anchor], undefined, false);
  const { zIndex } = useOverlayLayer(!ahead, { kind: "menu", floats: false, ref: menuRef, onEscape: onClose, onOutsidePress: onClose, initialFocus });
  const style = { ...(anchor ? { "--menu-x": `${anchor.x}px`, "--menu-y": `${anchor.y}px` } : {}), ...popoverPlacementStyle(place),
    ...(held ? accessHeldStyle : {}), zIndex } as CSSProperties;
  const close = (event: MouseEvent) => { event.stopPropagation(); onClose(); };
  return (
    <PopoverLayer>
      <button type="button" className={styles.backdrop} style={zIndex === undefined ? popoverBackdropStyle(place) : { zIndex: zIndex - 1 }} tabIndex={-1}
        aria-label={backdropLabel} onClick={close} />
      <div ref={menuRef} className={`${styles.menu} ${anchor ? styles.contextMenu : styles.hangingMenu}`} style={style} tabIndex={-1} aria-label={label}
        data-row-menu-variant={variant} onClick={(event) => event.stopPropagation()} onKeyDown={keepLayerKeys} onPointerMove={onPointerMove}>
        {children}
      </div>
      {after}
    </PopoverLayer>
  );
}
