import { FormEvent, useEffect, useRef, useState } from "react";
import { useWaitingNote } from "@/lib/pendingChoice";
import { isUncertainWrite as isUncertainWriteError } from "@/lib/uncertainWrites";
import { isWriteUncertainText, userErrorText, writeUncertainMark } from "@/lib/settingsErrors";
import { useSettingsUnsaved } from "@/components/settings/settingsUnsaved";
import { AdministratorServerIdentity } from "./AdministratorServerIdentity";
import { AdministratorServerUsage } from "./AdministratorServerUsage";
import { BackupOffLine } from "./BackupOffLine";
import { AdministratorServerBuild } from "./AdministratorServerBuild";
import { SettingSwitch } from "./AdministratorSettingRows";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import type {
  AdministratorSettings,
  AdministratorSettingsInput,
} from "../../administratorWire.generated";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import { refreshRuntimeConfig } from "../../lib/runtimeConfigService";
import { useNoticeFor } from "@/lib/useNoticeFor";
import type { Session } from "../../types";
import { AdministratorApps } from "./AdministratorApps";
import { AppPlatformMissingNotice } from "@/components/apps/AppPlatformMissing";
import { useAppPlatformMissing } from "@/lib/appPlatformAvailability";
import { AdministratorAuditLog } from "./AdministratorAuditLog";
import { AdministratorRequestLog } from "./AdministratorRequestLog";
import { AdministratorNetworkProxy } from "./AdministratorNetworkProxy";
import { AdministratorDataManagement } from "./AdministratorDataManagement";
import { AdministratorPlaceSearchSettings } from "./AdministratorPlaceSearchSettings";
import { AdministratorMapSettings } from "./AdministratorMapSettings";
import { AdministratorEmailSettings, type SmtpFieldError } from "./AdministratorEmailSettings";
import { AdministratorSignInSettings, type ProviderField } from "./AdministratorSignInSettings";
import { AdministratorUpdateSettings } from "./AdministratorUpdateSettings";
import { AdministratorTunnel } from "./AdministratorTunnel";
import { AdministratorUserManagement } from "./AdministratorUserManagement";
import { AdministratorWorkspaceList } from "./AdministratorWorkspaceList";
import { PersonSteps } from "./PersonSteps";
import styles from "./AdministratorSettingsPage.module.css";
import { LeafenApiError } from "@/apiTransport";
import { administratorErrorCodes } from "@/administratorWire.generated";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { SettingsSaveBar } from "./SettingsSaveBar";
import { authErrorCodes } from "@/authWire.generated";

/** The server's sections of the settings window, for its administrators. */
/** The server's apps, or on a phone's own server and the App Store edition why it has none. */
function ServerApps({ session, appInstall, appRow, currentTenancy }: { session: Session; appInstall?: string; appRow?: string; currentTenancy?: string }) {
  const { t } = useTranslation("serverConsole");
  const missing = useAppPlatformMissing();
  if (!missing) return <AdministratorApps session={session} appInstall={appInstall} appRow={appRow} currentTenancy={currentTenancy} />;
  return (
    <section className={styles.section} aria-labelledby="administrator-apps-title" data-administrator-apps-missing>
      <div id="administrator-apps-title" className={styles.sectionTitle}>{t("apps.title")}</div>
      <AppPlatformMissingNotice missing={missing} session={session} />
    </section>
  );
}

export type AdministratorSettingsSection = "general" | "users" | "workspaces" | "apps" | "audit" | "email" | "providers" | "tunnel" | "map" | "updates" | "data" | "standby" | "restore";

