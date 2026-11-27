import type { CommentApi } from "@/commentApi";
import { commentBounds, commentErrorCodes, type CommentListResponse } from "@/commentWire.generated";
import type { Session } from "@/types";

const CHANGED_WHILE_READ = Symbol("comments changed while read");

function changedWhileRead(error: unknown) {
  return error === CHANGED_WHILE_READ || (!!error && typeof error === "object"
    && Reflect.get(error, "code") === commentErrorCodes.snapshotChanged);
}

/**
 * A page's comments, read whole. The server answers them oldest first, a
 * part at a time (commentBounds.listPage); everything that shows them — a
 * thread's newest reply and how many it folds, the margin cards, the blocks'
 * counts, the top bar's — needs every one, as Notion's page has all its
 * discussions (seen 2026-11-16: 35 comments in one thread show its first,
 * "답글 34개 더 보기" and the newest). Read only up to a part, a page of 33 or
 * more lost its newest from view, the one just written among them. Each part
 * goes to `onPart` with those before it as it comes; a page whose comments
 * change between two parts is read again from its first.
 */
export async function readWholeCommentPage(
  transport: Pick<CommentApi, "listComments">,
  session: Session,
  pageId: string,
  signal: AbortSignal,
  onPart?: (parts: readonly CommentListResponse[]) => void,
): Promise<CommentListResponse[]> {
  for (;;) {
    const parts: CommentListResponse[] = [];
    let cursor: string | undefined;
    try {
      do {
        const part = await transport.listComments(session, pageId, { cursor, limit: commentBounds.listPage }, signal);
        if (parts.length > 0 && part.sourceRevision !== parts[0].sourceRevision) throw CHANGED_WHILE_READ;
        parts.push(part);
        onPart?.(parts);
        cursor = part.hasMore ? part.nextCursor ?? undefined : undefined;
      } while (cursor);
      return parts;
    } catch (error) {
      if (signal.aborted || !changedWhileRead(error)) throw error;
    }
  }
}
