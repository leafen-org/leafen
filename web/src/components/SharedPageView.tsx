"use client";

import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { pageHref, sharedPageHref } from "@/lib/navigation";
import { joinShareLink } from "@/lib/shareLinkJoin";
import { publicContentPageWindow } from "@/lib/publicContentDocument";
import { createSharedPageContentService } from "@/lib/publicContentService";
import { PublicPageHrefContext } from "@/lib/publicPageLinks";
import { PublicPageSourceContext, type PublicPageSource } from "@/lib/publicPageSource";
import { pushRoute, useSearchParams } from "@/lib/router";
import {
  publicContentFailurePresentation,
  usePublicContentDocument,
} from "@/lib/usePublicContentDocument";
import { PublicContentPage } from "./page/PublicContentPage";
import { TopBar } from "./page/TopBar";
import type { Session } from "@/types";
import { PublicNotFound } from "./PublicNotFound";
import { PublicDiscussionsButton } from "./page/PublicDiscussionsButton";
import { SharedPageLinkAccess } from "./SharedPageLinkAccess";
import { SharedPageMore } from "./SharedPageMore";
import { SharedPageLoading } from "./SharedPageLoading";
import styles from "./page/PageView.module.css";

export function SharedPageView({ token, session = null }: { token: string; session?: Session | null }) {
  const { t } = useTranslation(["sharedPageView", "common"]);
  const searchParams = useSearchParams();
  const requestedPageId = searchParams.get("page") || undefined;
  const peekPageId = searchParams.get("p") || undefined;
  const service = useMemo(() => createSharedPageContentService(token), [token]);
  const { state, page, reload } = usePublicContentDocument(service, requestedPageId, peekPageId);
  const pageLinkHref = useMemo(() => (pageId: string) => sharedPageHref(token, pageId), [token]);
  const sourceRevision = state.status === "ready" ? state.document.sourceRevision : undefined;
  const pageSource = useMemo<PublicPageSource | null>(() => sourceRevision === undefined ? null : {
    site: false,
    read: (pageId, graphCursor, signal) => service.snapshot(
      { pageId, sourceRevision, ...(graphCursor ? { graphCursor } : {}) }, { signal },
    ).then(publicContentPageWindow),
  }, [service, sourceRevision]);
  // Someone signed in who belongs to the page's organization reads it in the
  // app with the link's role, as Notion opens a public page in the app for
  // someone signed in (seen 2026-11-06); anyone else reads it here.
  const [joining, setJoining] = useState(Boolean(session));
  useEffect(() => {
    if (!session) {
      setJoining(false);
      return undefined;
    }
    let live = true;
    joinShareLink(session, { token, ...(requestedPageId ? { pageId: requestedPageId } : {}) }).then(
      joined => { if (live) pushRoute(pageHref(joined.pageId)); },
      () => { if (live) setJoining(false); },
    );
    return () => { live = false; };
  }, [session, token, requestedPageId]);

  // The page coming is drawn in its shape: the one the link names, or one opened from it.
  const loading = <SharedPageLoading pageId={requestedPageId} read={state.status === "ready" ? state.document : undefined} />;
  if (joining || state.status === "loading" || (state.status === "ready" && !page)) return loading;

  // An address that leads nowhere reads as Notion's plain "페이지 찾지 못함".
  if (state.status === "error" && state.failure.kind === "not-found") return <PublicNotFound />;
  if (state.status === "error") {
    const failure = publicContentFailurePresentation(
      state.failure,
      "publicContent.sharedPage",
    );
    return (
      <>
        <TopBar title={t("sharedPageView:title")} standalone />
        <div className={styles.missing} role="alert">
          <strong>{t("sharedPageView:unavailable")}</strong>
          <p>{failure.message}</p>
          <div className={styles.missingActions}>
            <button type="button" className={styles.restoreButton} onClick={reload}>
              {t("sharedPageView:tryAgain")}
            </button>
          </div>
        </div>
      </>
    );
  }
  if (!page) return loading;

  const linkRole = state.status === "ready" ? state.document.linkRole : undefined;
  return (
    <PublicPageHrefContext.Provider value={pageLinkHref}>
      <PublicPageSourceContext.Provider value={pageSource}>
        {linkRole ? <SharedPageLinkAccess token={token} pageId={page.page.id} role={linkRole} session={session} /> : null}
        <PublicContentPage document={page} sharedToken={token} pages={state.document.pages}
          topBarActions={<><PublicDiscussionsButton pageId={page.page.id} read={service.discussions} />
            {session ? null : <SharedPageMore token={token} pageId={page.page.id} />}</>} />
      </PublicPageSourceContext.Provider>
    </PublicPageHrefContext.Provider>
  );
}
