import { CreatedTime } from "@/components/CreatedTime";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { NativeComment } from "@/lib/commentService";
import type { ProductStatusSink } from "@/lib/productStatus";
import type { TextSpan } from "@/lib/types";
import { authorLabel, commentAuthorFace, editedCommentSpans, isReactionThread, richTextSpans } from "./commentModel";
import { CommentEditComposer, type CommentEditFiles } from "./CommentEditComposer";
import { CommentReactionChips } from "./CommentReactions";
import { CommentHoverActions, commentHoverHost, commentUnreadDot, suggestionSettleButtons } from "./CommentHoverActions";
import { replyMenuItems, threadMenuItems, useThreadMenuActions } from "./commentMenus";
import { discussionLine } from "./discussionListModel";
import { suggestionFacts } from "./suggestionModel";
import { SuggestionSummary, wrapSuggestionFace } from "./SuggestionSummary";
import type { useCommentsPanelController } from "./useCommentsPanelController";
import styles from "./MarginDiscussions.module.css";

type CommentsController = ReturnType<typeof useCommentsPanelController>;

/**
 * One comment of a resting margin card — its thread's first or its last
 * reply — with the comment's own buttons while it is hovered, as Notion's
 * resting card (seen 2026-11-16 at 1440 dark): [😊 ✓ ⋯] on the first,
 * [✓ ✕ 😊 ⋯] on an open suggestion's (seen 2026-11-25, two tabs) and
 * [😊 ⋯] on the reply, 4px over the comment's face and ending where its
 * words may, 12px inside the card. Its "…" and ✓ leave the card at rest; its
 * 😊 opens the card with the picker over it (onOpenReactionPicker,
 * restingCardReaction.tsx — Notion's, seen 2026-11-26 in two tabs; it rested
 * on 2026-11-16). Their "…" holds what an open card's does, and its edit
 * turns the comment's words into their field right there in the resting card.
 * Its reactions' chips stand 8px under its words, as an open card's, with
 * the smiley after them (Notion's, measured 2026-11-26 in Chrome at 1440 dpr
 * 2 in two tabs at different times: 25px pills from the words' start, 4px
 * apart, the person's own in blue): a chip pressed takes the person's emoji
 * back or adds it and opens the card as a press on it does; the smiley opens
 * the card and the picker as the 😊 does, the picker centred under where it
 * stood.
 */
