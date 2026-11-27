import { useEffect, useMemo, useState } from "react";
import { titleCustomEmojiPieces, titleWithClosedMentions } from "@/lib/closedMentions";
import { setMentionPageTitles } from "@/lib/peopleDirectory";
import type { PublicContentPageSummary } from "@/publicContentWire.generated";
import type { Page } from "@/types";
import { MentionImageIconRule, useMentionWindow } from "./mentionedOnScreen";
import { databaseMentionIconCss, databaseMentionSelector, MENTION_ICON_LIMIT, pageMentionIconCss, type MentionIconPage } from "./pageMentionIconCss";

// The most mentioned pages, and databases, whose icons a published page draws
// at a time, and the fewer whose pictures it fetches: those whose mentions are
// drawn nearest the view (useMentionWindow). The first 200 its text held
// alone were, and a mention further down showed the plain page however far
// one scrolled (2026-11-25).
const PUBLIC_MENTION_ICON_LIMIT = MENTION_ICON_LIMIT;
const PUBLIC_MENTION_PICTURE_LIMIT = 200;
const MENTION_SCAN_DELAY_MS = 200;
const MENTION_WITH_ICON = 'a[data-mention="page"][data-page-id]:is([data-mention-icon-type][data-mention-icon], [data-mention-has-content])';
const DATABASE_MENTION_WITH_ICON = 'a[data-mention="database"][data-database-id][data-mention-icon-type][data-mention-icon]';

/** The icons the page or database mentions on screen carry, every one, read again as the screen changes. */
function usePublicMentionIcons(workspaceId: string, selector: string, idOf: (element: HTMLElement) => string | undefined) {
  const [pages, setPages] = useState<readonly MentionIconPage[]>([]);
  useEffect(() => {
    let timer = 0;
    const scan = () => {
      const found = new Map<string, MentionIconPage>();
      for (const element of document.querySelectorAll<HTMLElement>(selector)) {
        const id = idOf(element) ?? "";
        if (!id || found.has(id)) continue;
        found.set(id, {
          id, icon: element.dataset.mentionIcon ?? "", iconType: (element.dataset.mentionIconType ?? "none") as Page["iconType"],
          hasContent: element.dataset.mentionHasContent === "true", inTrash: false, workspaceId,
        });
      }
      const next = [...found.values()];
      setPages((current) => current.length === next.length && current.every((page, index) => (
        page.id === next[index].id && page.icon === next[index].icon && page.iconType === next[index].iconType
        && page.hasContent === next[index].hasContent
      )) ? current : next);
    };
    const observer = new MutationObserver(() => {
      window.clearTimeout(timer);
      timer = window.setTimeout(scan, MENTION_SCAN_DELAY_MS);
    });
    observer.observe(document.body, { childList: true, subtree: true });
    scan();
    return () => {
      window.clearTimeout(timer);
      observer.disconnect();
    };
  }, [idOf, selector, workspaceId]);
  return pages;
}
/** Of the mentioned (`mentioned`, by `selector` and `idOf`), those drawn nearest the view, `limit` at most (useMentionWindow). */
function useNearestMentioned(
  mentioned: readonly MentionIconPage[], selector: string, idOf: (element: HTMLElement) => string | undefined, limit: number,
) {
  const taken = useMentionWindow(useMemo(() => mentioned.map(({ id }) => id), [mentioned]), selector, idOf, limit);
  return useMemo(() => mentioned.filter(({ id }) => taken.has(id)), [mentioned, taken]);
}

const pageIdOf = (element: HTMLElement) => element.dataset.pageId;
const databaseIdOf = (element: HTMLElement) => element.dataset.databaseId;
const hasImageIcon = (page: MentionIconPage) => (
  (page.iconType === "image" || page.iconType === "custom_emoji") && /^[A-Za-z0-9_-]+$/u.test(page.id)
);

/**
 * The icons of the pages a published page mentions, as the server read them
 * for the visitor, drawn as the app draws a mentioned page's icon: Notion's
 * public pages show a mention with its page's own icon (seen 2026-10-01),
 * and without one an empty document or, for a page holding anything, a
 * lined one, as a member's mention (pageMentionIconCss). A mentioned
 * database draws its own icon too, as a member's mention of it does.
 */
export function PublicMentionIconStyles({ workspaceId, listed = [] }: {
  workspaceId: string;
  /** The published pages the visitor reads: a mention of one whose title holds a custom emoji draws it, as a member's does. */
  listed?: readonly PublicContentPageSummary[];
}) {
  const pages = useNearestMentioned(usePublicMentionIcons(workspaceId, MENTION_WITH_ICON, pageIdOf), MENTION_WITH_ICON, pageIdOf, PUBLIC_MENTION_ICON_LIMIT);
  const databases = useNearestMentioned(usePublicMentionIcons(workspaceId, DATABASE_MENTION_WITH_ICON, databaseIdOf),
    DATABASE_MENTION_WITH_ICON, databaseIdOf, PUBLIC_MENTION_ICON_LIMIT);
  const pictured = useNearestMentioned(useMemo(() => pages.filter(hasImageIcon), [pages]), MENTION_WITH_ICON, pageIdOf, PUBLIC_MENTION_PICTURE_LIMIT);
  const picturedDatabases = useNearestMentioned(useMemo(() => databases.filter(hasImageIcon), [databases]),
    DATABASE_MENTION_WITH_ICON, databaseIdOf, PUBLIC_MENTION_PICTURE_LIMIT);
  useEffect(() => {
    const pieces = new Map(listed.flatMap((page) => {
      const shown = page.title ? titleCustomEmojiPieces(page.title, page.titleSpans) : null;
      return shown ? [[page.id, shown] as const] : [];
    }));
    setMentionPageTitles(new Map([...pieces.keys()].map((id) => {
      const page = listed.find((candidate) => candidate.id === id)!;
      return [id, titleWithClosedMentions(page.title, page.titleSpans)] as const;
    })), pieces);
  }, [listed]);
  const css = useMemo(() => pageMentionIconCss(pages) + "\n" + databaseMentionIconCss(databases), [databases, pages]);
  return (
    <>
      {css.trim() ? <style data-public-mention-icons="">{css}</style> : null}
      {pictured.map((page) => <MentionImageIconRule key={page.id} page={page} />)}
      {picturedDatabases.map((database) => (
        <MentionImageIconRule key={`database:${database.id}`} page={database} selector={databaseMentionSelector(database.id)} />
      ))}
    </>
  );
}
