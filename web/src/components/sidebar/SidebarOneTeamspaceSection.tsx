import { Plus, SidebarChevronDown } from "@/icons/leafen";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { canAddTeamspacePage, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import type { Page } from "@/types";
import { teamspaceSectionKey, type SidebarShowAllKey } from "./sidebarLayoutModel";
import { PageTreeItem } from "./PageTreeItem";
import { TeamspaceHeadAdd } from "./SidebarTeamspaceHead";
import { styles } from "./sidebarStyles";

/**
 * A teamspace as a section of its own ("섹션 추가 › 팀스페이스 ›", Notion's,
 * seen 2026-11-11): its name heads the section as the others' do, folding it,
 * with "+" for a new page in it where the reader may add one and its "…"; its pages are the section's
 * top rows. As a teamspace in the teamspaces section, one of two pages or
 * fewer ends with "+ 새로 추가", and one with none the reader may not add to
 * says "하위 페이지 없음".
 */
export function SidebarOneTeamspaceSection({
  sectionKey,
  teamspace,
  pages,
  more,
  showAll,
  menu,
  collapsed,
  onToggle,
  creatingPage,
  onCreatePage,
  onAddRowPage,
}: {
  sectionKey: string;
  teamspace: DialogTeamspace;
  /** All its top pages, of which the section lists `listed` up to its count. */
  pages: { all: readonly Page[]; listed: readonly Page[] };
  more: boolean;
  showAll: (key: SidebarShowAllKey) => ReactNode;
  menu: ReactNode;
  collapsed: boolean;
  onToggle: () => void;
  creatingPage: boolean;
  /** Its "+": a page made at once. */
  onCreatePage: (teamspaceId: string) => void;
  /** "+ 새로 추가": the add window, on a phone the page at once. */
  onAddRowPage: (teamspaceId: string) => void;
}) {
  const { t } = useTranslation("sidebar");
  const canAdd = canAddTeamspacePage(teamspace);
  const treeId = `sidebar-teamspace-section-${teamspace.id}`;
  return (
    <section className={styles.section} data-sidebar-teamspace-section={teamspace.id} data-section-folded={collapsed || undefined}>
      <div className={styles.sectionLabel}>
        <button type="button" className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
          aria-expanded={!collapsed} aria-controls={treeId} onClick={onToggle}>
          <span className={styles.sectionTitle}>{teamspace.name}</span>
          <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
        </button>
        <div className={styles.sectionActions} data-section-actions={sectionKey}>
          <TeamspaceHeadAdd teamspace={teamspace} canAdd={canAdd} creatingPage={creatingPage} onCreate={onCreatePage} />
          {menu}
        </div>
      </div>
      {collapsed ? null : <>
        <div id={treeId} className={styles.tree} role="tree" aria-label={t("teamspacePages", { name: teamspace.name })}>
          {pages.listed.map((page, index) => (
            <PageTreeItem key={page.id} pageId={page.id} depth={0} index={index} setSize={pages.listed.length} />
          ))}
          {more ? showAll(teamspaceSectionKey(teamspace.id)) : null}
        </div>
        {canAdd && pages.all.length <= 2 ? (
          <button type="button" className={styles.emptyTreeAction} data-sidebar-teamspace-add-row={teamspace.id}
            disabled={creatingPage} onClick={() => onAddRowPage(teamspace.id)}>
            <Plus size="small" aria-hidden="true" />
            <span>{t("addNew")}</span>
          </button>
        ) : null}
        {!canAdd && pages.all.length === 0 ? (
          <div className={styles.teamspaceEmpty} data-sidebar-teamspace-empty={teamspace.id}>{t("noPagesInside")}</div>
        ) : null}
      </>}
    </section>
  );
}
