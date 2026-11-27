import { KEYBOARD_SHORTCUTS, matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { isSlashCharacter } from "./triggerCharacters";

/**
 * Whether the person is working the page by the keyboard rather than the
 * mouse, as Notion's keyboard mode (its editor code, read 2026-11-18, and
 * Chrome at 1440 the same day): a key pressed without ⌘, ⌃ or ⌥ turns it on
 * — the "/" keys aside (isSlashCharacter), so a "/" alone keeps it as it
 * was — and so do copy, cut and paste; the mouse moving to another point
 * turns it off. A "/" typed
 * right after a letter opens its menu only while it is off
 * (triggerOpensAfter): typed straight on from the word ("and/or") it stays a
 * letter, typed after a click or a mouse move at the word's end it opens.
 * The keys that move the caret count as any other: End, Home and the arrows
 * turn it on, so a "/" typed after End at a word's end stays a letter, and ⌘
 * or ⌥ with an arrow keeps it as it was, so after a click ⌘→ or ⌥→ then "/"
 * opens — as Notion's (seen 2026-11-18 in Chrome at 1440, two tabs at
 * different times, nothing run in the page before the keys).
 * A key counts wherever it is pressed, not only in the page's text: one typed
 * in a link's address field, quick find or a menu, and the Esc or Enter that
 * goes back to the text, turn it on too. So does a key with ⌘, ⌃ or ⌥ that
 * is one of the app's own commands rather than the editor's (appCommand:
 * quick find ⌘P, the sidebar ⌘\, all toggles ⌘⌥T), while the editor's own
 * (⌘B, ⌘⇧↑) and a chord that is no command keep it as it was — as Notion's
 * (seen 2026-11-18 in Chrome at 1440, two tabs at different times: after
 * a click, ⌘\ or ⌘⌥T then ⌘→ and "/" opens nothing; ⌘B, ⌘⇧↑ or ⌃Q then ⌘→
 * and "/" opens the menu).
 */
let keyboard = false;
let pointer: { x: number; y: number } | null = null;
// The text the last input put in at once: a key's letter, or a whole word
// an input method commits, a correction or dictation puts in; and whether
// the last text came from the clipboard or a drop, which opens no menu.
let lastPiece = "";
let pasted = false;

// The app's own commands: those of the whole app and of the page, not the
// editor's formatting and block keys (keyboardShortcuts.ts scope).
const APP_COMMANDS = KEYBOARD_SHORTCUTS.filter(({ scope }) => scope !== "editor").map(({ id }) => id);

function appCommand(event: KeyboardEvent) {
  return APP_COMMANDS.some((id) => matchesKeyboardShortcut(id, event));
}

function keyPressed(event: KeyboardEvent) {
  pasted = false;
  const plain = !event.metaKey && !event.ctrlKey && !event.altKey;
  if (plain ? !isSlashCharacter(event.key) : appCommand(event)) keyboard = true;
}

function pointerMoved(event: MouseEvent) {
  if (pointer?.x === event.clientX && pointer.y === event.clientY) return;
  pointer = { x: event.clientX, y: event.clientY };
  keyboard = false;
}

function clipboardUsed(event: Event) {
  keyboard = true;
  if (event.type === "paste") pasted = true;
}

function textPut(event: InputEvent) {
  if (!event.inputType?.startsWith("insert")) return;
  lastPiece = event.data ?? event.dataTransfer?.getData("text/plain") ?? "";
  pasted = /^insertFrom(Paste|Drop|Yank)/u.test(event.inputType);
}

if (typeof window !== "undefined") {
  window.addEventListener("keydown", keyPressed, true);
  window.addEventListener("mousemove", pointerMoved, true);
  // An insertion made by a script (execCommand) has no beforeinput, only input.
  for (const type of ["beforeinput", "input"] as const) window.addEventListener(type, textPut as EventListener, true);
  for (const type of ["copy", "cut", "paste"] as const) window.addEventListener(type, clipboardUsed, true);
  window.addEventListener("drop", () => { pasted = true; }, true);
}

/** Whether the last thing the person did was on the keyboard (see above). */
export function workingByKeyboard() {
  return keyboard;
}

/**
 * Whether text of more than one letter came in by one input at once — an
 * input method's commit, a correction, dictation — rather than by keys that
 * landed together while the page was busy (typedTrigger).
 */
export function putInOnePiece(text: string) {
  return text.length > 1 && lastPiece === text;
}

/** Whether the text last put in was pasted or dropped, which opens no menu (fieldMenuTrigger). */
export function putInByPaste() {
  return pasted;
}
