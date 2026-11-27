import { useEffect, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import type {
  AppAllowlistEntry, AppCatalogEntry, AppGrant, AppInstall, AppPackageReview, AppPermission, AppPlatformSettings, AppRemovalPreview, AppRequest,
  AppTenancy,
} from "@/appPlatformWire.generated";
import { announceAppsChanged } from "@/lib/appsChangedEvents";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { confirmAction } from "../ConfirmActionDialog";
import {
  appDeveloperName,
  appText,
  installAppPackage,
  listAppRequests,
  listInstalledApps,
  readAppRemoval,
  removeInstalledApp,
  restoreInstalledApp,
  setInstalledAppGrants,
  setInstalledAppStatus,
  updateAppPlatformSettings,
  uploadAppPackage,
  decideAppRequest, fetchCatalogApp, listAppCatalogs, setAppAllowlist,
} from "./administratorAppsApi";
import { AppRequestsPanel } from "./AppRequestsPanel";
import { AppGrantPeopleDialog } from "./AppGrantPeopleDialog";
import { AppGrantRow } from "./AppGrantRow";
import { AppPackageReviewDialog } from "./AppPackageReviewDialog";
import { AppRemovalDialog } from "./AppRemovalDialog";
import { AppRowIcon } from "./AppRowIcon";
import { AppDataFailureNotes } from "./AppDataFailureNotes";
import { AppBrowseDialog } from "./AppBrowseDialog";
import { useOptionalProductToast } from "@/lib/productToast";
import { AppGuestsDialog } from "./AppGuestsDialog";
import { AdministratorAppsListEnd } from "./AdministratorAppsListEnd";
import { appPlatformBounds, appPlatformErrorCodes } from "@/appPlatformWire.generated";
import { LeafenApiError } from "@/apiTransport";
import { formatFileSize } from "@/components/database/files";

// The sizes an administrator may hold app packages to, the largest being the
// most Leafen takes.
const PACKAGE_LIMITS = [32 << 20, 256 << 20, 1 << 30, appPlatformBounds.packageMaxBytes];
// The lines the secrets window fills: secrets, sign-ins, hooks, set addresses, people's own accounts.
const SECRET_KINDS = ["secret", "outsideSignIn", "webhook", "settingSocket", "personCredential"];
import { AppJobsDialog } from "./AppJobsDialog";
import { useAppInstallLink } from "./useAppInstallLink";
import { useAppPackageRead } from "./useAppPackageRead";
import { AppUsageDialog } from "./AppUsageDialog";
import { AppSecretsDialog } from "./AppSecretsDialog";
import { AppOversightSettings } from "./AppOversightSettings";
import { AppStrictTenancies } from "./AppStrictTenancies";
import { SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import pageStyles from "./AdministratorSettingsPage.module.css";
import styles from "./AdministratorApps.module.css";
import { administratorUserStyles as rowStyles } from "./administratorUserManagementStyles";
import { displayDate } from "@/lib/displayDate";
import { AppSelect } from "@/components/AppSelect";
import { AppRowMenu } from "./AppRowMenu";

/**
 * The server's apps, for its administrators: each installed app
 * with who made it, how far it is trusted, whether it runs and who may use
 * it in each organization, its secrets, and installing one from a
 * .leafenapp file after reviewing what it asks for. Strict mode sends every
 * app's outside calls through the relay.
 */
export function AdministratorApps({ session, appInstall, appRow, currentTenancy }: {
  session: Session; appInstall?: string; appRow?: string; currentTenancy?: string;
}) {
  const { t, i18n } = useTranslation("serverConsole");
  const [installs, setInstalls] = useState<AppInstall[]>([]);
  // "사용할 사람 정하기" on the apps page lands on its app's row, its "who can
  // use it" (a stopped app's state) taking the focus as after an install, once
  // the list has come in.
  const shownRow = useRef("");
  useEffect(() => {
    // The apps page names the app, an app's own address its slug.
    const shown = installs.find((install) => !install.removedAt && (install.appId === appRow || install.slug === appRow));
    if (!appRow || shownRow.current === appRow || !shown) return;
    shownRow.current = appRow;
    window.requestAnimationFrame(() => {
      const row = document.querySelector<HTMLElement>(`[data-app-install="${CSS.escape(shown.appId)}"]`);
      row?.scrollIntoView({ block: "center" });
      // A stopped app is turned on first; another is given to people.
      const first = shown.status === "active" ? "[data-app-grant]" : "[data-app-status]";
      (row?.querySelector<HTMLElement>(`${first} button`) ?? row?.querySelector<HTMLElement>(first))?.focus({ preventScroll: true });
    });
  }, [appRow, installs]);
  const [tenancies, setTenancies] = useState<AppTenancy[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(""), [readFailed, setReadFailed] = useState(""); // a failed read of the list, said in its place
  const [review, setReview] = useState<AppPackageReview | null>(null);
  const [reviewError, setReviewError] = useState("");
  const [busy, setBusy] = useState(false);
  // A change says it was saved, or why not, wherever its control is on the
  // page (2026-10-16: changing the install policy or an app's state said
  // nothing, and a refusal showed under the whole list).
  const toast = useOptionalProductToast();
  const sayFailed = (failure: unknown) => {
    const message = settingsErrorMessage(failure, t("apps.changeFailed"));
    setError(message);
    toast?.notify(message, "error");
  };
  const [removal, setRemoval] = useState<{ install: AppInstall; preview: AppRemovalPreview } | null>(null);
  const [removalError, setRemovalError] = useState("");
  const [settings, setSettings] = useState<AppPlatformSettings>({ strictNetwork: false });
  const [secretsFor, setSecretsFor] = useState<AppInstall | null>(null);
  const [jobsFor, setJobsFor] = useState<AppInstall | null>(null);
  const [usageFor, setUsageFor] = useState<AppInstall | null>(null);
  const [guestsFor, setGuestsFor] = useState<AppInstall | null>(null);
  const [browsing, setBrowsing] = useState(false);
  const [requests, setRequests] = useState<AppRequest[]>([]);
  const [allowlist, setAllowlist] = useState<AppAllowlistEntry[]>([]);
  // A request whose app is being reviewed: installing it approves the request.
  const approvingRef = useRef<AppRequest | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const installFileRef = useRef<HTMLButtonElement>(null), reviewOpener = useRef<HTMLElement | null>(null);

  // A change saved while a reading of the list was on its way wins over it:
  // the list reread when installing changed the session came back after the
  // first "everyone" and unchecked it (2026-10-16).
  const savedChanges = useRef(0);
  const [updates, setUpdates] = useState<AppCatalogEntry[]>([]);
  async function load() {
    setLoading(true);
    const changesBefore = savedChanges.current;
    try {
      const listed = await listInstalledApps(session);
      if (savedChanges.current === changesBefore) setInstalls(listed.installs);
      setTenancies(listed.tenancies);
      setSettings(listed.settings ?? { strictNetwork: false });
      // Every waiting request, past the list's first window too.
      const requests = [...(listed.requests ?? [])];
      let cursor = listed.requestsNextCursor;
      while (cursor) {
        const page = await listAppRequests(session, cursor);
        requests.push(...page.requests);
        cursor = page.nextCursor;
      }
      setRequests(requests);
      setAllowlist(listed.allowlist ?? []);
      setError(""); setReadFailed("");
      // A newer version shows on the app's row; only opening Browse apps said so.
      void listAppCatalogs(session).then((browse) => setUpdates(browse.apps.filter((app) => app.installedVersion && !app.revoked)), () => undefined);
    } catch (failure) {
      setReadFailed(settingsErrorMessage(failure, t("apps.readFailed")));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // The list reloads when the session changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);

  const replace = (next: AppInstall) => {
    savedChanges.current += 1;
    setInstalls((current) => {
      const found = current.some((install) => install.installId === next.installId);
      return found ? current.map((install) => (install.installId === next.installId ? next : install)) : [...current, next];
    });
  };

  // A package is read in the review's own window, which says what it is
  // reading from the press on and why it stopped if it did (2026-11-12).
  const packages = useAppPackageRead((next) => { setReviewError(""); setReview(next); });
  /** A catalog's app, read for its review: to install, update, or approve a request for it. */
  function readCatalogApp(catalogId: string, appId: string, name?: Record<string, string>, version?: string) {
    setError("");
    setReviewError("");
    packages.read({ source: "catalog", name: name ? appText(name, i18n.language) : undefined, detail: version ? t("apps.version", { version }) : undefined },
      (signal) => fetchCatalogApp(session, catalogId, appId, signal), (failure) => settingsErrorMessage(failure, t("apps.browse.fetchFailed")));
  }
  useAppInstallLink(appInstall, (catalogId, appId) => readCatalogApp(catalogId, appId));

  function upload(file: File) {
    // The review comes after the file is read; closing it gives the focus back to "앱 파일로 설치".
    reviewOpener.current = installFileRef.current;
    setError("");
    setReviewError("");
    packages.read({ source: "file", name: file.name, detail: formatFileSize(file.size) }, (signal) => uploadAppPackage(session, file, signal), (failure) => {
      // A package Leafen refused says so and where its problems are listed —
      // `leafen app validate` in its folder, the checks Leafen ran (2026-10-16:
      // it said only that the file couldn't be read).
      const refused = failure instanceof LeafenApiError
        && (failure.code === appPlatformErrorCodes.manifestInvalid || failure.code === appPlatformErrorCodes.packageInvalid);
      const unsigned = failure instanceof LeafenApiError && failure.code === appPlatformErrorCodes.unsignedRefused;
      const tooLarge = failure instanceof LeafenApiError && failure.code === appPlatformErrorCodes.packageTooLarge;
      return unsigned ? t("apps.unsignedRefused") : tooLarge ? t("apps.packageTooLarge") : refused ? t("apps.uploadRefused")
        : settingsErrorMessage(failure, t("apps.uploadFailed"));
    });
    if (fileRef.current) fileRef.current.value = "";
  }

  async function install(acceptDataRemovals: boolean, everyone: readonly string[], personCredentials?: boolean) {
    if (!review) return;
    setBusy(true);
    try {
      let installed = await installAppPackage(session, review, acceptDataRemovals, personCredentials);
      if (!review.installed && everyone.length) {
        installed = await setInstalledAppGrants(session, installed.installId,
          everyone.map((tenancyKey) => ({ tenancyKey, everyone: true, userIds: [] })));
      }
      replace(installed);
      announceAppsChanged();
      setReview(null);
      setBrowsing(false);
      const change = review.installed ? review.installed.change || "update" : "";
      toast?.notify(t(change === "reinstall" ? "apps.reinstalledToast" : change === "downgrade" ? "apps.downgradedToast"
        : change ? "apps.updatedToast" : "apps.installedToast", { name: appText(installed.name, i18n.language), version: installed.version }), "success");
      // What the app needs before it works — its secrets and the addresses the
      // administrator sets — right after installing; an update asks only for
      // what it newly needs (2026-10-16: every update opened the window again).
      const needs = (list: readonly AppPermission[]) => list.filter((permission) => SECRET_KINDS.includes(permission.kind))
        .map((permission) => `${permission.kind}\u0000${permission.detail}`);
      const had = new Set(review.installed ? needs(review.installed.permissions) : []);
      const setUp = needs(installed.permissions).some((need) => !had.has(need));
      // A new app nobody was chosen for yet says so on its row, and its "who
      // can use it" takes the focus; otherwise its own row does, not the
      // window's search. The window for what it needs opens from there, so
      // closing it comes back to the row (2026-11-10: opened as the review
      // closed, it went back to the review's gone "설치", the focus nowhere).
      reviewOpener.current = null;
      const row = `[data-app-install="${CSS.escape(installed.appId)}"]`;
      window.requestAnimationFrame(() => {
        document.querySelector<HTMLElement>(!review.installed && !everyone.length ? `${row} [data-app-grant]` : `${row} button:not(:disabled)`)?.focus();
        if (setUp) setSecretsFor(installed);
      });
      const approving = approvingRef.current;
      approvingRef.current = null;
      if (approving) {
        await decideAppRequest(session, approving.id, true);
        void load();
      }
    } catch (failure) {
      setReviewError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function change(action: () => Promise<AppInstall | void>, installId?: string, done?: string) {
    setBusy(true);
    setError("");
    try {
      const next = await action();
      if (next) replace(next);
      else if (installId) setInstalls((current) => current.filter((install) => install.installId !== installId));
      announceAppsChanged();
      // A change its row shows (the state's line, the people ticked) says
      // nothing more (2026-11-06, as a saved switch); one it doesn't, says so.
      if (done) toast?.notify(done, "success");
    } catch (failure) {
      sayFailed(failure);
    } finally {
      setBusy(false);
    }
  }

  // An app with data shows what goes with it and asks how; one without asks only to confirm.
  async function remove(install: AppInstall) {
    let preview: AppRemovalPreview;
    try {
      preview = await readAppRemoval(session, install.installId);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
      return;
    }
    // A workspace the app made and nothing went into has nothing to decide about.
    if (preview.legalHold || preview.workspaces.some((workspace) => workspace.pages || workspace.databases || workspace.legalHold)) {
      setRemovalError("");
      setRemoval({ install, preview });
      return;
    }
    const name = appText(install.name, i18n.language);
    const confirmed = await confirmAction({
      message: t("apps.removeConfirmTitle", { name }),
      detail: preview.workspaces.length ? t("apps.removeConfirmTrash", { days: preview.keepDays }) : t("apps.removeConfirmBody"), confirmLabel: t("apps.remove"), danger: true,
    });
    if (confirmed && preview.workspaces.length) await confirmRemoval("trash", install);
    else if (confirmed) await change(() => removeInstalledApp(session, install.installId), install.installId, t("apps.removedToast", { name }));
  }

  async function confirmRemoval(mode: "trash" | "keep", target = removal?.install) {
    if (!target) return;
    const { appId } = target;
    const name = appText(target.name, i18n.language);
    setBusy(true);
    try {
      await removeInstalledApp(session, target.installId, mode);
      announceAppsChanged();
      setRemoval(null);
      toast?.notify(t(mode === "trash" ? "apps.trashedToast" : "apps.removedToast", { name }), "success");
    } catch (failure) {
      setRemovalError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
      // Focus goes to what the removal left: the row's way back.
      void Promise.resolve(load()).then(() => window.requestAnimationFrame(() => document.querySelector<HTMLElement>(
        `[data-app-install="${CSS.escape(appId)}"] [data-app-restore]`)?.focus()));
    }
  }

  function everyone(install: AppInstall, tenancy: AppTenancy, value: boolean) {
    const current = install.grants.find((grant) => grant.tenancyKey === tenancy.key);
    const grant: AppGrant = { tenancyKey: tenancy.key, everyone: value, userIds: current?.userIds ?? [], groupIds: current?.groupIds ?? [] };
    void change(() => setInstalledAppGrants(session, install.installId, [grant]));
  }

  async function saveSettings(next: AppPlatformSettings) {
    setBusy(true);
    setError("");
    try {
      // Its switch or choice already shows what was saved.
      setSettings(await updateAppPlatformSettings(session, next));
      // Who may add apps shows on /apps under this window.
      announceAppsChanged();
    } catch (failure) {
      sayFailed(failure);
    } finally {
      setBusy(false);
    }
  }


  async function decide(request: AppRequest, approve: boolean) {
    setBusy(true);
    setError("");
    try {
      if (approve && !request.installedId) {
        // Not installed yet: its review first; installing it then approves.
        approvingRef.current = request;
        readCatalogApp(request.catalogId, request.appId, request.appName);
        return;
      }
      await decideAppRequest(session, request.id, approve);
      announceAppsChanged();
      toast?.notify(t("apps.saved"), "success");
      void load();
    } catch (failure) {
      approvingRef.current = null;
      sayFailed(failure);
    } finally {
      setBusy(false);
    }
  }

  async function toggleAllowed(app: AppCatalogEntry, on: boolean) {
    const next = allowlist.filter((entry) => entry.appId !== app.id);
    if (on) next.push({ catalogId: app.catalogId, appId: app.id });
    try {
      setAllowlist((await setAppAllowlist(session, next)).apps);
      announceAppsChanged();
    } catch (failure) {
      sayFailed(failure);
    }
  }

  const tenancyName = (tenancy: AppTenancy) => tenancy.name || t("apps.serverTenancy");
  const [choosing, setChoosing] = useState<{ install: AppInstall; tenancy: AppTenancy } | null>(null);
  return (
    <section className={pageStyles.section} aria-labelledby="administrator-apps-title" data-administrator-apps>
      <div className={pageStyles.sectionHeader}>
        <div>
          <div id="administrator-apps-title" className={pageStyles.sectionTitle}>{t("apps.title")}</div>
          <div className={pageStyles.organizationMeta}>{t("apps.meta")}</div>
        </div>
        <div className={pageStyles.sectionActions}>
          <input ref={fileRef} type="file" accept=".leafenapp,application/zip" hidden data-app-file
            onChange={(event) => { const file = event.target.files?.[0]; if (file) void upload(file); }} />
          <button type="button" className={`${pageStyles.secondaryButton} ${styles.headerButton}`} disabled={busy} data-app-browse-open
            onClick={() => setBrowsing(true)}>{t("apps.browse.open")}</button>
          <button ref={installFileRef} type="button" className={`${pageStyles.primaryButton} ${styles.headerButton}`} disabled={busy} data-app-install-file
            onClick={() => fileRef.current?.click()}>{t("apps.installFile")}</button>
        </div>
      </div>
      <AppRequestsPanel requests={requests} busy={busy} onApprove={(request) => void decide(request, true)}
        onDecline={(request) => void decide(request, false)} />
      <div className={rowStyles.memberList} aria-busy={loading}>
        {installs.map((install) => {
          const name = appText(install.name, i18n.language);
          // Only a version later than the one installed now, in Browse apps' words (one read before an update offered it again).
          const newer = updates.find((app) => app.id === install.appId && app.installedVersion === install.version && app.installedChange === "update");
          return (
            <div key={install.installId} className={styles.appRow} data-app-install={install.appId} data-status={install.status}>
              <AppRowIcon className={rowStyles.memberAvatar} name={name} iconUrl={install.iconUrl} />
              <span className={rowStyles.memberText}>
                <strong>{name}</strong>
                <span>{[t("apps.version", { version: install.version }), ...(install.trust === "builtin" ? [] : [t("apps.by", { developer: appDeveloperName(install.developer.name, i18n.language) })]),
                  t(`apps.trust.${install.trust}`)].join(" · ")}</span>
                {install.pausedReason === "budget" ? (
                  <span className={styles.trashNote} data-app-paused="budget">{t("apps.pausedBudget")}</span>
                ) : null}
                <AppDataFailureNotes install={install} />
                {!install.removedAt && (install.status === "stopped" || install.status === "emergency") ? (
                  <span className={styles.trashNote} data-app-stopped-note>{t(`apps.statusNote.${install.status}`)}</span>
                ) : null}
                {install.status === "revoked" ? (
                  <span className={styles.trashNote} data-app-revoked>{t("apps.revokedNote", { reason: install.revokedReason || "—" })}</span>
                ) : null}
                {install.removedAt ? (
                  <span className={styles.trashNote} data-app-trashed>{t("apps.trashed", {
                    date: displayDate(install.purgeAt ?? 0) })}</span>
                ) : null}
              </span>
              {install.removedAt ? (
                <span className={styles.appControls}>
                  <button type="button" className={pageStyles.secondaryButton} disabled={busy} data-app-restore
                    onClick={() => void change(() => restoreInstalledApp(session, install.installId), undefined, t("apps.restoredToast", { name }))}>{t("apps.restore")}</button>
                </span>
              ) : (
              <span className={styles.appControls}>
                {newer ? (
                  <button type="button" className={pageStyles.secondaryButton} disabled={busy} data-app-update={newer.version}
                    title={t("apps.updateTo", { version: newer.version })} onClick={() => readCatalogApp(newer.catalogId, newer.id, newer.name, newer.version)}>{t("apps.browse.action.update", { version: install.version })}</button>
                ) : null}
                <AppSelect value={install.status === "revoked" ? "stopped" : install.status} disabled={busy || install.status === "revoked"}
                  aria-label={t("apps.statusLabel", { name })} menuWidth={280} data-app-status
                  onChange={(event) => void change(() => setInstalledAppStatus(session, install.installId,
                    event.target.value as "active" | "stopped" | "emergency"))}>
                  <option value="active" data-description={t("apps.statusHint.active")}>{t("apps.status.active")}</option>
                  <option value="stopped" data-description={t("apps.statusHint.stopped")}>{t("apps.status.stopped")}</option>
                  <option value="emergency" data-description={t("apps.statusHint.emergency")}>{t("apps.status.emergency")}</option>
                </AppSelect>
                {install.permissions.some((permission) => ["secret", "outsideSignIn", "webhook", "settingSocket", "personCredential"].includes(permission.kind)) ? (
                  <button type="button" className={pageStyles.secondaryButton} disabled={busy} data-app-secrets-open
                    onClick={() => setSecretsFor(install)}>{t("apps.settings.open")}</button>
                ) : null}
                <AppRowMenu label={t("apps.rowMenu", { name })} disabled={busy} items={[
                  ...(install.status === "active"
                    ? [{ key: "open", label: t("apps.openApp"), data: "data-app-open", onSelect: () => window.location.assign(`/apps/${install.slug}`) }] : []),
                  { key: "jobs", label: t("apps.jobs.open"), data: "data-app-jobs-open", onSelect: () => setJobsFor(install) },
                  { key: "usage", label: t("apps.usage.open"), data: "data-app-usage-open", onSelect: () => setUsageFor(install) },
                  ...(install.permissions.some((permission) => permission.kind === "guests")
                    ? [{ key: "guests", label: t("apps.guests.open"), data: "data-app-guests-open", onSelect: () => setGuestsFor(install) }] : []),
                  { key: "remove", label: t("apps.remove"), data: "data-app-remove", danger: true, onSelect: () => void remove(install) },
                ]} />
              </span>
              )}
              {!install.removedAt && !install.grants.some((grant) => grant.everyone || grant.userIds.length || grant.groupIds?.length) ? (
                <span className={styles.nobodyNote} data-app-nobody>{t("apps.nobodyYet")}</span>
              ) : null}
              <div className={styles.grants}>
                <span className={styles.grantsTitle}>{t("apps.users")}</span>
                {tenancies.map((tenancy) => {
                  const grant = install.grants.find((item) => item.tenancyKey === tenancy.key);
                  return (
                    <AppGrantRow key={tenancy.key} tenancyKey={tenancy.key} tenancyName={tenancyName(tenancy)} grant={grant}
                      disabled={busy || Boolean(install.removedAt)} onEveryone={(value) => everyone(install, tenancy, value)}
                      onChoose={() => setChoosing({ install, tenancy })} />
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
      <AdministratorAppsListEnd readFailed={readFailed} onReadAgain={() => { setReadFailed(""); void load(); }} loading={loading} empty={!installs.length} error={error} />
      {/* Below the list once it is read — pushed down as it came before; its switches are that read's. */}
      <div className={styles.policies} data-app-policies style={(loading || readFailed) && !installs.length ? { display: "none" } : undefined}>
        <div className={pageStyles.sectionTitle}>{t("apps.policiesTitle")}</div>
        {/* One thing to a row, switches that save as they turn, as 설정's rows (2026-11-05: checkbox groups). */}
        <div className={pageStyles.settingRows}>
          <SettingSwitch title={t("apps.strict.title")} description={t("apps.strict.body")} on={settings.strictNetwork} disabled={busy || loading}
            onChange={(on) => void saveSettings({ ...settings, strictNetwork: on })} data-app-strict={settings.strictNetwork ? "true" : "false"} />
          {settings.strictNetwork ? null : (
            <AppStrictTenancies session={session} tenancies={tenancies} disabled={busy || loading} onChange={setTenancies} />
          )}
          <SettingSwitch title={t("apps.homeScreen.title")} description={t("apps.homeScreen.body")} on={!settings.homeScreenAppsOff} disabled={busy || loading}
            onChange={(on) => void saveSettings({ ...settings, homeScreenAppsOff: !on })} data-app-home-screen={settings.homeScreenAppsOff ? "false" : "true"} />
          <AppOversightSettings session={session} tenancies={tenancies} disabled={busy || loading} onChange={setTenancies} />
          {/* Where the allowed apps are chosen, on the row that asks for them (2026-11-05: a first administrator looked for it). */}
          <SettingRow title={t("apps.policy.title")} description={settings.installPolicy === "allowlist" ? (
            <span data-app-allowlist-hint>
              <Trans t={t} i18nKey="apps.policy.allowlistHint"
                components={{ browse: <button type="button" className={styles.inlineLink} disabled={busy || loading} onClick={() => setBrowsing(true)} /> }} />
            </span>
          ) : undefined}>
            <AppSelect aria-label={t("apps.policy.title")} value={settings.installPolicy ?? "admins"} disabled={busy || loading} data-app-install-policy menuWidth={320}
              look="setting"
              onChange={(event) => void saveSettings({ ...settings, installPolicy: event.target.value as AppPlatformSettings["installPolicy"] })}>
              {(["admins", "request", "allowlist"] as const).map((policy) => (
                <option key={policy} value={policy}>{t(`apps.policy.${policy}`)}</option>
              ))}
            </AppSelect>
          </SettingRow>
          <SettingSwitch title={t("apps.unsigned.title")} description={t("apps.unsigned.body")} on={!settings.unsignedAppsOff} disabled={busy || loading}
            onChange={(on) => void saveSettings({ ...settings, unsignedAppsOff: !on })} data-app-unsigned={settings.unsignedAppsOff ? "false" : "true"} />
          <SettingRow title={t("apps.packageLimit.title")}>
            <AppSelect aria-label={t("apps.packageLimit.title")} value={settings.maxPackageBytes || appPlatformBounds.packageMaxBytes} disabled={busy || loading} data-app-package-limit
              look="setting"
              onChange={(event) => {
                const bytes = Number(event.target.value);
                void saveSettings({ ...settings, maxPackageBytes: bytes === appPlatformBounds.packageMaxBytes ? 0 : bytes });
              }}>
              {[...new Set([...PACKAGE_LIMITS, settings.maxPackageBytes || appPlatformBounds.packageMaxBytes])].sort((a, b) => a - b).map((bytes) => (
                <option key={bytes} value={bytes}>{formatFileSize(bytes)}</option>
              ))}
            </AppSelect>
          </SettingRow>
        </div>
      </div>
      {choosing ? (
        <AppGrantPeopleDialog session={session} install={choosing.install} tenancy={choosing.tenancy}
          name={appText(choosing.install.name, i18n.language)} onClose={() => setChoosing(null)}
          onSaved={(saved) => { replace(saved); announceAppsChanged(); setChoosing(null); }} />
      ) : null}
      {removal ? (
        <AppRemovalDialog session={session} name={appText(removal.install.name, i18n.language)} preview={removal.preview} busy={busy}
          error={removalError} onRemove={(mode) => void confirmRemoval(mode)} onClose={() => setRemoval(null)} />
      ) : null}
      {usageFor ? (
        <AppUsageDialog session={session} installId={usageFor.installId} name={appText(usageFor.name, i18n.language)} slug={usageFor.slug}
          onClose={() => setUsageFor(null)} />
      ) : null}
      {jobsFor ? (
        <AppJobsDialog session={session} installId={jobsFor.installId} name={appText(jobsFor.name, i18n.language)}
          onClose={() => setJobsFor(null)} />
      ) : null}
      {browsing ? (
        <AppBrowseDialog session={session} reviewing={Boolean(review || packages.reading)}
          // A catalog trusted there may have revoked a version installed here.
          onClose={() => { setBrowsing(false); void load(); }}
          allowed={settings.installPolicy === "allowlist" ? (app) => allowlist.some((entry) => entry.appId === app.id && entry.catalogId === app.catalogId)
            : undefined}
          onToggleAllowed={(app, on) => toggleAllowed(app, on)}
          // The review opens over it: cancelling it comes back to where the list was.
          onRead={(app) => readCatalogApp(app.catalogId, app.id, app.name, app.version)} />
      ) : null}
      {guestsFor ? (
        <AppGuestsDialog session={session} installId={guestsFor.installId} name={appText(guestsFor.name, i18n.language)}
          onClose={() => setGuestsFor(null)} />
      ) : null}
      {secretsFor ? (
        <AppSecretsDialog session={session} installId={secretsFor.installId} name={appText(secretsFor.name, i18n.language)} tenancies={tenancies}
          onClose={() => setSecretsFor(null)} />
      ) : null}
      {review || packages.reading ? (
        <AppPackageReviewDialog review={review} reading={packages.reading} tenancies={tenancies} currentTenancy={currentTenancy} busy={busy}
          inTrash={Boolean(review) && installs.some((install) => install.appId === review?.appId && Boolean(install.removedAt))} error={reviewError}
          onInstall={(acceptDataRemovals, everyone, personCredentials) => void install(acceptDataRemovals, everyone, personCredentials)}
          // Another file is chosen from the file window; a catalog's app is read again.
          onReadAgain={packages.reading?.source === "file" ? () => { packages.stop(); fileRef.current?.click(); } : packages.again}
          returnFocusRef={reviewOpener} onClose={() => { approvingRef.current = null; packages.stop(); setReview(null); }} />
      ) : null}
    </section>
  );
}
