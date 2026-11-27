import type { MarkdownToken } from "@tiptap/core";
import type { TextSpan } from "@/lib/types";
import { spansFromMarkdownTokens } from "./markdownInlinePaste";
import { markdownBlockTokens } from "./markdownLexer";
import type { MarkdownPasteContext } from "./markdownPasteBudget";

/**
 * A quote's lines as Notion's Markdown paste reads them (seen 2026-11-14):
 * what its ">" lines hold, with the lines that go on its words without one
 * (CommonMark's lazy lines), is Markdown of its own. The first block of it
 * is the quote's words, the blocks after it go in the quote ("> p⏎> - a" is
 * "p" holding the item "a", "> # a⏎> p" is "a" in bold holding "p"):
 * - a paragraph's words are its words, spaces as written;
 * - a heading's, of any level, are its words in bold ("> # a" is "a");
 * - code's are its text in code;
 * - a bulleted list's items are "• " and their words, a line apart; a
 *   numbered list's are their words alone, one right after another ("> 1. a⏎
 *   > 2. b" is "ab"). An item's own paragraphs are a line apart and a list in
 *   it stands four spaces further in for each level, its items marked "◦ ",
 *   "▪ ", "• " by level or numbered "1. ", one line apart, a blank line
 *   before it after the item's words (one line in a loose item): "> - a⏎>   -
 *   b" is "• a⏎⏎    ◦ b". A to-do mark stays words ("• [ ] a"), an item of
 *   no words is "• ", and a heading starting an item has no "• ";
 * - a quote in it reads as one; a divider is no words.
 * `lines` is how many of the lines it reads, `rest` the Markdown of the
 * blocks it holds; null when the lexer reads no quote there.
 */
export type QuoteLines = { rich: TextSpan[]; rest: string; lines: number };

const BULLETS = ["• ", "◦ ", "▪ "];
const TO_DO_MARK = /^[\t ]*(?:[-+*]|\d{1,9}[.)])[\t ]+(\[[ xX]\][\t ]+)/u;

type Token = MarkdownToken & { items?: Token[]; task?: boolean; ordered?: boolean; start?: number | "" };

function append(out: TextSpan[], spans: readonly TextSpan[]) {
  for (const span of spans) if (span.text || span.equation !== undefined || span.mention) out.push(span);
}

function itemWords(item: Token, prefix: string, level: number, context: MarkdownPasteContext) {
  const out: TextSpan[] = [];
  let last = "";
  for (const token of (item.tokens ?? []) as Token[]) {
    if (token.type === "space" || token.type === "checkbox") continue;
    if (token.type === "list") {
      append(out, [{ text: last === "text" ? "\n\n" : "\n" }]);
      append(out, listWords(token, level + 1, context));
    } else if (token.type === "text" || token.type === "paragraph") {
      if (last) append(out, [{ text: "\n" }]);
      else append(out, [{ text: prefix + (item.task ? TO_DO_MARK.exec(item.raw ?? "")?.[1] ?? "" : "") }]);
      append(out, spansFromMarkdownTokens(token.tokens ?? [], context));
    } else {
      if (last) append(out, [{ text: "\n" }]);
      append(out, blockWords(token, context));
    }
    last = token.type || "block";
  }
  return last ? out : [{ text: prefix }];
}

function listWords(list: Token, level: number, context: MarkdownPasteContext) {
  const out: TextSpan[] = [];
  const indent = "    ".repeat(level);
  const start = typeof list.start === "number" ? list.start : 1;
  (list.items ?? []).forEach((item, at) => {
    if (at > 0 && (level > 0 || !list.ordered)) append(out, [{ text: "\n" }]);
    const mark = list.ordered ? (level > 0 ? `${start + at}. ` : "") : BULLETS[level % BULLETS.length];
    append(out, itemWords(item, level > 0 ? indent + mark : mark, level, context));
  });
  return out;
}

function blockWords(token: Token, context: MarkdownPasteContext): TextSpan[] {
  switch (token.type) {
    case "paragraph":
    case "text":
      return spansFromMarkdownTokens(token.tokens ?? [], context);
    case "heading":
      return spansFromMarkdownTokens(token.tokens ?? [], context).map((span) => ({ ...span, bold: true }));
    case "code":
      return token.text ? [{ text: token.text, code: true }] : [];
    case "list":
      return listWords(token, 0, context);
    case "blockquote": {
      const first = ((token.tokens ?? []) as Token[]).find((inner) => inner.type !== "space");
      return first ? blockWords(first, context) : [];
    }
    case "hr":
      return [];
    default:
      return token.raw?.trim() ? [{ text: token.raw.trim() }] : [];
  }
}

export function readQuoteLines(source: string, context: MarkdownPasteContext): QuoteLines | null {
  const quote = markdownBlockTokens(source)[0] as Token | undefined;
  if (quote?.type !== "blockquote") return null;
  const tokens = (quote.tokens ?? []) as Token[];
  const first = tokens.findIndex((token) => token.type !== "space");
  return {
    rich: first < 0 ? [] : blockWords(tokens[first], context),
    rest: first < 0 ? "" : tokens.slice(first + 1).map((token) => token.raw ?? "").join(""),
    lines: (quote.raw ?? "").replace(/\n+$/u, "").split("\n").length,
  };
}
