import { CreatedTime } from "@/components/CreatedTime";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { Fragment, useEffect, useRef, useState, type MouseEvent, type MutableRefObject, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import type { CommentActorProfile } from "@/commentWire.generated";
import type { NativeComment } from "@/lib/commentService";
import { firstNewComment } from "@/lib/commentThreadUnread";
import type { TextSpan } from "@/lib/types";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import styles from "../CommentsPanel.module.css";
import listedStyles from "./AllDiscussions.module.css";
import headStyles from "./ReactedHead.module.css";
import { CommentAttachmentList } from "./CommentAttachmentList";
import type { CommentAttachmentDraft } from "./commentAttachmentDraft";
import type { CommentDraftSource } from "./commentDraftStore";
import { CommentEditComposer, type CommentEditFiles } from "./CommentEditComposer";
import { CommentHoverActions, commentHoverHost, commentUnreadDot, suggestionSettleButtons } from "./CommentHoverActions";
import { CommentReactionChips, useReactionPicker } from "./CommentReactions";
import { CommentReplyComposer } from "./CommentReplyComposer";
import { threadMenuItems } from "./commentMenus";
import type { CommentFieldHandle } from "./CommentRichField";
import { ReplyCard } from "./ReplyCard";
import { CommentNewLine } from "./CommentNewLine";
import { discussionShownWhole } from "./discussionListModel";
import {
  authorLabel,
  commentAuthorFace,
  editedCommentSpans,
  isReactionThread,
  quoteText,
  renderCommentRichText,
  richTextSpans,
  spansToPlainTextSafe,
  timeLabel,
} from "./commentModel";
import { commentQuotePlacesOf } from "./commentQuotePlaces";
import { copyCommentText } from "./commentCopy";
import { suggestionFacts } from "./suggestionModel";
import { SuggestionCardSummary, wrapSuggestionFace } from "./SuggestionSummary";
import ownReplyStyles from "./ThreadReplyRow.module.css";
import { TitleThreadReplies } from "./TitleThreadReplies";
import { ResolvedThreadCard, ResolvedThreadMark } from "./ResolvedThread";
import { focusReplyBox } from "./replyRowCaret";
import { useTitleThreadAsks } from "./titleThreadReveal";

export function CommentCard({
  comment,
  activeBlockId,
  userId,
  actorProfiles,
  canComment,
  canEditPage,
  replies,
  replying,
  replyDraft,
  replyRef,
  submittingReply,
  active,
  activeCommentId,
  activeQuiet,
  beside,
  under,
  inWindow,
  quoteLabel,
  flat,
  listedQuote,
  selected = false,
  onRevealAnchor,
  alone = false,
  onOpenSide,
  onStartReply,
  onReplyDraft,
  onSubmitReply,
  onToggleResolved,
  onSettleSuggestion,
  onEditComment,
  onAttachError,
  onDeleteComment,
  onReact,
  onCopyLink,
  onThreadState,
  replyAttachments,
  onRemoveAttachment,
}: {
  comment: NativeComment;
  activeBlockId?: string | null;
  userId: string;
  actorProfiles: Readonly<Record<string, CommentActorProfile>>;
  canComment: boolean;
  canEditPage: boolean;
  replies: readonly NativeComment[];
  replying: boolean;
  /** The words typed in the thread's reply box, which the box alone reads. */
  replyDraft: CommentDraftSource;
  /** Where the panel finds the box of the reply being written. */
  replyRef?: MutableRefObject<CommentFieldHandle | null>;
  submittingReply: boolean;
  active?: boolean;
  /** Drawn beside its block, as the page's margin shows it, without saying where it is. */
  beside?: boolean;
  /**
   * Drawn in a card under its block, as Notion's narrow page shows it (seen
   * 2026-10-08): the text it is on quoted by a bar, nothing saying where it is.
   */
  under?: boolean;
  /** Drawn in a window under a database value: its reply box is a block's card's. */
  inWindow?: boolean;
  /**
   * In such a window, the value's property's icon and name: the value it is
   * on is quoted after them by a yellow bar, as Notion's (seen 2026-11-12).
   */
  quoteLabel?: ReactNode;
  /**
   * Drawn under a page's title, in a feed post's discussion or in "모든
   * 토론", as Notion's (seen 2026-10-08; "모든 토론" 2026-11-25 in two tabs):
   * its pictures joined by a line, its replies folded
   * behind their own row and, open, one step in (TitleThreadReplies). It is
   * answered from a box of its own that its "↩" opens at its end, the caret
   * in it, and that stays once opened; the field under the threads starts a
   * new one (Notion's, seen 2026-11-25 on a Galaxy A90's Chrome at 411 and in
   * Chrome at 1440, two tabs each; a feed post's in two tabs at 1440).
   */
  flat?: boolean;
  /**
   * Listed in "All discussions", as Notion's: where the thread is, quoted by
   * a yellow bar (its text or its block's text), and clicking the thread
   * shows that place in the page. Null quotes nothing.
   */
  listedQuote?: ReactNode;
  /** The listed thread last pressed or reopened in "모든 토론". */
  selected?: boolean;
  activeCommentId?: string;
  /** The active comment was opened at its address (a notice, a link): no ring on it, as Notion's. */
  activeQuiet?: boolean;
  /** Shows where the thread is; true when the press opens its reply row too (a listed block's thread). */
  onRevealAnchor: () => boolean | void;
  /**
   * In a view of its own (DiscussionSideView): resolved, a card to reopen it
   * stands where its reply row was, as Notion's.
   */
  alone?: boolean;
  /** Opens the thread under a page's title in a view of its own ("▯ 열기"), from the button pressed. */
  onOpenSide?: (button: HTMLElement) => void;
  onStartReply: () => void;
  onReplyDraft: (value: TextSpan[]) => void;
  onSubmitReply: () => void;
  /** Resolves or reopens the thread; true when a reopen opens its reply row too. */
  onToggleResolved: () => boolean | void;
  /** Accepts or rejects the thread's suggestion. */
  onSettleSuggestion: (decision: "accept" | "reject") => void;
  onEditComment: (comment: NativeComment, spans: readonly TextSpan[], files?: CommentEditFiles) => void;
  /** A file that could not be added while editing. */
  onAttachError?: (message: string) => void;
  onDeleteComment: (comment: NativeComment) => void;
  /** Adds or takes back the person's emoji on the comment or a reply. */
  onReact: (comment: NativeComment, emoji: string, reacted: boolean) => void;
  /** Copies the thread's address. */
  onCopyLink: () => void;
  /** Sets the person's own state of the thread: its replies turned off, or marked unread. */
  onThreadState: (change: { repliesMuted?: boolean; unread?: boolean }) => void;
  /** The files the reply being written holds. */
  replyAttachments?: CommentAttachmentDraft;
  /** Deletes one of a comment's files from it. */
  onRemoveAttachment: (comment: NativeComment, fileId: string) => void;
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  const spans = richTextSpans(comment.body);
  const text = spansToPlainTextSafe(spans);
  // Under a value's property's icon and name, the quote is the value's words alone.
  const windowQuote = inWindow && quoteLabel !== undefined;
  const quote = quoteText(comment.body, { words: windowQuote });
  const reactionPicker = useReactionPicker((emoji) => onReact(comment, emoji, true),
    { workspaceId: comment.workspaceId, pageId: comment.pageId });
  // In a card under its block every thread is on that block; none stands out.
  const isTarget = !under && !!activeBlockId && comment.blockId === activeBlockId;
  usePeopleDirectoryRevision();
  const author = authorLabel(comment, userId, actorProfiles);
  const face = commentAuthorFace(comment, actorProfiles);
  const time = timeLabel(comment.createdAt);
  const isOwn = comment.authorId === userId;
  const suggestion = suggestionFacts(comment);
  // A thread begun by a reaction to text has no comment to edit, as Notion's.
  const reactionThread = isReactionThread(comment);
  // Without words it is headed by who reacted, as Notion's "본인 반응함"
  // (seen 2026-11-09): no time, no line of words under its quote.
  const reactedHead = reactionThread && !text;
  const reactedByMe = comment.reactions.some((reaction) => reaction.userIds.includes(userId));
  // A suggestion is settled by accepting or rejecting it, and its thread
  // can go once it is settled.
  const openSuggestion = suggestion !== null && suggestion.state === null;
  const canResolve = canComment && (canEditPage || isOwn) && !suggestion;
  const canManageOwn = canComment && isOwn && !openSuggestion;
  const [editing, setEditing] = useState(false);
  const [editDraft, setEditDraft] = useState<TextSpan[]>([]);
  // Flat, its replies fold and open behind their own row
  // (TitleThreadReplies): folded as it is first shown with replies, open when
  // it had none — or when a reply is the comment an address or a notice
  // opened, so it shows.
  const activeReply = Boolean(activeCommentId) && replies.some((reply) => reply.id === activeCommentId);
  const [repliesOpen, setRepliesOpen] = useState(() => replies.length === 0 || activeReply);
  useEffect(() => { if (activeReply) setRepliesOpen(true); }, [activeReply]);
  // The new reply its row's press read, its "New" line kept while they stay open.
  const [readNewId, setReadNewId] = useState<string | undefined>(undefined);
  // Its own reply box under a page's title, open since its "↩" was first
  // pressed, or while its replies are open; each press opens its replies and
  // takes the caret there again. An open suggestion listed in "모든 토론"
  // has one too, as Notion's [↩ ✓ ✕ 😊 ⋯] (seen 2026-11-25 in Chrome at
  // 1440 and 1000, two tabs: its "↩", or the suggestion pressed, opened its
  // reply row and a letter typed went there).
  const ownReply = Boolean(flat && canComment && !comment.resolved && !reactionThread);
  const [replyOpen, setReplyOpen] = useState(false);
  // Its reply row opened by a press — its "↩", or the thread pressed or
  // reopened as its thread card asks, listed or in its own view — takes the
  // caret once drawn, what is typed from the press going in (replyRowCaret.ts).
  const threadRef = useRef<HTMLDivElement>(null), cardRef = useRef<HTMLElement>(null);
  const openReply = () => {
    if (!canComment || reactionThread || !(flat || alone)) return;
    if (flat) { setRepliesOpen(true); setReplyOpen(true); }
    focusReplyBox(() => flat ? threadRef.current : cardRef.current);
    onStartReply();
  };
  const openOwnReply = ownReply ? openReply : undefined;
  const revealAnchor = () => { if (onRevealAnchor() && !comment.resolved) openReply(); };
  const toggleResolved = () => { if (onToggleResolved()) openReply(); };
  const listed = listedQuote !== undefined;
  // Under a page's title, its thread pressed in "모든 토론" opens its replies
  // and its reply row, the caret left where it was (titleThreadReveal).
  const revealAsks = useTitleThreadAsks(comment.id, Boolean(flat && !listed));
  const revealSeen = useRef(revealAsks);
  useEffect(() => {
    if (revealAsks === revealSeen.current) return;
    revealSeen.current = revealAsks;
    setRepliesOpen(true); setReplyOpen(true);
  }, [revealAsks]);

  // A listed thread shows its place when clicked anywhere but on its controls.
  function revealFromCard(event: MouseEvent<HTMLElement>) {
    const target = event.target instanceof Element ? event.target : null;
    if (!listed || target?.closest("button, a, input, textarea, [role='menu'], [role='menuitem'], [contenteditable='true']")) return;
    revealAnchor();
  }

  function saveEdit(files?: CommentEditFiles) {
    const next = editedCommentSpans(editDraft, spans, Boolean(files?.changed));
    if (next === null) return;
    if (next) onEditComment(comment, next, files?.changed ? files : undefined);
    setEditing(false);
  }
  const menu = threadMenuItems(t, {
    comment,
    canEdit: canManageOwn && !editing && !suggestion && !reactionThread, canDelete: canManageOwn,
    onEdit: () => { setEditDraft(spans); setEditing(true); }, onCopyLink, onThreadState,
    onDelete: () => onDeleteComment(comment),
  });

  // The "New" line goes before the thread's first comment new to the reader,
  // in Notion's shapes (CommentNewLine, seen 2026-11-25 in Chrome at 1440,
  // two tabs at different times): in a card — beside its block, in a block's
  // window — the card's line before that reply; drawn flat — under a page's
  // title, in "모든 토론", in a feed post's discussion — the reply's line
  // while the replies are open and none while they are folded, the thread's
  // dot saying so. A thread new as a whole has none but under a page's title
  // — where its "▯ 열기" is offered — the thread's line over it there when it
  // has replies.
  const firstNewId = firstNewComment([comment, ...replies])?.id ?? readNewId;
  const underTitle = Boolean(flat && !listed && onOpenSide);
  const newWhole = underTitle && replies.length > 0 && (Boolean(comment.new) || readNewId === comment.id);
  // Its folded replies opened by their row read the thread at once, as
  // Notion's (seen 2026-11-25 in Chrome at 1440, two tabs at different times:
  // an unread thread under a page's title kept its dot while folded and seen
  // there, and pressing "답글 보기" took the dot off at once, the "New" line
  // staying before the first new reply while they stayed open). One the
  // person marked unread stays so.
  const toggleReplies = () => {
    if (repliesOpen) { setReadNewId(undefined); setRepliesOpen(false); return; }
    setRepliesOpen(true);
    if (!comment.unread || comment.markedUnread) return;
    setReadNewId(firstNewId ?? (underTitle && comment.new ? comment.id : undefined));
    onThreadState({ unread: false });
  };
  const replyCard = (reply: NativeComment) => (<Fragment key={reply.id}>
    {reply.id === firstNewId ? <CommentNewLine shape={flat ? "reply" : "card"} /> : null}
    <ReplyCard
      comment={reply}
      userId={userId}
      actorProfiles={actorProfiles}
      canComment={canComment}
      active={reply.id === activeCommentId}
      quiet={activeQuiet}
      flat={flat}
      onEditComment={onEditComment}
      onAttachError={onAttachError}
      onDeleteComment={onDeleteComment}
      onReact={onReact}
      onCopyThreadLink={onCopyLink}
      repliesMuted={!!comment.repliesMuted}
      onMuteReplies={(muted) => onThreadState({ repliesMuted: muted })}
      onRemoveAttachment={onRemoveAttachment}
    />
  </Fragment>);

  const card = (
    <article ref={cardRef}
      className={`${styles.comment} ${commentHoverHost}`}
      role="listitem"
      aria-label={comment.resolved
        ? t("commentsPanel:card.commentAriaResolved", { author: author, time })
        : t("commentsPanel:card.commentAria", { author: author, time })}
      tabIndex={active ? -1 : undefined}
      data-comment-id={comment.id}
      data-active={active && !activeQuiet ? "true" : undefined}
      data-resolved={comment.resolved ? "true" : undefined}
      data-target={isTarget ? "true" : undefined}
      data-beside={beside ? "true" : undefined}
    >
      {/* None while it is being edited, as Notion's (seen 2026-11-06). */}
      {editing ? null : <CommentHoverActions canReact={canComment} onOpenReactionPicker={reactionPicker.open} reply={openOwnReply}
        open={flat && !reactionThread && !suggestion ? onOpenSide : undefined}
        resolve={canResolve ? { resolved: !!comment.resolved, onToggle: toggleResolved } : undefined}
        settle={suggestionSettleButtons(comment, { userId, canComment, canEditPage }, onSettleSuggestion)} menu={menu} />}
      {wrapSuggestionFace(suggestion, <PersonAvatar className={styles.avatar} name={author} {...face} size={24} />)}
      <div className={styles.commentBody}>
        <div className={styles.commentMeta}>
          {reactedHead ? (
            <div className={headStyles.reacted}>
              {reactedByMe
                ? <Trans t={t} i18nKey="commentsPanel:card.reactedSelf" components={{ name: <b /> }} />
                : <><b data-person-card={face.userId || undefined}>{author}</b> {t("commentsPanel:card.reacted")}</>}
            </div>
          ) : <><span data-person-card={face.userId || undefined}>{author}</span><CreatedTime value={comment.createdAt} /></>}
          {comment.resolved && !comment.parentId ? <ResolvedThreadMark /> : null}
          {comment.editedAt ? <span>{t("commentsPanel:card.edited")}</span> : null}
          {/* Under a page's title one shown whole there has none (discussionShownWhole). */}
          {comment.unread && !(underTitle && discussionShownWhole(comment, replies)) ? (
            <span className={commentUnreadDot} role="img" aria-label={t("commentsPanel:card.unread")} data-comment-unread />
          ) : null}
        </div>
        {listed || under || windowQuote ? (
          (listed ? listedQuote : quote) ? (
            <div className={listedStyles.quote} data-discussion-quote>
              <span className={listedStyles.quoteBar} aria-hidden="true" />
              {/* Words chosen across blocks quote each block on its own line, as Notion's (seen 2026-11-09). */}
              <div className={listedStyles.quoteText} data-quote-lines={commentQuotePlacesOf(comment.body).quoteBlocks ? "" : undefined}>
                {listed ? listedQuote : <>{windowQuote ? quoteLabel : null}{quote}</>}
              </div>
            </div>
          ) : null
        ) : comment.blockId && !reactionThread && !beside && (
          <div className={styles.anchorLabel}>
            {quote
              ? t("commentsPanel:anchor.onSelectedText")
              : isTarget
                ? t("commentsPanel:anchor.onSelectedBlock")
                : t("commentsPanel:anchor.onABlock")}
          </div>
        )}
        {quote && !beside && !listed && !under && !windowQuote && <div className={styles.commentQuote}>{quote}</div>}
        {suggestion ? (
          <SuggestionCardSummary facts={suggestion} listed={listed}>
            {suggestion.state ? <span> · {t(`commentsPanel:suggestion.${suggestion.state}`)}</span> : null}
          </SuggestionCardSummary>
        ) : editing ? (
          <CommentEditComposer draft={editDraft} workspaceId={comment.workspaceId} pageId={comment.pageId}
            held={comment.attachments} onAttachError={onAttachError}
            onDraft={setEditDraft} onCancel={() => setEditing(false)} onSave={saveEdit} />
        ) : reactedHead ? null : (
          // A comment's words set its direction, as a page's text does (lib/textDirection).
          <div className={styles.commentText} dir="auto" onCopy={copyCommentText}>
            {text ? renderCommentRichText(spans) : comment.attachments?.length ? null : t("commentsPanel:card.emptyComment")}
          </div>
        )}
        {!editing && (
          <CommentAttachmentList attachments={comment.attachments} canRemove={canManageOwn}
            editor={{ id: face.userId ?? "", name: author, at: comment.editedAt || comment.createdAt, bot: face.bot }}
            onRemove={(fileId) => onRemoveAttachment(comment, fileId)} />
        )}
        {!editing && (
          <CommentReactionChips reactions={comment.reactions} userId={userId} workspaceId={comment.workspaceId} canReact={canComment}
            onToggle={(emoji, reacted) => onReact(comment, emoji, reacted)} onOpenPicker={reactionPicker.open} />
        )}
        {/* An open suggestion's accept and reject are its buttons' (CommentHoverActions), no row of their own, as Notion's (seen 2026-11-22). */}
        {comment.blockId && !beside && !listed && !under && (
          <div className={styles.commentActions}>
            <button type="button" onClick={revealAnchor}>{t("commentsPanel:actions.showInPage")}</button>
          </div>
        )}
      </div>
      {/* Replies under the thread's comment, their faces in its column, as Notion's (seen 2026-10-28). */}
      {replies.length > 0 && !flat && (
        <div className={styles.replies} role="list" aria-label={t("commentsPanel:card.repliesAria", { count: replies.length })} data-comment-replies>
          {replies.map(replyCard)}
        </div>
      )}
      {/* The reply row, as Notion's (seen 2026-10-28): the reader's face in the
          faces' column, the box's words in line with the thread's. */}
      {canComment && !comment.resolved && !reactionThread && !flat && (
        <>
          <PersonAvatar className={`${styles.replyAvatar} ${styles.replyRowAvatar}`} userId={userId} avatar={actorProfiles[userId]?.avatar}
            name={authorLabel({ ...comment, authorId: userId }, userId, actorProfiles)} size={24} />
          <CommentReplyComposer active={replying} activeRef={replyRef} attachments={replyAttachments} draft={replyDraft}
            inWindow={under || inWindow} toolsShown={beside} pageId={comment.pageId}
            submitting={submittingReply} onStart={onStartReply} onDraft={onReplyDraft} onSubmit={onSubmitReply} />
        </>
      )}
      {alone && canComment && comment.resolved && !reactionThread
        ? <ResolvedThreadCard onReopen={canResolve ? toggleResolved : undefined} /> : null}
      {reactionPicker.picker}
    </article>
  );
  const lineFirst = newWhole ? <CommentNewLine shape="thread" /> : null;
  if (!flat) return <>{lineFirst}{card}</>;
  // The reader's face by the box, in the replies' column.
  const ownReplyRow = ownReply && (replyOpen || replying || replies.length > 0) ? (
    <div className={ownReplyStyles.row} data-thread-reply-row>
      <PersonAvatar className={`${styles.avatar} ${styles.replyRowAvatar}`} userId={userId} avatar={actorProfiles[userId]?.avatar}
        name={authorLabel({ ...comment, authorId: userId }, userId, actorProfiles)} size={24} />
      <CommentReplyComposer active={replying} activeRef={replyRef} attachments={replyAttachments} draft={replyDraft}
        underTitle={!listed} pageId={comment.pageId} submitting={submittingReply}
        onStart={onStartReply} onDraft={onReplyDraft} onSubmit={onSubmitReply} />
    </div>
  ) : null;
  // Listed, the whole thread is chosen and pressed (AllDiscussions).
  return <div ref={threadRef} className={`${styles.flatThread} ${listed ? listedStyles.listedThread : ""}`} role="none"
    data-comment-thread={comment.id} data-listed={listed ? "true" : undefined} data-selected={selected ? "true" : undefined}
    onClick={listed ? revealFromCard : undefined}>{lineFirst}{card}
    <TitleThreadReplies count={replies.length} open={repliesOpen} onToggle={toggleReplies}
      replies={replies.map((reply) => ({ id: reply.id, node: replyCard(reply) }))} replyRow={ownReplyRow} /></div>;
}
