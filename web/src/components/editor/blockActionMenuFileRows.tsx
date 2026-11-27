import type { HTMLAttributes, ReactNode } from "react";
import type { GraphBlockType } from "@/contentGraph";
import type { KeyboardShortcutId } from "@/lib/keyboardShortcuts";
import type { BlockSubmenu, BlockSubmenuEntry } from "./BlockMenuSubmenu";
import type { BlockMenuFile } from "./useBlockFileMenu";
import styles from "./editorStyles.generated.module.css";

type Row = { before?: ReactNode };

/** What the block menu (BlockActionMenu) lends a file block's rows to list them. */
export type FileMenuRowsMenu<A extends Row> = {
  add(id: string, label: string, run: () => void, attributes?: HTMLAttributes<HTMLButtonElement> & Record<string, unknown>, hint?: string): A;
  child(id: BlockSubmenu, title: string): A;
  keyed(action: A, id: KeyboardShortcutId): A;
  keysFor(id: KeyboardShortcutId): string | undefined;
  /** "여기에서 발표" on its own after the comments. */
  presentHere(): void;
  t(key: string): string;
  head: ReactNode;
  divider: ReactNode;
};

export type FileMenuRowsBlock = {
  file: BlockMenuFile;
  activeType: GraphBlockType;
  /** Whether the row keeps its original and rename under "추가 옵션" (fileCaptionInMore: its caption too). */
  fileMore: boolean;
  turns: number;
  captionVisible?: boolean;
  /** A touch screen's "아래에 삽입" (Notion's iPhone and iPad, seen 2026-11-13). */
  insertBelow?: () => void;
  /** A phone's sheet stands for the menu (videoFitInMore). */
  sheet?: boolean;
  toggleCaption(): void;
  onCopyLink?: () => void;
  onDuplicate(): void;
  onMove(): void;
  onDelete(): void;
  onComment?: () => void;
  onSuggest?: () => void;
};

/**
 * Whether a file row's caption is the first of its "추가 옵션", with its
 * original and rename: on a touch screen, a row that turns into another
 * block, as Notion's iPhone and iPad (seen 2026-11-14: a .docx and a .pdf
 * row, "캡션 · 원본 보기 · 이름 바꾸기"). A computer's menu shows it beside
 * "다운로드", as a touch screen does a row that turns into nothing (a .txt).
 */
export function fileCaptionInMore(file: BlockMenuFile | undefined, captionVisible: boolean | undefined) {
  return Boolean(file?.groupMore) && (file?.turnChoices.length ?? 0) > 0 && captionVisible !== undefined;
}

/**
 * Whether a phone's sheet keeps a video's "맞춤" and "원본 보기" under "추가
 * 옵션": one uploaded in its box, which has a fit, as Notion's A90 sheet
 * (seen 2026-11-15 twice: "캡션 · 추가 옵션 ›" holding "맞춤 › · 원본
 * 보기"); a computer's menu shows both in the menu, and a video the block
 * API made, with no fit, its original.
 */
export function videoFitInMore(file: BlockMenuFile | undefined, activeType: string, sheet: boolean | undefined) {
  return Boolean(sheet && file?.align) && activeType === "video";
}

/** "추가 옵션"'s row before the original: a PDF's full screen, or a phone's video's fit (videoFitInMore). */
export function fileMoreLead(file: BlockMenuFile, t: (key: string) => string, icons: { fullScreen: ReactNode; fit: ReactNode }): BlockSubmenuEntry {
  return file.onFullScreen ? { id: "full-screen", label: t("blockHandle:media.fullScreen"), icon: icons.fullScreen, run: file.onFullScreen }
    : { id: "align", label: t("blockHandle:media.fit"), icon: icons.fit, run: () => {}, opens: "align" };
}

/**
 * A file, PDF, video, audio or embed block's own menu, as Notion's (seen
 * 2026-10-11, 2026-11-06): turn and color, replace, download, caption, fit and
 * "추가 옵션" ›, the apps that open it; then the block's links, duplicate, move
 * and delete; then comment and suggest.
 */
