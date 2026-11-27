import { DotsHorizontal as DotsThree, Plus, SidebarChevronDown } from "@/icons/leafen";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { canAddTeamspacePage, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import type { Page } from "@/types";
import { teamspaceSectionKey, type SidebarShowAllKey } from "./sidebarLayoutModel";
import { PageTreeItem } from "./PageTreeItem";
import { SidebarTeamspaceHead } from "./SidebarTeamspaceHead";
import { useTeamspaceOrderDrag } from "./useTeamspaceOrderDrag";
import type { SidebarTeamspaceAction } from "./useSidebarTeamspaceActions";
import { styles } from "./sidebarStyles";

/**
 * The sidebar's teamspaces: each the reader joined with its pages and
 * "+ 새로 추가", folding on its name, a new page and its options beside it,
 * pulled by its name to another place in the reader's order, then
 * "··· 모두 보기" to the library's teamspaces — there for more joined ones
 * than the section shows, and for any the reader may join, where they are
 * joined, as Notion's (seen 2026-11-12, one joined teamspace and three to join).
 */
export function SidebarTeamspacesSection({
  teamspaces,
  roots,
  menu,
  libraryButton,
  collapsed: sectionCollapsed,
  onToggle,
  canCreateTeamspace,
  creatingPage,
  browseCount,
  limit,
  listed,
  showAll,
  onCreateTeamspacePage,
  onAddRowPage,
  onOpenTeamspaceManage,
  onOpenTeamspacesLibrary,
  onTeamspaceAction,
  onReorder,
}: {
  teamspaces: DialogTeamspace[];
  /** The workspace's top-level pages, of which each teamspace shows its own. */
  roots: readonly Page[];
  menu: ReactNode;
  /** "라이브러리에서 열기", the library's teamspaces. */
  libraryButton: ReactNode;
  collapsed: boolean;
  onToggle: () => void;
  canCreateTeamspace: boolean;
  /** Whether pages may be added at a teamspace's top: not in a project's space. */
  creatingPage: boolean;
  /** How many the reader may see and join. */
  browseCount: number;
  limit: number;
  /** A teamspace's pages in the section's order and count. */
  listed: (list: readonly Page[]) => { pages: Page[]; more: boolean };
  showAll: (key: SidebarShowAllKey) => ReactNode;
  onCreateTeamspacePage: (teamspaceId: string) => void;
  /** "+ 새로 추가": the add window, on a phone the page at once. */
  onAddRowPage: (teamspaceId: string) => void;
  onOpenTeamspaceManage: (teamspaceId: string, invite: boolean) => void;
  onOpenTeamspacesLibrary: () => void;
  onTeamspaceAction: (teamspace: DialogTeamspace, action: SidebarTeamspaceAction) => void;
  /** The teamspaces it shows, in the order a pull left them. */
  onReorder: (order: string[]) => void;
}) {
  const { t } = useTranslation(["sidebar", "pageTreeItem"]);
  const [collapsedTeamspaceIds, setCollapsedTeamspaceIds] = useState(() => new Set<string>());
  const shown = teamspaces.slice(0, limit);
  const [list, setList] = useState<HTMLElement | null>(null);
  useTeamspaceOrderDrag(list, onReorder);

  function toggleTeamspace(teamspaceId: string) {
    setCollapsedTeamspaceIds((current) => {
      const next = new Set(current);
      if (next.has(teamspaceId)) next.delete(teamspaceId);
      else next.add(teamspaceId);
      return next;
    });
  }

  return (
    <section className={styles.section} data-sidebar-teamspaces-section="true" data-section-folded={sectionCollapsed || undefined}>
      {/* As Notion's: "팀스페이스 ⌄" folds the section; "↗" opens the library's teamspaces, "…" the section's menu with "새 팀스페이스". */}
      <div className={styles.sectionLabel}>
        <button type="button" className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
          aria-expanded={!sectionCollapsed} aria-controls="sidebar-teamspaces-section" onClick={onToggle}>
          <span className={styles.sectionTitle}>{t("teamspaces")}</span>
          <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
        </button>
        <div className={styles.sectionActions} data-section-actions="teamspaces">
          {libraryButton}
          {menu}
        </div>
      </div>
      {sectionCollapsed ? null : <div ref={setList} className={styles.teamspaceList} id="sidebar-teamspaces-section">
        {shown.map((teamspace, index) => {
          const teamspacePages = roots.filter((page) => page.teamspaceId === teamspace.id);
          const teamspaceRoots = listed(teamspacePages);
          const canAdd = canAddTeamspacePage(teamspace);
          const collapsed = collapsedTeamspaceIds.has(teamspace.id);
          const treeId = `sidebar-teamspace-${teamspace.id}`;
          return (
            <div
              key={teamspace.id}
              className={styles.teamspaceEntry}
              data-sidebar-teamspace={teamspace.id}
              data-sidebar-teamspace-gap={!collapsed && index < shown.length - 1 || undefined}
            >
              <SidebarTeamspaceHead teamspace={teamspace} collapsed={collapsed} treeId={treeId} canAdd={canAdd}
                creatingPage={creatingPage} canCreateTeamspace={canCreateTeamspace} onToggle={() => toggleTeamspace(teamspace.id)}
                onCreateTeamspacePage={onCreateTeamspacePage} onOpenTeamspaceManage={onOpenTeamspaceManage}
                onTeamspaceAction={onTeamspaceAction} />
              {!collapsed ? (
                <div
                  id={treeId}
                  className={styles.tree}
                  role="tree"
                  aria-label={t("teamspacePages", { name: teamspace.name })}
                >
                  {teamspaceRoots.pages.map((page, index) => (
                    <PageTreeItem
                      key={page.id}
                      pageId={page.id}
                      depth={0}
                      index={index}
                      setSize={teamspaceRoots.pages.length}
                    />
                  ))}
                  {teamspaceRoots.more ? showAll(teamspaceSectionKey(teamspace.id)) : null}
                </div>
              ) : null}
              {/* Notion's rule (its sidebar code, read 2026-10-31): a teamspace of two
                  pages or fewer ends with "+ 새로 추가"; one with none that the reader
                  may not add to says "하위 페이지 없음". */}
              {!collapsed && canAdd && teamspacePages.length <= 2 ? (
                <button type="button" className={`${styles.emptyTreeAction} ${styles.teamspaceAddRow}`}
                  data-sidebar-teamspace-add-row={teamspace.id} disabled={creatingPage}
                  onClick={() => onAddRowPage(teamspace.id)}>
                  <Plus size="small" aria-hidden="true" />
                  <span>{t("addNew")}</span>
                </button>
              ) : null}
              {!collapsed && !canAdd && teamspacePages.length === 0 ? (
                <div className={styles.teamspaceEmpty} data-sidebar-teamspace-empty={teamspace.id}>{t("noPagesInside")}</div>
              ) : null}
            </div>
          );
        })}
        {teamspaces.length > limit || browseCount > 0 ? (
          <button type="button" className={`${styles.showAll} ${styles.teamspaceBrowseButton}`}
            data-sidebar-show-all="teamspaces" onClick={onOpenTeamspacesLibrary}>
            <DotsThree size="regular" aria-hidden="true" />
            <span>{t("showAll")}</span>
          </button>
        ) : null}
      </div>}
    </section>
  );
}
