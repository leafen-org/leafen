import {
  assertMarkdownPasteTable,
  type MarkdownPasteContext,
} from "./markdownPasteBudget";
import type { MarkdownToken } from "@tiptap/core";
import { spansFromMarkdownTokens } from "./markdownInlinePaste";
import { markdownBlockTokens } from "./markdownLexer";
import { isEscaped } from "./markdownPasteSyntax";

export function isMarkdownTableRow(line: string) {
  const trimmed = line.trim();
  return trimmed.startsWith("|") && trimmed.endsWith("|") && trimmed.slice(1, -1).includes("|");
}

function splitMarkdownTableRow(line: string) {
  const body = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  const cells: string[] = [];
  let current = "";
  for (let index = 0; index < body.length; index++) {
    const ch = body[index];
    if (ch === "|" && !isEscaped(body, index)) {
      cells.push(current);
      current = "";
    } else current += ch;
  }
  cells.push(current);
  return cells.map((cell) => cell.trim());
}

export function isMarkdownTableSeparator(line: string) {
  const cells = splitMarkdownTableRow(line);
  return cells.length >= 2 && cells.every((cell) => /^:?-{3,}:?$/.test(cell));
}

/**
 * A GitHub table as Tiptap Markdown's lexer reads it: each cell's spans and
 * plain text, every row as wide as the widest (and at least two cells).
 */
export function markdownTableWithin(text: string, context: MarkdownPasteContext) {
  const table = markdownBlockTokens(text.trim()).find((token) => token.type !== "space");
  if (table?.type !== "table") return null;
  const rows = [table.header as MarkdownToken[], ...(table.rows as MarkdownToken[][])];
  const width = Math.max(2, ...rows.map((row) => row.length));
  const cells = rows.map((row) => Array.from({ length: width }, (_, column) =>
    row[column] ? spansFromMarkdownTokens(row[column].tokens ?? [], context) : []));
  const plain = cells.map((row) => row.map((spans) => spans.map((span) => span.text).join("")));
  assertMarkdownPasteTable(plain, context);
  return { cells, plain };
}
