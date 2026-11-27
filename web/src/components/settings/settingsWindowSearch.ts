import { desktopBackgroundAvailable } from "@/lib/desktopBackground";
import { hasDesktopMenuBar, packagedWindows } from "@/lib/desktopMenuCommands";
import { pushSettingKeys } from "@/lib/desktopPush";
import { organizationModule } from "@/lib/organizationModule";
import { desktopOwnServer } from "@/components/account/AccountOpenInBrowser";
import { desktopUpdateSwitchShown } from "@/lib/desktopUpdater";
import { BACKUP_PLACE_FIELDS, BACKUP_PLACE_PATH_STYLE } from "@/lib/backupPlaceFields";
import type { SettingsGroup, SettingsSection } from "./settingsWindowModel";

// The settings search finds the rows of the settings, by their names and
// descriptions, as Notion's search does (seen 2026-10-02, again 2026-11-06:
// "숫자 형식" finds its row, "HTML" the 가져오기 rows that name it): it lists
// them under their group and section, and a found row opens its section with
// the row in sight (settingsFoundRow). A section's own name finds all its
// rows. Keys are in workspaceSettingsDialog unless they name another
// namespace. Every row a section draws belongs here, also those a switch or
// a choice draws: a row left out can't be found (2026-11-06: 107 of them,
// from "선호하는 이름" to "앱 설치"; 2026-11-07: "백업 폴더").

// A key may depend on where the settings show (the push row's, by device),
// and a row may show only on some devices: one that isn't drawn here isn't
// found (2026-11-06: "로그인할 때" found nothing though the app had the row).
type SearchKey = string | (() => string);
type SearchRow = readonly [title: SearchKey, description?: SearchKey, shown?: () => boolean, drawnBy?: readonly SearchKey[]];

// Rows a switch or a choice draws are found too; while they aren't drawn, the
// row that draws them is the one shown (2026-11-07: "백업 폴더" with backups off).
const drawnBy = (by: readonly SearchKey[], ...titles: SearchKey[]): SearchRow[] => titles.map((title) => [title, undefined, undefined, by]);
const BACKUP_ON = ["serverConsole:schedule.enabled", "serverConsole:schedule.enabledMeta"];
const BACKUP_PLACE = ["serverConsole:schedule.target", ...BACKUP_ON];
const SMTP_ON = ["serverConsole:email.useSmtp", "serverConsole:email.useSmtpBody"];
// Rows on one of a section's tabs: while another tab shows, the tab is the
// row that draws them, and it is chosen as the row is found (settingsFoundRow).
const onTab = (tab: SearchKey, rows: readonly SearchRow[]): SearchRow[] =>
  rows.map(([title, description, shown, by]) => [title, description, shown, [...(by ?? []), tab]]);

// The restricted member rows are the organization module's (WorkspaceSharingSecurity).
// The desktop app's command search: the Mac and Windows apps (desktop/command_search_desktop.go).
const commandSearchHere = () => hasDesktopMenuBar() || packagedWindows();
const restrictable = () => organizationModule()?.restrictMember !== undefined;
// Who invites whom, the same switches in 요청's "제어" and 보안's "멤버 및 게스트" (WorkspaceSharingSecurity).
const requestControlRows: SearchRow[] = [
  ["requestControlPageAccess", "requestControlPageAccessDescription"], ["requestControlGuestInvites", "requestControlGuestInvitesDescription"],
  ["requestControlGuestInviteRequests", "requestControlGuestInviteRequestsDescription"],
  ["requestControlMemberAdds", "requestControlMemberAddsDescription"], ["requestControlMembershipOthers", "requestControlMembershipOthersDescription"],
  ["requestControlMembershipGuests", "requestControlMembershipGuestsDescription"],
  ["pendingGuestPrivatePagesLabel", "pendingGuestPrivatePagesDescription", restrictable],
  ["restrictedPrivatePagesLabel", "restrictedPrivatePagesDescription", restrictable],
];

