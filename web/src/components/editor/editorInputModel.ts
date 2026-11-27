import { safeUrl } from "./richtext";
import type { PastedBlock } from "./markdownPaste";
import type { BlockType, TextSpan } from "@/lib/types";
import { spansToPlainText } from "@/lib/types";
import { type KeyboardShortcutEventLike, matchesKeyboardShortcut, shortcutModifierHeld } from "@/lib/keyboardShortcuts";
import { serverOrigin } from "@/lib/serverOrigin";

/** What ⌘⌥0–9 turn a block into, by the digit. */
const TURN_INTO_TYPES: readonly BlockType[] = [
  "paragraph", "heading_1", "heading_2", "heading_3", "to_do",
  "bulleted_list_item", "numbered_list_item", "toggle", "code", "child_page",
];

/**
 * The block type ⌘⌥0–9 turn a block into (the "turnInto" shortcut;
 * Ctrl+Shift+0–9 away from a Mac, never AltGr, whose digits are letters), as Notion's: 0 text, 1–3 the headings, 4
 * a to-do, 5 a bullet, 6 a number, 7 a toggle, 8 code, 9 a page (measured
 * 2026-11-20 in Chrome at 1440, each from a line's end, one letter typed after).
 */
export function shortcutBlockType(e: KeyboardShortcutEventLike): BlockType | null {
  const digit = /^Digit([0-9])$/u.exec(e.code ?? "")?.[1] ?? (/^[0-9]$/u.test(e.key) ? e.key : "");
  if (!digit) return null;
  return matchesKeyboardShortcut("turnInto", e) ? TURN_INTO_TYPES[Number(digit)] : null;
}

export function printableTextKey(e: KeyboardShortcutEventLike) {
  if (shortcutModifierHeld(e)) return "";
  if (e.key.length !== 1) return "";
  if (e.key === " ") return "";
  return e.key;
}

export type BlockTextMark = "bold" | "italic" | "underline" | "strikethrough" | "code";

export function shortcutTextMark(e: KeyboardShortcutEventLike): BlockTextMark | null {
  if (matchesKeyboardShortcut("strikethrough", e)) return "strikethrough";
  if (matchesKeyboardShortcut("bold", e)) return "bold";
  if (matchesKeyboardShortcut("italic", e)) return "italic";
  if (matchesKeyboardShortcut("underline", e)) return "underline";
  if (matchesKeyboardShortcut("inlineCode", e)) return "code";
  return null;
}

export type InlineMarkdownMark = "bold" | "italic" | "strikethrough" | "code";

const INLINE_MARKDOWN_SHORTCUTS: {
  open: string;
  close: string;
  mark: InlineMarkdownMark;
}[] = [
  { open: "**", close: "**", mark: "bold" },
  { open: "__", close: "__", mark: "bold" },
  { open: "~~", close: "~~", mark: "strikethrough" },
  { open: "`", close: "`", mark: "code" },
  { open: "~", close: "~", mark: "strikethrough" },
  { open: "*", close: "*", mark: "italic" },
  { open: "_", close: "_", mark: "italic" },
];

