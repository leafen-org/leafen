import { request } from "@/apiTransport";
import {
  appPlatformOperations,
  type AppGrant,
  type AppGroup,
  type AppGroupList,
  type AppInstall,
  type AppPackageReview,
  type AppPlatformListResponse,
  type AppPlatformSettings,
  type AppTenancy,
  type AppRemovalPreview,
  type AppHookList,
  type AppJobs,
  type AppRunsWindow,
  type AppApprovalHistoryWindow,
  type AppGuests,
  type AppGuestLinksWindow,
  type AppGuestReportsWindow,
  type AppCatalogBrowse,
  type AppUsage,
  type AppHookSecret,
  type AppSecretList,
  type AppSettingList,
  type AppTenancyPeople,
  type AppAdminCredentialList,
  type AppStatusInput,
  type AppAllowlistEntry,
  type AppRequest,
  type AppRequestList,
  type AppEndpointLimits,
  type AppEndpointUsage,
} from "@/appPlatformWire.generated";
import { answeredUnlessAway } from "@/lib/serverReach";
import type { Session } from "@/types";

// The server administrator's app calls (internal/app/app_platform_routes.go).

const fill = (path: string, values: Record<string, string>) =>
  path.replace(/\{(\w+)\}/gu, (_, name: string) => encodeURIComponent(values[name] ?? ""));

/** Server › Apps' list; failing at once while the server is away (answeredUnlessAway). */
export function listInstalledApps(session: Session, signal?: AbortSignal) {
  return answeredUnlessAway(request<AppPlatformListResponse>(appPlatformOperations.list.path, { method: appPlatformOperations.list.method, signal }, session));
}

/** The waiting requests after the list's first window, a window at a time. */
export function listAppRequests(session: Session, cursor: string, signal?: AbortSignal) {
  return request<AppRequestList>(`${appPlatformOperations.listRequests.path}?cursor=${encodeURIComponent(cursor)}`,
    { method: appPlatformOperations.listRequests.method, signal }, session);
}

/** Uploads a .leafenapp file and returns what it asks for, uninstalled. */
export function uploadAppPackage(session: Session, file: Blob, signal?: AbortSignal) {
  return request<AppPackageReview>(appPlatformOperations.uploadPackage.path, {
    method: appPlatformOperations.uploadPackage.method, body: file, headers: { "content-type": "application/zip" }, signal,
  }, session);
}

export function installAppPackage(session: Session, review: AppPackageReview, acceptDataRemovals = false, personCredentials?: boolean) {
  return request<AppInstall>(appPlatformOperations.install.path, {
    method: appPlatformOperations.install.method,
    body: JSON.stringify({ packageHash: review.packageHash, acceptPermissionsHash: review.permissionsHash, ...(acceptDataRemovals ? { acceptDataRemovals } : {}),
      ...(personCredentials === undefined ? {} : { personCredentials }) }),
  }, session);
}

export function setInstalledAppStatus(session: Session, installId: string, status: AppStatusInput["status"]) {
  return request<AppInstall>(fill(appPlatformOperations.setStatus.path, { installId }), {
    method: appPlatformOperations.setStatus.method, body: JSON.stringify({ status }),
  }, session);
}

export function setInstalledAppGrants(session: Session, installId: string, grants: AppGrant[]) {
  return request<AppInstall>(fill(appPlatformOperations.setGrants.path, { installId }), {
    method: appPlatformOperations.setGrants.method, body: JSON.stringify({ grants }),
  }, session);
}

/** An organization's people, for choosing who may use an app. */
export function listTenancyPeople(session: Session, tenancy: string, query: string) {
  const search = new URLSearchParams({ tenancy, q: query.trim() });
  return request<AppTenancyPeople>(`${appPlatformOperations.tenancyPeople.path}?${search}`, { method: appPlatformOperations.tenancyPeople.method }, session);
}

/** The groups one organization may grant apps to, every window of them. */
export async function listTenancyGroups(session: Session, tenancy: string, signal?: AbortSignal) {
  const groups: AppGroup[] = [];
  let cursor = "";
  do {
    const search = new URLSearchParams({ tenancy, ...(cursor ? { cursor } : {}) });
    const read = await request<AppGroupList>(`${appPlatformOperations.tenancyGroups.path}?${search}`,
      { method: appPlatformOperations.tenancyGroups.method, signal }, session);
    groups.push(...read.groups);
    cursor = read.nextCursor ?? "";
  } while (cursor);
  return groups;
}

