import type { TextSpan } from "@/lib/types";
import { escapeHtml } from "./richtextHtmlEscaping";

// A suggested edit marks the text it makes: text suggested in or away, or
// text whose formatting is suggested, with the comment thread that holds it.

export function safeSuggestionValue(value: string | undefined) {
  return value === "insert" || value === "delete" || value === "format" ? value : undefined;
}

export function safeSuggestionIdValue(value: string | undefined) {
  return value && /^[A-Za-z0-9_-]+$/.test(value) ? value : undefined;
}

// suggestionWrapped wraps the whole of a suggested span's markup, a mention
// too, so that reading the text back keeps the suggestion.
export function suggestionWrapped(span: TextSpan, html: string) {
  const suggestion = safeSuggestionValue(span.suggestion);
  const suggestionId = safeSuggestionIdValue(span.suggestionId);
  if (!suggestion || !suggestionId) return html;
  const before = span.suggestionBefore ? ` data-suggestion-before="${escapeHtml(span.suggestionBefore)}"` : "";
  return `<span data-suggestion="${suggestion}" data-suggestion-id="${escapeHtml(suggestionId)}"${before}>${html}</span>`;
}
