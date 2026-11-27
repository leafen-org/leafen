import type { NativeComment } from "./commentService";

/**
 * The parts of a first comment's body that are its discussion's: what it was
 * left on. They go to the reply that takes over (the server's
 * comment_thread_handover.go).
 */
const THREAD_BODY_KEYS = ["quote", "quoteStart", "quoteEnd", "quoteField", "quoteBlocks", "propertyId", "propertyName"] as const;

function earlier(left: NativeComment, right: NativeComment) {
  const leftAt = left.createdAt ?? "", rightAt = right.createdAt ?? "";
  if (leftAt !== rightAt) return leftAt < rightAt ? -1 : 1;
  return left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
}

function bodyWithThreadParts(reply: unknown, former: unknown): unknown {
  if (!reply || typeof reply !== "object") return reply;
  const next: Record<string, unknown> = { ...reply };
  const kept: Record<string, unknown> = former && typeof former === "object" ? { ...former } : {};
  for (const key of THREAD_BODY_KEYS) {
    delete next[key];
    if (key in kept) next[key] = kept[key];
  }
  return next;
}

/**
 * A page's comments once one is deleted, as Notion's delete leaves them (seen
 * 2026-11-21): the comment alone goes and its replies stay in their
 * discussion. A first comment's first reply takes over the discussion — its
 * place, its quote, whether it is resolved and the reader's state of it —
 * and the other replies are its replies; a reply's own replies go to its
 * parent. The server does the same and moves the discussion's marks.
 */
export function commentsAfterDelete(comments: readonly NativeComment[], deletedId: string): NativeComment[] {
  const deleted = comments.find((item) => item.id === deletedId);
  const rest = comments.filter((item) => item.id !== deletedId);
  if (!deleted) return rest;
  const replies = rest.filter((item) => item.parentId === deletedId).sort(earlier);
  const takesOver = !deleted.parentId ? replies[0] : undefined;
  const newParent = takesOver?.id ?? deleted.parentId;
  return rest.map((item) => {
    if (item === takesOver) {
      return {
        ...item,
        parentId: null,
        blockId: deleted.blockId,
        resolved: deleted.resolved,
        body: bodyWithThreadParts(item.body, deleted.body),
        revision: item.revision + 1,
        repliesMuted: deleted.repliesMuted,
        markedUnread: deleted.markedUnread,
      };
    }
    return item.parentId === deletedId ? { ...item, parentId: newParent ?? null } : item;
  });
}
