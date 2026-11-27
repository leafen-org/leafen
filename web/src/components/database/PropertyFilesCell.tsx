import {
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { getFiles } from "@atlaskit/pragmatic-drag-and-drop/external/file";
import { useTranslation } from "react-i18next";
import { dropTargetForFiles } from "@/lib/fileDropTarget";
import { placeAround, popoverRectOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { newId } from "@/lib/ids";
import { usePublicFileUrl } from "@/lib/publicFileUrls";
import type { UploadProgress } from "@/lib/storage";
import { safeStoredFileUrl } from "@/lib/urls";
import type { DbProperty, FileAttachment, Page } from "@/lib/types";
import { isImageAttachment, normalizeFileAttachments } from "./files";
import { externalDragHasFiles } from "../editor/fileDrop";
import { Download, Plus } from "../icons";
import { UploadProgressBar, UploadProgressValue } from "../import/UploadProgressBar";
import { openEditorFromCellBackground } from "./PropertyCellMenu";
import { FilesEditorFrame } from "./FilesEditorFrame";
import { touchScreen } from "@/lib/screenKind";
import { useStateUnlessSame } from "@/lib/useStateUnlessSame";
import {
  FileImagePreview,
  FileThumb,
  PropertyFileOpenLink,
  PropertyFileListItem,
  resolvedFileActionUrl,
  triggerFileDownload,
  uploadProgressText,
} from "./PropertyFilesCellView";
import { FilesAddWindow } from "./PropertyFilesAddWindow";
import { capturePropertyCellRequest, type PropertyCellEnvironment } from "./propertyCellTypes";
import { useOptionalProductToast } from "@/lib/productToast";
import styles from "./databaseSheet";
import { uploadRefusalReason } from "@/lib/uploadRefusal";
import { restoreFocusUnlessChosen } from "./tableCellKeys";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";
import { useDragReorder } from "@/lib/useDragReorder";
import { relationIdsMoved } from "./PropertyRelationCell";
import { giveFocusBack } from "@/lib/focusGiveBack";

function progressBarProgress(progress: UploadProgress) {
  return {
    phase: progress.phase === "preparing" ? "preparing" as const : "uploading" as const,
    percent: progress.percent,
  };
}

export function PropertyFilesCell({ row, prop, snapshot, operations, context }: {
  row: Page;
  prop: DbProperty;
} & PropertyCellEnvironment) {
  const { t } = useTranslation(["propertyCell", "common"]);
  const publicFileUrl = usePublicFileUrl();
  const toast = useOptionalProductToast();
  const [open, setOpen] = useState(false);
  const readOnly = useCellReadOnly();
  const [addOpen, setAddOpen] = useState(false);
  // Whether the add window was opened from its "+" line, which then takes the
  // focus back once a file is added; opened by itself on an empty value, no
  // line is ringed (Notion's, seen 2026-11-14; closeAddWindow).
  const [addFromLine, setAddFromLine] = useState(false);
  // Whether the add window closing now leaves the focus on nothing (addLink).
  const leaveFocus = useRef(false);
  const [fileMenuId, setFileMenuId] = useState<string | null>(null);
  const [renamingFileId, setRenamingFileId] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [uploadError, setUploadError] = useState("");
  const [fileDropActive, setFileDropActive] = useState(false);
  const [uploadProgress, setUploadProgress] = useStateUnlessSame<
    { fileName: string; phase: UploadProgress["phase"]; percent: number; waiting?: boolean } | null
  >(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const editorRef = useRef<HTMLDivElement>(null);
  const addLineRef = useRef<HTMLButtonElement>(null);
  const fileMenuTriggerRef = useRef<HTMLButtonElement | null>(null);
  const fileActionMenuRef = useRef<HTMLDivElement | null>(null);
  const files = normalizeFileAttachments(row.properties?.[prop.id]);
  const uploadedFiles = files.filter((file) => file.source !== "external");
  const oneFile = prop.config?.limit === 1;
  const uploading = uploadProgress !== null;
  // Files dragged in from outside onto the open menu upload here; the shared
  // file drop place owns the drag, and the upload checks how many may come.
  const fileDrop = useRef({ uploading, bounds: snapshot.fileDropBounds, addUploads });
  fileDrop.current = { uploading, bounds: snapshot.fileDropBounds, addUploads };
  const dropSurfaceRef = useCallback((element: HTMLDivElement | null) => element ? dropTargetForFiles({
    element,
    canDrop: ({ source }) => !fileDrop.current.uploading && externalDragHasFiles(source.types, fileDrop.current.bounds),
    getDropEffect: () => "copy",
    onDragEnter: () => setFileDropActive(true),
    onDragLeave: () => setFileDropActive(false),
    onDrop: ({ source }) => {
      setFileDropActive(false);
      const selected = getFiles({ source }).filter((file) => file.size > 0);
      if (selected.length > 0) void fileDrop.current.addUploads(selected);
    },
  }) : undefined, []);
  // The list's ground, which holds the focus where no line should: after a
  // file is uploaded from the add window that opened by itself.
  const surfaceRef = useRef<HTMLDivElement | null>(null);
  const holdSurface = useCallback((element: HTMLDivElement | null) => {
    surfaceRef.current = element;
    return dropSurfaceRef(element);
  }, [dropSurfaceRef]);

  useEffect(() => {
    setUploadProgress(null);
  }, [context.dataSourceId, context.generation, context.pageId, context.sessionIdentity]);

  const filePlace = usePopoverPlacement(fileActionMenuRef, fileMenuId ? (size, viewport) =>
    placeAround(popoverRectOf(fileMenuTriggerRef.current)!, size, viewport, { gap: 0 }) : null, [fileMenuId],
    undefined, { kind: "menu", inside: () => [fileMenuTriggerRef.current], onEscape: () => setFileMenuId(null),
      onOutsidePress: () => setFileMenuId(null) });

  function commit(next: FileAttachment[]) {
    void operations.setValue(row.id, prop, next.length ? next : null, context);
  }

  // A file dropped before or after another takes that place in the value's
  // own order, saved at once, as Notion's (seen 2026-11-11); ⌘Z takes it back.
  const reorder = useDragReorder(`files:${row.id}:${prop.id}`, (fileId, targetId, side) => {
    const current = currentFiles();
    const next = relationIdsMoved(current.map((file) => file.id), fileId, targetId, side);
    const byId = new Map(current.map((file) => [file.id, file]));
    if (next) commit(next.flatMap((id) => byId.get(id) ?? []));
  }, { live: true });

  function currentFiles() {
    return normalizeFileAttachments(operations.getCurrentFiles(row.id, prop.id));
  }

  // "⋯" › "삭제" lets the file go from the value at once, the list staying
  // open, as Notion's (seen 2026-11-13): uploaded or added by its link
  // alike, saved as a rename or a move is; the server lets go of a stored
  // file no value holds any longer, after a while in which it can come back.
  function removeFile(id: string) {
    const current = currentFiles();
    setFileMenuId(null);
    if (current.some((file) => file.id === id)) commit(current.filter((file) => file.id !== id));
  }

  async function addUploads(list: FileList | readonly File[] | null) {
    // Every file picked or dropped, uploaded one after another. A property limited to one file takes the first and replaces what it held, as Notion's "파일 1개".
    const selected = Array.from(list ?? []).slice(0, oneFile ? 1 : undefined);
    if (selected.length === 0) return;
    const request = capturePropertyCellRequest(context);
    leaveFocus.current = false;
    setUploadError("");
    setOpen(true);
    setAddOpen(true);
    setUploadProgress({ fileName: selected[0]?.name || t("common:labels.untitled"), phase: "preparing", percent: 0 });
    try {
      const uploaded: FileAttachment[] = [];
      for (const [index, file] of selected.entries()) {
        const uploadedFile = await operations.uploadFile(file, row, prop, (progress) => {
          if (!request.isCurrent()) return;
          const base = (index / selected.length) * 100;
          const span = 100 / selected.length;
          setUploadProgress({
            fileName: file.name || t("common:labels.untitled"),
            phase: progress.phase,
            percent: base + (progress.percent / 100) * span,
            waiting: progress.waiting,
          });
        }, { signal: request.signal }, context);
        if (!request.isCurrent()) return;
        uploaded.push(uploadedFile);
      }
      commit(oneFile ? uploaded : [...currentFiles(), ...uploaded]);
      closeAddWindow();
    } catch (error) {
      if (request.isCurrent()) setUploadError(uploadRefusalReason(error) ?? t("propertyCell:fileUploadFailed"));
    } finally {
      if (request.isCurrent()) setUploadProgress(null);
      request.abort();
    }
  }

  function addLink(text: string) {
    // Whatever is typed is taken, as Notion's (seen 2026-11-11: "abc" became
    // a file "abc" at "abc"); only a web address opens (fileAttachmentAddress).
    const typed = text.trim();
    if (!typed) return;
    const url = /^https?:\/\//iu.test(typed) ? safeStoredFileUrl(typed) || typed : typed;
    // A file added by its link is named by the whole link, as Notion's (seen
    // 2026-11-07: "https://example.com/report-file.pdf", cut short in the chip).
    const linked: FileAttachment = { id: newId(), source: "external", name: url, url };
    commit(oneFile ? [linked] : [...currentFiles(), linked]);
    leaveFocus.current = !addFromLine;
    closeAddWindow();
  }

  // Added, the window closes on the list, the focus back on the line that
  // opened it, ringed, as Notion's (2026-11-26, two tabs). A link added in
  // the window that opened by itself leaves the focus on nothing, as Notion's
  // (2026-11-12, 2026-11-15, 2026-11-26, Chrome, no script before the action,
  // judged by the screen and one typed letter: an empty cell's window, 링크,
  // Enter or a press on "링크" — the list stays open with no ring and the
  // letter lands nowhere); Tab then still leaves the list for the cell beside
  // and ⌘⇧O opens the share over it (tableCellEditorTab.ts, TopBarShareFace).
  // Uploaded files give it to the list's ground (not measured). A window
  // opened as it closes keeps the focus (Enter and ⌘⇧O at once, from the
  // empty cell's window and from the "+" line's: 2026-11-26, two tabs).
  function closeAddWindow() {
    setUploadError("");
    setAddOpen(false);
    if (leaveFocus.current) return;
    giveFocusBack({ to: addFromLine ? addLineRef.current : surfaceRef.current, keepScroll: true, unlessOpened: true });
  }

  function openEditor() {
    if (readOnly) return;
    leaveFocus.current = false;
    if (!open) {
      // A touch screen's opens on its "+" line alone, as Notion's phone (seen 2026-11-15).
      setAddOpen(files.length === 0 && !touchScreen());
      setAddFromLine(false);
    }
    setOpen(true);
  }

  async function resolveFileAction(file: FileAttachment, action: "download" | "open") {
    const request = capturePropertyCellRequest(context);
    const url = await resolvedFileActionUrl(file, publicFileUrl);
    if (!request.isCurrent()) return;
    if (!url) {
      // A link that is no web address opens nothing, as Notion's "원본 보기"
      // (seen 2026-11-11); an uploaded file the server can't give says so
      // in a notice over the page, the list left as it was.
      if (file.source !== "external") toast?.notify(t(action === "download" ? "propertyCell:noDownloadUrl" : "propertyCell:noOpenUrl"), "error");
      request.abort();
      return;
    }
    if (action === "download") triggerFileDownload(url, file.name);
    else window.open(url, "_blank", "noopener,noreferrer");
    request.abort();
  }

  async function downloadOne(file: FileAttachment) {
    await resolveFileAction(file, "download");
  }

  // "모두 다운로드하기" takes the uploaded files, as Notion's, which offers it
  // only where one is (seen 2026-11-11: none on a value of links).
  async function downloadAll() {
    const request = capturePropertyCellRequest(context);
    let downloaded = 0;
    try {
      for (const file of uploadedFiles) {
        const url = await resolvedFileActionUrl(file, publicFileUrl);
        if (!request.isCurrent()) return;
        if (!url) continue;
        triggerFileDownload(url, file.name);
        downloaded += 1;
        await new Promise((resolve) => window.setTimeout(resolve, 120));
        if (!request.isCurrent()) return;
      }
      if (downloaded === 0 && request.isCurrent()) toast?.notify(t("propertyCell:noDownloadUrl"), "error");
    } finally {
      request.abort();
    }
  }

  async function openOriginal(file: FileAttachment) {
    await resolveFileAction(file, "open");
  }

  function startRename(file: FileAttachment) {
    setFileMenuId(null);
    setRenamingFileId(file.id);
    setRenameDraft(file.name);
  }

  function commitRename(fileId: string) {
    const nextName = renameDraft.trim();
    setRenamingFileId(null);
    if (!nextName) return;
    const current = currentFiles();
    if (!current.some((file) => file.id === fileId)) return;
    commit(current.map((file) => file.id === fileId ? { ...file, name: nextName } : file));
  }

  function closeMenu(restoreFocus = false) {
    setOpen(false);
    setAddOpen(false);
    setFileDropActive(false);
    setFileMenuId(null);
    setRenamingFileId(null);
    if (restoreFocus) restoreFocusUnlessChosen(triggerRef.current);
  }

  return (
    <div className={styles.filesCell}>
      <div
        className={styles.filesChips}
        role="group"
        aria-label={prop.name}
        data-file-attachments
        data-empty={files.length === 0 ? "true" : undefined}
        onClick={(event) => openEditorFromCellBackground(event, openEditor)}
      >
        {files.map((file) => isImageAttachment(file) ? (
          <span key={file.id} className={styles.fileImageChip}>
            <PropertyFileOpenLink file={file} className={styles.fileChipLink} ariaLabel={t("propertyCell:open", { name: file.name })}>
              <FileImagePreview file={file} />
            </PropertyFileOpenLink>
          </span>
        ) : (
          <span key={file.id} className={styles.fileChip}>
            <PropertyFileOpenLink file={file} className={styles.fileChipLink} ariaLabel={t("propertyCell:open", { name: file.name })}>
              <FileThumb file={file} className={styles.fileThumb} iconSize="regular" />
              <span className={styles.fileName}>{file.name}</span>
            </PropertyFileOpenLink>
          </span>
        ))}
        {files.length === 0 && <span className={styles.cellEmpty}>&nbsp;</span>}
        <button ref={triggerRef} type="button" className={styles.propertyCellEditTrigger} aria-label={t("propertyCell:editFiles", { name: prop.name })} aria-haspopup="dialog" aria-expanded={open}
          {...readOnlyStop(readOnly)} onClick={openEditor}>
          <Plus size="tiny" aria-hidden="true" />
        </button>
      </div>
      {open && !readOnly && (
        // Notion's files editor (seen 2026-11-11): over the cell from its top
        // left, 240 wide or the cell's width, its files' lines and under a
        // line "모두 다운로드하기" where one is uploaded and "+ 파일 또는
        // 이미지를 추가하세요", which opens the add window under it
        // (FilesAddWindow) — open at once on an empty value, drawn once the
        // list is placed: its layer then stands over the list's, and Escape
        // closes it alone, the list staying, as Notion's (2026-11-26, two tabs).
        <FilesEditorFrame label={t("propertyCell:editFilesProperty")} triggerRef={triggerRef} editorRef={editorRef} onClose={() => closeMenu(true)}>
          {(placed) => <div ref={holdSurface} tabIndex={-1} className={styles.filesMenuDropSurface} data-file-drop-zone="true" data-file-drop-active={fileDropActive ? "true" : undefined} data-empty={files.length === 0 ? "true" : undefined} aria-busy={uploading}>
            {files.length > 0 && (
              <div className={styles.filesMenuList} role="list">
                {files.map((file, index) => (
                  <PropertyFileListItem
                    key={file.id}
                    file={file}
                    reorder={reorder}
                    neighbours={{ above: files[index - 1]?.id, below: files[index + 1]?.id }}
                    fileMenuId={fileMenuId}
                    fileMenuPlacement={filePlace?.side === "above" ? "above" : "below"}
                    fileMenuStyle={{ position: "fixed", right: "auto", bottom: "auto", ...popoverPlacementStyle(filePlace) }}
                    renamingFileId={renamingFileId}
                    renameDraft={renameDraft}
                    fileMenuTriggerRef={fileMenuTriggerRef}
                    fileActionMenuRef={fileActionMenuRef}
                    setRenameDraft={setRenameDraft}
                    cancelRename={() => { setRenamingFileId(null); setRenameDraft(""); }}
                    commitRename={commitRename}
                    closeFileMenu={() => setFileMenuId(null)}
                    toggleFileMenu={(id, trigger) => { fileMenuTriggerRef.current = trigger; setFileMenuId((current) => current === id ? null : id); }}
                    downloadOne={downloadOne}
                    openOriginal={openOriginal}
                    startRename={startRename}
                    removeFile={removeFile}
                  />
                ))}
              </div>
            )}
            {files.length > 0 && <div className={styles.filesMenuRule} role="separator" />}
            <div className={styles.filesMenuAdd}>
              {uploadedFiles.length > 0 && (
                <button type="button" className={styles.filesMenuAction} data-files-download-all onClick={() => void downloadAll()}>
                  <Download size="small" aria-hidden="true" /><span>{t("propertyCell:downloadAll")}</span>
                </button>
              )}
              <button ref={addLineRef} type="button" className={styles.filesMenuAction} aria-haspopup="dialog" aria-expanded={addOpen}
                data-files-add-line onClick={() => { setUploadError(""); leaveFocus.current = false; setAddFromLine(true); setAddOpen((current) => !current); }}>
                <Plus size="small" aria-hidden="true" /><span>{t("propertyCell:addFileOrImage")}</span>
              </button>
            </div>
            {addOpen && placed && (
              <FilesAddWindow line={addLineRef} holder={editorRef} returnTo={addFromLine ? addLineRef : surfaceRef} leavesFocus={() => leaveFocus.current} ringsFirst={!addFromLine} oneFile={oneFile} busy={uploading} error={uploadError}
                status={uploadProgress && (
                  <div className={styles.filesUploadProgress} role="status" aria-live="polite">
                    <div className={styles.filesUploadProgressHeader}><span>{uploadProgressText(uploadProgress)}</span><strong>{uploadProgress.waiting ? null : <UploadProgressValue progress={progressBarProgress(uploadProgress)} />}</strong></div>
                    <div className={styles.filesUploadProgressName}>{uploadProgress.fileName}</div>
                    <UploadProgressBar className={styles.filesUploadProgressTrack} label={t("propertyCell:uploadingFile", { name: uploadProgress.fileName })} progress={progressBarProgress(uploadProgress)} />
                  </div>
                )}
                onFiles={(list) => { if (!uploading) void addUploads(list.filter((file) => file.size > 0)); }} onLink={addLink}
                onClose={() => { setUploadError(""); setAddOpen(false); }} />
            )}
          </div>}
        </FilesEditorFrame>
      )}
    </div>
  );
}