export function addFileMenuRows<A extends Row>(menu: FileMenuRowsMenu<A>, block: FileMenuRowsBlock, actions: readonly A[]) {
  const { add, child, keyed, keysFor, t, divider } = menu;
  const { file, activeType, fileMore } = block;
  const start = actions.length;
  if (block.turns) child("turn", t("blockHandle:menu.turnInto"));
  if (activeType === "file") child("color", t("blockHandle:menu.color"));
  // A touch screen's file or PDF has it next, as Notion's iPhone and iPad (seen 2026-11-13).
  if (block.insertBelow) add("insert-below", t("blockHandle:menu.insertBelow"), block.insertBelow);
  add("replace", t("blockHandle:media.replace"), file.onReplace);
  actions[start].before = menu.head;
  // A link's block downloads only a PDF, and shows a PDF's full screen and
  // original in the menu itself, as Notion's (seen 2026-10-11).
  const linked = Boolean(file.onCopySource);
  if (!linked || activeType === "pdf") add("download", t("blockHandle:media.download"), file.onDownload);
  if (block.captionVisible !== undefined && !fileCaptionInMore(file, block.captionVisible)) {
    keyed(add("caption", t("blockHandle:media.caption"), block.toggleCaption), "toggleCaption");
  }
  const fitInMore = videoFitInMore(file, activeType, block.sheet);
  if (file.align && !fitInMore) child("align", t("blockHandle:media.fit"));
  // A PDF uploaded in its box keeps its full screen and original under "추가
  // 옵션", after its "맞춤"; one without that box — a link's, or an upload the
  // block API made across its column — shows them in the menu, as Notion's
  // (seen 2026-11-15 at 1440, twice for the API's: "캡션 · 전체 화면 · 원본
  // 보기"). A phone's, which has no full screen, shows its original in the
  // menu (Notion's sheet, seen 2026-11-14: "캡션 · 원본 보기").
  if ((activeType === "pdf" && !linked && file.onFullScreen && file.align) || fileMore || fitInMore) child("more", t("blockHandle:media.moreOptions"));
  else {
    if (file.onFullScreen) add("full-screen", t("blockHandle:media.fullScreen"), file.onFullScreen, undefined, keysFor("fullScreen"));
    add("view-original", t("blockHandle:media.viewOriginal"), file.onViewOriginal);
  }
  // The apps that open this kind of file, when any do (document apps decision of 2026-10-10, 5).
  if (file.openWith?.length) child("openWith", t("blockHandle:media.openWith"));
  // A document's block leads to every document, in the library.
  if (file.onAllDocuments) add("all-documents", t("blockHandle:media.allDocuments"), file.onAllDocuments);
  if (file.onRename && !fileMore) keyed(add("rename", t("blockHandle:media.rename"), file.onRename), "renamePage");
  // The link rows together, as an image's: its source's, then the block's (Notion's video and embed, seen 2026-11-06).
  const firstCommon = actions.length;
  if (file.onCopySource) add("copy-source", t("blockHandle:media.copySourceLink"), file.onCopySource);
  if (block.onCopyLink) add("copy-link", t("blockHandle:menu.copyLinkToBlock"), block.onCopyLink, undefined, keysFor("copyBlockLink"));
  add("duplicate", t("blockHandle:menu.duplicate"), block.onDuplicate, undefined, keysFor("duplicateBlock"));
  add("move", t("blockHandle:menu.moveTo"), block.onMove, undefined, keysFor("moveBlock"));
  add("delete", t("blockHandle:menu.delete"), block.onDelete, { className: `${styles.menuItem} ${styles.menuItemDanger}` }, keysFor("deleteBlock"));
  actions[firstCommon].before = divider;
  // A comment, where the block takes one: an uploaded file's does, a linked file's none (blockTakesComments, Notion's 2026-11-09).
  if (block.onComment) add("comment", t("blockHandle:menu.comment"), block.onComment, undefined, keysFor("addComment")).before = divider;
  if (block.onSuggest) add("suggest", t("blockHandle:menu.suggestEdits"), block.onSuggest, undefined, keysFor("suggestEdits"));
  // Nor is a linked file presented from here (Notion's menus, 2026-11-09).
  if (!(activeType === "file" && linked)) menu.presentHere();
}
