import { type RefObject, useLayoutEffect, useRef } from "react";
import { watchPageSize } from "../page/pageSizeWatch";
import { flushHeldStyleWrites, holdStyleWrite, startWithMeasures } from "./heldStyleWrites";
import { handDownAgain } from "./handDownAgain";
import { useWindowedTableRows } from "./useWindowedTableRows";

/**
 * A wide table in a page's text scrolls across the page's margins, as
 * Notion's (seen 2026-10-21, at 1280px and 900px, normal and full width):
 * when its columns are wider than the text, its scroller runs from the
 * page's left edge to its right one, its first column still where the text
 * starts and as much room after its last one, and scrolled sideways it
 * slides into the left margin. A table that fits stays as it is; a table in
 * a column, a callout, under another block, a row's page but its full page's
 * layout tab, or a dashboard's widget keeps its box.
 */
export function usePageWideTable(ref: RefObject<HTMLElement | null>, columnsWidth: number, { wide = true }: {
  /** Off, it keeps its database's box, marked "boxed" where it could have gone wide. */
  wide?: boolean;
} = {}) {
  // A table this wide draws its wrapped rows as Notion's windowed ones.
  useWindowedTableRows(ref, columnsWidth);
  const laidOut = useRef(false);
  useLayoutEffect(() => {
    const scroller = ref.current;
    if (!scroller) return undefined;
    // Its sideways bar gone, WebKit (Safari, the Mac app) kept the boxes
    // around the scroller as tall as with it (seen 2026-11-09: an inline
    // table 17px taller once the window was narrowed and widened again, until
    // the page was opened again). Each time its own size changes its box is
    // laid out once more, which the boxes around it then follow: a scroller's
    // least width is 0 and its own width more than 1px, so a 1px least width
    // for that moment changes nothing (its least height is the own page's
    // fill, useOwnPageViewFill). As the table is first laid out there is
    // nothing to follow yet. Blink lays them out again itself (Chrome 157,
    // seen 2026-11-13: an inline table's bar gone as a side peek closed, its
    // database as tall as drawn anew), so there each resize of a table's
    // scroller laid the page out once more for nothing.
    if (laysOutAroundScrollbars) return undefined;
    const relayout = new ResizeObserver(() => {
      if (!laidOut.current) {
        laidOut.current = true;
        return;
      }
      scroller.style.minWidth = "1px";
      void scroller.offsetHeight;
      scroller.style.removeProperty("min-width");
    });
    relayout.observe(scroller);
    return () => relayout.disconnect();
  }, [ref, columnsWidth, wide]);
  // A timeline's table shown or shut changes what its box hands down (DatabaseTimelineTable --timeline-cover).
  useLayoutEffect(() => handDownAgain(ref.current), [ref, wide]);
  // Started with the view's other first measures (startWithMeasures): what
  // its box is drawn as is worked out with theirs, for the whole page once.
  // A view put in another's place in the same database starts drawn as that
  // one was last worked out, where it is sure to be drawn so too (carried).
  useLayoutEffect(() => { carryWideOver(ref.current, columnsWidth, wide); return startWithMeasures(() => {
    const scroller = ref.current;
    // Its box, past wrappers that draw none of their own.
    let host = scroller?.parentElement ?? null;
    while (host && getComputedStyle(host).display === "contents") host = host.parentElement;
    const page = scroller?.closest<HTMLElement>("[data-page-scroll]");
    // A database under another block keeps its box too (Notion's, seen 2026-11-03).
    const block = scroller?.closest<HTMLElement>('[data-block-type="inline_database"]');
    if (!scroller || !host || !page || !scroller.closest('[data-placement="inline"]') || (block && block.dataset.depth !== "0")
      // A full page's layout tab spreads as its text's databases do (Notion's, seen 2026-11-12 at 1440).
      || (scroller.closest('[data-row-page="true"]') && !scroller.closest('[data-row-page-mode="full"] [data-row-layout-tab-panel]'))
      || scroller.closest('[data-column-id], [data-imported-database-section="true"], [data-database-view-embedded]')) {
      return undefined;
    }
    const clear = () => writeWide(scroller, null);
    // What it measures it writes after the view's other hooks have measured
    // too (heldStyleWrites): one restyling of the table for them all.
    const apply = () => {
      const section = scroller.closest<HTMLElement>('[data-placement="inline"]')!;
      if (!wide) {
        carried.set(section, "boxed");
        holdStyleWrite(() => writeWide(scroller, "boxed"));
        return;
      }
      const frame = page.getBoundingClientRect();
      const box = host.getBoundingClientRect();
      // With the text moved left for the discussions beside it, its box
      // moves as far with it, as Notion's whole page does (seen 2026-11-12
      // at 1440: 99–1254 by a page at 270–1425, the text 165 left of the
      // middle — the page clips it at its edge).
      const offset = Math.round((box.left - frame.left - page.clientLeft + marginShift(page, scroller)) * 10) / 10;
      // A table needs its columns and its "+ ⋯" after them, and in a page's
      // text at least what its styles hold it at past its row controls'
      // margin (Notion's 700px, --table-least). A database's own page keeps
      // its own edges. What it needs is the same whichever way it stands; the
      // width it is drawn at is not, as it also fills what holds it (its box,
      // 8px wider than the text, or the text across the page) and its least
      // held only in its box: measured as drawn, an inline table at 768
      // needed 700 in its box, went across the page, needed only the text's
      // 545 there and came back, every 0.4s for ever (seen 2026-11-14).
      const table = scroller.closest('[data-database-page="true"]') ? null : scroller.firstElementChild;
      const need = table ? Math.max(columnsWidth + addColumnWidth(table),
        parseFloat(getComputedStyle(table).getPropertyValue("--table-least")) || 0) : columnsWidth;
      const pageWidth = page.clientWidth;
      // It fits when its box holds it: the box starts 8px into the text and
      // ends with it, so its two margins differ (Notion's and this, seen
      // 2026-11-15 at 900: the text 96–804, the box 104–804, a table needing
      // 700 in it; the margins taken as equal left it 692 and took it across).
      if (offset <= 0 || need <= box.width) {
        carried.set(section, null);
        holdStyleWrite(clear);
        return;
      }
      // Its first column stays where it is, with the page's centred margin
      // (as if its text were 708 wide) and 8px before it, and that margin
      // after its last column's end: its box starts 6px left of the page's
      // edge once the text stands centred, and as far into its own end — a
      // table's, a board's and a timeline's alike (Notion's, seen 2026-11-10:
      // 264 by a page at 270 when 1280, 1460, 1700 and 1975 wide; 2026-11-12
      // and 2026-11-15 at 1440: 264–1419 by a page at 270–1425). Notion takes
      // that margin from a little under 96px, where its text still keeps 96
      // (seen 2026-11-14 and 2026-11-15 in a page 885 wide: the text at 96, the
      // box 7.5–892.5, the first column at 104; at 881 it keeps 96), so there
      // the box stands a little right of the page. At the page's edge where
      // the margin is less, on a full width page and on a database's own page,
      // whose text is not centred.
      // On a phone it stands as on a centred page, its margin the text's less
      // 2, so its box starts 6px left of the screen and that margin follows
      // "⋯" (Notion's, iPhone 17 Safari at 402pt, 2026-11-16: a table's box
      // −6..396, its first column at 26, its rows ending at "⋯" 24 short of the
      // box's end; a board's −6..396 by its first column at 26).
      const margin = (pageWidth - 708) / 2;
      const centred = margin >= CENTRED_MARGIN_FROM
        && !scroller.closest('[data-full-width="true"], [data-database-page="true"]');
      const edge = centred ? Math.round((margin + 8 - offset) * 10) / 10 : scroller.closest('[data-phone="true"]') ? 6 : 0;
      const across = { offset, pageWidth, edge, room: box.width };
      carried.set(section, across);
      holdStyleWrite(() => writeWide(scroller, across));
    };
    apply();
    // Measured with the page's other measures as its size changes, written
    // after them all (pageSizeWatch): its own observer measured after the
    // writes of those before it, laying the page out again each time.
    const stopPage = watchPageSize(page, apply);
    const stopHost = watchPageSize(host, apply);
    // The text moving aside for discussions moves it without resizing it;
    // the page's other variables on its text move it only as they resize it.
    const doc = scroller.closest<HTMLElement>("[data-page-search-root]");
    const moved = new MutationObserver((changes) => {
      if (doc?.dataset.marginDiscussions === "true" || changes.some(({ attributeName }) => attributeName === "data-margin-discussions")) apply();
    });
    if (doc) moved.observe(doc, { attributes: true, attributeFilter: ["data-margin-discussions", "style"] });
    return () => {
      stopPage();
      stopHost();
      moved.disconnect();
      flushHeldStyleWrites();
      clear();
    };
  }); }, [ref, columnsWidth, wide]);
}

