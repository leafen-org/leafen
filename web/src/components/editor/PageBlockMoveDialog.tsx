import { useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import type { Page } from "@/types";
import { MoveToDialog } from "../MoveToDialog";
import { type MovePopoverAnchor, movePopoverAnchorOf } from "../movePopoverPlacement";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { isPageBlock } from "./nativeEditorGraphAdapter";
import type { EditorMoveContext } from "./editorRuntimeTypes";

/**
 * The page a block's "옮기기" moves as a page: a sub-page's block moving
 * alone, from its menu (⋮⋮ › 옮기기) or chosen with ⌘⇧P, as Notion's opens
 * the page's own move window then — its workspace at the foot, the pages it
 * suggests and the tree as the page's "옮기기" has them, the place chosen
 * taking the page last, "〈page〉의 변경된 위치는 …입니다." said with "이동" and
 * "실행 취소" (seen 2026-11-22 in two Chrome tabs at 1440). Blocks moving
 * with others go by the block window.
 */
export function pageBlockMoving(blocks: readonly (GraphBlock | undefined)[], pages: readonly Page[]): Page | null {
  const [block] = blocks;
  if (blocks.length !== 1 || !block || !isPageBlock(block)) return null;
  return pages.find((page) => page.id === block.id && !page.inTrash) ?? null;
}

/**
 * The page's move window for its block, hung as Notion's hangs it (seen
 * 2026-11-22 at 1440×722): from the menu, lined up with the handle; by
 * ⌘⇧P, with the block; under it where it holds the whole list, else over it,
 * else moved up into the window (257..709 for a block at 442..474), right
 * against it (0px).
 */
export function PageBlockMoveDialog({ page, blockId, anchor, moveContext, onClose }: {
  page: Page;
  blockId: string;
  /** What it was asked from, the handle; none by ⌘⇧P, which hangs it from the block. */
  anchor: PopoverRect | null | undefined;
  moveContext: EditorMoveContext;
  onClose: () => void;
}) {
  // Read once as it opens, followed as the page scrolls (popoverAnchorOf).
  const [from] = useState<MovePopoverAnchor | null>(() => {
    const opener = anchor ?? movePopoverAnchorOf(document.querySelector(`[data-block-id="${CSS.escape(blockId)}"]`));
    return opener && Object.create(opener, { whole: { value: true } });
  });
  return (
    <MoveToDialog pageId={page.id} pages={moveContext.pages} session={moveContext.session} anchor={from} anchorGap={0}
      onClose={onClose} onPageChange={moveContext.onPageChange} onStatus={moveContext.onStatus} />
  );
}
