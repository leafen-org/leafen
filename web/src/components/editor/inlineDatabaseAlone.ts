import { useEffect, useRef, useSyncExternalStore } from "react";
import type { GraphBlock } from "@/contentGraph";
import type { Page, Session } from "@/types";
import { useOptionalProductToast } from "@/lib/productToast";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { announceBlocksDeleted, confirmBlockRemoval } from "./databaseBlockRemoval";
import { moveBlocksToPageUndoable } from "./blockMoveHistory";

/**
 * An inline database opened alone as a page, as its page's "⋯" menu changes
 * it: the database itself, not the page holding it, as Notion's (seen
 * 2026-11-11: its lock is "데이터베이스 잠금", "복제" copies it beside itself
 * and opens the copy, "옮기기" and "휴지통으로 이동" take it, and its foot
 * counts no words). Its block's editor, which holds it, does what is asked.
 */
export type InlineDatabaseAlone = {
  blockId: string;
  /** The page holding it, and its workspace. */
  pageId: string;
  workspaceId: string;
  /** A view block — a linked database, a dashboard — whose lock is "보기 잠금". */
  views: boolean;
  locked: boolean;
  archived: boolean;
  /** The source its shown view reads, which "CSV와 병합" merges into. */
  dataSourceId: string;
  title: string;
  setLocked(locked: boolean): void;
  setArchived(archived: boolean): void;
  /** Its copy beside it, with or without its rows; the copy's block, or null when none was made. */
  duplicate(withoutContent: boolean): Promise<string | null>;
  /** Asks, as Notion's does, then takes it off its page; whether it went. */
  trash(): Promise<boolean>;
  /**
   * What the move window needs, and the move to the page chosen (no
   * database is one), answering how to take it back.
   */
  move?: {
    pages: Page[];
    session: Session;
    onPageChange: (page: Page) => void;
    onStatus?: (message: string, tone?: "default" | "success" | "error") => void;
    /** Its undo calls `back` once the server has it back, and answers whether it went. */
    to(destination: Page, signal: AbortSignal): Promise<{ undo?: (back?: () => void) => Promise<boolean> }>;
  };
};

const shown = new Map<string, { current: InlineDatabaseAlone }>();
const listeners = new Set<() => void>();
let revision = 0;

function changed() {
  revision += 1;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The inline database drawn alone gives its page's menu what it does to itself; nothing when it is drawn on its page. */
export function useShareInlineDatabaseAlone(adapter: NativeEditorGraphAdapter, block: GraphBlock, state: {
  alone: boolean; views: boolean; dataSourceId: string; title: string;
}) {
  const toast = useOptionalProductToast();
  const viewAdapter = adapter.database.databaseViewAdapter;
  const context = adapter.moveContext;
  const actions: InlineDatabaseAlone | null = state.alone && viewAdapter && !adapter.readOnly ? {
    blockId: block.id, pageId: adapter.pageId, workspaceId: adapter.workspaceId, views: state.views,
    locked: block.content.locked === true, archived: block.content.archived === true,
    dataSourceId: state.dataSourceId, title: state.title,
    setLocked: (locked) => viewAdapter.setLocked(block, locked),
    setArchived: (archived) => viewAdapter.setArchived(block, archived),
    duplicate: async (withoutContent) => (await adapter.copyOnServer([block.id], { withoutContent }))[0] ?? null,
    trash: async () => {
      if (!await confirmBlockRemoval(adapter.graph, [block.id], { session: adapter.session, databases: [] })) return false;
      adapter.remove([block.id]);
      announceBlocksDeleted(toast, 1);
      return true;
    },
    move: context ? {
      pages: context.pages, session: context.session, onPageChange: context.onPageChange, onStatus: context.onStatus,
      to: async (destination, signal) => ({ undo: await moveBlocksToPageUndoable(adapter, [block.id], destination.id, signal) }),
    } : undefined,
  } : null;
  const latest = useRef(actions);
  latest.current = actions;
  const present = actions !== null;
  const { locked, archived } = actions ?? { locked: false, archived: false };
  useEffect(() => {
    if (!present) return;
    const entry = { get current() { return latest.current as InlineDatabaseAlone; } };
    shown.set(block.id, entry);
    changed();
    return () => {
      if (shown.get(block.id) === entry) shown.delete(block.id);
      changed();
    };
  }, [block.id, present]);
  useEffect(() => { if (present) changed(); }, [present, locked, archived, state.views, state.title]);
}

/** What this inline database, drawn alone, does to itself; null while it isn't drawn alone or can't be changed. */
export function useInlineDatabaseAlone(blockId: string | undefined): InlineDatabaseAlone | null {
  useSyncExternalStore(subscribe, () => revision, () => revision);
  return blockId ? shown.get(blockId)?.current ?? null : null;
}
