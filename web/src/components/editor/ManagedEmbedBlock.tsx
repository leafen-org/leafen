import { type ReactNode, useEffect, useRef, useState } from "react";
import { DocumentPreviewFrame } from "@/components/apps/DocumentPreview";
import { DocumentSheetTabs, useDocumentSheets } from "@/components/apps/DocumentSheetTabs";
import { embedDocumentKind } from "@/lib/documentKinds";
import { api } from "../../api";
import type { GraphBlock } from "../../contentGraph";
import { createFileDownloadCapabilityResolver } from "../../lib/fileDownloadCapability";
import { usePublicFileUrl } from "../../lib/publicFileUrls";
import { blockItemText, blockTypeLabel } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import { BlockCornerHandles, frameShareStyle, PictureHandlePlace, type BlockBoxSize } from "./BlockCornerHandles";
import { keptWidthPatch, useKeptFrameShare } from "./frameKeptWidth";
import { ImageBlockTools } from "./ImageBlockTools";
import { fileBlockAlignment } from "./blockFileMenuModel";
import { ManagedFileBlock } from "./ManagedFileBlock";
import { openBlockToolMenu, requestEditorBlockCommand } from "./editorCommands";
import { useEmbedInteract } from "./EmbedInteractOverlay";
import { useBlockFileActions } from "./useBlockFileActions";
import { CommentAddIcon, DotsHorizontal, ExpandDiagonalIcon, PresentIcon } from "@/icons/leafen";
import { openDocumentPresentation } from "@/components/apps/DocumentPresentation";
import headStyles from "./DocumentEmbedHead.module.css";

// An uploaded embed renders inline from an object URL of its file, so no
// expiring capability URL reaches the page. Leafen stores no active content,
// so an embed file is a document, a media file, a text file or another file.

const MANAGED_EMBED_MAX_BYTES = 50 * 1024 * 1024;
// A document's frame, as Notion's (seen 2026-11-07): 320px as an embed is
// made, or the height the block keeps (480 when added from a file's preview).
const DOCUMENT_EMBED_HEIGHT = 320;

type ManagedEmbedKind = "pdf" | "image" | "video" | "audio" | "docx" | "xlsx" | "pptx" | "csv" | "sheet" | "text" | "file";

type ManagedEmbedPreview = { kind: ManagedEmbedKind; url: string; text?: string };
// Status messages are kept as catalog keys and translated when shown.
type ManagedEmbedStatus = "" | "embed.publicUnavailable" | "embed.fileNotFound" | "embed.loading" | "embed.loadFailed";

function managedEmbedKind(contentType: string): ManagedEmbedKind | null {
  if (contentType === "application/pdf") return "pdf";
  if (contentType.startsWith("image/")) return "image";
  if (contentType.startsWith("video/")) return "video";
  if (contentType.startsWith("audio/")) return "audio";
  return null;
}

// A text file shows its words in the frame, as Notion's frame of one (seen
// 2026-11-12: the browser's plain text, 692 × 320), drawn as text here so
// nothing in it runs; any other file shows its row there.
function otherEmbedKind(contentType: string): ManagedEmbedKind {
  return contentType.startsWith("text/") || contentType === "application/json" ? "text" : "file";
}

type DocumentKind = "docx" | "xlsx" | "pptx" | "csv" | "sheet";
const documentKind = (kind: ManagedEmbedKind | null): kind is DocumentKind =>
  kind === "docx" || kind === "xlsx" || kind === "pptx" || kind === "csv" || kind === "sheet";

