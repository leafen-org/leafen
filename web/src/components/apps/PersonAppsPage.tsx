import { useEffect, useMemo, useRef, useState } from "react";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { useTranslation } from "react-i18next";
import { setDocumentChrome } from "@/lib/documentChrome";
import type { AppConsent, AppPersonCatalog, AppPersonCatalogEntry } from "@/appPlatformWire.generated";
import { appDeveloperName, appText } from "@/components/admin/administratorAppsApi";
import { appCatalogFacts } from "@/components/admin/appPermissionWords";
import { AppInstallFlow } from "@/components/admin/AppInstallFlow";
import { AppRowIcon } from "@/components/admin/AppRowIcon";
import { TopBar } from "@/components/page/TopBar";
import { useProductPages } from "@/components/product/productPagesContext";
import { announceAppsChanged, onAppsChanged } from "@/lib/appsChangedEvents";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { AppConsentDialog } from "./AppConsentDialog";
import { PersonAppCredentials } from "./PersonAppCredentials";
import { PersonAppPasswords } from "./PersonAppPasswords";
import { PersonAppSchedules } from "./PersonAppSchedules";
import { readPersonCatalog, requestApp, selfInstallApp } from "./personAppsApi";
import adminStyles from "@/components/admin/AdministratorApps.module.css";
import rowButtonStyles from "@/components/admin/AdministratorSettingsPage.module.css";
import browseStyles from "@/components/admin/AppBrowseDialog.module.css";
import styles from "./PersonAppsPage.module.css";
import { focusedOpener } from "@/lib/lastPress";

// The list last read in this tab, by the person: shown at once when "앱"
// opens again, the server's answer replacing it (2026-11-15: offline, the
// list read minutes before was gone, the screen empty for 55 s).
let lastCatalog: { userId: string; catalog: AppPersonCatalog } | null = null;

/**
 * "앱" (/apps — Notion has no apps): the apps a person may open,
 * and the apps the server's trusted catalogs and Leafen offer. What else
 * they may do follows the install policy (docs/app-platform.md 3): ask the
 * administrators for one, add one the administrators allowed after agreeing
 * to what it may do, or nothing but tell an administrator.
 */