/** Puts one organization's apps under strict mode, or takes them out. */
export function setAppStrict(session: Session, tenancyKey: string, strict: boolean) {
  return request<AppTenancy[]>(appPlatformOperations.setStrict.path, {
    method: appPlatformOperations.setStrict.method, body: JSON.stringify({ tenancyKey, strict }),
  }, session);
}

/** Oversees an organization's apps or stops: every action there asks a person, none for an hour on the app's screen. */
export function setAppOversight(session: Session, tenancyKey: string, overseen: boolean) {
  return request<AppTenancy[]>(appPlatformOperations.setOversight.path, {
    method: appPlatformOperations.setOversight.method, body: JSON.stringify({ tenancyKey, overseen }),
  }, session);
}

/** Takes the app off: an app without data at once, else as chosen — its data kept, or the app and its data to the trash. */
export function removeInstalledApp(session: Session, installId: string, mode?: "trash" | "keep") {
  return request<void>(fill(appPlatformOperations.remove.path, { installId }), {
    method: appPlatformOperations.remove.method, ...(mode ? { body: JSON.stringify({ mode }) } : {}),
  }, session);
}

export function readAppRemoval(session: Session, installId: string) {
  return request<AppRemovalPreview>(fill(appPlatformOperations.readRemoval.path, { installId }), { method: appPlatformOperations.readRemoval.method }, session);
}

export function restoreInstalledApp(session: Session, installId: string) {
  return request<AppInstall>(fill(appPlatformOperations.restore.path, { installId }), { method: appPlatformOperations.restore.method }, session);
}

/** Strict mode: every app's outside calls go through the relay. */
export function updateAppPlatformSettings(session: Session, settings: AppPlatformSettings) {
  return request<AppPlatformSettings>(appPlatformOperations.updateSettings.path, {
    method: appPlatformOperations.updateSettings.method, body: JSON.stringify(settings),
  }, session);
}

/** The secrets an app's relay addresses name, each set or not, by its last four characters. */
export function listAppSecrets(session: Session, installId: string) {
  return request<AppSecretList>(fill(appPlatformOperations.listSecrets.path, { installId }), { method: appPlatformOperations.listSecrets.method }, session);
}

/**
 * Says an app's outside sign-in's provider has the server's current return
 * address after an address change, or takes it back.
 */
export function setAppOAuthReturn(session: Session, installId: string, provider: string, added: boolean) {
  return request<AppSecretList>(fill(appPlatformOperations.oauthReturn.path, { installId, provider }), {
    method: appPlatformOperations.oauthReturn.method, body: JSON.stringify({ added }),
  }, session);
}

export function listAppSettings(session: Session, installId: string) {
  return request<AppSettingList>(fill(appPlatformOperations.listAppSettings.path, { installId }), { method: appPlatformOperations.listAppSettings.method }, session);
}

/** Sets an address the app's sockets use; an empty one clears it. */
export function setAppSetting(session: Session, installId: string, key: string, value: string) {
  return request<AppSettingList>(fill(appPlatformOperations.setAppSetting.path, { installId, key }), {
    method: appPlatformOperations.setAppSetting.method, body: JSON.stringify({ value }),
  }, session);
}

/** Whether people may give the app credentials, and how many have. */
export function listAppAdminCredentials(session: Session, installId: string) {
  return request<AppAdminCredentialList>(fill(appPlatformOperations.adminCredentials.path, { installId }), { method: appPlatformOperations.adminCredentials.method }, session);
}

export function setAppCredentialsPolicy(session: Session, installId: string, enabled: boolean) {
  return request<AppAdminCredentialList>(fill(appPlatformOperations.setCredentialsPolicy.path, { installId }), {
    method: appPlatformOperations.setCredentialsPolicy.method, body: JSON.stringify({ enabled }),
  }, session);
}

/** Takes back every credential people gave the app. */
export function revokeAppCredentials(session: Session, installId: string) {
  return request<AppAdminCredentialList>(fill(appPlatformOperations.revokeCredentials.path, { installId }), {
    method: appPlatformOperations.revokeCredentials.method,
  }, session);
}

