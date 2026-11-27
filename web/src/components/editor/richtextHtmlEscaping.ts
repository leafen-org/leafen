const RICHTEXT_HTML_MAX_TEXT_CHARS = 20_000_000;

export function assertRichtextHtmlTextBound(value: string) {
  if (value.length > RICHTEXT_HTML_MAX_TEXT_CHARS) {
    throw new Error("rich-text HTML input limit exceeded");
  }
}

export function escapeHtml(s: string): string {
  assertRichtextHtmlTextBound(s);
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