export function MarginRestingEntry({ comment, thread, controller, userId, canComment, canEditPage, linkTarget, onOpenReactionPicker, onStatus }: {
  comment: NativeComment;
  /** The thread's first comment; `comment` itself unless it is the thread's last reply. */
  thread: NativeComment;
  controller: CommentsController;
  userId: string;
  canComment: boolean;
  canEditPage: boolean;
  linkTarget: string;
  /** Its 😊 pressed: the picker lies beside the cards, staying as the press opens the card (restingCardReaction.tsx). */
  onOpenReactionPicker: (button: HTMLElement) => void;
  onStatus?: ProductStatusSink;
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  const [editDraft, setEditDraft] = useState<TextSpan[] | null>(null);
  const actions = useThreadMenuActions(controller, thread, linkTarget, onStatus);
  const first = comment.id === thread.id;
  const suggestion = suggestionFacts(comment);
  const isOwn = comment.authorId === userId;
  const canManageOwn = canComment && isOwn && !(suggestion && suggestion.state === null);
  const spans = richTextSpans(comment.body);
  const edit = () => setEditDraft(spans);
  const remove = () => controller.remove(comment);
  const menu = first
    ? threadMenuItems(t, {
      comment,
      canEdit: canManageOwn && !suggestion && !isReactionThread(comment), canDelete: canManageOwn,
      onEdit: edit, onCopyLink: () => void actions.copyThreadLink(), onThreadState: (change) => void actions.changeThreadState(change), onDelete: remove,
    })
    : replyMenuItems(t, {
      canManage: canComment && isOwn, repliesMuted: !!thread.repliesMuted, onEdit: edit,
      onCopyThreadLink: () => void actions.copyThreadLink(), onMuteReplies: (muted) => void actions.changeThreadState({ repliesMuted: muted }), onDelete: remove,
    });
  const canResolve = first && canComment && (canEditPage || isOwn) && !suggestion;

  function save(files?: CommentEditFiles) {
    if (!editDraft) return;
    const next = editedCommentSpans(editDraft, spans, Boolean(files?.changed));
    if (next === null) return;
    if (next) controller.edit(comment, next, files?.changed ? files : undefined);
    setEditDraft(null);
  }

  const author = authorLabel(comment, userId, controller.actorProfiles);
  const keepMoreWidth = useMoreWidth();
  const more = `… ${t("commentsPanel:margin.more")}`;
  return (
    <div className={`${styles.entry} ${commentHoverHost}`} data-margin-rest-entry={comment.id}>
      {editDraft ? null : <CommentHoverActions canReact={canComment} onOpenReactionPicker={onOpenReactionPicker}
        resolve={canResolve ? { resolved: !!comment.resolved, onToggle: () => controller.toggleResolved(comment) } : undefined}
        // A suggestion's folded card leads with its accept and reject, as Notion's [✓ ✕ 😊 ⋯] (suggestionSettleButtons).
        settle={first ? suggestionSettleButtons(comment, { userId, canComment, canEditPage }, (decision) => controller.settle(comment, decision)) : undefined}
        menu={menu} />}
      <span className={styles.meta}>
        {wrapSuggestionFace(suggestion, <PersonAvatar className={styles.avatar} name={author}
          {...commentAuthorFace(comment, controller.actorProfiles)} size={24} />)}
        <span className={styles.author}>{author}</span>
        <CreatedTime className={styles.time} value={comment.createdAt} />
        {first && comment.unread ? <span className={commentUnreadDot} role="img" aria-label={t("commentsPanel:card.unread")} data-comment-unread /> : null}
      </span>
      {/* Under the name, beside the card's avatar; the words take their own direction, as a page's text does. */}
      <span className={styles.textIndent}>{editDraft ? (
        <CommentEditComposer draft={editDraft} workspaceId={comment.workspaceId} pageId={comment.pageId} held={comment.attachments}
          onAttachError={(message) => actions.say(message, "error")}
          onDraft={setEditDraft} onCancel={() => setEditDraft(null)} onSave={save} />
      ) : <>
        {suggestion ? <span className={styles.text} dir="auto" data-suggestion-summary=""><SuggestionSummary facts={suggestion} /></span>
          : <RestingWords words={discussionLine(comment) || t("commentsPanel:card.emptyComment")} more={more} />}
        {/* A suggestion's closed card ends its line with a faint "… 더 보기", opening it as the card does (Notion's, seen 2026-11-15 twice). */}
        {suggestion ? <span ref={keepMoreWidth} className={styles.suggestionMore} aria-hidden="true">{more}</span> : null}
      </>}</span>
      {editDraft ? null : <CommentReactionChips reactions={comment.reactions} userId={userId} workspaceId={comment.workspaceId} canReact={canComment}
        className={styles.restReactions} onToggle={(emoji, reacted) => controller.react(comment, emoji, reacted)} onOpenPicker={onOpenReactionPicker} />}
    </div>
  );
}

/**
 * A comment's words on a closed card, as Notion's (seen 2026-11-22 twice at
 * 1440 dpr 1): its line breaks kept, six lines at most in a box as wide as
 * its longest line; words that run past the sixth end it with a faint
 * "… 더 보기" at the box's end, the last line fading out under it
 * (MarginDiscussions.module.css), the card opening when pressed there as
 * anywhere on it.
 */
function RestingWords({ words, more }: { words: string; more: string }) {
  const [clamped, setClamped] = useState(false);
  const text = useRef<HTMLSpanElement | null>(null);
  const keepMoreWidth = useMoreWidth();
  // Whether the words run past the six lines shown: checked as the box's
  // size changes, and as the words change within the same size.
  const observer = useRef<ResizeObserver | null>(null);
  const measure = useCallback((box: HTMLSpanElement | null) => {
    observer.current?.disconnect();
    observer.current = null;
    text.current = box;
    if (!box) return;
    observer.current = new ResizeObserver(() => setClamped(box.scrollHeight > box.clientHeight + 1));
    observer.current.observe(box);
  }, []);
  useLayoutEffect(() => {
    const box = text.current;
    if (box) setClamped(box.scrollHeight > box.clientHeight + 1);
  }, [words]);
  return (
    <span className={styles.wordsBox} data-clamped={clamped || undefined}>
      <span ref={measure} className={styles.text} dir="auto">{words}</span>
      {clamped ? <span ref={keepMoreWidth} className={styles.commentMore} aria-hidden="true">{more}</span> : null}
    </span>
  );
}

/**
 * Keeps a closed card's "… 더 보기" width on its line as --more-width, the
 * length its last line fades over under it, as Notion measures its own
 * (MarginDiscussions.module.css): one observer per button, following the
 * language and the font as they change its width.
 */
function useMoreWidth() {
  const observer = useRef<ResizeObserver | null>(null);
  return useCallback((more: HTMLElement | null) => {
    observer.current?.disconnect();
    observer.current = null;
    const line = more?.parentElement;
    if (!more || !line) return;
    observer.current = new ResizeObserver(([entry]) => {
      const width = entry?.borderBoxSize?.[0]?.inlineSize ?? more.getBoundingClientRect().width;
      line.style.setProperty("--more-width", `${width}px`);
    });
    observer.current.observe(more);
  }, []);
}
