import { useTranslation } from "react-i18next";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  mentionedBlockKey, mentionPageTitlesNow, setMentionDatabaseTitles, setMentionedBlocks, setMentionPageTitles, tellMentionPageTitles,
  type MentionedBlock,
} from "@/lib/peopleDirectory";
import { titleCustomEmojiPieces, titleWithClosedMentions } from "@/lib/closedMentions";
import type { MentionTitlePieces } from "@/lib/mentionTitlePieces";
import type { Page, Session } from "@/types";
import { databaseMentionIconCss, databaseMentionSelector, MENTION_ICON_LIMIT, pageMentionIconCss, type MentionIconPage } from "./pageMentionIconCss";
import {
  blockMentionPreviewAgain, blockMentionPreviewOnce, databasePreviewAgain, databasePreviewOnce, readShownPreviews,
} from "./pagePreviews";
import { keptFurther, MentionImageIconRule, mentionedNow, useMentionedIds, useMentionWindow } from "./mentionedOnScreen";
import { DataSourceMentionStyles, SHOWN_DATA_SOURCES, shownDataSourceIdOf } from "./DataSourceMentionStyles";
import { dataSourceMentionAddress } from "@/lib/dataSourceMentionNames";
import { listEditsOf, pageMapOf } from "@/lib/pageIndex";
import { i18next } from "@/i18n";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useListedPageNames } from "@/lib/pageNames";
import { useMentionReach } from "./useMentionReach";
import { MARKED_PAGES, markedPageIdOf, MENTIONED_PAGES, pageIdOf, useUnlistedMentionedPages } from "./unlistedMentionedPages";

// The most pages whose image icon is fetched for the mentions on screen,
// those nearest the view (useMentionWindow).
const MENTION_IMAGE_ICON_LIMIT = 200;
// The most databases and blocks read at a time for the mentions on screen,
// those nearest the view (useMentionWindow).
const MENTION_OTHER_PAGE_LIMIT = 100;

const MENTIONED_DATABASES = '[data-mention="database"][data-database-id]';
const MENTIONED_BLOCKS = 'a[data-mention="page"][data-page-id][data-mention-block-id]';
// A database mention drawn closed or missing, by the database it keeps
// (richtextPageLinks.ts keptTargetAttribute) — a data source's apart (DataSourceMentionStyles.tsx).
const MARKED_DATABASES = `[data-mention-database]:is([data-mention-unavailable], [data-mention-missing])`
  + `:not([data-mention-database^="${dataSourceMentionAddress("")}"])`;
const SHOWN_DATABASES = `${MENTIONED_DATABASES}, ${MARKED_DATABASES}`;
// How long the page list stays still before mentioned databases' names are read again.
const DATABASE_NAMES_DELAY_MS = 1000;

const databaseIdOf = (element: HTMLElement) => element.dataset.databaseId;
const markedDatabaseIdOf = (element: HTMLElement) => element.dataset.mentionDatabase;
const blockKeyOf = (element: HTMLElement) => element.dataset.pageId && element.dataset.mentionBlockId
  ? mentionedBlockKey(element.dataset.pageId, element.dataset.mentionBlockId) : undefined;
const shownDatabaseIdOf = (element: HTMLElement) => databaseIdOf(element) ?? markedDatabaseIdOf(element);

/** The IDs on screen as a list, the same while they are. */
function useIdList(ids: ReadonlySet<string>) {
  return useMemo(() => [...ids], [ids]);
}

/** A mentioned database as its preview read it: the page holding it, its own icon (database_mention_icon.go), whether it is in the trash. */
type MentionedDatabase = { id: string; pageId: string; icon: string; iconType: string; inTrash: boolean };

/**
 * The databases mentioned on screen by their names and icons now, and
 * whether each is in the trash, which their mentions read as Notion's do
 * (names seen 2026-11-03, icons 2026-11-22, the trash 2026-11-24): read at
 * once when another comes on screen, and again once the page list has stayed
 * still after a change. A full-page database's page in the list gives its
 * icon as it is drawn there.
 */
