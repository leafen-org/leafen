import type { ClipboardEvent } from "react";

/**
 * A comment's words copied as Notion's posted comments copy them (seen
 * 2026-11-11): every mention is its words — "@이름", "@오늘", a workspace
 * emoji's name — and a page's or a link's a link to where it leads, their
 * marks kept, so in a page or another comment they paste as words and a
 * link, not as the mentions a page's own text copies (its editor's copy).
 * Its plain text is its words as written, and its HTML keeps their spaces
 * and lines.
 */
export function copyCommentText(event: ClipboardEvent<HTMLElement>) {
  const selection = window.getSelection();
  if (event.defaultPrevented || !selection || selection.isCollapsed || selection.rangeCount !== 1) return;
  const range = selection.getRangeAt(0);
  // A copy running past this comment's words is the browser's.
  if (!event.currentTarget.contains(range.commonAncestorContainer)) return;
  const copied = document.createElement("span");
  copied.style.whiteSpace = "pre-wrap";
  copied.append(range.cloneContents());
  const mentions = copied.querySelectorAll<HTMLElement>("[data-mention], img[data-inline-custom-emoji]");
  if (!mentions.length) return;
  for (const mention of mentions) {
    // One inside a mention already made a link went with it.
    if (copied.contains(mention)) mention.replaceWith(copiedWords(mention));
  }
  event.clipboardData.setData("text/html", copied.outerHTML);
  event.clipboardData.setData("text/plain", copied.textContent ?? "");
  event.preventDefault();
}

/** A mention as copied: a link by its title to where it leads, else its words with their marks. */
function copiedWords(mention: HTMLElement): Node {
  if (mention instanceof HTMLImageElement) return document.createTextNode(mention.alt);
  if (mention instanceof HTMLAnchorElement && mention.href) {
    const link = document.createElement("a");
    link.href = mention.href;
    // A link's mention is its page's title, not the site's name drawn before it.
    link.textContent = (mention.querySelector('[data-mention-label="external"]') ?? mention).textContent;
    return link;
  }
  if (!mention.textContent) return document.createTextNode(mention.querySelector("img[alt]")?.getAttribute("alt") ?? "");
  const words = document.createDocumentFragment();
  words.append(...mention.childNodes);
  return words;
}
