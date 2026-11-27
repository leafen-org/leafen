import { imeComposing } from "./fieldKeyBoundary";
// Home and End in a text field, in Chrome on a Mac. The Mac's Home and End
// scroll what holds the caret to its start or end (the editing command
// scrollToBeginningOfDocument a real key brings, and Playwright's press
// too); Chrome moves the caret to the line's start or end only where
// nothing takes that scroll. A box whose overscroll-behavior isn't `auto`
// takes it though it cannot scroll: a layer over the page has `contain` on
// its own box (styles.css), and so do many menus' boxes, so in quick find,
// "위치 가져오기"'s search and any field in a window or a menu the keys did
// nothing (2026-11-18: quick find's caret stayed at the end, the field and
// its boxes contained; fields outside a layer and a plain page's moved). As
// Notion's fields in Chrome — Home, then one letter typed, put the letter at
// the field's start, the lit row staying lit (seen 2026-11-18 at 1440, dpr 1:
// quick find (⌘P) in two tabs, "위치 가져오기" once; no box around their
// fields contains its scroll) — the caret goes there where only such a box
// took the key: once the key has gone through the page's own handlers and
// none of them took it, and nothing it would scroll could. Elsewhere
// Chrome's own way stands (a list holding the field that can still scroll,
// scrolled; Shift, which chooses, never a scroll). Other browsers and the
// desktop app (WebKit) keep their own keys.

const KEYS = new Set(["Home", "End"]);
// Fields of one line whose text the caret moves through.
const TEXT_INPUTS = new Set(["text", "search", "email", "url", "tel", "password"]);

let watching = false;

// Chrome, and the browsers built on it (Edge's and a headless Chrome's names hold "Chrome/" too).
function chromeOnMac() {
  if (typeof navigator === "undefined") return false;
  return /mac/i.test(navigator.platform ?? "") && /Chrome\//.test(navigator.userAgent ?? "");
}

/** The field the key moves in: a one-line text input, a textarea, or the text being written (contenteditable). */
function fieldOf(target: EventTarget | null): HTMLElement | null {
  if (target instanceof HTMLInputElement) return TEXT_INPUTS.has(target.type) ? target : null;
  if (target instanceof HTMLTextAreaElement || target instanceof HTMLElement && target.isContentEditable) return target;
  return null;
}

const scrollsBy = (overflow: string) => overflow === "auto" || overflow === "scroll";
const holdsScroll = (overflow: string) => overflow !== "visible" && overflow !== "clip";

/**
 * Whether Chrome's scroll from the field goes nowhere because a contained box
 * took it: walking out from the field, the first box with overscroll-behavior
 * other than `auto` that holds a scroll (or the input itself) comes before
 * anything that can still scroll that way. Read before the key's default
 * runs, as Chrome reads it.
 */
function containedWithoutScroll(field: HTMLElement, toStart: boolean) {
  const root = document.scrollingElement ?? document.documentElement;
  for (let box: Element | null = field; box; box = box.parentElement) {
    const style = getComputedStyle(box);
    const own = box === field;
    // An input scrolls its text across only; a textarea or written text may scroll down itself.
    if (!(own && field instanceof HTMLInputElement) && (scrollsBy(style.overflowY) || box === root)) {
      const room = toStart ? box.scrollTop > 0 : box.scrollTop + box.clientHeight < box.scrollHeight - 1;
      if (room) return false;
    }
    // An input's own containment takes it (its text box scrolls); other boxes' only where they hold a scroll.
    const holds = own && field instanceof HTMLInputElement || holdsScroll(style.overflowY) || holdsScroll(style.overflowX);
    if (style.overscrollBehaviorY !== "auto" && holds) return true;
  }
  return false;
}

function moveCaret(field: HTMLElement, toStart: boolean) {
  if (field instanceof HTMLInputElement) {
    const at = toStart ? 0 : field.value.length;
    field.setSelectionRange(at, at);
    return;
  }
  // The line the caret is on as drawn, as Chrome's own MoveToBeginningOfLine (a textarea's too).
  window.getSelection()?.modify("move", toStart ? "backward" : "forward", "lineboundary");
}

function keyDown(event: KeyboardEvent) {
  if (!KEYS.has(event.key) || event.shiftKey || event.altKey || event.ctrlKey || event.metaKey || imeComposing(event)) return;
  const field = fieldOf(event.target);
  const toStart = event.key === "Home";
  if (!field || !containedWithoutScroll(field, toStart)) return;
  // After the page's own handlers and Chrome's own default, which found no scroll.
  window.setTimeout(() => {
    if (event.defaultPrevented || document.activeElement !== field && !field.contains(document.activeElement)) return;
    moveCaret(field, toStart);
  }, 0);
}

/** Home and End move the caret of a field in a layer, in Chrome on a Mac, where nothing scrolls. */
export function watchFieldHomeEnd() {
  if (watching || typeof window === "undefined" || !chromeOnMac()) return;
  watching = true;
  window.addEventListener("keydown", keyDown, true);
}