const SETTINGS_SEARCH_ROWS: Record<SettingsSection, readonly SearchRow[]> = {
  account: [
    ["navProfile", "myAccount"], ["profileIconField"], ["emailManagementTitle", "emailManagementDescription", undefined, ["accountEmail"]], ["passwordTitle", "passwordDesc"], ["mfaTitle"],
    ["recoveryCodesTitle", "recoveryCodesDesc"], ["localLockTitle"],
    ["supportSection"], ["accountDeletionTitle", "accountDeletionDesc"], ["devicesSection"], ["addDevice", "addDeviceDesc"], ["agentSignInSetting", "agentSignInSettingDesc"], ["signOutAllDevices", "signOutAllDevicesDesc"], ["accountId"],
    ["preferredName"], ["backupSignInTitle", "backupSignInDesc"], ["passkeysTitle", "passkeysDescription"], ["accountEmail"],
    ["openInBrowser", "openInBrowserDesc", desktopOwnServer],
  ],
  preferences: [
    ["themeField", "themeDescription"], ["contrastField", "contrastDescription"],
    ["enterNewLineField", "enterNewLineDescription"],
    ["languageField", "languageDescription"],
    ["weekStartMondayField", "weekStartMondayDescription"],
    ["dateMentionFormatField", "dateMentionFormatDescription"],
    ["numberFormatField", "numberFormatDescription"],
    ["textDirectionField", "textDirectionDescription"],
    ["timeZoneAutoField", "timeZoneAutoDescription"],
    ["timeZoneField", "timeZoneDescription"],
    ["startPageField", "startPageDescription"],
    ["viewHistoryField", "viewHistoryDescription"],
    ["desktopBackground:finishSkipped"],
  ],
  notifications: [
    [() => pushSettingKeys().title, () => pushSettingKeys().description], ["notificationsSlack", "notificationsSlackDesc"],
    ["notificationsEmailActivity", "notificationsEmailActivityDesc"], ["notificationsEmailAlways", "notificationsEmailAlwaysDesc"],
    ["notificationsEmailPageUpdates", "notificationsEmailPageUpdatesDesc"], ["notificationsEmailDigest", "notificationsEmailDigestDesc"],
  ],
  // The desktop app's own rows: "배터리일 때도" shows only while "깨워 두기"
  // is on, so it is left to that row.
  servers: [
    ["navServerAccounts"], ["serverAccountsAddTitle", "serverAccountsAddBody"],
    ["desktopBackground:title", undefined, desktopBackgroundAvailable],
    ["desktopBackground:keepRunning", "desktopBackground:keepRunningHelp", desktopBackgroundAvailable],
    ["desktopBackground:openAtLogin", "desktopBackground:openAtLoginHelp", desktopBackgroundAvailable],
    ["desktopBackground:keepAwake", "desktopBackground:keepAwakeHelp", desktopBackgroundAvailable],
    ["desktopBackground:commandSearch", "desktopBackground:commandSearchHelp", commandSearchHere],
    ["desktopBackground:commandSearchShortcut", "desktopBackground:commandSearchShortcutHelp", commandSearchHere],
    ["desktopBackground:menuBarIcon", "desktopBackground:menuBarIconHelp", hasDesktopMenuBar],
    ["networkProxy:title", "networkProxy:computerMeta", desktopBackgroundAvailable],
    ["networkProxy:mode", undefined, desktopBackgroundAvailable],
    ...drawnBy(["networkProxy:mode"], "networkProxy:address", "networkProxy:noProxy", "networkProxy:username", "networkProxy:password")
      .map(([title, description, , by]) => [title, description, desktopBackgroundAvailable, by] as const),
  ],
  general: [
    ["workspaceNameField"], ["workspaceUrlField"], ["iconField", "workspaceIconHint"], ["exportTitle", "exportWorkspaceContent"],
    ["move.title", "move.meta"], ["deleteWorkspace", "deleteWorkspaceDesc"],
    ["landingPageField"], ["allowedDomainsField"], ["move.outTitle"], ["move.codeLabel"], ["move.inTitle"], ["pageViewAnalyticsField", "pageViewAnalyticsDescription"], ["pageViewAnonymousField", "pageViewAnonymousDescription"], ["peopleDirectoryField"], ["peopleActivityField"], ["peopleHoverCardsField"],
    ["peopleCsvMembers", "peopleCsvDescription"], ["memberLeaveWorkspace", "memberLeaveWorkspaceDesc"],
  ],
  // A member who can't add members asks for them with their "멤버 추가" button.
  people: [["navWorkspaceMembers"], ["memberRequestTitle", "memberRequestNote", undefined, ["memberRequestOpen"]], ["inviteLinkTitle", "inviteLinkDescription"]],
  sidebar: [["sidebarPresetsTitle", "sidebarSubtitle"], ["sidebarPresetDefaultGroup"], ["sidebarPresetOptionalGroup"]],
  import: [["importDialog:file", undefined, undefined, ["importDialog:browse.filesTitle"]], ["importDialog:notion"], ["importDialog:leafen.tab"], ["importDialog:browse.dropTitle"]],
  analytics: [["workspaceAnalytics:title"]],
  usage: [["filesTile"], ["storageLimitField", "usageLimitDescription"]],
  integrations: [
    ...onTab("connectionsInstalledTab", [["connectionsInstalledTitle", "connectionsInstalledDescription"]]),
    ...onTab("connectionsManageTab", [["allConnectionsTitle", "allConnectionsDescription"],
      ["allPersonalAccessTokensTitle", "allPersonalAccessTokensDescription"], ["allAgentKeysTitle", "allAgentKeysDescription"]]),
  ],
  connections: [
    ["mcpServerUrlTitle", "mcpServerUrlDesc"], ["mcpSnippetsTitle", "mcpSnippetsDesc"], ["agents.connectedTitle"],
    ["manualTokensTitle", "manualTokensDesc"],
    ["keyPolicyAllowed", "keyPolicyDesc"], ["keyPolicyMaxScope"], ["keyPolicyMaxDays"],
    // Its own row shows only while the address must be https; else "문구 복사" connects one.
    ["agents.connect", undefined, undefined, ["agents.copyText"]],
  ],
  "verified-pages": [["navVerifiedPages", "verifiedPagesSubtitle"]],
  "public-pages": [["navPublicPages", "publicPagesSubtitle"], ["publicSitesTitle"], ["publicFormsTitle"], ["publicLinksTitle"], ["siteBannerTitle", "siteBannerDescription"]],
  emoji: [["navEmoji", "emojiSubtitle"], ["emojiAdd"], ["emojiOwnersOnlyField", "emojiOwnersOnlyDescription"]],
  developer: [["personalAccessTokenCreateTitle", "personalAccessTokenCreateDescription"], ["developerModeField", "developerModeDescription"]],
  teamspaces: [["teamspacesOwnersOnly"]],
  "sharing-security": [
    ...onTab("securityTabGeneral", [["disablePublicSharing", "disablePublicSharingDescription"], ["disableCrossWorkspaceCopies"], ["disableExports"],
      ["viewLinksTitle"], ["viewLinksLabel"], ["viewLinkSingleUseOnlyLabel"], ["viewLinkWithoutSecretLabel"], ["viewLinkMaxSecondsLabel"], ["userHtmlLabel"]]),
    ["membersGuestsTitle"],
    ...onTab("membersGuestsTitle", [...requestControlRows, ["restrictedTeamspaceAddsLabel", "restrictedAddsDescription", restrictable],
      ["restrictedPageAddsLabel", "restrictedAddsDescription", restrictable]]),
    ...onTab("securityTabRetention", [["trashRetentionLabel", "trashRetentionDescription"], ["deletedPagesRetentionLabel", "deletedPagesRetentionDescription"]]),
  ],
  identity: [
    ["identityDomainsTitle"], ["workspaceCreationTitle", "workspaceCreationAllow"], ["managedUsersTitle", "managedUsersDashboard"],
    ["managedUserProfileChanges"], ["managedUserExternalWorkspaces"], ["emailSecondaryPolicy"], ["managedUserSessionDuration", "managedUserSessionDurationDescription"], ["managedUsersPasswordResetAll"],
    ["enterpriseSsoTitle"], ["enterpriseMcpTitle"], ["enterpriseScimTitle"], ["identityWorkspaceId"],
    ["verifiedDomainsTitle", "verifiedDomainsDescription"], ["managedUsersLogoutAll"], ["enterpriseEnableSso", "enterpriseSsoDescription"], ["enterpriseAutoCreate", "enterpriseAutoCreateDescription"], ["enterpriseLinkedWorkspace", "enterpriseLinkedWorkspacesDescription"], ["enterpriseSamlValuesTitle", "enterpriseSamlValuesDescription"], ["enterpriseMcpEnable", "enterpriseMcpDescription"], ["enterpriseMcpClientsTitle", "enterpriseMcpClientsDescription"], ["enterpriseEnableScim", "enterpriseScimDescription"], ["enterpriseScimTokensTitle", "enterpriseScimTokensDescription"], ["identityOrganizationId"],
    ...drawnBy(["enterpriseEnableSso"], "enterpriseEnforcement"),
  ],
  enterprise: [["navEnterprise"], ["enterpriseEnableDlp", "enterpriseDlpDescription"], ["enterpriseRetention", "enterpriseRetentionDescription"], ["enterpriseExportAudit", "enterpriseExportAuditWhere"], ["enterpriseExportDiscovery", "enterpriseDiscoveryDescription"], ["enterpriseLegalHoldsRow", "enterpriseLegalDescription"], ["enterpriseAdminApiConnection", "enterpriseAdminApiDescription"], ["enterpriseAdminApiTokens"],
    ...drawnBy(["enterpriseEnableDlp"], "enterpriseContentMode", "enterpriseSensitiveTerms")],
  organization: [["navPoliciesDomains"], ["organizationGroupsRow", "organizationGroupsDescription"], ["activityRetention.feed"], ["activityRetention.analytics"], ["auditScopedViewTitle"], ["viewDisplay.title"]],
  requests: [["requestsTabAll"], ...onTab("requestsTabAll", [["requestsTypeMembers"]]), ...onTab("requestsTabControls", [["requestsControlsTitle"],
    ...requestControlRows])],
  "server-users": [["serverConsole:users.title"]],
  "server-apps": [["serverConsole:apps.title", "serverConsole:apps.meta"], ["serverConsole:apps.strict.title", "serverConsole:apps.strict.body"], ["serverConsole:apps.homeScreen.title", "serverConsole:apps.homeScreen.body"], ["serverConsole:apps.oversight.title", "serverConsole:apps.oversight.body"], ["serverConsole:apps.policy.title"], ["serverConsole:apps.unsigned.title", "serverConsole:apps.unsigned.body"], ["serverConsole:apps.packageLimit.title"]],
  "server-workspaces": [["navServerWorkspaces"]],
  "server-audit": [["navAuditLog"], ["serverConsole:requestLog.title", "serverConsole:requestLog.meta"], ["serverConsole:readLog.title", "serverConsole:readLog.meta"], ["serverConsole:requestLog.retention"], ["serverConsole:requestLog.successful", "serverConsole:requestLog.help"], ["serverConsole:readLog.keep"]],
  // What SMTP needs shows once "SMTP 사용" is on.
  "server-email": [["serverConsole:email.useSmtp", "serverConsole:email.useSmtpBody"],
    ["serverConsole:email.title", "serverConsole:email.meta", undefined, SMTP_ON],
    ["serverConsole:email.requireVerification", "serverConsole:email.requireVerificationBody", undefined, SMTP_ON],
    ...drawnBy(SMTP_ON, "serverConsole:email.host", "serverConsole:email.port", "serverConsole:email.username", "serverConsole:email.senderName",
      "serverConsole:email.senderAddress", "emailProviderTestRecipient", "serverConsole:email.password", "serverConsole:email.encryption")],
  "server-providers": [["serverConsole:nav.providers", "serverConsole:providers.meta"],
    ["serverConsole:email.signup", "serverConsole:email.signupInvitedHelp"], ["serverConsole:email.memberAdd", "serverConsole:email.memberAddHelp"],
    ["serverConsole:email.switcherLists", "serverConsole:email.switcherListsHelp"],
    ["serverConsole:email.signupContact", "serverConsole:email.signupContactHelp"], ["serverConsole:providers.useGoogle"], ["serverConsole:providers.useOidc"],
    ...drawnBy(["serverConsole:providers.useGoogle", "serverConsole:providers.useOidc"], "serverConsole:providers.clientId", "serverConsole:providers.clientSecret"),
    ...drawnBy(["serverConsole:providers.useOidc"], "serverConsole:providers.displayName"),
    ...drawnBy(["serverConsole:providers.useGoogle", "serverConsole:providers.useOidc"], "serverConsole:providers.redirectUri"),
  ],
  // Each way out shows as "연결 방법" is set to it.
  "server-tunnel": [["serverConsole:tunnel.title"],
    ...drawnBy(["serverConsole:tunnel.way"], "serverConsole:tunnel.tailscale", "serverConsole:tunnel.ownAddress", "serverConsole:tunnel.quick", "serverConsole:tunnel.token"),
    ["networkProxy:title", "networkProxy:serverMeta"], ["networkProxy:mode"],
    ...drawnBy(["networkProxy:mode"], "networkProxy:address", "networkProxy:noProxy", "networkProxy:username", "networkProxy:password"),
    ["serverConsole:tunnel.way"],
    ...drawnBy(["serverConsole:tunnel.way"], "serverConsole:tunnel.addressField", "serverConsole:tunnel.tokenField"),
  ],
  "server-map": [["serverConsole:map.title"], ["serverConsole:map.useTiles"], ["serverConsole:map.tileUrl"], ["serverConsole:placeSearch.enabled"],
    ...drawnBy(["serverConsole:map.useTiles"], "serverConsole:map.attribution", "serverConsole:map.attributionUrl"),
    ...drawnBy(["serverConsole:placeSearch.enabled"], "serverConsole:placeSearch.provider", "serverConsole:placeSearch.endpoint", "serverConsole:placeSearch.apiKey", "serverConsole:placeSearch.apiKeyId")],
  "server-general": [["serverConsole:identity.name", "serverConsole:identity.nameMeta"], ["serverConsole:identity.title", "serverConsole:identity.meta"],
    ["serverConsole:report.title", undefined, undefined, ["serverConsole:report.usageTitle"]], ["serverConsole:report.storage"], ["serverConsole:report.authActivity"]],
  "server-updates": [["serverConsole:nav.updates"], ["serverConsole:updates.dailyCheck"], ["serverConsole:updates.currentTitle"],
    ["serverConsole:updates.appAutoUpdate", undefined, desktopUpdateSwitchShown],
    ["serverConsole:build.build"], ["serverConsole:build.builtAt"], ["serverConsole:build.deployedAt"], ["serverConsole:build.schema"], ["serverConsole:build.changes"]],
  "server-data": [["serverConsole:schedule.title", "serverConsole:schedule.meta"], ["serverConsole:backup.title", "serverConsole:backup.meta"], ["serverConsole:schedule.enabled", "serverConsole:schedule.enabledMeta"], ["serverConsole:backup.create"],
    ...drawnBy(BACKUP_ON, "serverConsole:protection.title", "serverConsole:schedule.target", "serverConsole:schedule.interval", "serverConsole:schedule.keep",
      "serverConsole:protection.code", "serverConsole:protection.password"),
    ...drawnBy(BACKUP_PLACE, "serverConsole:schedule.folder",
      ...[...BACKUP_PLACE_FIELDS.webdav, ...BACKUP_PLACE_FIELDS.s3].map((field) => field.label), BACKUP_PLACE_PATH_STYLE)],
  "server-standby": [["serverConsole:replication.title", "serverConsole:replication.help"], ["serverConsole:standby.title", "serverConsole:standby.addHelp"], ["serverConsole:standby.joinLabel"],
    // Continuous copies and standbys show once regular backups are on: till then "연속 복제" says so.
    ["serverConsole:replication.toggle", "serverConsole:replication.help", undefined, ["serverConsole:replication.title"]],
    ...drawnBy(["serverConsole:replication.title"], "serverConsole:standby.add"),
    ...drawnBy(["serverConsole:standby.add", "serverConsole:replication.title"], "serverConsole:standby.auto")],
  "server-restore": [["serverConsole:restore.title", "serverConsole:restore.meta"], ["serverConsole:serverMove.title", "serverConsole:serverMove.meta"], ["serverConsole:serverMove.codeLabel"]],
};

