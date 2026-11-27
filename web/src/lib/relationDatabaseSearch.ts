import { api } from "@/api";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import { readSourcePages } from "@/lib/databaseSourceReading";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import type { Session } from "@/types";

const relationDatabaseSearchLimit = 25;
const relationDatabaseQueryCharacters = 128;

// Databases anywhere in the workspace the viewer can read, as relation
// targets: one entry per data source, whatever page holds it. A page, or with
// `upTo` that many (readSourcePages), so a list counts all it holds or found,
// not one page (the "N개 더 보기" of RelationPropertyCreationDialog,
// PageLayoutTabMenu and AutomationTargetDatabasePicker, searched or not).
// With `addsRows` only those a person may add rows to — a move's places, an
// action's that adds or edits pages — which leave People out, as Notion's
// do while its linked views and relations offer it (database_added_rows.go).
export function createRelationDatabaseSearch(session: Session, workspaceId: string, { addsRows = false } = {}) {
  return async (query: string, signal: AbortSignal, upTo?: number): Promise<RelationDatabaseOption[]> => {
    const searched = Array.from(query).slice(0, relationDatabaseQueryCharacters).join("");
    const { items } = await readSourcePages((cursor, limit) => api.databaseContainerSources(
      session, workspaceId, searched, { cursor, limit: limit ?? relationDatabaseSearchLimit, signal, addsRows }), { upTo });
    const seen = new Set<string>();
    return items.flatMap((item, index) => {
      if (seen.has(item.dataSourceId)) return [];
      seen.add(item.dataSourceId);
      // The page holding it, until a list with the reader's pages says where it sits (databaseSourcePlace).
      const where = item.pageTitle !== item.title ? item.pageTitle : "";
      // An untitled one is "새 데이터베이스", or "새 데이터 소스" among others, as Notion's (seen 2026-11-02).
      return [{ id: item.dataSourceId, title: dataSourceDisplayTitle(item.title, item.sourceTitles?.length ?? 1),
        position: relationDatabaseSearchLimit + index, pageId: item.pageId, containerId: item.id, where, sourceTitles: item.sourceTitles,
        ...(item.title.trim() ? {} : { untitled: true }),
        ...(item.databaseTitle ? { databaseTitle: item.databaseTitle } : {}),
        ...(item.icon && item.iconType ? { mark: { icon: item.icon, iconType: item.iconType } } : {}) }];
    });
  };
}

/**
 * The data source a page or a block moved onto a database becomes a row of,
 * with the page whose body holds that database: a full-page database's own
 * database block's source, read by its page's ID whatever its title; or,
 * for a database a page's body holds — chosen by its database's ID, under
 * the page holding it (inlineDatabaseChoice, a search's database), `held` —
 * that block's source, read on the page holding it. Each is read where it
 * is first, so no page that isn't there is asked for; the other place is
 * read only when that finds none. A database that only links another's
 * source has none.
 */
export async function fullPageDatabaseSource(
  session: Session, page: { id: string; parentId?: string }, signal = new AbortController().signal, held = false,
): Promise<{ id: string; pageId: string } | undefined> {
  const own = () => databaseBlockSource(session, page.id, (block) => !block.parentId, signal);
  const inHolder = () => page.parentId
    ? databaseBlockSource(session, page.parentId, (block) => block.id === page.id, signal) : Promise.resolve(undefined);
  const first = await (held ? inHolder() : own());
  if (first || signal.aborted) return first;
  return held ? own() : inHolder();
}

/** The source of the first database block on the page `pageId` that `which` takes, its own (not a linked view). */
async function databaseBlockSource(
  session: Session, pageId: string, which: (block: { id: string; parentId?: string | null }) => boolean, signal: AbortSignal,
) {
  const read = await api.contentGraph(session, pageId).catch(() => null);
  if (!read || signal.aborted) return undefined;
  const own = read.items.flatMap((item) => item.kind === "block" && item.value.type === "inline_database" && which(item.value)
    && item.value.content.linkedDatabaseSource !== true && typeof item.value.content.dataSourceId === "string" ? [item.value.content.dataSourceId] : []);
  return own[0] ? { id: own[0], pageId } : undefined;
}
