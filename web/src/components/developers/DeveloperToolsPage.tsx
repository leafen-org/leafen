import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TopBar } from "@/components/page/TopBar";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { useProductPages } from "@/components/product/productPagesContext";
import { DeveloperIcon, ConnectionsIcon, McpTokenIcon, PersonKeyIcon, Plus } from "@/icons/leafen";
import { setDocumentChrome } from "@/lib/documentChrome";
import { DEVELOPER_TOOLS_TABS, developerToolsHref, pushRoute, replaceRoute, useSearchParams, type DeveloperToolsTab } from "@/lib/router";
import { DeveloperConnectionPage } from "./DeveloperConnectionPage";
import { DeveloperMcpTokenPage } from "./DeveloperMcpTokenPage";
import { DeveloperConnectionsTab, DeveloperMcpTokensTab, DeveloperTokensTab } from "./DeveloperToolsTables";
import styles from "./DeveloperTools.module.css";

const TAB_ICONS = {
  connections: ConnectionsIcon,
  tokens: PersonKeyIcon,
  "mcp-tokens": McpTokenIcon,
} as const;

/**
 * "개발자 도구" in the app, as Notion's /developers (seen 2026-10-30): its
 * tabs 시작하기, 연결, 개인 액세스 토큰 and MCP 토큰, each at its own address,
 * the sidebar kept. Leafen's connections are the apps a person registers,
 * its MCP tokens the agents' connections. Notion's Worker tab, the CLI steps
 * and the platform's documentation cards have nothing to open here: Workers
 * are built with Notion's CLI and Leafen keeps no help site
 *.
 */
/**
 * Two pieces fitting together over "연결 만들기", where Notion draws its own
 * picture (seen 2026-10-30) — Leafen's own drawing, as its icons are
 * (decided 2026-09-25: no Notion art is copied).
 */
function ConnectionsArt() {
  return (
    <svg className={styles.art} width="96" height="72" viewBox="0 0 96 72" aria-hidden="true">
      <path d="M8 14h20a7 7 0 1 1 14 0h6v18a7 7 0 1 1 0 14v18H8z" fill="#ffb110" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" />
      <path d="M48 14h40v50H48V46a7 7 0 1 0 0-14z" fill="var(--bg-default, #fff)" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" />
      <path d="M58 26l20 0M58 36l14 0M58 46l18 0" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

export default function DeveloperToolsPage({ tab, connectionId }: { tab: DeveloperToolsTab; connectionId?: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const pages = useProductPages();
  const session = pages?.session;
  const workspace = useMemo(() => pages?.workspaces?.find((candidate) => candidate.id === pages.workspaceId),
    [pages?.workspaces, pages?.workspaceId]);
  const label = (id: DeveloperToolsTab) => t(`developerTools.tabs.${id}`);
  useEffect(() => {
    setDocumentChrome({ title: tab === "start" ? t("developerTools.title") : label(tab) });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, t]);
  // "신규 연결" opens its window over the connections, from any tab.
  const creating = useSearchParams().get("new") === "connection";
  const setCreating = (open: boolean) => (open ? pushRoute : replaceRoute)(
    `${developerToolsHref("connections")}${open ? "?new=connection" : ""}`);
  const TabIcon = tab === "start" ? null : TAB_ICONS[tab];
  // A connection opens on its own page under "연결", a token under "MCP 토큰", as Notion's.
  if (connectionId && session) return tab === "mcp-tokens"
    ? <DeveloperMcpTokenPage key={connectionId} session={session} connectionId={connectionId} />
    : <DeveloperConnectionPage session={session} connectionId={connectionId} />;
  const crumbs = (
    <>
      <span className={topBarStyles.crumbWrap}>
        <button type="button" className={topBarStyles.crumb} data-developer-tools-crumb onClick={() => pushRoute(developerToolsHref("start"))}>
          <span className={topBarStyles.crumbIcon}><DeveloperIcon size="small" aria-hidden="true" /></span>
          <span className={topBarStyles.crumbLabel}>{t("developerTools.title")}</span>
        </button>
      </span>
      {TabIcon ? <>
        <span className={styles.crumbSep} aria-hidden="true">/</span>
        <span className={topBarStyles.crumbWrap}>
          <span className={topBarStyles.crumb} data-topbar-current-page>
            <span className={topBarStyles.crumbIcon}><TabIcon size="small" aria-hidden="true" /></span>
            <span className={topBarStyles.crumbLabel}>{label(tab)}</span>
          </span>
        </span>
      </> : null}
    </>
  );
  return (
    <>
      <TopBar title={t("developerTools.title")} crumbs={crumbs} />
      <main className={styles.page} data-developer-tools={tab}>
        <h1 className={styles.title}>{t("developerTools.title")}</h1>
        <nav className={styles.tabs} role="tablist" aria-label={t("developerTools.title")}>
          {DEVELOPER_TOOLS_TABS.map((candidate) => (
            <button key={candidate.id} type="button" role="tab" aria-selected={candidate.id === tab} className={styles.tab}
              data-developer-tools-tab={candidate.id} onClick={() => pushRoute(developerToolsHref(candidate.id))}>
              {label(candidate.id)}
            </button>
          ))}
        </nav>
        {!session || !workspace ? null : tab === "start" ? (
          <section className={styles.start} role="tabpanel">
            <p className={styles.intro}>{t("developerTools.intro")}</p>
            <ConnectionsArt />
            <h2 className={styles.heading}>{t("developerTools.connectionsTitle")}</h2>
            <p className={styles.description}>{t("developerTools.connectionsDescription")}</p>
            <button type="button" className={styles.primary} data-developer-new-connection
              onClick={() => setCreating(true)}>
              <Plus size="small" aria-hidden="true" />{t("developerTools.newConnection")}
            </button>
          </section>
        ) : tab === "connections" ? (
          <DeveloperConnectionsTab session={session} workspace={workspace} creating={creating} onCreatingChange={setCreating} />
        ) : tab === "tokens" ? (
          <DeveloperTokensTab session={session} workspace={workspace} />
        ) : (
          <DeveloperMcpTokensTab session={session} workspace={workspace} />
        )}
      </main>
    </>
  );
}
