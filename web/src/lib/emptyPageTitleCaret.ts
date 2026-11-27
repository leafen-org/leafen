import { type RefObject, useEffect, useLayoutEffect, useRef } from "react";
import { openingTakesTitleCaret } from "@/components/database/peekFocusReturn";
import { caretOnceAnswered, placeCaret } from "@/components/editor/focus";
import { openedInApp } from "@/lib/router";
import { actedSince } from "./actedSince";
import { shutBehindModal } from "./modalFocus";
import { holdingTyping, pageTitleFocusAsked } from "./pageTitleFocus";
import { forgetTitleFocusAsked, takeTitleFocusAsked } from "./titleFocusAsked";

// How long the page's body must stay as it is before it is read: the first
// draw of a page opened from another can still carry the one left.
const SETTLE_MS = 120;

/**
 * A page that still wants writing opens with the caret at its title's end, as
 * Notion's (seen 2026-11-07, opened from the sidebar, from a page block, from
 * search or as a copy): its body is empty, or its title is — an untitled page
 * with blocks gets the caret in its empty title. A page with both a title and
 * blocks leaves the focus where it was. Each page is read once, when its body
 * has loaded — a body emptied later keeps the caret where the writing is. A
 * field already holding the keys keeps them. Opened by its address — the
 * screen this document opened at, a reload too — it takes no caret, a letter
 * typed then going nowhere, as Notion's in Chrome (seen 2026-11-14 and
 * 2026-11-16 at 1440, a title-only page made from the sidebar, judged by a
 * letter typed after the page showed, no script before it) and its phone.
 *
 * A touch screen — a phone's, a tablet's, an opened fold's — opens a page as
 * Notion's phone does (seen 2026-11-10 in iPhone Safari at 402pt and
 * 2026-11-24 in Chrome on a Galaxy A90): by its address (the screen this
 * document opened at), at its top with no caret; opened in the app — from the
 * drawer or the sidebar, a link, the home or another screen first shown — an
 * untitled one takes the caret in its title and a titled one none, its body
 * empty or not (Notion's iPad's empty row page too, openingTakesTitleCaret),
 * so no keyboard rises at the tap that opened it. Nothing is asked for at
 * that tap: the ask held what is typed in a field of its own, whose focus
 * raised the keyboard, dropped a moment on as the page wanted no caret
 * (2026-11-24: each page opened on an unfolded Galaxy Fold, a touch screen
 * about 900 wide, no phone, flashed the keyboard). On a phone one without a
 * cover scrolls its head away (scrollPageHeadAwayOnPhone). A page just made
 * asks for its title itself (requestPageTitleFocus) and takes nothing here.
 *
 * The caret comes with opening the page alone: one opened read only is read
 * then too and takes none when it turns editable where it stands — its
 * access raised by its owner, its lock or the person's suggesting ended — as
 * Notion's (seen 2026-11-15 in Chrome 1440, no script in the page before the
 * change: a guest's empty page raised from 읽기 to 편집 허용 showed no caret,
 * and a letter typed went nowhere; Leafen's took it into the title, which
 * changed for everyone).
 */
