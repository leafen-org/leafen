/** Only allow safe link schemes (blocks javascript:/data:/vbscript: XSS). */
export function safeUrl(url: string | undefined): string {
  if (!url) return "";
  return /^(https?:|mailto:|tel:|\/|#)/i.test(url.trim()) ? url.trim() : "";
}

/**
 * Where a span's link opens. A web address written without its scheme
 * (example.com, //a.com/x), which Notion's API keeps as written and the
 * server takes so (content_graph_text_links.go), opens over https, as a URL
 * property's bare address does; any other as safeUrl takes it, a path in the
 * app ("/…") and an anchor ("#…") staying in the app.
 */
export function textLinkHref(url: string | undefined): string {
  const trimmed = url?.trim() ?? "";
  if (/^(?:[/\\]{2}|\\)/u.test(trimmed)) return `https://${trimmed.replace(/^[/\\]+/u, "")}`;
  return safeUrl(trimmed) || (trimmed && `https://${trimmed}`);
}
