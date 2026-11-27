import { pastedCodeLanguage } from "./codeLanguages";
import { findUnescaped, unescapeMarkdownLiteral } from "./markdownPasteSyntax";
import type { PastedBlock } from "./markdownPasteTypes";

// How pasted Markdown lines read before they become blocks: how far in a
// line stands, a list item's words column, the holders a line goes in, a
// line that is only a picture, and code fences (markdownBlockPaste.ts).

// A line of nothing but a picture, "![caption](address)": a picture of its
// own between the lines around it, as Notion's paste makes one (seen
// 2026-11-11).
export function parseMarkdownImageLine(text: string) {
  if (!text.startsWith("![")) return null;
  const labelEnd = findUnescaped(text, "]", 2);
  if (labelEnd < 0 || text[labelEnd + 1] !== "(") return null;
  const hrefEnd = findUnescaped(text, ")", labelEnd + 2);
  if (hrefEnd !== text.length - 1) return null;
  return {
    label: unescapeMarkdownLiteral(text.slice(2, labelEnd)),
    href: text.slice(labelEnd + 2, hrefEnd).trim(),
  };
}

export function lineDepth(line: string) {
  const indent = line.match(/^[\t ]*/)?.[0] ?? "";
  const width = indent.replace(/\t/g, "  ").length;
  return Math.floor(width / 2);
}

export function stripDepthIndent(line: string, depth: number) {
  let remaining = depth * 2;
  let index = 0;
  while (index < line.length && remaining > 0) {
    if (line[index] === "\t") remaining -= 2;
    else if (line[index] === " ") remaining -= 1;
    else break;
    index++;
  }
  return line.slice(index);
}

// How far in a line stands, in columns, a tab going on to the next fourth.
export function columnsOf(line: string) {
  let columns = 0;
  for (const character of line) {
    if (character === "\t") columns += 4 - (columns % 4);
    else if (character === " ") columns += 1;
    else break;
  }
  return columns;
}

// A line with its first columns taken off, a tab they end inside of giving
// back the rest as spaces.
export function dropColumns(line: string, count: number) {
  let columns = 0;
  let index = 0;
  while (index < line.length && columns < count && (line[index] === " " || line[index] === "\t")) {
    columns += line[index] === "\t" ? 4 - (columns % 4) : 1;
    index++;
  }
  return " ".repeat(Math.max(columns - count, 0)) + line.slice(index);
}

// Where a list item's words begin, which a line must reach to stand in it:
// after its mark and the spaces after that, one when there are more than
// four or none (CommonMark's rule).
export function itemColumn(line: string) {
  const columns = columnsOf(line);
  const mark = /^([-+*]|\d{1,9}[.)])( *)/u.exec(line.trimStart());
  if (!mark) return columns + 2;
  const spaces = mark[2].length;
  return columns + mark[1].length + (spaces >= 1 && spaces <= 4 ? spaces : 1);
}

/**
 * The blocks a pasted line can go in by how far in it stands, as CommonMark
 * and Notion's paste place it (seen 2026-11-13): a list item or to-do whose
 * words it reaches, else the nearest one before it that it does, else the
 * page — never a paragraph, heading, quote, code or equation, however far
 * in. A callout's "<aside>" lines hold what stands between them. A list
 * item's holder is bare while its mark line held no words and nothing went
 * in it yet (ItemWords).
 */
export type Holder = { depth: number; column: number; aside?: boolean; item?: PastedBlock; bare?: boolean };

export function heldBy(holders: readonly Holder[], columns: number, base: number) {
  let kept = holders.length;
  while (kept > 0 && !holders[kept - 1].aside && holders[kept - 1].column > columns) kept--;
  const holder = holders[kept - 1];
  return { kept, depth: holder ? holder.depth + 1 : base, column: holder?.column ?? 0 };
}

export function codeFenceStart(line: string) {
  const match = line.match(/^(`{3,})(.*)$/);
  if (!match) return null;
  return {
    fenceLength: match[1].length,
    language: pastedCodeLanguage(match[2]),
  };
}

export function isCodeFenceEnd(line: string, fenceLength: number) {
  const match = line.trim().match(/^(`{3,})$/);
  return !!match && match[1].length >= fenceLength;
}
