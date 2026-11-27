import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { AtIcon, SendCircleIcon } from "@/icons/leafen";
import type { TextSpan } from "@/lib/types";
import styles from "../CommentsPanel.module.css";
import { CommentAttachButton, CommentPendingAttachments } from "./CommentAttachControls";
import type { CommentAttachmentDraft } from "./commentAttachmentDraft";
import { useCommentDraft, type CommentDraftSource } from "./commentDraftStore";
import { commentDraftEmpty } from "./commentModel";
import { commentFieldToolsShown } from "./commentFieldTools";
import { CommentRichField, type CommentFieldHandle } from "./CommentRichField";
import toolStyles from "./CommentTools.module.css";

export function CommentComposer({
  ariaLabel,
  attachments,
  avatarSize = 22,
  bare = false,
  blockId,
  currentUserLabel,
  currentUserId,
  draft: draftSource,
  fieldRef,
  inline,
  pageId,
  submitting,
  onSubmit,
  escapeLeaves,
  onEscapeDraft,
  onEscapeEmpty,
  onUpdate,
}: {
  /** Names the field where its place is something else than a page or a block, as a feed post's. */
  ariaLabel?: string;
  /** The files the comment will hold; without them it can't hold any. */
  attachments?: CommentAttachmentDraft;
  /** The writer's face beside an inline composer: 22px, 24px in a feed post (Notion's, seen 2026-10-20). */
  avatarSize?: number;
  /** Without the writer's face, the field alone. */
  bare?: boolean;
  blockId?: string | null;
  currentUserLabel: string;
  currentUserId?: string;
  /** The words typed in the field: a letter redraws the composer alone, not the threads beside it. */
  draft: CommentDraftSource;
  /** Escape, the @ list closed, in an empty field leaves it and the composer
   * stays, as a page's "댓글 추가" under its title (Notion's, seen 2026-11-09:
   * the focus went to the page and the row stayed); without it, an empty
   * field's Escape closes what it sits in. */
  escapeLeaves?: boolean;
  /** Escape in a field holding a draft puts the field away and keeps the
   * draft for later, as a property value's words' field (Notion's, seen
   * 2026-11-12); without it the field stays with its draft. */
  onEscapeDraft?: () => void;
  /** Escape, the @ list closed, in an empty field closes what it sits in
   * this way, whether its card stands open or rests with the caret left in
   * it after a draft was put away and then emptied (a margin's new comment,
   * Notion's, seen by the strike 2026-11-15). */
  onEscapeEmpty?: () => void;
  fieldRef: RefObject<CommentFieldHandle | null>;
  inline: boolean;
  pageId?: string;
  submitting: boolean;
  onSubmit: () => void;
  onUpdate: (value: TextSpan[]) => void;
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  // Its buttons show while the focus is in it — the field or its own
  // buttons — or it holds a draft, by the one rule of commentFieldToolsShown.
  const [focused, setFocused] = useState(false);
  const draft = useCommentDraft(draftSource);
  const idle = !commentFieldToolsShown({ focused, draftEmpty: commentDraftEmpty(draft), files: attachments?.fileIds.length ?? 0 });
  // Send is the inline composer's round tool or the panel's text button, one look or the other.
  const sendLook = inline ? `${toolStyles.tool} ${toolStyles.send}` : styles.submitButton;
  return (
    <div className={styles.composer} data-comments-composer data-idle={idle ? "true" : undefined}
      onFocus={() => setFocused(true)}
      // Focus going to a menu of its own (React carries a portal's focus back
      // here) or out of the window and back comes in again through onFocus.
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(false); }}>
      {inline && !bare && <PersonAvatar className={styles.inlineComposerAvatar} userId={currentUserId} name={currentUserLabel} size={avatarSize} />}
      <div className={styles.composerBox}>
        <CommentRichField
          handleRef={fieldRef}
          spans={draft}
          pageId={pageId}
          ariaLabel={ariaLabel ?? (blockId ? t("commentsPanel:composer.addBlockComment") : t("commentsPanel:composer.addPageComment"))}
          placeholder={blockId ? t("commentsPanel:composer.blockPlaceholder")
            : inline ? t("commentsPanel:composer.pageInlinePlaceholder") : t("commentsPanel:composer.pagePlaceholder")}
          onChange={onUpdate}
          onSubmit={onSubmit}
          onEscape={() => {
            // A field holding a draft keeps it, its caret and its composer,
            // as Notion's block and page fields (seen 2026-11-11).
            if (!commentDraftEmpty(draft) || attachments?.fileIds.length) {
              onEscapeDraft?.();
              return true;
            }
            if (onEscapeEmpty) {
              onEscapeEmpty();
              return true;
            }
            if (!escapeLeaves) return false;
            (document.activeElement as HTMLElement | null)?.blur();
            return true;
          }}
        />
        {attachments ? <CommentPendingAttachments draft={attachments} /> : null}
        <div className={styles.composerFoot} data-comment-composer-foot>
          <span>{blockId ? t("commentsPanel:subtitle.block") : t("commentsPanel:subtitle.page")}</span>
          <div className={styles.composerActions}>
            {attachments ? <CommentAttachButton draft={attachments} /> : null}
            <button type="button" className={toolStyles.tool} data-comment-mention
              aria-label={t("commentsPanel:mention.trigger")} title={t("commentsPanel:mention.trigger")}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => fieldRef.current?.mention()}><AtIcon size="regular" aria-hidden="true" /></button>
            <button type="button" className={sendLook} data-comment-send
              aria-label={inline ? t("commentsPanel:composer.submit") : undefined}
              disabled={(commentDraftEmpty(draft) && !attachments?.fileIds.length) || attachments?.uploading || submitting}
              onClick={() => { fieldRef.current?.publish(); onSubmit(); }}>
              {inline ? <SendCircleIcon /> : t("commentsPanel:composer.submit")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
