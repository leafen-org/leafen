import { useEffect, useMemo, useRef, useState } from "react";
import { i18next } from "@/i18n";
import { setMarkedMentionTitles } from "@/lib/markedMentionTitles";
import { pageMapOf } from "@/lib/pageIndex";
import { useGraphChangesHeard } from "@/lib/sidebarGraphFollow";
import type { Page, Session } from "@/types";
import { keptFurther, mentionedNow, useMentionWindow } from "./mentionedOnScreen";
import type { MentionIconPage } from "./pageMentionIconCss";
import { forgetPagePreviews, pagePreviewAgain, pagePreviewOnce, readShownPreviews } from "./pagePreviews";

/** The page mentions on screen with their pages' IDs. */
export const MENTIONED_PAGES = ':is(a[data-mention="page"], span[data-mention="page"][href])[data-page-id]';
/** A page mention that came in marks, closed or missing, by its page's link alone (richtextPageLinks.ts). */
export const MARKED_PAGES = 'a[data-mention="page"][href]:is([data-mention-missing], [data-mention-unavailable])';
// A page mention, by its page's ID or the link of one that came in marks.
const NAMED_PAGES = `${MENTIONED_PAGES}, ${MARKED_PAGES}`;

export const pageIdOf = (element: HTMLElement) => element.dataset.pageId;
export const markedPageIdOf = (element: HTMLElement) => {
  const id = /^\/p\/([^/?#]+)$/u.exec(element.getAttribute("href") ?? "")?.[1];
  try { return id ? decodeURIComponent(id) : undefined; } catch { return undefined; }
};
const namedPageIdOf = (element: HTMLElement) => pageIdOf(element) ?? markedPageIdOf(element);

// The most mentioned pages outside the workspace's page list read at a time,
// those nearest the view (useMentionWindow).
const MENTION_OTHER_PAGE_LIMIT = 100;
// The most pages holding their databases followed for them, beside the
// sidebar's (the server follows 64 a screen, workspaceChangeGraphsMax): those
// of the database pages mentioned nearest the view.
const MENTION_HOLDER_FOLLOW_LIMIT = 16;
// How long the page list stays still before the mentioned pages are read again.
const MENTIONED_PAGES_DELAY_MS = 1000;

type Read = {
  ids: ReadonlySet<string>;
  list?: readonly MentionIconPage[];
  heard: ReadonlyMap<string, number>;
  /** The page holding each one's database, as its preview said. */
  holders: ReadonlyMap<string, string>;
};

/**
 * The pages whose mentions are on screen that the workspace's page list
 * doesn't name — database rows and templates, a page in the trash, and those
 * that came in marks, closed or missing, when the server sent them — each
 * read from its preview, once for all it gives: its title, which names its
 * mentions (markedMentionTitles.ts) as a listed page's does, untitled by what
 * its database calls it, in the reader's language whatever words its writer
 * kept (an API's "New page") (2026-11-23); and for those the list lacks,
 * the icon it gives — or, without one, whether anything is in it — and
 * whether it is in the trash, itself, with its database or under a page
 * there, as Notion's mentions say (seen 2026-11-24 in Chrome at 1440, two
 * tabs), one that came in marks taking its icon with its name as it opens
 * (given only to those already drawn with their IDs, a row read while closed
 * and opened again was named and kept the plain page's icon, 2026-11-27).
 * Read at once as one comes on screen; again once the page list has stayed
 * still after a change (a share may open one that came closed); and again
 * at once when the page holding its database changed on another screen,
 * through the API or by an agent (useGraphChangesHeard), as Notion's
 * mention takes a row's new name within a second (2026-11-25, Chrome at
 * 1440: 0.6 s after an API rename, 1.2 s after
 * one typed in another tab). Read apart, the title kept what it first read
 * while the icon and the trash followed, and a row renamed elsewhere kept
 * its old name on an open page until it opened again (2026-11-25). Those
 * nearest the view are read (useMentionWindow), the others as they come
 * nearer, read anew then, as they may have changed meanwhile.
 */
export function useUnlistedMentionedPages(
  marked: ReadonlySet<string>,
  mentioned: ReadonlySet<string>,
  pages: readonly MentionIconPage[],
  known: readonly MentionIconPage[],
  session: Session | undefined,
  workspaceId: string | undefined,
) {
  const [others, setOthers] = useState<readonly MentionIconPage[]>([]);
  const [holders, setHolders] = useState<readonly string[]>([]);
  // The pages holding their databases, followed over the change socket, which tells of a row renamed,
  // sent to the trash or back elsewhere, and of them all as it opens again after it was closed.
  const heard = useGraphChangesHeard(holders);
  const titles = useRef<ReadonlyMap<string, string>>(new Map());
  const read = useRef<Read>({ ids: new Set(), heard, holders: new Map() });
  // The live list names its pages; one only in the trash is named here, its icon given by the trash's list.
  const named = useMemo(() => {
    const listed = pageMapOf(pages);
    return [...new Set([...marked, ...mentioned])].filter((id) => !listed.has(id));
  }, [marked, mentioned, pages]);
  const drawn = useRef({ named, known });
  drawn.current = { named, known };
  const taken = useMentionWindow(named, NAMED_PAGES, namedPageIdOf, MENTION_OTHER_PAGE_LIMIT);
  useEffect(() => {
    const ids = [...taken];
    if (!session || ids.length === 0) {
      titles.current = new Map();
      setMarkedMentionTitles(titles.current);
      setOthers((current) => current.length === 0 ? current : []);
      setHolders((current) => current.length === 0 ? current : []);
      return;
    }
    const was = read.current;
    const unread = ids.some((id) => !was.ids.has(id));
    const listChanged = was.list !== known;
    const holderChanged = (id: string) => {
      const holder = was.holders.get(id);
      return holder !== undefined && heard.get(holder) !== was.heard.get(holder);
    };
    const heldChanged = ids.some(holderChanged);
    // Those read before are read again after a change of the list or of the page holding them; those
    // coming on screen or only nearer the view are read as they are.
    const again = (id: string) => was.ids.has(id) && (listChanged || holderChanged(id));
    // Those further from the view are read anew as they come nearer.
    forgetPagePreviews(session, drawn.current.named.filter((id) => !taken.has(id)));
    return readShownPreviews(unread || heldChanged ? 0 : MENTIONED_PAGES_DELAY_MS,
      () => Promise.all(ids.map((id) => (again(id) ? pagePreviewAgain : pagePreviewOnce)(session, id, "refused"))),
      (previews) => {
        read.current = {
          ids: taken, list: known, heard,
          holders: new Map(previews.flatMap((preview, index) => preview?.containerPageId ? [[ids[index], preview.containerPageId] as const] : [])),
        };
        // Those further from the view keep what they showed, title, icon and trash alike, while drawn.
        const still = mentionedNow(NAMED_PAGES, namedPageIdOf);
        // An untitled row by what it is called while it has none, as its card names it.
        titles.current = keptFurther(titles.current, new Map(previews.flatMap((preview, index) => preview
          ? [[ids[index], preview.title || preview.untitledName || i18next.t("common:labels.newPage")] as const] : [])),
        taken, still);
        setMarkedMentionTitles(titles.current);
        const knownIds = pageMapOf(drawn.current.known);
        // Each read gives its icon with its title, one that came in marks too: the title just read draws its
        // mention with its ID, which the icon's rule names it by (pageMentionIconCss.ts).
        const now = new Map(workspaceId ? previews.flatMap((preview, index): Array<readonly [string, MentionIconPage]> => {
          const id = ids[index];
          if (!preview || knownIds.has(id)) return [];
          const hasContent = (preview.blocks?.length ?? 0) > 0, inTrash = preview.inTrash === true;
          if (preview.iconType !== "none" && preview.icon) {
            return [[id, { id, icon: preview.icon, iconType: preview.iconType as Page["iconType"], inTrash, workspaceId, hasContent }]];
          }
          return hasContent || inTrash ? [[id, { id, icon: "", iconType: "none", inTrash, workspaceId, hasContent }]] : [];
        }) : []);
        setOthers((current) => {
          const next = [...keptFurther(new Map(current.map((page) => [page.id, page])), now, taken, still).values()];
          return current.length === next.length && current.every((page, index) => (
            page.id === next[index].id && page.icon === next[index].icon && page.iconType === next[index].iconType
            && page.hasContent === next[index].hasContent && page.inTrash === next[index].inTrash
          )) ? current : next;
        });
        const held = [...new Set(previews.flatMap((preview) => preview?.containerPageId ? [preview.containerPageId] : []))]
          .slice(0, MENTION_HOLDER_FOLLOW_LIMIT);
        setHolders((current) => current.length === held.length && current.every((id, index) => id === held[index]) ? current : held);
      });
  }, [heard, known, session, taken, workspaceId]);
  return others;
}