export function setAppSecret(session: Session, installId: string, name: string, value: string) {
  return request<AppSecretList>(fill(appPlatformOperations.setSecret.path, { installId, name }), {
    method: appPlatformOperations.setSecret.method, body: JSON.stringify({ value }),
  }, session);
}

export function clearAppSecret(session: Session, installId: string, name: string) {
  return request<AppSecretList>(fill(appPlatformOperations.clearSecret.path, { installId, name }), {
    method: appPlatformOperations.clearSecret.method,
  }, session);
}

/** The app's incoming hooks: address, whether a secret is set, what waits. */
export function listAppHooks(session: Session, installId: string) {
  return request<AppHookList>(fill(appPlatformOperations.listHooks.path, { installId }), { method: appPlatformOperations.listHooks.method }, session);
}

/** Makes a hook's secret anew; the secret and the address come back only this once. */
export function makeAppHookSecret(session: Session, installId: string, hook: string, tenancyKey = "") {
  return request<AppHookSecret>(fill(appPlatformOperations.makeHookSecret.path, { installId, hook }), {
    method: appPlatformOperations.makeHookSecret.method, ...(tenancyKey ? { body: JSON.stringify({ tenancyKey }) } : {}),
  }, session);
}

/** Takes an organization's address for the hook away. */
export function removeAppHookOrg(session: Session, installId: string, hook: string, organizationId: string) {
  return request<AppHookList>(fill(appPlatformOperations.removeHookOrg.path, { installId, hook, organizationId }), {
    method: appPlatformOperations.removeHookOrg.method,
  }, session);
}

/** An app's work: schedules, data events, what waits or failed, recent runs. */
export function listAppJobs(session: Session, installId: string) {
  return request<AppJobs>(fill(appPlatformOperations.listJobs.path, { installId }), { method: appPlatformOperations.listJobs.method }, session);
}

/** One of an app's lists for the administrator, a window from the cursor on (the list's own nextCursor). */
function appListWindow<T>(operation: { method: string; path: string }, session: Session, installId: string, cursor: string) {
  return request<T>(`${fill(operation.path, { installId })}?cursor=${encodeURIComponent(cursor)}`, { method: operation.method }, session);
}

export const listAppRuns = (session: Session, installId: string, cursor: string) =>
  appListWindow<AppRunsWindow>(appPlatformOperations.listJobRuns, session, installId, cursor);

export const listAppApprovals = (session: Session, installId: string, cursor: string) =>
  appListWindow<AppApprovalHistoryWindow>(appPlatformOperations.listJobApprovals, session, installId, cursor);

export const listAppGuestLinks = (session: Session, installId: string, cursor: string) =>
  appListWindow<AppGuestLinksWindow>(appPlatformOperations.listGuestLinks, session, installId, cursor);

export const listAppGuestReports = (session: Session, installId: string, cursor: string) =>
  appListWindow<AppGuestReportsWindow>(appPlatformOperations.listGuestReports, session, installId, cursor);

export function getAppUsage(session: Session, installId: string) {
  return request<AppUsage>(fill(appPlatformOperations.usage.path, { installId }), { method: appPlatformOperations.usage.method }, session);
}

/** Sets the app's endpoint limits; the defaults clear them. */
export function setAppEndpointLimits(session: Session, installId: string, limits: AppEndpointLimits) {
  return request<AppEndpointUsage>(fill(appPlatformOperations.setEndpointLimits.path, { installId }), {
    method: appPlatformOperations.setEndpointLimits.method, body: JSON.stringify(limits),
  }, session);
}

/** Opens a public endpoint to requests without signing in, or closes it. */
export function setAppEndpointPublic(session: Session, installId: string, key: string, open: boolean) {
  return request<AppEndpointUsage>(fill(appPlatformOperations.setEndpointPublic.path, { installId }), {
    method: appPlatformOperations.setEndpointPublic.method, body: JSON.stringify({ key, open }),
  }, session);
}

/** The app's last 30 days of what ran, what it reached and what changed about it, without anyone's data. */
export function exportAppLog(session: Session, installId: string) {
  return request<unknown>(fill(appPlatformOperations.logExport.path, { installId }), { method: appPlatformOperations.logExport.method }, session);
}

export function setAppSchedule(session: Session, installId: string, schedule: string, change: { enabled?: boolean; timeZone?: string }) {
  return request<AppJobs>(fill(appPlatformOperations.setSchedule.path, { installId, schedule }), {
    method: appPlatformOperations.setSchedule.method, body: JSON.stringify(change),
  }, session);
}

