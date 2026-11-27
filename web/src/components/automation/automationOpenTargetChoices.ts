import { useEffect, useState } from "react";
import { api } from "@/api";
import { mentionSearchRank, type RowPageMentionCandidate } from "@/components/editor/blockPickerModel";
import { libraryRowPage } from "@/components/library/libraryModel";
import { databasePagePlace, pagePlace, placeUnder, type InlineDatabaseName, type Place } from "@/lib/pagePath";
import { pageEditedTime, searchAnswerOrder } from "@/lib/pageSearchRank";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { Page, Session } from "@/types";
import type { LibraryPage } from "@/workspaceMembershipWire.generated";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

/**
 * How many pages the open list shows before its "N개 더 보기", as Notion's
 * (seen 2026-11-24 in two tabs): five with nothing typed, twenty for a
 * search; each press shows twenty more, and the row names how many the next
 * press adds — twenty, or the few left.
 */
export const OPEN_TARGETS_AT_FIRST = { empty: 5, searched: 20 } as const;
export const OPEN_TARGETS_MORE = 20;

/** How many choices are shown after so many presses of "더 보기", and how many the next press adds. */
export function openTargetsShown(total: number, searched: boolean, presses: number) {
  const shown = Math.min(total, (searched ? OPEN_TARGETS_AT_FIRST.searched : OPEN_TARGETS_AT_FIRST.empty) + presses * OPEN_TARGETS_MORE);
  return { shown, more: Math.min(OPEN_TARGETS_MORE, total - shown) };
}

/** A database row's database, which no page list holds: the page holding it and, inline, the database between. */
type RowWhere = InlineDatabaseName & { containerPageId: string; containerTitle?: string };

/**
 * A page the open list offers, by its title: one of the reader's pages, a
 * database a page holds, or a database row, which no page list holds —
 * found by a search or visited lately, with its database's page.
 */
export type OpenTargetChoice = { page: Page; title: string; row?: RowWhere };

/**
 * The choices in the open list's order, as Notion's (seen 2026-11-24 in two
 * tabs): for a search, those whose title answers it in the order the "@"
 * menu gives them (searchAnswerOrder — all the words before some of them,
 * then the last edited first), pages, databases and rows alike, as Notion's
 * list mixes them by an order of its own the reader's visits don't decide
 * (it put the page visited last fifth of six answering as well, seen
 * 2026-11-24 in two tabs); with nothing typed, all of them, the one the
 * reader visited last first, as an empty search lists the pages visited
 * last. Then the list's own order.
 */
export function orderOpenTargetChoices(choices: readonly OpenTargetChoice[], needle: string, visitedAt: ReadonlyMap<string, number>) {
  return choices
    .map((choice, index) => ({ choice, index, rank: mentionSearchRank(choice.title, needle), visited: visitedAt.get(choice.page.id) ?? -1,
      edited: needle ? pageEditedTime(choice.page) : 0 }))
    .filter(({ rank }) => Number.isFinite(rank))
    .sort((left, right) => searchAnswerOrder(left, right) || (needle ? 0 : right.visited - left.visited) || left.index - right.index)
    .map(({ choice }) => choice);
}

/** When the reader last visited each page. */
export function visitTimes(visits: readonly { pageId: string; visitedAt: string }[]) {
  const times = new Map<string, number>();
  for (const visit of visits) {
    const parsed = Date.parse(visit.visitedAt);
    const at = Number.isFinite(parsed) ? parsed : 0;
    if (at > (times.get(visit.pageId) ?? -1)) times.set(visit.pageId, at);
  }
  return times;
}

/**
 * A database row a search found, as the open list offers it among the
 * pages, as Notion's (seen 2026-11-24 in two tabs). Neither People, a
 * database a row holds nor a source of a database is one: Notion's list
 * wasn't seen offering them, and offers a database of several once.
 */