export function AdministratorSettingsContent({
  section,
  session,
  initialSettings,
  onSettingsChange,
  appInstall,
  appRow,
  currentTenancy,
  onSelectSection,
}: {
  section: AdministratorSettingsSection;
  session: Session;
  initialSettings: AdministratorSettings;
  onSettingsChange: (settings: AdministratorSettings) => void;
  /** A catalog app whose review Server › Apps opens ("catalog/app"). */
  appInstall?: string;
  /** An installed app whose row Server › Apps opens at ("사용할 사람 정하기" on the apps page). */
  appRow?: string;
  /** The organization the settings were opened in ("org:<id>"): an install lets its people use the app at first. */
  currentTenancy?: string;
  /** Goes to another of the server's pages: backups, its standby, restoring. */
  onSelectSection?: (section: AdministratorSettingsSection) => void;
}) {
  const { t: tc } = useTranslation("serverConsole");
  const [settings, setSettings] = useState(initialSettings);
  const [draft, setDraft] = useState<AdministratorSettingsInput>(() => settingsDraft(initialSettings));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // What an agent left to the person here and they finished: the section reads again.
  const [stepsDone, setStepsDone] = useState(0);
  // The mail field the server says is missing, shown under it.
  const [smtpFieldError, setSmtpFieldError] = useState<SmtpFieldError | null>(null);
  const [issuerError, setIssuerError] = useState("");
  const [providerFieldError, setProviderFieldError] = useState<{ field: ProviderField; message: string } | null>(null);
  // "Saved" belongs to the section it was saved in.
  const [notice, setNotice] = useNoticeFor<string>(section);
  // What isn't saved yet: the save bar says so, and leaving settings asks.
  const dirty = JSON.stringify(cleanSettingsDraft(draft)) !== JSON.stringify(cleanSettingsDraft(settingsDraft(settings)));
  useSettingsUnsaved(dirty, (section) => section.startsWith("server-"));
  // A save's refusal is about what was to be saved: once nothing is left
  // unsaved, it goes, on every server section (2026-11-06: "이메일 인증을
  // 요구하려면 먼저 SMTP를 켜세요" stayed after SMTP was turned back on, and
  // followed to 가입 및 로그인). One whose answer didn't come stays.
  useEffect(() => {
    if (!dirty) setError((current) => (isWriteUncertainText(current) ? current : ""));
  }, [dirty]);
  // A save the server hasn't answered in ten seconds says so in the bar.
  const waitingNote = useWaitingNote(busy);
  // The window's settings read again — a change whose answer didn't come may
  // have been taken after all — are what this shows, unless a change made
  // here isn't saved yet.
  const shownFrom = useRef(initialSettings);
  useEffect(() => {
    if (initialSettings === shownFrom.current) return;
    shownFrom.current = initialSettings;
    // A save whose answer didn't come and that the server holds is saved;
    // one it doesn't hold stays as the change not saved, now known as such.
    const held = JSON.stringify(cleanSettingsDraft(settingsDraft(initialSettings))) === JSON.stringify(cleanSettingsDraft(draft));
    if (dirty && !held) {
      setError((current) => (isWriteUncertainText(current) ? "" : current));
      return;
    }
    setError((current) => (isWriteUncertainText(current) ? "" : current));
    setSettings(initialSettings);
    setDraft(settingsDraft(initialSettings));
  }, [initialSettings]);

  async function refresh() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const loaded = await api.administratorSettings(session);
      // What was changed here and not saved stays over what is read again;
      // a field the other change took too shows its new value, and the
      // notice gives what was typed here (2026-11-06: reading again after
      // another administrator's save dropped the contact being typed).
      const { merged, clashes } = mergeDraft(settingsDraft(settings), draft, settingsDraft(loaded));
      setSettings(loaded);
      setDraft(merged);
      onSettingsChange(loaded);
      if (clashes.shown.length) setNotice(tc("shell.reloadClashes", { values: clashes.shown.map((value) => `“${value}”`).join(", ") }));
      else if (clashes.hidden) setNotice(tc("shell.reloadClashesHidden"));
    } catch (caught) {
      setError(messageFor(
        caught,
        "administratorSettings.read",
      ));
    } finally {
      setBusy(false);
    }
  }

  // The return address answer leaves the draft as it is: none of it is in it.
  function applyProviderReturn(next: AdministratorSettings) {
    setSettings(next);
    onSettingsChange(next);
  }

  // The read log's switch saves as it is turned, with the rest as saved.
  async function saveReadLog(enabled: boolean) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const saved = await api.saveAdministratorSettings(session, cleanSettingsDraft({ ...settingsDraft(settings), readLog: { enabled } }));
      setSettings(saved);
      setDraft(settingsDraft(saved));
      onSettingsChange(saved);
      setNotice(tc("shell.saved"));
    } catch (caught) {
      setError(messageFor(caught, "administratorSettings.save"));
    } finally {
      setBusy(false);
    }
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const saved = await api.saveAdministratorSettings(session, cleanSettingsDraft(draft));
      setSettings(saved);
      setDraft(settingsDraft(saved));
      onSettingsChange(saved);
      void refreshRuntimeConfig();
      setNotice(tc("shell.saved"));
    } catch (caught) {
      const code = caught instanceof LeafenApiError ? caught.code : "";
      if (code === administratorErrorCodes.smtpHostRequired) setSmtpFieldError({ field: "host", message: tc("email.hostRequired") });
      else if (code === administratorErrorCodes.smtpSenderInvalid) setSmtpFieldError({ field: "senderAddress", message: tc("email.senderInvalid") });
      else if (code === administratorErrorCodes.smtpPasswordRequired) setSmtpFieldError({ field: "password", message: tc("email.passwordRequired") });
      else if (code === administratorErrorCodes.emailVerificationNeedsSmtp) setError(tc("email.verificationNeedsSmtp"));
      else if (providerFieldOfCode[code]) setProviderFieldError({ field: providerFieldOfCode[code], message: tc(`providers.${providerFieldOfCode[code]}Required`) });
      else if (code === administratorErrorCodes.oidcIssuerInvalid) setIssuerError(tc("providers.issuerRequired"));
      // An issuer that doesn't answer with usable OpenID settings isn't saved.
      else if (code === authErrorCodes.providerUnavailable || code === authErrorCodes.providerResponseInvalid) setIssuerError(tc("providers.issuerUnusable"));
      else setError(messageFor(
        caught,
        "administratorSettings.save",
      ));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.adminConsole} data-surface="server-admin">
      <PersonSteps session={session} section={section} onDone={() => { setStepsDone((done) => done + 1); void refresh(); }} />
      {section === "data" || section === "standby" || section === "restore" ? (
        <AdministratorDataManagement key={`${session.user.id}:${stepsDone}`} session={session} part={section} onSelectSection={onSelectSection} />
      ) : section === "users" ? (
        <AdministratorUserManagement key={`${session.user.id}:${stepsDone}`} session={session} />
      ) : section === "workspaces" ? (
        <AdministratorWorkspaceList key={session.user.id} session={session} />
      ) : section === "apps" ? (
        <ServerApps key={`${session.user.id}:${stepsDone}`} session={session} appInstall={appInstall} appRow={appRow} currentTenancy={currentTenancy} />
      ) : section === "audit" ? (
        <>
          <AdministratorAuditLog key={session.user.id} session={session} />
          <AdministratorRequestLog key={`log-${session.user.id}`} session={session} />
          {/* The server's read log, kept for every read unless turned off here (Leafen's own). */}
          <section className={styles.section} aria-labelledby="administrator-read-log-title" data-administrator-read-log={settings.readLog.enabled}>
            <div className={styles.sectionHeader}>
              <div><SettingsSectionTitle id="administrator-read-log-title" className={styles.sectionTitle}>{tc("readLog.title")}</SettingsSectionTitle></div>
            </div>
            <div className={styles.settingRows}>
              <SettingSwitch title={tc("readLog.keep")} description={tc("readLog.meta")} on={settings.readLog.enabled} disabled={busy}
                onChange={(enabled) => void saveReadLog(enabled)} />
              {error ? <p className={styles.settingNote} role="alert" {...writeUncertainMark(error)}>{error}</p> : null}
            </div>
          </section>
        </>
      ) : section === "tunnel" ? (
        <>
          <AdministratorTunnel key={session.user.id} session={session} />
          <AdministratorNetworkProxy key={`proxy-${session.user.id}:${stepsDone}`} session={session} />
        </>
      ) : (
      <form onSubmit={(event) => void save(event)}>
        {section === "email" ? (
          <AdministratorEmailSettings session={session} settings={settings} smtp={draft.smtp} busy={busy} fieldError={smtpFieldError} onChange={updateSMTP}
            saved={JSON.stringify(draft.smtp) === JSON.stringify(settingsDraft(settings).smtp)}
            about={JSON.stringify([settingsDraft(settings).smtp, draft.smtp])} />
        ) : section === "general" ? (<>
          <BackupOffLine session={session} place="settings" onTurnOn={onSelectSection ? () => onSelectSection("data") : undefined} />
          <AdministratorServerIdentity serverId={settings.serverId} name={draft.serverName ?? ""} alias={settings.serverAlias} busy={busy}
            onNameChange={(serverName) => setDraft((current) => ({ ...current, serverName }))} />
          <AdministratorServerUsage key={session.user.id} session={session} />
        </>) : section === "updates" ? (<>
          <AdministratorUpdateSettings
            status={settings.updates}
            value={draft.updates ?? { enabled: settings.updates.enabled }}
            busy={busy}
            onChange={(change) => setDraft((current) => ({
              ...current,
              updates: { ...(current.updates ?? { enabled: settings.updates.enabled }), ...change },
            }))}
          />
          <AdministratorServerBuild key={session.user.id} session={session} />
        </>) : section === "map" ? (
          <AdministratorMapSettings
            searchSettings={<AdministratorPlaceSearchSettings value={draft.placeSearch??placeSearchDraft(settings)} status={settings.placeSearch} busy={busy} onChange={change=>setDraft(current=>({...current,placeSearch:{...(current.placeSearch??placeSearchDraft(settings)),...change}}))}/>}
            value={draft.map ?? mapDraft(settings)}
            environmentDisabled={settings.map.environmentDisabled}
            busy={busy}
            onChange={(change) => setDraft((current) => ({
              ...current,
              map: { ...(current.map ?? mapDraft(settings)), ...change },
            }))}
          />
        ) : (
          <AdministratorSignInSettings session={session} settings={settings} draft={draft} busy={busy}
            providerError={providerFieldError} issuerError={issuerError}
            onDraft={(change) => setDraft((current) => ({ ...current, ...change }))}
            onGoogle={updateGoogle} onOIDC={updateOIDC} onProviderReturn={applyProviderReturn} />
        )}
        {/* It comes up when there is something to say or save; a refusal can start over from what's saved. */}
        {dirty || error || notice ? (
          <SettingsSaveBar>
            <span aria-live="polite" data-settings-unsaved={dirty && !error ? "true" : undefined} {...writeUncertainMark(error)}>
              {error || (busy && waitingNote ? waitingNote : dirty ? tc("shell.unsaved") : notice)}</span>
            {error ? <button type="button" className={styles.secondaryButton} onClick={() => void refresh()} disabled={busy}>{tc("shell.reload")}</button> : null}
            {dirty ? <button type="submit" className={styles.primaryButton} disabled={busy}>{busy ? tc("shell.saving") : tc("shell.saveChanges")}</button> : null}
          </SettingsSaveBar>
        ) : null}
      </form>
      )}
      {/* The Git server's answers take effect at once, so it sits past the saved settings. */}
    </div>
  );

  function updateSMTP(change: Partial<AdministratorSettingsInput["smtp"]>) {
    if (smtpFieldError && (smtpFieldError.field in change || (smtpFieldError.field === "password" && "username" in change))) setSmtpFieldError(null);
    setDraft((current) => current ? { ...current, smtp: { ...current.smtp, ...change } } : current);
  }
  function updateGoogle(change: Partial<AdministratorSettingsInput["providers"]["google"]>) {
    setProviderFieldError(null);
    setDraft((current) => current ? { ...current, providers: { ...current.providers, google: { ...current.providers.google, ...change } } } : current);
  }
  function updateOIDC(change: Partial<AdministratorSettingsInput["providers"]["oidc"]>) {
    setProviderFieldError(null);
    if ("issuer" in change || "enabled" in change) setIssuerError("");
    setDraft((current) => current ? { ...current, providers: { ...current.providers, oidc: { ...current.providers.oidc, ...change } } } : current);
  }
}

