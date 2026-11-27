import { useMemo, type ReactNode } from "react";
import type { ContentGraph } from "@/contentGraph";
import { PageGraphStoreContext, usePageGraph, type PageGraphStore } from "@/lib/pageGraphStore";

/**
 * Draws `children` with the page's graph as it is now, as `pick` takes it:
 * the page's body, drawn again at each change of its graph while the app
 * around it is drawn only when what it reads changes (usePageGraphFrame).
 * What it draws reaches the store itself, for a piece of the graph drawn
 * again only as that piece changes (usePageGraphSelect).
 */
export function WithPageGraph({ store, pick, children }: {
  store: PageGraphStore;
  pick?: (graph: ContentGraph) => ContentGraph;
  children: (graph: ContentGraph) => ReactNode;
}) {
  const graph = usePageGraph(store);
  const picked = useMemo(() => pick ? pick(graph) : graph, [graph, pick]);
  return <PageGraphStoreContext.Provider value={store}>{children(picked)}</PageGraphStoreContext.Provider>;
}
