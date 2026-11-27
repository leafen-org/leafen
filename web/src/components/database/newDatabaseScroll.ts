// Less room than this from its database's top to the window's foot, the panel
// opened by the pointer first scrolls that top to the top of the page's view.
const LOW_ROOM = 400;
// The height Notion counts a database just made at as it brings it up: its
// first drawing's (seen 2026-11-16 in a 723 window with the page's end at
// 291: a linked view and a board both went to 431, the page's end with a 180
// block, then grew to 290 and 185; at a long page's end a table stopped 34
// short of the end, its 214 less 180; a timeline, a map, an inline database
// and a tab's first chart went to 431 too — later charts, drawn at once, to
// 465). A calendar draws its month at once and counts whole: it went to 696,
// its top at the page's top, twice on two pages. Shorter views end the page.
const FIRST_DRAWN = 180;
const DRAWN_WHOLE = '[data-view-type="calendar"]';
// How long a database drawing more of itself is followed.
const FOLLOW_MS = 1000;

function scrollerOf(element: HTMLElement): HTMLElement | null {
  for (let at = element.parentElement; at; at = at.parentElement) {
    const { overflowY } = getComputedStyle(at);
    if ((overflowY === "auto" || overflowY === "scroll") && at.scrollHeight > at.clientHeight) return at;
  }
  return document.scrollingElement instanceof HTMLElement ? document.scrollingElement : null;
}

/**
 * Brings a database just made from "/" up as its start panel opens, when it
 * stands low in the window, as Notion's (seen 2026-11-08 in a 696 window: a
 * board made from "/" at 577 or 330 and a waiting view's "데이터 소스 연결" at
 * 300 went to 44; that button at 290, 270, 220 or 150 and a board made at 160
 * stayed; 2026-11-11 in a 723 window, by the pointer and the "/" menu's Enter
 * alike: made at 528 it went to 44, at 538 or 533 near the page's end as far
 * as the page goes, at 311 or 250 it stayed). Near the page's end it goes as
 * far as the page would go with the database at its first drawing's height
 * and no further as it grows: a long page's end leaves it 110 short of the
 * end for a linked view, 5 for a board, 34 for a table (seen 2026-11-16, three
 * times). A database still drawing itself (a waiting view's empty table) is
 * followed for its first second up to that place, never leaving it low with
 * the panel over it (seen 2026-11-14: a tab at 510, the panel at 145).
 * Returns what stops the following.
 */
export function bringNewDatabaseUp(host: HTMLElement): (() => void) | undefined {
  if (window.innerHeight - host.getBoundingClientRect().top >= LOW_ROOM) return undefined;
  const scroller = scrollerOf(host);
  if (!scroller) return undefined;
  const scrollerTop = scroller === document.scrollingElement ? 0 : scroller.getBoundingClientRect().top;
  const hostRect = host.getBoundingClientRect();
  const startAt = scroller.scrollTop + hostRect.top - scrollerTop
    - (parseFloat(getComputedStyle(host).scrollMarginTop) || 0) - (parseFloat(getComputedStyle(scroller).scrollPaddingTop) || 0);
  const counted = host.querySelector(DRAWN_WHOLE) ? hostRect.height : Math.min(hostRect.height, FIRST_DRAWN);
  const endAt = scroller.scrollHeight - scroller.clientHeight - (hostRect.height - counted);
  const place = Math.max(scroller.scrollTop, Math.min(startAt, endAt));
  scroller.scrollTop = place;
  const grown = new ResizeObserver(() => { if (scroller.scrollTop < place) scroller.scrollTop = place; });
  grown.observe(host);
  const done = window.setTimeout(() => grown.disconnect(), FOLLOW_MS);
  return () => { window.clearTimeout(done); grown.disconnect(); };
}
