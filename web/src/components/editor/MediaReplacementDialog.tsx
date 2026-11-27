import { useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphBlock } from "@/contentGraph";
import { fileNameFromUrl } from "../database/files";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { isSandboxedHtmlFile, normalizedFileContentType } from "@/lib/fileSecurity";
import { sandboxedHtmlContentType } from "@/fileSecurityPolicy.generated";
import { BlockEmptyLinkPane, BlockEmptyUploadPane } from "./BlockEmptyPopover";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { useMediaBlockUpload } from "./mediaBlockUpload";
import { givenWebLink } from "./mediaBlockUrl";
import { emptyText } from "./MediaUploadForm";
import { blockItemText } from "./blockItemLabels";
import { NativeTiptapReactContext } from "./nativeTiptapReactContext";
import { readBookmarkMetadata, UNREAD_BOOKMARK } from "./bookmarkMetadata";
import { EMBED_DEFAULT_HEIGHT, ownBlockService, serviceWindowText } from "./embedServices";
import styles from "./BlockEmptyPopover.module.css";
import { focusOpenedField } from "./openedFieldCaret";

const PRESENTATION = ["caption", "captionVisible", "width", "sizedWidth", "align", "altText", "imageLink", "color"] as const;
function externalReplacementContent(block: GraphBlock, url: string) {
  const content: Record<string, unknown> = Object.fromEntries(PRESENTATION.filter(key => block.content[key] !== undefined)
    .map(key => [key, block.content[key]]));
  // A crop belongs to its original image; the replacement starts uncut.
  content.url = url;
  // A service Notion keeps a block of its own stays that service's (EmbedService.ownBlock), its
  // height with it; any other embed's new link frames at a plain embed's height (embedDefaultHeight).
  if (block.type === "embed" && ownBlockService(block) && block.content.service) content.service = block.content.service;
  else if (block.type === "embed") content.height = EMBED_DEFAULT_HEIGHT;
  if (block.type === "file") { content.source = "external"; content.name = Array.from(fileNameFromUrl(url)).slice(0, 255).join(""); }
  return content;
}

/**
 * Notion's window for changing a block's file or a bookmark's address (seen
 * 2026-11-05, 2026-11-06), the one an empty block opens, under the block.
 * The current file stays until the new one has come: a wrong type or a
 * failed upload leaves it, and the window comes back with what went wrong.
 */
