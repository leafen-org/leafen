import {
  matchesKeyboardShortcut,
  type KeyboardShortcutEventLike,
} from "./keyboardShortcuts";


export function isNewPageShortcut(event: KeyboardShortcutEventLike) {
  return matchesKeyboardShortcut("newPage", event);
}
