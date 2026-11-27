import { useRef } from "react";
import { isFocusVisible, useFocusVisibleListener } from "react-aria/private/interactions/useFocusVisible";

// The input a component last heard of, as Aria's useFocusVisible counts it:
// on mount, whatever came last (none yet counts as keys); after that, each
// key or press it lets through — keys typed in a text field other than Tab
// and Escape leave it as it was. Held here, not in each component's state:
// every menu that can open (each comment's "…", the sidebar's header, each
// view's menus) kept it as its own state, so the first press after keys or
// after the page loaded drew all of them again inside that pointerdown
// (2026-11-15 on a Galaxy A90: 5–13 ms on a page with one comment, and once
// more, 4 ms, on the next press). Nothing is drawn from it but a menu as it
// opens, so it is read as that menu draws.
let lastHeard = true;
let heard = 0;

function hear(keys: boolean) {
  lastHeard = keys;
  heard += 1;
}

/**
 * Whether the last input was the keyboard, so a menu opening now starts on
 * its first row. Notion's menus opened with the mouse highlight no row until
 * the pointer or an arrow key moves (seen 2026-10-17 and 2026-10-20: a
 * sidebar row's menu, the workspace menu); opened with Enter they start on
 * the first. A menu showing its current choice highlights that either way.
 */
export function useOpenedFromKeyboard() {
  const mount = useRef<{ keys: boolean; heard: number } | null>(null);
  mount.current ??= { keys: isFocusVisible(), heard };
  useFocusVisibleListener(hear, []);
  return heard !== mount.current.heard ? lastHeard : mount.current.keys;
}
