import { copyTextWithBlocks } from "@/lib/clipboard";
import { clipboardServer } from "@/lib/clipboardServer";
import { copyText } from "@/lib/copyText";
import { packagedComputer } from "@/lib/desktopMenuCommands";
import { newId } from "@/lib/ids";
import { blocksClipboardHtml } from "./blockMarkdown";
import { EDITOR_CLIPBOARD_BOUNDS, EDITOR_MARKDOWN_BOUNDS } from "./editorClipboardPolicy";
import { escapeHtml } from "./richtextHtmlEscaping";

/**
 * Copies a page's link as Notion's link copy does (seen 2026-11-07): its
 * address as plain text, the page's name linked to it for other apps' rich
 * text (Notion's web, seen 2026-11-09) — in the desktop app the address
 * alone, as Notion's app gives other apps (7.42.3, seen 2026-11-09) — and
 * the page's mention for a page here, which pastes it as that mention
 * without asking how. An address pasted from elsewhere still asks
 * (PastedLinkMenu). Returns whether it copied.
 */
export async function copyPageLink(url: string, pageId: string, title: string): Promise<boolean> {
  return copyPage(url, pageId, title, packagedComputer() ? null : title.trim() || url);
}

/**
 * Copies a page's name as a link, as Notion's app does for "현재 페이지의 이름
 * 복사" (⌥⌘L, seen 2026-11-09): the name linked to the page's address for
 * other apps' rich text, the address as plain text, and the page's mention
 * for a page here. Returns whether it copied.
 */
export async function copyPageName(url: string, pageId: string, title: string): Promise<boolean> {
  return copyPage(url, pageId, title, title);
}

/** Copies a page as a link whose text is `shown`, or as its address alone
 * when `shown` is null, for other apps, and as its mention here. */
async function copyPage(url: string, pageId: string, title: string, shown: string | null): Promise<boolean> {
  try {
    const copyId = newId();
    const html = shown === null ? undefined
      : blocksClipboardHtml(`<p><a href="${escapeHtml(url)}">${escapeHtml(shown)}</a></p>`, EDITOR_MARKDOWN_BOUNDS, copyId);
    const blocks = [{
      type: "paragraph",
      content: { rich: [{ text: title, mention: "page", pageId }] },
      plainText: title,
      children: [],
    }];
    // Without HTML the mention rides beside the text alone (internalBlockPaste.ts); on
    // another server the address pastes as one from another app's.
    const data = JSON.stringify({ version: 2, copyId, text: url, blocks, ...(html ? {} : { textOnly: true }), pageLink: true, server: clipboardServer() });
    if (await copyTextWithBlocks(url, data, html, EDITOR_CLIPBOARD_BOUNDS)) return true;
  } catch {
    // The address alone, below.
  }
  return copyText(url);
}
