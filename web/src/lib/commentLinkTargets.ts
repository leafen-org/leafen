import type { CommentLinkResponse } from "@/commentWire.generated";
import type { CommentService } from "./commentService";

// A comment's address names a comment, and a discussion outlives its first
// comment: deleted, its first reply takes the discussion over. Notion's
// address names the discussion, and an old one still opens it (seen
// 2026-11-21 twice), so an address naming a comment no longer on the page is
// asked of the server, which follows what stays of the deleted comment to its
// discussion (comment_link_targets.go) — or, the discussion deleted whole, to
// the block it was left on.

/**
 * What an address naming `commentId` opens now on the service's page (or
 * row): its discussion's first comment if it was deleted (`commentId`), the
 * block of a block's discussion deleted whole (`blockId` alone); null when
 * nothing is left to open, or the answer didn't come.
 */
export async function commentLinkTarget(
  service: CommentService,
  pageId: string,
  commentId: string,
): Promise<CommentLinkResponse | null> {
  try {
    const response = await service.transport.readCommentLink(service.session, pageId, commentId);
    return response.commentId || response.blockId ? response : null;
  } catch {
    return null;
  }
}