function useMentionedDatabases(
  mentioned: ReadonlySet<string>, pages: readonly MentionIconPage[], session: Session | undefined, workspaceId: string | undefined,
) {
  const [read, setRead] = useState<readonly MentionedDatabase[]>([]);
  const shown = useRef<{ ids: ReadonlySet<string>; pages?: readonly MentionIconPage[] }>({ ids: new Set() });
  const names = useRef<{ titles: ReadonlyMap<string, string>; pieces: ReadonlyMap<string, MentionTitlePieces> }>({ titles: new Map(), pieces: new Map() });
  const taken = useMentionWindow(useIdList(mentioned), SHOWN_DATABASES, shownDatabaseIdOf, MENTION_OTHER_PAGE_LIMIT);
  useEffect(() => {
    const ids = [...taken];
    if (!session || ids.length === 0) return;
    const was = shown.current;
    const unread = ids.some((id) => !was.ids.has(id));
    // As others only came nearer the view, those read at the last change aren't asked again.
    const kept = (id: string) => unread && was.pages === pages && was.ids.has(id);
    return readShownPreviews(unread ? 0 : DATABASE_NAMES_DELAY_MS,
      () => Promise.all(ids.map((id) => (kept(id) ? databasePreviewOnce : databasePreviewAgain)(session, id, "refused"))),
      (previews) => {
        shown.current = { ids: taken, pages };
        const still = mentionedNow(SHOWN_DATABASES, shownDatabaseIdOf);
        const pieces = new Map<string, MentionTitlePieces>();
        const titles = new Map(previews.flatMap((preview, index) => {
          // A full-page database goes by its page's name, as its card's, its custom emoji drawn.
          const holder = preview ? pages.find((page) => page.id === preview.pageId) : undefined;
          const named = holder?.kind === "database" ? holder : undefined;
          // A name naming a page closed to the reader says so, as a plain title does (closedMentions.ts);
          // one without a name is "새 데이터베이스", of several sources "A 및 B", in the reader's language
          // whatever words its mention was written with (an API's "New database"), as Notion's (seen 2026-11-18).
          const title = titleWithClosedMentions((named ? named.title : preview?.title)?.trim() ?? "", named?.titleSpans)
            || (preview ? untitledPageDisplayTitle({ kind: "database", sourceTitles: preview.sourceTitles }) : "");
          const drawn = named ? titleCustomEmojiPieces(named.title ?? "", named.titleSpans) : null;
          if (title && drawn) pieces.set(ids[index], drawn);
          return title ? [[ids[index], title] as const] : [];
        }));
        names.current = { titles: keptFurther(names.current.titles, titles, taken, still), pieces: keptFurther(names.current.pieces, pieces, taken, still) };
        setMentionDatabaseTitles(names.current.titles, names.current.pieces);
        const now = new Map(previews.flatMap((preview, index) => preview ? [[ids[index], {
          id: ids[index], pageId: preview.pageId, icon: preview.icon ?? "", iconType: preview.iconType ?? "", inTrash: preview.inTrash === true,
        }] as const] : []));
        setRead((was) => {
          const next = [...keptFurther(new Map(was.map((database) => [database.id, database])), now, taken, still).values()];
          return was.length === next.length && was.every((database, index) => (
            database.id === next[index].id && database.pageId === next[index].pageId
            && database.icon === next[index].icon && database.iconType === next[index].iconType && database.inTrash === next[index].inTrash
          )) ? was : next;
        });
      });
  }, [pages, session, taken]);
  return useMemo(() => {
    if (!workspaceId) return [];
    const listed = pageMapOf(pages);
    return read.flatMap((database): MentionIconPage[] => {
      if (!mentioned.has(database.id)) return [];
      const holder = listed.get(database.pageId);
      const own = holder?.kind === "database" && holder.iconType !== "none" && holder.icon
        ? { icon: holder.icon, iconType: holder.iconType } : database;
      const drawn = Boolean(own.icon && own.iconType && own.iconType !== "none");
      // One without an icon of its own keeps its table, and says it is in the trash.
      return drawn || database.inTrash ? [{
        id: database.id, icon: drawn ? own.icon : "", iconType: (drawn ? own.iconType : "none") as Page["iconType"],
        inTrash: database.inTrash, workspaceId,
      }] : [];
    });
  }, [mentioned, pages, read, workspaceId]);
}