/** The least centred margin Notion lays a wide table by (seen 2026-11-16, Chrome, pages 883 and 884 wide keep 96, 885 takes 88.5). */
const CENTRED_MARGIN_FROM = 88.5;

/** How a view's box is drawn: across the page by its measures and its box's width (room), in its box ("boxed"), or as it is (null). */
type WideDrawing = { offset: number; pageWidth: number; edge: number; room: number } | "boxed" | null;

// Each database's last worked out (by its inline section, which its views share).
const carried = new WeakMap<Element, WideDrawing>();

/**
 * A view put in another's place starts drawn as the last was worked out where
 * it is sure to be drawn so: in its box, or across the page when its own
 * columns alone need more than its box holds. Worked out after its first
 * layout (above), a timeline beside its table was laid out in its box, then
 * styled and laid out again across the page, ~40 ms at CPU ×2 inside its view
 * tab's press (2026-11-15); written before it, its first layout is the one
 * drawn and the same values worked out change nothing (writeWide).
 */
function carryWideOver(scroller: HTMLElement | null, columnsWidth: number, wide: boolean) {
  const section = scroller?.closest<HTMLElement>('[data-placement="inline"]');
  if (!scroller || !section || scroller.dataset.pageWide !== undefined || !carried.has(section)) return;
  const last = carried.get(section) ?? null;
  if (!wide ? last === "boxed" : last !== null && last !== "boxed" && columnsWidth > last.room) writeWide(scroller, last);
}

