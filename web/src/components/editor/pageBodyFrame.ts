import { type BodyFocus, enterBodyFromTitle } from "@/lib/pageTitleFocus";
import { placeCaret } from "./focus";

/**
 * The frame a page's body stands in — a full page's, a peek's or a row
 * page's (`[data-page-scroll]`): the boxes from the body's editor out to the
 * frame's scroller, whose empty space beside, above and below the blocks is
 * the body's to take a press on, as Notion's frames are (seen 2026-11-11) —
 * while the body is drawn (`shown`). A row page's layout tab of a database
 * open in its place leaves the body kept unseen (RowPageLayoutTabs): a press
 * on the frame then is no press for it, its caret having nowhere to go, and
 * leaves the focus on the frame, where ⌘Z takes back the tab's last step, as
 * Notion's (2026-11-24: the body asked for a caret it never drew, and what
 * was typed meanwhile — ⌘Z too — was held for it and lost).
 */
export function pageBodyFrame(surface: HTMLElement): { margins: HTMLElement[]; headBottom: () => number; shown: () => boolean } {
  const scroller = surface.closest<HTMLElement>("[data-page-scroll]");
  const margins: HTMLElement[] = [];
  for (let node = surface.parentElement; scroller && node; node = node === scroller ? null : node.parentElement) margins.push(node);
  return {
    margins, headBottom: () => (scroller ? headBottom(surface, scroller) : Number.NEGATIVE_INFINITY),
    shown: () => drawn(surface),
  };
}

const drawn = (element: Element) => element.getClientRects().length > 0;

/** Whether a page's body (or what it draws) is kept unseen, a row page's layout tab of a database open in its place. */
export const bodyUnseen = (body: Element | null | undefined) => Boolean(body?.isConnected && !drawn(body));

/** Whether the body of the page a title heads is kept unseen. */
export const titleBodyUnseen = (title: Element | null) => bodyUnseen(title?.closest("[data-page-scroll]")?.querySelector('[role="region"][data-editor-page]'));

/**
 * A title's Enter: into its page's body, which may still be opening, what
 * is typed meanwhile waiting for it (enterBodyFromTitle). A body kept unseen
 * takes no caret, as it takes no press: the Enter makes its new first line
 * there unseen and the caret goes to the title's start, where what is typed
 * next goes at once. Notion's (seen 2026-11-24 in four Chrome tabs, one
 * letter typed after the key with no script in the page before it; a row
 * page's relation tab open, Enter at the title's end) made the unseen line
 * each time (7 of 7) and the letter went to the title's start in three of
 * five tries, two tabs; in the other two (after a ⌘Z and ⌘⇧Z of such a line,
 * after ↓) it went nowhere, which is not followed: a key is never lost. Its
 * ⌘Z and ⌘⇧Z of the line left the caret at the title's start in one tab and
 * its end in another: here the caret stays in the title where it is
 * (nativeTiptapController). Leafen's held what was typed for a caret that
 * never came, ⌘Z with it, and gave it to the title's end seconds on.
 */
export function enterBodyOrTitleStart(title: HTMLElement | null, focusBody: (selection: BodyFocus) => void) {
  if (!title || !titleBodyUnseen(title)) {
    enterBodyFromTitle(title, focusBody);
    return;
  }
  focusBody("unseen");
  placeCaret(title, "start", false);
}

/**
 * Whether a press lands on a page's room: its frame's empty space beside,
 * above and below its head and blocks (the boxes from its body's editor out to
 * the frame's scroller, above), or under its last block — not on the head, a
 * block or anything drawn in one.
 */
export function pressOnPageRoom(target: Element): boolean {
  if (target.closest("[data-editor-tail]")) return true;
  const surface = target.closest("[data-page-scroll]")?.querySelector('[role="region"][data-editor-page]');
  return Boolean(surface && target.contains(surface));
}

/**
 * Where the page's head ends: the foot of what the frame draws before the
 * body in its flow — its cover, icon, title, properties and discussion.
 */
function headBottom(surface: HTMLElement, scroller: HTMLElement) {
  let bottom = Number.NEGATIVE_INFINITY;
  for (let node: Element | null = surface; node && node !== scroller; node = node.parentElement) {
    for (let before = node.previousElementSibling; before; before = before.previousElementSibling) {
      const rect = before.getBoundingClientRect();
      const position = window.getComputedStyle(before).position;
      if (rect.height > 0 && position !== "absolute" && position !== "fixed") bottom = Math.max(bottom, rect.bottom);
    }
  }
  return bottom;
}