// What typing turns into, in Notion's order (its input rules, seen
// 2026-11-01): quotes curl by what is before them, "--" is a dash, and the
// arrows and signs — "<->" goes by way of "←>", as "<-" turns first.
const INLINE_SYMBOL_SHORTCUTS: readonly { pattern: RegExp; length: number; replacement: string }[] = [
  { pattern: /^'$/u, length: 1, replacement: "‘" },
  { pattern: / '$/u, length: 2, replacement: " ‘" },
  { pattern: /'$/u, length: 1, replacement: "’" },
  { pattern: /^"$/u, length: 1, replacement: "“" },
  { pattern: / "$/u, length: 2, replacement: " “" },
  { pattern: /"$/u, length: 1, replacement: "”" },
  { pattern: /--$/u, length: 2, replacement: "—" },
  { pattern: /->$/u, length: 2, replacement: "→" },
  { pattern: /<->$/u, length: 3, replacement: "↔" },
  { pattern: /←>$/u, length: 2, replacement: "↔" },
  { pattern: /=>$/u, length: 2, replacement: "⇒" },
  { pattern: /<-$/u, length: 2, replacement: "←" },
  { pattern: />=$/u, length: 2, replacement: "≥" },
  { pattern: /<=$/u, length: 2, replacement: "≤" },
  { pattern: /!=$/u, length: 2, replacement: "≠" },
  { pattern: /\.\.\.$/u, length: 3, replacement: "…" },
];

export function findInlineSymbolShortcut(textBeforeCaret: string) {
  for (const shortcut of INLINE_SYMBOL_SHORTCUTS) {
    if (!shortcut.pattern.test(textBeforeCaret)) continue;
    const start = textBeforeCaret.length - shortcut.length;
    if (isEscaped(textBeforeCaret, start)) continue;
    if (hasOpenInlineCodeDelimiter(textBeforeCaret, start)) continue;
    return { trigger: textBeforeCaret.slice(start), replacement: shortcut.replacement, start };
  }
  return null;
}

/**
 * The inline equation `$$TeX$$` typed up to the caret makes, as Notion's
 * does as the closing `$$` is typed (seen 2026-09-30), or null.
 */
export function findInlineEquationShortcut(textBeforeCaret: string) {
  const match = /\$\$([^$\n]+)\$\$$/u.exec(textBeforeCaret);
  if (!match || !match[1].trim()) return null;
  const start = textBeforeCaret.length - match[0].length;
  if (isEscaped(textBeforeCaret, start) || hasOpenInlineCodeDelimiter(textBeforeCaret, start)) return null;
  return { start, tex: match[1] };
}

export function findInlineMarkdownShortcut(textBeforeCaret: string) {
  for (const shortcut of INLINE_MARKDOWN_SHORTCUTS) {
    if (!textBeforeCaret.endsWith(shortcut.close)) continue;
    const innerEnd = textBeforeCaret.length - shortcut.close.length;
    if (isEscaped(textBeforeCaret, innerEnd)) continue;
    const start = textBeforeCaret.lastIndexOf(shortcut.open, innerEnd - 1);
    if (start < 0) continue;
    if (isEscaped(textBeforeCaret, start)) continue;
    // A single closing character must not consume the unfinished double
    // delimiter as italic/strike before its second closing character arrives.
    if (shortcut.open.length === 1 && shortcut.open !== "`" &&
      (textBeforeCaret[start - 1] === shortcut.open || textBeforeCaret[start + 1] === shortcut.open ||
        textBeforeCaret[innerEnd - 1] === shortcut.close)) continue;
    const beforeOpen = textBeforeCaret[start - 1] ?? "";
    if (beforeOpen && /[\p{L}\p{N}_]/u.test(beforeOpen)) continue;
    const innerStart = start + shortcut.open.length;
    if (innerStart >= innerEnd) continue;
    const inner = textBeforeCaret.slice(innerStart, innerEnd);
    if (!inner.trim() || /^\s|\s$/.test(inner)) continue;
    return {
      ...shortcut,
      start,
      innerLength: inner.length,
    };
  }
  return null;
}

function isEscaped(text: string, index: number) {
  let slashCount = 0;
  for (let i = index - 1; i >= 0 && text[i] === "\\"; i--) slashCount++;
  return slashCount % 2 === 1;
}

function hasOpenInlineCodeDelimiter(text: string, beforeIndex: number) {
  let open = false;
  for (let i = 0; i < beforeIndex; i++) {
    if (text[i] !== "`" || isEscaped(text, i)) continue;
    open = !open;
  }
  return open;
}

function findUnescapedReverse(text: string, needle: string, from: number) {
  for (
    let index = Math.min(from, text.length - 1);
    index >= 0;
    index = text.lastIndexOf(needle, index - 1)
  ) {
    if (text[index] === needle && !isEscaped(text, index)) return index;
  }
  return -1;
}

export function findInlineMarkdownLinkShortcut(textBeforeCaret: string) {
  if (!textBeforeCaret.endsWith(")")) return null;
  const closeLabel = findUnescapedReverse(textBeforeCaret, "]", textBeforeCaret.length - 2);
  if (closeLabel < 0 || textBeforeCaret[closeLabel + 1] !== "(") return null;
  const linkMarker = closeLabel;
  const start = findUnescapedReverse(textBeforeCaret, "[", linkMarker - 1);
  if (start < 0) return null;
  const beforeOpen = textBeforeCaret[start - 1] ?? "";
  if (beforeOpen && /[\p{L}\p{N}_]/u.test(beforeOpen)) return null;

  const label = textBeforeCaret.slice(start + 1, linkMarker);
  const rawUrl = textBeforeCaret.slice(linkMarker + 2, -1);
  if (!label.trim() || /^\s|\s$/.test(label)) return null;

  const url = normalizePastedLink(rawUrl);
  if (!url) return null;
  return {
    start,
    labelLength: label.length,
    rawUrlLength: rawUrl.length,
    url,
  };
}

export function findTypedAutoLinkShortcut(textBeforeCaret: string) {
  const match = textBeforeCaret.match(/(\S+)(\s+)$/u);
  if (!match) return null;
  const token = match[1] ?? "";
  const whitespace = match[2] ?? "";
  if (!token || !whitespace) return null;

  const urlText = token.replace(/[.,;:!?]+$/u, "");
  // Typed text links only a web address (a scheme or a domain); a pasted
  // "/path" or "#anchor" may link, but typed "/word " and "#tag " stay text.
  if (!urlText || /^[/#]/u.test(urlText)) return null;
  const url = normalizePastedLink(urlText);
  if (!url) return null;
  return {
    start: textBeforeCaret.length - token.length - whitespace.length,
    urlLength: urlText.length,
    trailingLength: token.length - urlText.length + whitespace.length,
    url,
  };
}

export function normalizePastedLink(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed || /\s/.test(trimmed)) return "";
  let candidate = trimmed;
  const bareDomain = /^(?:[a-z0-9-]+\.)+[a-z]{2,}(?::\d+)?(?:[/?#].*)?$/i;
  const localhost = /^localhost(?::\d+)?(?:[/?#].*)?$/i;
  if (bareDomain.test(candidate) || localhost.test(candidate)) {
    candidate = `https://${candidate}`;
  } else if (!/^([a-z][a-z0-9+.-]*:|\/|#)/i.test(candidate)) {
    return "";
  }
  return safeUrl(candidate);
}

export function isExternalPastedWebUrl(url: string) {
  try {
    const parsed = new URL(url, serverOrigin());
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

/** Text carried by a pasted block, as spans — null when it has none. */
export function pastedBlockTextSpans(pasted: PastedBlock): TextSpan[] | null {
  const rich = pasted.content?.rich;
  if (Array.isArray(rich) && spansToPlainText(rich).length > 0) return rich;
  const text = pasted.plainText ?? "";
  if (text) return [{ text }];
  return null;
}

export function pastedUrlFallbackTitle(url: string) {
  try {
    const parsed = new URL(url);
    return parsed.hostname.replace(/^www\./i, "") || url;
  } catch {
    return url;
  }
}
