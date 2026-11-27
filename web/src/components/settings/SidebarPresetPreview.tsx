import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { usePageVisits } from "@/components/library/useLibraryData";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import { rootTreePages, sharedSectionPages } from "@/components/sidebar/pageTreeModel";
import { SidebarShortcutIcon, sidebarShortcutLabel } from "@/components/sidebar/SidebarShortcuts";
import { sidebarSectionKey, sortSidebarPages } from "@/components/sidebar/sidebarLayoutModel";
import { useSidebarTeamspaces } from "@/components/sidebar/useSidebarTeamspaces";
import { ConnectionsIcon, DotsHorizontal, McpTokenIcon, PersonKeyIcon, TeamspaceIcon } from "@/icons/leafen";
import { usePageNames } from "@/lib/pageNames";
import type { Page, Session } from "@/types";
import type { SidebarLayoutSection, SidebarLayoutShortcut } from "@/workspaceMembershipWire.generated";
import styles from "./SidebarPresetPreview.module.css";
import { outOfTrashIn, pageMapOf } from "@/lib/pageIndex";

type Row = { key: string; icon: ReactNode; label: string };

/** The preset as Notion's preview heads it, as its tab reads (seen 2026-11-18): its icon and name in a shaded chip. */
export function SidebarPresetTab({ icon, name }: { icon: ReactNode; name: string }) {
  return (
    <span className={styles.tabLine}>
      <span className={styles.tab} data-sidebar-preset-preview-tab><span className={styles.tabIcon}>{icon}</span><span>{name}</span></span>
    </span>
  );
}

/**
 * "미리보기", as Notion's (seen 2026-11-09 and 2026-11-18): under the
 * editor's head the sidebar the preset makes — its shortcuts, then each
 * section with the reader's own pages in it, up to its count with "모두
 * 보기" past it, and one with nothing in it left out, as Notion's. Nothing in
 * it opens; the favorites and a database view show their heading alone,
 * their rows being the sidebar's to read.
 */
export function SidebarPresetPreview({ session, workspaceId, sections, shortcuts }: {
  session: Session;
  workspaceId: string;
  sections: readonly SidebarLayoutSection[];
  shortcuts: readonly SidebarLayoutShortcut[];
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("sidebar");
  const product = useProductPages();
  const pages = product?.pages ?? [];
  const shared = new Set(product?.sharedPageIds ?? []);
  const byId = pageMapOf(pages), outOfTrash = outOfTrashIn(pages);
  const visits = usePageVisits(session, workspaceId);
  const workspace = product?.workspaces?.find((candidate) => candidate.id === workspaceId) ?? null;
  const { joinedTeamspaces } = useSidebarTeamspaces({ session, workspace });
  const teamspaceOf = (section: SidebarLayoutSection) => joinedTeamspaces.find((teamspace) => teamspace.id === section.teamspaceId);
  const pageRow = (page: Page): Row => ({ key: page.id, icon: <PageIconGlyph page={page} size={18} />, label: pageNames.title(page) });
  const roots = rootTreePages(pages);

  // A section's rows, or none known (the favorites', a database view's); one with none is left out.
  const rowsOf = (section: SidebarLayoutSection): Row[] | null => {
    switch (section.id) {
      case "private":
        return sortSidebarPages(roots.filter((page) => !page.teamspaceId && !shared.has(page.id)), section.sort).map(pageRow);
      case "shared":
        return sortSidebarPages(sharedSectionPages(pages, shared, product?.workspaceWidePageIds), section.sort).map(pageRow);
      case "recents":
        return visits.flatMap((visit) => { const page = byId.get(visit.pageId); return page && outOfTrash(page) ? [pageRow(page)] : []; });
      case "teamspaces":
        return joinedTeamspaces.map((teamspace) => ({ key: teamspace.id, icon: <TeamspaceIcon size="regular" aria-hidden="true" />, label: teamspace.name }));
      case "teamspace":
        // One teamspace's own section, its pages, while the reader is in it, as the sidebar shows it.
        return teamspaceOf(section)
          ? sortSidebarPages(roots.filter((page) => page.teamspaceId === section.teamspaceId), section.sort).map(pageRow) : [];
      case "developer":
        return [
          { key: "connections", icon: <ConnectionsIcon size="wide" aria-hidden="true" />, label: t("developerConnections") },
          { key: "tokens", icon: <PersonKeyIcon size="regular" aria-hidden="true" />, label: t("developerTokens") },
          { key: "mcp-tokens", icon: <McpTokenIcon size="regular" aria-hidden="true" />, label: t("developerMcpTokens") },
        ];
      default:
        return null;
    }
  };
  // A section is headed as the sidebar heads it: a database view by its name, one teamspace's by the teamspace's.
  const heading = (section: SidebarLayoutSection) => (section.id === "view" ? section.view?.name.trim() || t("databaseView")
    : section.id === "teamspace" ? teamspaceOf(section)?.name ?? ""
      : t(section.id === "private" ? "privatePages" : section.id));

  return (
    <div className={styles.preview} data-sidebar-preset-preview>
      {shortcuts.length ? (
        <div className={styles.group}>
          {shortcuts.map((shortcut) => {
            const page = shortcut.pageId ? byId.get(shortcut.pageId) : undefined;
            return (
              <div key={shortcut.key} className={styles.row}>
                <span className={styles.icon}><SidebarShortcutIcon shortcut={shortcut} page={page} /></span>
                <span>{sidebarShortcutLabel(shortcut, page, (key, options) => t(key, options))}</span>
              </div>
            );
          })}
        </div>
      ) : null}
      <div className={styles.sections}>
        {sections.filter((section) => !section.hidden).map((section) => {
          const known = rowsOf(section), rows = known ?? [];
          if (known && known.length === 0) return null;
          return (
            <div key={sidebarSectionKey(section)} className={styles.section}>
              <div className={styles.heading}>{heading(section)}</div>
              {rows.slice(0, section.limit).map((row) => (
                <div key={row.key} className={styles.row}>
                  <span className={styles.icon}>{row.icon}</span>
                  <span>{row.label}</span>
                </div>
              ))}
              {rows.length > section.limit ? (
                <div className={`${styles.row} ${styles.more}`}>
                  <span className={styles.icon}><DotsHorizontal size="regular" aria-hidden="true" /></span>
                  <span>{t("showAll")}</span>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
