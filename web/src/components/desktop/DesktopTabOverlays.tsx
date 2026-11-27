import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { pageCard } from "@/components/editor/PageMentionPreview";
import { MentionPreviewCard } from "@/components/editor/mentionPreviewCard";
import { pagePreviewAgain } from "@/components/editor/pagePreviews";
import rowMenu from "@/components/RowMenu.module.css";
import { closeTabHistoryMenu, openInNewDesktopTab, useDesktopTabPage, type TabHistoryEntry } from "@/lib/desktopTabPage";
import { usePageNames } from "@/lib/pageNames";
import { placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { routeInfoFromPath } from "@/lib/router";
import type { Page, Session } from "@/types";
import type { PagePreviewResponse } from "@/workspaceSearchWire.generated";
import { DesktopStripMenus } from "./DesktopStripMenus";
import styles from "./DesktopTabMenus.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";
import { pagesByIdOf } from "@/lib/pageIndex";

// The strip's height over the page (desktop/window_tabs_darwin.go, window_tabs_windows.go).
const STRIP_HEIGHT = 40;
// A tab's card shows once the pointer has rested on the tab this long, as a
// page mention's card (Notion's, seen 2026-11-10).
const PREVIEW_AFTER_MS = 500;

/** The page a route shows, if it shows one. */
function routePageId(path: string): string | null {
  const route = routeInfoFromPath(new URL(path, "http://route").pathname);
  return route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : null;
}

/**
 * What the Mac or Windows app's shown tab draws for its strip of tabs
 * (lib/desktopTabPage.ts): the card of a tab the pointer rests on, the back
 * and forward buttons' history menu, and a tab's or a group's menu.
 */
export function DesktopTabOverlays({ pages, session, workspaceId }: { pages: Page[]; session: Session; workspaceId: string }) {
  const { history, preview } = useDesktopTabPage();
  const pagesById = pagesByIdOf(pages);
  // What each tab's card last read of its page, which stands while the card
  // shown again reads the page as it is now (TabPreviewCard).
  const [read, setRead] = useState<Record<string, PagePreviewResponse | null>>({});
  const remember = useCallback((pageId: string, preview: PagePreviewResponse | null) => setRead((all) => ({ ...all, [pageId]: preview })), []);
  return <>
    {preview ? <TabPreviewCard key={`${preview.path}:${preview.left}`} pagesById={pagesById} session={session} workspaceId={workspaceId}
      path={preview.path} left={preview.left} right={preview.right} read={read} onRead={remember} /> : null}
    {history ? <TabHistoryMenu entries={history.entries} pagesById={pagesById} /> : null}
    <DesktopStripMenus />
  </>;
}

/**
 * A tab's card, as Notion's app's tab preview (measured 2026-11-10): the
 * page card a mention shows — icon, place, title and first blocks — under
 * the tab the pointer rests on, from its start. Like a mention's card it
 * reads the page again each time it shows, as its body may have changed
 * since (2026-11-13: a tab's card kept its first reading); what was read
 * before stands meanwhile.
 */
function TabPreviewCard({ pagesById, session, workspaceId, path, left, right, read, onRead }: {
  pagesById: Record<string, Page>;
  session: Session;
  workspaceId: string;
  path: string;
  left: number;
  right: number;
  /** What the cards read before, by page. */
  read: Record<string, PagePreviewResponse | null>;
  onRead(pageId: string, preview: PagePreviewResponse | null): void;
}) {
  const pageId = routePageId(path);
  const [due, setDue] = useState(false);
  const cardRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const timer = window.setTimeout(() => setDue(true), PREVIEW_AFTER_MS);
    return () => window.clearTimeout(timer);
  }, []);
  useEffect(() => {
    if (!due || !pageId) return;
    let current = true;
    void pagePreviewAgain(session, pageId).then((preview) => { if (current) onRead(pageId, preview); });
    return () => { current = false; };
  }, [due, pageId, session, onRead]);
  if (!due || !pageId || typeof document === "undefined") return null;
  const card = pageCard(pageId, pageId in read ? read[pageId] : undefined, pagesById, workspaceId);
  if (!card) return null;
  // The tab's place is the strip's, over the page's top.
  const anchor = { left, right, top: -STRIP_HEIGHT, bottom: 0 };
  return <MentionPreviewCard shown={{ kind: "page", id: pageId, element: document.body, anchor }} card={card} cardRef={cardRef}
    marginY={6} />;
}

/**
 * The back or forward button's history menu, as Notion's app's (its web
 * client's tabHistoryPopup, read 2026-11-10): 10px right of the sidebar and
 * 40px down the page, at least 230px wide; each entry's page icon and name,
 * back from the newest or forward from the nearest. An entry goes there; ⌘
 * or a middle press opens it in a new tab behind.
 */
function TabHistoryMenu({ entries, pagesById }: { entries: TabHistoryEntry[]; pagesById: Record<string, Page> }) {
  const pageNames = usePageNames();
  const { t } = useTranslation("desktopTabs");
  const menuRef = useRef<HTMLDivElement>(null);
  const point = useMemo(() => {
    const slot = document.querySelector<HTMLElement>("[data-sidebar-slot]");
    const sidebar = slot && slot.dataset.collapsed !== "true" ? Math.round(slot.getBoundingClientRect().width) : 0;
    return { x: readsRightToLeft() ? window.innerWidth - sidebar - 10 : sidebar + 10, y: 40 };
  }, []);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAtPoint(point, size, viewport, { marginY: 6 }), [point], undefined,
    // It takes the keys from the page as it opens, its first entry in focus,
    // so Escape closes it even from a title's caret (2026-11-10: it stayed).
    { kind: "menu", onEscape: closeTabHistoryMenu, onOutsidePress: closeTabHistoryMenu, initialFocus: "first" });
  const choose = (entry: TabHistoryEntry, event: ReactMouseEvent) => {
    closeTabHistoryMenu();
    if (event.metaKey || event.ctrlKey || event.button === 1) openInNewDesktopTab(entry.path, false);
    else window.history.go(entry.offset);
  };
  return createPortal(<>
    <button type="button" className={rowMenu.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
      aria-label={t("history")} onClick={closeTabHistoryMenu} />
    <div ref={menuRef} role="menu" aria-label={t("history")} className={`${rowMenu.menu} ${styles.historyMenu}`}
      style={popoverPlacementStyle(place)} data-desktop-tab-history>
      {entries.map((entry) => {
        const pageId = routePageId(entry.path);
        const page = pageId ? pagesById[pageId] : undefined;
        return (
          <button key={entry.offset} type="button" role="menuitem" className={`${rowMenu.item} ${styles.menuRow}`} data-history-offset={entry.offset}
            onClick={(event) => choose(entry, event)}
            onAuxClick={(event) => { if (event.button === 1) { event.preventDefault(); choose(entry, event); } }}>
            <i className={styles.historyIcon} aria-hidden="true">
              <PageIconGlyph page={page ?? { icon: "", iconType: "none", workspaceId: "" }} size={20} fallback="file" />
            </i>
            <span className={styles.label}>{page ? pageNames.title(page) : entry.title}</span>
          </button>
        );
      })}
    </div>
  </>, document.body);
}
