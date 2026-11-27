import { createContext, useContext } from "react";
import type { GraphBlock, GraphDataSource, GraphProperty, GraphView } from "@/contentGraph";
import { blockDataSourceName } from "@/lib/dataSourceGraphName";

/**
 * One of a dashboard's widget sources, as its filter lists it: the name it
 * goes by — an untitled one by how many name its database
 * (blockDataSourceName) —, its properties and how many widgets show it.
 */
export type DashboardFilterSource = { source: GraphDataSource; name: string; properties: readonly GraphProperty[]; views: number };

/**
 * A dashboard's filters pick and filter the properties of its widgets'
 * sources, each source's under its own name with how many widgets show it
 * ("작업 트래커 · 3개 보기"), as Notion's "필터 기준" (seen twice on 2026-11-17
 * at 1440 in the Business trial's synthetic dashboard, and on an iPhone by
 * w1554): the sources and their properties by name in English order, Latin
 * before Hangul ("AB03c 1105" before "작업 트래커", "Q3 · Q4 · 날짜"), the
 * name property among them. A filter on a property narrows the widgets of
 * that property's source alone (dashboardFilter.ts). With no widget yet there
 * is none to list: a phone's "속성 필터링" finds no property, as Notion's
 * ("결과 없음", seen twice on 2026-11-17 in Android Chrome at 411). Only more
 * than one source is listed apart (groupedSources).
 */
const byName = new Intl.Collator("en").compare;

export function dashboardFilterSources(block: GraphBlock, widgets: readonly GraphView[],
  dataSources: readonly GraphDataSource[], properties: readonly GraphProperty[]): DashboardFilterSource[] {
  const counts = new Map<string, number>();
  for (const widget of widgets) counts.set(widget.dataSourceId, (counts.get(widget.dataSourceId) ?? 0) + 1);
  return [...counts].flatMap(([id, views]) => {
    const source = dataSources.find((candidate) => candidate.id === id);
    if (!source) return [];
    const own = properties.filter((property) => property.dataSourceId === id).sort((a, b) => byName(a.name, b.name));
    return [{ source, name: blockDataSourceName(block, source), properties: own, views }];
  }).sort((a, b) => byName(a.name, b.name));
}

/** The sources a dashboard's filter lists apart, under their names: more than one; one is listed as any view's (Notion's, seen 2026-11-17). */
export function groupedSources(sources: readonly DashboardFilterSource[] | null) {
  return sources && sources.length > 1 ? sources : null;
}

/** Every property a dashboard's filters can name, its widget sources' together. */
export function dashboardFilterProperties(sources: readonly DashboardFilterSource[]) {
  return sources.flatMap(({ properties }) => properties);
}

export const DashboardFilterSourcesContext = createContext<readonly DashboardFilterSource[] | null>(null);

export function useDashboardFilterSources() {
  return useContext(DashboardFilterSourcesContext);
}
