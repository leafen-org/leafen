import type { TextSpan } from "@/lib/types";
import {
  admitStandaloneSpans,
  assertMarkdownPasteDepth,
  consumeMarkdownPasteInput,
  createMarkdownPasteContext,
  type MarkdownPasteBounds,
  type MarkdownPasteContext,
} from "./markdownPasteBudget";
import type { MarkdownToken } from "@tiptap/core";
import { markdownInlineTokens } from "./markdownLexer";
import { decodeCharacterReferences, normalizeInlineAutoLink, pastedLinkAddress } from "./markdownPasteSyntax";
import { safeInlineEquationValue } from "./richtextEquationPolicy";

const INLINE_AUTO_LINK_RE =
  /^(https?:\/\/[^\s<>()]+|mailto:[^\s<>()]+|localhost(?::\d+)?(?:[/?#][^\s<>()]*)?|(?:[a-z0-9-]+\.)+[a-z]{2,}(?::\d+)?(?:[/?#][^\s<>()]*)?)/i;

function findInlineAutoLink(text: string, offset: number) {
  const previous = text[offset - 1] ?? "";
  if (previous && /[\p{L}\p{N}_@.-]/u.test(previous)) return null;
  const match = text.slice(offset).match(INLINE_AUTO_LINK_RE);
  if (!match) return null;
  const raw = match[1] ?? "";
  const linkText = raw.replace(/[.,;:!?]+$/u, "");
  if (!linkText) return null;
  const url = normalizeInlineAutoLink(linkText);
  if (!url) return null;
  return { text: linkText, trailing: raw.slice(linkText.length), rawLength: raw.length, url };
}

function dateFromMentionHref(raw: string) {
  const value = raw.trim();
  if (!/^leafen:\/\/date\//i.test(value)) return "";
  const date = value.replace(/^leafen:\/\/date\//i, "").split(/[?#]/, 1)[0];
  try {
    return decodeURIComponent(date);
  } catch {
    return date;
  }
}

function personIdFromMentionHref(raw: string) {
  const value = raw.trim();
  if (!/^leafen:\/\/person\//i.test(value)) return "";
  const userId = value.replace(/^leafen:\/\/person\//i, "").split(/[?#]/, 1)[0];
  try {
    return decodeURIComponent(userId);
  } catch {
    return userId;
  }
}

// Spans from Tiptap Markdown's inline tokens, an inline equation and
// underlined words among them (markdownLexer.ts). What a link points at stays
// Leafen's: a person or date mention, a safe address — a page's here too,
// which stays a link as Notion keeps a link to its own page pasted as
// Markdown (seen 2026-11-11) — or the text it was written as; a bare web
// address in text becomes a link.
function spansFromTokens(
  tokens: readonly MarkdownToken[],
  marks: Partial<TextSpan>,
  context: MarkdownPasteContext,
  depth: number,
  out: TextSpan[],
) {
  assertMarkdownPasteDepth(depth, context);
  const push = (span: TextSpan) => {
    if (!span.text) return;
    const last = out.at(-1);
    // A mention or an equation is a piece of its own, never joined.
    if (last && !last.mention && !span.mention && !last.equation && !span.equation && sameMarks(last, span)) last.text += span.text;
    else out.push(span);
  };
  const pushText = (text: string) => {
    let plain = "";
    for (let index = 0; index < text.length;) {
      const autoLink = findInlineAutoLink(text, index);
      if (!autoLink) {
        plain += text[index];
        index += 1;
        continue;
      }
      push({ ...marks, text: plain });
      plain = "";
      push({ ...marks, text: autoLink.text, link: autoLink.url });
      plain += autoLink.trailing;
      index += autoLink.rawLength;
    }
    push({ ...marks, text: plain });
  };
  for (const token of tokens) {
    switch (token.type) {
      case "text":
        if (token.tokens?.length) spansFromTokens(token.tokens, marks, context, depth, out);
        else pushText(decodeCharacterReferences(token.text ?? ""));
        break;
      case "escape":
        push({ ...marks, text: token.text ?? "" });
        break;
      case "codespan":
        // Code keeps "&copy;" as written, as CommonMark and Notion's (seen 2026-11-11).
        push({ ...marks, code: true, text: token.text ?? "" });
        break;
      case "strong":
        spansFromTokens(token.tokens ?? [], { ...marks, bold: true }, context, depth + 1, out);
        break;
      case "em":
        spansFromTokens(token.tokens ?? [], { ...marks, italic: true }, context, depth + 1, out);
        break;
      case "del":
        spansFromTokens(token.tokens ?? [], { ...marks, strikethrough: true }, context, depth + 1, out);
        break;
      case "underline":
        spansFromTokens(token.tokens ?? [], { ...marks, underline: true }, context, depth + 1, out);
        break;
      case "inlineEquation": {
        // An equation too long for one stays the words it was written as.
        const equation = safeInlineEquationValue(String(token.expression ?? ""));
        push(equation ? { ...marks, text: equation, equation } : { ...marks, text: token.raw ?? "" });
        break;
      }
      case "br":
        push({ ...marks, text: "\n" });
        break;
      case "link": {
        // Its address has its character references read ("&amp;" is "&"), as Notion's.
        const raw = decodeCharacterReferences(String(token.href ?? "").trim());
        // A bare www. address is a web one, as any bare address is.
        const url = /^www\./i.test(token.raw ?? "") ? normalizeInlineAutoLink(token.raw ?? "") || raw : raw;
        const date = dateFromMentionHref(url);
        const userId = personIdFromMentionHref(url);
        const href = normalizeInlineAutoLink(pastedLinkAddress(url));
        if (!token.tokens?.length || /\s/.test(url) || !(date || userId || href)) {
          push({ ...marks, text: token.raw ?? "" });
          break;
        }
        const label: TextSpan[] = [];
        spansFromTokens(token.tokens, marks, context, depth + 1, label);
        for (const span of label) {
          if (date) out.push({ ...span, mention: "date", date });
          else if (userId) out.push({ ...span, mention: "person", userId });
          else push({ ...span, link: href });
        }
        break;
      }
      default:
        // Images, HTML and anything else stay the text they were written as.
        if (token.type !== "checkbox") push({ ...marks, text: token.raw ?? token.text ?? "" });
    }
  }
}

const MARK_KEYS = ["bold", "italic", "strikethrough", "code", "underline", "link", "color"] as const;
function sameMarks(left: TextSpan, right: TextSpan) {
  return MARK_KEYS.every((key) => left[key] === right[key]);
}

function parseInlineRaw(text: string, context: MarkdownPasteContext, depth: number): TextSpan[] {
  assertMarkdownPasteDepth(depth, context);
  if (!text) return [];
  const out: TextSpan[] = [];
  spansFromTokens(markdownInlineTokens(text), {}, context, depth, out);
  return out;
}

/** Spans from inline tokens already read, as a block's own text. */
export function spansFromMarkdownTokens(tokens: readonly MarkdownToken[], context: MarkdownPasteContext): TextSpan[] {
  const out: TextSpan[] = [];
  spansFromTokens(tokens, {}, context, 0, out);
  return out;
}

export function parseInlineMarkdownWithin(text: string, context: MarkdownPasteContext) {
  return parseInlineRaw(text, context, 0);
}

export function parseInlineMarkdown(text: string, bounds: MarkdownPasteBounds): TextSpan[] {
  const context = createMarkdownPasteContext(bounds);
  consumeMarkdownPasteInput(text, context);
  const spans = parseInlineRaw(text, context, 0);
  admitStandaloneSpans(spans, context);
  return spans;
}
