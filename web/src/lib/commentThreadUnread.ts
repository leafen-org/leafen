/**
 * Whether a thread is unread to the reader, as Notion's (seen 2026-11-07):
 * marked so, or holding a comment new to them — one someone else wrote that
 * they were told of after they last read the thread (the server says which,
 * comment_thread_states.go). Its first comment carries the answer, for the
 * thread's dot, its "Mark as read" and the page's red marks; the first new
 * comment draws the "New" line before it.
 */

type ThreadComment = {
  id: string;
  parentId?: string | null;
  /** On a thread's first comment: the person marked it unread. */
  markedUnread?: boolean;
  /** Someone else's comment the person hasn't read yet. */
  new?: boolean;
  unread?: boolean;
};

function threadOf(comment: ThreadComment) {
  return comment.parentId ?? comment.id;
}

/** The comments with each first comment's `unread` said for its whole thread. */
export function withThreadUnread<T extends ThreadComment>(comments: readonly T[]): T[] {
  const fresh = new Set<string>();
  for (const comment of comments) if (comment.new) fresh.add(threadOf(comment));
  return comments.map((comment) => {
    if (comment.parentId) return comment;
    const unread = Boolean(comment.markedUnread) || fresh.has(comment.id);
    return Boolean(comment.unread) === unread ? comment : { ...comment, unread: unread || undefined };
  });
}

/** The thread read to now: nothing in it new, nor marked unread. */
export function readThread<T extends ThreadComment>(comments: readonly T[], rootId: string): T[] {
  return comments.map((comment) => threadOf(comment) !== rootId || (!comment.new && !comment.markedUnread)
    ? comment : { ...comment, new: undefined, markedUnread: undefined });
}

/**
 * The thread's first comment new to the reader, before which the "New" line
 * goes: none when the whole thread is new — its dot says so (Notion's, seen
 * 2026-11-07).
 */
export function firstNewComment<T extends ThreadComment>(thread: readonly T[]): T | undefined {
  const first = thread.find((comment) => comment.new);
  return first && first !== thread[0] ? first : undefined;
}
