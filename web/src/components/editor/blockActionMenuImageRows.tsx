import type { ReactNode } from "react";
import { AltTextIcon, ArrowUpRightIcon, CropIcon, ImageAlignIcon, LinkPlusIcon, ZoomInIcon } from "@/icons/leafen";
import type { FileMenuRowsMenu } from "./blockActionMenuFileRows";
import type { BlockTurnChoice } from "./blockTurnInto";
import type { BlockMenuFile } from "./useBlockFileMenu";
import styles from "./editorStyles.generated.module.css";

/** The image a single selected image block shows, with what the menu can do to it. */
export type BlockMenuImage = {
  managed: boolean;
  link: string;
  altText: string;
  onDownload: () => void;
  onCopyImage: () => void;
  onViewOriginal: () => void;
  onOpenLink: () => void;
  onCopySourceLink: () => void;
  onFullScreen: () => void;
  /** Closes the menu and opens the picture's link window by its block (ImageLinkWindow). */
  onEditLink: () => void;
  /** Closes the menu and opens the alt text window at the list it came from (ImageAltTextWindow). */
  onEditAltText: (from?: DOMRect) => void;
  onReplace: () => void;
  turnChoices?: BlockTurnChoice[];
  onCrop?: () => void;
  align?: BlockMenuFile["align"];
};

/** What an image's link window needs; an empty image has only its link. */
export type BlockMenuImageFields = Pick<BlockMenuImage, "link" | "onEditLink">;

/** The rows of an image's "추가 옵션", beside its menu. */
export const IMAGE_MORE_ICONS = {
  altText: <AltTextIcon size="regular" />,
  "full-screen": <ZoomInIcon size="regular" />,
  "view-original": <ArrowUpRightIcon size="regular" />,
  "open-link": <ArrowUpRightIcon size="regular" />,
};

export type ImageMenuRowsBlock = {
  image: BlockMenuImage;
  turns: number;
  captionVisible?: boolean;
  toggleCaption(): void;
  onCopyLink?: () => void;
  onDuplicate(): void;
  onMove(): void;
  onDelete(): void;
  onComment?: () => void;
  onSuggest?: () => void;
  /** A touch screen's "아래에 삽입", after "전환", as its other blocks' (Notion's iPhone, seen 2026-11-15). */
  insertBelow?: () => void;
  /** A phone's "작업" sheet: no "이미지 복사", nor a crop or fit in "추가 옵션" (imageMoreEntries). */
  sheet?: boolean;
};

/**
 * Whether a linked picture shows "맞춤" in its menu itself, its link then the
 * first of "추가 옵션", as Notion's once sized narrower than its column (seen
 * 2026-11-27, twice: "캡션 · 맞춤 › · 추가 옵션 ›" holding "이미지에 링크
 * 추가하기 · 대체 텍스트 · 전체 화면 · 원본 보기"); an uploaded one keeps it in
 * "추가 옵션". A phone's sheet has no fit.
 */
function linkedFitInMenu(image: BlockMenuImage, sheet: boolean | undefined) {
  return !image.managed && Boolean(image.align) && !sheet;
}

/**
 * An image's own menu, as Notion's (seen 2026-10-07): replace, copy the
 * image, download, caption, its link and "추가 옵션" ›; then the block's
 * links, copy, move and delete; then comment and suggest. Its alignment is
 * on the image's own tools, and its "맞춤" where it offers one
 * (imageAlignPlaces, linkedFitInMenu). A phone's sheet, as Notion's iPhone (seen
 * 2026-11-15 twice each, an uploaded and a linked image, Safari 402 dark):
 * "전환 ›" (an uploaded one's), "아래에 삽입", "바꾸기", "다운로드", "캡션",
 * a linked one's link, "추가 옵션 ›" — no "이미지 복사".
 */
