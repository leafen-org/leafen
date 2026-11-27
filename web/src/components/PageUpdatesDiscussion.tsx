import { PersonAvatar } from "@/components/people/PersonAvatar";
import { type MouseEvent, type ReactNode, useEffect, useMemo, useState } from "react";
import { Trans, useTranslation } from "react-i18next";

import { api } from "@/api";
import type { CommentActorProfile } from "@/commentWire.generated";
import { commentServiceFor, databaseRowCommentServiceFor, type NativeComment } from "@/lib/commentService";
import { pageHref } from "@/lib/navigation";
import type { Session } from "@/types";
import { authorLabel, commentAuthorFace, commentPropertyId, isReactionThread, quoteText, renderCommentRichText, richTextSpans } from "./comments/commentModel";
import { openComments } from "./comments/commentPanelEvents";
import { discussionLastAt } from "./comments/discussionListModel";
import { useShownBlockText } from "./comments/shownBlockText";
import { suggestionFacts } from "./comments/suggestionModel";
import { RowPropertyCommentsPopover } from "./database/RowPropertyComments";
import { usePageCommentSummary } from "./page/usePageCommentSummary";

import styles from "./PageAnalyticsPanel.module.css";

/** A discussion listed among a page's updates, at the time it was last written in. */
export type PageUpdateDiscussionEntry = { discussion: { root: NativeComment; comments: readonly NativeComment[] }; at: number };

/**
 * A page's discussions as its updates tab lists them, as Notion's (seen
 * 2026-11-11): each discussion of comments on the page, its blocks or its
 * text — a database page's property values too (seen 2026-11-12) — is one
 * update, at its latest comment, resolved ones too. A thread only
 * reacted to and a suggested edit comment nothing. Every comment the page
 * has is read, so its newest discussions are among them.
 */
export function usePageUpdateDiscussions({ pageId, row, session, enabled }: {
  pageId: string;
  /** A database row's page, whose discussion is read through its row. */
  row: boolean;
  session: Session;
  enabled: boolean;
}) {
  const summary = usePageCommentSummary({ pageId: enabled ? pageId : undefined, row, session });
  useEffect(() => {
    if (!enabled) return;
    const service = (row ? databaseRowCommentServiceFor : commentServiceFor)(session, api);
    // Read whole (commentPageReads.ts); a read left partway is finished.
    void service.load(pageId).then(() => service.loadMore(pageId)).catch(() => {});
  }, [enabled, pageId, row, session]);
  const entries = useMemo<PageUpdateDiscussionEntry[]>(() => summary.threads
    .filter((root) => !isReactionThread(root) && !suggestionFacts(root))
    .map((root) => ({
      discussion: { root, comments: [root, ...summary.replies.get(root.id) ?? []] },
      at: discussionLastAt(root, summary.latestReplies),
    })), [summary.threads, summary.replies, summary.latestReplies]);
  return { entries, actorProfiles: summary.actorProfiles };
}

/**
 * The words a discussion was left on: its quote — a property value's words
 * alone, without the property's name (Notion's, seen 2026-11-12) — or a
 * block's own text when it was left on the whole block.
 */
function DiscussionQuote({ root, href, onFollow }: { root: NativeComment; href: string; onFollow(event: MouseEvent<HTMLAnchorElement>): void }) {
  const quote = quoteText(root.body, { words: true });
  const blockText = useShownBlockText(root.blockId && !quote ? root.blockId : null);
  const text = quote || (root.blockId ? blockText : "");
  if (!text) return null;
  return (
    <a className={styles.updateDiscussionLine} href={href} onClick={onFollow}>
      {/* An open discussion's words keep the page's mark; a resolved one's are plain (Notion, seen 2026-11-11). */}
      <span className={styles.updateDiscussionQuote} data-resolved={root.resolved ? "true" : undefined}>{text}</span>
    </a>
  );
}

/** The database page a row's updates are listed for, whose property discussions open from them. */
export type PageUpdateRow = { containerPageId: string; workspaceId?: string; canEditPage: boolean };

