import { type CSSProperties, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LeafenApiError, request } from "@/apiTransport";
import { BlockMoveToDialog } from "@/components/editor/BlockMoveToDialog";
import { movePopoverAnchorOf } from "@/components/movePopoverPlacement";
import { useProductPages } from "@/components/product/productPagesContext";
import { AddToPageIcon, Download, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { afterPageMade } from "@/lib/pagesNotYetMade";
import type { EmbedDocumentKind } from "@/lib/documentKinds";
import { isActiveServerOrigin, serverUrl } from "@/lib/serverOrigin";
import styles from "./AppFileOpener.module.css";
import { DocumentPreviewFailedCard } from "./DocumentPreviewFailedCard";
import { DocumentSheetTabs, useDocumentSheets } from "./DocumentSheetTabs";

// Leafen's own preview of a Word, Excel or PowerPoint file, as Notion's file
// preview, where no document app opens the format (2026-11-05, built-in
// document viewing): the server's preview page (internal/app/documentpreview)
// draws the bytes this window reads and posts to it, in a frame with no
// origin. The window lies over the page area as a document app's does, with
// Notion's "다운로드하기", "페이지에 추가" — the file at a chosen page's end, shown
// in that page (2026-11-06) — and close.

export type DocumentPreviewFile = {
  /**
   * A CSV file is shown as a sheet, as its embed's full screen (Notion's, seen
   * 2026-11-12); another sheet but an .xlsx file as a plain table, its sheets' tabs in the head.
   */
  name: string; kind: EmbedDocumentKind | "pdf";
  /** The file's address for this person, or its bytes read another way (a linked PDF's, through the server). */
  resolve?: () => Promise<string>;
  read?: () => Promise<ArrayBuffer>;
  /** None for a linked PDF, whose Notion preview has only close (seen 2026-11-06). */
  download?: () => void;
  /** Where the file's block is, for adding the file to another page. */
  pageId?: string; blockId?: string; workspaceId?: string;
};

const PREVIEW_MAX_BYTES = 100 * 1024 * 1024;
// The frame comes from the name the page was opened at when the page is the
// server's own, as the server lets only the name asked for frame it: one server
// opened by two names (127.0.0.1 and a LAN or tailnet name) shows it under
// either (2026-11-07: opened by another name it was refused). The frame has no
// origin (sandbox), so the page's own name gives it nothing. The apps open it
// at the server's address.
const DOCUMENT_PREVIEW_PATH = "/api/leafen/v1/document-preview/index.html";
const frameAddress = () => /^https?:$/u.test(window.location.protocol) && isActiveServerOrigin(window.location.origin)
  ? new URL(DOCUMENT_PREVIEW_PATH, window.location.origin).toString()
  : serverUrl(DOCUMENT_PREVIEW_PATH);

let previewing: DocumentPreviewFile | null = null;
let version = 0;
const listeners = new Set<() => void>();
const publish = () => {
  version += 1;
  for (const listener of listeners) listener();
};

export function openDocumentPreview(file: DocumentPreviewFile) {
  previewing = file;
  publish();
}

function closeDocumentPreview() {
  previewing = null;
  publish();
}

export function DocumentPreviewHost() {
  useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  }, () => version, () => 0);
  useEffect(() => closeDocumentPreview, []);
  return previewing ? <DocumentPreview key={version} file={previewing} onClose={closeDocumentPreview} /> : null;
}

function pageAreaLeft() {
  const main = document.querySelector("main");
  return main ? Math.max(0, Math.round(main.getBoundingClientRect().left)) : 0;
}

async function readFile(file: DocumentPreviewFile) {
  if (file.read) return file.read();
  if (!file.resolve) throw new Error("no file");
  const response = await fetch(await file.resolve());
  if (!response.ok) throw new Error(`file ${response.status}`);
  if (Number(response.headers.get("content-length")) > PREVIEW_MAX_BYTES) throw new Error("too large");
  const data = await response.arrayBuffer();
  if (data.byteLength > PREVIEW_MAX_BYTES) throw new Error("too large");
  return data;
}

