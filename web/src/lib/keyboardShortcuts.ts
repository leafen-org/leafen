import { hasDesktopMenuBar, packagedComputer } from "./desktopMenuCommands";
import { imeComposing } from "./fieldKeyBoundary";
import {
  KEYBOARD_SHORTCUTS,
  type KeyboardShortcutChord,
  type KeyboardShortcutDefinition,
  type KeyboardShortcutId,
  type KeyboardShortcutPlatform,
  type ModifierRequirement,
} from "./keyboardShortcutTable";

export {
  KEYBOARD_SHORTCUTS,
  type KeyboardShortcutCategory,
  type KeyboardShortcutChord,
  type KeyboardShortcutDefinition,
  type KeyboardShortcutId,
  type KeyboardShortcutPlatform,
  type KeyboardShortcutScope,
} from "./keyboardShortcutTable";

export type KeyboardShortcutEventLike = {
  key: string;
  code?: string;
  metaKey?: boolean;
  ctrlKey?: boolean;
  shiftKey?: boolean;
  altKey?: boolean;
  repeat?: boolean;
  defaultPrevented?: boolean;
  isComposing?: boolean;
  keyCode?: number;
  getModifierState?: (key: "AltGraph") => boolean;
  nativeEvent?: {
    isComposing?: boolean;
    keyCode?: number;
  };
};

let macPlatform: boolean | null = null;

const shortcutById = new Map<KeyboardShortcutId, KeyboardShortcutDefinition>(
  KEYBOARD_SHORTCUTS.map((shortcut) => [shortcut.id, shortcut]),
);

export function keyboardShortcut(id: KeyboardShortcutId): KeyboardShortcutDefinition {
  const shortcut = shortcutById.get(id);
  if (!shortcut) throw new Error(`Unknown keyboard shortcut: ${id}`);
  macPlatform ??= detectKeyboardShortcutPlatform() === "mac";
  if (shortcut.pcChords && !macPlatform) return { ...shortcut, chords: shortcut.pcChords };
  if (shortcut.macAppChords && hasDesktopMenuBar()) return { ...shortcut, chords: shortcut.macAppChords };
  return shortcut.browserChords && !packagedComputer() ? { ...shortcut, chords: shortcut.browserChords } : shortcut;
}

function modifierMatches(actual: boolean | undefined, requirement: ModifierRequirement | undefined) {
  if (requirement === "any") return true;
  return Boolean(actual) === Boolean(requirement);
}


/**
 * Whether a key comes with the primary key: ⌘ on a Mac, where ⌃ with a
 * letter is macOS's own text command (⌃K deletes to the paragraph's end, ⌃A
 * and ⌃E go to the line's ends, ⌃P and ⌃N up and down) and Notion leaves it
 * so; Ctrl elsewhere, as it was.
 */
export function primaryModifier(event: { metaKey?: boolean; ctrlKey?: boolean }) {
  macPlatform ??= detectKeyboardShortcutPlatform() === "mac";
  return macPlatform ? Boolean(event.metaKey) : Boolean(event.metaKey || event.ctrlKey);
}

/**
 * The letter a ⌘ or Ctrl chord's key stands for, lowercased: the key's own
 * where it gives a Latin letter (Dvorak's too), else the letter key it sits
 * on, as the chords above match by code. With a Korean input source the Mac
 * app (WebKit) gives ⌘D as "ㅇ", which a check for "d" let pass (2026-11-12).
 * A key without ⌘ or Ctrl, and any other key, comes back as it is, lowercased.
 */
export function chordLetter(event: { key: string; code?: string; metaKey?: boolean; ctrlKey?: boolean }): string {
  const key = event.key.toLowerCase();
  if (!(event.metaKey || event.ctrlKey) || /^[a-z]$/u.test(key)) return key;
  const letter = /^Key([A-Z])$/u.exec(event.code ?? "");
  return letter ? letter[1].toLowerCase() : key;
}

