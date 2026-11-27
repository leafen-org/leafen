import { escapeImportMarkdown } from "./importMarkdownText";
import type { Block } from "./importMarkdownBlocks";
import { oneLine, paragraphText, type HtmlContext } from "./htmlImportText";

/**
 * A table holding a picture anywhere — a cell, its head, a link, bold, a
 * paragraph in a cell — in a document that is no export, as Notion's HTML
 * import reads it (seen 2026-11-13, 24 synthetic tables): an inline database
 * at its place, which the importer makes of the table the mark
 * [](leafen-table:database,…) opens (workspace_import_markdown_database.go).
 * A table of more than one row names the columns by its first row, a table
 * of one row is that row with the columns named by their numbers; after its
 * columns comes one holding each row's pictures, in its cells' order, and the
 * cells keep their words, formatted, the pictures' places left as the spaces
 * around them were. An empty row is a row too. The title is the first column
 * whose cells under the first row hold no link, or a new empty column after
 * the others when each does (Notion's of one row whose two cells link is
 * "1열", "2열", the title "3열" and the pictures "4열"). A picture is the file
 * the import uploaded for it, or else the address written. Notion's leaves
 * the first row's pictures out; here they stand as pictures right before the
 * database, so none is lost. One of a single column is its cells' blocks
 * (columnTableBlocks).
 */
export function holdsImageTable(table: Element, context: HtmlContext) {
  return context.breaks && Boolean(table.querySelector("img"));
}

type DatabaseCell = { words: string; files: string[]; pictures: string[]; linked: boolean };

export function imageTableBlocks(table: Element, indent: number, blocks: Block[], context: HtmlContext) {
  const rows = Array.from(table.querySelectorAll("tr")).map((row) => Array.from(row.children).map((cell) => databaseCell(cell, context)));
  if (!rows.length) return;
  const named = rows.length > 1;
  const body = named ? rows.slice(1) : rows;
  const columns = Math.max(...rows.map((cells) => cells.length));
  const linkless = Array.from({ length: columns }, (_, column) => column).findIndex((column) => body.every((cells) => !cells[column]?.linked));
  const title = linkless < 0 ? columns : linkless;
  const width = Math.max(columns, title + 1);
  if (named) for (const line of rows[0]!.flatMap((cell) => cell.pictures)) blocks.push({ prefix: line, text: "", indent });
  const line = (cells: DatabaseCell[], files: boolean) => [
    ...Array.from({ length: width }, (_, column) => cells[column]?.words ?? ""),
    files ? cells.flatMap((cell) => cell.files).join(" ") : "",
  ];
  blocks.push({
    rows: [...(named ? [line(rows[0]!, false)] : []), ...body.map((cells) => line(cells, true))],
    headers: { row: named, column: false }, database: { title }, indent,
  });
}

/**
 * A cell's words on one line, and its pictures: as the files of its row,
 * and as the lines a picture of the first row stands as. The other files
 * its words link follow the database (drainInto).
 */
function databaseCell(cell: Element, context: HtmlContext): DatabaseCell {
  const before = context.trailing.length;
  const words = oneLine(paragraphText(cell, context));
  const added = context.trailing.splice(before);
  const pictures = added.filter((line) => line.startsWith("!["));
  context.trailing.push(...added.filter((line) => !line.startsWith("![")));
  const files = Array.from(cell.querySelectorAll("img")).map((image) => databaseFile(image, context)).filter(Boolean);
  return { words, files, pictures, linked: Boolean(cell.querySelector("a[href]")) };
}

/** A picture as a file of its row: the import's upload of it, else the address written, which the importer names it by. */
function databaseFile(image: Element, context: HtmlContext) {
  const source = (image.getAttribute("src") ?? "").replace(/[\t\n\r]/g, "").trim();
  if (!source) return "";
  const reference = context.references.get(source);
  if (reference && reference.kind !== "page") return `![${escapeImportMarkdown(reference.name)}](leafen-file:${reference.fileId})`;
  return `![](<${source.replace(/[\\<>&]/g, "\\$&")}>)`;
}
