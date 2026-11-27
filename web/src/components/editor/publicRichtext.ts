import { type MouseEvent, useMemo } from "react";
import { usePublicPageHref } from "@/lib/publicPageLinks";
import { pushRoute } from "@/lib/router";
import { blockItemText } from "./blockItemLabels";
import type { RichtextPageLinks } from "./richtextPageLinks";

// A published page shows its text as the server projected it for the
// visitor: a page or database it mentions opens inside the share link or
// site, and one the visitor cannot open shows only "사용 권한 없음", as
// Notion's public pages do (seen 2026-10-01), a page's leading to its own
// address in the app (user decision 2026-10-01).

/** The published page's links for its text, or null in the app. */
export function usePublicRichtextLinks(): RichtextPageLinks | null {
  const publicHref = usePublicPageHref();
  return useMemo(() => publicHref ? {
    pageHref: publicHref,
    unavailable: blockItemText("pageLink.mentionNoAccess"),
  } : null, [publicHref]);
}

/**
 * Opens a page, or a published database's page, a read-only text mentions
 * without leaving the app. A mention the visitor can't open goes to its
 * page's own address as a plain link does.
 */
export function openPageMention(event: MouseEvent<HTMLElement>) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  const anchor = (event.target as HTMLElement)
    .closest<HTMLAnchorElement>('a[data-mention="page"]:not([data-mention-unavailable]), a[data-mention="database"]');
  const href = anchor?.getAttribute("href");
  if (!anchor || !href || !event.currentTarget.contains(anchor)) return;
  event.preventDefault();
  pushRoute(href);
}
