import { INLINE_DATABASE_LINK } from "./htmlImportDatabases";
import type { Block } from "./importMarkdownBlocks";

/**
 * How an export lays its blocks inside one another, Notion's HTML export and
 * Leafen's alike (seen 2026-11-15): a column list is a box of columns
 * (div.column-list > div.column), each its share of the row in its style's
 * width ("width:30%"; "width:100%" each when they share it alike); and a
 * paragraph's blocks are in a box inside it (<p>words<div class="indented">…</div></p>,
 * which a reader closes before the box, the box then right after the
 * paragraph). Brought back, the columns stay columns and a paragraph's
 * blocks stay under it, as Notion's API import keeps them (2026-11-14: the
 * columns came as their blocks one after another, a paragraph's blocks as
 * its siblings). The importer reads them from marks
 * (workspace_import_markdown_marks.go): an item [](leafen-columns:) is a
 * column list, its items [](leafen-column:<width>) its columns, and an item
 * [](leafen-paragraph:) a paragraph holding the blocks under it.
 */

const PARAGRAPH_MARK = "- [](leafen-paragraph:) ";

export function isColumnList(element: Element) {
  return element.tagName === "DIV" && element.classList.contains("column-list");
}

/** A column's share of its row in percent, none when its columns share it alike. */
function columnWidth(column: Element) {
  const match = /(?:^|;)\s*width\s*:\s*([\d.]+)%/i.exec(column.getAttribute("style") ?? "");
  const width = match ? Number(match[1]) : NaN;
  return Number.isFinite(width) && width > 0 && width < 100 ? Math.round(width * 100) / 100 : null;
}

/** A column list's columns, each its blocks under it as `read` reads them. */
export function columnListBlocks(list: Element, indent: number, blocks: Block[], read: (nodes: Iterable<Node>, indent: number) => void) {
  blocks.push({ prefix: "- [](leafen-columns:) ", text: "", indent });
  for (const column of Array.from(list.children).filter((part) => part.classList.contains("column"))) {
    blocks.push({ prefix: `- [](leafen-column:${columnWidth(column) ?? ""}) `, text: "", indent: indent + 1 });
    read(column.childNodes, indent + 2);
  }
}

/**
 * Whether a box of a paragraph's blocks (div.indented right after the
 * paragraph, held) follows the paragraph's own block, the last read; marks
 * that block as one holding them if so.
 */
export function holdsIndented(box: Element, held: { element: Element; block: Block } | null, blocks: Block[]) {
  if (!box.matches("div.indented") || !held || box.previousElementSibling !== held.element || blocks[blocks.length - 1] !== held.block) return false;
  if (!("prefix" in held.block) || held.block.prefix !== "") return false;
  held.block.prefix = PARAGRAPH_MARK;
  return true;
}

/**
 * An export's line linking a page (figure.link-to-page), a sub-page's and a
 * link to a page alike, which the importer makes the sub-page's block the
 * first time and a link to the page after (workspace_import_markdown_page_lines.go);
 * one standing for a database its page holds inline (withNotionDatabaseLinks)
 * is marked so, which the importer turns inline there.
 */
export function pageLinkBlock(figure: Element, text: string, indent: number): Block | null {
  if (figure.tagName !== "FIGURE" || !figure.classList.contains("link-to-page") || !text) return null;
  return { prefix: `[](leafen-page-link:${figure.hasAttribute(INLINE_DATABASE_LINK) ? "inline" : ""}) `, text, indent };
}
