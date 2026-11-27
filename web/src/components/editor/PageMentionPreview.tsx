import { useEffect, useState } from "react";
import { pagePlace, placeUnder, type Place } from "@/lib/pagePath";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import { pageNamesNow } from "@/lib/pageNames";
import type { Page, Session } from "@/types";
import type { DatabaseMentionPreviewResponse, DataSourceMentionPreviewResponse, PagePreviewResponse } from "@/workspaceSearchWire.generated";
import { pageTrashRoot } from "@/lib/pageTrash";
import { MentionPreviewCard, useMentionHover, type MentionCard } from "./mentionPreviewCard";
import { databasePreviewOnce, dataSourcePreviewOnce, pagePreviewAgain, pagePreviewKnown } from "./pagePreviews";
import { TrashedMentionTip, type MentionTrashMark } from "./TrashedMentionTip";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { useCustomEmojiTitles } from "@/components/page/TitleWithCustomEmoji";
import { pagesByIdOf } from "@/lib/pageIndex";
import { useGraphChangesHeard } from "@/lib/sidebarGraphFollow";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import { yTextToRichText } from "@/lib/collaborationCrdtCodec";
import { usePageTextHere } from "@/lib/pageTextsHere";
import type { PreviewLineBlock } from "@/components/search/SearchPagePreview";
import type * as Y from "yjs";

/**
 * The card a page, database or data source mention shows while the pointer
 * rests on it, as Notion's (seen 2026-09-30, a data source's 2026-11-24): a
 * card with the icon, the place, the title and, for a page, its first blocks
 * drawn small, for a database or data source a picture of a table; no
 * buttons. It stays while the pointer is on the mention (useMentionHover). A
 * page block's row shows its page's card too. One of what is in the trash, itself
 * or under a page there, shows who sent it there and when instead
 * (TrashedMentionTip), the one place a mention's preview is decided.
 */
const NO_PAGES: Readonly<Record<string, Page>> = {};
const NO_IDS: readonly string[] = [];
// A page open here is read again this long after writing in it pauses, for its lines made, moved or gone: the
// server keeps them some 1.5–2 s after (measured 2026-11-26 at 127.0.0.1). Their words show at once (wordsHere).
const WRITTEN_READS_MS = [300, 1_500, 3_000];

type MentionPreview = PagePreviewResponse | DatabaseMentionPreviewResponse | DataSourceMentionPreviewResponse;

export function PageMentionPreview({ pages, session, workspaceId }: {
  pages: Page[];
  session: Session;
  workspaceId: string;
}) {
  const { shown, cardRef } = useMentionHover();
  const [previews, setPreviews] = useState<Record<string, MentionPreview | null>>({});
  // The list's pages are looked into only while a card shows: a new list
  // (a title saved) drew this again, and a 44,000-page workspace's index and
  // titles were made again for no card, some 30 ms at CPU ×2 (2026-11-15).
  const pagesById = shown ? pagesByIdOf(pages) : NO_PAGES;
  // A page under a row reads its path from the server, as the card names it (Notion's).
  const shownPage = shown?.kind === "page" ? pagesById[shown.id] : undefined;
  usePathsAboveShown(shownPage ? [shownPage] : [], pagesById);
  const emojiTitles = useCustomEmojiTitles(shown ? pages : undefined);

  // A database's or data source's preview is read once while the app is open
  // (its mentions' marks read it again as it changes, pagePreviews.ts); a
  // page's again each time its card shows, as the page may have changed since
  // (its body edited in a peek or elsewhere), what was read before standing
  // meanwhile, and again as it changes while the card shows (useShownPageChanges),
  // as Notion's card follows the page (seen 2026-11-26 in Chrome at 1440, two
  // tabs: letters typed into the page under its own mention's card showed in
  // the card at once) — a page open here with the words of its lines as they
  // are written (wordsHere). A listed page in the trash reads nothing: the list
  // says who sent it.
  const key = shown ? `${shown.kind}:${shown.id}` : "";
  const listedInTrash = shown?.kind === "page" && Boolean(shownPage && pageTrashRoot(shownPage, (id) => pagesById[id]));
  const followed = shown?.kind === "page" && !listedInTrash ? shown.id : null;
  const changes = useShownPageChanges(followed);
  const here = usePageTextHere(followed);
  useEffect(() => {
    if (!key || listedInTrash) return;
    let current = true;
    const at = key.indexOf(":"), kind = key.slice(0, at), id = key.slice(at + 1);
    const read: Promise<MentionPreview | null> = kind === "database" ? databasePreviewOnce(session, id)
      : kind === "dataSource" ? dataSourcePreviewOnce(session, id) : pagePreviewAgain(session, id);
    void read.then((preview) => {
      if (current) setPreviews((all) => ({ ...all, [key]: preview }));
    });
    return () => { current = false; };
  }, [key, listedInTrash, session, changes]);

  if (!shown) return null;
  // A page the list lacks goes by its last read until its own comes back, as
  // its mention's mark does (useUnlistedMentionedPages): one in the trash shows
  // its tip at once rather than a card first.
  const preview = key in previews || shown.kind !== "page" || pagesById[shown.id] ? previews[key] : pagePreviewKnown(session, shown.id);
  const trash = mentionTrashMarkOf(shown.kind, shown.id, preview, pagesById);
  if (trash) return <TrashedMentionTip shown={shown} mark={trash} session={session} tipRef={cardRef} />;
  const card = shown.kind === "database"
    ? databaseCard(preview as DatabaseMentionPreviewResponse | null | undefined, pagesById, workspaceId)
    : shown.kind === "dataSource"
      ? dataSourceCard(preview as DataSourceMentionPreviewResponse | null | undefined, pagesById, workspaceId)
      : pageCard(shown.id, preview as PagePreviewResponse | null | undefined, pagesById, workspaceId);
  // What the reader can't open shows nothing; its place draws its titles' custom emoji.
  return card ? <MentionPreviewCard shown={shown} card={{ ...card, emojiTitles, blocks: wordsHere(card.blocks, here.document) }}
    cardRef={cardRef} /> : null;
}

