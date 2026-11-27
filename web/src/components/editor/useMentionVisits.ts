import { useMemo } from "react";
import { useLibraryPages, usePageVisitsNow } from "@/components/library/useLibraryData";
import { pageArchiveRoot } from "@/lib/pageArchive";
import type { BlockPickerSnapshot } from "./blockPickerModel";
import type { MentionVisit } from "./mentionLead";

/**
 * The pages and database rows' pages the reader visited, newest first, as
 * the "@" and "[[" menus' lead reads them (withMentionLead): the person's
 * visits the server keeps (page_visits.go), with a page opened here a moment
 * ago (usePageVisitsNow) — the search window's (searchVisits.ts), the same
 * on every device. A page is one of the listed pages the menu finds, so none
 * in the trash or under a page in it (useEditorMentionSnapshot); a row by
 * what the library reads of it, while its database's page is listed. Read as
 * the menu opens, where it lists pages (`on`) and has a reader.
 */
export function useMentionVisits(snapshot: BlockPickerSnapshot, on: boolean): MentionVisit[] {
  const session = on ? snapshot.session ?? null : null;
  const workspaceId = snapshot.workspaceId ?? "";
  const visits = usePageVisitsNow(session, workspaceId);
  const pagesById = session ? snapshot.pagesById : null;
  const unlisted = useMemo(() => pagesById ? visits.flatMap(({ pageId }) => pagesById[pageId] ? [] : [pageId]) : [], [pagesById, visits]);
  const details = useLibraryPages(session, workspaceId, unlisted).pages;
  const inlineDatabases = snapshot.inlineDatabases;
  return useMemo(() => !pagesById ? [] : visits.flatMap((visit): MentionVisit[] => {
    const page = pagesById[visit.pageId];
    if (page) return [{ page }];
    const row = details.get(visit.pageId)?.row;
    const container = row && !row.template ? pagesById[row.containerPageId] : undefined;
    if (!row || !container) return [];
    // A row of a page's own database names it as the server's row search
    // does, the page's only one; with several the page alone, until that
    // search finds the row (withMentionLead).
    const held = container.kind === "database" ? [] : (inlineDatabases ?? []).filter((database) => database.pageId === container.id);
    return [{ row: {
      rowId: visit.pageId, title: row.title, icon: row.icon, iconType: row.iconType, containerPageId: container.id,
      containerTitle: container.title, untitledName: row.untitledName, editedAt: Date.parse(row.updatedAt) || 0,
      ...(held.length === 1 ? { inlineDatabase: true, databaseTitle: held[0].title } : {}),
      ...(pageArchiveRoot(container, (id) => pagesById[id]) ? { archived: true } : {}),
    } }];
  }), [details, inlineDatabases, pagesById, visits]);
}