function DocumentPreview({ file, onClose }: { file: DocumentPreviewFile; onClose: () => void }) {
  const { t } = useTranslation("blockItem");
  const root = useRef<HTMLDivElement>(null), close = useRef<HTMLButtonElement>(null);
  const [left, setLeft] = useState(pageAreaLeft);
  useEffect(() => {
    const follow = () => setLeft(pageAreaLeft());
    window.addEventListener("resize", follow);
    return () => window.removeEventListener("resize", follow);
  }, []);
  // Out of sight while the file goes to a page (AddFileToPageButton), the page behind it free.
  const [leaving, setLeaving] = useState(false);
  const { zIndex } = useOverlayLayer(!leaving, { kind: "dialog", ref: root, modalOwned: () => [root.current], restoreFocus: true, onEscape: onClose });
  useEffect(() => { close.current?.focus(); }, []);
  const sheets = useDocumentSheets();
  return createPortal(
    <div className={styles.backdrop} style={{ zIndex, left, visibility: leaving ? "hidden" : undefined }}>
      <div ref={root} className={styles.dialog} role="dialog" aria-modal="true" aria-label={t("file.previewNamed", { fileName: file.name })}
        data-document-preview={file.kind}>
        <header className={styles.header}>
          {/* Notion's preview head names nothing (2026-11-06); the window's label does. A plain-table sheet's tabs start it (2026-11-15). */}
          <DocumentSheetTabs names={sheets.names} at={sheets.at} onPick={sheets.pick} label={t("file.previewSheets")} />
          <span className={styles.spacer} />
          {file.download ? <button type="button" className={styles.download} onClick={file.download} data-document-preview-download>
            <Download aria-hidden="true" />{t("file.previewDownload")}</button> : null}
          {file.pageId && file.blockId && file.workspaceId
            ? <AddFileToPageButton pageId={file.pageId} blockId={file.blockId} workspaceId={file.workspaceId}
              onLeave={() => { setLeaving(true); return true; }} onAdded={onClose} onRefused={() => setLeaving(false)} /> : null}
          <button ref={close} type="button" className={styles.close} aria-label={t("common.close")} onClick={onClose}><X aria-hidden="true" /></button>
        </header>
        <DocumentPreviewFrame kind={file.kind} title={file.name} read={() => readFile(file)} className={styles.frame}
          failedClassName={styles.previewFailed} onEscape={onClose} sheet={sheets.at} onFrameMessage={sheets.heard} />
      </div>
    </div>,
    document.body,
  );
}

/**
 * The server's preview page in a frame with no origin, drawing the bytes
 * read posts to it once it listens; a document it can't draw says so. The
 * preview window, a page's document embed and a presentation use it.
 */
export function DocumentPreviewFrame({ kind, title, read, className, style, failedClassName, onEscape, present, sheet, onFrameMessage, embedded, onDownload }: {
  /** A CSV file's words are drawn as a sheet's (an embed of one). */
  kind: DocumentPreviewFile["kind"]; title: string; read: () => Promise<ArrayBuffer>;
  className?: string; style?: CSSProperties; failedClassName?: string;
  /** Escape pressed inside the frame, where the window's own keys don't hear it. */
  onEscape?: () => void;
  /** A PowerPoint file drawn a slide at a time to present, this one (from 0) in view. */
  present?: number;
  /** A plain-table sheet's sheet (from 0) in view, as its head's tabs pick (DocumentSheetTabs). */
  sheet?: number;
  /** The frame's other words: a presentation's slides, keys and pointer moves, a plain-table sheet's names (preview.js). */
  onFrameMessage?: (message: { type: string; count?: unknown; key?: unknown; names?: unknown }) => void;
  /** Drawn in a page's document embed, whose pages and slides stand as Notion's embed draws them (preview.js). */
  embedded?: boolean;
  /** Saves the file, from the card an embedded plain-table sheet shows when it can't be read. */
  onDownload?: () => void;
}) {
  const { t, i18n } = useTranslation("blockItem");
  const frame = useRef<HTMLIFrameElement>(null);
  const [failed, setFailed] = useState(false);
  const reader = useRef(read);
  reader.current = read;
  const escaped = useRef(onEscape);
  escaped.current = onEscape;
  const named = useRef(title);
  named.current = title;
  const presenting = useRef(present !== undefined);
  const embedding = useRef(embedded === true);
  // The frame's own words (its zoom bar's, a sheet's), each in the person's
  // language where Leafen has it, else in English (preview.js).
  const words = useRef<Record<string, string>>({});
  words.current = {
    fit: t("file.previewFit"), reset: t("file.previewResetZoom"), zoom: t("file.previewZoom"),
    zoomIn: t("file.previewZoomIn"), zoomOut: t("file.previewZoomOut"), pickCell: t("file.previewPickCell"),
    sheetsLeft: t("file.previewSheetsLeft"), sheetsRight: t("file.previewSheetsRight"),
    unreadable: t("file.previewSheetUnreadable"), unreadableWhy: t("file.previewSheetUnreadableWhy"), noSlides: t("file.previewNoSlides"),
  };
  const heard = useRef(onFrameMessage);
  heard.current = onFrameMessage;
  useEffect(() => {
    if (present !== undefined) frame.current?.contentWindow?.postMessage({ type: "leafen:document-preview:slide", at: present }, "*");
  }, [present]);
  useEffect(() => {
    if (sheet !== undefined) frame.current?.contentWindow?.postMessage({ type: "leafen:document-preview:sheet", at: sheet }, "*");
  }, [sheet]);
  useEffect(() => {
    let live = true;
    setFailed(false);
    const reading = reader.current();
    reading.catch(() => { if (live) setFailed(true); });
    const answer = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow || typeof event.data?.type !== "string") return;
      if (event.data.type === "leafen:document-preview:failed") setFailed(true);
      if (event.data.type === "leafen:document-preview:escape") escaped.current?.();
      heard.current?.(event.data);
      if (event.data.type !== "leafen:document-preview:ready") return;
      void reading.then((data) => {
        if (!live) return;
        const theme = document.documentElement.dataset.theme === "dark" ? "dark" : "light";
        frame.current?.contentWindow?.postMessage({ type: "leafen:document-preview", ext: kind, data, language: i18n.language, words: words.current,
          theme, name: named.current, present: presenting.current, embed: embedding.current }, "*", [data]);
      }, () => undefined);
    };
    window.addEventListener("message", answer);
    return () => { live = false; window.removeEventListener("message", answer); };
  }, [kind, i18n.language]);
  // One that will not draw keeps the frame's size (a block's document embed, ManagedEmbedBlock), and says so
  // as Notion's engine for its kind does (seen 2026-11-15): a plain-table sheet's embed Notion's card
  // (DocumentPreviewFailedCard), a document Notion draws through a PDF that one line, as its PDF view's.
  if (failed) return embedded && kind === "sheet" ? <DocumentPreviewFailedCard style={style} onDownload={onDownload} />
    : <p className={failedClassName} style={style} role="status">{t("file.previewFailed")}</p>;
  return <iframe ref={frame} className={className} style={style} src={frameAddress()} title={title} sandbox="allow-scripts" referrerPolicy="no-referrer" />;
}

