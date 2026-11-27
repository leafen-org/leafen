import { escapeImportHeadingEnd, escapeImportLineStarts } from "./importMarkdownText";
import { blockColorMark, TEXT_EDGE } from "./htmlImportColors";

/**
 * The blocks an imported HTML or Word document is read into
 * (importDocumentConverters.ts, docxImportConverter.ts) and the block
 * Markdown the workspace importer reads, which blocksToMarkdown writes them
 * as: each block a line, or a table's or a code block's lines, a block's
 * blocks inside the list item or quote holding them.
 */

export type TableHeaders = { row: boolean; column: boolean };

/** A table that is an inline database (htmlImportImageTables.ts): its title column; its last column holds its rows' pictures. */
export type TableDatabase = { title: number };

export type Block =
  | { prefix: string; text: string; indent: number; color?: string }
  | { code: string; language: string; indent: number }
  | { rows: string[][]; headers: TableHeaders; database?: TableDatabase; indent: number };

export const HEADER_ROW: TableHeaders = { row: true, column: false };

export function collapse(value: string) {
  return value.replace(/[\s ]+/g, " ").trim();
}

const WRAPPED = new RegExp(`^(${TEXT_EDGE})([\\s\\S]*?)(${TEXT_EDGE})$`);

/** Wraps formatted text in a marker, keeping edge spaces and color marks outside it. */
export function wrapInline(inner: string, marker: string) {
  const match = WRAPPED.exec(inner);
  return match && match[2] ? `${match[1]}${marker}${match[2]}${marker}${match[3]}` : inner;
}

/**
 * A table's lines; its first cell marks the headers it has when they are no
 * header row alone, and those of a table of one column, which Markdown's
 * importer otherwise reads as its cells' blocks (workspace_import_markdown_column_table.go),
 * and a database's title column after "database".
 */
function tableMarkdown(rows: string[][], headers: TableHeaders, database?: TableDatabase) {
  const width = Math.max(...rows.map((row) => row.length), 1);
  // A pipe inside a cell, even in inline code, is escaped so it stays in the cell.
  const cell = (value: string) => value.replace(/\\?\|/g, (pipe) => (pipe.length === 2 ? pipe : "\\|"));
  const line = (cells: string[]) => `| ${Array.from({ length: width }, (_, column) => cell(cells[column] ?? "")).join(" | ")} |`;
  const names = [database && "database", headers.row && "header-row", headers.column && "header-column", database && `title=${database.title}`];
  const marked = headers.row && !headers.column && width > 1 && !database ? rows[0] ?? []
    : [`[](leafen-table:${names.filter(Boolean).join(",")})${rows[0]?.[0] ?? ""}`, ...(rows[0] ?? []).slice(1)];
  return [line(marked), `|${" --- |".repeat(width)}`, ...rows.slice(1).map(line)].join("\n");
}

/** The width of a list item's marker, which its blocks stand in by. */
function markerWidth(prefix: string) {
  const marker = /^([-*+]|\d+[.)]) /.exec(prefix);
  return marker ? marker[0].length : 0;
}

/**
 * A block's words as they read: a heading's own, and every other block's
 * with each line's start escaped where it would open a block of its own.
 */
function blockWords(block: Extract<Block, { prefix: string }>) {
  return /^#{1,6} $/.test(block.prefix) ? escapeImportHeadingEnd(block.text) : escapeImportLineStarts(block.text);
}

/** What a block's lines after its first stand in by: a quote's mark, or as far as a list item writes its words. */
function blockLead(block: Block) {
  return !("prefix" in block) ? "" : block.prefix === "> " ? "> " : " ".repeat(markerWidth(block.prefix));
}

export function blocksToMarkdown(blocks: Block[]) {
  // Each level's blocks stand inside the list items and quotes holding them.
  const leads: string[] = [];
  return blocks.map((block, index) => {
    const lead = Array.from({ length: block.indent }, (_, level) => leads[level] ?? "  ").join("");
    leads.length = block.indent;
    leads[block.indent] = blockLead(block);
    const lines = "rows" in block ? tableMarkdown(block.rows, block.headers, block.database)
      : "code" in block ? `\`\`\`${block.language}\n${block.code.replace(/\n$/, "").replace(/^```/gm, "\\`\\`\\`")}\n\`\`\``
      : `${block.prefix}${blockColorMark(block.color)}${blockWords(block).replace(/\n/g, `\n${blockLead(block)}`)}`;
    const written = lines.split("\n").map((line) => (line ? `${lead}${line}` : lead.trimEnd())).join("\n");
    return index ? `${lead.trimEnd()}\n${written}` : written;
  }).join("\n");
}