export function useEmptyPageTitleCaret(pageId: string, readOnly: boolean, titleRef: RefObject<HTMLElement | null>) {
  const read = useRef<string | null>(null);
  // When the page was first drawn here: what the person did after it, while
  // its body loaded, is theirs (actedSince.ts).
  const shown = useRef<{ pageId: string; at: number; made: boolean } | null>(null);
  if (shown.current?.pageId !== pageId) shown.current = { pageId, at: performance.now(), made: false };
  // The mark of a page just made is let go as the opening it was for ends (titleFocusAsked.ts).
  useEffect(() => () => forgetTitleFocusAsked(pageId), [pageId]);
  // A phone's head goes away in the frame the page is first drawn in (headAwayAtFirstDraw); not a page just made here.
  useLayoutEffect(() => {
    const opening = shown.current!;
    if (!readOnly && !(opening.made || (opening.made = takeTitleFocusAsked(pageId)))) headAwayAtFirstDraw(opening, titleRef.current);
  }, [pageId, readOnly, titleRef]);
  useEffect(() => {
    // Whether this opening is the one the page was just made in: its mark, taken once it comes, is kept for it.
    const opening = shown.current!;
    const madeHere = () => opening.made || (opening.made = takeTitleFocusAsked(pageId));
    madeHere();
    if (read.current === pageId) return;
    const scope = titleRef.current?.closest<HTMLElement>("[data-page-search-root]") ?? document.body;
    let frame = 0, tries = 0, settle = 0;
    // Opened in the app on a pointer's screen, the caret it may take is asked for now: what is typed while
    // its body loads waits for it and is typed there, or goes back where it was if it takes none
    // (caretOnceAnswered) — 2026-11-21: a letter typed right after an empty page was pressed in the
    // sidebar went nowhere, and the page took no caret for it. A press meanwhile keeps its own focus.
    // A page just made asks for its title itself (requestPageTitleFocus) and is not asked here: its
    // title may have the caret already, and this ask took it away into the hold (2026-11-22: the
    // title typed right after the sidebar's "새로 만들기" › "페이지" or ⌘N was lost). A touch screen
    // asks nothing: its opening takes no caret of itself (openingTakesTitleCaret, as every opening).
    const pointer = openingTakesTitleCaret();
    const asked = pointer && !readOnly && openedInApp() && !holdingTyping() && !pageTitleFocusAsked(pageId) && !madeHere()
      ? caretOnceAnswered(() => titleRef.current?.closest("[data-page-peek], [data-row-peek-panel], [data-page-search-root]")) : null;
    const typing = () => {
      const active = document.activeElement;
      return active instanceof HTMLElement && (active.isContentEditable || active.matches("input, textarea, select"));
    };
    // The title's editor may come into its place a few frames after the body.
    const caret = (then?: (title: HTMLElement) => void) => {
      if (typing()) return;
      const title = titleRef.current;
      if (!title?.isContentEditable || shutBehindModal(title) || !placeCaret(title, "end")) {
        if (tries++ < 30) frame = window.requestAnimationFrame(() => caret(then));
        return;
      }
      then?.(title);
    };
    // The body tells once it has loaded and can be written: its starter shows
    // (nothing in it) or it holds blocks; the title is read then. One only
    // read is read once it has loaded, its starter never showing.
    const decide = () => {
      const surface = scope.querySelector<HTMLElement>(`[role="region"][data-editor-page="${CSS.escape(pageId)}"]`);
      if (!surface || surface.getAttribute("aria-busy") === "true" || read.current === pageId) return;
      const empty = surface.dataset.pageStarterVisible === "true";
      if (!readOnly && !empty && surface.dataset.pageBodyPopulated !== "true") return;
      read.current = pageId;
      observer.disconnect();
      // A page just made here took the caret where its ask put it, and what
      // the person did since the page showed is theirs (titleFocusAsked.ts).
      if (madeHere()) { asked?.("kept"); return; }
      if (readOnly || !asked && actedSince(shown.current?.at ?? Number.POSITIVE_INFINITY)) return;
      const title = titleRef.current;
      const untitled = (title?.textContent ?? "").trim() === "";
      if (!openedInApp()) { asked?.(null); return; }
      if (pointer) {
        if (!empty && !untitled) asked?.(null);
        else if (asked) asked({ key: `title:${pageId}`, position: "end" });
        else caret();
      } else if (!typing()) {
        const phone = Boolean(title?.closest('[data-phone="true"]'));
        if (untitled) caret(phone ? (shown) => scrollPageHeadAwayOnPhone(opening, shown, true) : undefined);
        else if (phone && title) scrollPageHeadAwayOnPhone(opening, title, false);
      }
    };
    const check = () => { window.clearTimeout(settle); settle = window.setTimeout(decide, SETTLE_MS); };
    const observer = new MutationObserver(check);
    observer.observe(scope, { subtree: true, childList: true, attributes: true,
      attributeFilter: ["aria-busy", "data-editor-page", "data-page-starter-visible", "data-page-body-populated"] });
    check();
    return () => { observer.disconnect(); window.clearTimeout(settle); window.cancelAnimationFrame(frame); asked?.(null); };
  }, [pageId, readOnly, titleRef]);
}

// The openings whose head a phone has scrolled away, which are not scrolled again.
const headAway = new WeakSet<object>();

/**
 * Scrolls a phone's page opened in the app as Notion's phone does (seen
 * 2026-11-10, 402pt): without a cover, until its head's option buttons stand
 * under the top bar (44, an icon then 12 under the bar); with the caret in an
 * untitled title, no further than a page just made stands, its title where
 * the option row began (40 without an icon). A page with a cover stays at its
 * top, the cover showing. Once an opening, whichever asks first: the frame
 * the page is first drawn in (headAwayAtFirstDraw) or its body read; `whole`
 * only where the page can scroll that far now.
 */