export default function PersonAppsPage() {
  const { t, i18n } = useTranslation(["common", "serverConsole"]);
  // The tab says the apps, not the page opened before (2026-10-16).
  useEffect(() => { setDocumentChrome({ title: t("appShell:route.apps") }); }, [t]);
  const pages = useProductPages();
  const session = pages?.session;
  const tenancyKey = pages?.organizationId ? `org:${pages.organizationId}` : "";
  const [catalog, setCatalog] = useState<AppPersonCatalog | null>(() => (lastCatalog && lastCatalog.userId === session?.user.id ? lastCatalog.catalog : null));
  // An app just installed here hands the keys to its "열기" once the list
  // shows it usable (2026-11-06: its "설치" went away and the focus with it).
  const focusOpen = useRef<string | null>(null);
  useEffect(() => {
    const appId = focusOpen.current;
    if (!appId || !catalog) return;
    const row = document.querySelector<HTMLElement>(`[data-person-app="${CSS.escape(appId)}"]`);
    const open = row?.querySelector<HTMLElement>("[data-person-app-open]");
    if (open) open.focus();
    if (open || catalog.apps.some((app) => app.id === appId && app.usable)) focusOpen.current = null;
  }, [catalog]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The list couldn't be read because the server didn't answer: it is read again once the server may answer.
  const [unreachable, setUnreachable] = useState(false);
  const [query, setQuery] = useState("");
  const [asking, setAsking] = useState<{ app: AppPersonCatalogEntry; reason: string } | null>(null);
  const [consent, setConsent] = useState<{ app: AppPersonCatalogEntry; consent: AppConsent } | null>(null);
  const consentOpener = useRef<HTMLElement | null>(null);
  const [consentError, setConsentError] = useState("");
  const [installing, setInstalling] = useState<AppPersonCatalogEntry | null>(null);
  const toast = useOptionalProductToast();

  async function load() {
    if (!session) return;
    try {
      const read = await readPersonCatalog(session);
      lastCatalog = { userId: session.user.id, catalog: read };
      setCatalog(read);
      setError("");
      setUnreachable(false);
    } catch (failure) {
      setUnreachable(isServerUnreachable(failure));
      setError(settingsErrorMessage(failure, t("appPeople.readFailed")));
    }
  }

  useEffect(() => {
    void load();
    // Read again when the account changes, or when apps changed in a window over this page.
    return onAppsChanged(() => { void load(); });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);
  // Back online, the page reads the list again instead of staying on "can't reach the server" (2026-11-06).
  useEffect(() => {
    if (!unreachable) return undefined;
    return retryWhenServerMayAnswer(() => { void load(); });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [unreachable, session]);

  const name = (app: AppPersonCatalogEntry) => appText(app.name, i18n.language);
  const shown = useMemo(() => {
    const words = query.trim().toLowerCase();
    return (catalog?.apps ?? []).filter((app) => !words
      || [name(app), appText(app.summary, i18n.language), app.developer, appDeveloperName(app.developer, i18n.language)].join(" ").toLowerCase().includes(words))
      .sort((left, right) => Number(right.usable) - Number(left.usable) || Number(right.featured) - Number(left.featured)
        || name(left).localeCompare(name(right)));
    // The list follows the search and the reader's language.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog, i18n.language, query]);

  async function act(action: () => Promise<void>, fallback: string) {
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (failure) {
      setError(settingsErrorMessage(failure, t(fallback)));
    } finally {
      setBusy(false);
    }
  }

  function ask() {
    if (!session || !asking) return;
    const { app, reason } = asking;
    void act(async () => {
      await requestApp(session, { catalogId: app.catalogId, appId: app.id, tenancyKey, reason });
      setAsking(null);
      await load();
    }, "appPeople.requestFailed");
  }

  function review(app: AppPersonCatalogEntry) {
    if (!session) return;
    // The window opens after the server answers, by when "추가" may have lost
    // the focus; it is given back there when the window closes.
    consentOpener.current = focusedOpener();
    void act(async () => {
      setConsentError("");
      setConsent({ app, consent: await selfInstallApp(session, { catalogId: app.catalogId, appId: app.id, tenancyKey }) });
    }, "appPeople.addFailed");
  }

  async function agree() {
    if (!session || !consent) return;
    setBusy(true);
    setConsentError("");
    try {
      const added = await selfInstallApp(session, {
        catalogId: consent.app.catalogId, appId: consent.app.id, tenancyKey, acceptPermissionsHash: consent.consent.permissionsHash,
      });
      setConsent(null);
      // The switcher and the app's own page see it at once.
      announceAppsChanged();
      pushRoute(`/apps/${added.slug}`);
    } catch (failure) {
      setConsentError(settingsErrorMessage(failure, t("appPeople.addFailed")));
    } finally {
      setBusy(false);
    }
  }

  const policy = catalog?.policy ?? "admins";
  const manager = Boolean(catalog?.canManage);
  const action = (app: AppPersonCatalogEntry) => {
    // An app that only opens files says where it opens instead of a button
    // to a page it doesn't have (2026-11-05).
    if (app.usable && !app.fullPage) return <span className={styles.appMeta} data-person-app-files>{t("appPeople.opensFromFiles")}</span>;
    if (app.usable) {
      return <button type="button" className={rowButtonStyles.secondaryButton} data-person-app-open onClick={() => pushRoute(`/apps/${app.slug}`)}>
        {t("appPeople.open")}</button>;
    }
    // An app of theirs that is stopped or revoked says so in its facts and
    // offers nothing to press, as its page says what to do (2026-11-08: it
    // read "허용되지 않음").
    if (!manager && app.stopped) return null;
    // An administrator installs from here and chooses in its review who may
    // use it (2026-10-16: the first administrator found no install here, and
    // nobody could use what they installed); a later change of mind is in Server › Apps.
    if (manager) {
      // A stopped app is turned on again on its row, as its own page says.
      return app.installed
        ? <button type="button" className={rowButtonStyles.secondaryButton} data-person-app-choose-users={app.stopped ? undefined : true}
          data-person-app-stopped={app.stopped || undefined} data-focus-return={`app-settings:${app.id}`}
          onClick={() => requestSettingsWindow({ section: "server-apps", appRow: app.id })}>
          {t(app.stopped ? "appHost.unavailable.openServerApps" : "appPeople.chooseUsers")}</button>
        : <button type="button" className={rowButtonStyles.secondaryButton} disabled={busy} data-person-app-install
          onClick={() => { setError(""); setInstalling(app); }}>{t("appPeople.install")}</button>;
    }
    if (policy === "allowlist" && app.allowed) {
      if (app.inTrash) return <span className={browseStyles.browseCardSummary} data-person-app-in-trash>{t("appPeople.inTrash")}</span>;
      return <button type="button" className={rowButtonStyles.secondaryButton} disabled={busy} data-person-app-add onClick={() => review(app)}>
        {t("appPeople.add")}</button>;
    }
    // One the administrators haven't allowed says why it has no "추가"
    // (2026-11-06: rows without a button left people guessing).
    if (policy === "allowlist") return <span className={styles.appMeta} data-person-app-not-allowed>{t("appPeople.notAllowed")}</span>;
    if (policy === "request") {
      if (app.request === "pending") {
        return <button type="button" className={rowButtonStyles.secondaryButton} disabled data-person-app-requested>{t("appPeople.requested")}</button>;
      }
      return <button type="button" className={rowButtonStyles.secondaryButton} disabled={busy} data-person-app-request
        onClick={() => setAsking({ app, reason: "" })}>{t(app.request === "declined" ? "appPeople.requestAgain" : "appPeople.request")}</button>;
    }
    return null;
  };

  return (
    <>
    {/* As every screen's: the sidebar button on a phone and when the sidebar is closed. */}
    <TopBar title={t("appPeople.title")} />
    <div className={styles.page} data-person-apps>
      <h1 className={styles.title}>{t("appPeople.title")}</h1>
      {/* Who may add apps is known only once the catalog says; guessing "admins"
          first showed a manager the members' line while it loaded. */}
      {/* Its line is kept while the list loads, so the search doesn't move down under the cursor. */}
      <p className={styles.note} data-person-apps-policy={catalog ? manager ? "manager" : policy : undefined} aria-hidden={catalog ? undefined : true}>
        {catalog ? t(manager ? "appPeople.policy.manager" : `appPeople.policy.${policy}`) : "\u00a0"}</p>
      <input className={`${browseStyles.browseSearch} ${styles.search}`} type="search" value={query} placeholder={t("appPeople.search")}
        aria-label={t("appPeople.search")} onChange={(event) => setQuery(event.target.value)} />
      {error ? <div className={adminStyles.error} role="alert">{error}</div> : null}
      {catalog ? (
        shown.length ? (
          <div data-person-app-list>
            {[
              { key: "usable", title: t("appPeople.sections.usable"), apps: shown.filter((app) => app.usable || (!manager && app.stopped)) },
              // An administrator's installed apps they can't use yet are not
              // among those to install (2026-11-06: mail, installed for nobody,
              // stood under "설치할 수 있는 앱" with "사용할 사람 정하기").
              ...(manager ? [
                { key: "installed", title: t("appPeople.sections.installed"), apps: shown.filter((app) => !app.usable && app.installed) },
                { key: "others", title: t("appPeople.sections.install"), apps: shown.filter((app) => !app.usable && !app.installed) },
              // A member's own app that is stopped stays among theirs.
              ] : [{ key: "others", title: t("appPeople.sections.others"), apps: shown.filter((app) => !app.usable && !app.stopped) }]),
            ].filter((section) => section.apps.length).map((section) => (
              <section key={section.key} className={styles.section}>
                <h2 className={styles.sectionTitle}>{section.title}</h2>
                <ul className={styles.apps}>
                  {section.apps.map((app) => (
                    <li key={`${app.catalogId}/${app.id}`} className={styles.app} data-person-app={app.id}>
                      <AppRowIcon tile name={name(app)} iconUrl={app.iconUrl} />
                      <div className={styles.appText}>
                        <span className={styles.appName}>{name(app)}</span>
                        <span className={styles.appSummary}>{appText(app.summary, i18n.language)}</span>
                        <span className={styles.appMeta}>{[
                          ...(app.catalogId === "builtin" ? [t("serverConsole:apps.trust.builtin")]
                            // The official catalog (its row is "official" on every server) by kind, not its name again.
                            : [t("serverConsole:apps.by", { developer: appDeveloperName(app.developer, i18n.language) }),
                              ...(app.catalogId === "official" ? [t("serverConsole:apps.catalogs.kind.official")] : app.catalogName ? [app.catalogName] : [])]),
                          ...appCatalogFacts({ ...app, risk: undefined }, (key, values) => t(`serverConsole:${key}`, values)),
                          ...(app.stopped ? [t(`serverConsole:apps.status.${app.stopped}`)] : []),
                        ].join(" · ")}</span>
                        {asking?.app.id === app.id && asking.app.catalogId === app.catalogId ? (
                          <form className={styles.ask} onSubmit={(event) => { event.preventDefault(); ask(); }} data-person-app-ask>
                            <textarea autoFocus value={asking.reason} rows={2} maxLength={300} placeholder={t("appPeople.reason")} aria-label={t("appPeople.reason")}
                              onChange={(event) => setAsking({ app, reason: event.target.value })} />
                            <span className={styles.askActions}>
                              <button type="button" className={adminStyles.secondary} disabled={busy} onClick={() => setAsking(null)}>
                                {t("common:actions.cancel")}</button>
                              <button type="submit" className={adminStyles.primary} disabled={busy} data-person-app-send>{t("appPeople.send")}</button>
                            </span>
                          </form>
                        ) : null}
                      </div>
                      {asking?.app.id === app.id && asking.app.catalogId === app.catalogId ? null : <div className={styles.appAction}>{action(app)}</div>}
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
        ) : <p className={styles.note}>{t(catalog.apps.length ? "appPeople.none" : "appPeople.empty")}</p>
      ) : error ? null : <p className={`${styles.note} ${styles.loading}`} role="status" data-person-apps-loading>{t("appPeople.loading")}</p>}
      {/* After the list, so it doesn't push them down as it comes. */}
      {session && catalog ? <PersonAppCredentials session={session} /> : null}
      {session && catalog ? <PersonAppPasswords session={session} ready /> : null}
      {session && catalog ? <PersonAppSchedules session={session} /> : null}
      {installing && session ? (
        <AppInstallFlow session={session} catalogId={installing.catalogId} appId={installing.id} name={installing.name}
          currentTenancy={tenancyKey || undefined}
          onInstalled={(installedName) => {
            focusOpen.current = installing.id;
            setInstalling(null);
            announceAppsChanged();
            toast?.notify(t("appPeople.installed", { name: installedName }), "success");
          }}
          onClose={() => setInstalling(null)} />
      ) : null}
      {consent ? (
        <AppConsentDialog consent={consent.consent} again={false} busy={busy} error={consentError} onAgree={() => void agree()} returnFocusRef={consentOpener}
          onClose={() => setConsent(null)} />
      ) : null}
    </div>
    </>
  );
}