export function readAppGuests(session: Session, installId: string) {
  return request<AppGuests>(fill(appPlatformOperations.readGuests.path, { installId }), { method: appPlatformOperations.readGuests.method }, session);
}

export function setAppGuests(session: Session, installId: string, enabled: boolean) {
  return request<AppGuests>(fill(appPlatformOperations.setGuests.path, { installId }), {
    method: appPlatformOperations.setGuests.method, body: JSON.stringify({ enabled }),
  }, session);
}

export function revokeAppGuestLink(session: Session, installId: string, linkId: string) {
  return request<AppGuests>(fill(appPlatformOperations.revokeGuestLink.path, { installId, linkId }), {
    method: appPlatformOperations.revokeGuestLink.method,
  }, session);
}

/** "앱 둘러보기"' apps; failing at once while the server is away (answeredUnlessAway). */
export function listAppCatalogs(session: Session) {
  return answeredUnlessAway(request<AppCatalogBrowse>(appPlatformOperations.listCatalogs.path, { method: appPlatformOperations.listCatalogs.method }, session));
}

export function addAppCatalog(session: Session, input: { name: string; url: string; publicKey: string }) {
  return request<AppCatalogBrowse>(appPlatformOperations.addCatalog.path, {
    method: appPlatformOperations.addCatalog.method, body: JSON.stringify(input),
  }, session);
}

export function uploadAppCatalog(session: Session, input: { name: string; publicKey: string; bundle: File }) {
  const form = new FormData();
  form.set("name", input.name);
  form.set("publicKey", input.publicKey);
  form.set("bundle", input.bundle);
  return request<AppCatalogBrowse>(appPlatformOperations.uploadCatalog.path, { method: appPlatformOperations.uploadCatalog.method, body: form }, session);
}

export function refreshAppCatalog(session: Session, catalogId: string) {
  return request<AppCatalogBrowse>(fill(appPlatformOperations.refreshCatalog.path, { catalogId }), {
    method: appPlatformOperations.refreshCatalog.method,
  }, session);
}

export function removeAppCatalog(session: Session, catalogId: string) {
  return request<AppCatalogBrowse>(fill(appPlatformOperations.removeCatalog.path, { catalogId }), {
    method: appPlatformOperations.removeCatalog.method,
  }, session);
}

export function fetchCatalogApp(session: Session, catalogId: string, appId: string, signal?: AbortSignal) {
  return request<AppPackageReview>(fill(appPlatformOperations.fetchCatalogApp.path, { catalogId, appId }), {
    method: appPlatformOperations.fetchCatalogApp.method, signal,
  }, session);
}

export function decideAppRequest(session: Session, requestId: string, approve: boolean) {
  return request<AppRequest>(fill(appPlatformOperations.decideRequest.path, { requestId }), {
    method: appPlatformOperations.decideRequest.method, body: JSON.stringify({ approve }),
  }, session);
}

export function setAppAllowlist(session: Session, apps: AppAllowlistEntry[]) {
  return request<{ apps: AppAllowlistEntry[] }>(appPlatformOperations.setAllowlist.path, {
    method: appPlatformOperations.setAllowlist.method, body: JSON.stringify({ apps }),
  }, session);
}

/** An app's name in the reader's language, else English, else its first. */
/** Leafen's own apps name their maker as the language writes Leafen. */
export function appDeveloperName(name: string, language: string) {
  return name === "Leafen" && language.startsWith("ko") ? "리펜" : name;
}

/**
 * Who an app is from, in one phrase on every screen: a built-in app is
 * "리펜 제공" (Leafen's), as its card says; any other is "<developer> 제작"
 * (2026-11-05: its frame's bar said "리펜 제작", an agent's page "Leafen 제작").
 * The caller says it with its own words: serverConsole:apps.trust.builtin, or
 * its "by" line with the developer.
 */
export function appMaker(developer: string, builtin: boolean | undefined, language: string) {
  return builtin ? { builtin: true as const } : { builtin: false as const, developer: appDeveloperName(developer, language) };
}

export function appText(text: Record<string, string>, language: string) {
  const base = language.split("-")[0];
  return text[language] ?? text[base] ?? text.en ?? Object.values(text)[0] ?? "";
}
