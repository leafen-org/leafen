import { type ReactNode, useEffect, useRef, useState } from "react";
import { BlockEmptyRow } from "./BlockEmptyPopover";
import { api } from "../../api";
import { isManagedFileBlock, type GraphBlock } from "../../contentGraph";
import { createFileDownloadCapabilityResolver } from "../../lib/fileDownloadCapability";
import { createPdfPreviewObjectURL, createPdfPreviewObjectURLFromResponse } from "../../lib/pdfPreview";
import { createUrlMetadataApi } from "../../urlMetadataApi";
import { usePublicFileUrl } from "../../lib/publicFileUrls";
import { blockItemText } from "./blockItemLabels";
import { PdfRowIcon } from "@/icons/leafen";
import { MediaUploadForm, emptyText } from "./MediaUploadForm";
import { emptyRowMark } from "./emptyRowMarks";
import { safeMediaUrl } from "./mediaBlockUrl";
import { giveMediaLink } from "./mediaLinkBlock";
import { PdfPages } from "./PdfPages";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import emptyStyles from "./BlockEmptyPopover.module.css";
import { ImageBlockTools } from "./ImageBlockTools";
import { fileBlockAlignment, uploadedFrameBox } from "./blockFileMenuModel";
import { useBlockFileActions } from "./useBlockFileActions";
import { type BlockBoxSize, BlockCornerHandles, frameShareStyle, PictureHandlePlace } from "./BlockCornerHandles";
import { keptWidthPatch, useKeptFrameShare } from "./frameKeptWidth";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { formatFileSize } from "../database/files";
import { openPdfPreview } from "./useBlockFileMenu";

// Status messages are kept as catalog keys and translated when shown.
type PdfStatus =
  | ""
  | "pdf.loading"
  | "pdf.notFound"
  | "pdf.publicUnavailable"
  | "pdf.previewFailed"
  | "pdf.invalidLink"
  | "pdf.linkPreviewFailed";

