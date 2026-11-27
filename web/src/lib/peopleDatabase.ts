import { request } from "@/apiTransport";
import { materializeContentGraph, type ContentGraph, type GraphReadItem } from "@/contentGraph";
import { rememberPeopleDatabasePage } from "@/lib/peopleDatabasePage";
import type { Session } from "@/types";

/** People's own five properties, by what they are. */
export type PeoplePropertyIds = { name: string; email: string; about: string; membership: string; person: string };

/**
 * The workspace's People database as People reads it (people_database_routes.go):
 * its page and data source, its own five properties' IDs, whether the reader
 * — its owner — writes it, and its graph: its views, properties and rows, a
 * restricted member's only the rows of the people they share a place with.
 */
export type PeopleDatabase = {
  pageId: string;
  /** Its page's last edit and who made it, and whether the reader made it a favorite at which revision (its bar's, PeopleDatabaseTopBarActions). */
  updatedAt: string;
  lastEditedBy: string;
  favorite: boolean;
  favoriteRevision: number;
  dataSourceId: string;
  propertyIds: PeoplePropertyIds;
  editable: boolean;
  graph: ContentGraph;
};

type PeopleDatabaseAnswer = { pageId: string; dataSourceId: string; propertyIds: PeoplePropertyIds; editable?: boolean; items?: GraphReadItem[];
  updatedAt?: string; lastEditedBy?: string; favorite?: boolean; favoriteRevision?: number; hasMore?: boolean; nextCursor?: string };

export async function readPeopleDatabase(session: Session, workspaceId: string, signal?: AbortSignal): Promise<PeopleDatabase> {
  const path = `/workspaces/${encodeURIComponent(workspaceId)}/people/database`;
  const answer = await request<PeopleDatabaseAnswer>(path, { signal }, session);
  rememberPeopleDatabasePage(answer.pageId);
  const items = Array.isArray(answer.items) ? [...answer.items] : [];
  // A People database past one answer's windows says where it goes on; it is read to its end.
  for (let next = answer; next.hasMore === true && next.nextCursor; ) {
    next = await request<PeopleDatabaseAnswer>(`${path}?cursor=${encodeURIComponent(next.nextCursor)}`, { signal }, session);
    if (Array.isArray(next.items)) items.push(...next.items);
  }
  return {
    pageId: answer.pageId, updatedAt: answer.updatedAt ?? "", lastEditedBy: answer.lastEditedBy ?? "", favorite: answer.favorite === true, favoriteRevision: answer.favoriteRevision ?? 0, dataSourceId: answer.dataSourceId, propertyIds: answer.propertyIds, editable: answer.editable === true,
    graph: materializeContentGraph(items),
  };
}

/** The person a People row is: its Person value's one person. */
export function peopleRowPerson(row: { values: Record<string, { value?: unknown } | undefined> }, ids: PeoplePropertyIds): string {
  const value = row.values[ids.person]?.value;
  return Array.isArray(value) && typeof value[0] === "string" ? value[0] : "";
}
