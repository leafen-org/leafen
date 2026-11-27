import { textLinkHref } from "./richtextUrlPolicy";

/**
 * The whole address a link's copy gives: where it opens, or the address as
 * written where no URL holds that, as a page Markdown write keeps "<a b.com>"
 * (content_graph_text_links.go). A link's pill and a phone's link bar copy it.
 */
export function linkCopyAddress(href: string) {
  try {
    return new URL(textLinkHref(href), window.location.href).toString();
  } catch {
    return href.trim();
  }
}
