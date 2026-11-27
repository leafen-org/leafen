import { createContext } from "react";
import type { GraphDataSource, GraphView } from "@/contentGraph";
import type { DatabaseDataSourceMoveBackRequest, DatabaseDataSourceMoveRequest } from "@/databaseDataSourceWire.generated";
import type { DataSourceMoved } from "@/lib/nativeDataSourceManager";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import type { Page, Session } from "@/types";
import { dataSourceDisplayTitle, type DatabaseSourceTitle } from "../editor/databaseTitles";

// The Manage data sources panel opens deep inside the database toolbar, so it
// reaches its database block through this context.
export type DatabaseDataSourcesSurface = {
  databaseId: string;
  /** The page holding the database, its workspace and who changes it, which a source's move picks a page in. */
  pageId: string;
  workspaceId: string;
  session?: Session | null;
  /** A locked database lists its sources without changing them, as Notion's (seen 2026-10-01). */
  readOnly?: boolean;
  /** Its own sources, each one it keeps in the trash where it stands marked `sourceInTrash`. */
  own: readonly GraphDataSource[];
  /**
   * How many sources name it (databaseNamingSourceIds), those it links
   * among them: an untitled one of several is "새 데이터 소스". One sent to
   * the trash with its views isn't counted.
   */
  naming: number;
  /** While it holds several: their titles and its own name, which end its sources' places ("… / A 및 B", databaseSourcePlace). */
  sourceTitles?: readonly DatabaseSourceTitle[];
  databaseTitle?: string;
  /**
   * Sources other databases own, each with where it opens (useLinkedSourceHref),
   * the database holding it and that database's page.
   */
  linked: readonly { dataSourceId: string; title: string; href?: string; sourceContainerId?: string; pageId?: string }[];
  add(): Promise<boolean>;
  link(candidate: DatabaseContainerSource): Promise<boolean>;
  /** The database's views; one alone keeps its source, as Notion's (seen 2026-10-11). */
  viewCount: number;
  /** Its views themselves, which a new view of a linked source may take after too (DatabaseViewSourceViews). */
  views?: readonly GraphView[];
  /** How many of the database's views show each of its sources, as Notion's source rows count them (seen 2026-10-30). */
  viewCounts: Readonly<Record<string, number>>;
  /** Turns a view of the database to another source, linking it first when it is another database's. */
  switchView(viewId: string, candidate: DatabaseContainerSource): Promise<boolean>;
  unlink(dataSourceId: string): Promise<boolean>;
  move(dataSourceId: string, target: DatabaseDataSourceMoveRequest): Promise<DataSourceMoved | null>;
  /** Undoes a move of the source alone, back into this database. */
  moveBack(dataSourceId: string, back: Omit<DatabaseDataSourceMoveBackRequest, "databaseId">): Promise<"moved" | "moving" | "refused" | null>;
  trash(dataSourceId: string): Promise<boolean>;
  restore(dataSourceId: string): Promise<boolean>;
  /** Sources to link: a page of them, or unsearched up to `upTo` (nativeDataSourceManagement). */
  loadCandidates(query: string, signal: AbortSignal, upTo?: number): Promise<DatabaseContainerSource[]>;
  /** The chosen source's views in its own database, which a new view may take after. */
  loadSourceViews(candidate: DatabaseContainerSource, cursor: string | undefined, signal: AbortSignal): Promise<{ views: GraphView[]; nextCursor?: string }>;
  /** Makes the view take after one of those views: its layout, name and settings. */
  adoptView(viewId: string, saved: GraphView): Promise<boolean>;
  loadPages(query: string, signal: AbortSignal): Promise<Page[]>;
  // Rereads the page after the server changed it on its own.
  reread(): Promise<boolean>;
};

export const DatabaseDataSourcesContext = createContext<DatabaseDataSourcesSurface | null>(null);

/**
 * What a source the database shows goes by: its own name, else, one of its
 * own, by how many of its own name the database (`naming`) — "새 데이터 소스"
 * as one of several —, one another database owns by one, as the sources it
 * lists name them (DatabaseDataSourcesPanel).
 */
export function surfaceDataSourceName(surface: DatabaseDataSourcesSurface | null, source: { id: string; title?: string }) {
  return dataSourceDisplayTitle(source.title, surface?.own.some(({ id }) => id === source.id) ? surface.naming : 1);
}

/** Whether a view of the database shows a source another database owns, which its "데이터베이스" row marks "↗". */
export function viewSourceIsLinked(surface: DatabaseDataSourcesSurface | null, dataSourceId: string) {
  return Boolean(surface?.linked.some((member) => member.dataSourceId === dataSourceId));
}

/**
 * Whether a view's "데이터베이스 ›" turns it to another source, its view
 * settings' row and its tab's menu alike, as Notion's: a view of a source
 * another database owns can always turn, a linked view alone in its block
 * too (seen 2026-11-08: "↗ 〈source〉 ›", its block alone then showing the
 * new source); a view of the database's own source only beside another view,
 * its only view keeping its source (seen 2026-10-11); a locked or read-only
 * database's views keep theirs.
 */
export function viewSourceSwitchable(surface: DatabaseDataSourcesSurface | null, dataSourceId: string, locked: boolean) {
  return Boolean(surface && !surface.readOnly && !locked && (surface.viewCount > 1 || viewSourceIsLinked(surface, dataSourceId)));
}