/**
 * One discussion among a page's updates, as Notion's (seen 2026-11-11): its
 * last writer's face, "〈page〉에 댓글을 달았습니다.", when and where, the
 * words it was left on, each comment under its writer's name, and "답글",
 * which opens the discussion in the page to answer it — a property value's
 * in its own window centred under "답글", resolved too (seen 2026-11-12).
 * The words and the comments link to the discussion.
 */
export function PageUpdateDiscussion({ entry, pageId, page, meta, actorProfiles, session, row, onFollow }: {
  entry: PageUpdateDiscussionEntry;
  pageId: string;
  /** The page's title, as the line names it. */
  page: string;
  /** When and where, as each update says it. */
  meta: ReactNode;
  actorProfiles: Readonly<Record<string, CommentActorProfile>>;
  session: Session;
  /** A database page's updates, whose property discussions open beside them. */
  row?: PageUpdateRow;
  onFollow(event: MouseEvent<HTMLAnchorElement>): void;
}) {
  const { t } = useTranslation("pageAnalytics");
  const { root, comments } = entry.discussion;
  const propertyId = commentPropertyId(root.body);
  const propertyName = (root.body as { propertyName?: unknown } | null)?.propertyName;
  const [replyAt, setReplyAt] = useState<DOMRect | null>(null);
  const userId = session.user.id;
  const time = (comment: NativeComment) => Date.parse(comment.createdAt ?? "") || 0;
  const last = comments.reduce((latest, comment) => (time(comment) >= time(latest) ? comment : latest), root);
  const link = (comment: NativeComment) => `${pageHref(pageId)}#comment-${encodeURIComponent(comment.id)}`;
  return (
    <li className={styles.update} data-page-update="true" data-page-update-discussion={root.id}>
      <PersonAvatar className={styles.updateAvatar} name={authorLabel(last, userId, actorProfiles)}
        {...commentAuthorFace(last, actorProfiles)} size={24} />
      <div className={styles.updateBody}>
        <p className={`${styles.updateSummary} ${styles.updateDiscussionSummary}`}>
          <Trans t={t} i18nKey="updates.commented" values={{ page }}
            components={{ page: <a href={pageHref(pageId)} className={styles.updatePageLink} onClick={onFollow} /> }} />
        </p>
        {meta}
        <DiscussionQuote root={root} href={link(root)} onFollow={onFollow} />
        {comments.map((comment) => (
          <a key={comment.id} className={styles.updateComment} href={link(comment)} onClick={onFollow} data-page-update-comment={comment.id}>
            <span className={styles.updateCommentAuthor}>{authorLabel(comment, userId, actorProfiles)}</span>
            <span className={styles.updateCommentText}>{renderCommentRichText(richTextSpans(comment.body))}</span>
          </a>
        ))}
        <button type="button" className={styles.updateReply} data-page-update-reply={root.id}
          aria-haspopup={propertyId && row ? "dialog" : undefined} aria-expanded={propertyId && row ? Boolean(replyAt) : undefined}
          onClick={(event) => {
            if (propertyId && row) setReplyAt(event.currentTarget.getBoundingClientRect());
            else openComments({ pageId, blockId: root.blockId ?? null, activeCommentId: root.id });
          }}>
          {t("updates.reply")}
        </button>
        {replyAt && row ? (
          <RowPropertyCommentsPopover
            comments={{ containerPageId: row.containerPageId, rowId: pageId, session, workspaceId: row.workspaceId,
              canEditPage: row.canEditPage, openByProperty: new Map() }}
            property={{ id: propertyId, name: typeof propertyName === "string" ? propertyName : "" }} quote=""
            anchor={{ left: replyAt.left + replyAt.width / 2, top: replyAt.bottom + 6 }} centered
            threadId={root.id} resolvedToo onClose={() => setReplyAt(null)} />
        ) : null}
      </div>
    </li>
  );
}
