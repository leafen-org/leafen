export function pageMarkdownFileName(title: string, untitled: string) {
  const safeTitle = title
    .replace(/[\\/:*?"<>|#\u0000-\u001f]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 80);
  return `${safeTitle || untitled}.md`;
}
