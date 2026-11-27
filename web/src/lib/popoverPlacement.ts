import { computePosition, flip, offset, shift, size as floatingSize, platform, type Middleware, type Placement, type VirtualElement } from "@floating-ui/dom";
import { type CSSProperties, type DependencyList, type RefObject, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { type OverlayLayerOptions, useOverlayLayer } from "./overlayLayer";
import { PHONE_TOOLS_FOOT_GAP, phoneTools } from "./phoneTools";
import { placeAgainOn } from "./popoverPlaceAgain";
import { popoverAppears, popoverOrigin, usePopoverAppear } from "./popoverAppear";
import { usePopoverDrawnAnew } from "./popoverDrawnAnew";
import { popoverAnchorOf, popoverRectOf, type PopoverAnchor } from "./popoverAnchor";
import { naturalSize } from "./popoverNaturalSize";
import { pressedBox } from "./pressedPoint";
import { readsRightToLeft } from "./readingDirection";
import { keyboardPush, keyboardShortening, softKeyboardShown } from "./softKeyboard";
import type { PlaceAroundOptions, PlaceBesideOptions, PopoverOptions, PopoverPlacement, PopoverRect, PopoverRequest, PopoverSide, PopoverSize,
  PopoverViewport } from "./popoverRequest";
export type { PlaceAroundOptions, PlaceBesideOptions, PopoverPlacement, PopoverRect, PopoverRequest, PopoverSide, PopoverSize, PopoverViewport };

// How far a popover keeps from the window's edges, as Notion's (seen
// 2026-11-01; CSS widths use the same --popover-edge).
export const POPOVER_MARGIN = 12;

/** The top of what shows of the page (the visual viewport) where boxes are
 * measured from: iPhone Safari pans it in the window as it scrolls to a field,
 * Android Chrome's window shrinks with the keyboard instead (NativePhoneToolbar.tsx). */
function shownTop() {
  const shown = window.visualViewport;
  return shown ? document.documentElement.getBoundingClientRect().top + shown.pageTop : 0;
}
/** The foot of what shows of the page (the visual viewport), measured as boxes are. */
function shownFoot() {
  const shown = window.visualViewport;
  return shown ? shownTop() + shown.height : window.innerHeight;
}
/** Where a phone's editing tools stand while they show, measured as boxes
 * are: they are placed as fixed boxes, from the page's top, which iOS pans the
 * visual viewport over. They follow the line, under it, and stand at the foot
 * of what shows where the line is low (NativePhoneToolbar.tsx). */
function phoneToolsShown() {
  const tools = phoneTools();
  if (!tools) return null;
  const shift = (window.visualViewport?.offsetTop ?? 0) - shownTop();
  return { top: tools.top - shift, bottom: tools.bottom - shift, onKeyboard: softKeyboardShown() };
}
/** The room its callers choose a side and cap by: the window's width, and down
 * to the foot of what shows of the page (the visual viewport, over a phone's
 * keyboard), or of its editing tools where they stand at that foot over the
 * keyboard (2026-11-09: the ":" emoji list chose "under" by the window's
 * height, 313 under its line where 174 showed). */
function popoverViewport(): PopoverViewport {
  if (!window.visualViewport) return { width: window.innerWidth, height: window.innerHeight };
  const tools = phoneToolsShown(), foot = shownFoot();
  const toolsAtFoot = tools?.onKeyboard && tools.bottom >= foot - PHONE_TOOLS_FOOT_GAP - 1;
  return { width: window.innerWidth, height: toolsAtFoot ? Math.min(foot, tools.top) : foot };
}
// An opener read where it stands now at each placing (popoverAnchor.ts).
export { popoverAnchorOf, popoverRectOf, type PopoverAnchor };
export function popoverRectAt(place: { top: number; bottom: number; left: number; contextElement?: Element }): PopoverRect {
  return { get top() { return place.top; }, get bottom() { return place.bottom; },
    get left() { return place.left; }, get right() { return place.left; }, contextElement: place.contextElement };
}
export function samePopoverRect(a: PopoverRect | null, b: PopoverRect | null) {
  return a === b || (!!a && !!b && a.top === b.top && a.bottom === b.bottom && a.left === b.left && a.right === b.right);
}

// These builders describe native spacing/direction. They do not calculate a
// position. The measured size remains available to callers for row/label policy.
export function placeAround(anchor: PopoverRect, _size: PopoverSize, _viewport: PopoverViewport, options: PlaceAroundOptions = {}): PopoverRequest {
  const side = options.prefer === "above" ? "top" : "bottom";
  return { anchor, placement: options.align === "center" ? side : `${side}-${options.align ?? "start"}`,
    gap: options.gap ?? 4, options, minHeight: options.minHeight ?? 120, alignFallback: options.alignFallback, keepSide: options.keepSide };
}
export function placeBeside(anchor: PopoverRect, _size: PopoverSize, _viewport: PopoverViewport, options: PlaceBesideOptions = {}): PopoverRequest {
  return { anchor, placement: `${options.prefer ?? "right"}-start`, gap: options.gap ?? 0, offsetTop: options.offsetTop, options };
}
/**
 * A setting row's list as Notion's sub-lists open (2026-10-16: the date
 * window's "리마인더", a rollup's "대상 속성" and "계산"): beside the window
 * holding the row, its top on the row's, not over the rows under it; under
 * the row where it sits in no window.
 */
export function placeBesideRow(row: HTMLElement | null, size: PopoverSize, viewport: PopoverViewport): PopoverRequest {
  const at = popoverRectOf(row) ?? { top: 0, bottom: 0, left: 0, right: 0 };
  const holder = popoverRectOf(row?.closest<HTMLElement>('[role="dialog"], [role="menu"]') ?? null);
  return holder ? placeBeside({ ...holder, top: at.top, bottom: at.bottom }, size, viewport, { gap: 0 })
    : placeAround(at, size, viewport, { gap: 0, align: "end", alignFallback: true });
}
export function placeRowTip(row: PopoverRect, measured: PopoverSize, viewport: PopoverViewport): PopoverRequest {
  const gap = 4;
  // Beside the actual option when either side fits; otherwise below its
  // centre, as the narrow Notion menu. Floating UI can flip upward at the foot.
  if (Math.max(row.left - POPOVER_MARGIN, viewport.width - POPOVER_MARGIN - row.right) < measured.width + gap) {
    return { anchor: row, placement: "bottom", gap: 6, options: {} };
  }
  return placeBeside(row, measured, viewport, { gap, offsetTop: (row.bottom - row.top - measured.height) / 2 });
}
/** Its top start at the point: its top left, or its top right where the screen reads from the right, so a menu at a press opens away
 * from the pointer the way the screen reads, and one at its opener's start edge (`inlineStartX`) mirrors with it; a press's, whole
 * (pressedPoint), only fading in as it opens, as Notion's menu at a table row's right click (popoverAppear.ts). */
export function placeAtPoint(point: { x: number; y: number; contextElement?: Element; pressed?: boolean }, size: PopoverSize, viewport: PopoverViewport, options: PopoverOptions = {}): PopoverRequest {
  return point.pressed ? placeAround(pressedBox(point), size, viewport, { appear: "fade", ...options, gap: 0, whole: true })
    : { anchor: { left: point.x, right: point.x, top: point.y, bottom: point.y, contextElement: point.contextElement }, placement: "bottom-start", point: true, gap: 0, options };
}
/** Its top left where its caller worked it out (a box centred in the window). */
export function placeInWindow(wanted: { left: number; top: number }, measured: PopoverSize, viewport: PopoverViewport, options: PopoverOptions = {}): PopoverRequest {
  return { ...placeAtPoint({ x: wanted.left, y: wanted.top }, measured, viewport, options), corner: true };
}

export function popoverPlacementStyle(placement: PopoverPlacement | null): CSSProperties {
  // Placed by its left and top alone: a right or bottom its class gives (a
  // shared menu's "right: 4px") would stretch it to the window's edge
  // (2026-11-07: a tab's menu ran across the window, over the sidebar).
  const initial: CSSProperties = { maxWidth: `calc(100vw - ${POPOVER_MARGIN * 2}px)`, right: "auto", bottom: "auto" };
  // Unplaced, a popover is measured with its rows drawn, unseen and out of
  // the pointer's way: a row under the pointer as it is placed would light.
  if (!placement) return { ...initial, opacity: 0, pointerEvents: "none" };
  return { ...initial, left: placement.left, top: placement.top, ...(placement.settling ? { pointerEvents: "none" as const } : {}),
    ...(placement.maxHeight === undefined ? {} : { maxHeight: placement.maxHeight, overflowY: "auto" }),
    ...(placement.maxWidth === undefined ? {} : { maxWidth: placement.maxWidth }),
    ...(placement.zIndex === undefined ? {} : { zIndex: placement.zIndex }) };
}
export function popoverBackdropStyle(placement: PopoverPlacement | null): CSSProperties | undefined {
  return placement?.zIndex === undefined ? undefined : { zIndex: placement.zIndex - 1 };
}
/** A noninteractive row tip belongs above its menu without taking ownership
 * of Escape or the editor's caret keys from that menu. */
export function rowTipStyle(placement: PopoverPlacement | null, owner: PopoverPlacement | null): CSSProperties {
  return { ...popoverPlacementStyle(placement), ...(owner?.zIndex === undefined ? {} : { zIndex: owner.zIndex + 1 }) };
}

/** The widest its own CSS lets it be (a tip's 240px), apart from the width
 * placing writes on it; Infinity where its CSS sets none in pixels. */
function ownMaxWidth(element: HTMLElement) {
  const written = element.style.maxWidth;
  element.style.maxWidth = "";
  const own = getComputedStyle(element).maxWidth;
  element.style.maxWidth = written;
  return own.endsWith("px") ? Number.parseFloat(own) : Infinity;
}
// Places a hundredth of a pixel apart are the same place: a fraction's own
// noise sets nothing.
function samePlacement(a: PopoverPlacement | null, b: PopoverPlacement) {
  return !!a && Math.abs(a.left - b.left) < 0.01 && Math.abs(a.top - b.top) < 0.01 && a.maxHeight === b.maxHeight
    && a.maxWidth === b.maxWidth && a.side === b.side;
}
/** A `holdSide` popover asks first for the side it is already on. */
function holdingSide(request: PopoverRequest, held: PopoverSide | null): PopoverRequest {
  if (!request.options.holdSide || (held !== "above" && held !== "below")) return request;
  const [, align] = request.placement.split("-");
  const placement = `${held === "above" ? "top" : "bottom"}${align ? `-${align}` : ""}` as Placement;
  return request.placement.startsWith("top") || request.placement.startsWith("bottom") ? { ...request, placement } : request;
}
/** Beside its opener, a popover opens toward the reading direction's end:
 * a "right" placement is on the left where the opener reads from the right. */
function mirrored(placement: Placement, rtl: boolean): Placement {
  if (!rtl) return placement;
  return placement.replace(/^(left|right)/u, (side) => (side === "left" ? "right" : "left")) as Placement;
}
function referenceBox(rect: PopoverRect) {
  return { x: rect.left, y: rect.top, left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom,
    width: rect.right - rect.left, height: rect.bottom - rect.top };
}

/**
 * Where a popover placed by a line above a phone's editing tools keeps to:
 * what shows of the page under the top of the page's scrolling area, as far
 * inside as from the window's edges, and down to the lowest foot the tools
 * take over a keyboard (`floor`) — the page's top bar and any band over it stay in sight
 * (2026-11-10: turned over its line it covered the top bar and the "앱에서
 * 열기" band, 13..242 over a bar at 44..88) — but a line's list over a
 * keyboard that shortens the window, whose top it may reach as Notion's
 * (resolvePlacement) —, and the tools give way to it
 * rather than show at its sides (2026-11-09: 8px of them showed beside a list
 * over them). Null for any other popover.
 */
function phoneLineEdges(anchor: PopoverRect, opener: Element | undefined, padding: number) {
  const tools = phoneToolsShown();
  if (!tools || anchor.bottom > tools.top) return null;
  const page = (anchor.contextElement ?? opener)?.closest("[data-page-scroll]");
  const head = page ? Math.max(padding, page.getBoundingClientRect().top - shownTop() + padding) : padding;
  // It takes the tools' place, as Notion's list takes its tool bar's
  // (measured 2026-11-10): the tools give way while it is open
  // (NativePhoneToolbar.module.css). Over a keyboard it goes down as far as
  // they stand at the lowest, by the foot of what shows.
  if (!tools.onKeyboard) return { head, foot: padding, floor: window.innerHeight - padding, shortened: false };
  // `shortened`: the keyboard shortens the window itself, nothing of it under
  // what shows (Android Chrome with Leafen's resizes-content).
  return { head, foot: Math.max(padding, PHONE_TOOLS_FOOT_GAP), floor: shownFoot() - PHONE_TOOLS_FOOT_GAP,
    shortened: windowFootPadding() > -1 };
}
/** How far the window's foot is under the foot of what shows, as the padding
 * Floating UI takes from what shows (its viewport boundary): negative where
 * the browser draws the page on under a phone's keyboard (iPhone Safari keeps
 * its window's height there), none where the keyboard shortens the window
 * (Android Chrome with Leafen's resizes-content) or none shows. */
function windowFootPadding() {
  const shown = window.visualViewport;
  return shown ? shown.offsetTop + shown.height - window.innerHeight : 0;
}

/** Floating UI owns geometry and DOM measurement. Native options only set
 * approved spacing, minimum menu height, caps and whole template-row sizing. */
async function resolvePlacement(request: PopoverRequest, positionElement: HTMLElement, element: HTMLElement, measured: PopoverSize,
  current: () => boolean, opener?: Element): Promise<PopoverPlacement> {
  const across = request.options.margin ?? POPOVER_MARGIN, padding = request.options.marginY ?? across;
  const phoneLine = phoneLineEdges(request.anchor, opener, padding);
  if (current()) element.toggleAttribute("data-phone-line-list", Boolean(phoneLine));
  // A line's menu by a touch screen's line (`lineSide`) goes down as far as the window does.
  const lineSide = request.options.lineSide;
  // Over a keyboard that shortens the window (Android Chrome) it may
  // stand up to the window's top, over the fixed top bar: Notion's window
  // goes on under the keyboard there and its top bar slides away, its list
  // over the line reaching the top of what shows (the lineSide side below).
  const shortened = phoneLine?.shortened === true && lineSide !== undefined;
  const head = shortened ? 0 : phoneLine?.head ?? padding;
  const foot = phoneLine ? lineSide !== undefined ? windowFootPadding() : phoneLine.foot : padding;
  const overflow = { padding: padding === across && head === padding && foot === padding ? padding
    : { top: head, bottom: foot, left: across, right: across }, boundary: [] as Element[], rootBoundary: "viewport" as const };
  let maxHeight = request.options.cap, maxWidth: number | undefined;
  const ownCap = ownMaxWidth(element);
  const spacing = offset({ mainAxis: request.gap, crossAxis: request.offsetTop ?? 0 });
  const rise = request.options.rise === true, whole = request.options.whole === true;
  const turn = () => flip({ ...overflow, crossAxis: request.alignFallback ? "alignment" : false,
    flipAlignment: Boolean(request.alignFallback), fallbackStrategy: whole && !request.options.roomier ? "initialPlacement" : "bestFit" });
  const from = request.anchor;
  const reference: VirtualElement = { getBoundingClientRect: () => referenceBox(from), contextElement: from.contextElement };
  const strategy = getComputedStyle(positionElement).position === "fixed" ? "fixed" : "absolute";
  // Its start lines up with its opener's start in the reading direction of
  // the frame it opens from, past any text there that reads by its own words
  // (lib/readingDirection): a menu under a button in a right-to-left app ends
  // at the button's right edge, and so does one opened from an English line
  // there, as Notion's "/" (seen 2026-11-09); one at a point has its top
  // right there. One placed by its top left (`corner`) stays so.
  const rtl = !request.corner && readsRightToLeft(request.anchor.contextElement ?? opener ?? positionElement);
  // The popover is its natural size to Floating UI, not the height an
  // earlier pass held it to: held, it always "fit" below and never flipped
  // (2026-11-07: a link window near the window's foot stayed 64px high
  // there with 700px free above). Once this pass cuts it, it is the cut
  // size: still its natural one, it was slid up by the height cut off and
  // left a gap under it (2026-11-08: a column menu cut to 370 at 236..606
  // in a 723 window). An icon picker's positioned portal layer is
  // zero-sized; its inner dialog's size is the one measured.
  const sized = (size: () => PopoverSize) => ({ ...platform, isRTL: () => rtl,
    getDimensions: (target: Element) => target === positionElement ? size() : platform.getDimensions(target) });
  // A scroll bar the cut brings, where the system always shows them, widens
  // a popover as wide as its rows by the bar's own width: its place counts it
  // (2026-11-10: an "@" list placed at 315 was drawn 330 and ran past the
  // window's edge).
  let widened = 0;
  const drawn = () => ({ width: Math.min(measured.width + widened, maxWidth ?? Infinity), height: Math.min(measured.height, maxHeight ?? Infinity) });
  let placement = mirrored(request.placement, rtl), turns = !request.point && !request.keepSide && !rise;
  // By a phone's line it stands under its line, cut to the room above the
  // tools; it goes over its line only when that room holds less than its
  // least height, where Notion's sits behind the keyboard showing its head
  // alone.
  if (turns && phoneLine && lineSide === undefined) {
    const room = phoneLine.floor - from.bottom - request.gap;
    placement = placement.replace(/^(top|bottom)/u, room >= (request.minHeight ?? 120) ? "bottom" : "top") as Placement;
    turns = false;
  }
  if (turns && lineSide !== undefined) {
    const needs = lineSide + request.gap;
    // By a touch screen's line the room under it is the window's: Notion's
    // phone "@" and "[[" choose their side by it, keyboard or none, and draw
    // under the keyboard where the browser keeps the page there (seen
    // 2026-11-14 in Android Chrome at 411, 781 high, 251.2-high list, 12
    // gap: under a caret ending at 516.8, over one at 518.1 — by the window
    // with the keyboard up as down, under one at 291.8 with 452 showing;
    // iPhone Safari at 402 likewise, 2026-11-13). Else over it where the
    // room there, under the page's top bar, holds it, else under it. Its line
    // is where it stood before a keyboard came, as in Notion's window, which
    // goes on under the keyboard and is pushed up behind it by a line it
    // covered — Leafen's is shortened and its page scrolled
    // (keyboardPush): under a line typed on with the keyboard up, cut at it,
    // over one it came over low (seen 2026-11-21 on a Galaxy A90 in two tabs:
    // under carets ending at 140.2, 215.8 and 291.8 of 452 showing, over ones
    // at 520.2 and 671.8 in a window pushed 285 and 329.5). Where under it less than
    // its least height would show over the tools or keyboard, it is over its line.
    const push = keyboardPush((from.contextElement ?? opener)?.closest("[data-page-scroll]"));
    const over = from.top + push - (window.visualViewport?.offsetTop ?? 0) - head;
    const under = window.innerHeight + keyboardShortening() - from.bottom - push;
    let below = phoneLine ? under >= needs || over < needs
      : popoverViewport().height - from.bottom >= needs || from.top < needs;
    if (below && phoneLine && phoneLine.floor - from.bottom - request.gap < (request.minHeight ?? 120)) below = false;
    placement = placement.replace(/^(top|bottom)/u, below ? "bottom" : "top") as Placement;
    turns = false;
  }
  const sideBy = request.options.sideBy;
  if (turns && sideBy !== undefined && sideBy > measured.height) {
    // The side is chosen for its full height; it is then drawn at its own on that side.
    placement = (await computePosition(reference, positionElement, { placement, strategy, middleware: [spacing, turn()],
      platform: sized(() => ({ width: measured.width, height: sideBy })) })).placement;
    turns = false;
  }
  const middleware: Middleware[] = [spacing];
  if (turns) middleware.push(turn());
  // Where it stands on its side before it slides to fit (wholeShift).
  middleware.push({ name: "unshifted", fn: ({ y }) => ({ data: { y } }) });
  // Slide along the aligned edge before measuring available size. Otherwise
  // a right-edge opener shrinks a 290px menu to the remaining 114px rather
  // than shifting that same menu left. Above/below keep their chosen vertical
  // side until size; point and side menus may shift on both axes immediately.
  middleware.push(shift({ ...overflow, mainAxis: true,
    crossAxis: Boolean(request.point || request.placement.startsWith("left") || request.placement.startsWith("right")) }));
  middleware.push(floatingSize({ ...overflow, apply({ availableHeight, availableWidth }) {
    // By a phone's line it is cut to its side's room, scrolling: never
    // taller than its room, over its line, the top bar or the tools.
    const minimum = phoneLine ? 0 : Math.min(request.minHeight ?? 0, Math.max(0, window.innerHeight - padding * 2));
    let height = rise || whole ? Math.max(0, window.innerHeight - padding * 2) : Math.max(minimum, availableHeight, 0);
    height = Math.min(height, request.options.cap ?? Infinity);
    const row = request.options.heightStep;
    if (row && height > row.offset + row.step) height = row.offset + Math.floor((height - row.offset) / row.step) * row.step;
    maxHeight = height === request.options.cap ? height : Math.floor(height); maxWidth = Math.max(0, Math.floor(availableWidth)); // a cap's fraction kept, as Notion's half-window 361.5
    if (current()) {
      element.style.maxHeight = `${maxHeight}px`;
      // A menu's own cap in px (a sidebar menu's width and its slack) stays
      // where the window has room for it; written over, a long row's words
      // stretched the menu across the window (2026-11-06: a teamspace's "…"
      // drew 412px where Notion's is 282, its "복제" note on one line).
      // Nor is its CSS's own cap widened to the window's room: written over,
      // a description tip measured at its 300px grew to one long line over
      // what it was placed beside (2026-11-10).
      const authored = element.dataset.popoverMaxWidth ?? (element.dataset.popoverMaxWidth = element.style.maxWidth);
      const authoredPx = authored.endsWith("px") ? Number.parseFloat(authored) : Number.NaN;
      element.style.maxWidth = authoredPx <= maxWidth ? authored : `${Math.min(maxWidth, ownCap)}px`;
      element.style.overflowY = "auto";
      if (maxHeight < measured.height) widened = Math.max(0, Math.round(element.getBoundingClientRect().width) - measured.width);
    }
  } }), shift({ ...overflow, mainAxis: true, crossAxis: true }));
  const result = await computePosition(reference, positionElement, { placement, strategy, middleware, platform: sized(drawn) });
  const side: PopoverSide = request.point ? "point" : result.placement.startsWith("top") ? "above"
    : result.placement.startsWith("bottom") ? "below" : result.placement.startsWith("left") ? "left" : "right";
  // Its place keeps its fraction, as Notion's does (seen 2026-11-12: a
  // picker at 608.5, 656.5 and 238.4 under values at such places), slid to fit too (wholeShift).
  return { left: result.x, top: wholeShift(result.middlewareData.unshifted?.y, result.y), side, origin: popoverOrigin(side, result.placement.split("-")[1], rtl),
    ...(maxHeight !== undefined && (maxHeight < measured.height || request.options.cap !== undefined) ? { maxHeight } : {}),
    ...(maxWidth !== undefined && maxWidth < measured.width ? { maxWidth } : {}) };
}

/**
 * Slid up or down to fit the window, a popover moves by whole pixels and
 * keeps the fraction of the place its opener gave it, as Notion's (seen
 * 2026-11-14 at 1440×779: the date window's 시간대 row at 632.398 put its
 * 595-high list at 172.398, its foot 11.6 from the window's, and the date
 * window over a cell at .398 at 209.398; a settings 시간대 button at 375.5 put
 * Notion's list at 116.5, where sliding it to the margin itself stood it at 116).
 */
export function wholeShift(before: number | undefined, after: number) {
  return before === undefined || before === after ? after : before - Math.trunc(before - after);
}

// Every open popover's placing (usePopoverPlacement): one placed beside another as that one still grew into
// place (popoverAppear.ts) — a list opened by → the moment its menu opens — stood by its smaller box, and is placed again.
const placings = new Set<() => void>();
const placeAllAgain = (but: () => void) => { for (const again of placings) if (again !== but) again(); };

export function usePopoverPlacement(ref: RefObject<HTMLElement | null>,
  place: ((measured: PopoverSize, viewport: PopoverViewport) => PopoverRequest) | null,
  deps: DependencyList, sized?: (element: HTMLElement) => HTMLElement | null,
  layer: false | Omit<OverlayLayerOptions, "ref"> = {}): PopoverPlacement | null {
  const [placement, setPlacement] = useState<PopoverPlacement | null>(null);
  // Whether this opening's place has held still: until it has, its rows may
  // still be coming and growing it over its opener, and a row passing under
  // the pointer would light as if pointed at (2026-11-09: a page starter's
  // "⋯" menu, placed above the pill short, grew down over it as its rows
  // came and lit "표" under the pressed pointer). Moves after that keep the pointer.
  const [still, setStill] = useState(false);
  // What that state was last given: a closed popover's pass, or one landing
  // where it already is, sets nothing (each ran on every dependency change).
  const given = useRef<PopoverPlacement | null>(null);
  const stillWait = useRef(0);
  const give = (next: PopoverPlacement | null) => {
    if (next === null ? given.current === null : samePlacement(given.current, next)) return;
    given.current = next; setPlacement(next);
    window.cancelAnimationFrame(stillWait.current);
    if (next === null) { setStill(false); return; }
    // Two frames with no new place: its rows and size have come.
    stillWait.current = window.requestAnimationFrame(() => {
      stillWait.current = window.requestAnimationFrame(() => { if (given.current === next) setStill(true); });
    });
  };
  useEffect(() => () => window.cancelAnimationFrame(stillWait.current), []);
  const placeRef = useRef(place), sizedRef = useRef(sized);
  placeRef.current = place; sizedRef.current = sized;
  const heldSide = useRef<PopoverSide | null>(null); // The side it took last while this opening lasts (`holdSide`).
  const open = place !== null, drawnAnew = usePopoverDrawnAnew(ref, open); // Its element drawn anew is placed again.
  const { zIndex } = useOverlayLayer(open && layer !== false, { floats: true, ...(layer || {}), ref });
  useLayoutEffect(() => {
    const root = ref.current;
    const element = root && (sizedRef.current ? sizedRef.current(root) : root);
    drawnAnew.place(open ? root : undefined); if (!open) heldSide.current = null;
    if (!open || !root || !element || !placeRef.current) { give(null); return; }
    let live = true, revision = 0;
    // Only the popover itself keeps the written max-height; an inner element
    // sized apart has it set from the result, or its class's cap back.
    const uncapped = element === root;
    const measured = naturalSize(element, uncapped);
    const initial = placeRef.current(measured, popoverViewport());
    // Its opener is never in it nor it: a field it focuses as it opens made
    // its own list's scrolling place it again, and itself as its opener
    // placed it again every frame, each measuring it whole, which put a
    // scrolled list back at its top under the pointer (2026-11-11: a
    // database page's "속성 추가" made 사람 for the type scrolled to).
    const focused = document.activeElement instanceof Element && !root.contains(document.activeElement) ? document.activeElement : null;
    const opener = initial.anchor.contextElement ?? focused ?? root.ownerDocument.body;
    const reference: VirtualElement = { contextElement: opener,
      getBoundingClientRect: () => referenceBox(placeRef.current?.(naturalSize(element, uncapped), popoverViewport()).anchor ?? initial.anchor) };
    const update = () => {
      if (!live || !root.isConnected || !placeRef.current) return;
      const turn = ++revision, measured = naturalSize(element, uncapped);
      const request = holdingSide(placeRef.current(measured, popoverViewport()), heldSide.current);
      void resolvePlacement(request, root, element, measured, () => live && revision === turn && root.isConnected, opener).then(next => {
        if (!live || revision !== turn || !root.isConnected) return;
        heldSide.current = next.side;
        if (element !== root) {
          element.style.maxHeight = next.maxHeight === undefined ? "" : `${next.maxHeight}px`;
          element.style.maxWidth = next.maxWidth === undefined ? "" : `${next.maxWidth}px`;
          if (next.maxHeight !== undefined) element.style.overflowY = "auto";
        }
        const opening = given.current === null;
        flushSync(() => give(next)); // Placed in the frame its size step's cut is drawn in, not one later (2026-11-18: a guest's pages grown to 732 drew a frame at its old place, past the window).
        // It comes on from its first frame (popoverAppear.ts), the popovers placed by it as it grew placed again once it has.
        if (opening) popoverAppears(root, element, request.options.appear, next.origin, () => placeAllAgain(update));
      });
    };
    // It keeps to its opener, to what shows of the page and to its content's size, grown placed in that task (popoverPlaceAgain.ts).
    const stop = placeAgainOn(reference, root, element, update, { layoutShift: typeof IntersectionObserver !== "undefined", shown: true });
    placings.add(update);
    return () => { live = false; revision++; stop(); placings.delete(update); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, ref, drawnAnew.count, ...deps]);
  return useMemo(() => {
    if (!open || !placement) return null;
    const layered = zIndex === undefined ? placement : { ...placement, zIndex };
    return still ? layered : { ...layered, settling: true as const };
  }, [open, placement, zIndex, still]);
}

/** CSS chooses the unanchored menu's desired location. Floating UI owns its
 * clipping and (for fixed menus) movement into the viewport. */
export function useFitPopoverInWindow(ref: RefObject<HTMLElement | null>, open: boolean) {
  // It fades in as Notion's icon picker does (popoverAppear.ts).
  usePopoverAppear(open, ref, undefined, "fade");
  useLayoutEffect(() => {
    const element = ref.current;
    if (!open || !element) return;
    const original = { left: element.style.left, top: element.style.top, right: element.style.right, bottom: element.style.bottom };
    let wanted = element.getBoundingClientRect();
    let live = true, revision = 0;
    const reference: VirtualElement = { contextElement: element.parentElement ?? undefined, getBoundingClientRect: () => wanted };
    const update = () => {
      if (!live || !element.isConnected) return;
      const turn = ++revision;
      const current = { left: element.style.left, top: element.style.top, right: element.style.right, bottom: element.style.bottom };
      Object.assign(element.style, original);
      wanted = element.getBoundingClientRect();
      Object.assign(element.style, current);
      const request = placeInWindow({ left: wanted.left, top: wanted.top }, naturalSize(element), popoverViewport());
      void resolvePlacement(request, element, element, naturalSize(element), () => live && revision === turn && element.isConnected).then(next => {
        if (!live || revision !== turn) return;
        element.style.right = "auto"; element.style.bottom = "auto";
        element.style.left = `${next.left}px`; element.style.top = `${next.top}px`;
        element.style.maxHeight = next.maxHeight === undefined ? "" : `${next.maxHeight}px`;
        element.style.maxWidth = next.maxWidth === undefined ? "" : `${next.maxWidth}px`;
        if (next.maxHeight !== undefined) element.style.overflowY = "auto";
      });
    };
    const cleanup = placeAgainOn(reference, element, element, update, { layoutShift: false, shown: false });
    return () => { live = false; revision++; cleanup(); };
  }, [open, ref]);
}
