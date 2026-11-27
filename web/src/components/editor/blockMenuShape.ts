import type { GraphBlock, GraphBlockType } from "@/contentGraph";

/** Which of the block menu's common rows a block has. */
export type BlockMenuShape = { turn: boolean; color: boolean; comment: boolean; suggest: boolean };

/** A chosen block's kind among several: a full-page database's line is one of its own (blockMenuSeveral). */
export type SeveralBlockKind = GraphBlockType | "database_page";

const EVERY_ROW: BlockMenuShape = { turn: true, color: true, comment: true, suggest: true };

/**
 * The common rows of a block's handle menu by its type, as Notion's (seen
 * 2026-10-11, a synthetic page of every block): a divider and a synced block
 * keep only the link, duplicate, move and delete rows; a table of contents
 * and a link to a page add color to those; a breadcrumb has no turn or
 * color; an equation and a code block no color; a bookmark turns only into a
 * mention.
 *
 * Several blocks keep only the rows every one of them has, and none comments
 * or suggests, as Notion's (seen 2026-11-13: lines with a divider or a picture
 * neither turn nor color, with a code block they only turn, with a table of
 * contents or a bookmark only color; two lines or a line and a heading both).
 */
export function blockMenuShape(type: GraphBlockType | "mixed" | readonly SeveralBlockKind[]): BlockMenuShape {
  if (typeof type !== "string") {
    const shapes = type.map((one) => SEVERAL_OWN_MENU[one] ?? blockMenuShape(one as GraphBlockType));
    return { turn: shapes.every((shape) => shape.turn), color: shapes.every((shape) => shape.color), comment: false, suggest: false };
  }
  switch (type) {
    case "divider":
    case "synced_block":
      return { turn: false, color: false, comment: false, suggest: false };
    case "table_of_contents":
    case "link_to_page":
      return { turn: false, color: true, comment: false, suggest: false };
    case "breadcrumb":
      return { turn: false, color: false, comment: true, suggest: true };
    case "equation":
    case "code":
      return { ...EVERY_ROW, color: false };
    // A bookmark turns only into a mention, a row of its own.
    case "bookmark":
      return { ...EVERY_ROW, turn: false };
    default:
      return EVERY_ROW;
  }
}

/**
 * Among several blocks, the rows of those whose own menu draws them
 * (BlockActionMenu): a picture or other media and a simple table or an
 * inline database neither turn nor color (a picture's, seen 2026-11-13); a
 * file colors, as its own menu does, and so does a full-page database's line,
 * which doesn't turn (Notion's, seen 2026-11-26 in two tabs: with a line
 * before or after it, and two of them).
 */
const NEITHER: BlockMenuShape = { turn: false, color: false, comment: false, suggest: false };
const SEVERAL_OWN_MENU: Partial<Record<SeveralBlockKind, BlockMenuShape>> = {
  image: NEITHER, video: NEITHER, audio: NEITHER, pdf: NEITHER, embed: NEITHER, link_preview: NEITHER,
  table: NEITHER, inline_database: NEITHER, file: { ...NEITHER, color: true }, database_page: { ...NEITHER, color: true },
};

/**
 * Whether a block takes a comment, from its menu's row or ⌘⇧M: a file linked
 * from elsewhere takes none, as Notion's (seen 2026-11-09: an uploaded file's
 * menu has "댓글", a linked file's none and ⌘⇧M does nothing).
 */
export function blockTakesComments(block: Pick<GraphBlock, "type" | "content">) {
  if (block.type !== "file") return true;
  return typeof block.content.fileId === "string" || !(typeof block.content.url === "string" && block.content.url !== "");
}
