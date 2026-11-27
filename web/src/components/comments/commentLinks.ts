import { useEffect } from "react";
import type { CommentService } from "@/lib/commentService";
import { commentLinkTarget } from "@/lib/commentLinkTargets";
import { motionSafeScrollBehavior } from "@/lib/motion";
import { pageHref } from "@/lib/navigation";
import { requestEditorBlockSelection } from "@/components/editor/editorCommands";
import { commentBlockElement } from "./commentBlockElement";
import { openComments } from "./commentPanelEvents";
import { noteGoneDiscussionLink } from "./GoneDiscussionCard";
import { serverUrl } from "@/lib/serverOrigin";

// A thread's address, as Notion's "Copy link" (seen 2026-09-30): the page
// with the thread named in the fragment, which opens the page and that
// discussion. Comment notifications link a comment the same way, and an
// address whose comment was deleted since opens that comment's discussion
// while it is there; a block's discussion deleted whole opens at its block,
// chosen, with a card saying none is left (GoneDiscussionCard), and a page's
// opens nothing, as Notion's (seen 2026-11-21).

const COMMENT_LINK = /^#comment-(.+)$/u;
const BLOCK_WAIT_MS = 3_000;

/** The address of the thread whose first comment is `rootCommentId`. */
export function commentThreadLink(pageId: string, rootCommentId: string) {
  return serverUrl(`${pageHref(pageId)}#comment-${encodeURIComponent(rootCommentId)}`);
}

function linkedCommentId() {
  const match = COMMENT_LINK.exec(window.location.hash);
  if (!match) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return null;
  }
}

/**
 * Opening a page (or a database row's page, keyed by the row) at a
 * comment's address opens that comment's discussion with the comment in
 * view, and brings a block's discussion's block into view.
 */
export function useCommentLinkOpen(service: CommentService | null, pageId: string) {
  useEffect(() => {
    if (!service) return;
    let cancelled = false;
    let blockTimer: ReturnType<typeof setTimeout> | undefined;
    const comments = () => service.getSnapshot(pageId).comments;

    async function open() {
      const linkedId = linkedCommentId();
      if (!linkedId || !service) return;
      const find = (commentId: string) => comments().find((comment) => comment.id === commentId);
      // The page's comments are read whole (commentPageReads.ts); a read left partway is finished.
      const seek = async (commentId: string, restart: boolean) => {
        await service.load(pageId, restart).catch(() => {});
        if (!cancelled && !find(commentId)) await service.loadMore(pageId).catch(() => {});
      };
      await seek(linkedId, false);
      // A comment newer than the list read here, such as one an update links
      // to on the open page, is read again before it is given up.
      if (!cancelled && !find(linkedId)) await seek(linkedId, true);
      // A deleted comment's address opens its discussion as it is now, led by
      // the reply that took it over, or the block a discussion deleted whole
      // was left on (commentLinkTargets.ts).
      const discussionNow = async (): Promise<{ comment?: ReturnType<typeof find>; goneBlockId?: string }> => {
        const now = cancelled ? null : await commentLinkTarget(service, pageId, linkedId);
        if (!now?.commentId) return { goneBlockId: now?.blockId };
        if (!cancelled && !find(now.commentId)) await seek(now.commentId, true);
        return { comment: find(now.commentId) };
      };
      const linked = find(linkedId);
      const { comment, goneBlockId } = linked ? { comment: linked, goneBlockId: undefined } : await discussionNow();
      if (cancelled) return;
      if (!comment) {
        if (!goneBlockId) return;
        noteGoneDiscussionLink(linkedId);
        openComments({ pageId, blockId: goneBlockId, activeCommentId: linkedId, quiet: true });
        revealBlock(goneBlockId, Date.now(), true);
        return;
      }
      const root = comment.parentId ? comments().find((item) => item.id === comment.parentId) ?? comment : comment;
      openComments({ pageId, blockId: root.blockId || null, activeCommentId: comment.id, quiet: true,
        ...(root.blockId ? { linkedThreadId: root.id } : {}) });
      // Opened at its address — a notice's, a copied link — the thread is read, as Notion's (seen 2026-11-07):
      // a page's own at once, a block's when its window closes (useCommentsPanelController).
      if (root.unread && !root.blockId) void service.setThreadState(pageId, root, { unread: false }).catch(() => {});
      if (root.blockId) revealBlock(root.blockId, Date.now());
    }

    // The block may render after the discussion opens; a discussion deleted
    // whole leaves its block chosen (`choose`), as Notion's.
    function revealBlock(blockId: string, since: number, choose = false) {
      const block = commentBlockElement(blockId);
      if (block) {
        block.scrollIntoView({ behavior: motionSafeScrollBehavior(), block: "center", inline: "nearest" });
        // A table row's discussion scrolls to its row's cells; the row is no block to choose.
        if (choose && !block.hasAttribute("data-table-cell")) requestEditorBlockSelection(blockId, false, block);
        return;
      }
      if (!cancelled && Date.now() - since < BLOCK_WAIT_MS) blockTimer = setTimeout(() => revealBlock(blockId, since, choose), 100);
    }

    const onHashChange = () => void open();
    void open();
    window.addEventListener("hashchange", onHashChange);
    return () => {
      cancelled = true;
      if (blockTimer) clearTimeout(blockTimer);
      window.removeEventListener("hashchange", onHashChange);
    };
  }, [pageId, service]);
}
