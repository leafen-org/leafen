import { type CSSProperties, type RefObject, useLayoutEffect, useMemo } from "react";
import type { OverlayLayerOptions } from "@/lib/overlayLayer";
import { holdWheelForCaretMenu } from "@/lib/pageHoverShield";
import { placeAround, type PopoverPlacement, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import { inlineSign } from "@/lib/readingDirection";
import type { SlashMenuAnchor } from "./SlashMenu";

// Notion's menu (seen 2026-09-30/10-01): 324px, 32px rows of an icon and a
// name with the Markdown or keys that make it faint at the end, the chosen
// row's description in a dark tip beside the menu, "메뉴 닫기 esc" at its foot.
const MENU_WIDTH = 324;
const MENU_MAX_HEIGHT = 400;
// On a desktop window it is 40% of the window's height, 448px at most, as
// Notion's (seen 2026-11-01 at 290–1300px tall: 116, 240, 360, 448), under
// the "/" where that fits and else over it, however few its rows.
const desktopMenuHeight = (windowHeight: number) => Math.min(Math.round(windowHeight * 0.4), 448);
// It stands 12px under the "/" and 6.4px left of it, so the rows' marks start
// just right of the "/" (seen 2026-11-01), and ends 6.4px right of it where
// the screen reads from the right, whatever way the line reads (Notion's in
// Hebrew, seen 2026-11-09); a phone's menu keeps its own place.
export const MENU_GAP = 12;
const MENU_SHIFT = -6.4;
const PHONE_MENU_GAP = 6;
// Keep the template menu viewport on whole command rows. These values mirror
// the template menu's compact rows (EditorSlashMenu.module.css,
// [data-template-slash-menu]); the visual smoke measures both row height and
// bottom clipping so a future style change cannot drift silently.
const TEMPLATE_MENU_FIRST_ROW_OFFSET = 34;
const TEMPLATE_MENU_ROW_HEIGHT = 35;

/**
 * Where a "/" menu (`menuRef`) stands at its `anchor`: under the line, or over
 * it where only that side has room for its cap, else cut on the roomier side;
 * the template menu shows whole rows only. `dismiss` is its Escape and a press
 * outside it; `keys` are its list's (useCaretPicker). Without an anchor it is
 * not placed (a menu in its own place).
 */
export function useSlashMenuPlace(menuRef: RefObject<HTMLDivElement | null>, anchor: SlashMenuAnchor | undefined, { phone, templateMode, dismiss, keys }: {
  phone: boolean;
  templateMode: boolean;
  dismiss: () => void;
  keys: Pick<OverlayLayerOptions, "onCaretKeyDown" | "inside">;
}): { atCaret: boolean; place: PopoverPlacement | null; fixedStyle: CSSProperties | undefined } {
  const atCaret = !phone && !templateMode;
  const place = usePopoverPlacement(menuRef, anchor ? (size, viewport) =>
    placeAround(popoverRectAt(phone ? anchor : { get top() { return anchor.top; }, get bottom() { return anchor.bottom; },
      get left() { return anchor.left + MENU_SHIFT * inlineSign(menuRef.current); } }), size, viewport, { gap: phone ? PHONE_MENU_GAP : MENU_GAP,
      cap: phone ? MENU_MAX_HEIGHT : desktopMenuHeight(viewport.height), minHeight: 96,
      // A desktop menu chooses its side by its cap, however few its rows (lineSide).
      ...(phone || templateMode ? {} : { lineSide: desktopMenuHeight(viewport.height), marginY: 0 }),
      ...(templateMode ? { heightStep: { offset: TEMPLATE_MENU_FIRST_ROW_OFFSET, step: TEMPLATE_MENU_ROW_HEIGHT } } : {}) })
    : null, [anchor, phone, templateMode], undefined, { kind: "caret-menu", onEscape: dismiss,
      onOutsidePress: dismiss,
      // A menu at the caret: a press beside it closes it and goes on, the
      // page under it keeping the hover and only the wheel held back, as
      // Notion's (pageHoverShield.ts holdWheelForCaretMenu).
      pressGoesOn: atCaret,
      onCaretKeyDown: keys.onCaretKeyDown, inside: keys.inside });
  useLayoutEffect(() => {
    const menu = menuRef.current;
    return atCaret && place && menu ? holdWheelForCaretMenu(menu) : undefined;
  }, [atCaret, Boolean(place)]);
  const fixedStyle = useMemo<CSSProperties | undefined>(() => anchor ? {
    position: "fixed",
    width: MENU_WIDTH,
    ...popoverPlacementStyle(place),
    // The list scrolls inside; the menu keeps its foot row.
    overflowY: undefined,
  } : undefined, [anchor, place]);
  return { atCaret, place, fixedStyle };
}
