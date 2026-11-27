import { useLayoutEffect, useRef, useState, type RefObject } from "react";
import { readsRightToLeft } from "@/lib/readingDirection";
import { handDownAgain } from "./handDownAgain";

// The panels hanging now; the last to close takes the gap away.
let hanging = 0;
// The page they hang on, held still with its scrollbar's room while they do.
let held: HTMLElement | null = null;
const DRAWN_ROW_LOOKS = 120;

/**
 * While a panel hangs, the page under it stands still and keeps its
 * scrollbar's room whether it scrolls or not, as Notion's (seen 2026-11-13 at
 * 1440 with scrollbars shown: "overflow-y: hidden" and "scrollbar-gutter:
 * stable" on the page under the new database's panel and under the view
 * settings — the wheel, Page Down and the arrows move it no more, the panel
 * scrolling on its own — so the panel stands at 1039–1329 and the toolbar
 * ends at 1321 over a page too short to scroll, 15px in from where they
 * stand over one with no panel). Where scrollbars overlay the page, it takes
 * no room. Notion's panel, inside the page, runs its 96px end on past it,
 * a scrollbar across that moves the table by those 96px; this one hangs
 * outside the page and adds nothing to it.
 *
 * The view settings (their every screen) and a new relation's target panel
 * keep that room on a page that shows its scrollbar, every time, so nothing
 * under them moves (Notion's, seen 2026-11-17 at 1440: a full-page database
 * of 40 rows, opened, closed and opened again in one tab, both times
 * "scrollbar-gutter: stable", "새로 만들기" staying at 1217–1297; the audit's
 * pair database the same on 2026-11-16, its gallery and its wide table on
 * 2026-11-17 at 04:0x). On a page too short to scroll a view that runs
 * wider than the page — its table or board rolling sideways — keeps it only
 * the first time one of them hangs after the app loads: every later one
 * holds the page still all the same with no room, as Notion's (seen
 * 2026-11-16 at 1440 in a full-page database of twelve columns, in four tabs
 * at different times, and 2026-11-17 at 04:0x in a twelve-column one of
 * three rows: the first stood at 1039–1329 with "scrollbar-gutter: stable",
 * each later one at 1054–1344 with "overflow-y: hidden" alone; at 1280, 879
 * then 894). A view as narrow as the page keeps it every time (2026-11-17
 * at 04:0x: three rows of two columns as a table and as a gallery, opened
 * four times in one tab, each "stable" with the cards at 305 against 310
 * with no panel). The room is settled as the page is held: rolling the
 * table while a panel hangs leaves it as it was. The other panels keep the
 * room every time (the start panel, seen twice running on 2026-11-16).
 */
// Whether one of the panels keeping the room once has hung since the app loaded.
let roomGiven = false;
function holdPage(page: HTMLElement | null, roomOnce = false, scrolls = false, wide = false) {
  if (page === held) return false;
  if (held) {
    held.style.removeProperty("scrollbar-gutter");
    held.style.removeProperty("overflow-y");
    held.style.removeProperty("--dock-gap");
  }
  held = page;
  if (page) {
    if (!roomOnce || !roomGiven || scrolls || !wide) page.style.setProperty("scrollbar-gutter", "stable");
    if (roomOnce) roomGiven = true;
    page.style.setProperty("overflow-y", "hidden");
  }
  return true;
}

/**
 * Where a database's own page hangs its settings panel: under the database's
 * toolbar row, as Notion's (seen 2026-10-10: the row 126–166, the panel from
 * 166 down to the window's bottom), following the row up to the top bar
 * should the page move under it (the reader's scrolling held, holdPage).
 * Null while there's no row to hang under. It starts right where the row
 * ends, a part of a pixel and all: under a full-page database whose 32px
 * title stands a 38.4 line high Notion's row ends at 166.39 and its panel
 * starts there, every row and menu in it with it (seen 2026-11-18 in Chrome
 * at 1440, devicePixelRatio 1: a chart's "계산" 508.2 and its menu 536.2),
 * where a panel set at the whole pixel stood 0.39 higher.
 *
 * Across, the panel's 290px column ends 96px before the end of the page's
 * frame, inside its scrollbar, whatever row it hangs under, its white
 * ground running on to the window's edge (seen 2026-11-02: 894–1184 at
 * 1280, 879–1169 beside a scrollbar; 2026-11-11: 790–1080 at 1191 beside a
 * scrollbar, and 2026-11-09: the start panel and "새 보기" alike from 653 at
 * 1054). That gap is `--dock-gap`, written on the panel and the page it
 * hangs on, which read it, and not inherited (dockedPanelColumn.module.css):
 * written on the page's root, every element of the page worked out its
 * styles again as a panel opened (3,828 for a database of 60 rows, 46 ms in
 * the press that opened its view settings, NAS condition, CPU ×2,
 * 2026-11-13).
 *
 * Its first place is read as the panel is first drawn, before the draw
 * changes anything, so the press that opens it lays nothing out ahead of its
 * frame: what it reads, the toolbar's row and the page, is laid out already.
 * It is read again at the next frame, with the page held.
 */
