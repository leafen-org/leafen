"use client";

import { type MouseEvent, type ReactNode, useCallback, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { savesOnDevice, triggerBrowserLinkDownload } from "@/lib/browserFileDownload";
import { openPdfInNewTab } from "@/lib/pdfPreview";
import { isActiveServerOrigin } from "@/lib/serverOrigin";
import type { FileAttachment } from "@/lib/types";
import { ImagePreviewDialog } from "../ImagePreviewDialog";
import { isPdfAttachment, isPreviewableImageAttachment } from "./files";

export function AttachmentOpenLink({
  file,
  href,
  className,
  ariaLabel,
  children,
}: {
  file: FileAttachment;
  href: string;
  className: string;
  ariaLabel: string;
  children: ReactNode;
}) {
  const { t } = useTranslation("blockItem");
  const [previewOpen, setPreviewOpen] = useState(false);
  const [pdfViewerReady, setPdfViewerReady] = useState(false);
  const restorePreviewFocusRef = useRef(true);
  const image = isPreviewableImageAttachment(file);
  const pdf = isPdfAttachment(file);
  const closePreview = useCallback(() => setPreviewOpen(false), []);

  if (!href) return <span className={className}>{children}</span>;

  function onClick(event: MouseEvent<HTMLAnchorElement>) {
    event.stopPropagation();
    if (image) {
      event.preventDefault();
      restorePreviewFocusRef.current = event.detail === 0;
      setPreviewOpen(true);
      return;
    }
    if (pdf) {
      event.preventDefault();
      setPdfViewerReady(false);
      void openPdfInNewTab(() => href, file.name, () => setPdfViewerReady(true));
      return;
    }
    // Any other file opens at its address in a new tab, as Notion's files
    // property does (seen 2026-11-15 twice: a .txt's name in a cell's list
    // opened its file in a new tab, the text shown): the browser shows what
    // it can (a text file, product_file_download_capability.go) and saves
    // the rest. An app's window, which opens no tab, saves a file of
    // Leafen's own on the device (lib/browserFileDownload); another site's
    // link opens there.
    if (savesOnDevice() && (!/^https?:\/\//iu.test(href) || isActiveServerOrigin(new URL(href).origin))) {
      event.preventDefault();
      void triggerBrowserLinkDownload(href, file.name);
    }
  }

  return (
    <>
      <a
        className={className}
        href={href}
        target={image ? undefined : "_blank"}
        rel="noreferrer noopener"
        aria-label={ariaLabel}
        aria-haspopup={image ? "dialog" : undefined}
        aria-expanded={image ? previewOpen : undefined}
        data-attachment-open
        data-attachment-kind={image ? "image" : pdf ? "pdf" : "file"}
        data-pdf-viewer-ready={pdf && pdfViewerReady ? "true" : undefined}
        onClick={onClick}
        onAuxClick={(event) => event.stopPropagation()}
      >
        {children}
      </a>
      {previewOpen && (
        <ImagePreviewDialog
          src={href}
          alt={file.name}
          label={t("image.preview")}
          closeLabel={t("image.closePreview")}
          openLabel={t("file.openNamed", { fileName: file.name })}
          openText={t("common.open")}
          downloadLabel={t("file.downloadNamed", { fileName: file.name })}
          downloadText={t("common.download")}
          toolbarLabel={t("image.previewControls")}
          zoomInLabel={t("image.zoomIn")}
          zoomOutLabel={t("image.zoomOut")}
          zoomLevelLabel={t("image.zoomLevel")}
          restoreFocusOnClose={restorePreviewFocusRef.current}
          onClose={closePreview}
        />
      )}
    </>
  );
}
