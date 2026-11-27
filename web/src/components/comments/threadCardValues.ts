import type { NativeComment } from "@/lib/commentService";

/**
 * What the comments controller hands each thread's card
 * (CommentThreadCard), kept the same while that thread stays the same: the
 * card is drawn again only when its own discussion changes, not when another
 * one does (2026-11-25: pressing ✓ or ✗ on one of 150 discussions in "모든
 * 토론" drew all 150 cards, 123–158 ms under the NAS condition).
 */

/** No replies: one list for every thread without any. */
export const NO_REPLIES: readonly NativeComment[] = Object.freeze([]);

/**
 * Each thread's replies in the page's order, by its first comment; a thread
 * whose replies are the very comments `kept` lists for it keeps that list.
 */
export function repliesByThread(
  comments: readonly NativeComment[],
  kept: ReadonlyMap<string, readonly NativeComment[]>,
): ReadonlyMap<string, readonly NativeComment[]> {
  const read = new Map<string, NativeComment[]>();
  for (const comment of comments) {
    if (!comment.parentId) continue;
    const replies = read.get(comment.parentId);
    if (replies) replies.push(comment);
    else read.set(comment.parentId, [comment]);
  }
  const threads = new Map<string, readonly NativeComment[]>();
  for (const [id, replies] of read) {
    const before = kept.get(id);
    threads.set(id, before && before.length === replies.length && before.every((reply, index) => reply === replies[index]) ? before : replies);
  }
  return threads;
}

type Calls = Record<string, (...args: never[]) => unknown>;

/**
 * An object whose functions call the ones `latest` holds at the time of the
 * call: the same object, and the same functions, however often those change.
 */
export function latestCalls<T extends Calls>(latest: { readonly current: T }): T {
  const calls: Record<string, (...args: unknown[]) => unknown> = {};
  for (const key of Object.keys(latest.current)) {
    calls[key] = (...args) => (latest.current[key] as (...given: unknown[]) => unknown)(...args);
  }
  return calls as unknown as T;
}
