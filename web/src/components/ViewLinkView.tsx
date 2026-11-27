"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { resolvedLanguageForPreference, switchLanguage } from "@/i18n";
import { publicContentPageWindow } from "@/lib/publicContentDocument";
import { createViewLinkContentService } from "@/lib/publicContentService";
import { PublicPageHrefContext } from "@/lib/publicPageLinks";
import { PublicPageSourceContext, type PublicPageSource } from "@/lib/publicPageSource";
import { chooseLinkedDatabaseView } from "@/lib/databaseViewChoice";
import { setDocumentChrome } from "@/lib/documentChrome";
import { usePageFaviconHref } from "@/components/page/pageFavicon";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { applyTheme } from "@/lib/theme";
import { publicContentFailurePresentation, usePublicContentDocument } from "@/lib/usePublicContentDocument";
import { materializeContentGraph } from "@/contentGraph";
import { PublicFileUrlContext, publicFileUrlsFor } from "@/lib/publicFileUrls";
import { PageSlideView } from "./page/PageSlideshow";
import { PublicContentPage } from "./page/PublicContentPage";
import { TopBar } from "./page/TopBar";
import { PublicNotFound } from "./PublicNotFound";
import { SharedPageLoading } from "./SharedPageLoading";
import { publicPageOpening, viewLinkOpeningSuffix } from "@/lib/publicOpeningShape";
import { insideViewFrame, viewLinkFrameFor } from "@/lib/viewLinkScreen";
import { viewLinkErrorCodes } from "@/viewLinkWire.generated";
import { ViewLinkFrameHost } from "./ViewLinkFrameHost";
import styles from "./page/PageView.module.css";
import frameStyles from "./ViewLinkView.module.css";
import { plainTitleOf } from "@/lib/closedMentions";

/** The view link's token, which rides after "#" so no server log holds it. */
function viewLinkToken() {
  if (typeof window === "undefined") return "";
  return new URLSearchParams(window.location.hash.slice(1)).get("token") ?? "";
}

/** The slide a link opens on (`present=N` after "#", 0 the title), or none for the page. */
function presentedViewSlide() {
  if (typeof window === "undefined") return null;
  const value = new URLSearchParams(window.location.hash.slice(1)).get("present");
  return value !== null && /^\d{1,4}$/u.test(value) ? Number(value) : null;
}

/** The secret the page around a frame passes it, after "#" as the token. */
function frameSecret() {
  if (typeof window === "undefined" || !insideViewFrame()) return "";
  return new URLSearchParams(window.location.hash.slice(1)).get("secret") ?? "";
}

/**
 * A view link's page: read-only, at the screen size, theme and language the
 * link was made with (a database on the view it names), for an agent's browser to capture. A browser
 * wider than a tablet or phone screen shows the page in a frame of that size,
 * so the page lays itself out as on that screen. Links to other pages stay
 * plain, as the link shows its one page.
 */
