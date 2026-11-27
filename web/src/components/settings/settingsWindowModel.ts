import type { ImportDestination, ImportSource } from "@/components/import/ImportDialog";
import { routeInfoFromPath } from "@/lib/router";

// The settings window holds what Notion's "Settings" holds, in its groups
// (seen 2026-10-02): the account, the workspace, its features and its
// administration, and for the server's administrators the server itself,
// which Notion doesn't have (an approved difference).

export const SERVER_SETTINGS_SECTIONS = [
  "server-general", "server-users", "server-workspaces", "server-apps", "server-audit", "server-email",
  "server-providers", "server-tunnel", "server-map", "server-updates", "server-data", "server-standby", "server-restore",
] as const;
export type ServerSettingsSection = typeof SERVER_SETTINGS_SECTIONS[number];

/**
 * The sections Notion's settings have no item for: the server's, the
 * account's other servers and the enterprise controls. Their names, Leafen's
 * own words in many languages, are read whole: one longer than the list goes
 * on to a second line (the baseline's Leafen-only screens, 2026-11-24);
 * Notion's own items are cut as Notion's list cuts them, with nothing on
 * hover (seen 2026-11-18).
 */
export const LEAFEN_OWN_SETTINGS_SECTIONS: ReadonlySet<string> = new Set([...SERVER_SETTINGS_SECTIONS, "servers", "enterprise"]);

export type SettingsSection =
  | "account" | "preferences" | "notifications" | "servers"
  | "general" | "people" | "sidebar" | "import" | "analytics" | "usage"
  | "integrations" | "connections" | "verified-pages" | "public-pages" | "emoji" | "developer"
  | "teamspaces" | "sharing-security" | "identity" | "enterprise" | "organization"
  | "requests"
  | ServerSettingsSection;

export type SettingsGroupId = "account" | "workspace" | "features" | "admin" | "server";

export type SettingsWindowState = {
  section: SettingsSection;
  /** The people section opens with its invitation form, as "멤버 초대" does. */
  invite?: boolean;
  /** Server › Apps opens this catalog app's review ("catalog/app"), as "설치" on the apps page asks. */
  appInstall?: string;
  /** Server › Apps opens at this installed app's row, as "사용할 사람 정하기" on the apps page asks. */
  appRow?: string;
  /** The part of the section it opens at (its data-settings-part), as a link to it asks. */
  part?: string;
  /** The rows it lights as it opens, by title, as a found row is, as the landing page's "페이지 변경하기" asks. */
  lit?: string[];
  /** Import puts files in this page, as a page's own "가져오기" asks. */
  importPlace?: ImportDestination;
  /** Import opens at this source, as a new workspace's "리펜에서" asks. */
  importSource?: ImportSource;
  /** Notion's window was asked for by an agent (its `import_screen` link, `leafen setup notion`): it opens at the token. */
  importArrival?: "agent";
};

/** What the signed-in person may see; null while it is still being read. */
export type SettingsAccess = {
  /** A workspace the person is a member of, not a guest. */
  workspace: boolean;
  /** A restricted member, who has no "연결", agents or developer settings, as Notion's "제한된 멤버" (seen 2026-11-05). */
  restricted?: boolean;
  canManage: boolean;
  canUseImports: boolean;
  canViewEnterprise: boolean | null;
  /** The organization's security and identity items, which are the IT admin's (Notion's admin roles). */
  enterpriseItems: { security: boolean; identity: boolean };
  canReadOrganization: boolean | null;
  administrator: boolean;
  /** Whether the server's apps section shows: it has the app platform, or says why not (a certified build has none). */
  appPlatform?: boolean;
  /**
   * A touch screen wider than a phone, whose list is Notion's iPad's (seen
   * 2026-11-16 in iPad Safari at 834, twice, where its computer's list has
   * it): no 이모지 (one is added from the emoji picker), and no 요청, which is
   * a "요청" tab of its 멤버 page there (seen 2026-11-16 at 834 and 1210;
   * WorkspacePeopleSection), an address asking for it opening that tab. One
   * item stays that Notion's iPad list hasn't: 가져오기, which a page's and
   * "/가져오기"'s import open, the tablet's only way in.
   */
  touchList?: boolean;
};

export type SettingsGroup = { id: SettingsGroupId; sections: SettingsSection[] };

export function settingsGroups(access: SettingsAccess): SettingsGroup[] {
  const groups: SettingsGroup[] = [{
    id: "account",
    sections: ["account", "preferences", "notifications", "servers"],
  }];
  if (access.workspace) {
    groups.push({
      id: "workspace",
      sections: [
        // In Notion's order (seen 2026-10-11): 일반 · 사람 · 사이드바 · 애널리틱스 · 가져오기; its usage is Leafen's own.
        // 요청 moved here from "사용 권한 및 청구", after 일반 (seen 2026-11-09). A member's is 일반 · 사람 · 가져오기,
        // with no 요청 or 애널리틱스 (Notion's member, seen 2026-11-21 in two tabs of the business trial).
        "general",
        ...(access.touchList || !access.canManage ? [] : ["requests" as const]),
        "people",
        ...(access.canManage ? ["sidebar" as const, "analytics" as const] : []),
        ...(access.canUseImports ? ["import" as const] : []),
        ...(access.canManage ? ["usage" as const] : []),
      ],
    });
    const emoji = access.touchList ? [] : ["emoji" as const];
    groups.push({ id: "features", sections: access.restricted
      ? ["verified-pages", "public-pages", ...emoji]
      : ["integrations", "connections", "verified-pages", "public-pages", ...emoji, "developer"] });
    const admin: SettingsSection[] = [
      ...(access.canManage ? ["teamspaces" as const] : []),
      // Each admin role shows only the settings it covers.
      ...(access.canViewEnterprise ? [
        ...(access.enterpriseItems.security ? ["sharing-security" as const] : []),
        ...(access.enterpriseItems.identity ? ["identity" as const] : []),
        "enterprise" as const,
      ] : []),
      ...(access.canReadOrganization ? ["organization" as const] : []),
    ];
    if (admin.length > 0) groups.push({ id: "admin", sections: admin });
  }
  if (access.administrator) {
    groups.push({ id: "server", sections: SERVER_SETTINGS_SECTIONS.filter((section) => section !== "server-apps" || access.appPlatform) });
  }
  return groups;
}

