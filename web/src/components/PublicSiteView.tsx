"use client";

import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { errorReportUrl, type ErrorReportClass } from "@/lib/errorReport";
import { sharedPageHref, siteNavigationToken } from "@/lib/navigation";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { usePageNames } from "@/lib/pageNames";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { publicContentPageWindow, publicSummariesWithEmoji } from "@/lib/publicContentDocument";
import { createPublicSiteContentService } from "@/lib/publicContentService";
import { PublicFileUrlContext, publicFileUrlsFor } from "@/lib/publicFileUrls";
import { PublicPageHrefContext } from "@/lib/publicPageLinks";
import { PublicPageSourceContext, type PublicPageSource } from "@/lib/publicPageSource";
import { useSitePreviewDraft } from "@/lib/sitePreviewFrame";
import { useRouter, useSearchParams } from "@/lib/router";
import {
  publicContentFailure,
  publicContentFailurePresentation,
  usePublicContentDocument,
  type PublicContentFailure,
} from "@/lib/usePublicContentDocument";
import type { PublicContentPageSummary } from "@/publicContentWire.generated";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import { Search, X } from "./icons";
import { PublicContentPage } from "./page/PublicContentPage";
import { PublicNotFound } from "./PublicNotFound";
import { PublicSiteActions } from "./PublicSiteActions";
import { SharedPageLoading } from "./SharedPageLoading";
import styles from "./PublicSiteView.module.css";

function mergeById<T extends { id: string }>(...lists: Array<readonly T[]>): T[] {
  const merged = new Map<string, T>();
  for (const list of lists) for (const item of list) merged.set(item.id, item);
  return Array.from(merged.values());
}

function reportLink(errorClass: ErrorReportClass, failure: Pick<PublicContentFailure, "reference" | "status">) {
  return errorReportUrl({
    errorClass,
    pathname: typeof window === "undefined" ? "" : window.location.pathname,
    reference: failure.reference,
    status: failure.status,
    userAgent: typeof navigator === "undefined" ? "" : navigator.userAgent,
  });
}

