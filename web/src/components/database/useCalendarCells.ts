import { type Dispatch, type RefObject, type SetStateAction, startTransition, useEffect, useMemo, useRef, useState } from "react";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { loadSession } from "@/lib/authSessionStore";
import { calendarCells, type CalendarLayout } from "./calendarViewModel";
import { useDatabaseOwnPage } from "./databaseFullPageLink";

/**
 * On its database's own page a month runs on below its last week into the
 * weeks after it, as Notion's (seen 2026-11-15 at 1440, the month 2026-11:
 * 27 weeks from 11/1's to 5/2's, the other months' days in the faint
 * outside colour with "12월 1일", "1월 1일" on their first days, the head
 * still "2026년 11월", and more weeks as the page nears their end — 3,719px
 * of page grown to 17,813 by its foot). Inline, a month is its own weeks.
 */
const FIRST_WEEKS = 27;
const MORE_WEEKS = 26;

/** The days a calendar shows, and whether it is on its database's own page. */
export function useCalendarCells(anchor: Date, layout: CalendarLayout, weekStart: 0 | 1, gridRef: RefObject<HTMLElement | null>) {
  const ownPage = useDatabaseOwnPage();
  const running = ownPage && layout === "month";
  const [shown, setShown] = useState({ at: anchor.getTime(), weeks: FIRST_WEEKS });
  // A month newly shown starts again from its first weeks.
  const weeks = shown.at === anchor.getTime() ? shown.weeks : FIRST_WEEKS;
  // A month shown draws its own weeks on the press's frame and the weeks
  // after them in the frames after, a newer press taking their place (user
  // decision 2026-11-12; all 27 at once took a month's ‹ › from 64 to 152 ms
  // at CPU ×2, 2026-11-15).
  const [grownAt, setGrownAt] = useState<number | null>(null);
  const grown = running && grownAt === anchor.getTime();
  useEffect(() => {
    if (!running || grown) return undefined;
    const at = anchor.getTime();
    const frame = requestAnimationFrame(() => startTransition(() => setGrownAt(at)));
    return () => cancelAnimationFrame(frame);
  }, [anchor, grown, running]);
  const cells = useMemo(() => calendarCells(anchor, layout, weekStart, grown ? weeks : undefined),
    [anchor, layout, grown, weekStart, weeks]);
  // The page scrolled to within a screen of its end takes the next weeks;
  // its scroller is looked for after every drawing, as the grid may come after the view.
  const watched = useRef<{ scroller: Element; run: string; stop: () => void } | null>(null);
  useEffect(() => {
    const scroller = grown ? gridRef.current?.closest("[data-page-scroll]") ?? null : null;
    const run = `${anchor.getTime()}/${weeks}`;
    if (watched.current?.scroller === scroller && watched.current?.run === run) return;
    watched.current?.stop();
    watched.current = null;
    if (!scroller) return;
    const at = anchor.getTime();
    const near = () => {
      if (scroller.scrollTop + scroller.clientHeight * 2 < scroller.scrollHeight) return;
      scroller.removeEventListener("scroll", near);
      setShown({ at, weeks: weeks + MORE_WEEKS });
    };
    scroller.addEventListener("scroll", near, { passive: true });
    watched.current = { scroller, run, stop: () => scroller.removeEventListener("scroll", near) };
  });
  useEffect(() => () => watched.current?.stop(), []);
  return { cells, ownPage };
}

const KEPT_KEY = "leafen:calendar-shown";

/**
 * The day a calendar shows from, kept in this browser for the person and the
 * view as it moves (‹, ›, 오늘, a changed unit), as Notion keeps its
 * "LocalPreferenceStore3:<person>:CalendarView:<view>" {dateRangeStart}
 * in the browser's local storage (seen 2026-11-15: December shown again
 * after a reload in Chrome, twice; the Notion desktop app, its own storage,
 * opened the same view on today's week). Nothing kept, it opens on today.
 */
export function useKeptCalendarAnchor(viewer: string | undefined, viewId: string): [Date, Dispatch<SetStateAction<Date>>] {
  // The person signed in here where the view names none.
  const [userId] = useState(() => viewer ?? loadSession()?.user.id);
  const [anchor, setAnchor] = useState(() => {
    const kept = userId ? Number(personValue(KEPT_KEY, { userId, part: viewId })) : Number.NaN;
    return Number.isFinite(kept) && kept > 0 ? new Date(kept) : new Date();
  });
  const opened = useRef(anchor);
  useEffect(() => {
    // The day it opened on is not kept until it moves.
    if (!userId || anchor === opened.current) return;
    storePersonValue(KEPT_KEY, String(anchor.getTime()), { userId, part: viewId });
  }, [anchor, userId, viewId]);
  return [anchor, setAnchor];
}
