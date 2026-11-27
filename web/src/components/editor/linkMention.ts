import type { TextSpan } from "@/lib/types";

/** What an outside page's mention keeps of the page, as its preview gave it. */
export type LinkPreview = {
  title?: string;
  description?: string;
  iconUrl?: string;
  imageUrl?: string;
  /** The site's name, as the page gives it (og:site_name), or as its title ends. */
  provider?: string;
};

const webAddress = (value: string | undefined) => value && /^https?:/iu.test(value) ? value : "";

/**
 * A mention of an outside page, as Notion's link mention keeps it (seen
 * 2026-11-11, its API's link_mention): named by the page's title, or its
 * address where it has none, with its icon, its site's name, description
 * and picture — whatever made it (a pasted link's "멘션", a bookmark's
 * "멘션으로 전환").
 */
export function linkMentionSpan(href: string, preview: LinkPreview | null | undefined): TextSpan {
  const iconUrl = webAddress(preview?.iconUrl), imageUrl = webAddress(preview?.imageUrl);
  const provider = preview?.provider?.trim() ?? "", description = preview?.description?.trim() ?? "";
  return {
    text: preview?.title?.trim() || href,
    mention: "external",
    link: href,
    ...(iconUrl ? { iconUrl } : {}),
    ...(provider ? { provider } : {}),
    ...(description ? { description } : {}),
    ...(imageUrl ? { imageUrl } : {}),
  };
}

/**
 * Asked of a mention's link element by its card's "미리보기로 전환": the
 * editor holding it, where it can be changed, answers with the change that
 * turns it into a bookmark (NativeTiptapLinkMentionTurn).
 */
export const LINK_MENTION_TURN_REQUEST = "leafen:link-mention-turn";
export type LinkMentionTurnRequest = { turn?: () => void };

/** The change that turns this mention into a bookmark, where its text can be changed. */
export function linkMentionTurn(element: HTMLElement): (() => void) | undefined {
  const request: LinkMentionTurnRequest = {};
  element.dispatchEvent(new CustomEvent<LinkMentionTurnRequest>(LINK_MENTION_TURN_REQUEST, { detail: request, bubbles: true }));
  return request.turn;
}