/**
 * The pages whose icons the mentions draw, those mentioned on screen first,
 * nearest the view (useMentionWindow), as the rules hold MENTION_ICON_LIMIT
 * at most: in the list's order a workspace with more pages to draw (an icon,
 * or anything in them) left a mention of one past them the plain page
 * (2026-11-25). While all fit, the list as it is.
 */
function useMentionedFirst(pages: readonly MentionIconPage[], mentioned: ReadonlySet<string>) {
  const over = pages.length > MENTION_ICON_LIMIT;
  const named = useMemo(() => over ? pages.flatMap((page) => mentioned.has(page.id) ? [page.id] : []) : [], [mentioned, over, pages]);
  const near = useMentionWindow(named, MENTIONED_PAGES, pageIdOf, MENTION_ICON_LIMIT);
  return useMemo(() => !over || near.size === 0 ? pages
    : [...pages.filter((page) => near.has(page.id)), ...pages.filter((page) => !near.has(page.id))], [near, over, pages]);
}

/** Two sets of IDs on screen as one, the same while they are. */
function useBothIds(first: ReadonlySet<string>, second: ReadonlySet<string>) {
  return useMemo(() => second.size === 0 ? first : new Set([...first, ...second]), [first, second]);
}

/**
 * The blocks mentioned on screen by their words and their page's title now,
 * which their mentions read as Notion's do (seen 2026-11-11): read at once
 * when another comes on screen, and again once the page list has stayed still
 * after a change.
 */
function useMentionedBlocks(mentioned: ReadonlySet<string>, pages: readonly MentionIconPage[], session: Session | undefined) {
  const shown = useRef<{ keys: ReadonlySet<string>; pages?: readonly MentionIconPage[] }>({ keys: new Set() });
  const blocks = useRef<ReadonlyMap<string, MentionedBlock>>(new Map());
  const taken = useMentionWindow(useIdList(mentioned), MENTIONED_BLOCKS, blockKeyOf, MENTION_OTHER_PAGE_LIMIT);
  useEffect(() => {
    const keys = [...taken];
    if (!session || keys.length === 0) return;
    const was = shown.current;
    const unread = keys.some((key) => !was.keys.has(key));
    const kept = (key: string) => unread && was.pages === pages && was.keys.has(key);
    return readShownPreviews(unread ? 0 : DATABASE_NAMES_DELAY_MS, () => Promise.all(keys.map((key) => {
      const at = key.lastIndexOf("#");
      return (kept(key) ? blockMentionPreviewOnce : blockMentionPreviewAgain)(session, key.slice(0, at), key.slice(at + 1), "refused");
    })), (previews) => {
      shown.current = { keys: taken, pages };
      blocks.current = keptFurther(blocks.current, new Map(previews.flatMap((preview, index) => (
        preview ? [[keys[index], { title: preview.title, words: preview.words }] as const] : []
      ))), taken, mentionedNow(MENTIONED_BLOCKS, blockKeyOf));
      setMentionedBlocks(blocks.current);
    });
  }, [pages, session, taken]);
}

/**
 * The workspace's pages named by the list drawn now, for the owner of pages'
 * names (lib/pageNames.ts) and for mentions: their titles are kept as it
 * draws, before what it draws reads them, and the places drawn apart (a
 * table's words, an editor's mention) hear of them once it is drawn. Told
 * after it was drawn, the whole app was drawn again for each change of the
 * list (2026-11-12).
 */
