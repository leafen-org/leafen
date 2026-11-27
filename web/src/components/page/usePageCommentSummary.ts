import type { ProductStatusSink } from "@/lib/productStatus";
import { useEffect, useMemo, useSyncExternalStore } from "react";
import i18next from "i18next";
import { api } from "@/api";
import { userErrorToast } from "@/lib/settingsErrors";
import {
  commentServiceFor,
  databaseRowCommentServiceFor,
  type CommentPageSnapshot,
  type NativeComment,
} from "@/lib/commentService";
import type { Session } from "@/types";
import { commentPropertyId, quoteRange } from "@/components/comments/commentModel";
import { discussionShownWhole, latestRepliesOf } from "@/components/comments/discussionListModel";
import { isGuestMFARequired } from "@/lib/guestMFARequired";
import { commentReadGone } from "@/components/comments/commentRefusal";

/**
 * A page's discussions from its comments: each thread's first comment
 * (a database page's property threads are its properties': they show on
 * their lines, not in the bar — Notion's, seen 2026-10-01; those only
 * reacted to count for the bar's button, but its lists leave them out —
 * listedDiscussions), each one's
 * latest reply, which its line shows (Notion's, seen 2026-10-08), and its
 * replies. The bar's "토론" for members and a web link's visitors alike.
 */
export function pageDiscussions(comments: readonly NativeComment[]) {
  const threads = comments.filter((comment) => !comment.parentId);
  const discussions = threads.filter((comment) => !commentPropertyId(comment.body));
  const replies = new Map<string, NativeComment[]>();
  for (const comment of comments) {
    if (comment.parentId) replies.set(comment.parentId, [...(replies.get(comment.parentId) ?? []), comment]);
  }
  return {
    threads, discussions,
    latestReplies: latestRepliesOf(comments),
    replies: replies as ReadonlyMap<string, readonly NativeComment[]>,
  };
}

const EMPTY_COMMENT_SUMMARY: CommentPageSnapshot = Object.freeze({
  actorProfiles: Object.freeze({}),
  comments: Object.freeze([]),
  error: null,
  hasMore: false,
  loading: false,
  pageReactions: Object.freeze([]),
  pendingDeletion: null,
  sourceRevision: null,
});

/**
 * A page's discussions as its top bar shows them: whether it has any, open or
 * resolved, which is when Notion's bar offers its comments, and how many of
 * its own are open. A database page's are read through its row.
 */
export function usePageCommentSummary({
  onStatus,
  pageId,
  row = false,
  session,
}: {
  onStatus?: ProductStatusSink;
  pageId?: string;
  row?: boolean;
  session?: Session;
}) {
  const service = useMemo(
    () => session ? (row ? databaseRowCommentServiceFor : commentServiceFor)(session, api) : null,
    [row, session],
  );
  const subscribe = useMemo(
    () => (listener: () => void) => (
      service && pageId ? service.subscribe(pageId, listener) : () => {}
    ),
    [pageId, service],
  );
  const getSnapshot = useMemo(
    () => () => service && pageId ? service.getSnapshot(pageId) : EMPTY_COMMENT_SUMMARY,
    [pageId, service],
  );
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);

  useEffect(() => {
    if (!service || !pageId) return;
    void service.load(pageId).catch(() => {});
  }, [pageId, service]);

  useEffect(() => {
    if (!snapshot.error || !onStatus) return;
    // A guest asked for 2-step verification sees the page's screen for it, not this refusal,
    // and so does one whose page is no longer theirs (the card saying so).
    if (isGuestMFARequired(snapshot.error) || commentReadGone(snapshot.error)) return;
    const text = userErrorToast(snapshot.error, i18next.t("commentsPanel:errors.loadFailed"));
    if (text) onStatus(text, "error");
  }, [onStatus, snapshot.error]);

  const { threads, discussions, latestReplies, replies } = pageDiscussions(snapshot.comments);
  const openByProperty = new Map<string, number>();
  const wholeValueProperties = new Set<string>();
  const wordThreads: string[] = [];
  for (const comment of threads) {
    const propertyId = commentPropertyId(comment.body);
    if (!propertyId || comment.resolved) continue;
    openByProperty.set(propertyId, (openByProperty.get(propertyId) ?? 0) + 1);
    if (!quoteRange(comment.body)) wholeValueProperties.add(propertyId);
    else wordThreads.push(comment.id);
  }
  return {
    any: discussions.length > 0,
    /**
     * An open discussion is unread to the reader (lib/commentThreadUnread.ts):
     * the top bar's comments mark it, as Notion's — but for one shown whole
     * under the title (discussionShownWhole).
     */
    unread: discussions.some((comment) => !comment.resolved && comment.unread && !discussionShownWhole(comment, replies.get(comment.id))),
    open: discussions.filter((comment) => !comment.blockId && !comment.resolved).length,
    /** The page's own reactions, which keep its discussion under its title too (usePageDiscussionPlace). */
    reactions: snapshot.pageReactions.length,
    /** Every discussion's first comment, on the page, its blocks or its text. */
    discussions,
    /** Every thread's first comment, a database page's property threads too. */
    threads,
    /** Each discussion's latest reply, by its first comment. */
    latestReplies,
    /** Each discussion's replies, by its first comment. */
    replies,
    /** How many open threads each of a database page's properties has. */
    openByProperty,
    /** The properties with an open thread on their whole value, which tints it; a thread on some words marks those. */
    wholeValueProperties: wholeValueProperties as ReadonlySet<string>,
    /** The open threads on some of a property's words, whose marks show (CellCommentMarks). */
    wordThreads: wordThreads as readonly string[],
    actorProfiles: snapshot.actorProfiles,
  };
}