export function searchedRowChoice(row: RowPageMentionCandidate, workspaceId: string, title: (page: Page) => string): OpenTargetChoice | null {
  if (row.databaseId || row.peopleDatabase || row.dataSourceId) return null;
  const page = {
    id: row.rowId, workspaceId, parentId: row.containerPageId, parentType: "database", kind: "page",
    title: row.title, untitledName: row.untitledName, icon: row.icon, iconType: row.iconType || "none", position: 0,
    ...(row.editedAt ? { updatedAt: new Date(row.editedAt).toISOString() } : {}),
  } as unknown as Page;
  return { page, title: title(page), row: { containerPageId: row.containerPageId, containerTitle: row.containerTitle,
    inlineDatabase: row.inlineDatabase, databaseTitle: row.databaseTitle } };
}

/** A database row the reader visited, as an empty search offers it among the pages visited; none for a template, a person or People. */
export function visitedRowChoice(detail: LibraryPage, workspaceId: string, title: (page: Page) => string): OpenTargetChoice | null {
  if (!detail.row || detail.row.template || detail.row.person) return null;
  const page = libraryRowPage(detail, workspaceId);
  return page ? { page, title: title(page), row: { containerPageId: detail.row.containerPageId } } : null;
}

/**
 * Where a choice sits, under its title: a page's place as Notion's lists
 * show it (pagePlace); a row's as the "@" menu shows one, "Page / Database"
 * — its source's name for one of several (seen 2026-11-24, "한지테스트페이지
 * / ... / BothB") once the server read the path above it
 * (databasePagePlace) — its database's page alone while that page isn't
 * among the reader's.
 */
export function openTargetPlace(choice: OpenTargetChoice, pagesById: Readonly<Record<string, Page>>): Place {
  const byId = pagesById as Record<string, Page>;
  if (!choice.row) return pagePlace(choice.page, byId);
  return byId[choice.row.containerPageId]
    ? databasePagePlace(choice.row.containerPageId, byId, choice.row, choice.page.id)
    : [choice.row.containerTitle ?? ""].filter(Boolean);
}

/** A form the server found, with where it is (form_view_picker.go). */
export type FoundForm = {
  viewId: string; name: string; dataSourceId: string; pageId: string; pageTitle: string; pageKind?: string; viewBlock?: boolean;
  sourceTitles?: DatabaseSourceTitle[]; containerId?: string;
};

/**
 * Where a form sits, under its name, as Notion's open list shows it (seen
 * 2026-11-24 in two tabs): the path ending at its database — a full-page
 * database's own page, an inline one's name after the page holding it, one
 * of several sources by the database's own name ("Top / ... / Both", not
 * its source's) — past two titles the first and the last with "..."
 * between, as a page's place. A page outside the reader's list by the title
 * the server read, an untitled one by its kind.
 */
export function formPlace(form: FoundForm, pagesById: Readonly<Record<string, Page>>, held: ReadonlyMap<string, Page>, title: (page: Page) => string): Place {
  const byId = pagesById as Record<string, Page>;
  if (!byId[form.pageId]) return [form.pageTitle || untitledPageDisplayTitle(listedPageNameKind(form))];
  if (form.pageKind === "database" && !form.viewBlock) return placeUnder(form.pageId, byId);
  const database = form.containerId ? held.get(form.containerId) : undefined;
  return database ? placeUnder(form.pageId, byId, title(database)) : placeUnder(form.pageId, byId);
}

/**
 * The page of a row chosen to open, read for its name where no list here
 * holds it: a row chosen before the editor opened (libraryRowPage).
 */
export function useChosenRowPage(session: Session | undefined, workspaceId: string | undefined, pageId: string) {
  const [read, setRead] = useState<{ id: string; page: Page | null } | null>(null);
  useEffect(() => {
    if (!session || !workspaceId || !pageId) return undefined;
    const controller = new AbortController();
    api.readLibraryPages(session, workspaceId, [pageId], controller.signal).then((result) => {
      const detail = result.pages.find((page) => page.pageId === pageId);
      setRead({ id: pageId, page: detail?.row ? libraryRowPage(detail, workspaceId) : null });
    }, () => undefined);
    return () => controller.abort();
  }, [pageId, session, workspaceId]);
  return read?.id === pageId ? read.page : null;
}