export function useMentionPageTitles(pages: readonly Page[]) {
  useListedPageNames(pages);
  useMemo(() => keepMentionPageTitles(pages), [pages]);
  useEffect(() => tellMentionPageTitles(), [pages]);
}

type KeptTitles = { pages: readonly MentionIconPage[]; titles: ReadonlyMap<string, string>; pieces: ReadonlyMap<string, MentionTitlePieces> };
type TitleMaps = { titles: Map<string, string>; pieces: Map<string, MentionTitlePieces> };
// The list the titles were last kept from, and what they were.
let keptTitles: KeptTitles | null = null;

// An untitled page's mention goes by what it is called, "새 페이지" or
// "새 데이터베이스" ("A 및 B" of several sources), in the reader's language
// whatever words it was written with (an API's "New page"), as Notion's (seen
// 2026-11-18 in two tabs). The two plain names are read once per language, as
// a large workspace's every untitled page is named so at once.
let untitledNames: { language: string; page: string; database: string } | null = null;
function untitledMentionTitle(page: MentionIconPage) {
  if (page.untitledName?.trim()) return page.untitledName.trim();
  if (page.kind === "database" && (page.viewBlock || (page.sourceTitles?.length ?? 0) > 0)) return untitledPageDisplayTitle(page);
  if (untitledNames?.language !== i18next.language) {
    untitledNames = { language: i18next.language, page: untitledPageDisplayTitle("page"), database: untitledPageDisplayTitle("database") };
  }
  return page.kind === "database" ? untitledNames.database : untitledNames.page;
}

function putTitle(kept: TitleMaps, page: MentionIconPage) {
  kept.titles.set(page.id, page.title ? titleWithClosedMentions(page.title, page.titleSpans) : untitledMentionTitle(page));
  const pieces = page.title ? titleCustomEmojiPieces(page.title, page.titleSpans) : null;
  if (pieces) kept.pieces.set(page.id, pieces);
  else kept.pieces.delete(page.id);
}

/**
 * Keeps the list's titles for mentions. A list made from the last one by new
 * records of its pages, a few pages left out or added at its end
 * (listEditsOf), has only those pages' titles worked out again, and none
 * when no title changed: each title saved in a 44,000-page workspace worked
 * every page's title out again, some 70 ms at CPU ×2 (2026-11-15), and so
 * did a page put in the trash elsewhere, which leaves the live list.
 */
function keepMentionPageTitles(pages: readonly MentionIconPage[]) {
  const last = keptTitles;
  const edits = last && last.titles === mentionPageTitlesNow() ? listEditsOf(last.pages, pages) : null;
  if (last && edits) {
    const retitled = edits.replaced.filter(([was, now]) => (
      last.pages[was].title !== pages[now].title || last.pages[was].titleSpans !== pages[now].titleSpans
      || (!pages[now].title && untitledMentionTitle(last.pages[was]) !== untitledMentionTitle(pages[now]))
    ));
    if (retitled.length === 0 && edits.removed.length === 0 && edits.added.length === 0) {
      keptTitles = { ...last, pages };
      return;
    }
    const next: TitleMaps = { titles: new Map(last.titles), pieces: new Map(last.pieces) };
    for (const page of edits.removed) {
      next.titles.delete(page.id);
      next.pieces.delete(page.id);
    }
    for (const [, now] of retitled) putTitle(next, pages[now]);
    for (const now of edits.added) putTitle(next, pages[now]);
    keep(pages, next);
    return;
  }
  const next: TitleMaps = { titles: new Map(), pieces: new Map() };
  for (const page of pages) putTitle(next, page);
  keep(pages, next);
}

function keep(pages: readonly MentionIconPage[], next: TitleMaps) {
  setMentionPageTitles(next.titles, next.pieces, false);
  // Titles the same as those kept stay the ones kept (setMentionPageTitles).
  keptTitles = { pages, titles: mentionPageTitlesNow(), pieces: next.pieces };
}