export function MediaReplacementDialog({ adapter, block, onClose }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; onClose(): void;
}) {
  const { t } = useTranslation(["blockHandle"]);
  // A new link is one step of the page's history, ⌘Z bringing the old one back, as Notion's (seen 2026-11-06).
  const run = useContext(NativeTiptapReactContext)?.controller.run ?? ((change: () => void) => change());
  const root = useRef<HTMLDivElement>(null);
  const before = useRef(String(block.content.fileId ?? block.content.url ?? ""));
  // A linked embed's opens on its link, the tab first; a picture's, video's or
  // file's on its upload even when linked, as Notion's (seen 2026-11-06).
  const linkFirst = !block.content.fileId && Boolean(block.content.url) && block.type === "embed";
  const kind = block.type === "image" || block.type === "video" || block.type === "audio" || block.type === "pdf" ? block.type : "file";
  const html = block.type === "embed" && normalizedFileContentType(String(block.content.contentType ?? "")) === sandboxedHtmlContentType;
  const bookmark = block.type === "bookmark";
  const { upload, progress, error, setError } = useMediaBlockUpload(adapter, block, kind);
  const words = emptyText(kind);
  // A service's own block opens its own window, its link alone, as Notion's
  // (seen 2026-11-06: a map's and a form's); a plain embed the embed window,
  // and a bookmark its link window alone, the one its Enter opens.
  const service = block.type === "embed" ? ownBlockService(block) : undefined;
  const own = service ? serviceWindowText(service) : undefined;
  const text = own ? { ...words, ...own } : bookmark ? { ...words, placeholder: blockItemText("blockEmpty.bookmark.placeholder"),
    submit: blockItemText("blockEmpty.bookmark.submit"), hint: blockItemText("blockEmpty.bookmark.hint") }
    : block.type === "embed" ? { ...words, placeholder: blockItemText("blockEmpty.bookmark.placeholder"),
      submit: blockItemText("blockEmpty.embed.submit") } : words;
  const tabs: readonly ("upload" | "link")[] = own || bookmark ? [] : linkFirst && !html ? ["link", "upload"] : html ? ["upload"] : ["upload", "link"];
  const first = tabs[0] ?? "link";
  const [tab, setTab] = useState<"upload" | "link">(first);
  const open = !progress;
  const { zIndex } = useOverlayLayer(open, { kind: "picker", ref: root, onEscape: onClose, onOutsidePress: onClose });
  // Centered under what the block shows (a file's whole row, a bookmark's
  // card, seen 2026-11-06) and its caption when one shows (a Miro board's,
  // seen 2026-11-06: under the caption, not over it), over them when only
  // that side has room, and whole in the window: with room on neither side
  // of a tall block, under it moved up into view (PopoverOptions.whole).
  const place = usePopoverPlacement(root, open ? () => {
    const row = document.querySelector(`[data-block-id="${CSS.escape(block.id)}"]`);
    const frame = (block.type === "file" ? null : row?.querySelector("figure, [data-block-empty], [data-bookmark-card]")) ?? row;
    const shown = popoverRectOf(frame) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    const caption = row?.querySelector("[data-native-caption]");
    const captionBox = caption && frame && !frame.contains(caption) ? caption.getBoundingClientRect() : null;
    const anchor = captionBox && captionBox.height > 0 && captionBox.bottom > shown.bottom
      ? { top: shown.top, bottom: captionBox.bottom, left: shown.left, right: shown.right, contextElement: frame ?? undefined } : shown;
    return { anchor, placement: "bottom", gap: 0, options: { whole: true } };
  } : null, [block.id, tab], undefined, false);
  // The file on its way shows in the block, whose job it now is; the window is done.
  useEffect(() => { if (progress) onClose(); }, [progress, onClose]);
  // After the menu that opened it has given focus back, what is typed from the press going in (focusOpenedField).
  useEffect(() => {
    if (!open) return undefined;
    return focusOpenedField(() => root.current?.querySelector<HTMLElement>(first === "link" ? "[data-block-empty-link]" : "[data-block-empty-tab]"),
      { scroll: false, from: 2 });
  }, [open]);
  useEffect(() => {
    if (adapter.readOnly || String(block.content.fileId ?? block.content.url ?? "") !== before.current) onClose();
  }, [adapter.readOnly, block.content.fileId, block.content.url, onClose]);
  function saveLink(value: string) {
    const url = givenWebLink(value);
    if (!url) return blockItemText("file.invalidLink");
    const current = adapter.graph.blocks.find(candidate => candidate.id === block.id);
    if (bookmark) {
      // The new page is read first, so its address and card are one step ⌘Z takes back.
      onClose();
      void readBookmarkMetadata(adapter, url).then((found) => {
        const live = adapter.graph.blocks.find(candidate => candidate.id === block.id);
        if (!live || adapter.readOnly || !adapter.isCurrent() || adapter.signal.aborted) return;
        run(() => adapter.updateBlock(live, { url, ...(found ?? UNREAD_BOOKMARK) }));
      });
      return "";
    }
    if (current && !adapter.readOnly) run(() => adapter.replaceBlocks([{ ...current, content: externalReplacementContent(current, url) }]));
    onClose();
    return "";
  }
  if (!open) return null;
  // A picture's or video's window shows its own kind of files; the others any file (Notion's, seen 2026-11-07).
  const accept = html ? ".html,.htm,text/html" : kind === "image" || kind === "video" ? `${kind}/*` : "";
  return createPortal(<div ref={root} className={styles.popover} role="dialog" aria-label={t("blockHandle:media.replace")} data-media-replacement
    style={{ ...popoverPlacementStyle(place), zIndex, "--block-empty-width": kind === "image" ? "540px" : "300px" } as never}>
    {tabs.length ? <div className={styles.tabs} role="tablist">
      {tabs.map(id => (
        <button key={id} type="button" role="tab" className={styles.tab} aria-selected={tab === id} data-block-empty-tab={id}
          onClick={() => {
            setTab(id);
            // The link tab puts the caret in its field, as the empty block's window and Notion's do (seen 2026-11-06;
            // measured once 2026-11-15 in Chrome 1440 with no script before the press: a letter went into the field).
            if (id === "link") focusOpenedField(() => root.current?.querySelector<HTMLElement>("[data-block-empty-link]"), { scroll: false });
          }}>{blockItemText(id === "upload" ? "blockEmpty.upload" : "blockEmpty.link")}</button>
      ))}
    </div> : null}
    {tab === "upload" && !own ? <BlockEmptyUploadPane accept={accept} choose={text.choose} progress={progress} outlined={kind === "image"}
      kind={html ? undefined : kind}
      onFile={(chosen) => {
        if (html && chosen && !isSandboxedHtmlFile(chosen)) { setError(blockItemText("htmlEmbed.wrongType")); return; }
        upload(chosen);
      }} /> : <BlockEmptyLinkPane placeholder={text.placeholder} submit={text.submit} hint={text.hint}
        onLink={saveLink} />}
    {error ? <div className={styles.error} role="alert">{error}</div> : null}
  </div>, document.body);
}