/**
 * Marks and sizes a scroller as `drawing` says, changing only what differs,
 * which would restyle it all; what it changed is handed down again to the
 * boxes under it that take it (handDownAgain).
 */
function writeWide(scroller: HTMLElement, drawing: WideDrawing) {
  const mark = drawing === null ? undefined : drawing === "boxed" ? "boxed" : "true";
  let changed = false;
  if (scroller.dataset.pageWide !== mark) {
    changed = true;
    if (mark === undefined) delete scroller.dataset.pageWide;
    else scroller.dataset.pageWide = mark;
  }
  const across = drawing !== null && drawing !== "boxed" ? drawing : null;
  const values: Record<string, string | null> = {
    "--page-wide-offset": across && `${across.offset}px`, "--page-wide-width": across && `${across.pageWidth}px`,
    "--page-wide-edge": across && `${across.edge}px`, "--page-wide-reach": across && `${across.offset + across.edge}px`,
  };
  for (const [name, value] of Object.entries(values)) {
    if ((scroller.style.getPropertyValue(name) || null) === value) continue;
    changed = true;
    if (value === null) scroller.style.removeProperty(name);
    else scroller.style.setProperty(name, value);
  }
  if (changed) handDownAgain(scroller);
  // A table is given the room before its first column on itself, as the
  // stylesheet works it out on the scroller: WebKit (the Mac app, Safari, iOS)
  // kept what the table took of it with `inherit` as it changed, its first
  // column where the page's earlier width put it (seen 2026-11-16 in the
  // desktop app: at 98 in a page 1153 wide after the window went 1024 and back).
  const table = scroller.querySelector<HTMLElement>(":scope > [role='grid']");
  const pad = across && `calc(${across.offset + across.edge}px - var(--table-gutter-width))`;
  if (!table || (table.style.getPropertyValue("--table-scroll-pad") || null) === pad) return;
  if (pad === null) table.style.removeProperty("--table-scroll-pad");
  else table.style.setProperty("--table-scroll-pad", pad);
}

/** Blink (Chrome, Edge, Android's web views): its user agent names Chrome; WebKit's (Safari, the Mac app, iOS) and Gecko's don't. */
const laysOutAroundScrollbars = typeof navigator !== "undefined" && /\bChrome\//u.test(navigator.userAgent);

/**
 * How wide a table's "+ ⋯" after its last column is drawn: its buttons from
 * the cell's start, or the new column's box while it is open. The cell itself
 * stretches to the table's end, so it is not read.
 */
function addColumnWidth(table: Element) {
  const cell = table.querySelector<HTMLElement>("[data-table-add-property]");
  if (!cell) return 0;
  const box = cell.getBoundingClientRect();
  if (cell.dataset.open === "true") return box.width;
  const rtl = getComputedStyle(cell).direction === "rtl";
  let end = parseFloat(getComputedStyle(cell).minWidth) || 0;
  for (const child of cell.children) {
    const own = child.getBoundingClientRect();
    end = Math.max(end, rtl ? box.right - own.left : own.right - box.left);
  }
  return end;
}

/** How far left of the middle the page's text stands for the discussions beside it (PageView). */
function marginShift(page: HTMLElement, element: Element) {
  const doc = element.closest<HTMLElement>('[data-margin-discussions="true"]');
  if (!doc) return 0;
  const left = doc.getBoundingClientRect().left - page.getBoundingClientRect().left - page.clientLeft;
  return Math.max(0, (page.clientWidth - doc.offsetWidth) / 2 - left);
}
