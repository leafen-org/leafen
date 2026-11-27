import { useTranslation } from "react-i18next";
import { ImagePreviewDialog } from "@/components/ImagePreviewDialog";
import type { GraphBlock } from "@/contentGraph";
import type { Session } from "@/types";
import { useBlockFileActions } from "./useBlockFileActions";

/**
 * An image block's picture full screen, its file opened or downloaded from
 * the bar: the block menu's and tools' "전체 화면" (useBlockImageMenu), and a
 * single press on the picture where the page can't be edited (MediaBlockContent).
 */
export function BlockImagePreview({ block, url, onClose, onOpen, onDownload }: {
  block: GraphBlock; url: string; onClose(): void; onOpen(): void; onDownload(): void;
}) {
  const { t } = useTranslation(["blockItem"]);
  const name = String(block.content.name ?? block.content.fileName ?? "image");
  const alt = String(block.content.altText ?? block.content.alt ?? "");
  return <ImagePreviewDialog
    src={url}
    alt={alt || name}
    label={t("blockItem:image.preview")}
    closeLabel={t("blockItem:image.closePreview")}
    openLabel={t("blockItem:file.openNamed", { fileName: name })}
    openText={t("blockItem:common.open")}
    downloadLabel={t("blockItem:file.downloadNamed", { fileName: name })}
    downloadText={t("blockItem:common.download")}
    toolbarLabel={t("blockItem:image.previewControls")}
    zoomInLabel={t("blockItem:image.zoomIn")}
    zoomOutLabel={t("blockItem:image.zoomOut")}
    zoomLevelLabel={t("blockItem:image.zoomLevel")}
    onClose={onClose}
    onOpen={onOpen}
    onDownload={onDownload}
  />;
}

/** The preview a single press on a picture opens where the page can't be edited, with its own file actions. */
export function ReaderImagePreview({ block, session, url, onClose }: {
  block: GraphBlock; session: Session | null; url: string; onClose(): void;
}) {
  const { t } = useTranslation(["blockItem"]);
  const fileActions = useBlockFileActions(block, session);
  return <>
    <BlockImagePreview block={block} url={url} onClose={onClose} onOpen={fileActions.open} onDownload={fileActions.download} />
    {fileActions.error ? <span role="alert">{t(fileActions.blocked ? "blockItem:file.downloadBlocked" : "blockItem:file.loadFailed")}</span> : null}
  </>;
}
