import { useEffect } from "react";
import { holdDocumentChrome } from "@/lib/documentChrome";
import { pageOpened } from "@/lib/typingFade";
import { tellRowDrawnWhole } from "@/lib/rowDrawnWhole";
import { usePageFaviconHref } from "@/components/page/pageFavicon";
import type { Page } from "@/types";
import { useTypedPageTitle } from "@/lib/pageNames";

/**
 * A row opened full names the window and its icon while it shows, as Notion's
 * (seen 2026-11-08); and, as another page opening does (TopBar), it shows its
 * bar whatever keys were pressed to get there — a search's for it among them
 * (Notion's, seen 2026-11-14). A database's page (`drawn`: its trash mark and
 * the page holding its database) tells the product it is the page drawn
 * (lib/rowDrawnWhole.ts), which the keys and search that read the open page
 * read wherever it opened full — over its database's page from search or the
 * trash too, as Notion's page at its own address (useDrawnPage.ts).
 */
export function useFullRowWindowTitle(active: boolean, title: string, row: Pick<Page, "icon" | "iconType"> & { workspaceId?: string; id?: string;
  drawn?: { inTrash: boolean; parentId: string } }) {
  // A title being typed in its head names the window at each key, a syllable still composing too, as a page's does.
  const named = useTypedPageTitle(active ? row.id : null, title) ?? title;
  const favicon = usePageFaviconHref(active ? row : null);
  useEffect(() => active ? holdDocumentChrome({ title: named, iconHref: favicon }) : undefined, [active, named, favicon]);
  useEffect(() => { if (active) pageOpened(); }, [active, row.id]);
  const { id } = row, inTrash = row.drawn?.inTrash, parentId = row.drawn?.parentId;
  useEffect(() => active && id && inTrash !== undefined
    ? tellRowDrawnWhole({ id, title, inTrash, parentId: parentId || null }) : undefined, [active, id, title, inTrash, parentId]);
}
