import { dataSourceMentionAddress } from "@/lib/dataSourceMentionNames";
import { databasePagePlace, placeText } from "@/lib/pagePath";
import type { Page } from "@/lib/types";
import type { Page as WirePage } from "@/types";
import type { MentionItem, RowPageMentionCandidate } from "./blockPickerModel";
import { untitledDatabaseSourcesName } from "./databaseTitles";

/**
 * A source of a database holding several as the "@" menu offers it, as
 * Notion's (seen 2026-11-24 in two tabs): its own row by its name and a data
 * source's icon, under "Top / ... / Database" — a full-page database's page,
 * an inline one's own name or, while it has none, its sources' "A 및 B" —
 * and mentioned as that source (dataSourceMentionNames.ts), which opens its
 * database at the source's view. While the database's page isn't among the
 * reader's, its title the server read and the database's name.
 */
export function sourceMentionItem(
  row: RowPageMentionCandidate,
  pagesById: Readonly<Record<string, Page>>,
  title: (page: Page) => string,
  icon: (page: Page) => string,
): MentionItem {
  const id = row.dataSourceId ?? "";
  const source = { id, workspaceId: "", parentId: row.containerPageId, parentType: "page", kind: "database",
    title: row.title, icon: row.icon, iconType: row.iconType || "none", position: 0 } as unknown as Page;
  const databaseTitle = row.databaseTitle?.trim() || (row.sourceTitles && row.sourceTitles.length > 1 ? untitledDatabaseSourcesName(row.sourceTitles) : "");
  const place = pagesById[row.containerPageId]
    ? databasePagePlace(row.containerPageId, pagesById as unknown as Record<string, WirePage>, { inlineDatabase: row.inlineDatabase, databaseTitle })
    : [row.containerTitle, row.inlineDatabase ? databaseTitle : ""].filter(Boolean);
  return { kind: "page", id: `source:${id}`, label: title(source), place, description: placeText(place),
    icon: icon(source), pageId: id, page: source, databaseId: dataSourceMentionAddress(id), dataSource: true };
}
