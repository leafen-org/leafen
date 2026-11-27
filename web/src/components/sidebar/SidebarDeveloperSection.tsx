import { ConnectionsIcon, McpTokenIcon, PersonKeyIcon, SidebarChevronDown } from "@/icons/leafen";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DeveloperToolsTab } from "@/lib/router";
import { styles } from "./sidebarStyles";

/**
 * The sidebar's "개발자" section, which Notion's "섹션 추가 › 고급" puts on it
 * (seen 2026-10-30): links to its developer tools' connections, personal
 * access tokens and MCP tokens, each opening that tab of "개발자 도구"
 * (/developers). Notion's "Worker" link has no row: its Workers are built
 * and deployed with the Notion CLI, and Leafen's server workers
 * come inside the apps a server installs.
 */
export function SidebarDeveloperSection({ menu, onOpen }: {
  /** The section's "…" menu. */
  menu: ReactNode;
  onOpen: (tab: DeveloperToolsTab) => void;
}) {
  const { t } = useTranslation("sidebar");
  const [collapsed, setCollapsed] = useState(false);
  const rows = [
    { key: "connections", label: t("developerConnections"), icon: <ConnectionsIcon size="wide" aria-hidden="true" />,
      open: () => onOpen("connections") },
    { key: "tokens", label: t("developerTokens"), icon: <PersonKeyIcon size="regular" aria-hidden="true" />,
      open: () => onOpen("tokens") },
    { key: "mcp-tokens", label: t("developerMcpTokens"), icon: <McpTokenIcon size="regular" aria-hidden="true" />,
      open: () => onOpen("mcp-tokens") },
  ];
  return (
    <section className={styles.section} data-sidebar-developer-section="true" data-section-folded={collapsed || undefined}>
      <div className={styles.sectionLabel}>
        <button type="button" className={`${styles.sectionToggle} ${styles.privateSectionToggle}`}
          aria-expanded={!collapsed} aria-controls="sidebar-developer-section" onClick={() => setCollapsed((value) => !value)}>
          <span className={styles.sectionTitle}>{t("developer")}</span>
          <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
        </button>
        <div className={styles.sectionActions} data-section-actions="developer">{menu}</div>
      </div>
      {collapsed ? null : (
        <div id="sidebar-developer-section" className={styles.shortcutList}>
          {rows.map((row) => (
            <div key={row.key} className={styles.shortcutItem}>
              <button type="button" className={`${styles.shortcutRow} ${styles.developerRow}`} data-sidebar-developer-link={row.key} onClick={row.open}>
                <span className={styles.viewRowIcon}>{row.icon}</span>
                <span className={styles.viewRowTitle}>{row.label}</span>
              </button>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
