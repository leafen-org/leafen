import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { appRegistrationOperations, type AppRegistration, type AppRegistrationListResponse } from "@/appRegistrationWire.generated";
import { AppSelect } from "@/components/AppSelect";
import { TopBar } from "@/components/page/TopBar";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { ConnectionsIcon, Copy, DeveloperIcon } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { setDocumentChrome } from "@/lib/documentChrome";
import { developerToolsHref, pushRoute } from "@/lib/router";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { ConnectionMark } from "./ConnectionIcon";
import { InternalConnectionPage } from "./InternalConnectionPage";
import { askDeveloperDelete } from "./DeveloperQuestions";
import { DeveloperDangerZone } from "./DeveloperDangerZone";
import styles from "./DeveloperTools.module.css";
import portal from "./DeveloperSelect.module.css";

/**
 * A connection's own page, as Notion's /developers/connections/〈id〉 (seen
 * 2026-10-30): the path "개발자 도구 / 연결 / 〈name〉", its name as the
 * title, then "구성" — "정보 표시" with its tile and "연결 이름", the
 * connection's OAuth client (Leafen's connections are apps a person
 * authorizes, not Notion's internal API tokens), and "위험 구역" with
 * "삭제하기". A field keeps what was typed when it is left.
 */
export function DeveloperConnectionPage({ session, connectionId }: { session: Session; connectionId: string }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [app, setApp] = useState<AppRegistration | null | undefined>(undefined);
  const [error, setError] = useState("");
  useEffect(() => {
    let current = true;
    void request<AppRegistrationListResponse>(appRegistrationOperations.list.path, { method: appRegistrationOperations.list.method }, session)
      .then((listed) => { if (current) setApp(listed.apps.find((candidate) => candidate.id === connectionId) ?? null); })
      .catch((failure) => { if (current) { setApp(null); setError(settingsErrorMessage(failure, t("appLoadFailed"))); } });
    return () => { current = false; };
  }, [connectionId, session, t]);
  useEffect(() => { if (app) setDocumentChrome({ title: app.name }); }, [app]);

  async function save(change: Partial<Pick<AppRegistration, "name" | "redirectUris" | "actsAs">>) {
    if (!app) return;
    const next = { ...app, ...change };
    setApp(next);
    setError("");
    try {
      await request<AppRegistration>(appRegistrationOperations.update.path.replace("{appId}", encodeURIComponent(app.id)), {
        method: appRegistrationOperations.update.method,
        body: JSON.stringify({ name: next.name, redirectUris: next.redirectUris, actsAs: next.actsAs }),
      }, session);
    } catch (failure) {
      setApp(app);
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  // Deleting it ends every connection made with it at once: asked first,
  // as Notion's developer tools ask before a connection goes.
  async function remove() {
    if (!app || !await askDeveloperDelete()) return;
    try {
      await request<void>(appRegistrationOperations.remove.path.replace("{appId}", encodeURIComponent(app.id)),
        { method: appRegistrationOperations.remove.method }, session);
      pushRoute(developerToolsHref("connections"));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }

  const name = app?.name ?? "";
  // Not an app: an internal connection of the workspace, on its own page.
  if (app === null && !error) return <InternalConnectionPage session={session} connectionId={connectionId} />;
  return (
    <>
      <TopBar title={name || t("developerTools.title")} crumbs={<DeveloperConnectionCrumbs name={name} />} />
      <main className={styles.page} data-developer-connection={connectionId}>
        {app === null ? <p className={styles.description}>{error || t("developerTools.connection.missing")}</p> : app ? <>
          <h1 className={styles.title}>{app.name}</h1>
          <nav className={styles.tabs} role="tablist" aria-label={app.name}>
            <button type="button" role="tab" aria-selected="true" className={styles.tab}>{t("developerTools.connection.config")}</button>
          </nav>
          <section className={styles.card} data-connection-card="display">
            <h2 className={styles.cardTitle}>{t("developerTools.connection.display")}</h2>
            <div className={styles.fieldGrid}>
              <div className={styles.displayRow}>
                <ConnectionMark name={app.name} size="display" />
                <label className={styles.cardField}>
                  <span>{t("developerTools.connection.name")}</span>
                  <input defaultValue={app.name} maxLength={200} data-connection-name
                    onBlur={(event) => { const value = event.currentTarget.value.trim(); if (value && value !== app.name) void save({ name: value }); }} />
                </label>
              </div>
            </div>
          </section>
          <section className={styles.card} data-connection-card="oauth">
            <h2 className={styles.cardTitle}>{t("developerTools.connection.oauth")}</h2>
            <p className={styles.description}>{t("appsDescription")}</p>
            <label className={styles.cardField}>
              <span>{t("developerTools.connection.clientId")}</span>
              <span className={styles.secretRow}>
                <code>{app.id}</code>
                <button type="button" className={styles.icon} aria-label={t("developerTools.connection.copy")} title={t("developerTools.connection.copy")}
                  onClick={() => void copyText(app.id)}><Copy size="small" aria-hidden="true" /></button>
              </span>
            </label>
            <label className={styles.cardField}>
              <span>{t("appRedirects")}</span>
              <input defaultValue={app.redirectUris.join(" ")} placeholder="https://example.com/callback" data-connection-redirects
                onBlur={(event) => {
                  const redirectUris = event.currentTarget.value.split(/\s+/u).filter(Boolean);
                  if (redirectUris.length && redirectUris.join(" ") !== app.redirectUris.join(" ")) void save({ redirectUris });
                }} />
            </label>
            <label className={styles.cardField}>
              <span>{t("appActsAs")}</span>
              <AppSelect aria-label={t("appActsAs")} value={app.actsAs}
                className={portal.select} settingsList menuWidth={200} menuGap={0} menuAlign="start"
                onChange={(event) => void save({ actsAs: event.target.value as AppRegistration["actsAs"] })}>
                <option value="person">{t("appActsAsPerson")}</option>
                <option value="integration">{t("appActsAsIntegration")}</option>
              </AppSelect>
            </label>
          </section>
          <DeveloperDangerZone kind="connection" description={t("developerTools.connection.dangerDescription")} onRemove={() => void remove()} />
          {error ? <p className={styles.error} role="alert">{error}</p> : null}
        </> : null}
      </main>
    </>
  );
}

/** "개발자 도구 / 연결 / 〈name〉" over a connection's page. */
export function DeveloperConnectionCrumbs({ name }: { name: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <>
      <span className={topBarStyles.crumbWrap}>
        <button type="button" className={topBarStyles.crumb} onClick={() => pushRoute(developerToolsHref("start"))}>
          <span className={topBarStyles.crumbIcon}><DeveloperIcon size="small" aria-hidden="true" /></span>
          <span className={topBarStyles.crumbLabel}>{t("developerTools.title")}</span>
        </button>
      </span>
      <span className={styles.crumbSep} aria-hidden="true">/</span>
      <span className={topBarStyles.crumbWrap}>
        <button type="button" className={topBarStyles.crumb} onClick={() => pushRoute(developerToolsHref("connections"))}>
          <span className={topBarStyles.crumbIcon}><ConnectionsIcon size="small" aria-hidden="true" /></span>
          <span className={topBarStyles.crumbLabel}>{t("developerTools.tabs.connections")}</span>
        </button>
      </span>
      <span className={styles.crumbSep} aria-hidden="true">/</span>
      <span className={topBarStyles.crumbWrap}>
        <span className={topBarStyles.crumb} data-topbar-current-page><span className={topBarStyles.crumbLabel}>{name}</span></span>
      </span>
    </>
  );
}