export function addImageMenuRows<A extends { before?: ReactNode }>(menu: FileMenuRowsMenu<A>, block: ImageMenuRowsBlock, actions: readonly A[]) {
  const { add, child, keyed, keysFor, t, divider } = menu;
  const { image } = block;
  if (block.turns) child("turn", t("blockHandle:menu.turnInto"));
  if (block.insertBelow) add("insert-below", t("blockHandle:menu.insertBelow"), block.insertBelow);
  add("replace", t("blockHandle:media.replace"), image.onReplace, { "data-image-action": "replace" });
  actions[0].before = menu.head;
  if (!block.sheet) add("copy-image", t("blockHandle:media.copyImage"), image.onCopyImage, { "data-image-action": "copy-image" });
  add("download", t("blockHandle:media.download"), image.onDownload, { "data-image-action": "download" });
  if (block.captionVisible !== undefined) keyed(add("caption", t("blockHandle:media.caption"), block.toggleCaption, { "data-image-action": "caption" }), "toggleCaption");
  // A linked picture's "맞춤" takes its link's place, the link going to "추가 옵션" (linkedFitInMenu).
  if (linkedFitInMenu(image, block.sheet)) child("align", t("blockHandle:media.fit"));
  else if (!image.managed) keyed(add("imageLink", image.link ? t("blockHandle:media.editImageLink") : t("blockHandle:media.addImageLink"), image.onEditLink), "imageLink");
  child("more", t("blockHandle:media.moreOptions"));
  const blockRows = actions.length;
  if (!image.managed) add("copy-source", t("blockHandle:media.copySourceLink"), image.onCopySourceLink, { "data-image-action": "copy-source" });
  if (block.onCopyLink) add("copy-link", t("blockHandle:menu.copyLinkToBlock"),
    block.onCopyLink, { "data-copy-block-link": true }, keysFor("copyBlockLink"));
  add("duplicate", t("blockHandle:menu.duplicate"), block.onDuplicate, undefined, keysFor("duplicateBlock"));
  add("move", t("blockHandle:menu.moveTo"), block.onMove, undefined, keysFor("moveBlock"));
  add("delete", t("blockHandle:menu.delete"), block.onDelete, { className: `${styles.menuItem} ${styles.menuItemDanger}` }, keysFor("deleteBlock"));
  actions[blockRows].before = divider;
  const talkRows = actions.length;
  if (block.onComment) add("comment", t("blockHandle:menu.comment"), block.onComment, { "data-block-comment": true }, keysFor("addComment"));
  if (block.onSuggest) add("suggest", t("blockHandle:menu.suggestEdits"), block.onSuggest, { "data-block-suggest": true }, keysFor("suggestEdits"));
  if (actions[talkRows]) actions[talkRows].before = divider;
  menu.presentHere();
}

/**
 * An image's "추가 옵션": its crop, an uploaded one's fit and link (a linked
 * one's link when its fit is in the menu), alt text (its own window at the
 * list it came from), full screen, the original and the image's link. A
 * phone's sheet has no crop or fit, as Notion's iPhone (seen 2026-11-15: an
 * uploaded one's "이미지에 링크 추가하기 · 대체 텍스트 · 전체 화면 · 원본
 * 보기", a linked one's the last three).
 */
export function imageMoreEntries(image: BlockMenuImage, t: (key: string) => string, list: { close(): void; rect(): DOMRect | undefined }, sheet = false) {
  return [
    ...(image.onCrop && !sheet ? [{ id: "crop", label: t("blockHandle:media.crop"), icon: <CropIcon size="regular" />, run: image.onCrop }] : []),
    // Its mark the left alignment's, as the menu's own "맞춤" (Notion's alignLeft, seen 2026-11-27 twice).
    ...(image.managed && image.align && !sheet ? [{ id: "align", label: t("blockHandle:media.fit"), icon: <ImageAlignIcon align="left" size="regular" />, run: () => {}, opens: "align" as const }] : []),
    ...(image.managed || linkedFitInMenu(image, sheet) ? [{ id: "imageLink", label: t(image.link ? "blockHandle:media.editImageLink" : "blockHandle:media.addImageLink"), icon: <LinkPlusIcon size="regular" />, run: () => { list.close(); image.onEditLink(); } }] : []),
    { id: "altText", label: t("blockHandle:media.altText"), icon: IMAGE_MORE_ICONS.altText, run: () => {
      const from = list.rect(); list.close(); image.onEditAltText(from); } },
    { id: "full-screen", label: t("blockHandle:media.fullScreen"), icon: IMAGE_MORE_ICONS["full-screen"], run: image.onFullScreen },
    { id: "view-original", label: t("blockHandle:media.viewOriginal"), icon: IMAGE_MORE_ICONS["view-original"], run: image.onViewOriginal },
    ...(image.link ? [{ id: "open-link", label: t("blockHandle:media.openLink"), icon: IMAGE_MORE_ICONS["open-link"], run: image.onOpenLink }] : []),
  ];
}
