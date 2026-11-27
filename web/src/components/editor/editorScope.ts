import { layerOrigin } from "@/lib/overlayLayer";

// An editor can hold another editor, as a synced block copy holds the
// editor of its original's content. A keyboard shortcut belongs to the
// editor holding the focus, else to the one last pointed at, else to the
// page's own editor; a click belongs to the innermost editor under it.

let pointedEditorRoot: Element | null = null;

/** The innermost editor an element is in. */
export function editorRootOf(target: EventTarget | null): Element | null {
  return target instanceof Element ? target.closest("[data-editor-root]") : null;
}

const PEEK = "[data-page-peek],[data-row-peek-panel]";
const MAIN = "#app-main-content";
const CENTER_PEEK = '[data-page-peek][data-mode="center"],[data-row-peek-panel][data-mode="center"]';

/**
 * A center peek open over the page. Peeks are drawn into the document's
 * body beside the app (PagePeek, DatabaseRowPage), so only what stands
 * beside the app is looked through: the whole document, a long page's tens
 * of thousands of elements, was gone through at each key a chosen block took
 * (2026-11-14: 12 ms of a ⌘V with a picture chosen at CPU ×2).
 */
function centerPeek(): Element | null {
  for (const layer of document.body.children) {
    if (layer.id === "root") continue;
    const peek = layer.matches(CENTER_PEEK) ? layer : layer.querySelector(CENTER_PEEK);
    if (peek) return peek;
  }
  return null;
}

/** The page's own editor on a peek, or on the page behind any peek: not a synced block's within it. */
function ownEditorOf(surface: Element | null) {
  const editors = [...(surface ?? document).querySelectorAll("[data-editor-root]:not([data-editor-nested])")];
  return (surface ? editors[0] : editors.find((editor) => !editor.closest(PEEK))) ?? null;
}

/**
 * Whether a press or a focus is on the page around an editor — its title,
 * properties and discussion, the bar over it, the room beside it — or a peek
 * around it, and not in it, in another editor or in a menu or window drawn
 * over it: chosen blocks are let go there, as Notion's (seen 2026-11-12: a
 * press on the title, on the top bar's empty part or beside the page let go
 * of a chosen line; the search window opened over it kept it).
 */
export function besideEditor(target: EventTarget | null, root: Element | null) {
  if (!(target instanceof Element) || !root || editorRootOf(target)) return false;
  if (!root.closest(`${PEEK},${MAIN}`)?.contains(target)) return false;
  const over = target.closest('[role="menu"],[role="dialog"],[role="alertdialog"],[role="listbox"],[role="tooltip"]');
  return !over || over.contains(root);
}

/**
 * Notes the editor a pointer went down in. A blank part of a peek or of the
 * page behind it belongs to that surface's own editor, so ⌘Z there doesn't
 * reach the other's (seen 2026-11-05: it took back text on the page behind).
 */
export function notePointedEditor(target: EventTarget | null) {
  const at = placeOf(target);
  // A press in a menu drawn over everything whose opener isn't known keeps
  // the place it was opened from.
  if (at instanceof Element && !editorRootOf(at) && !at.closest(PEEK) && !at.closest(MAIN)) return;
  pointedEditorRoot = editorRootOf(at) ?? ownEditorOf(at instanceof Element ? at.closest(PEEK) : null);
}

/**
 * Where something is, for whose keys it takes: a press or focus in a menu or
 * window drawn over the page is where that was opened from, a peek's
 * property menu its peek (2026-11-07: renaming a property from a peek's menu
 * went in the page behind's history).
 */
function placeOf(target: EventTarget | null) {
  if (!(target instanceof Element) || editorRootOf(target) || target.closest(PEEK)) return target;
  return layerOrigin(target) ?? target;
}

export function editorCommandOwner() {
  const active = placeOf(document.activeElement);
  const editor = editorRootOf(active);
  if (editor) return editor;
  // Over a center peek the page behind is out of reach; a focus on a peek's own
  // button or panel belongs to its editor.
  const surface = (active instanceof Element ? active.closest(PEEK) : null) ?? centerPeek();
  if (surface) return ownEditorOf(surface);
  return pointedEditorRoot?.isConnected ? pointedEditorRoot : null;
}

/**
 * The page of a peek's editor that a keyboard shortcut would go to now, when
 * one does: what is done in a peek is taken back from it, as Notion's (seen
 * 2026-11-07: a property added in a peek went with ⌘Z there, and not from
 * the page behind once the peek closed).
 */
export function peekPageOwningCommands(): string | null {
  const owner = editorCommandOwner();
  return owner?.closest(PEEK) ? owner.getAttribute("data-editor-page") : null;
}

/**
 * Chosen blocks leave no text range in their editor, as Notion's (seen
 * 2026-11-05): what is typed or composed then has nowhere to go.
 */
export function dropTextRangeIn(root: Element | null) {
  const shown = window.getSelection();
  if (root && shown?.anchorNode && root.contains(shown.anchorNode)) shown.removeAllRanges();
}

/**
 * The page whose history a ⌘Z pressed now takes from: that of the editor
 * holding the keys (the page's own, past a synced block's inside it), else
 * the page open behind any peek.
 */
export function pageOwningHistory(): string | null {
  const owner = editorCommandOwner();
  const own = owner ? owner.closest("[data-editor-root]:not([data-editor-nested])") : ownEditorOf(null);
  return own?.getAttribute("data-editor-page") ?? null;
}

/** Whether a keyboard shortcut belongs to the editor with this root. */
export function editorOwnsKeyboard(root: Element | null, nested: boolean) {
  const owner = editorCommandOwner();
  return owner ? owner === root : !nested;
}