export type SettingsSearchResult = {
  section: SettingsSection;
  /** The group and the section, as "계정 / 기본 설정". */
  path: string;
  /** A row's title, then the rows that draw it, while it isn't drawn. */
  rows: Array<{ title: string; description: string; drawnBy: string[] }>;
};

function searchKey(key: SearchKey) {
  return typeof key === "function" ? key() : key;
}

export function searchSettings(
  query: string,
  groups: SettingsGroup[],
  name: { group: (group: SettingsGroup["id"]) => string; section: (section: SettingsSection) => string },
  translate: (key: string) => string,
): SettingsSearchResult[] {
  const wanted = query.trim().toLocaleLowerCase();
  if (!wanted) return [];
  // Each word is looked for on its own, anywhere in the row, as Notion's
  // search does (seen 2026-11-07: "워크스페이스 삭제" finds "내 계정 삭제",
  // whose line names the workspace; "백업 폴더" found nothing as one string).
  const words = wanted.split(/\s+/);
  const results: SettingsSearchResult[] = [];
  for (const group of groups) {
    for (const section of group.sections) {
      const sectionName = name.section(section);
      const rows = SETTINGS_SEARCH_ROWS[section].filter(([, , shown]) => !shown || shown()).map(([title, description, , by]) => ({
        title: translate(searchKey(title)),
        // A line's link marks (<people>…</people>) aren't words to find or show.
        description: description ? translate(searchKey(description)).replace(/<\/?[a-z]+>/gu, "") : "",
        drawnBy: (by ?? []).map((key) => translate(searchKey(key))),
      }));
      const found = sectionName.toLocaleLowerCase().includes(wanted)
        ? rows
        : rows.filter((row) => {
          const text = `${row.title}\n${row.description}`.toLocaleLowerCase();
          return words.every((word) => text.includes(word));
        });
      if (found.length > 0) {
        results.push({ section, path: `${name.group(group.id)} / ${sectionName}`, rows: found });
      }
    }
  }
  return results;
}