/**
 * The icons of a workspace's pages, and of the database pages and data sources on screen, for the mentions that name them,
 * and how those the list lacks stand to their reader, asked on the open page (`pageId`, useMentionReach.ts).
 */
export function PageMentionIconStyles({ pages, trashedPages = [], session, workspaceId, pageId }: {
  pages: readonly MentionIconPage[];
  /** The workspace's pages in the trash, whose mentions say so. */
  trashedPages?: readonly MentionIconPage[];
  session?: Session;
  workspaceId?: string;
  pageId?: string | null;
}) {
  const mentioned = useMentionedIds(MENTIONED_PAGES, pageIdOf);
  // A database mention drawn closed is read too, its name coming back as it opens to the reader.
  const mentionedDatabases = useBothIds(useMentionedIds(MENTIONED_DATABASES, databaseIdOf), useMentionedIds(MARKED_DATABASES, markedDatabaseIdOf));
  const databases = useMentionedDatabases(mentionedDatabases, pages, session, workspaceId);
  useMentionedBlocks(useMentionedIds(MENTIONED_BLOCKS, blockKeyOf), pages, session);
  const marked = useMentionedIds(MARKED_PAGES, markedPageIdOf);
  const known = useMemo(() => trashedPages.length > 0 ? [...pages, ...trashedPages] : pages, [pages, trashedPages]);
  // Data sources' mentions, drawn open, closed or missing, are asked by the addresses they name them by.
  const mentionedSources = useMentionedIds(SHOWN_DATA_SOURCES, shownDataSourceIdOf);
  const sourceAddresses = useMemo(() => new Set([...mentionedSources].map(dataSourceMentionAddress)), [mentionedSources]);
  useMentionReach(pageId ?? undefined, session, { pages: mentioned, marked, databases: useBothIds(mentionedDatabases, sourceAddresses) }, known);
  // Rows', templates' and pages' the list lacks: their titles, icons and trash, read as one, following the pages holding them.
  const others = useUnlistedMentionedPages(marked, mentioned, pages, known, session, workspaceId);
  const iconPages = useMemo(() => others.length > 0 ? [...known, ...others] : known, [others, known]);
  const { t } = useTranslation("pageView");
  const trashedLabel = t("inTrashTopBarTitle");
  const ruled = useMentionedFirst(iconPages, mentioned);
  const css = useMemo(() => pageMentionIconCss(ruled, trashedLabel), [ruled, trashedLabel]);
  const pictured = useMemo(() => iconPages.filter((page) => !page.inTrash && mentioned.has(page.id) && hasImageIcon(page)), [iconPages, mentioned]);
  const picturedNear = useMentionWindow(useMemo(() => pictured.map((page) => page.id), [pictured]), MENTIONED_PAGES, pageIdOf, MENTION_IMAGE_ICON_LIMIT);
  const imagePages = useMemo(() => pictured.filter((page) => picturedNear.has(page.id)), [pictured, picturedNear]);
  const databaseCss = useMemo(() => databaseMentionIconCss(databases, trashedLabel), [databases, trashedLabel]);
  return (
    <>
      {css ? <style data-page-mention-icons="">{css}</style> : null}
      {imagePages.map((page) => <MentionImageIconRule key={page.id} page={page} />)}
      {databaseCss ? <style data-database-mention-icons="">{databaseCss}</style> : null}
      {databases.filter(hasImageIcon).slice(0, MENTION_IMAGE_ICON_LIMIT).map((database) => (
        <MentionImageIconRule key={`database:${database.id}`} page={database} selector={databaseMentionSelector(database.id)} />
      ))}
      <DataSourceMentionStyles mentioned={mentionedSources} session={session} workspaceId={workspaceId} pages={pages} trashedLabel={trashedLabel} />
    </>
  );
}

const hasImageIcon = (page: MentionIconPage) => (page.iconType === "image" || page.iconType === "custom_emoji") && Boolean(page.icon)
  && /^[A-Za-z0-9_-]+$/u.test(page.id);
