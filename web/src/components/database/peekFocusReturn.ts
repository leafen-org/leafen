import type { RefObject } from "react";
import { caretOnceAnswered } from "@/components/editor/focus";
import { actedSince } from "@/lib/actedSince";
import { keyCameLast, keysLedFocus } from "@/lib/lastPress";
import { holdingTyping } from "@/lib/pageTitleFocus";
import { shutBehindModal } from "@/lib/modalFocus";
import { touchScreen } from "@/lib/screenKind";

// Whether anything has been pressed or typed in this tab yet: a peek open
// before that came with the tab's own address.
let acted = false;
if (typeof document !== "undefined") {
  const note = () => { acted = true; };
  document.addEventListener("pointerdown", note, { capture: true, once: true });
  document.addEventListener("keydown", note, { capture: true, once: true });
}

// When each peek's panel began taking its focus: its opening, or a caret
// asked of its title. That focus comes a frame or more on, and what the
// person did in the meantime is theirs (actedSince.ts).
const focusAskedAt = new WeakMap<Element, number>();
function actedSinceFocusAsked(panel: Element) {
  const asked = focusAskedAt.get(panel);
  return asked !== undefined && actedSince(asked);
}

/**
 * Takes a row peek's focus as it opens, for its closing (DatabaseRowPage):
 * the focus goes back where a key left it, and only while it has nowhere
 * else to be — on the page itself, or still in the peek. A peek a press
 * opened closes to nothing chosen, as Notion's (2026-11-15, Chrome, no script
 * before the action; judged by the screen and one typed letter: a row's
 * "열기" pressed, Escape — no selection, the letter landing nowhere): text being
 * written shows its focus with no key behind it, and what a press beside the
 * peek took — a cell's field, a line of the page — keeps it (2026-11-12: a
 * peek opened from a row's "열기" gave the page's text the focus as it
 * closed, out of the title cell just pressed beside it).
 */
export function peekFocusReturn(panel: RefObject<HTMLElement | null>): () => void {
  if (panel.current) focusAskedAt.set(panel.current, performance.now());
  const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const back = keyCameLast() && keysLedFocus(active) ? active : null;
  return () => {
    const peek = panel.current;
    window.requestAnimationFrame(() => {
      const now = document.activeElement;
      const lost = !now || now === document.body || !now.isConnected || Boolean(peek?.contains(now));
      if (back?.isConnected && lost) back.focus();
    });
  };
}

/**
 * Whether a field of an opening peek — its title, its body — already has the
 * focus, which it keeps. A modal layer gives a center peek's first button the
 * focus as it opens (overlayLayer.ts), which wore its ring when the peek was
 * opened by its address; Notion's opens on its title, or itself, with no ring
 * (seen 2026-11-11): the peek takes that from the button. So does a peek the
 * person pressed, typed or pasted in after it began opening (blocks pasted
 * in chosen, no field focused): its opening leaves the focus where they did.
 */
export function peekFieldFocused(panel: HTMLElement | null): boolean {
  return Boolean(panel && actedSinceFocusAsked(panel)) || peekFieldHasFocus(panel);
}

/** Whether a field of a peek — its title, its body — has the focus now. */
export function peekFieldHasFocus(panel: HTMLElement | null): boolean {
  const active = document.activeElement;
  return Boolean(panel && active && active !== panel && panel.contains(active)
    && active.closest("[contenteditable='true'], input, textarea, select"));
}

/**
 * Whether a page or row opening — a peek, a row's page opened full, a page
 * peek, a page (emptyPageTitleCaret.ts) — puts the caret in its title of
 * itself. A touch screen's leaves the focus on the page, no keyboard rising,
 * as Notion's iPad (seen 2026-11-14 in iPad Safari: a row's page opened from
 * its "열기", body empty, had no focus or range), but for an untitled page,
 * as Notion's phone; a row or page just made still asks for its title
 * (pageTitleFocus), as Notion's "+ 새 페이지" put the caret in the new row's
 * empty title there.
 */
export function openingTakesTitleCaret(): boolean {
  return !touchScreen();
}

// How long an opening peek's title is waited for.
const TITLE_WAIT_MS = 3000;
// The title of a peek's page or row, before anything else of it.
export const PEEK_TITLE = "[data-row-page-title], [data-page-title-editor='true']";

/**
 * Puts the caret at the end of an opening peek's title, as Notion's (seen
 * 2026-11-13 at 1440×723): a center one's always, a side one's opened from
 * the page — a block's menu, a press with ⌥ — while one the tab's own
 * address opened beside the page takes no focus. The title can come a few
 * frames after the peek (`wait`: no sooner than the next), editable once the
 * page has loaded; what is typed meanwhile waits for it and is typed there
 * (caretOnceAnswered — 2026-11-21: a letter typed while it loaded made the
 * caret give up, and went nowhere). A field of it, or anything outside it,
 * the reader focuses first, or a press meanwhile, keeps it. Turned between
 * side and center by its view options, it takes the caret there again (seen
 * 2026-11-13 on a row's); `panel` is then read as it is drawn. Returns what
 * stops the wait.
 */
export function caretAtPeekTitle(panel: HTMLElement | null | (() => HTMLElement | null), title: string, center: boolean, wait = false): () => void {
  const peek = () => (typeof panel === "function" ? panel() : panel);
  const opening = peek();
  if (!opening || (!center && !acted) || !openingTakesTitleCaret()) return () => undefined;
  let frame = 0;
  const started = performance.now();
  focusAskedAt.set(opening, started);
  const land = caretOnceAnswered(peek);
  const place = () => {
    const shown = peek(), active = document.activeElement;
    if (!shown?.isConnected) { land(null); return; }
    if (peekFieldHasFocus(shown) || (active && active !== document.body && !shown.contains(active) && !holdingTyping())) { land("kept"); return; }
    const element = shown.querySelector<HTMLElement>(title);
    if (element?.isContentEditable && !shutBehindModal(element)) land({ field: element, position: "end" });
    else if (performance.now() - started < TITLE_WAIT_MS) frame = window.requestAnimationFrame(place);
    else land(null);
  };
  if (wait) frame = window.requestAnimationFrame(place); else place();
  return () => { window.cancelAnimationFrame(frame); land(null); };
}
