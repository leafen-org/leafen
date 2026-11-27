import { type ReactNode, useEffect, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import { formatFileSize } from "../database/files";
import { BlockEmptyLinkPane, BlockEmptyPopover, BlockEmptyUploadPane } from "./BlockEmptyPopover";
import { i18next } from "@/i18n";
import { blockItemText } from "./blockItemLabels";
import { blockUploadProgressLabel, type BlockUploadProgress, type DroppedFileBlockType } from "./fileDrop";
import { useMediaBlockUpload } from "./mediaBlockUpload";
import { safeMediaUrl } from "./mediaBlockUrl";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { phoneScreen } from "@/lib/screenKind";
import { PageCoverUnsplash, UnsplashTabMark } from "../page/PageCoverUnsplash";
import emptyStyles from "./BlockEmptyPopover.module.css";
import { emptyRowMark } from "./emptyRowMarks";
import phoneStyles from "./BlockEmptyPhoneSheet.module.css";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";

function UploadSpinner() {
  return <svg className={emptyStyles.uploadingSpinner} viewBox="0 0 16 16" aria-hidden="true">
    <circle cx="8" cy="8" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeDasharray="30 11" />
  </svg>;
}

/** "업로드 중", and how far once it is under way (a large file takes minutes). */
function uploadStatus(progress: BlockUploadProgress) {
  const label = blockUploadProgressLabel(progress);
  return progress.phase === "uploading" && progress.percent > 0 && !progress.waiting ? `${label} ${Math.round(progress.percent)}%` : label;
}

/**
 * A file on its way, as Notion's (seen 2026-11-05): the block's window closes at
 * once. A picture or video shows from its bytes with a small "업로드 중" in its
 * corner; anything else, or until that shows, a gray row with its name and size.
 */
export function UploadingMedia({ kind, file, progress }: {
  kind: DroppedFileBlockType; file: File; progress: BlockUploadProgress;
}) {
  const visual = kind === "image" || kind === "video";
  const [source, setSource] = useState("");
  const [shown, setShown] = useState(false);
  const [naturalWidth, setNaturalWidth] = useState(0);
  useEffect(() => {
    if (!visual) return undefined;
    const url = URL.createObjectURL(file);
    setSource(url);
    setShown(false);
    return () => URL.revokeObjectURL(url);
  }, [file, visual]);
  const status = uploadStatus(progress);
  const chip = <span className={emptyStyles.uploadingChip} role="status"><UploadSpinner />{status}</span>;
  return <>
    {!shown ? <div className={emptyStyles.root} contentEditable={false} data-media-uploading={kind}>
      <div className={emptyStyles.uploadingRow} role="status">
        <span className={emptyStyles.rowIcon} aria-hidden="true">{emptyRowMark(kind)}</span>
        <span className={emptyStyles.uploadingText}>
          <span className={emptyStyles.uploadingName}>{progress.fileName}</span>
          <span className={emptyStyles.uploadingMeta}>{formatFileSize(file.size)} – <UploadSpinner />{status}</span>
        </span>
      </div>
    </div> : null}
    {kind === "video" && source ? <figure className={styles.videoBlock} contentEditable={false} data-file-source="managed"
      data-media-uploading={kind} style={shown ? undefined : { display: "none" }}>
      <div className={styles.videoFrame}>
        <video className={styles.videoPlayer} src={source} muted playsInline preload="auto" onLoadedData={() => setShown(true)} />
        {chip}
      </div>
    </figure> : null}
    {kind === "image" && source ? <figure className={styles.imageBlock} contentEditable={false} data-align="center"
      data-media-uploading={kind} style={shown ? undefined : { display: "none" }}>
      <div className={styles.imageFrame}>
        <img src={source} alt="" onLoad={(event) => { setNaturalWidth(event.currentTarget.naturalWidth); setShown(true); }}
          style={naturalWidth ? { width: Math.max(1, naturalWidth - 32) } : undefined} />
        {chip}
      </div>
    </figure> : null}
  </>;
}

// Notion's words for each block's row, window and link (seen 2026-10-18).
export function emptyText(kind: DroppedFileBlockType) {
  const text = (key: string) => blockItemText(`blockEmpty.${key}`);
  switch (kind) {
    case "image":
      return { row: text("image.row"), placeholder: text("image.placeholder"), submit: text("image.submit"), hint: text("image.hint"),
        choose: text("image.upload") };
    case "video":
      return { row: text("video.row"), placeholder: text("video.placeholder"), submit: text("video.submit"), hint: text("video.hint"),
        choose: text("chooseVideo") };
    case "audio":
      return { row: text("audio.row"), placeholder: text("linkPlaceholder"), submit: text("audio.submit"), hint: text("audio.hint"),
        choose: text("chooseFile") };
    case "pdf":
      return { row: text("pdf.row"), placeholder: "https://...", submit: text("pdf.submit"), hint: text("pdf.hint"),
        choose: text("chooseFile") };
    default:
      return { row: text("file.row"), placeholder: text("file.placeholder"), submit: text("file.submit"), hint: text("file.hint"),
        choose: text("chooseFile") };
  }
}

/**
 * The empty state of an image, video, audio, PDF or file block: Notion's
 * gray row and its window, to upload a file (an image's dashed area, the
 * others' blue button) or paste a link. `onLink` returns an error to show,
 * or "" once the block took the link. A block whose link is being replaced
 * starts from that link. A file dropped on the editor uploads here, its
 * progress under the row.
 */
export function MediaUploadForm({
  adapter,
  block,
  kind,
  accept,
  icon,
  initialLink = "",
  row,
  onLink,
}: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  kind: DroppedFileBlockType;
  accept: string;
  icon: ReactNode;
  initialLink?: string;
  /** The row's words where they aren't its kind's own (an empty PDF on a phone says a file's). */
  row?: string;
  onLink: (url: string) => string;
}) {
  const { progress, error, upload, retry } = useMediaBlockUpload(adapter, block, kind);
  const text = emptyText(kind);
  const image = kind === "image";
  const unsplash = useUnsplashEnabled(image);
  return (
    // A file's window opens 8px under its row; a picture's, video's, audio's
    // and PDF's touch theirs (Notion's, measured 2026-11-15 at 1440).
    // On a phone a whole screen, "파일" for a file or PDF and "임베드" for the others (Notion's, seen 2026-11-15).
    <BlockEmptyPopover blockId={block.id} name={kind} icon={icon} label={row ?? text.row} width={image ? 540 : 300} gap={kind === "file" ? 8 : 0}
      phoneTitle={blockItemText(kind === "file" || kind === "pdf" ? "file.label" : "common.embed")}
      tabs={[{ id: "upload", label: blockItemText("blockEmpty.upload") },
        { id: "link", label: blockItemText("blockEmpty.link") }, ...(unsplash ? [{ id: "unsplash", label: "Unsplash", icon: <UnsplashTabMark /> }] : [])]}
      rowHint={error ? <div className={styles.imageError} role="alert">{error}
        {retry ? <button type="button" className={styles.imageErrorRetry} data-block-upload-retry onClick={retry}>{i18next.t("common:retry")}</button> : null}
      </div> : null}>
      {(tab) => tab === "upload" ? (
        <BlockEmptyUploadPane accept={accept} choose={text.choose} progress={progress} outlined={image} kind={kind}
          onFile={upload} />
      ) : tab === "unsplash" ? (
        // The cover's Unsplash search; the picked photo, its credit in the
        // address, becomes the image (CoverCredit names its photographer and
        // Unsplash on it). On a phone's sheet its search is the sheet's row,
        // as Notion's.
        <div className={emptyStyles.unsplash} data-block-empty-pane="unsplash" data-sheet={phoneScreen() ? "true" : undefined}>
          <PageCoverUnsplash currentCover="" busy={false} sheet={phoneScreen() ? { row: phoneStyles.row, field: phoneStyles.field } : undefined}
            onApplyCover={async (cover) => {
              if (!safeMediaUrl(cover)) return false;
              adapter.updateBlock(block, { url: cover });
              return true;
            }} />
        </div>
      ) : (
        <BlockEmptyLinkPane placeholder={text.placeholder} submit={text.submit} hint={text.hint} initial={initialLink}
          onLink={onLink} />
      )}
    </BlockEmptyPopover>
  );
}

/**
 * Whether an image's window offers Unsplash (2026-10-18, Notion's tab): only
 * on a server whose operator gave it an Unsplash key, as the cover picker
 * does: the operator opts in, and there is no central key.
 */
function useUnsplashEnabled(image: boolean) {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    if (!image) return;
    let cancelled = false;
    void fetchRuntimeConfig().then(({ config }) => { if (!cancelled) setEnabled(config.unsplashCoverEnabled === true); });
    return () => { cancelled = true; };
  }, [image]);
  return enabled;
}
