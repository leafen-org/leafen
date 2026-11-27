import { useEffect, useState, type ReactNode } from "react";
import type { GraphProperty } from "@/contentGraph";
import type { PopoverRect } from "@/lib/popoverPlacement";
import type { Page } from "@/lib/types";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { galleryPresentationProperty } from "./galleryViewModel";
import { RowPagePropertyMenu } from "./RowPagePropertyMenu";
import { RowPropertyCommentsPopover, type RowPropertyComments } from "./RowPropertyComments";
import { canCommentOnProperty } from "./rowPropertiesModel";

/** What a pinned property's name opens, where the page has it: the property's settings, the layout builder and its discussions. */
export type PinnedPropertyMenu = {
  editing?: DatabasePropertyEditing;
  onCustomizeLayout?(): void;
  comments?: RowPropertyComments;
};

/**
 * A pinned property's name opens its menu under it, as Notion's (seen
 * 2026-11-16 at 1440 on a full page and in a side peek): "이름 바꾸기",
 * "속성 편집" and "댓글", then "속성 삭제", then "레이아웃 사용자 지정",
 * without the property list's showing and copying rows; on a phone its sheet
 * (RowPagePropertyMenu). Its "댓글" opens the discussion's field 4 under the
 * value, its middle under the value's, as Notion's (a side peek: the value's
 * box to 182.4, the field from 186.4, both their middles at 855).
 */
export function usePinnedPropertyMenu(menu: PinnedPropertyMenu | undefined, properties: readonly GraphProperty[], row: Page, rowId: string) {
  const [open, setOpen] = useState<{ propertyId: string; anchor: PopoverRect } | null>(null);
  const [discussion, setDiscussion] = useState<{ propertyId: string; quote: string; left: number; top: number } | null>(null);
  useEffect(() => { setOpen(null); setDiscussion(null); }, [rowId]);
  const commentable = (property: GraphProperty) => Boolean(menu?.comments)
    && canCommentOnProperty(row, galleryPresentationProperty(property, properties.indexOf(property)));
  const opens = (property: GraphProperty) => Boolean(menu?.editing) || commentable(property);
  const onOpen = (property: GraphProperty, name: HTMLElement) => {
    const { left, right, top, bottom } = name.getBoundingClientRect();
    setDiscussion(null);
    setOpen({ propertyId: property.id, anchor: { left, right, top, bottom } });
  };
  const comment = (property: GraphProperty) => {
    const value = document.querySelector<HTMLElement>(`[data-row-pinned-property="${CSS.escape(property.id)}"] [data-property-value-box]`);
    if (!value) return;
    const box = value.getBoundingClientRect();
    setDiscussion({ propertyId: property.id, quote: (value.innerText || "").replace(/\s+/gu, " ").trim().slice(0, 200),
      left: box.left + box.width / 2, top: box.bottom + 4 });
  };
  const menuProperty = open ? (menu?.editing?.properties ?? properties).find((property) => property.id === open.propertyId) : undefined;
  const discussed = discussion ? properties.find((property) => property.id === discussion.propertyId) : undefined;
  const layer: ReactNode = <>
    {menu && open && menuProperty ? (
      <RowPagePropertyMenu property={menuProperty} anchor={open.anchor} editing={menu.editing} pinned onCustomizeLayout={menu.onCustomizeLayout}
        onComment={commentable(menuProperty) ? () => comment(menuProperty) : undefined} onClose={() => setOpen(null)} />
    ) : null}
    {menu?.comments && discussion && discussed ? (
      <RowPropertyCommentsPopover comments={menu.comments} property={{ id: discussed.id, name: discussed.name, type: discussed.type, config: discussed.config }}
        quote={discussion.quote} anchor={{ left: discussion.left, top: discussion.top }} centered onClose={() => setDiscussion(null)} />
    ) : null}
  </>;
  return { openId: open?.propertyId, opens: menu ? opens : undefined, onOpen, layer };
}