const FRAME_END = 96;
// The column with its line (dockedPanelColumn.module.css), and how far it
// keeps from the window's edges where the frame's end leaves it no room: a
// window narrowed toward a phone's width hangs it 8px from its end, as the
// toolbar's row did before the frame was followed, never off the screen
// (Notion's desktop in Chrome narrows no further than 768, where its 96 still
// holds: 367–657 beside a scrollbar, seen 2026-11-16).
const COLUMN = 291, EDGE = 8;

// `scrolls`: the page is taller than its frame, its scrollbar shown, as it was read before being held;
// `wide`: the view's table or board runs wider than its frame, rolling sideways.
type DockPlace = { top: number | null; gap: number; page: HTMLElement | null; scrolls: boolean; wide: boolean };

function dockPlace(dock: HTMLElement | null): DockPlace {
  const row = dock?.closest<HTMLElement>("[data-database-chrome]") ?? dock;
  const box = row?.isConnected ? row.getBoundingClientRect() : null;
  // The panel hangs at the window's reading-direction end (inset-inline-end):
  // its left where the screen reads from the right.
  const page = row?.isConnected ? row.closest<HTMLElement>("[data-page-scroll]") : null;
  const inside = page?.getBoundingClientRect();
  const left = page && inside ? inside.left + page.clientLeft : null;
  const start = left === null ? box?.left ?? 0 : left + FRAME_END;
  const end = left === null ? box?.right ?? 0 : left + page!.clientWidth - FRAME_END;
  const rtl = readsRightToLeft();
  const room = rtl ? window.innerWidth - start - COLUMN : end - COLUMN;
  const gap = !box ? 0 : room < EDGE ? EDGE : rtl ? start : window.innerWidth - end;
  const across = row?.closest("[data-database-view]")?.querySelector<HTMLElement>("[data-table-scroll], [data-board-scroll]");
  return { top: box ? box.bottom : null, gap: Math.max(0, Math.round(gap)), page,
    scrolls: Boolean(page && page.scrollHeight > page.clientHeight), wide: Boolean(across && across.scrollWidth > across.clientWidth) };
}

export function useDockedPanelTop(dockRef: RefObject<HTMLElement | null> | undefined, enabled: boolean,
  panelRef?: RefObject<HTMLElement | null>, roomOnce = false) {
  // Read in its first draw alone; null once its effect has taken it.
  const drawn = useRef<DockPlace | null | undefined>(undefined);
  if (drawn.current === undefined) drawn.current = enabled && dockRef?.current ? dockPlace(dockRef.current) : null;
  const [top, setTop] = useState<number | null>(() => drawn.current?.top ?? null);
  useLayoutEffect(() => {
    if (!enabled || !dockRef) return;
    let frame = 0, looks = 0;
    hanging += 1;
    const panel = panelRef?.current ?? null;
    let gap: number | null = null, watched: HTMLElement | null = null;
    // The page's frame changing width moves the column with its end — the
    // frame a side peek had narrowed widening as the page made there opens
    // whole (2026-11-18: read while the peek still stood, the start panel of
    // a full-page database made in a row's peek hung at 634–925, not 1038–1329).
    const resized = typeof ResizeObserver === "function" ? new ResizeObserver(() => schedule()) : null;
    // Its place across is read from the page held, with its scrollbar's
    // room: a page it was read from before it was held — the row drawn a
    // moment after the panel opened, or the first draw's — is read again at
    // the next frame (2026-11-16: read before, the panel stood over that room,
    // 1053–1344 beside a toolbar ending at 1321).
    const write = (place: DockPlace) => {
      setTop(place.top);
      if (holdPage(place.page, roomOnce, place.scrolls, place.wide) && place.page) schedule();
      if (place.page !== watched) {
        if (watched) resized?.unobserve(watched);
        watched = place.page;
        if (watched) resized?.observe(watched);
      }
      for (const element of [place.page, panel]) element?.style.setProperty("--dock-gap", `${place.gap}px`);
      // Its ground beside it (dockedPanelColumn ::after) takes the gap with `inherit`.
      if (place.gap !== gap) handDownAgain(panel);
      gap = place.gap;
    };
    const measure = () => {
      frame = 0;
      const place = dockPlace(dockRef.current);
      write(place);
      // A row the page is still putting in place (a block the editor draws a
      // moment after the panel opens) is looked for again, a frame at a time, for about two seconds.
      if (place.top === null && looks++ < DRAWN_ROW_LOOKS) frame = window.requestAnimationFrame(measure);
    };
    const schedule = () => { if (!frame) frame = window.requestAnimationFrame(measure); };
    const first = drawn.current;
    drawn.current = null;
    if (first?.top != null) { write(first); schedule(); } else measure();
    window.addEventListener("scroll", schedule, { capture: true, passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      window.removeEventListener("scroll", schedule, { capture: true });
      window.removeEventListener("resize", schedule);
      resized?.disconnect();
      panel?.style.removeProperty("--dock-gap");
      hanging -= 1;
      if (hanging === 0) holdPage(null);
    };
  }, [dockRef, enabled, panelRef, roomOnce]);
  return enabled ? top : null;
}
