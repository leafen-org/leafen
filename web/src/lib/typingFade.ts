import { touchScreen } from "@/lib/screenKind";
import { afterNextPaint } from "./afterPress";
import { fieldEnter } from "./fieldKeyBoundary";

// A key pressed while writing a page's title or text puts the page's chrome
// away until the mouse moves, as Notion's (seen 2026-10-31): its top bar
// fades out over 0.7s and its block handles go, and they come back as the
// mouse moves — not as the page scrolls under a still one. Keys in a menu or
// a dialog (a row's page opened over the page aside) leave them, and so does a touch screen, with no mouse to bring
// them back. The document carries it as `data-typing` for the styles:
// "true" on a page, "title" on a full-page database, whose bar stays.

// A row page's title is a page's title too (2026-11-05: its row of
// "아이콘 추가 · 커버 추가 · 댓글 추가" stayed under a still pointer while one wrote).
// So is a comment's box (CommentRichField), wherever it sits: Notion's bar
// went at a key in the page's comment box under its title (seen 2026-11-15
// and 2026-11-16 in Chrome, after the first letter and after Enter sent it),
// in a new comment's card beside chosen text and in the reply box of the
// discussions panel (each seen once, 2026-11-16).
const COMMENT_FIELD = "[data-comment-field]";
const WRITING = `[data-native-editor-content], [data-page-header-root] h1, [data-row-page-title], ${COMMENT_FIELD}`;

// With blocks chosen nothing holds the focus, and a press in the page's
// margin leaves it on the page's area: keys there are the page's too (Notion's,
// seen 2026-11-06: ↓ with a block chosen or after a margin press, ⌘Z after a
// block's deletion from its menu).
function writing(target: Element) {
  return !!target.closest(WRITING) || (target === document.body || target.matches("[data-app-main]"))
    && !!document.querySelector("[data-app-main] [data-native-editor-content]");
}

// Only where a key writes: on a locked page, or one only read, Notion keeps
// its bar while keys are pressed in its text (seen 2026-11-06). With blocks
// chosen, the page's own text says whether it is written.
function writable(target: Element) {
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) return !target.readOnly && !target.disabled;
  if (target instanceof HTMLElement && target.isContentEditable) return true;
  return (target === document.body || target.matches("[data-app-main]"))
    && document.querySelector("[data-app-main] [data-native-editor-content]")?.getAttribute("contenteditable") === "true";
}

// A key that is only a modifier, which writes nothing by itself.
const MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta"]);
// Keys that, with nothing holding the focus, only move the page.
const PAGE_MOVES = new Set(["Home", "End", "PageUp", "PageDown"]);

// A key that writes or takes away text where the focus is, with no ⌘, Ctrl or Option.
function writesWithoutModifier(event: KeyboardEvent) {
  return !event.metaKey && !event.ctrlKey && !event.altKey
    && (event.key.length === 1 || fieldEnter(event) || event.key === "Backspace" || event.key === "Delete");
}

// A press on a block's own frame, which holds no text (a picture, its size
// handles), chooses that block away from the text: keys with nothing holding
// the focus then write nothing, and the bar stays through Delete on it, ⌘Z
// after that, or a letter after a handle's drag. ↑ or ↓ moves the page's
// choice as a key in its text does and puts the bar away. A block chosen by a
// key in the text (Escape) stays the text's, its Delete putting the bar away.
// Notion's, seen 2026-11-12 and twice on 2026-11-15 in fresh Chrome tabs with
// no script before the key, judged by the screen after it (a picture; other
// frames not compared). A database in the page is left out: ⌘Z after a card's
// drag in one put Notion's bar away (seen once).
const FRAME = "[data-native-editor-content] figure";
const CHOICE_KEYS = new Set(["ArrowUp", "ArrowDown"]);
const LAYERS = '[role="dialog"], [role="menu"], [role="listbox"]';
// A block the page's choice holds (EditorBlockSelection).
const CHOSEN_BLOCK = '[data-app-main] [data-native-editor-content] [data-selected="true"]';
let pressedOnFrame = false;

let watching = false;
// Set as the browser goes back or forward, cleared by the next key or press.
let traversed = false;

