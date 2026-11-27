import { useTranslation } from "react-i18next";
import {
  Bell, ChartIcon, ConnectionsIcon, DeveloperIcon, EnvelopeIcon, GlobeIcon, IdentityIcon, ImportIcon, LayoutIcon, MailIcon, McpTokenIcon, SecurityKeyIcon, ServerAuditIcon, ServerDataIcon, ServerMapIcon, ServerRestoreIcon, ServerStandbyIcon, ServerProvidersIcon, ServerUpdatesIcon, ServerUsersIcon, ServerAppsIcon, ServerWorkspacesIcon, ServerAccountsIcon, OrganizationIcon, Settings as Gear, GroupPeopleIcon, SlidersIcon, SmileIcon, TeamspaceIcon, Upload, UserGearIcon, VerifiedIcon,
  Search,
} from "@/icons/leafen";
import type { SettingsGroupId, SettingsSection } from "./settingsWindowModel";
import styles from "./SettingsWindow.module.css";

const GROUP_KEYS: Record<SettingsGroupId, string> = {
  account: "navAccountGroup",
  workspace: "navWorkspaceGroup",
  features: "navFeaturesGroup",
  admin: "navAdminGroup",
  server: "navServerGroup",
};

/**
 * The names of the settings window, its groups and its sections. A touch
 * screen wider than a phone names 사람 "멤버", as Notion's iPad list does
 * (seen 2026-11-16 in iPad Safari at 834, twice; its computer's says 사람).
 */
export function useSettingsLabels(touchList = false) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { t: tc } = useTranslation("serverConsole");
  return {
    title: t("settingsWindowTitle"),
    close: t("common:actions.close"),
    searchPlaceholder: t("settingsSearchPlaceholder"),
    /** What a search that finds nothing says, naming the words as typed (Notion's "spaceSettings.search.noResults"). */
    noResults: (query: string) => t("settingsSearchEmpty", { query }),
    newVersion: tc("nav.newVersion"),
    group: (id: SettingsGroupId) => t(GROUP_KEYS[id]),
    /** The line under a section's title, as Notion's (seen 2026-10-11); its own sections have none. */
    subtitle(section: SettingsSection): string | undefined {
      switch (section) {
        case "account": return t("accountSubtitle");
        case "preferences": return t("preferencesSubtitle");
        case "notifications": return t("notificationsSubtitle");
        case "general": return t("generalSubtitle");
        case "people": return t("peopleSubtitle");
        case "sidebar": return t("sidebarSubtitle");
        case "analytics": return t("analyticsSubtitle");
        case "import": return t("importDialog:browse.subtitle");
        case "connections": return t("agents.meta");
        case "integrations": return t("connectionsSubtitle");
        case "verified-pages": return t("verifiedPagesSubtitle");
        case "public-pages": return t("publicPagesSubtitle");
        case "emoji": return t("emojiSubtitle");
        case "developer": return t("developerSubtitle");
        case "teamspaces": return t("teamspacesSubtitle");
        case "sharing-security": return t("securitySubtitle");
        case "identity": return t("identitySubtitle");
        case "enterprise": return t("enterpriseSubtitle");
        case "organization": return t("organizationSubtitle");
        case "requests": return t("requestsSubtitle");
        case "usage": return t("usageLead");
        case "servers": return t("serverAccountsDescription");
        case "server-users": return tc("users.meta");
        case "server-workspaces": return t("serverWorkspacesSubtitle");
        case "server-email": return tc("email.meta");
        case "server-providers": return tc("providers.meta");
        case "server-general": return tc("general.meta");
        case "server-tunnel": return tc("tunnel.meta");
        case "server-map": return tc("map.meta");
        case "server-updates": return tc("updates.meta");
        case "server-data": return tc("schedule.meta");
        case "server-standby": return tc("standby.meta");
        case "server-restore": return tc("restore.pageMeta");
        default: return undefined;
      }
    },
    section(section: SettingsSection): string {
      switch (section) {
        // The account's section is titled as Notion's, "계정"; its item
        // shows the person's name.
        case "account": return t("navAccountGroup");
        case "preferences": return t("preferencesTitle");
        case "notifications": return t("navNotifications");
        case "servers": return t("navServerAccounts");
        case "general": return t("navGeneral");
        case "people": return t(touchList ? "peopleTabMembers" : "navPeople");
        case "sidebar": return t("navSidebar");
        case "import": return t("navImports");
        case "analytics": return t("workspaceAnalytics:title");
        case "usage": return t("navUsage");
        case "integrations": return t("navIntegrations");
        case "verified-pages": return t("navVerifiedPages");
        case "public-pages": return t("navPublicPages");
        case "emoji": return t("navEmoji");
        case "connections": return t("navAiConnections");
        case "developer": return t("navDeveloper");
        case "teamspaces": return t("navTeamspaces");
        case "sharing-security": return t("navSecurityGroup");
        case "identity": return t("navIdentity");
        case "enterprise": return t("navEnterprise");
        case "organization": return t("navPoliciesDomains");
        case "requests": return t("navRequests");
        case "server-general": return tc("nav.general");
        case "server-users": return tc("nav.users");
        case "server-workspaces": return t("navServerWorkspaces");
        case "server-apps": return tc("nav.apps");
        case "server-audit": return t("navAuditLog");
        case "server-email": return tc("nav.email");
        case "server-providers": return tc("nav.providers");
        case "server-tunnel": return tc("nav.tunnel");
        case "server-map": return tc("nav.map");
        case "server-updates": return tc("nav.updates");
        case "server-data": return tc("nav.data");
        case "server-standby": return tc("nav.standby");
        case "server-restore": return tc("nav.restore");
      }
    },
  };
}