/**
 * A page's first lines with their words as written where the page is open
 * here (pageTextsHere.ts), the server keeping them some 1.5–2 s after they
 * are typed: each line it read from the page's own text (its blockId) takes
 * that text's words and marks now. A line holding a suggested edit, or a
 * mention the reader can't open, keeps the server's, which decides how those
 * show; drawn again as the text changes (usePageTextHere).
 */
function wordsHere(blocks: readonly PreviewLineBlock[] | undefined, document: Y.Doc | null) {
  if (!blocks?.length || !document) return blocks;
  return blocks.map((block) => {
    const root = block.blockId ? BLOCK_TEXT_ROOT_PREFIX + block.blockId : "";
    if (!root || !document.share.has(root) || block.spans?.some((span) => (span as { unavailable?: boolean }).unavailable)) return block;
    const spans = yTextToRichText(document.getText(root)) ?? [];
    if (spans.some((span) => span.suggestion || span.suggestionId || span.suggestionBefore)) return block;
    const text = spans.map((span) => span.text).join("");
    return block.type === "code" ? { ...block, text } : { ...block, text: text.trim(), spans };
  });
}

/**
 * How many times the page a card shows changed while it shows: its lines
 * changed on another screen or by an agent, as the change socket hears
 * (useGraphChangesHeard), or something written in it where it is open here,
 * read back as the server comes to keep it (WRITTEN_READS_MS).
 */
function useShownPageChanges(pageId: string | null): number {
  const heard = useGraphChangesHeard(pageId ? [pageId] : NO_IDS).get(pageId ?? "") ?? 0;
  const [written, setWritten] = useState(0);
  useEffect(() => {
    const texts = pageId ? document.querySelectorAll(`[data-editor-page="${CSS.escape(pageId)}"] [data-native-editor-content]`) : [];
    if (!texts.length) return undefined;
    let reads: number[] = [];
    const stop = () => { for (const read of reads) window.clearTimeout(read); };
    const observer = new MutationObserver(() => {
      stop();
      reads = WRITTEN_READS_MS.map((after) => window.setTimeout(() => setWritten((count) => count + 1), after));
    });
    for (const text of texts) observer.observe(text, { childList: true, characterData: true, subtree: true });
    return () => { observer.disconnect(); stop(); };
  }, [pageId]);
  return heard + written;
}

/**
 * Who sent what a mention names to the trash and when, where it is there: a
 * listed page by the page in the trash holding it, itself or the nearest
 * above (pageTrashRoot, as its banner names it); a page the list lacks (a
 * database page), a database or a data source by its preview
 * (mention_trash_mark.go). Null for one out of the trash, or one the trash
 * keeps no record of.
 */
