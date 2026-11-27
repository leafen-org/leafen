import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { CommentAttachment } from "@/commentWire.generated";
import { DotsHorizontal, DownloadCircleIcon, FileText, OpenInNew, Trash } from "@/icons/leafen";
import { filePreviewWidth, useWorkspaceFile, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { useImageLoadFailure } from "@/lib/imageLoadFailure";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import { placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { RowMenuEditedFoot } from "../RowMenuEditedFoot";
import { formatFileSize } from "../database/files";
import { ImageLoadFailure } from "../editor/ImageLoadFailure";
import { HoverTip } from "../IconTipButton";
import styles from "./CommentAttachments.module.css";
import { triggerBrowserLinkDownload } from "@/lib/browserFileDownload";
import { inlineStartX } from "@/lib/readingDirection";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

function openFile(url: string) {
  if (url) window.open(url, "_blank", "noopener,noreferrer");
}

function downloadFile(url: string, name: string) {
  if (url) void triggerBrowserLinkDownload(url, name);
}

/**
 * A comment's files after its text, as Notion's (seen 2026-09-30): a picture
 * shows itself and opens whole when pressed; another file is a chip with its
 * name and size that downloads when pressed, and whose "…" offers deleting
 * it from the comment (the comment's writer), viewing the original and
 * downloading.
 */
export function CommentAttachmentList({ attachments, canRemove, editor, onRemove }: {
  attachments?: readonly CommentAttachment[];
  canRemove: boolean;
  editor?: CommentFileEditor;
  onRemove: (fileId: string) => void;
}) {
  if (!attachments?.length) return null;
  return (
    <div className={styles.list} data-comment-attachments>
      {attachments.map((file) => file.contentType.startsWith("image/")
        ? <CommentImage key={file.id} file={file} />
        : <CommentFileChip key={file.id} file={file} canRemove={canRemove} editor={editor} onRemove={() => onRemove(file.id)} />)}
    </div>
  );
}

// A picture that couldn't be loaded shows the page's failure state at the
// picture's size, as an image in Notion's pages and feed does (seen
// 2026-10-21); Notion's own comment files don't go missing.
function CommentImage({ file }: { file: CommentAttachment }) {
  const shown = useWorkspaceFile(nativeWorkspaceFileLocator(file.id), ["data:image/"], { width: filePreviewWidth(320) });
  const url = shown.url;
  const failure = useImageLoadFailure(url, shown.failedStatus);
  if (failure.failed) {
    return (
      <div className={styles.imageFailed} title={file.name} data-comment-attachment={file.id}>
        <ImageLoadFailure url={url} refused={shown.failedStatus} />
      </div>
    );
  }
  if (!url) return <span className={styles.imagePlaceholder} data-comment-attachment={file.id} aria-hidden="true" />;
  return (
    <img className={styles.image} src={url} alt={file.name} title={file.name} data-comment-attachment={file.id}
      onClick={() => openFile(url)} onError={failure.fail} />
  );
}

/** Who put a comment's files there and when: its writer, as Notion's file menu foot. */
/** Who wrote a comment's file in; `bot` for a connection, named alone on its face, as an integration's edit is. */
export type CommentFileEditor = { id: string; name: string; at?: string | null; bot?: boolean };

function CommentFileChip({ file, canRemove, editor, onRemove }: {
  file: CommentAttachment;
  canRemove: boolean;
  editor?: CommentFileEditor;
  onRemove: () => void;
}) {
  const { t } = useTranslation("commentsPanel");
  const url = useWorkspaceFileUrl(nativeWorkspaceFileLocator(file.id));
  // Opened where it was pressed, as Notion's file menu (seen 2026-11-06);
  // from the keyboard, under its "…".
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);
  const menuOpen = menuAt !== null;
  const host = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const close = () => setMenuAt(null);
  const place = usePopoverPlacement(menuRef, menuAt ? (size, viewport) => placeAtPoint(menuAt, size, viewport) : null,
    [menuAt], undefined, { kind: "menu", onEscape: close, onOutsidePress: close, inside: () => [host.current] });
  // As Notion's: "파일", then 삭제 in the menu's own color with "Del", 원본
  // 보기 and 다운로드 with their marks, and under a line who put it there
  // and when.
  const actions = [
    ...(canRemove ? [{ id: "delete", label: t("attachments.delete"), icon: <Trash size="regular" aria-hidden="true" />, hint: "Del", choose: onRemove }] : []),
    { id: "view", label: t("attachments.viewOriginal"), icon: <OpenInNew size="regular" aria-hidden="true" />, hint: "", choose: () => openFile(url) },
    { id: "download", label: t("attachments.downloadItem"), icon: <DownloadCircleIcon size="regular" aria-hidden="true" />, hint: "", choose: () => downloadFile(url, file.name) },
  ];
  const picker = useNativeMenu({ items: actions, open: menuOpen, menuRef, label: t("attachments.file"), quietOnPointer: true,
    keyOf: item => item.id, textOf: item => item.label, onAction: item => { close(); item.choose(); } });

  return (
    <div ref={host} className={styles.chip} role="button" tabIndex={0} data-comment-attachment={file.id}
      aria-label={t("attachments.download", { name: file.name })}
      onClick={() => downloadFile(url, file.name)}
      onKeyDown={(event) => {
        if (event.target !== event.currentTarget || (!fieldEnter(event) && event.key !== " ")) return;
        event.preventDefault();
        downloadFile(url, file.name);
      }}>
      <span className={styles.chipIconBox}><FileText size="regular" aria-hidden="true" /></span>
      <span className={styles.chipText}>
        <HoverTip label={file.name} placement="top" className={styles.chipName}>{file.name}</HoverTip>
        <span className={styles.chipSize}>{formatFileSize(file.size)}</span>
      </span>
      <button type="button" className={styles.more} aria-haspopup="menu" aria-expanded={menuOpen}
        aria-label={t("attachments.more", { name: file.name })}
        onClick={(event) => {
          event.stopPropagation();
          if (menuOpen) { close(); return; }
          const box = event.currentTarget.getBoundingClientRect();
          setMenuAt(event.detail === 0 ? { x: inlineStartX(box, 0, event.currentTarget), y: box.bottom + 4 } : { x: event.clientX, y: event.clientY + 1 });
        }}>
        <DotsHorizontal size="regular" aria-hidden="true" />
      </button>
      {menuOpen && typeof document !== "undefined" ? createPortal(
        <>
          <button type="button" className={styles.menuBackdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
            aria-label={t("actions.closeMenu")} onClick={close} />
          <div {...picker.menuProps} ref={menuRef} className={styles.menu} style={popoverPlacementStyle(place)}
            onKeyDown={(event) => {
              // "Del" deletes it from the comment, as the menu says.
              if (canRemove && (event.key === "Delete" || event.key === "Backspace")) {
                event.preventDefault(); close(); onRemove(); return;
              }
              picker.menuProps.onKeyDown?.(event);
            }}>
            <div className={styles.menuLabel}>{t("attachments.file")}</div>
            {actions.map(item => <NativeMenuItem key={item.id} state={picker.state} itemKey={item.id} className={styles.menuItem}>
              {({ labelProps }) => <>{item.icon}<span {...labelProps}>{item.label}</span>{item.hint ? <span className={styles.menuHint}>{item.hint}</span> : null}</>}
            </NativeMenuItem>)}
            {editor ? <RowMenuEditedFoot name={editor.name} editorId={editor.id} at={editor.at} marker="comment-file-foot"
              via={editor.bot ? { name: editor.name, integration: true } : undefined} /> : null}
          </div>
        </>,
        document.body,
      ) : null}
    </div>
  );
}
