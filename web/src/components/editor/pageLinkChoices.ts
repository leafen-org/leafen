import { pagePlace, type Place } from "@/lib/pagePath";
import { pageEditedTime, searchAnswerOrder } from "@/lib/pageSearchRank";
import type { Page as ModelPage } from "@/lib/types";
import type { Page } from "@/types";
import { mentionSearchRank, type MentionInlineDatabase, type RowPageMentionCandidate } from "./blockPickerModel";
import { inlineDatabasePagesOf } from "./inlineDatabasePages";
import { rowMentionItem } from "./rowMentionItem";

/** The most a search lists, as Notion's (seen 2026-11-19: 20 of 26 pages named alike; 2026-11-24: 20). */
const PAGE_LINK_SEARCH_LIMIT = 20;

/**
 * One of a page link block's choices: what it shows — the icon of `page`,
 * its title, where it is, whether it is archived — and what the link then
 * names: a page (a database row's too) or a database, by its block.
 */
export type PageLinkChoice = {
  id: string;
  page: Page;
  title: string;
  place: Place;
  archived: boolean;
  target: { pageId: string } | { databaseId: string };
};

function searchText(value: string) {
  return value.normalize("NFC").trim().toLowerCase();
}

/**
 * What a page link block's picker lists for what is typed. With nothing,
 * every page at the workspace's top, the open one too, oldest first, and no
 * page under another, as Notion's (seen 2026-11-19 in two Chrome tabs: 23
 * pages in the order they were made, one changed since still in its place).
 * With words, as Notion's picker finds them (seen 2026-11-24 in two Chrome
 * tabs): the workspace's pages, the databases they hold in their bodies,
 * and the database rows the server's row search found (`rows`, as the "@"
 * menu's), mixed, those holding the words whole first, then all the words
 * apart, then some of them, and among those answering as well the one edited
 * last — the "@" menu's order (searchAnswerOrder; Notion's own order inside
 * a tier is a learned ranking, as its "@" menu's) — twenty at most, none in
 * the trash or under a page in it (`outOfTrash`). Nothing leads: the pages
 * the reader opened lately go in their place, where Notion's "@" puts them
 * first, and a word a letter off finds nothing. One source of several
 * isn't offered, nor People, which no link names.
 */
export function pageLinkChoices({ pages, workspaceId, query, title, archived, outOfTrash, inlineDatabases, rows }: {
  pages: readonly Page[];
  workspaceId: string;
  query: string;
  title: (page: Page) => string;
  archived: ReadonlySet<string>;
  outOfTrash: (page: Page) => boolean;
  inlineDatabases?: readonly MentionInlineDatabase[];
  rows: readonly RowPageMentionCandidate[];
}): PageLinkChoice[] {
  const byId = Object.fromEntries(pages.map((page) => [page.id, page])) as Record<string, Page>;
  const folded = searchText(query);
  const listed = pages.filter((page) => outOfTrash(page) && page.workspaceId === workspaceId);
  const pageChoice = (page: Page, held = false): PageLinkChoice => ({ id: page.id, page, title: title(page), place: pagePlace(page, byId),
    archived: archived.has(held ? page.parentId ?? "" : page.id), target: held ? { databaseId: page.id } : { pageId: page.id } });
  if (!folded) {
    return listed.filter((page) => !page.parentId && !page.databaseRow)
      .sort((left, right) => (left.createdAt ?? "").localeCompare(right.createdAt ?? "")).map((page) => pageChoice(page));
  }
  const held = inlineDatabasePagesOf(inlineDatabases?.filter((database) => database.title || (database.sourceTitles?.length ?? 0) < 2),
    Object.fromEntries(listed.map((page) => [page.id, page])));
  const rowChoice = (row: RowPageMentionCandidate): PageLinkChoice => {
    const item = rowMentionItem(row, byId as unknown as Record<string, ModelPage>, (page) => title(page as unknown as Page),
      () => "");
    const page = (item.kind === "page" ? item.page : null) as unknown as Page;
    return { id: item.id, page, title: item.label, place: item.kind === "page" ? item.place ?? [] : [], archived: Boolean(row.archived),
      target: row.databaseId ? { databaseId: row.databaseId } : { pageId: row.rowId } };
  };
  const found = [
    ...listed.map((page) => ({ choice: pageChoice(page), edited: pageEditedTime(page) })),
    ...[...held.values()].map((page) => ({ choice: pageChoice(page, true), edited: pageEditedTime(page) })),
    ...rows.filter((row) => !row.dataSourceId && !row.peopleDatabase && !byId[row.databaseId ?? row.rowId])
      .map((row) => ({ choice: rowChoice(row), edited: row.editedAt ?? 0, row: true })),
  ];
  return found.map((entry, index) => ({ ...entry, index, rank: mentionSearchRank(searchText(entry.choice.title), folded) }))
    .filter((entry) => "row" in entry || Number.isFinite(entry.rank))
    .sort((left, right) => searchAnswerOrder(left, right) || left.index - right.index)
    .slice(0, PAGE_LINK_SEARCH_LIMIT)
    .map(({ choice }) => choice);
}
