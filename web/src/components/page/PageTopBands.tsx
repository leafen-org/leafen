import type { Session } from "@/types";
import { useProductPagesSelect } from "../product/productPagesStore";
import { PageRestoredNotice } from "../product/PageRestoredNotice";
import { PublishedSiteBanner } from "./PublishedSiteBanner";
import { PhoneLockBand } from "./PhoneLockBand";

/**
 * The bands under a page's top bar, the one place a page and a database's
 * row page — full, side or center — mount them: a phone's lock (PhoneLockBand), a restored version's notice,
 * then the bar over a page published as a site or open to anyone with its web
 * link (PageSiteBand). A page in the trash has that bar under its trash band
 * instead, as Notion's (seen 2026-11-24: the blue published bar under the red
 * one), which its caller mounts there.
 */
export function PageTopBands({ session, workspaceId, pageId, guest, trashed = false }: {
  session: Session; workspaceId: string; pageId: string; guest?: boolean; trashed?: boolean;
}) {
  return (
    <>
      <PhoneLockBand pageId={pageId} />
      <PageRestoredNotice pageId={pageId} />
      {trashed ? null : <PageSiteBand session={session} workspaceId={workspaceId} pageId={pageId} guest={guest} />}
    </>
  );
}

/**
 * The bar over a page published as a site or open to anyone with its web link
 * (PublishedSiteBanner). A page has one such bar, the published one over the
 * web link's. A workspace's guest is told only of the web link; where the
 * caller does not say whether the reader is one, the workspace list does.
 */
export function PageSiteBand({ session, workspaceId, pageId, guest }: {
  session: Session; workspaceId: string; pageId: string; guest?: boolean;
}) {
  const listedGuest = useProductPagesSelect((pages) =>
    pages?.workspaces?.find((workspace) => workspace.id === workspaceId)?.guest === true);
  return <PublishedSiteBanner session={session} workspaceId={workspaceId} pageId={pageId} guest={guest ?? listedGuest} />;
}
