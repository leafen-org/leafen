import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import { api } from "@/api";
import { pageHref } from "@/lib/navigation";
import { libraryHref, replaceRoute } from "@/lib/router";
import { libraryOpeningView } from "@/components/library/libraryViewOrder";
import { readLastOpenedPage, readStartPage } from "@/lib/startPage";
import type { Page, Session, Workspace } from "@/types";
import { pageTrashRoot } from "@/lib/pageTrash";
import { onReloadKeepingPlace, takeKeptPlace } from "@/lib/reloadKeepingPlace";
import { setStartChoosing } from "@/lib/startChoice";
import { isQuickSearchWindow } from "@/lib/quickSearchWindow";

type StartTarget = { page: Page } | { library: true } | null;

// A reload the app made by itself on Home comes back on Home, not on the
// start page: a tab in the background that took a new build at rest showed
// the last page when brought forward (2026-11-06). A reload of the person's
// own at the root still opens the start page, as Notion's does.
let keptHome = typeof window === "undefined" ? false : takeKeptPlace<boolean>("home") === true;

/**
 * Opens a workspace's start page (`lib/startPage.ts`) as Notion does when
 * it starts at its root and when the person switches workspace (seen
 * 2026-10-02), and, for someone new there, its landing page (2026-10-05).
 * A root opened later inside the product stays home, where "내 작업" lives.
 */
export function useStartPage({
  session,
  pathname,
  loaded,
  pages,
  workspace,
  openPage,
  openLibrary,
}: {
  session: Session;
  pathname: string;
  loaded: boolean;
  pages: readonly Page[];
  workspace: Workspace | null;
  openPage: (page: Page) => void;
  openLibrary: (workspace: Workspace) => void;
}) {
  const pagesRef = useRef(pages);
  pagesRef.current = pages;
  // The desktop app's command search window holds the search alone, no page.
  const startedAtRoot = useRef(pathname === "/" && !isQuickSearchWindow());
  useEffect(() => onReloadKeepingPlace("home", () => (window.location.pathname === "/" ? true : null)), []);
  // Started at the root, the first screen waits for the start page.
  useLayoutEffect(() => {
    if (startedAtRoot.current && !keptHome && !started.current) setStartChoosing(true);
    return () => setStartChoosing(false);
  }, []);
  const started = useRef(false);

  const startTarget = useCallback(async (target: Workspace): Promise<StartTarget> => {
    const choice = readStartPage(session.user.id, target.id);
    if (choice === "library") return { library: true };
    const usable = (id: string | null | undefined) => {
      const page = id ? pagesRef.current.find((item) => item.id === id) : undefined;
      // Not a page under a page in the trash, which is in the trash too.
      return page && page.workspaceId === target.id && !pageTrashRoot(page, (parentId) => pagesRef.current.find((item) => item.id === parentId)) ? page : null;
    };
    if (choice === "sidebar_top_page") {
      // The sidebar's first row, once it has this workspace's layout and
      // teamspaces and draws its pages.
      for (let attempt = 0; attempt < 60; attempt += 1) {
        const row = document.querySelector("aside[data-app-sidebar][data-sections-ready] [data-tree-page-id]");
        const page = usable(row?.getAttribute("data-tree-page-id"));
        if (page) return { page };
        await new Promise((resolve) => window.setTimeout(resolve, 50));
      }
      return null;
    }
    const visits = await api.readPageVisits(session, target.id).catch(() => null);
    for (const visit of visits?.visits ?? []) {
      const page = usable(visit.pageId);
      if (page) return { page };
    }
    // Someone who hasn't opened a page here yet, as when they just joined,
    // starts on the workspace's landing page when they can read it.
    if (visits && visits.visits.length === 0) {
      const landing = usable(target.landingPageId);
      if (landing) return { page: landing };
    }
    // Without the server's visits, as when it can't be reached, the page
    // this device opened last.
    const kept = usable(readLastOpenedPage(session.user.id, target.id));
    if (kept) return { page: kept };
    // A workspace the person prepared has no visit yet: it opens on the first
    // page they made there, its welcome page, rather than leaving it behind
    // Home. Someone who joined a workspace made by others, with no landing
    // page, starts on Home, as Notion's new member does — not on whatever
    // shared page the list holds first (seen 2026-11-07).
    const first = pagesRef.current.find((page) => page.workspaceId === target.id && page.createdBy === session.user.id
      && !pageTrashRoot(page, (parentId) => pagesRef.current.find((item) => item.id === parentId)));
    return first ? { page: first } : null;
  }, [session]);

  useEffect(() => {
    // Loaded with no workspace to open: nothing to choose, nothing to wait for.
    if (loaded && !workspace && !started.current) setStartChoosing(false);
    if (started.current || !loaded || !workspace) return;
    started.current = true;
    // Taken by the start that opens a page, not by a mount that didn't get here.
    const home = keptHome;
    keptHome = false;
    if (!startedAtRoot.current || home) {
      setStartChoosing(false);
      return;
    }
    void startTarget(workspace).then((target) =>
      target ? ("page" in target ? pageHref(target.page.id) : libraryHref(libraryOpeningView(session.user.id, workspace))) : null).then((href) => {
      // The person may have gone elsewhere while it was read.
      if (href && window.location.pathname === "/") replaceRoute(href);
    }, () => undefined).finally(() => setStartChoosing(false));
  }, [loaded, session.user.id, startTarget, workspace]);

  /** Opens `target`'s start page after switching to it, or else `fallback`. */
  return useCallback(async (target: Workspace, fallback: () => void) => {
    const found = await startTarget(target);
    if (!found) fallback();
    else if ("page" in found) openPage(found.page);
    else openLibrary(target);
  }, [openLibrary, openPage, startTarget]);
}
