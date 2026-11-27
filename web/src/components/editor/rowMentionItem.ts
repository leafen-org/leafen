import { databasePagePlace, placeText, rowBodyDatabasePlace } from "@/lib/pagePath";
import { LEAFEN_SYSTEM_USER_ID } from "@/lib/peopleDirectory";
import type { Page } from "@/lib/types";
import type { Page as WirePage } from "@/types";
import type { MentionItem, RowPageMentionCandidate } from "./blockPickerModel";
import { sourceMentionItem } from "./databaseSourceMentionItems";

/**
 * What the "@" menu offers for a database row page the server's row search
 * found or the reader opened lately (mentionLead.ts): the row's page under
 * "Page / Database", a database a row holds in its body, the workspace's
 * People itself, or one source of a database holding several.
 */
export function rowMentionItem(
  row: RowPageMentionCandidate,
  pagesById: Readonly<Record<string, Page>>,
  title: (page: Page) => string,
  icon: (page: Page) => string,
): MentionItem {
  const byId = pagesById as unknown as Record<string, WirePage>;
  const page = {
    id: row.rowId, workspaceId: "", parentId: row.containerPageId, parentType: "database",
    kind: "page", title: row.databaseId ? row.rowTitle ?? "" : row.title, untitledName: row.untitledName, icon: row.icon, iconType: row.iconType || "none", position: 0,
  } as unknown as Page;
  // People itself, by its People icon (PageIcon), as Notion's "[[People" (seen 2026-11-21).
  if (row.peopleDatabase && row.databaseId) {
    const people = { id: row.databaseId, workspaceId: "", parentId: null, parentType: "workspace", kind: "database", title: row.title,
      icon: row.icon, iconType: row.iconType || "none", createdBy: LEAFEN_SYSTEM_USER_ID, position: 0 } as unknown as Page;
    return { kind: "page", id: `database:${row.databaseId}`, label: title(people), description: "", icon: icon(people),
      pageId: row.databaseId, page: people, databaseId: row.databaseId };
  }
  // A source of a database holding several, mentioned as that source (sourceMentionItem).
  if (row.dataSourceId) return sourceMentionItem(row, pagesById, title, icon);
  // A database a row holds in its body, a database's mention under "Top / ... / Row", as Notion's (seen 2026-11-15).
  if (row.databaseId) {
    const database = { id: row.databaseId, workspaceId: "", parentId: row.rowId, parentType: "page", kind: "database",
      title: row.title, icon: row.icon, iconType: row.iconType || "none", position: 0 } as unknown as Page;
    const place = pagesById[row.containerPageId] ? rowBodyDatabasePlace(row.containerPageId, byId, row, row.rowId, title(page)) : undefined;
    return { kind: "page", id: `database:${row.databaseId}`, label: title(database), ...(place ? { place } : {}),
      description: place ? placeText(place) : title(page), icon: icon(database), pageId: row.databaseId, page: database, databaseId: row.databaseId,
      ...(row.archived ? { archived: true } : {}) };
  }
  // "Page / Database", as Notion's @ menu shows a database page.
  const place = pagesById[row.containerPageId] ? databasePagePlace(row.containerPageId, byId, row, row.rowId) : undefined;
  return {
    kind: "page", id: `row:${row.rowId}`, label: title(page),
    ...(place ? { place } : {}),
    description: place ? placeText(place) : row.containerTitle,
    icon: icon(page), pageId: row.rowId, page, ...(row.archived ? { archived: true } : {}),
  };
}