export function PdfBlock({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const managed = isManagedFileBlock(block);
  const fileId = String(block.content.fileId ?? "");
  // A link opens as a media block's (safeMediaUrl); one kept that opens nothing,
  // as Notion's API keeps "mailto:…", can't be shown, as a link that fails to
  // draw, as Notion's (seen 2026-11-10: "Failed to load PDF file.").
  const externalURL = managed ? "" : safeMediaUrl(block.content.url);
  const unopened = !managed && !externalURL && typeof block.content.url === "string" && block.content.url.trim() !== "";
  // A linked PDF with no name kept is named by its address's last part, as a linked file's row (MediaBlockContent).
  const fileName = String(block.content.name || (externalURL && new URL(externalURL).pathname.split("/").filter(Boolean).at(-1)) || "PDF");
  // A phone draws the PDF as its file's row, as Notion's iPhone (PhonePdfRow).
  const { phone } = useSidebarLayout(true);
  const [previewURL, setPreviewURL] = useState("");
  const [status, setStatus] = useState<PdfStatus>(managed ? "pdf.loading" : "");
  const [loading, setLoading] = useState(managed);
  const publicFileUrl = usePublicFileUrl();
  const fileActions = useBlockFileActions(block, adapter.session);
  const figureRef = useRef<HTMLElement>(null), boxRef = useRef<HTMLDivElement>(null);
  const [dragged, setDragged] = useState<BlockBoxSize | null>(null);
  // A file read but not drawable as a PDF keeps its box, tools and corners, its words at the top left, as
  // Notion's (seen 2026-11-06: "Failed to load PDF file." in the 320px box); the words are ours (principle 5).
  const [broken, setBroken] = useState(false);
  // Dragged past the column it stays so, as Notion's (seen 2026-11-15: 944 wide in a 704 column), its px kept (useKeptFrameShare).
  const storedWidth = useKeptFrameShare(figureRef, block.content, "pdf");
  const storedHeight = typeof block.content.height === "number" && block.content.height > 0 ? block.content.height : undefined;
  const width = dragged?.width ?? storedWidth, height = dragged?.height ?? storedHeight;

  useEffect(() => {
    setBroken(false);
    // A phone's row reads nothing until it is pressed.
    if (phone) {
      setPreviewURL("");
      setStatus("");
      setLoading(false);
      return;
    }
    if (!managed) {
      // A linked PDF is read through the server and shown as the local PDF it
      // was checked to be, as an uploaded one: the browser's viewer draws
      // nothing in the isolated frame an outside page would get.
      setPreviewURL("");
      if (!externalURL || !adapter.session) {
        setStatus(externalURL || unopened ? "pdf.linkPreviewFailed" : "");
        setLoading(false);
        return;
      }
      let live = true;
      let linkedURL = "";
      const controller = new AbortController();
      setStatus("pdf.loading");
      setLoading(true);
      void createUrlMetadataApi().readLinkedPdf(adapter.session, externalURL, controller.signal)
        .then(createPdfPreviewObjectURLFromResponse)
        .then((url) => {
          linkedURL = url;
          if (!live) return URL.revokeObjectURL(url);
          setPreviewURL(url);
          setStatus("");
          setLoading(false);
        })
        .catch(() => {
          if (!live) return;
          setStatus("pdf.linkPreviewFailed");
          setLoading(false);
        });
      return () => {
        live = false;
        controller.abort();
        if (linkedURL) URL.revokeObjectURL(linkedURL);
      };
    }
    setPreviewURL("");
    const readable = Boolean(adapter.session || publicFileUrl);
    if (!readable || !fileId) {
      setStatus(readable ? "pdf.notFound" : "pdf.publicUnavailable");
      setLoading(false);
      return;
    }
    let current = true;
    let objectURL = "";
    // A published page reads the file from its public route.
    const resolver = publicFileUrl ? null : createFileDownloadCapabilityResolver(
      () => api.managedFileCapability(adapter.session!, fileId),
    );
    setStatus("pdf.loading");
    setLoading(true);
    void createPdfPreviewObjectURL(async () => (
      resolver ? (await resolver.resolve()).url : publicFileUrl!(fileId)
    )).then((url) => {
      objectURL = url;
      if (!current) return URL.revokeObjectURL(url);
      setPreviewURL(url);
      setStatus("");
      setLoading(false);
    }).catch(() => {
      if (current) {
        // Read but no PDF (its bytes don't start as one): the broken box, as one pdf.js can't draw.
        setBroken(true);
        setStatus("");
        setLoading(false);
      }
    });
    return () => {
      current = false;
      resolver?.clear();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [adapter.session, externalURL, fileId, managed, phone, publicFileUrl, unopened]);

  const statusText = status ? blockItemText(status) : "";

  // An empty PDF on a phone is a file's empty row, its words and mark, as its filled one is a file's row
  // (PhonePdfRow) — Notion's (seen 2026-11-15 on a Galaxy A90 at 411 and twice on an iPhone at 402) — and
  // that row keeps the PDF block's own 8px round it, 16px in from the column and the block's top where the
  // other empty rows stand 8px in (measured 2026-11-15: A90 411 PDF 34–377 and 81 high, file, image, video
  // and audio 26–385 and 65 high; iPhone 402 PDF 34–367, the others 26–376). A desktop's stands as the others.
  const emptyIcon = emptyRowMark(phone ? "file" : "pdf");
  const emptyRow = emptyText(phone ? "file" : "pdf").row;
  const inset = (empty: ReactNode) => phone ? <div className={emptyStyles.phonePdfInset}>{empty}</div> : empty;
  // A reader's empty PDF keeps its row, not the box of a PDF that didn't draw (Notion's, 2026-11-08).
  if (!externalURL && !managed && !unopened && adapter.readOnly) {
    return inset(<BlockEmptyRow name="pdf" icon={emptyIcon} label={emptyRow} />);
  }
  if (!externalURL && !managed && !unopened && !adapter.readOnly) {
    return inset(<MediaUploadForm adapter={adapter} block={block} kind="pdf" accept="" icon={emptyIcon} row={emptyRow}
      onLink={(value) => giveMediaLink(adapter, block, value, "pdf.invalidLink", { caption: [] })} />);
  }

  if (phone) return <PhonePdfRow adapter={adapter} block={block} fileName={fileName} managed={managed} externalURL={externalURL}
    fileActions={fileActions} />;

  const align = fileBlockAlignment(block.content.align, nested);
  return <figure ref={figureRef} className={styles.pdfBlock} contentEditable={false} data-file-source={managed ? "managed" : "external"}
    data-align={align} data-upload-box={uploadedFrameBox(block, managed, width, align)} data-resizing={dragged ? "true" : undefined}
    style={frameShareStyle(width, "pdf")}
    aria-busy={loading || undefined}>
    <div className={styles.pdfFrame}>
      <PictureHandlePlace align={align} />
      {/* Uploaded or linked, a PDF shows as the local PDF it was checked to
          be, its pages drawn by pdf.js as Notion's block draws them. */}
      {broken ? <div ref={boxRef} className={styles.pdfPages} role="document" data-pdf-broken
        aria-label={blockItemText("pdf.previewNamed", { fileName })} style={height ? { height } : undefined}>
        <p className={styles.pdfBroken}>{blockItemText("pdf.previewFailed")}</p>
      </div> : previewURL ? <PdfPages url={previewURL} title={blockItemText("pdf.previewNamed", { fileName })} boxRef={boxRef}
        height={height}
        onFailed={() => setBroken(true)} /> : <div className={styles.pdfUnavailable}>
          <span>{statusText || blockItemText("pdf.previewUnavailable")}</span>
          {externalURL && !loading ? <a href={externalURL} target="_blank" rel="noopener noreferrer"
            data-pdf-open-original>{blockItemText("pdf.openOriginal")}</a> : null}
        </div>}
      {(previewURL || broken) && !adapter.readOnly ? <>
        <ImageBlockTools adapter={adapter} block={block} mode="pdf" align={align} />
        <BlockCornerHandles kind="pdf" figureRef={figureRef} boxRef={boxRef} onSize={setDragged} onEnd={(size) => {
          setDragged(null);
          const patch = keptWidthPatch(size, block.content);
          if (size && (patch || size.height !== storedHeight)) adapter.updateBlock(block, { ...patch, height: size.height });
        }} />
      </> : null}
      {previewURL && adapter.readOnly ? <div className={styles.pdfActions} data-reader-file-actions><button type="button"
        onClick={fileActions.open}>{blockItemText("common.openNewTab")}</button></div> : null}
    </div>

    {statusText && previewURL ? <span className={styles.pdfStatus} role="status" aria-live="polite">{statusText}</span> : null}
    {fileActions.error ? <span className={styles.pdfStatus} role="alert" data-error>{blockItemText(fileActions.blocked ? "file.downloadBlocked" : "file.loadFailed")}</span> : null}
  </figure>;
}

/**
 * A PDF on a phone, as Notion's iPhone draws it (seen 2026-11-13 at 402,
 * light): its file's row, the PDF mark, the name and an upload's size, 8px in
 * from each side of its block as its viewer's box would be, the row 28px in a
 * 40px box in a 56px block. Pressed, it opens Leafen's preview of the PDF
 * whole — an upload's with "다운로드하기" and "페이지에 추가", a link's with
 * close alone, as Notion's; its menu is the long press's (useBlockLongPress).
 */
function PhonePdfRow({ adapter, block, fileName, managed, externalURL, fileActions }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; fileName: string; managed: boolean; externalURL: string;
  fileActions: ReturnType<typeof useBlockFileActions>;
}) {
  const size = Number(block.content.size);
  const fileSize = managed && block.content.sizeHidden !== true && Number.isFinite(size) && size >= 0 ? formatFileSize(size) : "";
  const open = () => {
    if (adapter.session) openPdfPreview(adapter, block, fileActions);
    else if (managed) fileActions.open();
    else if (externalURL) window.open(externalURL, "_blank", "noopener,noreferrer");
  };
  return <figure className={styles.pdfBlock} contentEditable={false} data-file-source={managed ? "managed" : "external"} data-pdf-row>
    <div className={styles.pdfRow}>
      <div className={styles.fileCard}>
        <span className={styles.fileIcon} data-kind="pdf"><PdfRowIcon size="wide" aria-hidden="true" /></span>
        <button type="button" className={styles.fileInfo} onClick={open} disabled={managed && !fileActions.available}
          aria-label={blockItemText("pdf.previewNamed", { fileName })} data-file-open>
          <span className={styles.fileName}>{fileName}</span>
          {fileSize ? <span className={styles.fileMeta}>{fileSize}</span> : null}
        </button>
      </div>
    </div>
    {fileActions.error ? <span className={styles.pdfStatus} role="alert" data-error>{blockItemText(fileActions.blocked ? "file.downloadBlocked" : "file.loadFailed")}</span> : null}
  </figure>;
}
