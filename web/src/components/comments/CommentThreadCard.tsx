import type { ProductStatusSink } from "@/lib/productStatus";
import { memo, useCallback, useRef, type MutableRefObject, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { CommentActorProfile } from "@/commentWire.generated";
import type { NativeComment } from "@/lib/commentService";
import { CommentCard } from "./CommentCard";
import type { CommentAttachmentDraft } from "./commentAttachmentDraft";
import type { CommentDraftStore } from "./commentDraftStore";
import type { CommentFieldHandle } from "./CommentRichField";
import { openDiscussionSide } from "./discussionSide";
import { useThreadMenuActions } from "./commentMenus";
import { quoteText } from "./commentModel";
import { useShownBlockText } from "./shownBlockText";
import { suggestionFacts } from "./suggestionModel";
import { NO_REPLIES } from "./threadCardValues";
import type { useCommentsPanelController } from "./useCommentsPanelController";

type CommentsController = ReturnType<typeof useCommentsPanelController>;

type ThreadCardProps = {
  comment: NativeComment;
  userId: string;
  canEditPage: boolean;
  canComment?: boolean;
  activeCommentId?: string;
  /** The active comment was opened at its address: no ring on it. */
  activeQuiet?: boolean;
  /** Drawn beside its block, in the page's margin. */
  beside?: boolean;
  /** Drawn in a card under its block. */
  under?: boolean;
  /** Drawn in a window under a database value, its reply box a block card's. */
  inWindow?: boolean;
  /** In such a window, what names the value before its quote: its property's icon and name. */
  quoteLabel?: ReactNode;
  /** Drawn under a page's title or in a feed post's discussion, answered from a box of its own its "↩" opens. */
  flat?: boolean;
  /**
   * Flat under a page's title, the title discussion whose view its "▯ 열기"
   * opens it in (DiscussionSide): none in a feed post's discussion or "모든
   * 토론", as Notion's.
   */
  sideOwner?: string;
  /** Drawn in that view of its own (DiscussionSideView). */
  alone?: boolean;
  /** Listed in "All discussions", quoting where it is. */
  listed?: boolean;
  /** The listed thread last pressed or reopened. */
  selected?: boolean;
  /** Choosing a listed thread, by pressing it or reopening it. */
  onSelect?(comment: NativeComment): void;
  /** The page, or database row, the thread's link opens. */
  linkTarget: string;
  onStatus?: ProductStatusSink;
};

/**
 * A thread as the comments panel and the page's margin show it: its card
 * with the reply box, reactions, resolving and its "…" menu, all through the
 * one comments controller of the page. What the controller gives the thread
 * stays the same while the thread does (threadCardValues.ts), so the card is
 * drawn again only when its own discussion changes.
 */
export function CommentThreadCard({ controller, onStatus, ...card }: ThreadCardProps & { controller: CommentsController }) {
  // The page may hand a new status function as it draws; the card keeps one.
  const statusNow = useRef(onStatus);
  statusNow.current = onStatus;
  const status = useCallback<ProductStatusSink>((message, tone) => statusNow.current?.(message, tone), []);
  const replying = controller.replyingId === card.comment.id;
  return (
    <ThreadCard {...card} onStatus={onStatus ? status : undefined} actions={controller.threadActions}
      activeBlockId={controller.blockId} actorProfiles={controller.actorProfiles}
      replies={controller.repliesByParent.get(card.comment.id) ?? NO_REPLIES} replying={replying}
      replyDraft={controller.replyDraftOf(card.comment.id)} replyRef={controller.replyRef}
      // Only the box being written sends (CommentReplyComposer); the others' send is off anyway.
      submittingReply={replying && controller.submittingReply} replyAttachments={controller.replyAttachments} />
  );
}

const ThreadCard = memo(function ThreadCard({
  comment, actions, activeBlockId, actorProfiles, replies, replying, replyDraft, replyRef, submittingReply, replyAttachments,
  userId, canEditPage, canComment = true, activeCommentId, activeQuiet, beside, under, inWindow, quoteLabel, flat, sideOwner, alone, listed, selected, onSelect, linkTarget, onStatus,
}: ThreadCardProps & {
  actions: CommentsController["threadActions"];
  activeBlockId?: string | null;
  actorProfiles: Readonly<Record<string, CommentActorProfile>>;
  replies: readonly NativeComment[];
  replying: boolean;
  replyDraft: CommentDraftStore;
  replyRef: MutableRefObject<CommentFieldHandle | null>;
  submittingReply: boolean;
  replyAttachments: CommentAttachmentDraft;
}) {
  // Listed, as Notion's: a comment on text quotes the text and one on a block
  // the block's text; a suggestion quotes nothing over its summary (seen
  // 2026-11-22 twice at 1440), and the page's own thread nothing, drawn as
  // under the page's title (seen 2026-11-25 in Chrome at 1440, two tabs).
  // One that lost what it was left on says so in place of its words, as Notion's (seen 2026-11-27; comment_original_gone.go).
  const { t } = useTranslation("commentsPanel");
  const suggested = listed && suggestionFacts(comment) !== null;
  const textQuote = suggested ? "" : quoteText(comment.body);
  const blockText = useShownBlockText(listed && !suggested && comment.blockId && !textQuote ? comment.blockId : null);
  const listedQuote = !listed ? undefined : suggested ? null : comment.originalGone ? t("originalGone")
    : textQuote || (comment.blockId ? blockText : null) || null;
  // Listed, a thread on the page's text pressed, or any reopened, opens its
  // reply row with the caret in it (Notion's, seen 2026-11-25 in two tabs:
  // pressed, a block's thread took a letter typed; a page's own took none):
  // the card does as the press says (CommentCard's openReply).
  const { say, changeThreadState, copyThreadLink } = useThreadMenuActions(actions, comment, linkTarget, onStatus);
  // Under a page's title its "▯ 열기" opens it in a view of its own, drawn as
  // a thread in a block's card is — its replies in its column, its reply row
  // — under its first comment's line, by the title's discussion
  // (DiscussionSide), which keeps it there once it is resolved. The address
  // names the open view (discussionSide.ts).
  const opensSide = flat && sideOwner ? () => openDiscussionSide(comment.id, sideOwner) : undefined;

  return (
    <CommentCard
      comment={comment} activeBlockId={activeBlockId}
      userId={userId} actorProfiles={actorProfiles}
      canComment={canComment} canEditPage={canEditPage} replies={replies}
      replying={replying} replyDraft={replyDraft}
      replyRef={replyRef} submittingReply={submittingReply}
      active={comment.id === activeCommentId} activeCommentId={activeCommentId} activeQuiet={activeQuiet} beside={beside} under={under} inWindow={inWindow} quoteLabel={quoteLabel} flat={flat} listedQuote={listedQuote}
      selected={selected}
      onRevealAnchor={() => {
        actions.reveal(comment);
        onSelect?.(comment);
        return Boolean(listed && comment.blockId);
      }} onStartReply={() => actions.startReply(comment)}
      // A listed thread's "↩" leaves the thread chosen before it chosen (Notion's, seen 2026-11-25 in two tabs).
      alone={alone} onOpenSide={opensSide}
      onReplyDraft={replyDraft.set}
      onSubmitReply={() => void actions.submitReply(comment)}
      onToggleResolved={() => {
        actions.toggleResolved(comment);
        // Reopened in the list it is chosen, the caret in its reply box, the
        // page and its address left where they are, as Notion's (seen
        // 2026-11-15 by the strike for a block's and a text's thread); in its
        // own view the caret goes into its reply row too, by its card's "다시
        // 열기" or its own button (Notion's, seen 2026-11-25 in Chrome at
        // 1440, two tabs — one letter typed after the press went there).
        if (!comment.resolved) return false;
        onSelect?.(comment);
        return Boolean(onSelect || alone);
      }}
      onSettleSuggestion={(decision) => actions.settle(comment, decision)}
      onEditComment={actions.edit} onAttachError={(message) => say(message, "error")}
      onDeleteComment={actions.remove} onReact={actions.react}
      onCopyLink={() => void copyThreadLink()}
      onThreadState={(change) => void changeThreadState(change)}
      replyAttachments={replyAttachments}
      onRemoveAttachment={actions.removeAttachment}
    />
  );
});
