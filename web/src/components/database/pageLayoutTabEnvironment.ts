import { createContext, useContext } from "react";
import type { GraphDataSource } from "@/contentGraph";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import type { Session } from "@/types";
import type { DatabaseFilterGraph } from "./databaseFilterSnapshot";

/**
 * What a database page's layout tabs show their views with: the graph the
 * database's own view read — the tabs' sources are read with it — and its
 * writers, which save a tab's row to its source's page.
 */
export type PageLayoutTabEnvironment = {
  dataSources: GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  databaseViewAdapter: NativeDatabaseViewProductAdapter;
  tableAdapter?: NativeTableProductAdapter;
  boardAdapter?: NativeBoardProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
  pageId: string;
  workspaceId: string;
  session: Session | null;
  readOnly: boolean;
  /** The database's name as it shows, its page's for a full-page database. */
  title?: string;
  /**
   * Sources read from their own page, not with the database (a source just
   * connected in the builder), by the page their properties are written to.
   */
  sourcePages?: Readonly<Record<string, string>>;
};

export const PageLayoutTabEnvironmentContext = createContext<PageLayoutTabEnvironment | null>(null);

export function usePageLayoutTabEnvironment() {
  return useContext(PageLayoutTabEnvironmentContext);
}

/**
 * The layout builder's draft over whatever its tabs read their sources
 * with, the database's graph or the holder's page: their properties as the
 * draft has them and their changes kept in it (usePageLayoutPropertyDraft).
 */
export const PageLayoutTabDraftContext = createContext<((environment: PageLayoutTabEnvironment) => PageLayoutTabEnvironment) | null>(null);

/**
 * The page holding a database page's database, where no view of it gives
 * the tabs their environment — a database page at its own address, or a
 * peek away from its database: a tab reads it for its source, as that page's
 * own view does, its layout's tabs' sources with it (Notion's, seen
 * 2026-11-12: the tab's table with filter, sort, search and "새로 만들기").
 */
export type PageLayoutTabHolder = { session: Session; pageId: string; workspaceId: string };

export const PageLayoutTabHolderContext = createContext<PageLayoutTabHolder | null>(null);

export function usePageLayoutTabHolder() {
  return useContext(PageLayoutTabHolderContext);
}

/**
 * Where a tab's view shows, as Notion's (seen 2026-10-12): on a database page
 * ("page") its bar keeps filter, sort, search and new beside the tabs; in the
 * layout builder ("builder") its settings too. Neither adds or names views.
 */
export type PageLayoutTabMode = "page" | "builder";

export const PageLayoutTabModeContext = createContext<PageLayoutTabMode | null>(null);

export function usePageLayoutTabMode() {
  return useContext(PageLayoutTabModeContext);
}

/** The layout builder's "보기 복제 · 보기 삭제" at the end of a tab's view settings, as Notion's (seen 2026-10-12). */
export type PageLayoutTabActions = { duplicate(): void; remove(): void };

export const PageLayoutTabActionsContext = createContext<PageLayoutTabActions | null>(null);

/**
 * A one-way relation tab's rows, which Notion's tabs don't offer, named as
 * its view's filter: "Related to 〈the open page's database〉 (〈the
 * relation〉)", a pill set by the layout that the view can't change. A
 * two-way relation's are its view's own filter instead (PageLayoutTabView).
 * It names the tab's own view, so a database opened within the tab doesn't
 * take it.
 */
export const PageLayoutTabFilterContext = createContext<{ viewId: string; label: string } | null>(null);

export function usePageLayoutTabFilter() {
  return useContext(PageLayoutTabFilterContext);
}
