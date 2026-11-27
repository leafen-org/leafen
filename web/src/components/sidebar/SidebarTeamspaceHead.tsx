import { Plus, SidebarChevronRight } from "@/icons/leafen";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { usePageTreeRowContext } from "./pageTreeContext";
import { teamspaceTopDropPlan } from "./pageTreeModel";
import { SidebarTeamspaceMenu } from "./SidebarTeamspaceMenu";
import { useMovePageToTop } from "./sidebarSectionOrder";
import { usePageSectionDrop } from "./usePageTreeRowDrag";
import type { SidebarTeamspaceAction } from "./useSidebarTeamspaceActions";
import { styles } from "./sidebarStyles";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A teamspace's name row in the sidebar: it folds the teamspace's pages, adds
 * one at its top for a reader who may (TeamspaceHeadAdd), opens its options, and takes a page dragged onto it to the
 * top of the teamspace, as Notion's (seen 2026-11-13: a 4px line under the
 * name, where the first page's line falls).
 */
export function SidebarTeamspaceHead({
  teamspace,
  collapsed,
  treeId,
  canAdd,
  creatingPage,
  canCreateTeamspace,
  onToggle,
  onCreateTeamspacePage,
  onOpenTeamspaceManage,
  onTeamspaceAction,
}: {
  teamspace: DialogTeamspace;
  collapsed: boolean;
  treeId: string;
  /** Whether the reader may add pages at the teamspace's top. */
  canAdd: boolean;
  creatingPage: boolean;
  canCreateTeamspace: boolean;
  onToggle: () => void;
  onCreateTeamspacePage: (teamspaceId: string) => void;
  onOpenTeamspaceManage: (teamspaceId: string, invite: boolean) => void;
  onTeamspaceAction: (teamspace: DialogTeamspace, action: SidebarTeamspaceAction) => void;
}) {
  const { t } = useTranslation(["sidebar", "pageTreeItem"]);
  const { store, pageAccess } = usePageTreeRowContext();
  const moveToTop = useMovePageToTop();
  const [head, setHead] = useState<HTMLElement | null>(null);
  const [dropOver, setDropOver] = useState(false);

  const planFor = (pageId: string) => teamspaceTopDropPlan({
    pages: store.get().pages, draggedPageId: pageId, teamspaceId: teamspace.id, canEditPage: pageAccess.canEdit,
  });
  usePageSectionDrop(head, {
    accepts: (pageId) => canAdd && planFor(pageId) !== null,
    drop: (pageId) => void moveToTop(pageId, planFor(pageId)),
  }, setDropOver);

  return (
    <div ref={setHead} className={styles.sectionLabel} data-sidebar-teamspace-head="true"
      data-teamspace-drop={dropOver || undefined}>
      {/* As Notion's (seen 2026-11-11): the row folds its pages, pressed or
          with Enter, and holds its own arrow, "열기" while folded and
          "닫기" while open, next in Tab's order, which folds them too. */}
      <div
        role="button"
        tabIndex={0}
        className={`${styles.sectionToggle} ${styles.teamspaceToggle}`}
        aria-label={teamspace.name}
        aria-expanded={!collapsed}
        aria-controls={treeId}
        onClick={onToggle}
        onKeyDown={(event) => {
          if (event.target !== event.currentTarget || !fieldEnter(event) && event.key !== " ") return;
          event.preventDefault();
          onToggle();
        }}
      >
        {/* As a page row's: the arrow takes the icon's place while the row is pointed at. */}
        <span className={styles.teamspaceLeading}>
          <span className={styles.teamspaceIcon} aria-hidden="true">
            {teamspaceGlyph(teamspace)}
          </span>
          <button
            type="button"
            className={styles.teamspaceDisclosure}
            aria-label={t(collapsed ? "pageTreeItem:showInside" : "pageTreeItem:hideInside")}
            aria-describedby={`${treeId}-name`}
            aria-expanded={!collapsed}
            aria-controls={treeId}
            data-sidebar-teamspace-disclosure="true"
            onClick={(event) => { event.stopPropagation(); onToggle(); }}
          >
            <SidebarChevronRight
              className={`${styles.sectionChevron} ${styles.teamspaceChevron}`}
              size="micro"
              aria-hidden="true"
            />
          </button>
        </span>
        <span id={`${treeId}-name`} className={styles.teamspaceName}>{teamspace.name}</span>
      </div>
      <div
        className={styles.sectionActions}
        data-sidebar-teamspace-actions="true"
      >
        <TeamspaceHeadAdd teamspace={teamspace} canAdd={canAdd} creatingPage={creatingPage}
          onCreate={onCreateTeamspacePage} />
        <SidebarTeamspaceMenu teamspace={teamspace} canCreate={canCreateTeamspace}
          onManage={onOpenTeamspaceManage} onAct={onTeamspaceAction} />
      </div>
    </div>
  );
}

/**
 * A teamspace head's "+", a new page at its top — the teamspaces section's
 * and a teamspace's own section's alike. Only a reader who may add there has
 * it: Notion draws no "+" on the head of a teamspace whose owners keep its
 * sidebar to themselves for a member, its "…" staying at the row's end
 * (seen 2026-11-17 at 1416 and in iPhone Safari 402, each by its buttons'
 * names and on screen).
 */
export function TeamspaceHeadAdd({ teamspace, canAdd, creatingPage, onCreate }: {
  teamspace: Pick<DialogTeamspace, "id" | "name">;
  canAdd: boolean;
  creatingPage: boolean;
  onCreate: (teamspaceId: string) => void;
}) {
  const { t } = useTranslation("sidebar");
  if (!canAdd) return null;
  return (
    <button type="button" className={`${styles.sectionAction} ${styles.sectionAdd}`}
      aria-label={t("addPageToTeamspace", { name: teamspace.name })} disabled={creatingPage}
      onClick={() => onCreate(teamspace.id)}>
      <Plus size="small" aria-hidden="true" />
    </button>
  );
}
