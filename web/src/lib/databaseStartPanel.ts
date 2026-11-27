import { useSyncExternalStore, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { requestPageUndo } from "@/components/editor/pageUndo";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { imeComposing } from "./fieldKeyBoundary";

// A data source just added to a database, a linked view waiting for its
// source, or a database made from "/" opens Notion's start panel once, as
// Notion's does (seen 2026-11-02 and 2026-11-15): the request is left for
// the database's block, which shows the panel when it draws or, already
// drawn, when asked — drawn twice, in the copy that shows (shownBlockCopy).
type StartRequest = {
  dataSourceId?: string;
  /** The block waits for a source to link ("데이터 소스 보기 연결"): the panel offers the sources first. */
  link?: boolean;
  /** A linked view gone back from its "새 보기": it stays as it is until another source or a template is picked. */
  relink?: boolean;
  /** Such a view's source was picked in the panel's "기존 데이터 소스 연결하기": the panel opens there again. */
  sources?: boolean;
  /** Asked for as the linked view was just made: a phone waits for its "데이터 소스 연결" instead (Notion's, seen 2026-11-11 at 402pt). */
  made?: boolean;
  /** Asked for by a database just made from "/" on a page: put away, it leaves the caret at the page title's start (DatabaseStartPanel). */
  slash?: boolean;
};
const requested = new Map<string, StartRequest>();
const listeners = new Set<() => void>();

// Where, in this reading, each block's linked source was picked in its start
// panel: the "기존 데이터 소스 연결하기" list, or a list on the panel's first
// screen (a waiting view's, a new chart's). A linked view's "새 보기" "←"
// goes back there, as Notion's does (seen 2026-11-09: to that list, inline or
// on the database's own page; to the first screen from a waiting view's
// list); one made with its page from a source card has no "←".
const pickedIn = new Map<string, "sources" | "start">();

/** Notes the panel's screen a block's linked source was picked in. */
export function noteLinkedFromStartPanel(blockId: string, screen: "sources" | "start") {
  pickedIn.set(blockId, screen);
}

/** The panel's screen the block's linked source was picked in this reading, if it was. */
export function linkedFromStartPanel(blockId: string) {
  return pickedIn.get(blockId) ?? null;
}

export function requestDatabaseStartPanel(blockId: string, request: StartRequest = {}) {
  requested.set(blockId, request);
  for (const listener of listeners) listener();
}

export function databaseStartPanelRequest(blockId: string) {
  return requested.get(blockId) ?? null;
}

export function clearDatabaseStartPanel(blockId: string) {
  requested.delete(blockId);
}

export function subscribeDatabaseStartPanel(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

// The blocks whose start panel shows now. The panel is the window of the
// database's settings: their "⚙" stands pressed while it shows, and a press
// on it puts the panel away, opening nothing (Notion's, seen 2026-11-13 for a
// database made inline and on its own page).
const showing = new Map<string, () => void>();
const showingListeners = new Set<() => void>();

function showingChanged() {
  for (const listener of showingListeners) listener();
}

function subscribeShowing(listener: () => void) {
  showingListeners.add(listener);
  return () => { showingListeners.delete(listener); };
}

/** Notes the block's start panel shown, `close` putting it away; the returned call notes it gone. */
export function noteDatabaseStartPanelShown(blockId: string, close: () => void) {
  showing.set(blockId, close);
  showingChanged();
  return () => {
    if (showing.get(blockId) !== close) return;
    showing.delete(blockId);
    showingChanged();
  };
}

/** Puts the block's start panel away, if it shows. */
export function closeShownDatabaseStartPanel(blockId: string) {
  const close = showing.get(blockId);
  if (!close) return false;
  close();
  return true;
}

/** Whether the block's start panel shows, kept up as it opens and closes. */
export function useDatabaseStartPanelShown(blockId: string | undefined) {
  return useSyncExternalStore(subscribeShowing, () => Boolean(blockId) && showing.has(blockId!), () => false);
}

// The other windows standing in a database's settings' place — "관계형 대상"
// and its "새 관계형", made from a new property's menu — keep that database's
// "⚙" pressed while they show, as its start panel does, and a press on it
// puts them away, by the toolbar they open from (Notion's, seen 2026-11-16).
const placeWindows = new Map<HTMLElement, () => void>();

/** Notes a window shown in the settings' place of `toolbar`, `close` putting it away; the returned call notes it gone. */
export function noteSettingsPlaceWindowShown(toolbar: HTMLElement, close: () => void) {
  placeWindows.set(toolbar, close);
  showingChanged();
  return () => {
    if (placeWindows.get(toolbar) !== close) return;
    placeWindows.delete(toolbar);
    showingChanged();
  };
}

/** Puts away the window shown in the settings' place of `toolbar`, if one shows. */
export function closeSettingsPlaceWindow(toolbar: HTMLElement | null) {
  const close = toolbar ? placeWindows.get(toolbar) : undefined;
  if (!close) return false;
  close();
  return true;
}

/** Whether a window shows in the settings' place of the toolbar `toolbarRef` holds, kept up as it opens and closes. */
export function useSettingsPlaceWindowShown(toolbarRef: { readonly current: HTMLElement | null }) {
  return useSyncExternalStore(subscribeShowing, () => Boolean(toolbarRef.current && placeWindows.has(toolbarRef.current)), () => false);
}

/**
 * ⌘Z and ⌘⇧Z (⌘Y) pressed on a start panel's first screen are its page's,
 * as Notion's (measured 2026-11-16 in Chrome at 1440, dark, no script in the
 * page: with the panel of a database just made from "/" open — its own
 * field focused — ⌘Z took the database back to the empty line it was made
 * on, the panel going with it, and "k" typed next went into that line; on a
 * full-page database's own page, nothing there to take back, the panel
 * stayed; ⌘⇧Z with nothing to make again left all as it was). Its "기존 데이터
 * 소스 연결하기" search keeps its own (Notion's did nothing there). Says
 * whether it took the key.
 */
export function takeStartPanelPageStep(event: ReactKeyboardEvent<HTMLElement>, pageId: string): boolean {
  if (!primaryModifier(event) || event.altKey || imeComposing(event)) return false;
  const key = chordLetter(event);
  if (key !== "z" && key !== "y") return false;
  // A search typed in takes back its own words first.
  const field = event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement ? event.target : null;
  if (field?.value) return false;
  event.preventDefault();
  event.stopPropagation();
  requestPageUndo(pageId, event.shiftKey || key === "y");
  return true;
}
