import { useContext, useRef, type ReactNode, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphBlock } from "@/contentGraph";
import { copyImageTold, imagePng } from "@/lib/copyImage";
import { copyText } from "@/lib/copyText";
import type { BlockMenuImage, BlockMenuImageFields } from "./BlockActionMenu";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { safeMediaUrl, useMediaBlockUrl } from "./mediaBlockUrl";
import { MediaReplacementDialog } from "./MediaReplacementDialog";
import { useBlockFileActions } from "./useBlockFileActions";
import { fileBlockTurnChoices, fileBlockAlignment } from "./blockFileMenuModel";
import { ImageCropUploadDialog } from "./ImageCropUploadDialog";
import { ImageLinkWindow } from "./ImageLinkWindow";
import { ImageAltTextWindow } from "./ImageAltText";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { NativeTiptapReactContext } from "./nativeTiptapReactContext";
import { BlockImagePreview } from "./BlockImagePreview";
import { imageMenuFits } from "./imagePlacement";

type Notify = (message: string, tone?: "default" | "success" | "error") => void;

/**
 * The image actions of an image block's menu: replace the file, copy the
 * image, download it, give it a link or alt text, see it full screen, view
 * the original, open or copy its links, align it where it offers that. It renders the full-screen
 * view, the file picker the replace action uses and the link window
 * (ImageLinkWindow), which stands by the block `blockElement` gives.
 */
