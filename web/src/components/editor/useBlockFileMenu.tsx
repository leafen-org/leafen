import { useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphBlock } from "@/contentGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { blockItemText } from "./blockItemLabels";
import { NativeTiptapReactContext } from "./nativeTiptapReactContext";
import { embedOwnService } from "./embedServices";
import { useBlockFileActions } from "./useBlockFileActions";
import { safeMediaUrl } from "./mediaBlockUrl";
import { MediaReplacementDialog } from "./MediaReplacementDialog";
import { embedBoxWidth, FILE_MENU_TYPES, fileBlockTurnChoices, fileBlockAlignment } from "./blockFileMenuModel";
import { BlockMediaPreviewDialog } from "./BlockMediaPreviewDialog";
import type { BlockTurnChoice } from "./blockTurnInto";
import renameStyles from "@/components/page/PageRenamePopover.module.css";
import { normalizedFileContentType } from "@/lib/fileSecurity";
import { sandboxedHtmlContentType } from "@/fileSecurityPolicy.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { fileOpenersFor } from "@/components/apps/AppFileOpener";
import { openAppFile } from "@/components/apps/appFileOpening";
import { blockAppFile } from "./useBlockDocumentApp";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { useProductPages } from "@/components/product/productPagesContext";
import { documentKindOf, embedDocumentKind } from "@/lib/documentKinds";
import { libraryHref, pushRoute } from "@/lib/router";
import { copyText } from "@/lib/copyText";
import { openDocumentPreview } from "@/components/apps/DocumentPreview";
import { readPdfBytes } from "@/lib/pdfPreview";
import { createUrlMetadataApi } from "@/urlMetadataApi";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { keptBlockWidth, keptSized } from "./frameKeptWidth";

export type BlockMenuFile = {
  turnChoices: BlockTurnChoice[]; align?: { active: "left" | "center" | "right"; onSelect(value: "left" | "center" | "right"): void };
  onReplace(): void; onDownload(): void; onViewOriginal(): void; onFullScreen?: () => void; onRename?: () => void;
  /** The apps that open this kind of file, by name, with their app's (appName) the menu's search finds them by too. */
  openWith?: { id: string; name: string; appName: string; run(): void }[];
  /** Opens the library's documents, for a document's block. */
  onAllDocuments?: () => void;
  /** A block showing a link rather than an uploaded file copies the link ("원본 링크 복사"). */
  onCopySource?: () => void;
  /** The menu's heading where it is not the block type's: a service's own block's name ("Google Maps"). */
  label?: string;
  /** A file row's original and rename stand under "추가 옵션" whether or not it turns into another block, and its caption too where it does (fileCaptionInMore). */
  groupMore?: boolean;
};

const RENAME_WIDTH = 380;

/**
 * A file's rename box, as Notion's (seen 2026-11-06): the 380×36 box its
 * pages rename in, centered under the block's handle 4px down, the name
 * chosen whole; Enter, Escape or a press outside keeps what was typed.
 * Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: a linked file's ⋮⋮ › 이름 바꾸기,
 * the name with its extension chosen whole and "z" typed over it, kept by Escape (Leafen by code, not seen).
 */
function FileNameBox({ name, at, onSave, onClose }: {
  name: string; at: { top: number; left: number }; onSave(name: string): void; onClose(): void;
}) {
  const { t } = useTranslation(["blockHandle", "common"]);
  const input = useRef<HTMLInputElement>(null);
  const [value, setValue] = useState(name);
  useEffect(() => { input.current?.focus(); input.current?.select(); }, []);
  const close = () => {
    const next = value.replace(/[\r\n]/gu, "").trim();
    if (next && next !== name) onSave(Array.from(next).slice(0, 255).join(""));
    onClose();
  };
  return createPortal(<>
    <button type="button" className={renameStyles.backdrop} tabIndex={-1} aria-label={t("blockHandle:media.rename")} onClick={close} />
    <div className={renameStyles.popover} role="dialog" aria-label={t("blockHandle:media.rename")} data-file-rename style={at}>
      <input ref={input} className={renameStyles.title} value={value} placeholder={t("common:labels.untitled")}
        aria-label={t("blockHandle:media.rename")} spellCheck={false} onChange={event => setValue(event.currentTarget.value)}
        onKeyDown={event => {
          if (imeComposing(event) || (!fieldEnter(event) && event.key !== "Escape")) return;
          event.preventDefault(); event.stopPropagation(); close();
        }} />
    </div>
  </>, document.body);
}

/** Centered under the handle, 4px down, kept inside the window (Notion's, seen 2026-11-06). */
function renameBoxUnder(handle: Element | null | undefined, row: Element | null | undefined) {
  const rect = (handle ?? row)?.getBoundingClientRect();
  if (!rect) return { top: 16, left: 12 };
  const left = Math.round(rect.left + rect.width / 2 - RENAME_WIDTH / 2);
  return { top: Math.min(Math.round(rect.bottom + 4), window.innerHeight - 48),
    left: Math.max(12, Math.min(left, window.innerWidth - RENAME_WIDTH - 12)) };
}

