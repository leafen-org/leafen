import { ImageBrokenIcon, type LeafenIconSize } from "@/icons/leafen";
import { type MutableRefObject, type ReactNode, type CSSProperties, useState } from "react";
import { useTranslation } from "react-i18next";
import { i18next } from "@/i18n";
import { waitingNoteText } from "@/lib/pendingChoice";
import { filePreviewWidth, resolveWorkspaceFileUrl, storageKeyFromUrl, useWorkspaceFile, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { useBackgroundImageFailure } from "@/lib/imageLoadFailure";
import type { PublicFileUrl } from "@/lib/publicFileUrls";
import { safeStoredFileUrl } from "@/lib/urls";
import type { UploadProgress } from "@/lib/storage";
import type { FileAttachment } from "@/lib/types";
import { AttachmentOpenLink } from "./AttachmentOpenLink";
import { fileAttachmentAddress, fileNameFromUrl, isImageAttachment } from "./files";
import { DotsHorizontal, FileBlank } from "../icons";
import { DragHandleIcon } from "@/icons/leafen";
import type { DragReorder } from "@/lib/useDragReorder";
import styles from "./databaseSheet";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { triggerBrowserLinkDownload } from "@/lib/browserFileDownload";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

export { fileNameFromUrl };

export function uploadProgressText(progress: UploadProgress) {
  if (progress.waiting) return waitingNoteText();
  if (progress.phase === "preparing") return i18next.t("propertyCell:uploadPreparing");
  if (progress.phase === "finalizing") return i18next.t("propertyCell:uploadFinishing");
  if (progress.phase === "complete") return i18next.t("propertyCell:uploadComplete");
  return i18next.t("propertyCell:uploadingProgress");
}

export function storedWorkspaceFileKey(file: FileAttachment) {
  if (file.key?.startsWith("workspaces/")) return file.key;
  if (file.id.startsWith("workspaces/")) return file.id;
  return storageKeyFromUrl(file.url);
}

export async function resolvedFileActionUrl(file: FileAttachment, publicFileUrl?: PublicFileUrl | null) {
  if (file.source === "external") return fileAttachmentAddress(file);
  const key = storedWorkspaceFileKey(file);
  // A published page opens its files from the page's public file route.
  if (key && publicFileUrl && !key.startsWith("workspaces/")) return publicFileUrl(key);
  if (key) {
    try {
      return await resolveWorkspaceFileUrl(key);
    } catch {
      // Preserve the retained stored-URL fallback when capability signing fails.
    }
  }
  return safeStoredFileUrl(file.url, ["data:"]);
}

export function triggerFileDownload(url: string, name: string) {
  return triggerBrowserLinkDownload(url, name || "download");
}

export function FileThumb({ file, className, iconSize }: {
  file: FileAttachment;
  className: string;
  iconSize: LeafenIconSize;
}) {
  const isImage = isImageAttachment(file);
  const thumb = useWorkspaceFile(fileAttachmentAddress(file), ["data:image/"], { width: filePreviewWidth(120) });
  const url = thumb.url;
  // A picture that couldn't be loaded, or whose file the server no longer
  // gives, shows a broken picture, as Notion's thumbnail does (seen 2026-10-21).
  // Any other file shows Notion's blank page, whatever its kind (seen 2026-11-15,
  // twice: a pdf, a txt and a csv alike in a cell's chips, its list and the phone's screen).
  const failed = useBackgroundImageFailure(isImage ? url : undefined, isImage ? thumb.failedStatus : 0);
  const style = isImage && url && !failed ? { backgroundImage: `url("${url.replace(/"/g, '\\"')}")` } : undefined;
  return (
    <span className={className} data-image={isImage ? "true" : undefined} data-image-failed={failed ? "true" : undefined} style={style}>
      {!isImage && <FileBlank size={iconSize} aria-hidden="true" />}
      {failed && <ImageBrokenIcon size={iconSize} aria-hidden="true" />}
    </span>
  );
}

/** An address no picture comes from, for one the server refused. */
const UNLOADABLE_PICTURE = "data:image/png;base64,";

/**
 * A files value's picture, its words the link's around it: one that can't be
 * loaded is left as the browser draws an image it couldn't load, with no
 * alternative words, as Notion's is (seen 2026-11-16: Chrome's 16px broken
 * picture in a cell, a page's value and the files list; iPhone Safari's "?"
 * box) — one the server refused takes an address that can't load.
 */
function FilePicture({ url, refused, className }: { url: string; refused: boolean; className?: string }) {
  const [broken, setBroken] = useState("");
  return <img className={className} src={refused ? UNLOADABLE_PICTURE : url} draggable={false} aria-hidden="true"
    data-image-failed={refused || broken === url ? "true" : undefined} onError={() => setBroken(url)} />;
}

/**
 * A picture in a files value shows itself alone, 24px high with its own
 * width, no pill and no name, as Notion's cell (seen 2026-11-10); one that
 * can't be loaded is the browser's 16px broken picture (FilePicture).
 */
export function FileImagePreview({ file }: { file: FileAttachment }) {
  const thumb = useWorkspaceFile(fileAttachmentAddress(file), ["data:image/"], { width: filePreviewWidth(120) });
  if (!thumb.url && !thumb.failedStatus) return <span className={styles.fileImagePending} aria-hidden="true" />;
  return <FilePicture className={styles.fileImage} url={thumb.url} refused={Boolean(thumb.failedStatus)} />;
}

export function PropertyFileOpenLink({
  file,
  className,
  ariaLabel,
  children,
}: {
  file: FileAttachment;
  className: string;
  ariaLabel: string;
  children: ReactNode;
}) {
  const href = useWorkspaceFileUrl(fileAttachmentAddress(file), ["data:image/", "data:application/pdf"]);
  return (
    <AttachmentOpenLink file={file} href={href} className={className} ariaLabel={ariaLabel}>
      {children}
    </AttachmentOpenLink>
  );
}

/**
 * A picture's line in the files editor shows the picture itself, up to 180
 * high and the line's width, in place of its icon and name, as Notion's
 * (seen 2026-11-11, dark); pressed, it opens as the name does. One that
 * can't be loaded is the browser's 16px broken picture at the line's top,
 * the line 40 high with its 24px ⠿ (Notion's, seen 2026-11-16: 232×40, the
 * picture 16 high 8px down).
 */
function FileMenuImage({ file }: { file: FileAttachment }) {
  const picture = useWorkspaceFile(fileAttachmentAddress(file), ["data:image/"], { width: filePreviewWidth(180) });
  return (
    <PropertyFileOpenLink file={file} className={styles.fileMenuImage} ariaLabel={i18next.t("propertyCell:open", { name: file.name })}>
      {picture.url || picture.failedStatus
        ? <FilePicture url={picture.url} refused={Boolean(picture.failedStatus)} />
        : <span className={styles.fileMenuImageEmpty} />}
    </PropertyFileOpenLink>
  );
}

function FileMenuText({ file }: { file: FileAttachment }) {
  return (
    <PropertyFileOpenLink
      file={file}
      className={styles.fileMenuText}
      ariaLabel={i18next.t("propertyCell:open", { name: file.name })}
    >
      <span>{file.name}</span>
    </PropertyFileOpenLink>
  );
}

export function PropertyFileListItem({
  file,
  reorder,
  neighbours,
  fileMenuId,
  fileMenuPlacement,
  fileMenuStyle,
  renamingFileId,
  renameDraft,
  fileMenuTriggerRef,
  fileActionMenuRef,
  setRenameDraft,
  cancelRename,
  commitRename,
  closeFileMenu,
  toggleFileMenu,
  downloadOne,
  openOriginal,
  startRename,
  removeFile,
}: {
  file: FileAttachment;
  reorder: DragReorder;
  neighbours: { above?: string; below?: string };
  fileMenuId: string | null;
  fileMenuPlacement: "above" | "below";
  fileMenuStyle: CSSProperties;
  renamingFileId: string | null;
  renameDraft: string;
  fileMenuTriggerRef: MutableRefObject<HTMLButtonElement | null>;
  fileActionMenuRef: MutableRefObject<HTMLDivElement | null>;
  setRenameDraft: (value: string) => void;
  cancelRename: () => void;
  commitRename: (fileId: string) => void;
  closeFileMenu: () => void;
  toggleFileMenu: (fileId: string, trigger: HTMLButtonElement) => void;
  downloadOne: (file: FileAttachment) => Promise<void>;
  openOriginal: (file: FileAttachment) => Promise<void>;
  startRename: (file: FileAttachment) => void;
  removeFile: (fileId: string) => void;
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  const isRenaming = renamingFileId === file.id;
  const picture = isImageAttachment(file) && !isRenaming;
  // A file's line, as Notion's list (seen 2026-11-11): its ⠿, its icon (20px,
  // 10 after the ⠿'s glyph, measured 2026-11-15), its name 8 after and "⋯"
  // at the end, 28px high, a picture's the picture alone; what
  // it can do is in "⋯". The ⠿, shown on every line, one alone too, drags
  // the file to another place in the value's order (Alt+↑↓ moves it by
  // one); not while it is renamed.
  return (
    <div className={styles.filesMenuItem} role="listitem" data-image-line={picture ? "true" : undefined} {...reorder.row(file.id)}>
      <span className={styles.fileDragHandle} data-file-drag-handle role="button" tabIndex={-1} aria-label={t("propertyCell:movePage")}
        {...reorder.handle(file.id, !isRenaming, neighbours)}
        onPointerDown={(event) => event.stopPropagation()} onClick={(event) => { event.preventDefault(); event.stopPropagation(); }}>
        <DragHandleIcon size="small" aria-hidden="true" />
      </span>
      {picture ? <FileMenuImage file={file} /> : <FileThumb file={file} className={styles.fileMenuThumb} iconSize="regular" />}
      {picture ? null : isRenaming ? (
        <input
          className={styles.fileRenameInput}
          autoFocus
          // Its Tab goes round the list as before, not out of the cell (tableCellEditorTab.ts; Notion's not measured).
          data-keeps-tab=""
          aria-label={t("propertyCell:renameFile", { name: file.name })}
          value={renameDraft}
          // The name opens chosen without its extension, as a file manager's rename does.
          onFocus={(event) => {
            const dot = event.currentTarget.value.lastIndexOf(".");
            event.currentTarget.setSelectionRange(0, dot > 0 ? dot : event.currentTarget.value.length);
          }}
          onChange={(event) => setRenameDraft(event.target.value)}
          onBlur={() => commitRename(file.id)}
          onClick={(event) => event.stopPropagation()}
          onKeyDown={(event) => {
            if (imeComposing(event)) return;
            if (fieldEnter(event)) commitRename(file.id);
            if (event.key === "Escape") cancelRename();
          }}
        />
      ) : <FileMenuText file={file} />}
      <button
        ref={fileMenuId === file.id ? fileMenuTriggerRef : undefined}
        type="button"
        className={styles.fileActionButton}
        aria-label={t("propertyCell:fileMenu", { name: file.name })}
        aria-haspopup="menu"
        aria-expanded={fileMenuId === file.id}
        onClick={(event) => { event.stopPropagation(); toggleFileMenu(file.id, event.currentTarget); }}
      >
        <DotsHorizontal size="small" aria-hidden="true" />
      </button>
      {fileMenuId === file.id && (
        <PropertyFileActionMenu file={file} menuRef={fileActionMenuRef} placement={fileMenuPlacement} style={fileMenuStyle}
          close={closeFileMenu} downloadOne={downloadOne} openOriginal={openOriginal} startRename={startRename} removeFile={removeFile} />
      )}
    </div>
  );
}

function PropertyFileActionMenu({ file, menuRef, placement, style, close, downloadOne, openOriginal, startRename, removeFile }: {
  file: FileAttachment; menuRef: MutableRefObject<HTMLDivElement | null>; placement: "above" | "below"; style: CSSProperties;
  close(): void; downloadOne(file: FileAttachment): Promise<void>; openOriginal(file: FileAttachment): Promise<void>;
  startRename(file: FileAttachment): void; removeFile(id: string): void;
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  const { touch } = useSidebarLayout(true);
  // A linked file is only opened, renamed or let go, as Notion's (seen
  // 2026-11-11: "원본 보기", "이름 바꾸기", "삭제"); an uploaded one downloads too.
  const items = [
    ...(file.source === "external" ? [] : [{ id: "download", label: t("propertyCell:download"), run: () => void downloadOne(file) }]),
    { id: "open", label: t("propertyCell:viewOriginal"), run: () => void openOriginal(file) },
    { id: "rename", label: t("propertyCell:rename"), run: () => startRename(file) },
    { id: "delete", label: t("common:actions.delete"), run: () => removeFile(file.id) },
  ];
  const menu = useNativeMenu({ items, open: !touch, menuRef, label: t("propertyCell:fileActions", { name: file.name }),
    // A choice closes the menu first, as Notion's, whatever it then comes to.
    keyOf: item => item.id, textOf: item => item.label, onAction: item => { close(); item.run(); } });
  // On a touch screen the same choices are Notion's phone half sheet "파일 작업"
  // (seen 2026-11-15 at 411 on the A90, twice in two tabs): one group of 45px
  // rows of 17px words, no icons; the sheet stands for the menu, so a press
  // in it is not a press outside the menu.
  if (touch) return (
    <PhoneSheet size="half" title={t("propertyCell:fileActionsTitle")} label={t("propertyCell:fileActions", { name: file.name })}
      sheetRef={menuRef} onClose={close} data={{ "data-file-action-sheet": "" }}>
      <PhoneSheetGroup>
        {items.map(item => <PhoneSheetRow key={item.id} more={false} label={item.label} data={{ "data-file-action": item.id }}
          onPress={() => { close(); item.run(); }} />)}
      </PhoneSheetGroup>
    </PhoneSheet>
  );
  return <div {...menu.menuProps} ref={menuRef} className={styles.fileActionMenu} data-placement={placement} style={style} data-keeps-tab=""
    onClick={event => event.stopPropagation()}>
    {items.map(item => <NativeMenuItem key={item.id} state={menu.state} itemKey={item.id}>
      {({ labelProps }) => <span {...labelProps}>{item.label}</span>}
    </NativeMenuItem>)}
  </div>;
}
