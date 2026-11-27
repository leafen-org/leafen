import { useSyncExternalStore } from "react";
import { largeViewportHeight, softKeyboardHeight } from "@/lib/softKeyboard";
import type { PhoneTool } from "./PhoneSelectionTools";

/**
 * The lists a phone strip's "+", "전환" and "A" open in the keyboard's place,
 * as the Notion Android app's (seen 2026-11-21 on the A90 at 411, its own
 * screens read twice each): the keyboard goes down, the caret staying in its
 * line, the strip standing where it stood, and the list takes the keyboard's
 * height under it; the tool that opened it holds its grey tile and the
 * keyboard's cell turns to ⓧ, which brings the keyboard back. A choice made
 * there brings it back too ("전환" seen; "+" and the colors as that one).
 * A browser puts the keyboard away with the focus kept only through
 * navigator.virtualKeyboard (Android Chrome and its WebView): elsewhere
 * (iPhone Safari) the tools open their lists over the screen as before.
 */
export type StripPanelKind = "add" | "turn" | "color";
export type StripPanel = { kind: StripPanelKind; height: number; field: HTMLElement };

type VirtualKeyboard = { show(): void; hide(): void };
const keyboardApi = () => typeof navigator === "undefined" ? undefined
  : (navigator as Navigator & { virtualKeyboard?: VirtualKeyboard }).virtualKeyboard;

let open: (StripPanel & { close?: () => void }) | null = null;
const listeners = new Set<() => void>();
const notify = () => { for (const listener of listeners) listener(); };
// The keyboard's height last seen at each width, for a list opened with it down.
const keyboards = new Map<number, number>();

/** Whether the strip's lists stand in the keyboard's place here. */
export function stripPanelsHere() {
  return Boolean(keyboardApi());
}

/** The list open in the keyboard's place, if one is. */
export function currentStripPanel(): StripPanel | null {
  return open;
}

/** Hears the list come, go or change. */
export function watchStripPanel(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The list open in the keyboard's place, watched. */
export function useStripPanel(): StripPanel | null {
  return useSyncExternalStore(watchStripPanel, currentStripPanel, () => null);
}

/**
 * Opens the `kind` list in the keyboard's place for the caret in `field`:
 * the keyboard goes down and the field keeps its focus. Pressed again, the
 * tool that opened it closes it and brings the keyboard back; another tool's
 * list takes its place. False where a browser cannot put the keyboard away
 * with the focus kept.
 */
export function openStripPanel(kind: StripPanelKind, field: HTMLElement) {
  const api = keyboardApi();
  if (!api) return false;
  if (open?.kind === kind) { closeStripPanel(true); return true; }
  const width = window.innerWidth, shown = softKeyboardHeight();
  if (shown) keyboards.set(width, shown);
  // Where no keyboard was up (a hardware one), the list stands as tall as the
  // last keyboard at this width, or four tenths of the screen (the app's is
  // its keyboard's 326 of 914).
  const height = open?.height ?? (shown || keyboards.get(width) || Math.round(largeViewportHeight() * 0.4));
  const was = open;
  open = { kind, height, field };
  was?.close?.();
  field.setAttribute("virtualkeyboardpolicy", "manual");
  api.hide();
  notify();
  return true;
}

/**
 * The list's owner (the "+" menu, the block's "전환" or colors) holds it
 * while it shows, and is closed by ⓧ or another tool's list; it gives the
 * list up as it closes.
 */
export function holdStripPanel(kind: StripPanelKind, close: () => void) {
  if (!open || open.kind !== kind) return () => {};
  const held = open;
  held.close = close;
  return () => { if (open === held) end(held); };
}

/**
 * Brings the keyboard back where the list was open: in the press of a choice
 * or of ⓧ, which a browser asks for (a press may raise the keyboard).
 */
export function keyboardBackFromStripPanel() {
  if (open) keyboardApi()?.show();
}

/** Closes the list, the keyboard brought back where `keyboard` (ⓧ, its tool pressed again, a tap in the page). */
export function closeStripPanel(keyboard: boolean) {
  const was = open;
  if (!was) return;
  if (keyboard) keyboardApi()?.show();
  end(was);
  was.close?.();
}

function end(was: StripPanel) {
  if (open !== was) return;
  open = null;
  notify();
  // The field takes the keyboard as a press raises it again, once the keyboard's call is through.
  requestAnimationFrame(() => { if (open?.field !== was.field) was.field.removeAttribute("virtualkeyboardpolicy"); });
}

/** The strip tools that open a list in the keyboard's place: a line's "+" and "전환", and the "A" of "Aa" or of chosen words. */
const PANEL_OF: Readonly<Record<string, StripPanelKind>> = { add: "add", turn: "turn", color: "color" };

/**
 * A phone strip's tools with their lists in the keyboard's place where a
 * browser can put it away (stripPanelsHere): each opens its list, held on its
 * grey tile while it shows, and closes it pressed again.
 */
export function withStripPanels(tools: readonly PhoneTool[], field: HTMLElement, shown: StripPanel | null): PhoneTool[] {
  if (!stripPanelsHere()) return [...tools];
  return tools.map(tool => {
    const kind = PANEL_OF[tool.key];
    return !kind ? tool : { ...tool, pressed: shown?.kind === kind || tool.pressed,
      // Pressed again, it only closes its list; where the keyboard cannot be put away it opens its sheet.
      run: button => { if (!openStripPanel(kind, field) || currentStripPanel()?.kind === kind) tool.run(button); } };
  });
}
