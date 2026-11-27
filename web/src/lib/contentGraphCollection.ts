import { type GraphReadItem, type GraphReadResponse } from "@/contentGraph";

export type ContentGraphCollectionOptions = {
  readPage: (cursor: string | null) => Promise<GraphReadResponse>;
  /** The items read so far, after each window; none when only the whole read is wanted. */
  onSnapshot?: (items: GraphReadItem[]) => void;
  onInvalidContinuation: () => never;
  shouldContinue?: () => boolean;
};

export type ContentGraphCollectionResult = {
  items: GraphReadItem[];
  /**
   * The page's position the graph holds every change up to: its first
   * window's, when the page changed while it was read.
   */
  snapshotRevision: number;
  /** The server would take this reader's edits to the page's content. */
  canEdit: boolean;
  /** The server would take this reader's suggested edits to the page's text. */
  canSuggest: boolean;
  /** The operation holding the page for now, when it takes no edits for that. */
  busy?: string;
};

/**
 * Reads a page's graph a window at a time. A change to the page while it is
 * read doesn't start the read over: the windows after it are read as the
 * page is then, an item met twice (moved past the cursor) counts as last
 * read, and the read says the position of its first window, so the page's
 * reader finds itself behind the change and reads it again once it shows
 * (2026-11-11: a database of 100,000 rows, read in some 20 seconds, began
 * again with each edit made meanwhile and failed after the second).
 */
export async function collectContentGraphPages(
  options: ContentGraphCollectionOptions,
): Promise<ContentGraphCollectionResult | null> {
  const items: GraphReadItem[] = [];
  const seenCursors = new Set<string>();
  const shouldContinue = options.shouldContinue ?? (() => true);
  let cursor: string | null = null;
  let pageId: string | null = null;
  let snapshotRevision: number | null = null;
  let changed = false;

  while (shouldContinue()) {
    const result = await options.readPage(cursor);
    if (!shouldContinue()) return null;
    if (pageId !== null && result.pageId !== pageId) options.onInvalidContinuation();
    if (snapshotRevision !== null && result.snapshotRevision !== snapshotRevision) changed = true;
    pageId = result.pageId;
    snapshotRevision = Math.min(snapshotRevision ?? result.snapshotRevision, result.snapshotRevision);
    items.push(...result.items);
    options.onSnapshot?.(changed ? lastRead(items, itemKey) : items.slice());
    if (!result.hasMore) {
      return {
        items: changed ? lastRead(items, itemKey) : items.slice(), snapshotRevision,
        canEdit: result.canEdit === true, canSuggest: result.canSuggest === true,
        ...(result.busy ? { busy: result.busy } : {}),
      };
    }
    if (!result.nextCursor || seenCursors.has(result.nextCursor)) {
      options.onInvalidContinuation();
    }
    seenCursors.add(result.nextCursor);
    cursor = result.nextCursor;
  }
  return null;
}

/**
 * Each item once, as its last window read it, in the order read: what a read
 * in windows holds when what it reads changed meanwhile (a row page's body
 * too, databaseRowPageService.ts).
 */
export function lastRead<T>(items: readonly T[], key: (item: T) => string): T[] {
  const last = new Map<string, number>();
  items.forEach((item, index) => last.set(key(item), index));
  return items.filter((item, index) => last.get(key(item)) === index);
}

function itemKey(item: GraphReadItem) {
  const value = item.value as { id?: unknown; pageId?: unknown };
  return `${item.kind}\u0000${String(value.id ?? value.pageId ?? "")}`;
}
