import { api } from "@/api";
import {
  publicContentActions,
  publicContentSourceKinds,
  type PublicContentSearchRequest,
  type PublicContentSnapshotRequest,
} from "@/publicContentWire.generated";

type RequestOptions = { signal?: AbortSignal };
type SnapshotWindow = Omit<
  PublicContentSnapshotRequest,
  "action" | "sourceKind" | "token" | "siteSlug"
>;
type SiteSearchWindow = Omit<PublicContentSearchRequest, "action" | "siteSlug">;

export function createSharedPageContentService(token: string) {
  return {
    check: (pageId: string, options: RequestOptions = {}) => (
      api.checkPublicContent({ action: publicContentActions.check, sourceKind: publicContentSourceKinds.share, token, pageId }, options)
    ),
    discussions: (pageId: string, cursor = "", options: RequestOptions = {}) => (
      api.publicContentDiscussions({
        action: publicContentActions.discussions, sourceKind: publicContentSourceKinds.share, token, pageId,
        ...(cursor ? { cursor } : {}),
      }, options)
    ),
    snapshot: (input: SnapshotWindow = {}, options: RequestOptions = {}) => (
      api.publicContentSnapshot({
        action: publicContentActions.snapshot,
        sourceKind: publicContentSourceKinds.share,
        token,
        ...input,
      }, options)
    ),
  };
}

/** A view link's page, as a view link shows it: its one page. */
/** A view link's pages; secret is what it opens with, when made with one. */
export function createViewLinkContentService(token: string, secret = "") {
  return {
    check: (pageId: string, options: RequestOptions = {}) => (
      api.checkPublicContent({ action: publicContentActions.check, sourceKind: publicContentSourceKinds.view, token, pageId },
        { ...options, viewSecret: secret })
    ),
    snapshot: (input: SnapshotWindow = {}, options: RequestOptions = {}) => (
      api.publicContentSnapshot({
        action: publicContentActions.snapshot,
        sourceKind: publicContentSourceKinds.view,
        token,
        ...input,
      }, { ...options, viewSecret: secret })
    ),
  };
}

export function createPublicSiteContentService(siteSlug?: string) {
  const siteRoute = siteSlug ? { siteSlug } : {};
  return {
    check: (pageId: string, options: RequestOptions = {}) => (
      api.checkPublicContent({ action: publicContentActions.check, sourceKind: publicContentSourceKinds.site, ...siteRoute, pageId }, options)
    ),
    snapshot: (input: SnapshotWindow = {}, options: RequestOptions = {}) => (
      api.publicContentSnapshot({
        action: publicContentActions.snapshot,
        sourceKind: publicContentSourceKinds.site,
        ...siteRoute,
        ...input,
      }, options)
    ),
    search: (input: SiteSearchWindow, options: RequestOptions = {}) => (
      api.searchPublicSiteContent({
        action: publicContentActions.searchSite,
        ...siteRoute,
        ...input,
      }, options)
    ),
  };
}
