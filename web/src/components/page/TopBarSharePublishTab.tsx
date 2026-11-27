import { useState } from "react";
import type { useTranslation } from "react-i18next";
import type { ProductStatusSink } from "@/lib/productStatus";
import { pushRoute } from "@/lib/router";
import { serverUrl } from "@/lib/serverOrigin";
import { publicMutationBounds, type PublicMutationInput, type PublicMutationState } from "@/publicMutationWire.generated";
import type { Session } from "@/types";
import type { ShareFieldErrors, ShareSiteField } from "./ShareErrorLine";
import { SiteStyleEditor } from "./SiteStyleEditor";
import { TopBarSharePublishInTrash, TopBarSharePublishNeedsAccess, TopBarShareSitePanel } from "./TopBarShareSitePanel";
import type { SitePreviewSettings } from "./TopBarShareSitePreview";
import {
  defaultSiteSlug, normalizeSiteSlug, shareChangeTook, siteNavigationCandidates, type ShareMutationOutcome, type SharePage,
} from "./topBarShareModel";

const EMPTY_SITE_PREVIEW: SitePreviewSettings = { previewImageFileId: "", faviconFileId: "", googleAnalyticsId: "", allowIndexing: false, allowDuplicate: true, showShare: true };

/**
 * The site as the "게시" tab is editing it: what the server last said, or a
 * new site's defaults, until a publish sends it. A failed publish from the
 * site editor keeps what is being edited there (the window skips hydrate).
 */
export function useShareSiteDraft(page: SharePage, untitled: string, onStatus: ProductStatusSink, t: ReturnType<typeof useTranslation>["t"]) {
  const [slug, setSlug] = useState(() => defaultSiteSlug(page, untitled));
  // An empty title follows the page's name as it changes (2026-10-16).
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [theme, setTheme] = useState<"system" | "light" | "dark">("system");
  const [breadcrumbs, setBreadcrumbs] = useState(true);
  const [search, setSearch] = useState(true);
  const [branding, setBranding] = useState(true);
  // A new site has no page shortcuts, as Notion's (seen 2026-11-05).
  const [navigationPageIds, setNavigationPageIds] = useState<string[]>([]);
  const [preview, setPreview] = useState<SitePreviewSettings>(EMPTY_SITE_PREVIEW);
  // The site editor, open over everything from "사이트 스타일 사용자 지정".
  const [styling, setStyling] = useState(false);
  // Why the last change refused what the site's fields hold — an address
  // another site has, or one the site can't have, an Analytics ID, an image,
  // a link title or description — each said under its own field
  // (shareFieldErrors).
  const [fieldErrors, setFieldErrors] = useState<ShareFieldErrors>({});

  function hydrate(next: PublicMutationState) {
    if (!next.site) {
      setSlug(defaultSiteSlug(page, untitled));
      setTitle("");
      setDescription("");
      setTheme("system");
      setBreadcrumbs(true);
      setSearch(true);
      setBranding(true);
      setNavigationPageIds([]);
      setPreview(EMPTY_SITE_PREVIEW);
      return;
    }
    setSlug(next.site.published ? next.site.slug : defaultSiteSlug(page, untitled)); // Taken down, it is published anew at its first path, as Notion's (2026-11-13).
    setTitle(next.site.title);
    setDescription(next.site.description);
    setTheme(next.site.theme);
    setBreadcrumbs(next.site.showBreadcrumbs);
    setSearch(next.site.showSearch);
    setBranding(next.site.showBranding);
    setNavigationPageIds(next.site.navigationPageIds);
    setPreview({
      previewImageFileId: next.site.previewImageFileId,
      faviconFileId: next.site.faviconFileId,
      googleAnalyticsId: next.site.googleAnalyticsId,
      allowIndexing: next.site.allowIndexing,
      allowDuplicate: next.site.allowDuplicate,
      showShare: next.site.showShare,
    });
  }

  function toggleNavigationPage(pageId: string) {
    setNavigationPageIds((current) => {
      if (current.includes(pageId)) return current.filter((candidate) => candidate !== pageId);
      if (current.length >= publicMutationBounds.navigationPages) {
        onStatus(t("siteBuilder:navigationLimit"), "error");
        return current;
      }
      return [...current, pageId];
    });
  }

  return {
    slug, setSlug, title, description, theme, setTheme, breadcrumbs, setBreadcrumbs, search, setSearch, branding, setBranding,
    navigationPageIds, setNavigationPageIds, toggleNavigationPage, preview, setPreview, styling, setStyling, fieldErrors, setFieldErrors, hydrate,
  };
}

