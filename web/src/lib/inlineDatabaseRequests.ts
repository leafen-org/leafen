import { useEffect, useRef } from "react";
import { shownBlockCopy } from "./shownBlockCopy";

/**
 * What an inline database's or a sub-page's block menu asks the block itself
 * to do; "title" is its title's own "⋯ › 제목 편집", which names a linked
 * view's source in its head where "rename" opens its own name's box.
 */
export type InlineDatabaseRequest = "icon" | "rename" | "title" | "subitems" | "view";

/** A database block's copy a listener is part of: the one drawn alone (a peek's, its own page's) or its page's. */
export type InlineDatabaseCopy = { blockId: string; alone: boolean };
type Listener = { answer(request: InlineDatabaseRequest): void; copy?: InlineDatabaseCopy };

const listeners = new Map<string, Set<Listener>>();

/**
 * Asks the inline database of this block to open its icon picker or name
 * itself; false when none is shown. A block drawn twice answers in the copy
 * that shows (shownBlockCopy), so its window opens once.
 */
export function requestInlineDatabase(blockId: string, request: InlineDatabaseRequest) {
  const shown = [...listeners.get(blockId) ?? []].filter(({ copy }) => !copy || shownBlockCopy(copy.blockId, copy.alone));
  for (const listener of shown) listener.answer(request);
  return shown.length > 0;
}

/** The shown inline database of this block answers its block menu's requests. */
export function useInlineDatabaseRequests(blockId: string, onRequest: (request: InlineDatabaseRequest) => void, copy?: InlineDatabaseCopy) {
  const latest = useRef(onRequest);
  latest.current = onRequest;
  const copyBlockId = copy?.blockId, alone = copy?.alone === true;
  useEffect(() => {
    const listener: Listener = { answer: (request) => latest.current(request), ...(copyBlockId ? { copy: { blockId: copyBlockId, alone } } : {}) };
    const shown = listeners.get(blockId) ?? new Set();
    shown.add(listener);
    listeners.set(blockId, shown);
    return () => {
      shown.delete(listener);
      if (!shown.size) listeners.delete(blockId);
    };
  }, [blockId, copyBlockId, alone]);
}

/**
 * How a shown inline database's title joins a line, as Notion's does (seen
 * 2026-11-03): ⌫ at the start of the line after it puts the line at the
 * title's end, the caret there, and ⌦ at the end of the line before it takes
 * the title into the line, the database going to the trash.
 */
export type InlineDatabaseTitleJoin = {
  title: string;
  set(title: string, caretAt?: number, undo?: () => void): void;
  /** Undoes what was typed in a page's title, its caret going there; false when there is nothing. */
  undoTyped?(): boolean;
  /** The caret at its end, when the line under it goes. */
  caretAtEnd?(): void;
  /** The caret at its start, when a page's only block comes back (nativeTiptapController caretInDatabaseBack). */
  caretAtStart?(): void;
};

const titleJoins = new Map<string, Set<{ current: () => InlineDatabaseTitleJoin | null }>>();

/** The title join of this block's shown inline database; null when its title can't take part (linked, locked, hidden). */
export function inlineDatabaseTitleJoin(blockId: string) {
  for (const join of titleJoins.get(blockId) ?? []) {
    const answer = join.current();
    if (answer) return answer;
  }
  return null;
}

const rowTitleUndos = new Map<string, { undo: () => boolean; caretAtEnd: () => void }>();

/**
 * A row page's title takes the Undo the body has used up, as a page's does,
 * and the caret when the line its Enter made goes, in the same step as a
 * page's (seen 2026-11-05: Notion's row takes three Undos for title, Enter
 * and a line; the row page took four, its caret staying in the empty body).
 */
export function registerRowTitleUndo(rowId: string, undo: () => boolean, caretAtEnd: () => void) {
  const entry = { undo, caretAtEnd };
  rowTitleUndos.set(rowId, entry);
  return () => { if (rowTitleUndos.get(rowId) === entry) rowTitleUndos.delete(rowId); };
}

export function rowTitleUndo(rowId: string) {
  return rowTitleUndos.get(rowId)?.undo() ?? false;
}

/** The caret at a row page title's end; whether there is one. */
export function rowTitleCaretAtEnd(rowId: string) {
  const entry = rowTitleUndos.get(rowId);
  entry?.caretAtEnd();
  return Boolean(entry);
}

/** A page's title takes part as a database's does, under the page's id. */
export const pageTitleJoin = inlineDatabaseTitleJoin;
export const usePageTitleJoin = useInlineDatabaseTitleJoin;

export function useInlineDatabaseTitleJoin(blockId: string, join: () => InlineDatabaseTitleJoin | null) {
  const latest = useRef(join);
  latest.current = join;
  useEffect(() => {
    const shown = titleJoins.get(blockId) ?? new Set();
    shown.add(latest);
    titleJoins.set(blockId, shown);
    return () => {
      shown.delete(latest);
      if (!shown.size) titleJoins.delete(blockId);
    };
  }, [blockId]);
}