/**
 * A PDF block's file in Leafen's preview: an uploaded one's address, or a
 * link's bytes read through the server from the address the block itself
 * shows (safeMediaUrl: one kept without its scheme over https, as a text
 * link opens it).
 */
export function openPdfPreview(adapter: NativeEditorGraphAdapter, block: GraphBlock, actions: ReturnType<typeof useBlockFileActions>) {
  const session = adapter.session;
  if (!session) return;
  const name = String(block.content.name ?? "PDF");
  if (typeof block.content.fileId === "string") {
    openDocumentPreview({ name, kind: "pdf", resolve: actions.resolve, download: actions.download,
      pageId: adapter.pageId, blockId: block.id, workspaceId: adapter.workspaceId });
    return;
  }
  const url = safeMediaUrl(block.content.url);
  openDocumentPreview({ name, kind: "pdf",
    read: async () => readPdfBytes(await createUrlMetadataApi().readLinkedPdf(session, url)) });
}

export function useBlockFileMenu(adapter: NativeEditorGraphAdapter, block: GraphBlock, onDone: () => void,
  notify?: (message: string, tone?: "default" | "success" | "error") => void, handle?: () => Element | null): {
  file?: BlockMenuFile; overlays: ReactNode;
  /** A bookmark's link window, which its Enter opens while it is chosen (Notion's, seen 2026-11-06). */
  relink?: () => void;
} {
  const actions = useBlockFileActions(block, adapter.session);
  const { phone, touch } = useSidebarLayout(true);
  // What the menu changes is one step of the page's history, as Notion's (seen 2026-11-06).
  const editor = useContext(NativeTiptapReactContext);
  const run = editor?.controller.run ?? ((change: () => void) => change());
  const [replacing, setReplacing] = useState(false);
  const [fullScreen, setFullScreen] = useState(false), [rename, setRename] = useState<{ top: number; left: number } | null>(null);
  const live = adapter.graph.blocks.find(candidate => candidate.id === block.id);
  const source = String(block.content.fileId ?? block.content.url ?? "");
  const html = normalizedFileContentType(String(block.content.contentType ?? "")) === sandboxedHtmlContentType;
  const [previewSource, setPreviewSource] = useState("");
  const { i18n } = useTranslation();
  const productPages = useProductPages();
  const usable = useUsableAppList(adapter.session, FILE_MENU_TYPES.has(block.type) && Boolean(block.content.fileId));
  const openers = typeof block.content.fileId === "string"
    ? fileOpenersFor(usable.apps, String(block.content.name ?? ""), String(block.content.contentType ?? ""), productPages?.organizationId) : [];
  // Another file's or block's, not on waking: the tool that woke this sleeping handle has just opened one.
  const shownFile = `${block.id}/${block.type}/${source}/${adapter.readOnly}`;
  const lastShownFile = useRef(shownFile);
  useEffect(() => {
    if (lastShownFile.current === shownFile) return;
    lastShownFile.current = shownFile;
    setFullScreen(false); setRename(null); setReplacing(false);
  }, [shownFile]);
  const replacement = replacing && live && !adapter.readOnly
    ? <MediaReplacementDialog adapter={adapter} block={live} onClose={() => setReplacing(false)} /> : null;
  if (block.type === "bookmark" && source) return { overlays: replacement, relink: () => { if (!adapter.readOnly) setReplacing(true); } };
  if (!FILE_MENU_TYPES.has(block.type) || block.type === "image" || !actions.available) return { overlays: null };
  const update = (next: Record<string, unknown>) => {
    const current = adapter.graph.blocks.find(candidate => candidate.id === block.id);
    if (current && !adapter.readOnly) run(() => adapter.updateBlock(current, next));
  };
  // A link's block, as Notion's (seen 2026-10-11): its link copied, and no
  // download or fit where Notion offers none — a PDF still downloads, and an
  // embed fits once made narrower than the column: one across it has no "맞춤"
  // (seen 2026-11-06: a map, a form, a pen and example.com, each the column wide),
  // as one the block API made in its narrower box has (embedBoxWidth; seen 2026-11-11).
  const external = typeof block.content.fileId !== "string" && typeof block.content.url === "string" && block.content.url !== "";
  const narrowed = typeof block.content.width === "number" && block.content.width < 100
    || keptBlockWidth(block.content) !== undefined || embedBoxWidth(block) !== undefined;
  const ownService = block.type === "embed" ? embedOwnService(block) : undefined;
  const spansColumn = block.content.columnWide === true && !keptSized(block.content);
  const file: BlockMenuFile = {
    turnChoices: fileBlockTurnChoices(block),
    ...(ownService ? { label: ownService.name } : {}),
    // A touch screen's file row keeps its original and rename under "추가 옵션", a .txt's too, where
    // a desktop shows a row that turns into nothing else with them in the menu (Notion's iPad, seen 2026-11-13).
    ...(touch && block.type === "file" ? { groupMore: true } : {}),
    // A phone's PDF is a row, with nothing to fit (Notion's iPhone and Android sheets, seen 2026-11-14).
    // An upload the block API made spans its column with no box, and no "맞춤" until sized, as Notion's
    // (seen 2026-11-15: a video's menu at 1440 and its sheet on an A90, a PDF's at 1440; uploadedFrameBox).
    ...(block.type === "embed" && !html && narrowed || !external && !spansColumn && (block.type === "video" || block.type === "pdf" && !phone) ? {
      align: { active: fileBlockAlignment(block.content.align), onSelect: (value: "left" | "center" | "right") => { update({ align: value }); onDone(); } },
    } : {}),
    onReplace: () => { if (!adapter.readOnly) setReplacing(true); onDone(); },
    onDownload: () => { actions.download(); onDone(); }, onViewOriginal: () => { actions.open(); onDone(); },
    // Its source link is copied with Notion's "링크 복사 완료" (seen 2026-11-11, an embed's and a bookmark's).
    ...(external ? { onCopySource: () => {
      void copyText(String(block.content.url)).then((copied) => notify?.(
        String(i18n.t(copied ? "selectionToolbar:linkEditor.copiedLink" : "blockHandle:notifications.couldNotCopyLink")),
        copied ? "success" : "error"));
      onDone();
    } } : {}),
    // A PDF opens over the page area in Leafen's own preview, its zoom bar and
    // head as Notion's (seen 2026-11-06): an uploaded one with "다운로드하기" and
    // "페이지에 추가", a linked one, read through the server, with close alone.
    // A phone's PDF is a row that opens it so, and its menu has no "전체 화면" (Notion's iPhone, seen 2026-11-13).
    ...(block.type === "pdf" && adapter.session && !phone ? { onFullScreen: () => { openPdfPreview(adapter, block, actions); onDone(); } } : {}),
    // An uploaded document's embed opens in Leafen's preview over the page
    // area, as Notion's "전체화면으로 열기" opens its file preview (seen 2026-11-12, a CSV file).
    ...(block.type === "embed" ? { onFullScreen: () => {
      const kind = typeof block.content.fileId === "string" && adapter.session
        ? embedDocumentKind(String(block.content.name ?? ""), String(block.content.contentType ?? "")) : null;
      if (kind) openDocumentPreview({ name: String(block.content.name ?? ""), kind, resolve: actions.resolve, download: actions.download,
        pageId: adapter.pageId, blockId: block.id, workspaceId: adapter.workspaceId });
      else { setPreviewSource(source); setFullScreen(true); }
      onDone();
    } } : {}),
    // The block is let go of as its rename box opens, as Notion's (seen 2026-11-06).
    ...(block.type === "file" ? { onRename: () => {
      if (!adapter.readOnly) {
        setRename(renameBoxUnder(handle?.(), document.querySelector(`[data-block-id="${CSS.escape(block.id)}"]`)));
        editor?.setSelection(new Set());
      }
      onDone();
    } } : {}),
    ...(typeof block.content.fileId === "string" && documentKindOf(String(block.content.name ?? ""), String(block.content.contentType ?? ""))
      ? { onAllDocuments: () => { pushRoute(libraryHref("documents")); onDone(); } } : {}),
    ...(openers.length ? { openWith: openers.map(({ app, opener }) => ({
      id: `${app.installId}:${opener.key}`,
      name: appText(opener.name, i18n.language) || appText(app.name, i18n.language),
      appName: appText(app.name, i18n.language),
      run: () => {
        // The window belongs to the page, not this block, so it stays while the editor draws again.
        if (live && adapter.session) openAppFile({ session: adapter.session, app, opener, file: blockAppFile(adapter, live) });
        onDone();
      },
    })) } : {}),
  };
  const overlays = <>
    {replacement}
    {actions.error ? <span role="alert" data-file-menu-error>{blockItemText(actions.blocked ? "file.downloadBlocked" : "file.loadFailed")}</span> : null}
    {rename && live && !adapter.readOnly ? <FileNameBox name={String(live.content.name ?? "")} at={rename}
      onClose={() => setRename(null)} onSave={name => update({ name })} /> : null}
    {fullScreen && live && source === previewSource ? <BlockMediaPreviewDialog key={source} adapter={adapter} block={live}
      onClose={() => setFullScreen(false)} /> : null}
  </>;
  return { file, overlays };
}
