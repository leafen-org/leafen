import { createContext, useContext } from "react";

// A published page opens its page links inside the share link or site it was
// opened from, as Notion's public pages do. The server projects each link:
// a target the visitor can open carries its current title, and one that is
// not published carries only the "unavailable" mark.

export type PublicPageHref = (pageId: string) => string;

export const PublicPageHrefContext = createContext<PublicPageHref | null>(null);

/** The public page's link builder, or null outside published pages. */
export function usePublicPageHref() {
  return useContext(PublicPageHrefContext);
}
