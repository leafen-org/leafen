import { DotsHorizontal, SidebarChevronDown } from "@/icons/leafen";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { SidebarLayoutSection } from "@/workspaceMembershipWire.generated";
import { PageTreeItem } from "./PageTreeItem";
import { usePageTreeContext } from "./pageTreeContext";
import { styles } from "./sidebarStyles";
import { pageTrashRoot } from "@/lib/pageTrash";
import { libraryHref, pushRoute } from "@/lib/router";
import { useKeptTeamspaces } from "@/lib/workspaceTeamspaces";
import { teamspaceIdAt } from "@/components/page/TopBarTeamspaceCrumb";
import { useSidebarRecentsRead } from "./useSidebarRecentsRead";

/**
 * The sidebar's "최근 항목" section, which Notion's "섹션 추가" puts on it
 * (seen 2026-10-01): the pages the reader opened last, newest first, as many
 * as the section shows, with "··· 모두 보기" past that, which opens the
 * library's recent pages as Notion's does (seen 2026-11-11). Its rows are
 * copies of the pages' rows elsewhere; a page of an archived teamspace the
 * reader still opens is listed with the archive mark, as Notion's (seen
 * 2026-11-14), though no other section lists it. It is read as the sidebar
 * comes up and each time Home shows, not as pages open (useSidebarRecentsRead).
 */
export function SidebarRecentsSection({
  workspaceId,
  section,
  menu,
}: {
  workspaceId: string;
  section: SidebarLayoutSection;
  /** The section's "…" menu. */
  menu: ReactNode;
}) {
  const { t } = useTranslation("sidebar");
  const { pagesById, selectedPageId, session } = usePageTreeContext();
  const { visitsRead, order } = useSidebarRecentsRead(session, workspaceId, selectedPageId);
  const [collapsed, setCollapsed] = useState(false);
  const pages = order.flatMap((pageId) => {
    const page = pagesById[pageId];
    return page && !pageTrashRoot(page, (id) => pagesById[id]) ? [page] : [];
  });
  const shown = pages.slice(0, section.limit);
  const archivedPlaces = useKeptTeamspaces(session.user.id, workspaceId)?.archived;
  const archived = new Set((archivedPlaces ?? []).map((place) => place.id));
  return (
    <section className={styles.section} data-sidebar-recents-section="true" data-section-folded={collapsed || undefined}>
      <div className={styles.sectionLabel}>
        <button type="button" className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
          aria-expanded={!collapsed} aria-controls="sidebar-recents-section" onClick={() => setCollapsed((value) => !value)}>
          <span className={styles.sectionTitle}>{t("recents")}</span>
          <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
        </button>
        <div className={styles.sectionActions} data-section-actions="recents">{menu}</div>
      </div>
      {collapsed ? null : (
        <div id="sidebar-recents-section" className={styles.tree} role="tree" aria-label={t("recentPages")}>
          {shown.map((page, index) => (
            <PageTreeItem key={page.id} pageId={page.id} depth={0} index={index} setSize={shown.length} listCopy="recent"
              inArchivedTeamspace={archived.size > 0 && archived.has(teamspaceIdAt(page, (id) => pagesById[id]))} />
          ))}
          {/* Visits not read (before the server answers, or without it) are no empty list. */}
          {pages.length === 0 && visitsRead.state === "ready" ? <p className={styles.sectionEmpty}>{t("noRecents")}</p> : null}
          {pages.length > section.limit ? (
            <button type="button" className={styles.showAll} data-sidebar-show-all="recents" onClick={() => pushRoute(libraryHref("recents"))}>
              <DotsHorizontal size="regular" aria-hidden="true" />
              <span>{t("showAll")}</span>
            </button>
          ) : null}
        </div>
      )}
    </section>
  );
}
