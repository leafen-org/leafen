import type { MarkdownToken } from "@tiptap/core";
import { MarkdownManager } from "@tiptap/markdown";

/**
 * Standard Markdown is read by Tiptap Markdown's lexer: MarkedJS, CommonMark
 * with GitHub's tables, strikethrough, task lists and autolinks. Tiptap
 * Markdown's own document parse builds ProseMirror nodes per Markdown type
 * and reads HTML through a schema; the native editor's blocks are not such
 * nodes, so Leafen maps the tokens to its blocks itself and keeps HTML as the
 * text it was written as. A paste is read a line (or a fence, table or
 * equation) at a time, as Notion reads one, so lines never run together.
 */
const manager = new MarkdownManager();

/**
 * Words read beyond standard Markdown, as Notion's paste reads them (real
 * ⌘V, seen 2026-11-11): "$…$" or "$$…$$" is an inline equation whose dollar
 * signs touch no letter, digit or other "$" outside them ("a$x$", "$x$a",
 * "$5 and $6" stay words; "\$" inside is the equation's own) — but a
 * "$$…$$" may have words right after it ("$$x$$y" is "x" and "y", seen
 * 2026-11-13), and either may run over a block's lines ("$a⏎b$" is the
 * equation "a⏎b", seen 2026-11-13) — and
 * "<u>…</u>" is underlined (no other HTML tag is read); "mailto:…" is a
 * link as a whole, as a web address is (GitHub's Markdown links only the
 * address after it); a lone "~" is a "~", only "~~…~~" striking words
 * through ("~a~" stays words, seen 2026-11-13).
 */
const MAIL_LINK = /^mailto:[^\s<>()]+/iu;
const UNDERLINE = /^<u>([\s\S]+?)<\/u>/u;
type InlineTokenizer = (this: { lexer: { inlineTokens(source: string): unknown[] } }, source: string, tokens: MarkdownToken[]) => MarkdownToken | undefined;

/**
 * Notion's paste reads no backslash escape (seen 2026-11-13): "\$5", "\*",
 * "\\" and "1\. n" stay as written, and the mark after one still works
 * ("\*a\*" is "\" and "a\" in italics, "\[e\](u)" a link "e\"), save that a
 * "\$" opens or closes no equation ("a \$x$ b" stays words, "$x\$y$" is
 * "x\$y"). The lexer reads each backslash as another mark of punctuation
 * it has no rule for, one the text doesn't hold, which the tokens give back
 * as a backslash.
 */
const STAND_INS = ["\u2E3C", "\u2E2E", "\u2E40", "\uA8F8"];

function standIn(source: string) {
  return source.includes("\\") ? STAND_INS.find(mark => !source.includes(mark)) : undefined;
}

function restored<T>(value: T, mark: string): T {
  if (typeof value === "string") return value.replaceAll(mark, "\\") as T;
  if (Array.isArray(value)) {
    for (let index = 0; index < value.length; index++) value[index] = restored(value[index], mark);
  } else if (value && typeof value === "object") {
    for (const key of Object.keys(value)) (value as Record<string, unknown>)[key] = restored((value as Record<string, unknown>)[key], mark);
  }
  return value;
}

function pastedWords(backslash: string): InlineTokenizer {
  const escaped = backslash.replace(/[\\^$.*+?()[\]{}|]/gu, "\\$&");
  const equationAt = new RegExp(`^(\\\${1,2})(?!\\$)((?:${escaped}\\$|[^$])+?)(?<!${escaped})\\1(?!\\$)`, "u");
  return function (source, tokens) {
    const before = tokens.at(-1)?.raw?.slice(-1) ?? "";
    const mail = MAIL_LINK.exec(source)?.[0].replace(/[.,;:!?]+$/u, "") ?? "";
    if (mail.length > "mailto:".length && !/[\p{L}\p{N}]/u.test(before)) {
      return { type: "link", raw: mail, href: mail, text: mail, tokens: [{ type: "text", raw: mail, text: mail }] };
    }
    const equation = equationAt.exec(source);
    if (equation) {
      const expression = equation[2].trim();
      if (!expression || before === backslash || /[\p{L}\p{N}$]/u.test(before)) return undefined;
      if (equation[1] === "$" && /^[\p{L}\p{N}]/u.test(source.slice(equation[0].length))) return undefined;
      // `text` stays what was written, which a reader that knows no equation shows.
      return { type: "inlineEquation", raw: equation[0], text: equation[0], expression };
    }
    if (/^~(?!~)/u.test(source) && before !== "~") return { type: "text", raw: "~", text: "~" };
    const underline = UNDERLINE.exec(source);
    if (underline) {
      return { type: "underline", raw: underline[0], text: underline[1], tokens: this.lexer.inlineTokens(underline[1]) as MarkdownToken[] };
    }
    return undefined;
  };
}

function lexer(backslash: string) {
  const { defaults } = manager.instance;
  const own = defaults.extensions;
  return new manager.instance.Lexer({ ...defaults, gfm: true, extensions: {
    renderers: own?.renderers ?? {}, childTokens: own?.childTokens ?? {}, ...own,
    inline: [pastedWords(backslash), ...(own?.inline ?? [])],
    startInline: [(source: string) => { const at = source.search(/\$|~|<u>|mailto:/iu); return at < 0 ? undefined : at; }, ...(own?.startInline ?? [])],
  } as typeof defaults.extensions });
}

function read(source: string, lex: (reader: ReturnType<typeof lexer>, source: string) => unknown[]): MarkdownToken[] {
  const mark = standIn(source);
  if (!mark) return lex(lexer("\\"), source) as MarkdownToken[];
  return restored(lex(lexer(mark), source.replaceAll("\\", mark)) as MarkdownToken[], mark);
}

export function markdownBlockTokens(source: string): MarkdownToken[] {
  return read(source, (reader, text) => reader.lex(text));
}

export function markdownInlineTokens(source: string): MarkdownToken[] {
  return read(source, (reader, text) => reader.inlineTokens(text));
}