/**
 * Notion's "⊞ 페이지에 추가" in a file window's head: the person picks a page
 * they can edit (BlockMoveToDialog leaves out the rest) and the file goes to
 * its end as an embed that shows it there (2026-11-06). The window leaves
 * sight at once, as Notion's preview closes (user decision 2026-11-12: a
 * press answers on the next frame), and closes once the server has added it,
 * a new private page made here first included (pagesNotYetMade); refused, it
 * comes back with why, so the person is where they were.
 */
export function AddFileToPageButton({ pageId, blockId, workspaceId, onLeave, onAdded, onRefused }: {
  pageId: string; blockId: string; workspaceId: string;
  /** Takes the window out of sight; false when it stays (changes the person chose to keep). */
  onLeave: () => Promise<boolean> | boolean;
  /** The file is on the page: the window closes. */
  onAdded: () => void;
  /** The server refused it: the window shows again. */
  onRefused: () => void;
}) {
  const { t } = useTranslation("blockItem");
  const product = useProductPages();
  const button = useRef<HTMLButtonElement>(null);
  const [adding, setAdding] = useState(false);
  const session = product?.session;
  if (!session || !product) return null;
  return <>
    <button ref={button} type="button" className={styles.download} onClick={() => setAdding(true)} aria-expanded={adding}
      data-file-add-to-page>
      <AddToPageIcon aria-hidden="true" />{t("file.previewAddToPage")}</button>
    {adding ? <BlockMoveToDialog
      blockId={blockId} sourcePageId={pageId} workspaceId={workspaceId} pages={product.pages} session={session}
      title={t("file.previewAddToPage")} placeholder={t("file.previewAddToPageSearch")} sourcePage="always"
      place={{ current: t("file.previewCurrentPage"), none: t("file.previewNoPages"), create: t("file.previewNewPrivatePage") }}
      doneMessage={null} failedMessage={t("file.previewAddFailed")} madeHere
      anchor={movePopoverAnchorOf(button.current)}
      onChooseDestination={async (destinationPageId) => {
        const added = afterPageMade(destinationPageId).then(() => request(`/pages/${encodeURIComponent(pageId)}/blocks/${encodeURIComponent(blockId)}/add-to-page`,
          { method: "POST", body: JSON.stringify({ destinationPageId }) }, session));
        return () => void Promise.resolve(onLeave()).then((left) => added.then(() => {
          product.onStatus?.(t("file.previewAdded"), "success");
          if (left) onAdded();
        }, (error: unknown) => {
          if (left) { onRefused(); requestAnimationFrame(() => button.current?.focus({ preventScroll: true })); }
          product.onStatus?.(t(addRefusedForAccess(error) ? "file.previewAddNoAccess" : "file.previewAddFailed"), "error");
        }));
      }}
      onClose={() => setAdding(false)}
      onPageChange={() => { void product.refreshPages?.(); }}
      onStatus={product.onStatus} /> : null}
  </>;
}

/** Whether the server refused the file because the person can't edit the page. */
function addRefusedForAccess(error: unknown) {
  return error instanceof LeafenApiError && (error.status === 403 || error.code === "LEAFEN_PERMISSION_DENIED");
}