// A block's row (EditorBlocks): its handles and "💬+" show only for the row
// the pointer is on or beside, so a key puts away only theirs, each row
// marked `data-typing-pointed` (EditorBlockHandles). The document's own flag
// on every row's handles restyled all of a long page's at the first key
// after a move (2026-11-14: 600 blocks, about 60 ms at CPU ×2).
const POINTED_ROW = '[role="group"][data-type]';
// What the pointer is on, the row it is beside in the page's room
// (useBlockHoverReach), and the rows marked.
let pointedAt: Element | null = null;
let besideRow: Element | null = null;
const marked = new Set<Element>();

function unmark() {
  for (const row of marked) row.removeAttribute("data-typing-pointed");
  marked.clear();
}

function markPointed() {
  const rows = new Set<Element>();
  for (let at = pointedAt; at; at = at.parentElement) if (at.matches(POINTED_ROW)) rows.add(at);
  if (besideRow?.isConnected) rows.add(besideRow);
  for (const row of marked) if (!rows.has(row)) { row.removeAttribute("data-typing-pointed"); marked.delete(row); }
  for (const row of rows) if (!marked.has(row)) { row.setAttribute("data-typing-pointed", ""); marked.add(row); }
}

/** The row the pointer is beside in the page's room, or none (useBlockHoverReach). */
export function notePointerBeside(row: Element | null) {
  besideRow = row;
  if (document.documentElement.dataset.typing === "true") markPointed();
}

/**
 * Shows the page's chrome as another page opens, whatever was typed to get
 * there (Notion's "/페이지"), but not on one reached by going back or forward:
 * Notion keeps its bar away there as the keys left it (seen 2026-11-07, ⌘[).
 */
export function pageOpened() {
  if (traversed) {
    traversed = false;
    return;
  }
  delete document.documentElement.dataset.typing;
}