export function PublicSiteView({
  slug,
  onRootSiteNotFound,
}: {
  slug?: string;
  onRootSiteNotFound?: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["publicSiteView", "common"]);
  const router = useRouter();
  const searchParams = useSearchParams();
  const requestedPageId = searchParams.get("page") || undefined;
  // In another site's frame (the publish tab's "이 페이지 임베드"), the page
  // alone: no site bar, path or mark, its title only when asked.
  const embedded = searchParams.get("embed") === "1";
  const embeddedTitle = !embedded || searchParams.get("title") !== "0";
  // In the site editor's frame, the site drawn with the settings being edited.
  const previewing = !embedded && searchParams.get("preview") === "1";
  const { draft, pick } = useSitePreviewDraft(previewing);
  const service = useMemo(() => createPublicSiteContentService(slug), [slug]);
  const { state, page, reload } = usePublicContentDocument(service, requestedPageId);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchExpanded, setSearchExpanded] = useState(false);
  const [remoteSearchPages, setRemoteSearchPages] = useState<PublicContentPageSummary[]>([]);
  const [searchFailure, setSearchFailure] = useState<PublicContentFailure>();
  const searchInputId = useId();
  const searchInputRef = useRef<HTMLInputElement>(null);
  const searchToggleRef = useRef<HTMLButtonElement>(null);
  const menuToggleRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLDivElement>(null);
  const rootSiteNotFound = !slug && state.status === "error"
    && state.failure.phase === "initial" && state.failure.kind === "not-found";

  useEffect(() => {
    if (rootSiteNotFound) onRootSiteNotFound?.();
  }, [onRootSiteNotFound, rootSiteNotFound]);

  useEffect(() => {
    if (searchExpanded) searchInputRef.current?.focus();
  }, [searchExpanded]);

  // A phone has no Escape: a tap outside the search over the bar closes it.
  useEffect(() => {
    if (!searchExpanded) return undefined;
    const onPointer = (event: PointerEvent) => {
      if (searchRef.current?.contains(event.target as Node)) return;
      setSearchQuery("");
      setSearchExpanded(false);
    };
    window.document.addEventListener("pointerdown", onPointer);
    return () => window.document.removeEventListener("pointerdown", onPointer);
  }, [searchExpanded]);

  const document = state.status === "ready" ? state.document : undefined;
  const sourceRevision = document?.sourceRevision;
  const basePath = slug ? `/site/${encodeURIComponent(slug)}` : "/";
  // A page link in an embedded page stays embedded.
  const embedSuffix = [embedded ? `embed=1${embeddedTitle ? "" : "&title=0"}` : "", previewing ? "preview=1" : ""]
    .filter(Boolean).join("&");
  const pageLinkHref = useMemo(() => {
    const navigationToken = siteNavigationToken(basePath);
    return (pageId: string) => {
      const href = sharedPageHref(navigationToken, pageId);
      return embedSuffix ? `${href}${href.includes("?") ? "&" : "?"}${embedSuffix}` : href;
    };
  }, [basePath, embedSuffix]);
  const pageSource = useMemo<PublicPageSource | null>(() => sourceRevision === undefined ? null : {
    site: true,
    read: (pageId, graphCursor, signal) => service.snapshot(
      { pageId, sourceRevision, ...(graphCursor ? { graphCursor } : {}) }, { signal },
    ).then(publicContentPageWindow),
  }, [service, sourceRevision]);
  useEffect(() => {
    const query = searchQuery.trim();
    if (sourceRevision === undefined || !query) {
      setRemoteSearchPages([]);
      setSearchFailure(undefined);
      return;
    }
    const controller = new AbortController();
    service.search({ query, sourceRevision }, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        setRemoteSearchPages(publicSummariesWithEmoji(result.searchResults, result.customEmoji));
        setSearchFailure(undefined);
      })
      .catch((error) => {
        if (controller.signal.aborted) return;
        const failure = publicContentFailure(error, "search");
        setRemoteSearchPages([]);
        if (failure.status === 409) reload();
        else setSearchFailure(failure);
      });
    return () => controller.abort();
  }, [reload, searchQuery, service, sourceRevision]);
  // The site's bar draws its titles' emoji through the site, as its page does (PublicContentPage).
  const siteSlug = document?.site?.slug;
  const siteFiles = useMemo(() => siteSlug ? publicFileUrlsFor({ kind: "site", key: siteSlug }) : null, [siteSlug]);

  // The page coming is drawn in its shape: the one the address names, or one opened from it.
  const loading = <SharedPageLoading pageId={requestedPageId} read={document} />;
  if (rootSiteNotFound || state.status === "loading" || (state.status === "ready" && !page)) return loading;

  // An address that leads nowhere reads as Notion's plain "페이지 찾지 못함".
  // A page missing from a site it does show keeps the site's branding.
  if (state.status === "error" && state.failure.kind === "not-found") {
    return <PublicNotFound branded={state.document?.site?.showBranding ?? true} />;
  }
  if (state.status === "error") {
    const errorClass: ErrorReportClass = `site-${state.failure.kind}`;
    const failure = publicContentFailurePresentation(
      state.failure,
      "publicContent.site",
    );
    return (
      <div className={styles.error} role="alert">
        <strong>{t("publicSiteView:unavailable")}</strong>
        <p>{failure.message}</p>
        <div className={styles.errorActions}>
          <button type="button" onClick={reload}>{t("publicSiteView:tryAgain")}</button>
          <a href={reportLink(errorClass, state.failure)} target="_blank" rel="noopener noreferrer">
            {t("common:actions.reportIssue")}
          </a>
        </div>
      </div>
    );
  }
  if (!page) return loading;

  const saved = state.document.site;
  if (!saved) return loading;
  const site = draft ? {
    ...saved, title: draft.title || saved.title, theme: draft.theme, showBreadcrumbs: draft.showBreadcrumbs,
    showSearch: draft.showSearch, showBranding: draft.showBranding, allowDuplicate: draft.allowDuplicate, showShare: draft.showShare,
    navigationPageIds: draft.navigationPageIds,
  } : saved;
  // Its root page's title, standing for a site's own, as the visitor reads
  // it: its mentions said by the root page's spans (closedMentions.ts).
  const rootPage = state.document.pages.find((candidate) => candidate.id === state.document.rootPageId);
  const siteTitle = titleWithClosedMentions(site.title, rootPage?.title === site.title ? rootPage.titleSpans : undefined);
  // The site's first page is its title, never a shortcut, as Notion's picker (seen 2026-11-05).
  const navigationPages = site.navigationPageIds
    .filter((pageId) => pageId !== state.document.rootPageId)
    .map((pageId) => state.document.pages.find((candidate) => candidate.id === pageId))
    .filter((candidate): candidate is PublicContentPageSummary => Boolean(candidate));
  const query = foldNfcText(searchQuery.trim());
  const matchingNavigationPages = navigationPages.filter(
    (candidate) => !query || foldNfcText(pageNames.title(candidate)).includes(query),
  );
  const searchPages = mergeById(matchingNavigationPages, remoteSearchPages).slice(0, 12);
  const navigationToken = siteNavigationToken(basePath);
  const activePageSummary = state.document.pages.find((candidate) => candidate.id === page.page.id);
  // The page shown is named by its own title's spans, as its header draws them, its custom emoji drawn
  // (TitleWithCustomEmoji); the site's title is its root page's while it is that page's own words, and
  // every page the site lists is named so, as the app's lists name them.
  const shownTitle = <PageTitleText page={page.page} />;
  const rootSummary = state.document.pages.find((candidate) => candidate.id === state.document.rootPageId);
  const siteTitleShown = page.page.id === state.document.rootPageId && site.title.trim() === page.page.title.trim() ? shownTitle
    : rootSummary && site.title.trim() === rootSummary.title.trim() ? <PageTitleText page={rootSummary} /> : siteTitle;

  const openPage = (pageId: string) => {
    const params = new URLSearchParams();
    if (pageId !== state.document.rootPageId) params.set("page", pageId);
    if (embedded) params.set("embed", "1");
    if (!embeddedTitle) params.set("title", "0");
    if (previewing) params.set("preview", "1");
    router.push(params.size ? `${basePath}?${params}` : basePath);
    setSearchQuery("");
    setSearchExpanded(false);
  };
  // Search opens over the bar from its icon on a narrow window, or from the
  // ☰ sheet on a phone; closing it goes back to what opened it.
  const closeMobileSearch = () => {
    setSearchQuery("");
    setSearchExpanded(false);
    requestAnimationFrame(() => {
      const toggle = searchToggleRef.current;
      (toggle?.getClientRects().length ? toggle : menuToggleRef.current)?.focus();
    });
  };

  return (
    <PublicFileUrlContext.Provider value={siteFiles}>
    <section
      className={styles.site}
      data-site-theme={site.theme}
      data-theme={site.theme === "system" ? undefined : site.theme}
      data-site-slug={site.slug}
      data-site-embed={embedded ? (embeddedTitle ? "title" : "plain") : undefined}
    >
      {embedded ? null : <header className={styles.header} data-site-preview={previewing || undefined}
        data-picked={draft?.picked === "header" || undefined}
        onClickCapture={previewing ? (event) => { event.preventDefault(); event.stopPropagation(); pick(); } : undefined}>
        {/* As Notion's site header (seen 2026-11-05): one 44px row, the page's
            path (or its title) on the left, the page shortcuts as plain links
            before the icons on the right. */}
        {site.showBreadcrumbs && page.page.id !== state.document.rootPageId ? (
          <div className={styles.crumbs} aria-label={t("publicSiteView:breadcrumbs")}>
            <button type="button" onClick={() => openPage(state.document.rootPageId)}><span>{siteTitleShown}</span></button>
            <span aria-hidden="true">/</span>
            <span>{activePageSummary ? shownTitle : null}</span>
          </div>
        ) : (
          <button type="button" className={styles.identity} onClick={() => openPage(state.document.rootPageId)}>
            <span>{page.page.id === state.document.rootPageId ? siteTitleShown : activePageSummary ? shownTitle : siteTitleShown}</span>
          </button>
        )}
        <nav className={styles.navigation} aria-label={t("publicSiteView:navigation")}>
          {navigationPages.map((candidate) => (
            <button
              key={candidate.id}
              type="button"
              data-active={candidate.id === page.page.id ? "true" : undefined}
              onClick={() => openPage(candidate.id)}
            >
              <PageTitleText page={candidate} />
            </button>
          ))}
        </nav>
        <div className={styles.tools}>
          {site.showSearch && (
            <div ref={searchRef} className={styles.search} data-expanded={searchExpanded ? "true" : "false"}>
              <button
                ref={searchToggleRef}
                type="button"
                className={styles.searchToggle}
                aria-label={t("publicSiteView:search")}
                aria-controls={searchInputId}
                aria-expanded={searchExpanded}
                onClick={() => setSearchExpanded(true)}
              >
                <Search size="regular" aria-hidden="true" />
              </button>
              <div className={styles.searchField}>
                <Search size="regular" aria-hidden="true" />
                <input
                  ref={searchInputRef}
                  id={searchInputId}
                  value={searchQuery}
                  aria-label={t("publicSiteView:search")}
                  placeholder={t("publicSiteView:search")}
                  onChange={(event) => setSearchQuery(event.currentTarget.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Escape" && searchExpanded) closeMobileSearch();
                  }}
                />
                {searchQuery && (
                  <button type="button" aria-label={t("publicSiteView:clearSearch")} onClick={() => setSearchQuery("")}>
                    <X size="small" aria-hidden="true" />
                  </button>
                )}
                {searchQuery && (
                  <div className={styles.searchResults}>
                    {searchFailure ? (
                      <span>{publicContentFailurePresentation(
                        searchFailure,
                        "publicContent.search",
                      ).message}</span>
                    ) : searchPages.length ? searchPages.map((candidate) => (
                      <button key={candidate.id} type="button" onClick={() => openPage(candidate.id)}>
                        <PageTitleText page={candidate} />
                      </button>
                    )) : <span>{t("publicSiteView:noResults")}</span>}
                  </div>
                )}
              </div>
            </div>
          )}
          <PublicSiteActions title={pageNames.title(activePageSummary) || siteTitle} titleNamed={activePageSummary}
            share={site.showShare}
            duplicate={site.allowDuplicate && slug ? { slug, pageId: page.page.id } : undefined}
            navigation={navigationPages.map((candidate) => ({
              id: candidate.id, label: pageNames.title(candidate), named: candidate, active: candidate.id === page.page.id,
            }))}
            onOpenPage={openPage}
            onSearch={site.showSearch ? () => setSearchExpanded(true) : undefined}
            menuToggleRef={menuToggleRef} />
        </div>
      </header>}
      <main className={styles.content}>
        <PublicPageHrefContext.Provider value={pageLinkHref}>
          <PublicPageSourceContext.Provider value={pageSource}>
            <PublicContentPage
              document={page}
              sharedToken={navigationToken}
              publicSiteSlug={site.slug}
              faviconHref={site.faviconUrl}
              unbranded={!site.showBranding}
              hideTopBar
              pages={state.document.pages}
            />
          </PublicPageSourceContext.Provider>
        </PublicPageHrefContext.Provider>
      </main>
      {site.showBranding && !embedded && <footer className={styles.branding}>{t("publicSiteView:madeWithLeafen")}</footer>}
    </section>
    </PublicFileUrlContext.Provider>
  );
}
