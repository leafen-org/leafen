import { afterNextPaint } from "@/lib/afterPress";

/**
 * Another page opened from a large one. Tearing the large page's body down
 * held the press's own frame (2026-11-13: a page of 38,000 elements, 121 to
 * 128 ms in the click before anything was drawn; user decision 2026-11-12, a
 * press answers on the next frame). So the press shows first — the row
 * pressed marked as the page opening, the row of the page being left no
 * longer — and the page changes over once that is painted. From a smaller
 * page it changes over in the press itself, as before. Every press opens what
 * it pressed, in order: one pressed meanwhile opens after it.
 */

// Elements in the page's place above which its teardown outlasts a frame
// (counting them takes well under a millisecond at 38,000).
const LARGE_PAGE_ELEMENTS = 6_000;

let waiting: Promise<void> = Promise.resolve();

function leavingLargePage() {
  const main = document.querySelector("[data-app-main]");
  return Boolean(main && main.getElementsByTagName("*").length > LARGE_PAGE_ELEMENTS);
}

/**
 * The page left lets go of the focus a link or button in it holds — the
 * press that opens the other page put it there — before it is taken down:
 * taken down holding it, Chrome worked out the styles and layout of all that
 * was there and then, in the middle of putting the other page in (a
 * sub-page's link above a table: 70–85 ms of the press's 160–200 at CPU ×2,
 * 2026-11-22). The focus ends on the body either way. A field or an editor
 * keeps its focus, to end it its own way.
 */
function pageLetsFocusGo() {
  const focused = document.activeElement;
  if (!(focused instanceof HTMLElement) || focused.isContentEditable || focused.matches("input, textarea, select")) return;
  if (focused.closest("[data-page-scroll]")) focused.blur();
}

function openNow(open: () => void) {
  pageLetsFocusGo();
  open();
}

/**
 * Opens a page from a press on `pressed`, past the press's frame when the page
 * left is large. The sidebar's row of the page it goes to, `toPageId`, shows
 * as opening meanwhile, as a press on that row shows it.
 */
export function openPastPaint(pressed: Element | null, open: () => void, toPageId?: string | null) {
  if (typeof document === "undefined") {
    open();
    return;
  }
  if (!leavingLargePage()) {
    openNow(open);
    return;
  }
  const left = [...document.querySelectorAll("[data-page-tree-item][aria-current='page']")].filter((row) => row !== pressed);
  const opening = toPageId ? [...document.querySelectorAll(`[data-page-tree-item][data-tree-page-id="${CSS.escape(toPageId)}"]`)] : [];
  if (pressed && !opening.includes(pressed)) opening.push(pressed);
  for (const row of opening) row.setAttribute("data-opening", "");
  for (const row of left) row.setAttribute("data-leaving", "");
  waiting = waiting.then(() => new Promise<void>((resolve) => {
    afterNextPaint(() => {
      try {
        openNow(open);
      } finally {
        for (const row of opening) row.removeAttribute("data-opening");
        for (const row of left) row.removeAttribute("data-leaving");
        resolve();
      }
    });
  }));
}

// The press being handled now — a pointer, a click or a key a person made —
// noted ahead of the page's own handlers, until its task ends.
let pressNow: Event | null = null;

function notePress(event: Event) {
  if (!event.isTrusted) return;
  if (!pressNow) {
    const channel = new MessageChannel();
    channel.port1.onmessage = () => {
      channel.port1.close();
      pressNow = null;
    };
    channel.port2.postMessage(null);
  }
  pressNow = event;
}

if (typeof window !== "undefined") {
  for (const type of ["pointerdown", "pointerup", "click", "keydown", "keyup"]) {
    window.addEventListener(type, notePress, { capture: true, passive: true });
  }
}

const PRESSED = "a[href], button, [role='link'], [role='button'], [role='option'], [role='menuitem'], [role='treeitem']";

/**
 * Opens another page or screen from whatever was pressed — a page link in
 * the body, a breadcrumb, a search result, a menu's "open" — past the press's
 * frame when the page left is large, the pressed link and the sidebar's row
 * of `toPageId` shown as opening first (openPastPaint). Outside a press — a
 * move after the server answered, a script's — it opens at once.
 */
export function openPressedPastPaint(open: () => void, toPageId?: string | null) {
  const press = pressNow;
  if (!press) {
    open();
    return;
  }
  const target = press instanceof KeyboardEvent || !(press.target instanceof Element) ? null : press.target.closest(PRESSED);
  // An editor's own elements stay as its document draws them: a mark there
  // would read as an edit to the page left.
  openPastPaint(target?.closest("[contenteditable]") ? null : target, open, toPageId);
}
