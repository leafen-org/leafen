import { type RefObject, useLayoutEffect, useRef } from "react";
import { isBesideList } from "./besideListLeaving";
import { lessMotion } from "./sidePeekMotion";

/**
 * How a popover comes on as it opens, the first time usePopoverPlacement
 * places it, as Notion's popovers do (seen 2026-11-25 in Chrome at 1440, dpr
 * 2, two tabs, by their transitions and computed styles):
 * - "grow": one opened from something on the page — a button, a line typed
 *   in, a block's or a row's handle, a row of a window pressed — fades in
 *   and grows from 96% of its size, both over 200 ms, CSS `ease`, from where
 *   its opener is (`origin`): the page "⋯", the share window and "새로
 *   만들기 ⌄" under their button from their top end corner; "/", "@", a
 *   column's menu and the filter menu under their line or button from their
 *   top start corner, "/" over its line and a select option's window over
 *   its "⋯" from their foot start corner; a block's "⋮⋮" menu and a table
 *   row's beside their handle and a date window's "리마인더" list beside its
 *   row from their middle;
 * - "fade": a list beside a menu's row (besideListLeaving.ts: a block menu's
 *   "전환 ›", a page "⋯"'s "알림 받기 ›", a column menu's "정렬 ›", the
 *   selected text's "일반 텍스트 ›"), a menu opened where the pointer pressed
 *   (a table row's right click, placeAtPoint) and the icon picker only fade
 *   in, 200 ms `ease`;
 * - "panel": the selected text's panel fades in and grows from 98% about its
 *   middle over 100 ms `ease`, 30 ms after it is drawn;
 * - "none": a value's own editor over or under its cell (a select's, a
 *   date's) shows at once.
 * Every one takes the pointer and presses from its first frame. A hover tip
 * comes in its own way (IconTipButton.module.css), and with less motion asked
 * for none moves.
 */
export type PopoverAppear = "grow" | "fade" | "panel" | "none";

/** The point of a popover it grows from, as parts of its width and height from its top left. */
export type PopoverOrigin = { x: number; y: number };

const MIDDLE: PopoverOrigin = { x: 0.5, y: 0.5 };
const MOTION: Record<Exclude<PopoverAppear, "none">, { scale: number; duration: number; delay: number; origin?: PopoverOrigin }> = {
  grow: { scale: 0.96, duration: 200, delay: 0 },
  fade: { scale: 1, duration: 200, delay: 0 },
  panel: { scale: 0.98, duration: 100, delay: 30, origin: MIDDLE },
};

/**
 * Where a popover grows from, by the side it stands on and the edge it lines
 * up with: under its opener its top, over it its foot, at the start or end
 * edge of the opener's that corner (the start on the right where the screen
 * reads from the right), centred on it the middle; beside its opener its
 * middle; at a point worked out from a box its top start corner, which
 * stands there.
 */
export function popoverOrigin(side: string, edge: string | undefined, rtl: boolean): PopoverOrigin {
  const start = rtl ? 1 : 0;
  if (side === "point") return { x: start, y: 0 };
  if (side !== "below" && side !== "above") return MIDDLE;
  return { x: edge === "start" ? start : edge === "end" ? 1 - start : 0.5, y: side === "below" ? 0 : 1 };
}

function kindOf(root: HTMLElement, asked: PopoverAppear | undefined): PopoverAppear {
  if (asked) return asked;
  if (root.getAttribute("role") === "tooltip") return "none";
  return isBesideList(root) ? "fade" : "grow";
}

/**
 * Brings `root` on as its kind says (`asked`, else a list beside a menu's
 * row fading, a hover tip as it comes, any other growing), from `origin` of
 * `sized` — itself, or the part of it that has its size. One placed unseen,
 * held at no opacity till what it shows is known (accessHeldStyle), comes on
 * as it is shown. `grown` is told once it has grown to its size.
 */
export function popoverAppears(root: HTMLElement, sized: HTMLElement, asked: PopoverAppear | undefined, origin: PopoverOrigin | undefined,
  grown?: () => void) {
  const kind = kindOf(root, asked);
  if (kind === "none" || lessMotion() || typeof root.animate !== "function") return;
  const motion = MOTION[kind];
  const run = () => {
    const from = motion.origin ?? origin ?? MIDDLE;
    // By its own box as it grows to its rows still coming; by the sized part's within it as it stands now.
    const at = root.getBoundingClientRect(), box = sized.getBoundingClientRect();
    const transformOrigin = sized === root ? `${from.x * 100}% ${from.y * 100}%`
      : `${box.left - at.left + box.width * from.x}px ${box.top - at.top + box.height * from.y}px`;
    // A transform of its own (a place it is moved to) stays under the growing.
    const own = getComputedStyle(root).transform, base = own === "none" ? "" : own;
    const keyframes: Keyframe[] = motion.scale === 1 ? [{ opacity: 0 }, { opacity: 1 }]
      : [{ opacity: 0, transform: `${base} scale(${motion.scale})`.trim(), transformOrigin },
        { opacity: 1, transform: base || "none", transformOrigin }];
    const growing = root.animate(keyframes, { duration: motion.duration, delay: motion.delay, easing: "ease", fill: "backwards" });
    if (motion.scale !== 1 && grown) void growing.finished.then(grown, () => undefined);
  };
  if (Number(getComputedStyle(root).opacity) > 0) { run(); return; }
  const shown = new MutationObserver(() => {
    if (!root.isConnected) { shown.disconnect(); return; }
    if (Number(getComputedStyle(root).opacity) === 0) return;
    shown.disconnect();
    run();
  });
  shown.observe(root, { attributes: true, attributeFilter: ["style", "class"] });
  window.setTimeout(() => shown.disconnect(), 2000);
}

/**
 * A popover its own style places, not usePopoverPlacement (a menu drawn under
 * its button by CSS), comes on as the others do each time `open` turns true:
 * it grows from the side of its opener it stands on — `opener`, else the
 * control holding the focus as it opens —, from its middle where it stands
 * apart from it; `kind` where it comes on otherwise (the icon picker's fade).
 */
export function usePopoverAppear(open: boolean, ref: RefObject<HTMLElement | null>, opener?: () => Element | null | undefined, kind?: PopoverAppear) {
  const was = useRef(false), openerOf = useRef(opener);
  openerOf.current = opener;
  const focused = useRef<Element | null>(null);
  // The control holding the focus as it opens, read before what opens takes it.
  if (open && !was.current && typeof document !== "undefined") focused.current = document.activeElement;
  useLayoutEffect(() => {
    const opening = open && !was.current, root = ref.current;
    was.current = open;
    if (!opening || !root) return;
    const from = openerOf.current?.() ?? (focused.current && !root.contains(focused.current) ? focused.current : null);
    popoverAppears(root, root, kind, from ? originAgainst(root.getBoundingClientRect(), from.getBoundingClientRect()) : MIDDLE);
  });
}

// Its origin by where it stands against its opener's box: under it its top, over it its foot, lined up with an edge that corner.
function originAgainst(box: DOMRect, opener: DOMRect): PopoverOrigin {
  const y = box.top >= opener.bottom - 1 ? 0 : box.bottom <= opener.top + 1 ? 1 : 0.5;
  if (y === 0.5) return MIDDLE;
  return { x: Math.abs(box.left - opener.left) < 2 ? 0 : Math.abs(box.right - opener.right) < 2 ? 1 : 0.5, y };
}