/** The sign-in provider field a refused save is about. */
const providerFieldOfCode: Record<string, ProviderField> = {
  [administratorErrorCodes.googleClientIdRequired]: "googleClientId",
  [administratorErrorCodes.googleSecretRequired]: "googleSecret",
  [administratorErrorCodes.oidcNameRequired]: "oidcName",
  [administratorErrorCodes.oidcClientIdRequired]: "oidcClientId",
  [administratorErrorCodes.oidcSecretRequired]: "oidcSecret",
  [administratorErrorCodes.oidcScopesInvalid]: "oidcScopes",
};

function settingsDraft(settings: AdministratorSettings): AdministratorSettingsInput {
  return {
    version: settings.version,
    revision: settings.revision,
    signupPolicy: settings.signupPolicy,
    signupContact: settings.signupContact,
    serverName: settings.serverName,
    memberAddPolicy: settings.memberAddPolicy,
    smtp: { ...settings.smtp, password: null, clearPassword: false },
    // Only what is saved: what the server shows about a provider (its
    // return address, one it had before) isn't the draft's.
    providers: {
      google: { enabled: settings.providers.google.enabled, clientId: settings.providers.google.clientId, clientSecret: null, clearSecret: false },
      oidc: {
        enabled: settings.providers.oidc.enabled, name: settings.providers.oidc.name, issuer: settings.providers.oidc.issuer,
        clientId: settings.providers.oidc.clientId, scopes: settings.providers.oidc.scopes, clientSecret: null, clearSecret: false,
      },
    },
    map: mapDraft(settings),
    placeSearch:placeSearchDraft(settings),
    updates: { enabled: settings.updates.enabled },
    switcherLists: { enabled: settings.switcherLists.enabled },
    readLog: { enabled: settings.readLog.enabled },
  };
}

