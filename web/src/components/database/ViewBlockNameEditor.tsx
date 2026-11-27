import { useMemo } from "react";
import type { GraphDataSource } from "@/contentGraph";
import { i18next } from "@/i18n";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import type { Page } from "@/types";
import type { TextSpan } from "@/lib/types";
import { AnchoredIconPicker, PICKER_WIDTH } from "../page/AnchoredEmojiPicker";
import { PageRenamePopover } from "../page/PageRenamePopover";

type IconType = NonNullable<GraphDataSource["iconType"]>;

/**
 * Where a view block's name box opens over its tabs (databaseViewBlock): 4
 * above the foot of their row and at the handle, 42 before the block, as
 * Notion's dashboard's (seen 2026-11-09).
 */
export function viewBlockNameAt(blockLeft: number, tabsBottom: number, margin: number) {
  return { top: tabsBottom - 4, left: Math.max(margin, blockLeft - 42) };
}

/**
 * Where a view block's icon picker opens, given its name box's place: its
 * middle under the block's handle, which stands where the box starts and is
 * 18 wide, and its top at the handle's foot, 4 above the box, as Notion's
 * dashboard's (seen 2026-11-09 at 1054×715: the handle 332–350 × 268–292,
 * the picker 408 wide from 137, 292 down). The picker opens 4 under its
 * place and is kept inside the window (AnchoredIconPicker).
 */
function viewBlockIconAnchor(at: { top: number; left: number }) {
  const width = Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN);
  return { top: at.top - 28, bottom: at.top - 8, left: at.left + 9 - width / 2 };
}

/**
 * A view block's own name and icon, edited over its tabs with its name row
 * left hidden, as Notion's dashboard and waiting link (seen 2026-11-09):
 * "이름 바꾸기" opens the small box, "제목 없음" while it has no name, and
 * "아이콘 편집" the icon picker under the block's handle.
 */
export function ViewBlockNameEditor({ editing, at, blockId, workspaceId, uploadPageId, title, titleSpans, icon, iconType, onTitleChange, onIconChange, onClose }: {
  editing: "rename" | "icon";
  at: { top: number; left: number };
  blockId: string;
  workspaceId: string;
  /** The page an uploaded icon belongs to. */
  uploadPageId: string;
  title: string;
  /** The name's spans as its reader reads them: its mentions, which the rename popover keeps. */
  titleSpans?: readonly TextSpan[];
  icon: string;
  iconType: IconType;
  onTitleChange(title: string, spans?: TextSpan[]): void;
  onIconChange(icon: string, iconType: IconType): void;
  onClose(): void;
}) {
  // One place while it is open, so the picker isn't placed again on each draw.
  const iconAnchor = useMemo(() => viewBlockIconAnchor(at), [at]);
  if (editing === "rename") {
    return <PageRenamePopover at={at} iconFallback="database" title={title} titleSpans={titleSpans} untitled={i18next.t("databaseView:untitled")}
      page={{ id: blockId, workspaceId, icon, iconType } as unknown as Page}
      onTitleChange={onTitleChange}
      onPresentationChange={(change) => { if (change.iconType) onIconChange(change.icon ?? "", change.iconType as IconType); }}
      onClose={onClose} />;
  }
  const pick = (value: string, type: IconType) => { onIconChange(value, type); onClose(); };
  return (
    <AnchoredIconPicker anchor={iconAnchor} marker="view-block-icon-picker" onClose={onClose} picker={{
      uploadTarget: { workspaceId, pageId: uploadPageId }, current: { icon, iconType },
      onPick: (emoji) => pick(emoji, "emoji"),
      onPickImage: (url) => pick(url, "image"),
      onPickCustomEmoji: (emoji) => pick(emoji.id, "custom_emoji"),
      onPickBuiltInIcon: (value) => pick(value, "icon"),
      onRemove: () => pick("", "none"),
    }} />
  );
}