const ACCOUNT_SECTIONS: readonly SettingsSection[] = ["account", "preferences", "notifications", "servers"];

/**
 * The section the window shows: the one asked for when the person may see
 * it, the one asked for while that is still being read, or else the
 * workspace's general settings for a workspace's section, and the account
 * for the rest.
 */
export function visibleSettingsSection(section: SettingsSection, access: SettingsAccess): SettingsSection {
  if (section === "requests" && access.touchList && access.canManage) return visibleSettingsSection("people", access);
  if (settingsGroups(access).some((group) => group.sections.includes(section))) return section;
  const pending = access.workspace && (
    (access.canViewEnterprise === null && (section === "sharing-security" || section === "identity" || section === "enterprise"))
    || (access.canReadOrganization === null && section === "organization")
  );
  if (pending) return section;
  const workspaceSection = !ACCOUNT_SECTIONS.includes(section) && !section.startsWith("server-");
  return access.workspace && workspaceSection ? "general" : "account";
}

const WORKSPACE_ADDRESS_SECTIONS: Record<string, SettingsSection> = {
  overview: "general", general: "general", mcp: "connections", agents: "connections", connections: "connections", integrations: "integrations", "verified-pages": "verified-pages", "public-pages": "public-pages", emoji: "emoji", developer: "developer",
  analytics: "analytics", organization: "organization", "policies-domains": "organization",
  enterprise: "enterprise", identity: "identity", "sharing-security": "sharing-security", members: "people", people: "people",
  teamspaces: "teamspaces", usage: "usage", import: "import", requests: "requests", sidebar: "sidebar",
};
const ACCOUNT_ADDRESS_SECTIONS: Record<string, SettingsSection> = {
  profile: "account", security: "account", account: "account", preferences: "preferences",
  notifications: "notifications", servers: "servers",
};

/**
 * The window an address of the former consoles opens, and the place it
 * goes back to: /settings (the workspace console), /account and
 * /admin/settings keep working as links.
 */
export function legacySettingsAddress(pathname: string, search: string): {
  state: SettingsWindowState;
  returnTo: string;
} | null {
  const params = new URLSearchParams(search);
  const requested = params.get("section") ?? "";
  if (pathname === "/admin/settings" || pathname === "/admin/settings/") {
    // A notice opens the server section it names (an app's the apps, the
    // address certificate alert remote access, a sign-in return change the
    // sign-in providers); the console's own address, the mail settings.
    const named = SERVER_SETTINGS_SECTIONS.find((section) => section === `server-${requested}`);
    // One that names the workspace's or the account's opens that, not the mail settings.
    const other = named ? undefined : WORKSPACE_ADDRESS_SECTIONS[requested] ?? ACCOUNT_ADDRESS_SECTIONS[requested];
    if (other) return { state: { section: other, ...settingsPart(params) }, returnTo: returnAddress(params.get("from")) };
    const section: ServerSettingsSection = named ?? "server-email";
    // An app that can't open sends its administrator here and back, and the
    // apps page's "설치" its app's review (2026-10-16).
    const appInstall = section === "server-apps" ? params.get("install") ?? undefined : undefined;
    const appRow = section === "server-apps" ? params.get("app") ?? undefined : undefined;
    const state: SettingsWindowState = appInstall ? { section, appInstall } : appRow ? { section, appRow } : { section, ...settingsPart(params) };
    return { state, returnTo: returnAddress(params.get("from")) };
  }
  const route = routeInfoFromPath(pathname);
  if (route.kind === "settings") {
    const section = WORKSPACE_ADDRESS_SECTIONS[requested] ?? ACCOUNT_ADDRESS_SECTIONS[requested] ?? "general";
    // The people's address shows the people; its form opens only when asked
    // for. The import's opens Notion's window when asked (`leafen setup notion`).
    const importSource = section === "import" && params.get("source") === "notion"
      ? { importSource: "notion" as const, ...(params.get("via") === "agent" ? { importArrival: "agent" as const } : {}) } : {};
    return { state: { section, invite: section === "people" && params.has("invite"), ...importSource, ...settingsPart(params) },
      returnTo: returnAddress(params.get("from")) };
  }
  if (route.kind === "account") {
    return { state: { section: ACCOUNT_ADDRESS_SECTIONS[requested] ?? "account", ...settingsPart(params) }, returnTo: returnAddress(params.get("from")) };
  }
  return null;
}

function settingsPart(params: URLSearchParams): Pick<SettingsWindowState, "part"> {
  const part = params.get("part") ?? "";
  return /^[a-z-]{1,32}$/u.test(part) ? { part } : {};
}

function returnAddress(candidate: string | null): string {
  if (!candidate || !candidate.startsWith("/") || candidate.startsWith("//")) return "/";
  try {
    const url = new URL(candidate, window.location.origin);
    const kind = routeInfoFromPath(url.pathname).kind;
    if (url.origin !== window.location.origin || kind === "account" || kind === "settings") return "/";
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return "/";
  }
}