export type ShareSiteDraft = ReturnType<typeof useShareSiteDraft>;

function publishedSiteHref(state: PublicMutationState | null) {
  if (!state?.site?.published) return "";
  return serverUrl(`/site/${encodeURIComponent(state.site.slug)}`);
}

/**
 * The share window's "게시" tab: the page in the trash, a reader's note, or
 * the site's panel and its style editor. A page in the trash that was never
 * published says it can't be, with "복원" (Notion's, seen 2026-11-13); one
 * whose site is published keeps the published tab as it is, with no
 * restore there — Notion restores it only from the page's trash band (seen
 * 2026-11-24).
 */
export function TopBarSharePublishTab({ site, state, session, page, pages, untitled, busy, controlsDisabled, copied, errorCode, setErrorCode,
  onCopy, runMutation, onRestore, inTrash, hydrate, close, closeNow, t }: {
  site: ShareSiteDraft; state: PublicMutationState | null; session: Session; page: SharePage; pages: SharePage[]; untitled: string;
  busy: boolean; controlsDisabled: boolean; copied: boolean; errorCode: string; setErrorCode(next: string): void;
  onCopy(siteLink: string): void;
  runMutation(input: Omit<PublicMutationInput, "requestId" | "expectedRevision">, successMessage?: string,
    change?: { address?: boolean }): Promise<ShareMutationOutcome>;
  /** "복원" for a page in the trash, where the window can restore it. */
  onRestore?: () => void; inTrash: boolean;
  hydrate(next: PublicMutationState): void; close(): void; closeNow(): void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const siteLink = typeof window === "undefined" ? "" : publishedSiteHref(state);
  // Where the published panel's last change was made: a refusal of the
  // address row says why under it, any other at the end of the screen it
  // was made on (ShareErrorLine).
  const [changedAddress, setChangedAddress] = useState(false);
  // A field typed or chosen again puts away why it was refused, and the
  // window's line that said it.
  const fieldChanged = (...fields: ShareSiteField[]) => {
    if (!fields.some((field) => site.fieldErrors[field])) return;
    site.setFieldErrors(Object.fromEntries(Object.entries(site.fieldErrors).filter(([field]) => !fields.includes(field as ShareSiteField))));
    setErrorCode("");
  };
  const navigationCandidates = siteNavigationCandidates(
    page,
    pages,
    publicMutationBounds.navigationPages,
  );
  // Publishing or updating the site, with these share settings; the
  // address as the site will have it, from either place it is typed, and
  // its link title and description as "검색 엔진 인덱싱" saves them.
  const publishSite = (preview: SitePreviewSettings,
    message = t(state?.site?.published ? "siteBuilder:updated" : "siteBuilder:published"), slug = site.slug,
    named = { title: site.title, description: site.description }, address = false) => {
    setChangedAddress(address);
    return runMutation({
      action: "publish_site", slug: normalizeSiteSlug(slug) || slug, title: named.title.trim(), description: named.description,
      theme: site.theme, showBreadcrumbs: site.breadcrumbs, showSearch: site.search, showBranding: site.branding,
      navigationPageIds: site.navigationPageIds, ...preview,
    }, message, { address });
  };

  return <>
    {inTrash && !state?.site?.published ? <TopBarSharePublishInTrash onRestore={onRestore} /> : state?.canManage === false ? <TopBarSharePublishNeedsAccess /> : <TopBarShareSitePanel
      busy={busy}
      controlsDisabled={controlsDisabled}
      copied={copied}
      errorCode={errorCode}
      navigationCandidates={navigationCandidates}
      onCopy={() => onCopy(siteLink)}
      // The page's path edited in the address row, as Notion's toast says (seen 2026-11-07).
      onAddressChange={async (slug) => {
        fieldChanged("slug");
        return shareChangeTook(await publishSite(site.preview, t("siteBuilder:addressUpdated"), slug, undefined, true));
      }}
      addressError={changedAddress && !site.styling ? errorCode : ""}
      // A link title or description the site can't have says so under its
      // field on the indexing screen, and not again at its end.
      searchErrors={site.styling ? {} : { title: site.fieldErrors.title, description: site.fieldErrors.description }}
      failure={changedAddress || site.styling || site.fieldErrors.title || site.fieldErrors.description ? "" : errorCode}
      onScreenChange={() => { setErrorCode(""); site.setFieldErrors({}); }}
      // The tab turning to the published site's rows says it; Notion shows no toast (seen 2026-11-06).
      onPublish={() => void publishSite(site.preview, "")}
      onIndexingChange={(allowIndexing) => {
        const next = { ...site.preview, allowIndexing };
        site.setPreview(next);
        void publishSite(next);
      }}
      onSearchDetailsSave={async (title, description) => shareChangeTook(
        await publishSite(site.preview, undefined, site.slug, { title, description }))}
      onSearchTyped={(field) => fieldChanged(field)}
      onDuplicateChange={(allowDuplicate) => {
        const next = { ...site.preview, allowDuplicate };
        site.setPreview(next);
        void publishSite(next);
      }}
      onManageSites={() => {
        close();
        pushRoute(`/settings?section=public-pages&from=${encodeURIComponent(`${window.location.pathname}${window.location.search}`)}`);
      }}
      onStyle={() => { setErrorCode(""); site.setStyling(true); }}
      // The tab turning back to "웹에 게시" says it; Notion shows no toast (seen 2026-11-06).
      onUnpublish={() => { setChangedAddress(false); void runMutation({ action: "unpublish_site" }); }}
      page={page}
      publishingAllowed={state?.publishingAllowed ?? true}
      published={state?.site?.published ?? false}
      session={session}
      siteLink={siteLink}
      sitePreview={site.preview}
      siteTitle={site.title} siteDescription={site.description} siteDefaultDescription={state?.site?.defaultDescription ?? ""}
      untitled={untitled}
    />}
    {/* "취소" leaves what was published and "변경 사항 게시" publishes;
        either closes the editor and the share menu with it, as Notion's. */}
    {site.styling && state?.site?.published ? <SiteStyleEditor
      session={session} page={page} untitled={untitled} siteLink={siteLink} busy={busy} disabled={controlsDisabled}
      publishingAllowed={state.publishingAllowed ?? true} errorCode={errorCode} navigationCandidates={navigationCandidates}
      fieldErrors={site.fieldErrors}
      siteTitle={site.title} siteSlug={site.slug} siteTheme={site.theme}
      siteBreadcrumbs={site.breadcrumbs} siteSearch={site.search} siteBranding={site.branding}
      siteNavigationPageIds={site.navigationPageIds} sitePreview={site.preview}
      onSlugChange={(value) => { site.setSlug(value); fieldChanged("slug"); }}
      onThemeChange={site.setTheme} onBreadcrumbsChange={site.setBreadcrumbs} onSearchChange={site.setSearch}
      onBrandingChange={site.setBranding} onToggleNavigation={site.toggleNavigationPage} onNavigationOrderChange={site.setNavigationPageIds}
      onSitePreviewChange={(next) => {
        site.setPreview(next);
        fieldChanged(...(["googleAnalyticsId", "previewImageFileId", "faviconFileId"] as const).filter((key) => next[key] !== site.preview[key]));
      }}
      onCancel={() => { hydrate(state); setErrorCode(""); site.setFieldErrors({}); site.setStyling(false); closeNow(); }}
      onPublish={() => void publishSite(site.preview, t("siteBuilder:changesPublished")).then((outcome) => {
        if (shareChangeTook(outcome)) { site.setStyling(false); closeNow(); }
      })}
    /> : null}
  </>;
}