function scrollPageHeadAwayOnPhone(opening: object, title: HTMLElement, caret: boolean, whole = false) {
  const scroller = title.closest<HTMLElement>("[data-page-scroll]");
  const head = title.closest<HTMLElement>("[data-page-header-root], [data-row-page-header]");
  const options = head?.querySelector<HTMLElement>("[data-page-header-controls], [data-row-page-header-controls]");
  const button = options?.querySelector<HTMLElement>("[data-page-header-control]");
  if (!scroller || !head || !options || !button || scroller.scrollTop > 0 || head.dataset.hasCover === "true"
    || headAway.has(opening)) return;
  const away = button.getBoundingClientRect().bottom - scroller.getBoundingClientRect().top;
  const madePlace = title.getBoundingClientRect().top - options.getBoundingClientRect().top;
  const by = caret ? Math.min(away, madePlace) : away;
  if (by <= 0 || whole && scroller.scrollHeight - scroller.clientHeight < by) return;
  scroller.scrollTop += by;
  headAway.add(opening);
}

/**
 * A phone's page opened in the app has its head away from the frame it is
 * first drawn in, before that frame shows, as Notion's phone opens it (seen
 * 2026-11-25 on a Galaxy A90 in Chrome at 411, two tabs, frames recorded:
 * from the page block or link pressed, a loading bar, then the page's first
 * frame with its title under the top bar), not once its body is read and
 * has settled (SETTLE_MS). An untitled one goes as far as its caret's place;
 * one with a cover stays at its top, and one that can't scroll that far yet
 * goes once its body is read.
 * By its address, on any other screen or in a side peek's compact head, nothing.
 */
function headAwayAtFirstDraw(opening: object, title: HTMLElement | null | undefined) {
  if (!title?.closest('[data-phone="true"]') || !openedInApp()) return;
  // Once the drawing it is part of is put in whole, before that frame shows: the page's scroll takes
  // its height as it marks the page area around it (pageScrollHost.ts), after the head's own effects.
  queueMicrotask(() => {
    const active = document.activeElement;
    if (!title.isConnected || active instanceof HTMLElement && (active.isContentEditable || active.matches("input, textarea, select"))) return;
    scrollPageHeadAwayOnPhone(opening, title, (title.textContent ?? "").trim() === "", true);
  });
}

/**
 * A database page a phone opens in the app scrolls its head away as a page's
 * does (Notion's, seen 2026-11-14 at 402pt: one opened from its table 44, its
 * option buttons under the top bar; a new one from the table's "+" 40, the
 * caret in its title where the option row began). By its address it stays
 * at its top. Read once a page, when its body has loaded.
 */
export function useRowPageHeadAwayOnPhone(rowId: string, headRef: RefObject<HTMLElement | null>) {
  // This opening of the row: its head away in the frame it is first drawn in where it can be
  // (headAwayAtFirstDraw), else once its body is read.
  const opening = useRef<{ rowId: string }>({ rowId });
  if (opening.current.rowId !== rowId) opening.current = { rowId };
  useLayoutEffect(() => {
    const head = headRef.current;
    if (head && head.dataset.compact !== "true") headAwayAtFirstDraw(opening.current, head.querySelector<HTMLElement>("[data-row-page-title]"));
  }, [rowId, headRef]);
  useEffect(() => {
    const head = headRef.current;
    const scope = head?.closest<HTMLElement>("[data-page-scroll]");
    if (!head || !scope || !head.closest('[data-phone="true"]') || head.dataset.compact === "true" || !openedInApp()) return;
    let settle = 0;
    const decide = () => {
      const surface = scope.querySelector<HTMLElement>(`[role="region"][data-editor-page="${CSS.escape(rowId)}"]`);
      if (surface?.getAttribute("aria-busy") === "true") return;
      observer.disconnect();
      const title = head.querySelector<HTMLElement>("[data-row-page-title]");
      if (title) scrollPageHeadAwayOnPhone(opening.current, title, (title.textContent ?? "").trim() === "");
    };
    const check = () => { window.clearTimeout(settle); settle = window.setTimeout(decide, SETTLE_MS); };
    const observer = new MutationObserver(check);
    observer.observe(scope, { subtree: true, childList: true, attributes: true, attributeFilter: ["aria-busy", "data-editor-page"] });
    check();
    return () => { observer.disconnect(); window.clearTimeout(settle); };
  }, [rowId, headRef]);
}
