import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { viewDisplayName } from "@/components/database/databaseViewShared";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { DotsHorizontal, SidebarChevronRight } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { readPeopleDatabase, type PeopleDatabase } from "@/lib/peopleDatabase";
import { peoplePeekHref } from "@/lib/peopleDatabasePage";
import { editorName } from "@/lib/peopleDirectory";
import { PEOPLE_DIRECTORY_HREF } from "@/lib/peopleProfiles";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { serverUrl } from "@/lib/serverOrigin";
import type { Page, Session } from "@/types";
import { setPeopleDatabaseFavorite } from "@/components/people/peopleFavorite";
import { FavoriteDatabaseMenu } from "./SidebarFavoriteDatabaseRow";
import { onTreeRowKeyDown } from "./pageTreeDom";
import { usePageTreeContext } from "./pageTreeContext";
import { treeMenuPoint } from "./pageTreeModel";
import { styles as treeStyles } from "./sidebarStyles";

/**
 * People among favorites, as Notion's sidebar lists a starred People (seen
 * 2026-11-16 in Chrome at 1440, a free workspace's owner): its person in a
 * ring and "People", "›" under the pointer to show its views — "• Directory
 * View", "• Card View", each opening People at it — and "⋯" alone (no "+":
 * People's rows are the server's). Pressing it opens People; its "⋯" is a
 * database's favorite menu under "데이터베이스" — 즐겨찾기 해제, 링크 복사,
 * 새 탭에서 열기, 사이드 보기에서 열기 and who edited it last (Notion's
 * "데이터베이스 사용법" opens Notion's help center, left out as a database's
 * menu leaves it). Its link is People's page's, which opens People.
 * ⌥-pressed or by "사이드 보기에서 열기" it opens beside the page open, as
 * Notion's (seen 2026-11-16 in two tabs at two times, PeoplePeek).
 */
export function SidebarFavoritePeopleRow({ page, link, session }: { page: Page; link: string; session: Session }) {
  const { t } = useTranslation(["rowMenu", "pageTreeItem"]);
  const toast = useOptionalProductToast();
  const { expandedPageIds, setPageExpanded } = usePageTreeContext();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [database, setDatabase] = useState<PeopleDatabase | null>(null);
  const moreRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const expansionKey = `favorite:${page.id}`;
  const expanded = expandedPageIds.has(expansionKey);
  const href = PEOPLE_DIRECTORY_HREF;
  // Its views are read as it opens, or as its menu does (who changed it last).
  const wanted = expanded || menu !== null;
  useEffect(() => {
    if (!wanted || database) return undefined;
    const controller = new AbortController();
    readPeopleDatabase(session, page.workspaceId, controller.signal).then(setDatabase, () => {});
    return () => controller.abort();
  }, [database, page.workspaceId, session, wanted]);
  const views = database ? database.graph.views.filter((view) => view.dataSourceId === database.dataSourceId) : [];
  async function unfavorite() {
    setMenu(null);
    try {
      const current = database ?? await readPeopleDatabase(session, page.workspaceId);
      await setPeopleDatabaseFavorite(session, page.workspaceId, page.id, false, current.favoriteRevision);
    } catch {
      toast?.notify(t("rowMenu:toast.couldntUpdateFavorites"), "error");
    }
  }
  const open = (to: string, newTab: boolean) => (newTab ? window.open(to, "_blank", "noopener") : pushRoute(to));
  return (
    <>
      <div className={treeStyles.treeRow} role="treeitem" aria-level={1} aria-expanded={expanded} data-favorite-people={page.id}
        data-hover-leading="chevron" data-has-children={views.length > 0} style={{ paddingInlineStart: 8 }} tabIndex={0}
        onClick={(event) => (event.altKey && !event.metaKey && !event.ctrlKey ? pushRoute(peoplePeekHref(page.id)) : open(href, event.metaKey || event.ctrlKey))}
        onKeyDown={(event) => onTreeRowKeyDown(event, {
          open: (newTab) => open(href, newTab),
          expanded, setExpanded: (next) => setPageExpanded(expansionKey, next), menu: () => moreRef.current?.click(),
        })}>
        <span className={treeStyles.treeLeading} data-tree-leading="true">
          <button type="button" className={`${treeStyles.disclosure} ${expanded ? treeStyles.disclosureOpen : ""}`}
            aria-label={t(expanded ? "pageTreeItem:hideInside" : "pageTreeItem:showInside")} aria-describedby={titleId}
            aria-expanded={expanded} data-tree-disclosure="true"
            onClick={(event) => { event.stopPropagation(); setPageExpanded(expansionKey, !expanded); }}>
            <SidebarChevronRight size="micro" />
          </button>
          <span className={treeStyles.treeIcon} data-tree-icon="true"><PageIconGlyph page={page} size={18} /></span>
        </span>
        <span id={titleId} className={treeStyles.treeTitle} data-tree-title="true">{page.title}</span>
        <span className={treeStyles.treeActions}>
          <button ref={moreRef} type="button" className={treeStyles.iconBtn} aria-label={t("pageTreeItem:openActions", { title: page.title })}
            onClick={(event) => { event.stopPropagation(); setMenu(treeMenuPoint(event)); }}>
            <DotsHorizontal size="small" />
          </button>
        </span>
      </div>
      {expanded ? views.map((view) => (
        <div key={view.id} className={treeStyles.treeRow} role="treeitem" aria-level={2} data-favorite-people-view={view.id}
          style={{ paddingInlineStart: "calc(8px + 22px)" }} tabIndex={0}
          onClick={(event) => open(`${href}?v=${encodeURIComponent(view.id)}`, event.metaKey || event.ctrlKey)}
          onKeyDown={(event) => onTreeRowKeyDown(event, { open: (newTab) => open(`${href}?v=${encodeURIComponent(view.id)}`, newTab) })}>
          <span className={treeStyles.treeLeading} aria-hidden="true">•</span>
          <span className={treeStyles.treeTitle}>{viewDisplayName(view)}</span>
        </div>
      )) : null}
      {menu ? (
        <FavoriteDatabaseMenu anchor={menu} title={page.title} editable={false} archived={false} favorite fullPage
          edited={{ name: editorName(page.lastEditedBy, session.user.id), editorId: page.lastEditedBy || null, at: database?.updatedAt || page.updatedAt }}
          onClose={() => setMenu(null)} onUnfavorite={() => void unfavorite()}
          onCopyLink={() => {
            setMenu(null);
            void copyText(serverUrl(link)).then((ok) => toast?.notify(ok ? t("rowMenu:toast.copiedLink") : t("rowMenu:toast.couldntCopyLink"), ok ? "success" : "error"));
          }}
          onRename={() => setMenu(null)} onMove={() => setMenu(null)} onArchive={() => setMenu(null)}
          onOpenInNewTab={() => { setMenu(null); open(href, true); }}
          onOpenInSidePeek={() => { setMenu(null); pushRoute(peoplePeekHref(page.id)); }} />
      ) : null}
    </>
  );
}
