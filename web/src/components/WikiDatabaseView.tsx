"use client";

import { type ReactNode, type RefObject, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import type { Page } from "@/types";
import { escapeLayerOver, useOverlayLayer } from "@/lib/overlayLayer";
import { usePageNames } from "@/lib/pageNames";
import { forgetNewWiki, isNewWiki } from "@/lib/wikiChangeEvents";
import { requestSearch } from "@/lib/searchRequest";
import { addressWithView } from "@/lib/pagePeek";
import { replaceRoute, routeInfoFromPath, useSearchParams } from "@/lib/router";
import { WIKI_HOME_VIEW_ID, wikiHomeIndex, type WikiHome } from "@/lib/wikiHome";
import { Search } from "@/icons/leafen";
import { TEMPLATE_PAGE_PARAM } from "./database/useTemplateAddress";
import { WikiHomeSettingsPanel } from "./database/WikiHomeSettingsPanel";
import { WikiHomeViewPicker } from "./database/WikiHomeViewPicker";
import { databaseViewLabels } from "./database/databaseViewLabels";
import { WikiViewsContext, type WikiViews, type WikiViewsRequest } from "./database/wikiViewsContext";
import styles from "./WikiView.module.css";
import { imeComposing } from "@/lib/fieldKeyBoundary";

const WELCOMED_KEY = "leafen:wiki-welcomed";

// The first wiki someone makes here greets them; later ones don't.
function firstWikiHere() {
  try {
    if (window.localStorage.getItem(WELCOMED_KEY)) return false;
    window.localStorage.setItem(WELCOMED_KEY, "1");
  } catch {
    // Without storage the greeting shows each time, which does no harm.
  }
  return true;
}

/** This address naming a view (`v=`), its peeked page and the rest kept. */
function addressNaming(viewId: string) {
  const url = addressWithView(new URL(window.location.href), viewId);
  return `${url.pathname}${url.search}${url.hash}`;
}

export type WikiDatabaseViews = {
  views: readonly GraphView[];
  activeViewId: string;
  /** Home's own name and place among the views. */
  home: WikiHome;
  /** Whether the wiki opens on its Home, as this person last chose here or as it stands first. */
  homeChosen: boolean;
  /** Whether this person keeps the views as they are. */
  readOnly: boolean;
  onSelectView: (viewId: string) => void;
  /** Keeps Home as the view this person chose here. */
  onChooseHome: () => void;
  /** Moves a row of the views list before or after another, Home's (`home`) among them, for everyone. */
  onReorder: (sourceId: string, targetId: string, side: "before" | "after") => void;
  /** Names Home for everyone; empty, it is "홈" again. */
  onRenameHome: (name: string) => void;
  /** The wiki's database, showing its active view. */
  database: ReactNode;
};

/**
 * A wiki whose pages are its database's pages shows its Home, the wiki's
 * page without its database, or one of its database's views, its picker on
 * the database's toolbar row with Home in it (WikiViews). It opens on the
 * view its address names (`v=`, Home's `home`), else the one this person last
 * chose here, else the first, and its address then names it, as Notion's
 * (seen 2026-11-11); a page the address peeks (`p=`) opens over it. A wiki
 * just made greets its maker under "홈" with a welcome to the new wiki, as
 * Notion's (seen 2026-10-11), and pressing "홈" then "새로운 보기 — …".
 */
export function WikiDatabaseView({
  root,
  wiki,
  children,
}: {
  root: Page;
  wiki: WikiDatabaseViews;
  children: ReactNode;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("wikiView");
  // A template its address names (a favorite's) opens over the wiki's
  // database, whose toolbar keeps its templates.
  const search = useSearchParams();
  const namedTemplate = search.get(TEMPLATE_PAGE_PARAM), named = search.get("v");
  const namesView = Boolean(named && wiki.views.some(({ id }) => id === named));
  const home = !namedTemplate && (named === WIKI_HOME_VIEW_ID || (!namesView && wiki.homeChosen));
  // The view the address names is kept as this person's choice, as Notion's
  // (seen 2026-11-11: opened at "모든 페이지"'s address, the wiki reopened
  // there); Home's address follows Home, a view's its database
  // (useDatabaseViewController).
  const { homeChosen, onChooseHome, onSelectView, activeViewId } = wiki;
  useEffect(() => {
    if (namesView && named && (homeChosen || named !== activeViewId)) onSelectView(named);
    if (!home) return;
    if (!homeChosen) onChooseHome();
    if (named !== WIKI_HOME_VIEW_ID) replaceRoute(addressNaming(WIKI_HOME_VIEW_ID));
  }, [activeViewId, home, homeChosen, named, namesView, onChooseHome, onSelectView]);
  const chooseView = (viewId: string) => {
    wiki.onSelectView(viewId);
    replaceRoute(addressNaming(viewId));
  };
  // The wiki going back to a page — its notice's undo, its menu's, another
  // device's — leaves its page at the page's own address, without the view
  // the wiki named, as Notion's (seen 2026-11-14 after the notice's undo);
  // a wiki left for another page leaves that page's address alone.
  const viewIds = useRef<string[]>([]);
  viewIds.current = wiki.views.map(({ id }) => id);
  useEffect(() => () => {
    const url = new URL(window.location.href), shown = url.searchParams.get("v");
    const route = routeInfoFromPath(url.pathname);
    if (route.kind !== "page" || route.pageId !== root.id || !shown || (shown !== WIKI_HOME_VIEW_ID && !viewIds.current.includes(shown))) return;
    url.searchParams.delete("v");
    replaceRoute(`${url.pathname}${url.search}${url.hash}`);
  }, [root.id]);
  const [tip, setTip] = useState<"welcome" | "views" | null>(() => (isNewWiki(root.id) ? "welcome" : null));
  const tipRef = useRef<HTMLDivElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const greeted = useRef(false);
  useEffect(() => {
    if (tip !== "welcome" || greeted.current) return;
    greeted.current = true;
    forgetNewWiki(root.id);
    if (!firstWikiHere()) setTip(null);
  }, [root.id, tip]);
  // "홈" itself moves the greeting on rather than closing it. A press anywhere else closes it and goes on to what it
  // pressed: the greeting put itself up, so it takes no press from the page (2026-11-18: the first press on the top
  // bar's "⋯" right after 위키로 전환 only closed the greeting, and the menu opened on the second).
  useOverlayLayer(tip !== null, { kind: "card", ref: tipRef, inside: () => [wrapRef.current?.querySelector("[data-wiki-view-menu-trigger]") ?? null],
    onEscape: () => setTip(null), onOutsidePress: () => setTip(null) });
  // Escape closes it wherever the focus is, as Notion's (the caret stays in
  // the page under it); a popup opened over it closes first.
  useEffect(() => {
    if (!tip) return;
    const close = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented || imeComposing(event)) return;
      if (escapeLayerOver(wrapRef.current, event)) { event.preventDefault(); event.stopPropagation(); }
    };
    document.addEventListener("keydown", close, true);
    return () => document.removeEventListener("keydown", close, true);
  }, [tip]);
  // The first press on the picker while the greeting shows moves it on.
  const onTrigger = () => {
    if (tip !== "welcome") {
      setTip(null);
      return false;
    }
    setTip("views");
    return true;
  };
  // A view's action or a new view chosen on Home runs once the views show.
  const request = useRef<WikiViewsRequest | null>(null);
  // Home's own name shows wherever Home is named, its place among the views in their lists.
  const homeLabel = wiki.home.name || t("home"), homeIndex = wikiHomeIndex(wiki.views, wiki.home);
  const [renaming, setRenaming] = useState(false);
  // Its settings hang under the views' picker, wherever it shows.
  const dockRef: RefObject<HTMLElement | null> = useMemo(() => ({
    get current() { return wrapRef.current?.querySelector<HTMLElement>("[data-wiki-view-menu-trigger]") ?? null; },
  }), []);
  const views: WikiViews = useMemo(() => ({ homeLabel, homeIndex, chooseHome: () => { onChooseHome(); replaceRoute(addressNaming(WIKI_HOME_VIEW_ID)); },
    reorder: wiki.onReorder, renameHome: () => setRenaming(true), onTrigger,
    takeRequest: () => { const taken = request.current; request.current = null; return taken; } }),
  // eslint-disable-next-line react-hooks/exhaustive-deps
  [homeIndex, homeLabel, onChooseHome, tip, wiki.onReorder]);

  return (
    <div ref={wrapRef} className={styles.wiki} data-wiki-database-view>
      {/* Its views on the left and "검색" within the wiki on the right, as Notion's (seen 2026-10-31). */}
      {home ? <div className={styles.viewRow}>
      <WikiHomeViewPicker label={databaseViewLabels().viewsFor(pageNames.title(root))} homeLabel={homeLabel} homeIndex={homeIndex} views={wiki.views}
        readOnly={wiki.readOnly} onReorder={wiki.onReorder} onRenameHome={() => setRenaming(true)} onTrigger={onTrigger}
        onSelectView={chooseView}
        onRequest={(asked) => { request.current = asked; chooseView(wiki.activeViewId); }} />
      <button type="button" className={styles.viewSearch} data-wiki-search onClick={() => requestSearch({ inPages: [root.id] })}>
        <Search size="small" aria-hidden="true" />{t("search")}
      </button>
      </div> : null}
      {tip ? (
        <div ref={tipRef} className={styles.tip} role="dialog" aria-label={t(tip === "welcome" ? "welcomeTitle" : "viewsTitle")} data-wiki-tip={tip}>
          <strong>{t(tip === "welcome" ? "welcomeTitle" : "viewsTitle")}</strong>
          <p>{t(tip === "welcome" ? "welcomeBody" : "viewsBody")}</p>
        </div>
      ) : null}
      {renaming && !wiki.readOnly ? createPortal(<WikiHomeSettingsPanel name={wiki.home.name} placeholder={t("home")} dockRef={dockRef}
        onRename={wiki.onRenameHome} onClose={() => setRenaming(false)} />, document.body) : null}
      {home ? <div className={styles.home}>{children}</div> : (
        <WikiViewsContext.Provider value={views}><div className={styles.database}>{wiki.database}</div></WikiViewsContext.Provider>
      )}
    </div>
  );
}