function mentionTrashMarkOf(
  kind: string, id: string, preview: MentionPreview | null | undefined, pagesById: Record<string, Page>,
): MentionTrashMark | null {
  const page = kind === "page" ? pagesById[id] : undefined;
  if (page) {
    const root = pageTrashRoot(page, (pageId) => pagesById[pageId]);
    return root && (root.trashedAt || root.lastEditedBy) ? { at: root.trashedAt ?? "", by: root.lastEditedBy ?? "" } : null;
  }
  return preview?.inTrash && (preview.trashedAt || preview.trashedBy) ? { at: preview.trashedAt ?? "", by: preview.trashedBy ?? "" } : null;
}

export function pageCard(
  pageId: string,
  preview: PagePreviewResponse | null | undefined,
  pagesById: Record<string, Page>,
  workspaceId: string,
): MentionCard | null {
  const page = pagesById[pageId];
  if (!page && preview === null) return null;
  // A database page sits in its database, under the page holding it:
  // "Top / … / Database", as Notion's card shows it.
  const location: Place = page
    ? pagePlace(page, pagesById)
    : preview?.containerPageId
      ? placeUnder(preview.containerPageId, pagesById, preview.databaseTitle?.trim() || untitledPageDisplayTitle("database"))
      : [];
  // Its icon says whether anything is in it as the page is read now, or as
  // the list knows it until then (an empty document or a lined one).
  const hasContent = preview ? (preview.blocks?.length ?? 0) > 0 : page?.hasContent;
  return {
    icon: page ? { ...page, hasContent } : { icon: preview?.icon ?? "", iconType: (preview?.iconType || "none") as Page["iconType"], workspaceId, hasContent },
    fallback: "file",
    location,
    title: page ? pageNamesNow.title(page) : preview?.title || preview?.untitledName?.trim()
      || untitledPageDisplayTitle(listedPageNameKind(preview)),
    named: page,
    lines: preview?.lines ?? [],
    blocks: preview?.blocks,
    // Until its blocks are read, a page the list says holds something keeps
    // its body's room, so its card doesn't grow under the pointer.
    reserve: preview === undefined && page?.hasContent === true,
    table: false,
  };
}

/**
 * A database's card: a full-page database as its page, with that page's
 * icon and place; a database in a page with a table icon, placed under that
 * page. Both end in a picture of a table, as Notion's does. One in the trash
 * shows none, as a page's mention in the trash shows none.
 */
function databaseCard(
  database: Pick<DatabaseMentionPreviewResponse, "pageId" | "title" | "sourceTitles" | "inTrash"> | null | undefined,
  pagesById: Record<string, Page>,
  workspaceId: string,
): MentionCard | null {
  if (!database || database.inTrash) return null;
  const holder = pagesById[database.pageId];
  const fullPage = holder?.kind === "database";
  return {
    icon: fullPage && holder ? holder : { icon: "", iconType: "none", workspaceId },
    fallback: "database",
    location: holder ? (fullPage ? pagePlace(holder, pagesById) : placeUnder(holder.id, pagesById)) : [],
    // A full-page database goes by its page's name, as the sidebar shows it; one without a name of several sources by them.
    title: (fullPage && holder ? pageNamesNow.title(holder) : database.title.trim())
      || untitledPageDisplayTitle({ kind: "database", sourceTitles: database.sourceTitles }),
    named: fullPage ? holder : undefined,
    lines: [],
    table: true,
  };
}

/**
 * A data source's card, as Notion's (seen 2026-11-24 in Chrome at 1440: the
 * only source of a full-page database): its database's card with the
 * source's own name and icon, the cylinder where it has none.
 */
function dataSourceCard(
  source: DataSourceMentionPreviewResponse | null | undefined,
  pagesById: Record<string, Page>,
  workspaceId: string,
): MentionCard | null {
  const card = source ? databaseCard({ pageId: source.pageId, title: source.title, inTrash: source.inTrash }, pagesById, workspaceId) : null;
  if (!card || !source) return null;
  const titled = source.title.trim();
  return {
    ...card,
    icon: { icon: source.icon ?? "", iconType: (source.iconType || "none") as Page["iconType"], workspaceId },
    fallback: "dataSource",
    title: titled || card.title,
    named: undefined,
  };
}