export function useBlockImageMenu(
  adapter: NativeEditorGraphAdapter,
  block: GraphBlock,
  onDone: () => void,
  notify: Notify,
  blockElement?: () => Element | null,
): { image?: BlockMenuImage; emptyImage?: BlockMenuImageFields; overlays: ReactNode; openLink: () => void } {
  const { t } = useTranslation(["blockHandle", "blockItem"]);
  const { managed, url } = useMediaBlockUrl(block);
  const fileActions = useBlockFileActions(block, adapter.session);
  // What the menu changes is one step of the page's history, as Notion's (seen 2026-11-06).
  const run = useContext(NativeTiptapReactContext)?.controller.run ?? ((change: () => void) => change());
  const [fullScreen, setFullScreen] = useState(false);
  const [cropping, setCropping] = useState(false);
  const [cropSource, setCropSource] = useState("");
  const [replacing, setReplacing] = useState(false);
  const [linking, setLinking] = useState<Element | null>(null);
  const [altAt, setAltAt] = useState<PopoverRect | null>(null);
  // Another file's or block's. Not on waking: the tool that woke this sleeping
  // handle has just opened one (2026-11-05: the first press of full screen did nothing).
  const shownFile = `${block.id}/${block.type}/${String(block.content.fileId ?? "")}/${String(block.content.url ?? "")}/${adapter.readOnly}`;
  const lastShownFile = useRef(shownFile);
  useEffect(() => {
    if (lastShownFile.current === shownFile) return;
    lastShownFile.current = shownFile;
    setFullScreen(false); setCropping(false); setReplacing(false); setLinking(null); setAltAt(null);
  }, [shownFile]);
  const openLink = () => {
    const at = blockElement?.();
    if (at && !adapter.readOnly && block.type === "image") setLinking(at);
  };
  if (block.type !== "image") return { overlays: null, openLink };

  const content = block.content;
  const alt = String(content.altText ?? content.alt ?? "");
  const link = typeof content.imageLink === "string" ? safeMediaUrl(content.imageLink) : "";
  const update = (next: Record<string, unknown>) => {
    const current = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
    if (current) run(() => adapter.updateBlock(current, next));
  };
  const open = (target: string) => {
    if (target) window.open(target, "_blank", "noopener,noreferrer");
    onDone();
  };

  // A page of this server is kept by its path, as a text's link to it is; any other address as the picture's own.
  const saveLink = (value: string) => {
    update({ imageLink: value.startsWith("/") ? value : safeMediaUrl(value) || undefined });
    setLinking(null);
  };
  // From a menu the window opens once the menu has gone, the focus back where the menu found it.
  const editLink = () => {
    onDone();
    window.requestAnimationFrame(openLink);
  };
  const saveAltText = (text: string) => update({ altText: text.trim() || undefined });
  // From "추가 옵션" the alt text window opens where that list stood, as Notion's
  // (seen 2026-11-12), once the menu has gone; without it, at the block's start.
  const editAltText = (from?: PopoverRect) => {
    onDone();
    window.requestAnimationFrame(() => {
      const box = from ?? blockElement?.()?.getBoundingClientRect();
      if (box && !adapter.readOnly) setAltAt({ top: box.top, bottom: box.top, left: box.left, right: box.left });
    });
  };
  // An uploaded image is one before its address comes: a handle woken by a
  // tool's press asks at once, and the press went nowhere (2026-11-05: full
  // screen opened only on the second press). Its view opens once the address is in.
  const shown = Boolean(url || (managed && content.fileId));
  const image: BlockMenuImage | undefined = shown ? {
    managed,
    link,
    altText: alt,
    turnChoices: fileBlockTurnChoices(block),
    // "맞춤" where the picture drawn offers it in its menu (imageMenuFits): sized, or uploaded in the page, narrower than its column.
    ...(imageMenuFits(blockElement?.()) ? { align: { active: fileBlockAlignment(content.align), onSelect: (value: "left" | "center" | "right") => { update({ align: value }); onDone(); } } } : {}),
    onDownload: () => {
      fileActions.download();
      onDone();
    },
    onViewOriginal: () => { fileActions.open(); onDone(); },
    ...(managed ? { onCrop: () => { if (!adapter.readOnly) { setCropSource(String(block.content.fileId ?? block.content.url ?? "")); setCropping(true); } onDone(); } } : {}),
    // Started at the press, as WebKit copies only what a press starts (lib/copyImage.ts).
    onCopyImage: () => {
      const picture = imagePng(fileActions.resolve().then(url => fileActions.isCurrent() ? url : ""));
      void copyImageTold(picture, { progress: true, still: fileActions.isCurrent });
      onDone();
    },
    onOpenLink: () => open(link),
    // Said as every block's "원본 링크 복사" is, Notion's "링크 복사 완료" (seen 2026-11-11 on an embed and a bookmark).
    onCopySourceLink: () => {
      void (url ? Promise.resolve(url) : fileActions.resolve()).then(copyText).catch(() => false).then((copied) => notify(
        copied ? t("selectionToolbar:linkEditor.copiedLink") : t("blockHandle:notifications.couldNotCopyLink"),
        copied ? "success" : "error",
      ));
      onDone();
    },
    onFullScreen: () => {
      setFullScreen(true);
      onDone();
    },
    onEditLink: editLink,
    onEditAltText: editAltText,
    onReplace: () => {
      if (!adapter.readOnly) setReplacing(true);
      onDone();
    },
  } : undefined;

  const overlays = (
    <>
      {replacing && !adapter.readOnly ? <MediaReplacementDialog adapter={adapter} block={block} onClose={() => setReplacing(false)} /> : null}
      {fullScreen && url ? <BlockImagePreview block={block} url={url} onClose={() => setFullScreen(false)}
        onOpen={fileActions.open} onDownload={fileActions.download} /> : null}
      {cropping && !adapter.readOnly && cropSource === String(content.fileId ?? content.url ?? "") ? <ImageCropUploadDialog
        adapter={adapter} block={block} src={url} resolveUrl={fileActions.resolve} onClose={() => setCropping(false)} /> : null}
      {altAt && !adapter.readOnly ? <ImageAltTextWindow anchor={{ at: altAt }} alt={alt} onSave={saveAltText} onClose={() => setAltAt(null)} /> : null}
      {linking && !adapter.readOnly ? <ImageLinkWindow anchor={linking} link={link} onSave={saveLink} onClose={() => setLinking(null)}
        source={adapter.session ? { session: adapter.session, workspaceId: adapter.workspaceId } : undefined} /> : null}
      {fileActions.error ? <span role="alert">{t(fileActions.blocked ? "blockItem:file.downloadBlocked" : "blockItem:file.loadFailed")}</span> : null}
    </>
  );
  const emptyImage = shown ? undefined : { link, onEditLink: editLink };
  return { image, emptyImage, overlays, openLink };
}