export function ViewLinkView() {
  const { t } = useTranslation(["sharedPageView", "common"]);
  const [token] = useState(viewLinkToken);
  const [framed] = useState(insideViewFrame);
  const [slide] = useState(presentedViewSlide);
  // The secret a person typed lives in this tab's memory only; an agent's
  // browser sends it in a header instead, so the page never sees it.
  const [secret, setSecret] = useState(frameSecret);
  const service = useMemo(() => createViewLinkContentService(token, secret), [secret, token]);
  const { state, page, reload } = usePublicContentDocument(service, undefined, undefined);
  const view = state.status === "ready" ? state.document.view : undefined;
  const sourceRevision = state.status === "ready" ? state.document.sourceRevision : undefined;
  const pageSource = useMemo<PublicPageSource | null>(() => sourceRevision === undefined ? null : {
    site: false,
    read: (pageId, graphCursor, signal) => service.snapshot(
      { pageId, sourceRevision, ...(graphCursor ? { graphCursor } : {}) }, { signal },
    ).then(publicContentPageWindow),
  }, [service, sourceRevision]);
  const pageLinkHref = useMemo(() => () => "#", []);

  // Another view link opened in this tab changes only what follows "#",
  // which doesn't reload the page: load it afresh, its secret not carried.
  useEffect(() => {
    const onHashChange = () => {
      if (viewLinkToken() !== token) window.location.reload();
    };
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, [token]);

  const viewport = view?.viewport ?? "desktop";
  // Laid out as on that screen, in its frame, as the screen shown while it was read was (SharedPageLoading).
  const frameScreen = page && !framed ? viewLinkFrameFor(viewport) : null;
  const hostsFrame = Boolean(frameScreen);

  useEffect(() => {
    if (!view) return;
    applyTheme(view.theme);
    if (view.language) void switchLanguage(resolvedLanguageForPreference(view.language));
    chooseLinkedDatabaseView(view.viewId || null);
  }, [view]);
  // The frame's page names its own window; the window around it says the same.
  const pageFavicon = usePageFaviconHref(hostsFrame ? page?.page : null);
  useEffect(() => {
    if (!hostsFrame || !page) return;
    setDocumentChrome({
      title: plainTitleOf(page.page) || untitledPageDisplayTitle(page.page),
      iconHref: pageFavicon,
      plain: true,
    });
  }, [hostsFrame, page, pageFavicon]);

  if (state.status === "loading" || (state.status === "ready" && !page)) return <SharedPageLoading />;
  if (state.status === "error" && (state.failure.code === viewLinkErrorCodes.secretRequired
    || state.failure.code === viewLinkErrorCodes.secretWrong)) {
    return <ViewLinkSecretForm wrong={state.failure.code === viewLinkErrorCodes.secretWrong} onOpen={(value) => {
      if (value === secret) reload();
      else setSecret(value);
    }} />;
  }
  if (state.status === "error" && state.failure.kind === "not-found") return <PublicNotFound />;
  if (state.status === "error") {
    const failure = publicContentFailurePresentation(state.failure, "publicContent.sharedPage");
    return (
      <>
        <TopBar title={t("sharedPageView:title")} standalone />
        <div className={styles.missing} role="alert">
          <strong>{t("sharedPageView:unavailable")}</strong>
          <p>{failure.message}</p>
          <div className={styles.missingActions}>
            <button type="button" className={styles.restoreButton} onClick={reload}>{t("sharedPageView:tryAgain")}</button>
          </div>
        </div>
      </>
    );
  }
  if (!page) return <SharedPageLoading />;
  if (frameScreen) {
    return (
      <ViewLinkFrameHost screen={frameScreen}>
        <iframe className={frameStyles.frame} data-view-link-frame title={page.page.title || t("sharedPageView:title")}
          src={`/view?frame=1#token=${encodeURIComponent(token)}${secret ? `&secret=${encodeURIComponent(secret)}` : ""}${slide !== null ? `&present=${slide}` : ""}${viewLinkOpeningSuffix(publicPageOpening(page.page))}`}
          width={frameScreen.width} height={frameScreen.height} />
      </ViewLinkFrameHost>
    );
  }
  if (slide !== null) {
    return (
      <div data-view-link={viewport}>
        <PublicFileUrlContext.Provider value={view?.fileKey ? publicFileUrlsFor({ kind: "view", key: view.fileKey }) : null}>
          <PageSlideView page={page.page} graph={materializeContentGraph(page.graph)} index={slide} />
        </PublicFileUrlContext.Provider>
      </div>
    );
  }
  return (
    <div data-view-link={viewport}>
      <PublicPageHrefContext.Provider value={pageLinkHref}>
        <PublicPageSourceContext.Provider value={pageSource}>
          <PublicContentPage document={page} viewLinkFileKey={view?.fileKey} pages={state.document.pages} />
        </PublicPageSourceContext.Provider>
      </PublicPageHrefContext.Provider>
    </div>
  );
}

/**
 * A view link made with a secret asks for it first, for an agent that types
 * rather than sending it in a header; a wrong one says so, and enough of them
 * close the link.
 */
function ViewLinkSecretForm({ wrong, onOpen }: { wrong: boolean; onOpen(secret: string): void }) {
  const { t } = useTranslation(["sharedPageView", "common"]);
  const [value, setValue] = useState("");
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (value.trim()) onOpen(value.trim());
  };
  return (
    <>
      <TopBar title={t("sharedPageView:title")} standalone />
      <form className={styles.missing} onSubmit={submit} data-view-link-secret>
        <strong>{t("sharedPageView:viewSecretTitle")}</strong>
        <p>{t("sharedPageView:viewSecretHint")}</p>
        <input className={frameStyles.secretInput} type="password" autoComplete="off" autoFocus
          aria-label={t("sharedPageView:viewSecretLabel")} placeholder={t("sharedPageView:viewSecretLabel")}
          value={value} onChange={(event) => setValue(event.target.value)} />
        {wrong && <p className={frameStyles.secretWrong} role="alert">{t("sharedPageView:viewSecretWrong")}</p>}
        <div className={styles.missingActions}>
          <button type="submit" className={styles.restoreButton} disabled={!value.trim()}>{t("sharedPageView:viewSecretOpen")}</button>
        </div>
      </form>
    </>
  );
}
