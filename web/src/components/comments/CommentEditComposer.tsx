import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { CommentAttachment } from "@/commentWire.generated";
import { AtIcon, CheckCircleIcon, XCircleIcon } from "@/icons/leafen";
import type { TextSpan } from "@/lib/types";
import editStyles from "./CommentEditComposer.module.css";
import { CommentAttachButton, CommentPendingAttachments } from "./CommentAttachControls";
import { useCommentAttachmentDraft } from "./commentAttachmentDraft";
import { commentDraftEmpty } from "./commentModel";
import { CommentRichField, type CommentFieldHandle } from "./CommentRichField";

/** What an edit saves besides its words: the files kept and those added. */
export type CommentEditFiles = { kept: readonly string[]; added: readonly string[]; changed: boolean };

/**
 * One's own comment being edited: its formatting and mentions stay, as
 * Notion's (seen 2026-10-15), and Enter saves it as a new comment's box sends.
 */
export function CommentEditComposer({
  draft,
  workspaceId,
  pageId,
  held = [],
  onDraft,
  onCancel,
  onSave,
  onAttachError,
}: {
  draft: TextSpan[];
  workspaceId?: string;
  pageId?: string;
  /** The files the comment holds; it holds three at most. */
  held?: readonly CommentAttachment[];
  onDraft: (value: TextSpan[]) => void;
  onCancel: () => void;
  onSave: (files: CommentEditFiles) => void;
  onAttachError?: (message: string) => void;
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  const fieldRef = useRef<CommentFieldHandle>(null);
  // Its files show as the cards of a comment being written, each removable,
  // and more can be added, as Notion's edit (seen 2026-11-06); each goes up
  // as it is picked.
  const [kept, setKept] = useState(() => held.map((file) => file.id));
  const keptFiles = held.filter((file) => kept.includes(file.id));
  const attachments = useCommentAttachmentDraft({ workspaceId, pageId: pageId ?? "", held: kept.length, onError: onAttachError });
  const save = () => {
    if (attachments.uploading) return;
    onSave({ kept, added: attachments.fileIds, changed: attachments.fileIds.length > 0 || kept.length !== held.length });
  };
  const empty = commentDraftEmpty(draft) && keptFiles.length === 0 && attachments.items.length === 0;
  const withFiles = keptFiles.length > 0 || attachments.items.length > 0;
  // In its own line, as Notion's (seen 2026-11-06): the words with no box
  // around them, then 📎, @, a line, and a grey ✕ ("댓글 편집 취소") and a
  // blue ✓ ("댓글 업데이트") in filled circles — under its files when it
  // holds any.
  const actions = (
    <div className={editStyles.actions}>
      <CommentAttachButton draft={attachments} />
      <button type="button" className={editStyles.tool}
        aria-label={t("commentsPanel:mention.trigger")} title={t("commentsPanel:mention.trigger")}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => fieldRef.current?.mention()}><AtIcon size="regular" aria-hidden="true" /></button>
      <span className={editStyles.divider} aria-hidden="true" />
      <button type="button" className={`${editStyles.tool} ${editStyles.cancel}`} aria-label={t("commentsPanel:edit.cancel")}
        title={t("commentsPanel:edit.cancel")} onClick={onCancel}>
        <XCircleIcon size="large" aria-hidden="true" />
      </button>
      <button type="button" className={`${editStyles.tool} ${editStyles.save}`} aria-label={t("commentsPanel:edit.save")}
        title={t("commentsPanel:edit.save")} disabled={empty || attachments.uploading}
        onClick={() => { fieldRef.current?.publish(); save(); }}>
        <CheckCircleIcon size="large" weight="fill" aria-hidden="true" />
      </button>
    </div>
  );
  return (
    <div className={editStyles.wrap} data-comment-edit-composer>
      <div className={editStyles.edit}>
        <div className={editStyles.field}>
          <CommentRichField
            handleRef={fieldRef}
            spans={draft}
            pageId={pageId}
            autoFocus
            ariaLabel={t("commentsPanel:edit.aria")}
            onChange={onDraft}
            onSubmit={save}
            onEscape={() => { onCancel(); return true; }}
          />
        </div>
        {withFiles ? null : actions}
      </div>
      {withFiles ? (
        <>
          <CommentPendingAttachments draft={attachments} held={keptFiles}
            onRemoveHeld={(fileId) => setKept((current) => current.filter((id) => id !== fileId))} />
          <div className={editStyles.foot}>{actions}</div>
        </>
      ) : null}
    </div>
  );
}
