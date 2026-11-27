import { useRef } from "react";
import { useTranslation } from "react-i18next";
import type { CommentAttachment } from "@/commentWire.generated";
import { FileText, PaperclipIcon, X } from "@/icons/leafen";
import { HoverTip } from "../IconTipButton";
import { formatFileSize } from "../database/files";
import { waitingNoteText } from "@/lib/pendingChoice";
import type { CommentAttachmentDraft } from "./commentAttachmentDraft";
import styles from "./CommentAttachments.module.css";
import toolStyles from "./CommentTools.module.css";

/** A comment field's "파일 첨부", as Notion's, beside its mention button. */
export function CommentAttachButton({ draft }: { draft: CommentAttachmentDraft }) {
  const { t } = useTranslation("commentsPanel");
  const input = useRef<HTMLInputElement>(null);
  if (!draft.enabled) return null;
  return (
    <>
      <button type="button" className={toolStyles.tool} aria-label={t("attachments.attach")} title={t("attachments.attach")}
        data-comment-attach disabled={draft.full} onMouseDown={(event) => event.preventDefault()}
        onClick={() => input.current?.click()}>
        <PaperclipIcon size="regular" aria-hidden="true" />
      </button>
      <input ref={input} type="file" multiple hidden data-comment-attach-input
        onChange={(event) => {
          draft.add([...(event.currentTarget.files ?? [])]);
          event.currentTarget.value = "";
        }} />
    </>
  );
}

/**
 * The files a comment being written or edited will hold, as Notion's (seen
 * 2026-11-06): a card each with its name and size, a round "×" ("파일 제거")
 * on its corner while the pointer is on it, its whole name in a tip above.
 * One being edited shows the files it holds first, removable the same way.
 */
export function CommentPendingAttachments({ draft, held = [], onRemoveHeld }: {
  draft: CommentAttachmentDraft;
  held?: readonly CommentAttachment[];
  onRemoveHeld?: (fileId: string) => void;
}) {
  const files = [
    ...held.map((file) => ({ key: file.id, name: file.name, size: file.size, uploading: false, waiting: false, remove: () => onRemoveHeld?.(file.id) })),
    ...draft.items.map((item) => ({ key: item.key, name: item.name, size: item.size, uploading: !item.fileId, waiting: Boolean(item.waiting), remove: () => draft.remove(item.key) })),
  ];
  if (files.length === 0) return null;
  return (
    <div className={styles.pending} data-comment-pending-attachments>
      {files.map(({ key, ...file }) => <PendingFileCard key={key} {...file} />)}
    </div>
  );
}

function PendingFileCard({ name, size, uploading, waiting, remove }: { name: string; size: number; uploading: boolean; waiting: boolean; remove: () => void }) {
  const { t } = useTranslation("commentsPanel");
  return (
    <span className={styles.pendingCard} data-uploading={uploading || undefined}
      aria-label={uploading ? t("attachments.uploading", { name }) : name}>
      <span className={styles.chipIconBox}><FileText size="regular" aria-hidden="true" /></span>
      <span className={styles.chipText}>
        <HoverTip label={name} placement="top" className={styles.chipName}>{name}</HoverTip>
        <span className={styles.chipSize} {...(waiting ? { role: "status" } : {})}>{waiting ? waitingNoteText() : formatFileSize(size)}</span>
      </span>
      <button type="button" className={styles.pendingRemove} aria-label={t("attachments.remove")}
        onMouseDown={(event) => event.preventDefault()} onClick={remove}>
        <X size="tiny" aria-hidden="true" />
      </button>
    </span>
  );
}