/**
 * "검색 설정"'s magnifier, Notion's magnifyingGlassSmall trimmed to its ink
 * (12.45×16) and centred in a 24px box, 18 high on a wider touch screen
 * (seen 2026-11-17 at 1440 twice and in iPad Safari at 834 twice, and
 * 2026-11-21 at 1440).
 */
export function SettingsSearchMark({ wide }: { wide: boolean }) {
  return (
    <span className={styles.searchMark}>
      <Search size={wide ? "page" : "small"} trim aria-hidden="true" />
    </span>
  );
}

export function SettingsSectionIcon({ section, size = "regular" }: { section: SettingsSection; size?: "regular" | "wide" }) {
  const props = { size, "aria-hidden": true } as const;
  switch (section) {
    case "preferences": return <SlidersIcon {...props} />;
    case "notifications": return <Bell {...props} />;
    case "general": return <Gear {...props} />;
    case "people": return <GroupPeopleIcon {...props} />;
    case "sidebar": return <LayoutIcon {...props} />;
    case "teamspaces": return <TeamspaceIcon {...props} />;
    case "import": return <ImportIcon {...props} />;
    case "usage": return <Upload {...props} />;
    case "analytics": return <ChartIcon {...props} />;
    case "developer": return <DeveloperIcon {...props} />;
    case "integrations": return <ConnectionsIcon {...props} />;
    // The agents' section takes the MCP connection mark the developer tools' MCP keys show, not the globe of public pages.
    case "connections": return <McpTokenIcon {...props} />;
    case "identity": return <IdentityIcon {...props} />;
    case "verified-pages": return <VerifiedIcon {...props} />;
    case "emoji": return <SmileIcon {...props} />;
    case "sharing-security":
    case "enterprise": return <SecurityKeyIcon {...props} />;
    // Notion's 요청 takes the inbox's tray (seen 2026-11-09).
    case "requests": return <MailIcon {...props} />;
    case "server-users": return <ServerUsersIcon {...props} />;
    case "server-workspaces": return <ServerWorkspacesIcon {...props} />;
    case "server-apps": return <ServerAppsIcon {...props} />;
    case "server-audit": return <ServerAuditIcon {...props} />;
    case "server-email": return <EnvelopeIcon {...props} />;
    case "server-providers": return <ServerProvidersIcon {...props} />;
    case "server-tunnel": return <GlobeIcon {...props} />;
    case "server-map": return <ServerMapIcon {...props} />;
    case "server-updates": return <ServerUpdatesIcon {...props} />;
    case "server-data": return <ServerDataIcon {...props} />;
    case "server-general": return <Gear {...props} />;
    case "server-standby": return <ServerStandbyIcon {...props} />;
    case "server-restore": return <ServerRestoreIcon {...props} />;
    case "servers": return <ServerAccountsIcon {...props} />;
    case "organization": return <OrganizationIcon {...props} />;
    // The account's item shows the person's picture; this is its mark elsewhere.
    case "account": return <UserGearIcon {...props} />;
    case "public-pages": return <GlobeIcon {...props} />;
  }
  // Every section names its own mark: one left to a shared fallback read as
  // another section (2026-11-06: 서버 계정 and 조직 took 공개 페이지's globe).
  const unmarked: never = section;
  return unmarked;
}
