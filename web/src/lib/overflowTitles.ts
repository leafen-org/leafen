// Text cut off with an ellipsis shows all of itself in a tooltip on hover
// (2026-10-16: two servers' addresses cut alike in the switcher, an app's
// action name cut in a page's menu, with no way to read them): browsers show
// none for an ellipsis. An element that names itself with its own title
// keeps it; one whose text fits again loses the tooltip it was given. Text an
// editor draws gets none, editable or not (a read-only editor's root is
// `contenteditable="false"`): the editor draws again what it did not write,
// the new text under the pointer asked for the tooltip again, and so on
// without end, so a press never landed (2026-11-13: a database's cut name in
// a page could not be pressed into). Pressed, an editable one shows its whole
// self, and Notion gives its cut names none (seen 2026-11-13).
//
// It looks after the pointer's next frame is painted, at the latest element
// the pointer came over: measuring in the pointer's own event made the
// browser work out the whole page's new hover styles there and then
// (2026-11-12: 60–100 ms each time the pointer came into a table of 30 rows
// by 40 properties). A tooltip shows only after the pointer rests, so a frame
// later changes nothing on screen.
//
// The page moving under a resting pointer (a side peek sliding shut from
// under it, a scroll) brings it over other elements with the pointer where
// it last moved: those wait for the pointer to move again, so nothing is
// measured while the page moves (2026-11-14: a side peek sliding shut from
// under the pointer on its "×" had the look work out some 310 elements'
// styles in its slide, 5–6 ms, four closes in six, NAS condition, CPU ×2).

import { afterNextPaint } from "./afterPress";

const GIVEN = "data-overflow-title";
const DEPTH = 3;

function cut(element: HTMLElement) {
  const style = getComputedStyle(element);
  if (style.textOverflow === "ellipsis" && element.scrollWidth > element.clientWidth + 1) return true;
  return style.webkitLineClamp !== "none" && style.webkitLineClamp !== "" && element.scrollHeight > element.clientHeight + 1;
}

/** Whether an editor draws the element: inside editable text, or a read-only editor's. */
function editorDrawn(element: HTMLElement) {
  const owner = element.closest<HTMLElement>("[contenteditable]");
  return Boolean(owner && (owner.isContentEditable || owner.classList.contains("ProseMirror")));
}

function showWhole(target: HTMLElement) {
  if (editorDrawn(target)) return;
  let element: HTMLElement | null = target;
  for (let depth = 0; element && depth < DEPTH; depth += 1, element = element.parentElement) {
    const given = element.hasAttribute(GIVEN);
    if (element.hasAttribute("title") && !given) return;
    if (!cut(element)) {
      if (given) { element.removeAttribute("title"); element.removeAttribute(GIVEN); }
      continue;
    }
    const text = element.textContent?.replace(/\s+/gu, " ").trim() ?? "";
    if (text && element.getAttribute("title") !== text) {
      element.setAttribute("title", text);
      element.setAttribute(GIVEN, "");
    }
    return;
  }
}

let latest: HTMLElement | null = null;
let looking = false;

function lookAfterPaint(target: EventTarget | null) {
  latest = target instanceof HTMLElement ? target : null;
  if (looking || !latest) return;
  looking = true;
  afterNextPaint(() => {
    looking = false;
    const target = latest;
    latest = null;
    if (target?.isConnected) showWhole(target);
  });
}

// Where the pointer last moved to, and whether the page moved under it since.
let pointerX = Number.NaN, pointerY = Number.NaN;
let pageMoved = false;

// A finger has no hover for a tooltip to show on: its press looks at
// nothing, so a touch on a phone or a tablet runs none of this (2026-11-14 on
// a Galaxy A90: each press came over its target and looked after its paint).
function cameOver(event: PointerEvent) {
  if (event.pointerType === "touch") return;
  pageMoved = event.clientX === pointerX && event.clientY === pointerY;
  // What a look on its way was for has moved from under the pointer.
  if (pageMoved) latest = null;
  else lookAfterPaint(event.target);
}

function pointerMoved(event: PointerEvent) {
  if (event.pointerType === "touch") return;
  pointerX = event.clientX; pointerY = event.clientY;
  if (!pageMoved) return;
  pageMoved = false;
  lookAfterPaint(event.target);
}

let watching = false;

/** Gives cut-off text a tooltip of its whole, everywhere in the app. */
export function watchOverflowTitles() {
  if (watching || typeof document === "undefined") return;
  watching = true;
  document.addEventListener("pointerover", cameOver, { capture: true, passive: true });
  document.addEventListener("pointermove", pointerMoved, { capture: true, passive: true });
}
