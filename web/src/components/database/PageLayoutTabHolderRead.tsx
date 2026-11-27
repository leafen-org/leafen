import { useMemo, useState, type ReactNode } from "react";
import { emptyContentGraph, type ContentGraph } from "@/contentGraph";
import { useProductPages, type ProductPages } from "@/components/product/productPagesContext";
import { usePageBodyEditing } from "@/components/product/usePageBodyEditing";
import type { PageLayoutTabEnvironment, PageLayoutTabHolder } from "./pageLayoutTabEnvironment";

const noPages: ProductPages = { pages: [], mentionPages: [], setData: () => {} };
const noStatus = () => {};
const noPicker = () => {};

/**
 * A layout tab's environment read from the page holding its database, as
 * that page's own view reads it (its layout's tabs' sources come with it):
 * its graph and the writes its views make, under the reader's permission
 * there. `children` hears it once it is read, null where it can't be.
 */
export default function PageLayoutTabHolderRead({ holder, children }: {
  holder: PageLayoutTabHolder;
  children: (environment: PageLayoutTabEnvironment | null, read: boolean) => ReactNode;
}) {
  const { pageId } = holder;
  const product = useProductPages() ?? noPages;
  const [graph, setGraph] = useState<ContentGraph>(emptyContentGraph);
  const page = useMemo(() => ({ id: pageId, workspaceId: holder.workspaceId }), [holder.workspaceId, pageId]);
  const body = usePageBodyEditing({
    bootstrapRevision: 0, graph, setGraph, onStatus: product.onStatus ?? noStatus, page,
    pages: product.pages, mentionPages: product.mentionPages, organizationId: product.organizationId,
    refreshPages: product.refreshPages, session: holder.session, setData: product.setData, setDatabasePickerOpen: noPicker,
    workspaceId: holder.workspaceId, mutationOutbox: product.mutationOutbox, inlineDatabases: product.inlineDatabases,
  });
  // Read, or not to be read: the tab then says its source can't be seen.
  const read = (graph !== emptyContentGraph && !body.graphLoading && !body.graphAccessPending) || body.graphUnavailable;
  const { databaseViewAdapter, tableAdapter, boardAdapter, timelineAdapter, graphCanEdit } = body;
  const environment = useMemo<PageLayoutTabEnvironment | null>(() => read && graph !== emptyContentGraph ? {
    dataSources: graph.dataSources, filterGraph: graph, databaseViewAdapter, tableAdapter, boardAdapter, timelineAdapter,
    pageId, workspaceId: holder.workspaceId, session: holder.session, readOnly: !graphCanEdit,
  } : null, [boardAdapter, databaseViewAdapter, graph, graphCanEdit, holder.session, holder.workspaceId, pageId, read, tableAdapter, timelineAdapter]);
  return <>{children(environment, read)}</>;
}