export function ManagedEmbedBlock({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const fileId = String(block.content.fileId ?? "");
  const name = String(block.content.name ?? blockTypeLabel("embed"));
  const contentType = String(block.content.contentType ?? "").split(";")[0].trim().toLowerCase();
  // A Word, Excel or PowerPoint file shows in the page through Leafen's
  // document preview, as Notion's embed of one does (2026-11-06), a CSV file
  // as a sheet (seen 2026-11-12); one stored without its type goes by its name.
  const kind = managedEmbedKind(contentType) ?? embedDocumentKind(name, contentType) ?? (fileId ? otherEmbedKind(contentType) : null);
  const [preview, setPreview] = useState<ManagedEmbedPreview | null>(null);
  const [status, setStatus] = useState<ManagedEmbedStatus>("");
  const publicFileUrl = usePublicFileUrl();
  // A drawn document takes presses only once its "상호 작용" is pressed, as Notion's.
  const interact = useEmbedInteract(Boolean(preview && documentKind(preview.kind)));
  // A sheet drawn as a plain table has its sheets' tabs beside its name, as Notion's (2026-11-15).
  const sheets = useDocumentSheets();
  // A document that can't be drawn may offer its file instead (DocumentPreviewFrame).
  const fileActions = useBlockFileActions(block, adapter.session);

  useEffect(() => {
    setPreview(null);
    if (!adapter.session && !publicFileUrl) {
      setStatus("embed.publicUnavailable");
      return;
    }
    if (!fileId || !kind) {
      setStatus("embed.fileNotFound");
      return;
    }
    // A file of no kind the page draws is its row, which reads nothing yet.
    if (kind === "file") {
      setStatus("");
      return;
    }
    let current = true;
    let objectURL = "";
    const session = adapter.session;
    // A published page reads the file from its public route.
    const resolver = publicFileUrl || !session
      ? null
      : createFileDownloadCapabilityResolver(() => api.managedFileCapability(session, fileId));
    setStatus("embed.loading");
    void (async () => {
      const address = resolver ? (await resolver.resolve()).url : publicFileUrl!(fileId);
      const response = await fetch(address);
      if (!response.ok) throw new Error("embed file is unavailable");
      const blob = await response.blob();
      if (blob.size > MANAGED_EMBED_MAX_BYTES) throw new Error("embed file exceeds the preview limit");
      if (!current) return;
      if (kind === "text") {
        const text = await blob.text();
        if (current) setPreview({ kind, url: "", text });
        return;
      }
      objectURL = URL.createObjectURL(new Blob([blob], { type: contentType }));
      if (current) setPreview({ kind, url: objectURL });
    })().then(() => {
      if (current) setStatus("");
    }).catch(() => {
      if (current) setStatus("embed.loadFailed");
    });
    return () => {
      current = false;
      resolver?.clear();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [adapter.session, contentType, fileId, kind, publicFileUrl]);

  if (kind === "text" || kind === "file") {
    return <FramedManagedEmbed adapter={adapter} block={block} kind={kind} nested={nested}>
      {kind === "file" ? <div className={styles.fileEmbedRow}>
        <ManagedFileBlock block={block} session={adapter.session} readOnly={adapter.readOnly} pageId={adapter.pageId} workspaceId={adapter.workspaceId} />
      </div> : preview?.kind === "text" ? <pre className={styles.textEmbed} data-text-embed="true">{preview.text}</pre>
        : <div className={styles.mediaEmpty} role="status">{status ? blockItemText(status) : ""}</div>}
    </FramedManagedEmbed>;
  }

  let content = <div className={styles.mediaEmpty} role="status">{status ? blockItemText(status) : ""}</div>;
  if (preview?.kind === "pdf") {
    content = <iframe src={preview.url} title={name} loading="lazy" data-embed-iframe="true" />;
  } else if (preview?.kind === "image") {
    content = <img src={preview.url} alt={name} />;
  } else if (preview?.kind === "video") {
    content = <video src={preview.url} controls playsInline />;
  } else if (preview?.kind === "audio") {
    content = <audio src={preview.url} controls />;
  } else if (preview && documentKind(preview.kind)) {
    const url = preview.url;
    const height = typeof block.content.height === "number" && block.content.height > 0 ? block.content.height : DOCUMENT_EMBED_HEIGHT;
    // A CSV file is drawn as a sheet, by the preview's sheet reader.
    content = <DocumentPreviewFrame kind={preview.kind} title={name} embedded failedClassName={styles.documentEmbedFailed}
      className={`${styles.documentEmbedFrame} ${interact.contentClassName}`} style={{ height }}
      read={async () => (await fetch(url)).arrayBuffer()} sheet={sheets.at} onFrameMessage={sheets.heard}
      onDownload={fileActions.available ? fileActions.download : undefined} />;
  }
  return (
    <figure className={styles.embedBlock} contentEditable={false} data-managed-embed={kind ?? "unsupported"}
      data-align={fileBlockAlignment(block.content.align, nested)}
      style={typeof block.content.width === "number" ? { width: `${Math.max(20, Math.min(100, block.content.width))}%` } : undefined}>
      {documentKind(kind) ? <DocumentEmbedHead adapter={adapter} block={block} name={name} kind={kind}
        tabs={<DocumentSheetTabs names={sheets.names} at={sheets.at} onPick={sheets.pick} label={blockItemText("file.previewSheets")} />}
        onPresent={() => {
          if (preview?.kind === "pptx") openDocumentPresentation({ name, read: async () => (await fetch(preview.url)).arrayBuffer() });
        }} /> : null}
      <div className={`${styles.embedFrame} ${documentKind(kind) ? styles.documentEmbedBox : ""} ${interact.frameClassName}`} data-embed-frame="true">{content}
        {interact.overlay}
        {preview && !adapter.readOnly && !documentKind(kind) ? <ImageBlockTools adapter={adapter} block={block} mode="embed"
          align={fileBlockAlignment(block.content.align, nested)} /> : null}
      </div>
    </figure>
  );
}

/**
 * A document's head as Notion's (seen 2026-11-12, a CSV, an Excel, a Word and
 * a PowerPoint file): its name — a plain-table sheet's tabs after it (seen
 * 2026-11-15) —, and at its end, while the pointer is on the
 * block, "댓글 추가" (a sheet's), "전체화면으로 열기" (a sheet's or a Word
 * file's) or "프레젠테이션 모드" (a PowerPoint file's), then "더 보기" always;
 * no tools over the document.
 */
function DocumentEmbedHead({ adapter, block, name, kind, tabs, onPresent }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; name: string; kind: DocumentKind; tabs: ReactNode; onPresent: () => void;
}) {
  const sheet = kind === "csv" || kind === "xlsx" || kind === "sheet";
  return <div className={styles.documentEmbedHead} data-document-embed-head>
    <span className={headStyles.name}>{name}</span>
    {tabs}
    {!adapter.readOnly ? <span className={headStyles.tools} data-document-embed-tools>
      {sheet && adapter.session ? <button type="button" className={headStyles.hoverTool} aria-label={blockItemText("embed.documentTools.comment")}
        data-document-tool="comment" onClick={(event) => requestEditorBlockCommand(block.id, "comment", undefined, event.currentTarget)}>
        <CommentAddIcon size="small" aria-hidden="true" /></button> : null}
      {kind === "pptx" ? <button type="button" className={headStyles.hoverTool} aria-label={blockItemText("embed.documentTools.present")}
        data-document-tool="present" onClick={onPresent}>
        <PresentIcon size="small" aria-hidden="true" /></button>
        : <button type="button" className={headStyles.hoverTool} aria-label={blockItemText("embed.documentTools.fullScreen")}
          data-document-tool="full-screen" onClick={(event) => requestEditorBlockCommand(block.id, "imageFullScreen", undefined, event.currentTarget)}>
          <ExpandDiagonalIcon size="small" aria-hidden="true" /></button>}
      <button type="button" aria-label={blockItemText("embed.documentTools.more")} data-document-tool="menu"
        onClick={(event) => openBlockToolMenu(block.id, event)}>
        <DotsHorizontal size="small" aria-hidden="true" /></button>
    </span> : null}
  </div>;
}

/**
 * A text file's or another file's embed in Notion's frame for one (seen
 * 2026-11-12): the column wide, 8px in, 320px high until its sides or foot
 * are dragged, as a link's embed (LinkEmbedBlock), its tools on hover.
 */
function FramedManagedEmbed({ adapter, block, kind, nested, children }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; kind: ManagedEmbedKind; nested: boolean; children: ReactNode;
}) {
  const figureRef = useRef<HTMLElement>(null), boxRef = useRef<HTMLDivElement>(null);
  const [dragged, setDragged] = useState<BlockBoxSize | null>(null);
  const storedWidth = useKeptFrameShare(figureRef, block.content, "embed");
  const storedHeight = typeof block.content.height === "number" && block.content.height > 0 ? block.content.height : undefined;
  const width = dragged?.width ?? storedWidth, height = dragged?.height ?? storedHeight ?? DOCUMENT_EMBED_HEIGHT;
  const align = fileBlockAlignment(block.content.align, nested);
  return (
    <figure ref={figureRef} className={styles.embedBlock} contentEditable={false} data-managed-embed={kind} data-align={align}
      data-sized={width && width < 100 ? "true" : undefined} data-resizing={dragged ? "true" : undefined}
      style={width && width !== 100 ? frameShareStyle(width, "embed") : undefined}>
      <div ref={boxRef} className={`${styles.embedFrame} ${styles.webEmbedFrame}`} data-embed-frame="true" style={{ height }}>
        <PictureHandlePlace align={align} />
        {children}
        {!adapter.readOnly ? <>
          <ImageBlockTools adapter={adapter} block={block} mode="embed" align={align} />
          <BlockCornerHandles kind="embed" figureRef={figureRef} boxRef={boxRef} grips="edges" onSize={setDragged} onEnd={(size) => {
            setDragged(null);
            const patch = keptWidthPatch(size, block.content);
            if (size && (patch || size.height !== storedHeight)) adapter.updateBlock(block, { ...patch, height: size.height });
          }} />
        </> : null}
      </div>
    </figure>
  );
}
