import { useMemo } from "react";
import type { ContentGraph, GraphProperty, GraphRow } from "@/contentGraph";
import type { Page } from "@/types";
import type { Page as DbPage } from "@/lib/types";
import { type DatabasePageLayout, pageLayoutPlacedProperties } from "@/lib/databasePageLayout";
import { nativeDatabaseFilterSnapshot, nativeDatabaseHydratedRelationTargetIds } from "./databaseFilterSnapshot";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { galleryPresentationPage, galleryPresentationProperty } from "./galleryViewModel";
import { useSidebarLayout } from "../shell/AppShell";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { PageLayoutTabProviders } from "./PageLayoutTabProviders";
import type { PageLayoutTabEnvironment } from "./pageLayoutTabEnvironment";
import type { PropertyValueReadContext } from "./PropValue";
import { RowPageLayoutBody, useRowPageDetails } from "./RowPageLayoutBody";
import { RowPageLayoutTabs } from "./RowPageLayoutTabs";
import { RowPagePinnedProperties, detailsToggleLast } from "./RowPagePinnedProperties";
import { RowPagePropertyList } from "./RowPagePropertyList";
import layoutStyles from "./RowPageLayout.module.css";

const publicSchemaReadOwner = Object.freeze({ request: () => false, retire: () => {} });

/** A visitor changes nothing of a database: its views' filters, sorts and searches last the visit. */
const visitorViewAdapter: NativeDatabaseViewProductAdapter = {
  changeTitle: () => {}, changeIcon: () => {}, changeDescription: () => {}, changeCover: () => {}, showRowContent: () => {},
  selectView: () => {}, setLocked: () => {}, setArchived: () => {}, setLinkedPresentation: () => {},
  createView: async () => null, updateViews: async () => {}, deleteView: async () => {}, restoreView: async () => {},
  addDashboardWidget: async () => null, duplicateDashboardWidget: async () => null, retypeDashboardWidget: async () => {},
  removeDashboardWidget: async () => null, arrangeDashboard: async () => {},
  turnViewIntoDashboard: async () => false, turnDashboardInto: async () => false,
};

/**
 * The context a published database page's values read in: the pages its read
 * carries name a relation's targets, and one it doesn't carry is one the
 * reader may not see, not one still on its way (databaseFilterSnapshot.ts
 * addRelationTargetIds).
 */
function publicRowValueContext(row: GraphRow, graph: ContentGraph, workspaceId: string): PropertyValueReadContext {
  const read = nativeDatabaseFilterSnapshot({ currentUserId: "", graph, snapshotIdentity: `public-row:${row.id}`, workspaceId });
  // Its own database's properties, and those of the databases its layout's tabs show.
  const bySource: Record<string, GraphProperty[]> = {};
  for (const property of graph.properties) (bySource[property.dataSourceId] ??= []).push(property);
  const propsByDatabaseId = Object.fromEntries(Object.entries(bySource).map(([source, list]) => [source, list.map(galleryPresentationProperty)]));
  return {
    bounds: nativeBoardQueryBounds.formula,
    contextIdentity: `public-row:${row.id}`,
    hydratedRelationTargetIds: nativeDatabaseHydratedRelationTargetIds(graph),
    pagesById: Object.fromEntries(Object.entries(read.pagesById).filter((entry): entry is [string, DbPage] => Boolean(entry[1]))),
    propsByDatabaseId,
    schemaReadOwner: publicSchemaReadOwner,
    snapshotIdentity: `public-row:${row.id}`,
  };
}

/**
 * A published database page's properties under its title, as Notion's
 * public database pages show them, "속성" over them; a page that is not a
 * database page, whose read carries no row of its own, shows none. Where its
 * database lays its pages out, they follow the layout, as Notion's published
 * pages do (seen 2026-11-12): the pinned properties and "세부 정보 보기" under
 * the title, then the page's modules — its body after them, as the page
 * draws it, under its tabs where it has them (seen 2026-11-12: another
 * database's tab shows its view as the published page's own databases do,
 * filter, sort and search on its bar and no "새로 만들기") — and the details panel, a column at the window's right from its
 * top to its foot, 385 wide, its "»" in the header's row, or on a phone a
 * whole screen "페이지 정보" (seen 2026-11-12).
 */
export function PublicRowPageProperties({ page, graph, layout }: { page: Page; graph: ContentGraph; layout?: DatabasePageLayout }) {
  const row = graph.rows.find((candidate) => candidate.id === page.id);
  const properties = useMemo(
    () => row ? graph.properties.filter((property) => property.dataSourceId === row.dataSourceId) : [],
    [graph.properties, row],
  );
  const valueRow = useMemo(() => row ? galleryPresentationPage({
    row, titleProperty: properties.find(({ type }) => type === "title"), workspaceId: page.workspaceId, position: 0,
  }) : null, [page.workspaceId, properties, row]);
  const context = useMemo(() => row ? publicRowValueContext(row, graph, page.workspaceId) : null, [graph, page.workspaceId, row]);
  // The tabs' databases come with the page's read, so a tab shows its view read-only, as the page's own databases do.
  const tabEnvironment = useMemo<PageLayoutTabEnvironment>(() => ({
    dataSources: graph.dataSources, filterGraph: graph, databaseViewAdapter: visitorViewAdapter,
    pageId: page.id, workspaceId: page.workspaceId, session: null, readOnly: true,
  }), [graph, page.id, page.workspaceId]);
  // "세부 정보 보기" only where the panel has properties to show, as on a member's page.
  const detailIds = useMemo(() => properties.filter((property) => property.type !== "title").map((property) => property.id), [properties]);
  const details = useRowPageDetails(layout ?? null, detailIds);
  const { phone } = useSidebarLayout(true);
  if (!row || !valueRow || !context) return null;
  const list = (shape: Pick<Parameters<typeof RowPagePropertyList>[0], "layout" | "only">) => (
    <RowPagePropertyList rowId={row.id} properties={properties} values={row.values}
      valueRow={valueRow} propertyValueContext={context} visitor {...shape} />
  );
  if (!layout) return <div data-public-row-properties={row.id}>{list({})}</div>;
  const placed = pageLayoutPlacedProperties(layout);
  const icons = layout.options.propertyIcons;
  return (
    <div className={layoutStyles.publicLayout} data-public-row-properties={row.id}>
      <RowPagePinnedProperties pinned={layout.heading.pinned} properties={properties} labels={layout.heading.labels} icons={icons}
        values={row.values} valueRow={valueRow} propertyValueContext={context} details={details} />
      <PageLayoutTabProviders environment={tabEnvironment}>
        <RowPageLayoutTabs layout={layout} row={valueRow} context={context} underDetails={detailsToggleLast(layout.heading.pinned, properties, details)}>
          <RowPageLayoutBody layout={layout} details={details} properties={properties} frameMode={phone ? "phone" : "window"}
            valueOf={(propertyId) => valueRow.properties?.[propertyId]}
            renderModule={(module) => module.kind === "properties" ? list({ layout: { items: layout.propertyGroup, placed, icons } })
              : module.kind === "property" ? list({ only: module.propertyId, layout: { items: [], placed: new Set(), icons } }) : null} />
        </RowPageLayoutTabs>
      </PageLayoutTabProviders>
    </div>
  );
}