/**
 * Whether the key comes with AltGr, which Windows sends as Ctrl+Alt (and Linux
 * as its own key): a letter of the layout — "{" "[" "]" "}" from AltGr+7–0 on
 * a German keyboard, "ł" from AltGr+L on a Polish one — and never a shortcut,
 * so it types. A Mac's ⌥ is no AltGr.
 */
export function altGraphKey(event: Pick<KeyboardShortcutEventLike, "getModifierState">) {
  macPlatform ??= detectKeyboardShortcutPlatform() === "mac";
  return !macPlatform && Boolean(event.getModifierState?.("AltGraph"));
}

/**
 * Whether ⌘, Ctrl or Alt comes with the key as for a shortcut — AltGr aside,
 * whose keys are letters (altGraphKey): where a typed letter starts typing in
 * a chosen cell, goes to a range's cell, is swallowed over chosen blocks or
 * seeks a menu's search, an AltGr letter does as any letter.
 */
export function shortcutModifierHeld(event: Pick<KeyboardShortcutEventLike, "metaKey" | "ctrlKey" | "altKey" | "getModifierState">) {
  return Boolean(event.metaKey || event.ctrlKey || event.altKey) && !altGraphKey(event);
}

function chordMatches(chordDefinition: KeyboardShortcutChord, event: KeyboardShortcutEventLike) {
  const primary = Boolean(event.metaKey || event.ctrlKey);
  if (primary !== chordDefinition.primary) return false;
  if (primary && !chordDefinition.ctrl && !primaryModifier(event)) return false;
  if (chordDefinition.ctrl && (!event.ctrlKey || event.metaKey)) return false;
  // ⌘⌃ together is a Mac's own chord, apart from ⌘ alone.
  if (Boolean(event.metaKey && event.ctrlKey) !== Boolean(chordDefinition.control)) return false;
  if (!modifierMatches(event.altKey, chordDefinition.alt)) return false;
  if (!modifierMatches(event.shiftKey, chordDefinition.shift)) return false;
  const keyMatches = event.key.toLowerCase() === chordDefinition.key.toLowerCase();
  const codeMatches = Boolean(chordDefinition.code && event.code === chordDefinition.code);
  return keyMatches || codeMatches;
}

/** The chord of that shortcut the key is, if it is one. */
function matchingChord(id: KeyboardShortcutId, event: KeyboardShortcutEventLike) {
  if (event.defaultPrevented || event.repeat || imeComposing(event) || altGraphKey(event)) return undefined;
  return keyboardShortcut(id).chords.find((definition) => chordMatches(definition, event));
}

export function matchesKeyboardShortcut(
  id: KeyboardShortcutId,
  event: KeyboardShortcutEventLike,
) {
  return Boolean(matchingChord(id, event));
}

/**
 * The place in the workspace menu (1–9) that the desktop app's workspace key
 * goes to — ⌃⇧ and the number on a Mac, Alt+Shift elsewhere (the table's
 * switchWorkspace) —, or 0 for any other key.
 */
export function workspaceNumberKey(event: KeyboardShortcutEventLike): number {
  return Number(matchingChord("switchWorkspace", event)?.key ?? 0);
}

/** The Option key's name where Leafen runs: "Option" on a Mac, "Alt" elsewhere. */
export function optionKeyName() {
  return detectKeyboardShortcutPlatform() === "mac" ? "Option" : "Alt";
}

/** The primary key's mark where Leafen runs, for words such as "⌘+V": "⌘" on a Mac, "Ctrl" elsewhere. */
export function primaryKeyName() {
  return detectKeyboardShortcutPlatform() === "mac" ? "⌘" : "Ctrl";
}

export function detectKeyboardShortcutPlatform(value?: string): KeyboardShortcutPlatform {
  const platform = value ?? (
    typeof navigator === "undefined"
      ? ""
      : `${navigator.platform ?? ""} ${navigator.userAgent ?? ""}`
  );
  if (/mac|iphone|ipad|ipod/i.test(platform)) return "mac";
  if (/win/i.test(platform)) return "windows";
  return "linux";
}

