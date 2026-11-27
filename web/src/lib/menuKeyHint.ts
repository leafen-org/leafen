import { detectKeyboardShortcutPlatform, isolatedShortcut, type KeyboardShortcutPlatform } from "./keyboardShortcuts";

/**
 * A menu row's shortcut written as a Mac shows it ("⌘⇧P"), in the form of
 * the platform Leafen runs on: as written on a Mac, "Ctrl+Shift+P" elsewhere,
 * as the shortcut table's own hints are.
 */
export function menuKeyHint(mac: string, platform: KeyboardShortcutPlatform = detectKeyboardShortcutPlatform()) {
  if (platform === "mac") return isolatedShortcut(mac);
  let primary = false, alt = false, shift = false, key = "";
  for (const character of Array.from(mac)) {
    if (character === "⌘" || character === "⌃") primary = true;
    else if (character === "⌥") alt = true;
    else if (character === "⇧") shift = true;
    else key += character === "↵" ? "Enter" : character;
  }
  return [primary ? "Ctrl" : "", alt ? "Alt" : "", shift ? "Shift" : "", key].filter(Boolean).join("+");
}

/**
 * A key a moving guide shows pressed (written as a Mac shows it, "⌘V" or
 * "↵"), as the keyboard Leafen runs on labels it: a Mac's keys apart ("⌘ V",
 * "return ⏎"), elsewhere menuKeyHint's words ("Ctrl+V", "Enter ⏎"). A
 * Windows first run's paste guide showed "⌘ V" (2026-11-24).
 */
export function guideKeyCap(mac: string, platform: KeyboardShortcutPlatform = detectKeyboardShortcutPlatform()) {
  if (platform === "mac") return Array.from(mac).map((character) => (character === "↵" ? "return ⏎" : character)).join(" ");
  return menuKeyHint(mac, platform).replace(/Enter$/u, "Enter ⏎");
}
