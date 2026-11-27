import { escapeHtml } from "@/components/editor/richtextHtmlEscaping";
import { clipboardServer } from "@/lib/clipboardServer";
import { newId } from "@/lib/ids";

/**
 * What copying chosen cards or table rows gives. Other apps take the pages
 * as Notion's copy gives them (seen 2026-11-05): the first's title as a
 * heading, "# 제목", each other's as a link to its page, a paragraph apart.
 * A page in Leafen takes them as Notion's takes its own copy (seen
 * 2026-11-12): a copy of each row's page as a sub-page where it is pasted,
 * "일 1 (1)", as a copied sub-page's block pastes (copyPageBlock); the
 * heading carries the copy's mark, which the paste matches its blocks by.
 * Cut (⌘X), each names its page as cut: pasted, the item leaves its
 * database as a sub-page there by the same ID (cutPagePaste.ts).
 */
export function rowPagesClipboard(pages: readonly { id: string; title: string; href: string }[], cut = false) {
  const copyId = newId();
  const text = pages.map(({ title }, index) => index ? title : `# ${title}`).join("\n\n");
  const html = pages.map(({ title, href }, index) => index ? `<p><a href="${escapeHtml(href)}">${escapeHtml(title)}</a></p>`
    : `<h1 data-leafen-copy-id="${escapeHtml(copyId)}">${escapeHtml(title)}</h1>`).join("\n");
  const blocks = pages.map(({ id, title }) => ({
    type: "child_page", content: { rich: title ? [{ text: title }] : [], childPageId: id, [cut ? "cutPage" : "copiedPage"]: id }, plainText: title,
  }));
  return { text, html, blocks: JSON.stringify({ version: 2, copyId, text, blocks, server: clipboardServer() }) };
}