function placeSearchDraft(settings:AdministratorSettings): NonNullable<AdministratorSettingsInput["placeSearch"]> {
 return {enabled:settings.placeSearch.enabled,provider:settings.placeSearch.provider,endpoint:"",apiKey:"",apiKeyId:"",clearConfig:false};
}

function mapDraft(settings: AdministratorSettings): NonNullable<AdministratorSettingsInput["map"]> {
  const { enabled, tileUrl, attribution, attributionUrl } = settings.map;
  return { enabled, tileUrl, attribution, attributionUrl };
}

// A secret typed here is never repeated on the screen.
const SECRET_FIELD = /password|secret|apikey/iu;

/**
 * Field by field: what was changed here (mine, from base) over what the
 * server has now (theirs). Where both changed a field differently, theirs is
 * shown and what was typed here is given back to say.
 */
function mergeDraft<T>(base: T, mine: T, theirs: T): { merged: T; clashes: { shown: string[]; hidden: boolean } } {
  const clashes = { shown: [] as string[], hidden: false };
  const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
  const plain = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null && !Array.isArray(value);
  const merge = (at: string, b: unknown, m: unknown, th: unknown): unknown => {
    if (plain(b) && plain(m) && plain(th)) {
      const out: Record<string, unknown> = {};
      for (const key of new Set([...Object.keys(th), ...Object.keys(m)])) out[key] = merge(key, b[key], m[key], th[key]);
      return out;
    }
    if (same(m, b) || same(m, th)) return th;
    if (same(th, b)) return m;
    if (SECRET_FIELD.test(at)) clashes.hidden = true;
    else if (typeof m === "string" && m.trim()) clashes.shown.push(m.trim());
    else clashes.hidden = true;
    return th;
  };
  return { merged: merge("", base, mine, theirs) as T, clashes };
}

function cleanSettingsDraft(draft: AdministratorSettingsInput): AdministratorSettingsInput {
  const nullable = (value: string | null) => value?.trim() ? value : null;
  return {
    ...draft,
    smtp: { ...draft.smtp, password: nullable(draft.smtp.password) },
    providers: {
      google: { ...draft.providers.google, clientSecret: nullable(draft.providers.google.clientSecret) },
      oidc: { ...draft.providers.oidc, clientSecret: nullable(draft.providers.oidc.clientSecret) },
    },
  };
}

function messageFor(
  error: unknown,
  context: "administratorSettings.read" | "administratorSettings.save",
): string {
  // A save sent without the server's answer is said as the other settings
  // say it: not known whether saved, read again once the server answers.
  if (isUncertainWriteError(error)) return userErrorText(error, "");
  return productErrorMessage(error, context);
}