function displayKey(chordDefinition: KeyboardShortcutChord) {
  if (chordDefinition.displayKey) return chordDefinition.displayKey;
  if (/^arrow/i.test(chordDefinition.key)) return chordDefinition.key.replace(/^Arrow/i, "");
  return chordDefinition.key.length === 1
    ? chordDefinition.key.toUpperCase()
    : chordDefinition.key;
}

function chordDisplayKeys(
  chordDefinition: KeyboardShortcutChord,
  platform: KeyboardShortcutPlatform,
) {
  if (chordDefinition.control && platform !== "mac") return [];
  const keys: string[] = [];
  if (chordDefinition.primary) keys.push(platform === "mac" && !chordDefinition.ctrl ? "Cmd" : platform === "mac" ? "Control" : "Ctrl");
  if (chordDefinition.control) keys.push("Control");
  // Shift before Option, as Notion's shortcuts window writes "Cmd Shift Option L" (seen 2026-11-20) and its menus "⌘⇧⌥X".
  if (chordDefinition.shift === true) keys.push("Shift");
  if (chordDefinition.alt === true) keys.push(platform === "mac" ? "Option" : "Alt");
  // Away from a Mac the keys are words, Enter too, as menuKeyHint writes them.
  const key = displayKey(chordDefinition);
  keys.push(platform !== "mac" && key === "↵" ? "Enter" : key);
  return keys;
}

/**
 * A tip's shortcut line, as Notion's: every chord, " or " between them
 * ("⌘⇧X or ⌘⇧S" for strikethrough, seen 2026-10-28).
 */
export function shortcutTipKeys(
  id: KeyboardShortcutId,
  platform: KeyboardShortcutPlatform = detectKeyboardShortcutPlatform(),
) {
  return keyboardShortcut(id).chords.map((definition) => compactChord(definition, platform)).filter(Boolean).join(" or ");
}

export function shortcutDisplayKeys(
  id: KeyboardShortcutId,
  platform: KeyboardShortcutPlatform = detectKeyboardShortcutPlatform(),
) {
  return keyboardShortcut(id).chords.map((definition) => chordDisplayKeys(definition, platform));
}

export function shortcutCompactLabel(
  id: KeyboardShortcutId,
  platform: KeyboardShortcutPlatform = detectKeyboardShortcutPlatform(),
) {
  const [definition] = keyboardShortcut(id).chords;
  return definition ? compactChord(definition, platform) : "";
}

/**
 * A shortcut's keys set apart from the words beside them, read in the screen's direction as Notion's menus and
 * tips write them (seen 2026-11-19 in Arabic and Hebrew: "⌘⇧P" reads "P⇧⌘" from the left, ⌘ at the line's start).
 */
export function isolatedShortcut(label: string | undefined) {
  const rightToLeft = typeof document !== "undefined" && document.documentElement.dir === "rtl";
  return label ? `${rightToLeft ? "\u2067" : "\u2066"}${label}\u2069` : "";
}

function compactChord(definition: KeyboardShortcutChord, platform: KeyboardShortcutPlatform) {
  return isolatedShortcut(chordLabel(definition, platform));
}

function chordLabel(definition: KeyboardShortcutChord, platform: KeyboardShortcutPlatform) {
  if (definition.control && platform !== "mac") return "";
  if (platform === "mac") {
    // Notion's menus put ⇧ before ⌥ ("⌘⇧⌥X", seen 2026-10-04, 2026-10-11).
    const modifiers = [
      definition.primary ? (definition.ctrl ? "⌃" : "⌘") : "",
      definition.control ? "⌃" : "",
      definition.shift === true ? "⇧" : "",
      definition.alt === true ? "⌥" : "",
    ].join("");
    return `${modifiers}${displayKey(definition)}`;
  }
  return chordDisplayKeys(definition, platform).join("+");
}
