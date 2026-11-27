import { CreatedTime } from "@/components/CreatedTime";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { CommentActorProfile } from "@/commentWire.generated";
import type { NativeComment } from "@/lib/commentService";
import type { TextSpan } from "@/lib/types";
import styles from "../CommentsPanel.module.css";
import { CommentAttachmentList } from "./CommentAttachmentList";
import { copyCommentText } from "./commentCopy";
import { CommentEditComposer, type CommentEditFiles } from "./CommentEditComposer";
import { CommentHoverActions, commentHoverHost } from "./CommentHoverActions";
import { replyMenuItems } from "./commentMenus";
import { CommentReactionChips, useReactionPicker } from "./CommentReactions";
import {
  authorLabel,
  commentAuthorFace,
  editedCommentSpans,
  renderCommentRichText,
  richTextSpans,
  spansToPlainTextSafe,
  timeLabel,
} from "./commentModel";

export function ReplyCard({
  comment,
  userId,
  actorProfiles,
  canComment,
  active,
  quiet = false,
  flat = false,
  onEditComment,
  onAttachError,
  onDeleteComment,
  onReact,
  onCopyThreadLink,
  repliesMuted,
  onMuteReplies,
  onRemoveAttachment,
}: {
  comment: NativeComment;
  userId: string;
  actorProfiles: Readonly<Record<string, CommentActorProfile>>;
  canComment: boolean;
  active: boolean;
  /** Opened at its address: no ring on it. */
  quiet?: boolean;
  /** In its thread's column under a page's title, as its first comment. */
  flat?: boolean;
  onEditComment: (comment: NativeComment, spans: readonly TextSpan[], files?: CommentEditFiles) => void;
  /** A file that could not be added while editing. */
  onAttachError?: (message: string) => void;
  onDeleteComment: (comment: NativeComment) => void;
  onReact: (comment: NativeComment, emoji: string, reacted: boolean) => void;
  /** Copies the address of the thread the reply is in. */
  onCopyThreadLink: () => void;
  /** The person turned off the thread's replies' notifications. */
  repliesMuted: boolean;
  onMuteReplies: (muted: boolean) => void;
  /** Deletes one of the reply's files from it. */
  onRemoveAttachment: (comment: NativeComment, fileId: string) => void;
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  const spans = richTextSpans(comment.body);
  const text = spansToPlainTextSafe(spans);
  const author = authorLabel(comment, userId, actorProfiles);
  const face = commentAuthorFace(comment, actorProfiles);
  const time = timeLabel(comment.createdAt);
  const canManageOwn = canComment && comment.authorId === userId;
  const [editing, setEditing] = useState(false);
  const [editDraft, setEditDraft] = useState<TextSpan[]>([]);
  const reactionPicker = useReactionPicker((emoji) => onReact(comment, emoji, true),
    { workspaceId: comment.workspaceId, pageId: comment.pageId });

  function saveEdit(files?: CommentEditFiles) {
    const next = editedCommentSpans(editDraft, spans, Boolean(files?.changed));
    if (next === null) return;
    if (next) onEditComment(comment, next, files?.changed ? files : undefined);
    setEditing(false);
  }
  // A reply shows add reaction and "…" on hover (commentMenus.ts).
  const menu = editing ? [] : replyMenuItems(t, {
    canManage: canManageOwn, repliesMuted, onEdit: () => { setEditDraft(spans); setEditing(true); },
    onCopyThreadLink, onMuteReplies, onDelete: () => onDeleteComment(comment),
  });

  return (
    <article
      className={`${flat ? styles.comment : styles.reply} ${commentHoverHost}`}
      role="listitem"
      aria-label={t("commentsPanel:card.replyAria", { author: author, time })}
      tabIndex={active ? -1 : undefined}
      data-comment-id={comment.id}
      data-active={active && !quiet ? "true" : undefined}
    >
      {editing ? null : <CommentHoverActions canReact={canComment} onOpenReactionPicker={reactionPicker.open} menu={menu} />}
      <PersonAvatar className={flat ? styles.avatar : styles.replyAvatar} name={author} {...face} size={24} />
      <div className={flat ? styles.commentBody : styles.replyBody}>
        <div className={styles.commentMeta}>
          <span data-person-card={face.userId || undefined}>{author}</span>
          <CreatedTime value={comment.createdAt} />
          {comment.editedAt ? <span>{t("commentsPanel:card.edited")}</span> : null}
        </div>
        {editing ? (
          <CommentEditComposer
            draft={editDraft}
            workspaceId={comment.workspaceId}
            pageId={comment.pageId}
            held={comment.attachments}
            onAttachError={onAttachError}
            onDraft={setEditDraft}
            onCancel={() => setEditing(false)}
            onSave={saveEdit}
          />
        ) : (
          <div className={styles.commentText} dir="auto" onCopy={copyCommentText}>
            {text ? renderCommentRichText(spans) : comment.attachments?.length ? null : t("commentsPanel:card.emptyReply")}
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
      </div>
      {reactionPicker.picker}
    </article>
  );
}
