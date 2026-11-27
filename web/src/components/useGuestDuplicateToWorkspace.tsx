import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { WorkspaceIconGlyph, workspaceIconLetter } from "@/components/page/PageIcon";
import { Copy, Plus } from "@/icons/leafen";
import { requestWorkspaceCreate } from "@/lib/openWorkspace";
import type { Page, Session } from "@/types";
import { pageMenuItem, type PageMenuSubmenu } from "./PageMenu";
import { useProductPagesSelect } from "./product/productPagesStore";
import { useMoveCopy } from "./useMoveCopy";

/** One place a guest's copy can go, as the desktop menu and the phone sheet both list it. */
export type GuestCopyChoice = { key: string; label: string; icon: ReactNode; run(): void; data: Record<`data-${string}`, string> };

/** A guest's "페이지 복제 ›" with its places in their groups, for the phone sheet too. */
export type GuestDuplicateToWorkspace = PageMenuSubmenu & { groups: readonly (readonly GuestCopyChoice[])[] };

/**
 * A guest's "페이지 복제 ›", as Notion's (seen 2026-11-15 by a guest who may
 * edit, below "페이지 내용 복사하기"; one who may move the page has no such
 * row): beside it the guest's own workspaces, choosing one copying the page to
 * the top of its private pages, and "새 워크스페이스" at the foot. A guest can
 * neither move the page nor copy its contents, so this is how they keep a copy.
 * The phone's page sheet lists the same places in a half sheet.
 */
export function useGuestDuplicateToWorkspace(session: Session, page: Page, busy: boolean, onStarted: () => void): GuestDuplicateToWorkspace {
  const { t } = useTranslation(["rowMenu", "sidebar"]);
  const own = useProductPagesSelect((pages) => pages?.workspaces);
  const workspaces = (own ?? []).filter((workspace) => workspace.guest !== true && workspace.id !== page.workspaceId);
  const copy = useMoveCopy(session, page, undefined, onStarted);
  const item = pageMenuItem(busy);
  const groups: GuestCopyChoice[][] = [
    workspaces.map((workspace) => ({ key: `workspace-${workspace.id}`, label: workspace.name,
      icon: <WorkspaceIconGlyph icon={workspace.icon} size={20} fallback={workspaceIconLetter(workspace.name)} />,
      run: () => void copy(null, workspace, false), data: { "data-duplicate-to-workspace": workspace.id } })),
    [{ key: "new-workspace", label: t("sidebar:newWorkspace"), icon: <Plus size="regular" aria-hidden="true" />,
      run: () => { onStarted(); requestWorkspaceCreate(); }, data: { "data-duplicate-to-new-workspace": "" } }],
  ];
  return {
    submenu: "duplicate-to-workspace", label: t("rowMenu:actions.duplicateToWorkspace"), icon: <Copy size="regular" aria-hidden="true" />,
    width: 260, data: { "data-page-duplicate-to-workspace": "" }, groups,
    sections: (show) => groups.map((choices) => ({
      rows: choices.map((choice) => item(show, choice.key, choice.label, choice.icon, choice.run, { data: choice.data })),
    })),
  };
}
