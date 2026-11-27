import { useEffect, useRef } from "react";
import type { CommentService, NativeComment } from "@/lib/commentService";

// The reads waiting on a block's discussion window to stay closed.
const closeReads = new Map<string, ReturnType<typeof setTimeout>>();

/**
 * A block's discussion window reads, as it closes, the discussions it showed:
 * the one opened at its address (Notion's, seen 2026-11-07) and, in a window
 * opened from the block's mark or its marked words (`readsShown`), every one
 * it showed, as Notion's (seen 2026-11-25 in Chrome at 900, two tabs at
 * different times: a window opened from a block's mark kept its discussion's
 * dot and "New" while open, and closed by Esc, by a press outside it or by
 * another block's mark, read it at once and in a new tab after). One the
 * person marked unread while it was open stays so, as beside a block
 * (MarginDiscussions). A page's own discussion is read as it opens
 * (commentLinks.ts).
 */
export function useWindowCloseReads(input: {
  service: CommentService;
  /** The service's key for the discussions: the page, or a database row. */
  target: string;
  blockId?: string | null;
  /** The comment opened at its address, quietly. */
  addressed?: string;
  readsShown: boolean;
  shown: readonly NativeComment[];
  comments: readonly NativeComment[];
}) {
  const { service, target, blockId, addressed, readsShown } = input;
  const key = blockId && (readsShown || addressed) ? `${target}:${blockId}:${addressed ?? ""}` : undefined;
  // Each discussion to read, by its first comment, and whether the person had
  // it marked unread when the window first showed it.
  const seen = useRef(new Map<string, boolean>());
  useEffect(() => {
    if (!key) return;
    const linked = addressed ? input.comments.find((comment) => comment.id === addressed) : undefined;
    const root = linked?.parentId ? input.comments.find((comment) => comment.id === linked.parentId) : linked;
    for (const thread of [...(root ? [root] : []), ...(readsShown ? input.shown : [])]) {
      if (!seen.current.has(thread.id)) seen.current.set(thread.id, Boolean(thread.markedUnread));
    }
  });
  useEffect(() => {
    if (!key) return;
    // The same window shown again at once (React's development double run) is still open.
    const pending = closeReads.get(key);
    if (pending) clearTimeout(pending);
    closeReads.delete(key);
    const threads = seen.current;
    return () => {
      closeReads.set(key, setTimeout(() => {
        closeReads.delete(key);
        const comments = service.getSnapshot(target).comments;
        for (const [id, markedBefore] of threads) {
          const root = comments.find((comment) => comment.id === id);
          if (root?.unread && !(root.markedUnread && !markedBefore)) {
            void service.setThreadState(target, root, { unread: false }).catch(() => {});
          }
        }
      }, 0));
    };
  }, [key, service, target]);
}
