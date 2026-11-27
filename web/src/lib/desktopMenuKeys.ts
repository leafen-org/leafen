import { hasDesktopMenuBar, onDesktopMenuEvent } from "./desktopMenuCommands";

// The Mac app's 편집 › 실행 취소, 다시 실행 and 모두 선택
// (desktop/app_menu_bar_darwin.go) do what ⌘Z, ⇧⌘Z and ⌘A do where the focus
// is, as Notion's app menu does: the page's editor, a database cell, a title
// and the page's chosen blocks step their own way, and a field nothing of the
// page answers for undoes its own typing or chooses all its text. WebKit's own
// undo, which the menu's system items asked for, knew nothing of the editor's
// steps: a split line stayed split with the caret moved, and undone words came
// back chosen as a new edit that took Redo away (2026-11-14, the test app).
// WebKit's own select-all chose the whole page's text at once, where Notion's
// menu, as its ⌘A, first chooses the caret's line and then its block (seen in
// both apps 2026-11-17). The Windows app's menu presses the keys themselves
// (app_menu_windows.go).

let following = false;

/** Answers the Mac app's menu Undo, Redo and Select All in this window, as its ⌘Z, ⇧⌘Z and ⌘A. */
export function followDesktopMenuKeys() {
  if (following || !hasDesktopMenuBar()) return;
  following = true;
  onDesktopMenuEvent("leafen:undo", () => {
    if (pressKey("z", 90, false)) document.execCommand("undo");
  });
  onDesktopMenuEvent("leafen:redo", () => {
    if (pressKey("z", 90, true)) document.execCommand("redo");
  });
  onDesktopMenuEvent("leafen:select-all", () => {
    if (pressKey("a", 65, false)) void webKitSelectAll();
  });
}

/** Presses ⌘ and the letter where the focus is; true when nothing of the page took it. */
function pressKey(letter: string, keyCode: number, shiftKey: boolean) {
  const target = document.activeElement instanceof HTMLElement ? document.activeElement : document.body;
  const key = new KeyboardEvent("keydown", {
    key: letter, code: `Key${letter.toUpperCase()}`, metaKey: true, shiftKey, bubbles: true, cancelable: true, composed: true,
  });
  // A Mac keymap reads ⇧⌘'s letter from the key's number (w3c-keyname), which a made key lacks.
  Object.defineProperty(key, "keyCode", { value: keyCode });
  Object.defineProperty(key, "which", { value: keyCode });
  return target.dispatchEvent(key);
}

/**
 * WebKit's own select-all, as the menu's system item did, for a key the page
 * leaves: it chooses in the frame the focus is in, which the page can't reach
 * into (desktop/text_menu_darwin.go Edit).
 */
async function webKitSelectAll() {
  try {
    const { Call } = await import("@wailsio/runtime");
    await Call.ByName("main.TextMenu.Edit", "selectAll");
  } catch {
    document.execCommand("selectAll");
  }
}