/** Starts putting the page's chrome away while someone writes. */
export function watchTypingFade() {
  if (watching || typeof window === "undefined") return;
  watching = true;
  const root = document.documentElement;
  let last: { x: number; y: number } | null = null;
  window.addEventListener("popstate", () => { traversed = true; });
  // A modifier held alone (the ⌘ of a ⌘V) is looked at once its frame is
  // painted, with the key after it looking for itself meanwhile: its look
  // went through the whole document and laid it out again ahead of the key
  // it starts, some 6 ms with blocks chosen in a 1,031-block page at CPU ×2
  // (2026-11-15). Its chrome goes as it did, a frame on.
  let modifierLook: (() => void) | null = null;
  window.addEventListener("keydown", (event) => {
    traversed = false;
    // Tab with nothing holding the focus and no block chosen only moves it
    // on — to the top bar's first crumb from its empty middle — and the bar
    // stays to show where, as Notion's (seen 2026-11-16 in Chrome, no script
    // before the key: the top bar's middle pressed, then Tab, the first crumb
    // ringed in the bar; and 2026-11-15, Tab, ⇧Tab and Enter on the bar).
    // Over chosen blocks Tab moves them in or out, as a key in the text does.
    // A key that writes (a letter, Space, Enter, Backspace, Delete) goes
    // nowhere then either, and the bar stays: Notion's did at a letter on a
    // page opened with nothing holding the focus, and on one whose reader was
    // let edit it while open (seen 2026-11-16 and 2026-11-15 in Chrome, no
    // script before the key, the letter written nowhere). So do the keys that
    // only move the page as the browser does — Home, End, Page Up and Page
    // Down, with Ctrl or ⌘ or not — and a modifier held alone, whose chord's
    // key then decides: Notion's bar stayed through End, Page Down, Ctrl+End
    // and Ctrl, ⌘ or Shift held alone after a press above the title, and went
    // at ⌘↓ and ⌘↑, which chose the page's last or first block (seen twice
    // 2026-11-24 in Chrome on a Mac, fresh tabs, no script before the key, a
    // letter after it written nowhere).
    if ((event.key === "Tab" || writesWithoutModifier(event) || PAGE_MOVES.has(event.key) || MODIFIERS.has(event.key))
      && (event.target === document.body || event.target instanceof Element && event.target.matches("[data-app-main]"))
      && !document.querySelector(CHOSEN_BLOCK)) return;
    if (CHOICE_KEYS.has(event.key) || event.target instanceof HTMLElement && event.target.isContentEditable) pressedOnFrame = false;
    modifierLook?.();
    modifierLook = null;
    if (MODIFIERS.has(event.key)) {
      const target = event.target;
      modifierLook = afterNextPaint(() => { modifierLook = null; typed(target); });
      return;
    }
    typed(event.target);
  }, { capture: true, passive: true });
  const typed = (pressed: EventTarget | null) => {
    const target = pressed instanceof Element ? pressed : null;
    // A table's cell is no page text, though its database sits in one: Notion
    // keeps its bar while a cell is written in (seen 2026-11-06). A row's page
    // open over the page, side or center, is page text though it is a dialog:
    // Notion fades the page's bar behind it and keeps the peek's own (seen 2026-11-07).
    // So is a page's peek, the window a page made with a row's "+" opens in:
    // its keys put its title's "아이콘 추가 …" row away, and the Escape that
    // closes it leaves the page's bar away until the mouse moves (Notion's,
    // seen 2026-11-14).
    const layer = target?.closest('[role="dialog"]');
    const rowPage = target?.closest("[data-row-page], [data-page-peek]");
    // With the focus fallen to the page while a panel or menu is open (its
    // screen changed under a press), a key is still that layer's: Escape
    // closing a view's settings keeps the bar (Notion's, seen 2026-11-07).
    const loose = target === document.body || target?.matches("[data-app-main]");
    // A comment's box writes wherever it is, a window or a cell's discussion too.
    const comment = !!target?.closest(COMMENT_FIELD);
    if (touchScreen() || !target || loose && pressedOnFrame || !writing(target) || !writable(target)
      || !comment && (layer && !(rowPage && layer.contains(rowPage)) || target.closest('[role="gridcell"]'))) return;
    // Writing a full-page database's name puts away only the "아이콘 추가 ·
    // 커버 추가 …" row over it; its top bar stays (Notion's, seen 2026-11-06).
    // So do both bars while a row's page open over a full-page database (side,
    // center, a template's) is written in (Notion's, seen 2026-11-08).
    const overDatabase = layer && !!document.querySelector('[data-app-main] [data-database-page="true"]');
    const typing = target.closest('[data-database-page="true"]') || overDatabase ? "title" : "true";
    // Open layers are looked for only when the key would change the flag: the
    // look goes through the whole document, some 3 ms of each key with blocks
    // chosen in a 1,031-block page at CPU ×2 (2026-11-15), and the ⌘ of a ⌘V
    // has set the flag by its V, a frame after it. A full-page database's own
    // keys with nothing holding the focus keep its bar too, as its name's do:
    // Notion's stayed through ⌘Z after a card's drag (2026-11-12) and through
    // Delete on a chosen card and ⌘Z after it (2026-11-15), each in Chrome.
    if (loose && root.dataset.typing !== typing && (document.querySelector('[data-app-main] [data-database-page="true"]')
      || [...document.querySelectorAll(LAYERS)].some((open) => open.getClientRects().length > 0))) return;
    root.dataset.typing = typing;
    if (typing === "true") markPointed();
  };
  const show = (event: PointerEvent) => {
    if (event.type === "pointerdown") {
      traversed = false;
      const pressed = event.target instanceof Element ? event.target : null;
      if (!pressed?.closest(LAYERS)) pressedOnFrame = !!pressed?.closest(FRAME) && !(pressed instanceof HTMLElement && pressed.isContentEditable);
    }
    pointedAt = event.target instanceof Element ? event.target : null;
    const moved = event.type === "pointerdown" || !last || last.x !== event.clientX || last.y !== event.clientY;
    last = { x: event.clientX, y: event.clientY };
    // A modifier's look waiting for its frame is the pointer's now.
    if (moved && modifierLook) {
      modifierLook();
      modifierLook = null;
    }
    if (moved && root.dataset.typing) delete root.dataset.typing;
  };
  window.addEventListener("pointermove", show, { capture: true, passive: true });
  window.addEventListener("pointerdown", show, { capture: true, passive: true });
  // The page moving under a still pointer as one writes (a line made above
  // it) puts it on another row, whose handles stay away too.
  window.addEventListener("mouseover", (event) => {
    pointedAt = event.target instanceof Element ? event.target : null;
    if (root.dataset.typing === "true") markPointed();
  }, { capture: true, passive: true });
  // However the flag goes (the pointer, another page, ⌘⇧O's bar), the marks go with it.
  new MutationObserver(() => { if (root.dataset.typing !== "true") unmark(); })
    .observe(root, { attributes: true, attributeFilter: ["data-typing"] });
}
