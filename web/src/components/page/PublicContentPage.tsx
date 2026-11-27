import { Suspense, useMemo, type ReactNode } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { materializeContentGraph, type ContentGraph, type GraphBlock, type GraphDataSource, type GraphView } from "@/contentGraph";
import { PublicFileUrlContext, publicFileUrlsFor } from "@/lib/publicFileUrls";
import { writeDatabaseViewChoice } from "@/lib/databaseViewChoice";
import { StructuredEditor } from "@/StructuredEditor";
import type { PublicContentPageDocument } from "@/lib/publicContentDocument";
import type { PublicContentPageSummary } from "@/publicContentWire.generated";
import { PublicMentionIconStyles } from "../editor/PublicMentionIconStyles";
import { PublicMentionPreview } from "../editor/PublicMentionPreview";
import { PageView } from "./PageView";
import { databasePageSource } from "./DatabaseDescription";

// A shared database row shows its properties; their cells' code comes only
// for such a page.
const PublicRowPageProperties = lazyScreen(() => import("../database/PublicRowPageProperties")
  .then((module) => ({ default: module.PublicRowPageProperties })));

const ignoreTitle = (_title: string) => {};
const ignorePresentation = () => {};
const ignoreFocus = () => {};
const ignoreAdd = () => {};
const ignoreParagraph = (_block: GraphBlock, _text: string) => {};
const ignoreDatabaseTitle = (_source: GraphDataSource, _title: string) => {};
// A visitor switches a database's views, as on Notion's published pages; the
// choice lasts the visit and changes nothing of the page.
const chooseView = (block: GraphBlock, view: GraphView) => writeDatabaseViewChoice(undefined, block.id, view.id);

/** A published page's body, read-only. */
export function PublicPageBody({
  document,
  graph,
  staticReadOnlySemantics,
}: {
  document: PublicContentPageDocument;
  graph: ContentGraph;
  staticReadOnlySemantics: boolean;
}) {
  return (
    <StructuredEditor
      pageId={document.page.id}
      showPageOutline={document.page.kind !== "database" && document.page.tableOfContentsDisplay !== "off"}
      workspaceId={document.page.workspaceId}
      session={null}
      collaborationDocument={null}
      graph={graph}
      loading={false}
      readOnly
      fixedBody={document.page.kind === "database"}
      staticReadOnlySemantics={staticReadOnlySemantics}
      onAddParagraph={ignoreAdd}
      onAddDatabase={ignoreAdd}
      onChangeParagraph={ignoreParagraph}
      onChangeDatabaseTitle={ignoreDatabaseTitle}
      onSelectView={chooseView}
    />
  );
}

export function PublicContentPage({
  document,
  sharedToken,
  publicSiteSlug,
  viewLinkFileKey,
  faviconHref,
  unbranded = false,
  hideTopBar = false,
  topBarActions,
  pages,
}: {
  document: PublicContentPageDocument;
  sharedToken?: string;
  publicSiteSlug?: string;
  /** A view link's file key, from its open: its files come through it. */
  viewLinkFileKey?: string;
  /** A published site's own favicon, shown instead of the page icon. */
  faviconHref?: string;
  /** A published site with Leafen's branding off: the window names the page alone. */
  unbranded?: boolean;
  hideTopBar?: boolean;
  topBarActions?: ReactNode;
  /** The published pages the visitor reads; given, the page draws its mentions' icons and cards. */
  pages?: readonly PublicContentPageSummary[];
}) {
  const graph = useMemo(() => materializeContentGraph(document.graph), [document.graph]);
  // A full-page database is drawn as its own page is for its owner, for a web
  // link's, a site's and a view link's visitor alike: laid on the whole canvas
  // with its table under its title (PageView's databasePage), and headed with
  // its name with its formatting and its description, read in one place
  // (databasePageSource), without its writes.
  const fullPageDatabase = document.page.kind === "database";
  const database = fullPageDatabase ? databasePageSource(graph, "database", null) : undefined;
  // Files are read through the site when one is shown, else the share or view link.
  const publicFileUrl = useMemo(() => {
    const source = publicSiteSlug
      ? { kind: "site" as const, key: publicSiteSlug }
      : sharedToken ? { kind: "share" as const, key: sharedToken }
        : viewLinkFileKey ? { kind: "view" as const, key: viewLinkFileKey } : null;
    return source ? publicFileUrlsFor(source) : null;
  }, [publicSiteSlug, sharedToken, viewLinkFileKey]);
  return (
    <PublicFileUrlContext.Provider value={publicFileUrl}>
    <PageView
      page={document.page}
      onTitleChange={ignoreTitle}
      databasePage={fullPageDatabase}
      databaseDescription={database}
      onPresentationChange={ignorePresentation}
      onFocusBody={ignoreFocus}
      publicReadOnly
      sharedToken={sharedToken}
      publicSiteSlug={publicSiteSlug}
      faviconHref={faviconHref}
      unbranded={unbranded}
      hideTopBar={hideTopBar}
      topBarActions={topBarActions}
    >
      {graph.rows.some((row) => row.id === document.page.id)
        ? <Suspense fallback={null}><PublicRowPageProperties page={document.page} graph={graph} layout={document.pageLayout} /></Suspense> : null}
      <PublicPageBody document={document} graph={graph} staticReadOnlySemantics={Boolean(publicSiteSlug)} />
    </PageView>
    {pages ? (
      <>
        <PublicMentionIconStyles workspaceId={document.page.workspaceId} listed={pages} />
        <PublicMentionPreview pages={pages} workspaceId={document.page.workspaceId} />
      </>
    ) : null}
    </PublicFileUrlContext.Provider>
  );
}
