import type { TextSpan } from "@/lib/types";
import { escapeHtml } from "./richtextHtmlEscaping";
import { safeExternalMentionUrlValue } from "./richtextMentionPolicy";

/** How much of a link mention's title its line shows, as Notion's (seen 2026-11-11). */
const SHOWN_TITLE_CHARACTERS = 60;

/**
 * A mention of an outside page's title as its line shows it, as Notion's
 * (seen 2026-11-11: a 108-character GitHub title shows its first 60 and
 * "…"); the mention keeps the whole title, which its card shows.
 */
export function linkMentionShownTitle(title: string) {
  const characters = Array.from(title);
  return characters.length > SHOWN_TITLE_CHARACTERS ? `${characters.slice(0, SHOWN_TITLE_CHARACTERS).join("")}…` : title;
}

/**
 * A mention of an outside page, as Notion's link mention (seen 2026-11-11):
 * the page's icon, or a grey link where it has none, the site's name in
 * grey where the page gave one ("GitHub", "MDN Web Docs"), then its title
 * in medium weight over a light underline. What it keeps of the page's
 * preview rides on the link for its card (LinkMentionPreview).
 */
export function linkMentionHtml(span: TextSpan, href: string, titleHtml: string) {
  const iconUrl = safeExternalMentionUrlValue(span.iconUrl);
  const imageUrl = safeExternalMentionUrlValue(span.imageUrl);
  const provider = span.provider?.trim() ?? "";
  const description = span.description?.trim() ?? "";
  const icon = iconUrl
    ? `<img src="${escapeHtml(iconUrl)}" alt="" draggable="false" data-mention-icon="external" />`
    : `<span data-mention-icon="link"></span>`;
  const data = [
    iconUrl ? ` data-icon-url="${escapeHtml(iconUrl)}"` : "",
    provider ? ` data-provider="${escapeHtml(provider)}"` : "",
    description ? ` data-description="${escapeHtml(description)}"` : "",
    imageUrl ? ` data-image-url="${escapeHtml(imageUrl)}"` : "",
    ` data-title="${escapeHtml(span.text ?? "")}"`,
  ].join("");
  const site = provider ? `<span data-mention-provider="">${escapeHtml(provider)}</span>` : "";
  return `<a href="${escapeHtml(href)}" data-mention="external"${data} contenteditable="false">${icon}${site}<span data-mention-label="external">${titleHtml}</span></a>`;
}

/** What a mention's link element keeps of its page's preview, read back from linkMentionHtml's. */
export function linkMentionPreviewOf(element: HTMLElement, safe: (value: string | undefined) => string): Partial<TextSpan> {
  const iconUrl = safe(element.dataset.iconUrl);
  const imageUrl = safe(element.dataset.imageUrl);
  const provider = element.dataset.provider?.trim() ?? "";
  const description = element.dataset.description?.trim() ?? "";
  return {
    ...(iconUrl ? { iconUrl } : {}),
    ...(provider ? { provider } : {}),
    ...(description ? { description } : {}),
    ...(imageUrl ? { imageUrl } : {}),
  };
}
