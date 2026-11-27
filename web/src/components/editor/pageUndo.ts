// The page menu's "실행 취소" (Notion's, ⌘Z) asks the page's own editor to
// undo its last change, as ⌘Z there does. One editor answers, however many
// show the page.

import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { pageOwningHistory } from "./editorScope";

const pageUndoEvent = "leafen:page-undo";

type PageUndoDetail = { pageId: string; forward: boolean; handled: boolean; begun?: Promise<void> };

function askPageUndo(pageId: string, forward: boolean) {
  const detail: PageUndoDetail = { pageId, forward, handled: false };
  document.dispatchEvent(new CustomEvent<PageUndoDetail>(pageUndoEvent, { detail }));
  return detail;
}

/** Whether an editor took the step; forward redoes. */
export function requestPageUndo(pageId: string, forward = false) {
  return askPageUndo(pageId, forward).handled;
}

/**
 * Asks for the step as requestPageUndo does, and says when it began, for
 * what reads the page as the step left it: at once, or — pressed while a
 * step before it was on its way to the server, which it waits for — as its
 * turn comes. Null when no editor took it.
 */
export function requestPageUndoBegun(pageId: string, forward = false): Promise<void> | null {
  const detail = askPageUndo(pageId, forward);
  return detail.handled ? detail.begun ?? Promise.resolve() : null;
}

const pageGraphChangeEvent = "leafen:page-graph-change";
type PageGraphChange = { pageId: string; before: ContentGraph; after: ContentGraph; operations: readonly GraphOperation[]; titleLine?: string };
const titleMade = new Map<string, Set<string>>();

/** The lines on the page that its title's Enter made. */
export function linesTitleMade(pageId: string, graph: ContentGraph) {
  const made = titleMade.get(pageId);
  return made ? graph.blocks.filter(block => made.has(block.id)).map(block => block.id) : [];
}

/** A change made outside the page's editor (a title's Enter making the first line) joins its history. */
export function notePageGraphChange(change: PageGraphChange) {
  if (change.titleLine) titleMade.set(change.pageId, new Set(titleMade.get(change.pageId)).add(change.titleLine));
  document.dispatchEvent(new CustomEvent<PageGraphChange>(pageGraphChangeEvent, { detail: change }));
}

export function listenForPageGraphChange(pageId: string, listener: (change: PageGraphChange) => void) {
  const heard = (event: Event) => {
    const detail = (event as CustomEvent<PageGraphChange>).detail;
    if (detail?.pageId === pageId) listener(detail);
  };
  document.addEventListener(pageGraphChangeEvent, heard);
  return () => document.removeEventListener(pageGraphChangeEvent, heard);
}

const pageHistoryStepEvent = "leafen:page-history-step";
type PageHistoryStep = { pageId: string; key: string; step: (forward: boolean) => Promise<boolean> };

/**
 * Whose history a change to a page joins: the page's own while it is open
 * (its editor drawn, alone or in a peek); else, made from the sidebar or a
 * database's row, the history ⌘Z takes from now — the open page's, which
 * takes back whatever was changed while it is open, as Notion's does (seen
 * 2026-11-11: a page renamed in the sidebar came back with ⌘Z on another).
 */
function historyPageOf(changed: string) {
  const open = document.querySelector(`[data-editor-root][data-editor-page="${CSS.escape(changed)}"]:not([data-editor-nested])`);
  return open ? changed : pageOwningHistory() ?? changed;
}

/** What was typed in the page's title, or a change to its look, takes its place in a page's history (historyPageOf): there is one ⌘Z, as Notion's (seen 2026-11-05). */
export function notePageHistoryStep(pageId: string, key: string, step: (forward: boolean) => Promise<boolean>) {
  document.dispatchEvent(new CustomEvent<PageHistoryStep>(pageHistoryStepEvent, { detail: { pageId: historyPageOf(pageId), key, step } }));
}

let changeSteps = 0;
/** A saved change to the page's look or place: ⌘Z saves what it replaced, ⌘⇧Z the change again. */
export function notePageChangeStep<Change>(pageId: string, before: Change, after: Change, save: (change: Change) => Promise<unknown>) {
  notePageHistoryStep(pageId, `change:${pageId}:${++changeSteps}`, async (forward) => (await save(forward ? after : before)) !== false);
}

/**
 * A rename in a popover is one step however many keys saved it: from the
 * name it opened with to the last one saved, noted once the popover has
 * closed and that save has gone.
 */
export function renameStep<Name>(pageId: string, before: Name, same: (left: Name, right: Name) => boolean, save: (name: Name) => Promise<unknown>) {
  let last: Promise<Name | null> = Promise.resolve(null);
  return {
    saved(name: Name, saving: Promise<unknown>) {
      const written = saving.then((result) => (result === false ? null : name), () => null);
      last = Promise.all([last, written]).then(([earlier, now]) => now ?? earlier);
    },
    close() {
      void last.then((after) => { if (after !== null && !same(after, before)) notePageChangeStep(pageId, before, after, save); });
    },
  };
}

export function listenForPageHistoryStep(pageId: string, listener: (key: string, step: (forward: boolean) => Promise<boolean>) => void) {
  const heard = (event: Event) => {
    const detail = (event as CustomEvent<PageHistoryStep>).detail;
    if (detail?.pageId === pageId) listener(detail.key, detail.step);
  };
  document.addEventListener(pageHistoryStepEvent, heard);
  return () => document.removeEventListener(pageHistoryStepEvent, heard);
}

const pageChooseEvent = "leafen:page-choose-blocks";

/** Chooses blocks in the page's own editor, once a step that went through the server has drawn. */
export function choosePageBlocks(pageId: string, ids: readonly string[]) {
  requestAnimationFrame(() => document.dispatchEvent(new CustomEvent(pageChooseEvent, { detail: { pageId, ids } })));
}

export function listenForPageChoose(pageId: string, listener: (ids: readonly string[]) => void) {
  const heard = (event: Event) => {
    const detail = (event as CustomEvent<{ pageId: string; ids: readonly string[] }>).detail;
    if (detail?.pageId === pageId) listener(detail.ids);
  };
  document.addEventListener(pageChooseEvent, heard);
  return () => document.removeEventListener(pageChooseEvent, heard);
}

/** Runs undo for the page's requests while listening: whether it took the step, or when the step it took began. */
export function listenForPageUndo(pageId: string, undo: (forward: boolean) => boolean | Promise<void>) {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<PageUndoDetail>).detail;
    if (!detail || detail.handled || detail.pageId !== pageId) return;
    const taken = undo(detail.forward);
    detail.handled = taken !== false;
    if (taken instanceof Promise) detail.begun = taken;
  };
  document.addEventListener(pageUndoEvent, listener);
  return () => document.removeEventListener(pageUndoEvent, listener);
}
