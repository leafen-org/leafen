import type { ReactNode } from "react";
import { compareGraphOrder } from "@/contentGraph";
import type { ContentGraph, GraphBlock, GraphView } from "@/contentGraph";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import type { WikiDatabaseViews } from "@/components/WikiDatabaseView";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { WIKI_HOME_VIEW_ID, wikiHomeIndex, wikiHomeOf, wikiOrderAfterMove, type WikiHome } from "@/lib/wikiHome";

/**
 * A wiki whose pages are its database's pages keeps its database in a
 * database block on the wiki's page. The wiki's Home is the page without
 * that block; its other views are that database's views.
 */
export type WikiDatabaseLayout = {
  /** The wiki's page as its Home shows it. */
  homeGraph: ContentGraph;
  /** The wiki's database block. */
  block: GraphBlock;
  /** The wiki database's views, in order. */
  views: GraphView[];
  /** The view the wiki's database block shows. */
  activeViewId: string;
  /** Home's own name and place among the views. */
  home: WikiHome;
  /**
   * Whether the wiki opens on its Home: the view this person last chose
   * here, else the first of its views with Home among them, as Notion's
   * (seen 2026-11-11: chosen in this browser, for that person).
   */
  homeChosen: boolean;
  /** The wiki's database block alone, as its views show it. */
  databaseGraph: ContentGraph;
};

function wikiDatabaseBlock(graph: ContentGraph, dataSourceId: string): GraphBlock | undefined {
  return graph.blocks.find((block) => block.type === "inline_database" && !block.parentId
    && block.content.dataSourceId === dataSourceId && block.content.linkedDatabaseSource !== true);
}

export function wikiDatabaseLayout(
  graph: ContentGraph,
  dataSourceId: string | undefined,
  /** This person's view chosen on the block here, Home's id for Home; none, the first in order shows. */
  chosenViewId?: (blockId: string) => string | null,
): WikiDatabaseLayout | null {
  if (!dataSourceId) return null;
  const block = wikiDatabaseBlock(graph, dataSourceId);
  if (!block) return null;
  const views = graph.views
    .filter((view) => view.dataSourceId === dataSourceId && (!view.containerId || view.containerId === block.id)
      && !isDashboardWidget(view))
    .sort(compareGraphOrder);
  if (views.length === 0) return null;
  const local = chosenViewId?.(block.id) ?? "";
  const home = wikiHomeOf(block);
  const viewChosen = views.some((view) => view.id === local);
  return {
    homeGraph: { ...graph, blocks: graph.blocks.filter((candidate) => candidate.id !== block.id) },
    block,
    views,
    activeViewId: viewChosen ? local : views[0].id,
    home,
    homeChosen: local === WIKI_HOME_VIEW_ID || (!viewChosen && wikiHomeIndex(views, home) === 0),
    databaseGraph: { ...graph, blocks: [block] },
  };
}

/** The wiki's views as its page shows them (WikiDatabaseView), its database drawn by `database`. */
export function wikiDatabaseViews(layout: WikiDatabaseLayout, graph: ContentGraph, readOnly: boolean,
  adapter: Pick<NativeDatabaseViewProductAdapter, "selectView" | "updateViews" | "setWikiHome">, database: ReactNode,
  /** The graph as it is now, where `graph` is the app's frame, whose views' rows' order may be behind (lib/pageGraphStore.ts). */
  latest: () => ContentGraph = () => graph): WikiDatabaseViews {
  return {
    views: layout.views,
    activeViewId: layout.activeViewId,
    home: layout.home,
    homeChosen: layout.homeChosen,
    readOnly,
    onSelectView: (viewId) => {
      const view = latest().views.find((candidate) => candidate.id === viewId);
      if (view) adapter.selectView(layout.block, view);
    },
    // Home is chosen as a view is, by its id.
    onChooseHome: () => adapter.selectView(layout.block, { ...layout.views[0], id: WIKI_HOME_VIEW_ID }),
    // A row moved in the views list, Home's too: the views placed anew, and Home if its place changed.
    onReorder: (sourceId, targetId, side) => {
      const moved = wikiOrderAfterMove(layout.views, layout.home, sourceId, targetId, side);
      if (!moved) return;
      const now = latest().views;
      const placed = moved.views.filter((view) => view.position !== layout.views.find(({ id }) => id === view.id)?.position)
        .map((view) => ({ ...now.find(({ id }) => id === view.id) ?? view, position: view.position }));
      if (placed.length) void adapter.updateViews(placed, () => true);
      if (moved.homePosition !== layout.home.position) adapter.setWikiHome?.(layout.block, { position: moved.homePosition });
    },
    onRenameHome: (name) => adapter.setWikiHome?.(layout.block, { name }),
    database,
  };
}
