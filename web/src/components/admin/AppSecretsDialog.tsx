import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppAdminCredentialList, AppHook, AppHookSecret, AppOAuthReturn, AppSecret, AppSetting, AppTenancy } from "@/appPlatformWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { confirmAction } from "../ConfirmActionDialog";
import { AppHookOrganizations } from "./AppHookOrganizations";
import {
  appText, clearAppSecret, listAppAdminCredentials, listAppHooks, listAppSecrets, listAppSettings, makeAppHookSecret, revokeAppCredentials,
  setAppCredentialsPolicy, setAppOAuthReturn, setAppSecret, setAppSetting,
} from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import credentialStyles from "./AppCredentialsSection.module.css";
import { useOptionalProductToast } from "@/lib/productToast";
import { displayDateTime } from "@/lib/displayDate";
import { SettingSwitch } from "./AdministratorSettingRows";
import { ReadFailed } from "../ReadFailed";

/**
 * An app's secrets (docs/app-platform.md 8): each one its relay addresses
 * name, set or empty, shown back only by its last four characters. The
 * relay fills them into the app's requests; the app never sees them. Its
 * incoming hooks take calls once a secret is made, shown only that once.
 * The addresses the administrator sets — a mail server's — come first: the
 * app's sockets open there and nowhere else. Then whether people may give
 * the app their own credentials, how many have, and taking them all back.
 */
export function AppSecretsDialog({ session, installId, name, tenancies = [], onClose }: {
  session: Session;
  installId: string;
  name: string;
  tenancies?: AppTenancy[];
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [secrets, setSecrets] = useState<AppSecret[]>([]);
  const [settings, setSettings] = useState<AppSetting[]>([]);
  const [addresses, setAddresses] = useState<Record<string, string>>({});
  const [callback, setCallback] = useState("");
  // After an address change: the sign-ins whose provider has the earlier
  // return address (app_platform_address_change.go).
  const [returns, setReturns] = useState<AppOAuthReturn[]>([]);
  const [hooks, setHooks] = useState<AppHook[]>([]);
  const [made, setMade] = useState<AppHookSecret | null>(null);
  const [credentials, setCredentials] = useState<AppAdminCredentialList | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The first read that failed, said where what it reads shows, with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  // The setting whose address was refused, marked with the line (rejected-entry focus).
  const [refusedSetting, setRefusedSetting] = useState("");
  // A saved secret, address or credentials policy says so (2026-10-16: only
  // the row's state changed).
  const toast = useOptionalProductToast();
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  useEffect(() => {
    let live = true;
    Promise.all([listAppSecrets(session, installId), listAppHooks(session, installId), listAppSettings(session, installId),
      listAppAdminCredentials(session, installId)]).then(
      ([listed, listedHooks, listedSettings, listedCredentials]) => {
        if (!live) return;
        setCredentials(listedCredentials.credentials.length ? listedCredentials : null);
        setSecrets(listed.secrets);
        setSettings(listedSettings.settings);
        setAddresses(Object.fromEntries(listedSettings.settings.map((setting) => [setting.key, setting.value])));
        setCallback(listed.oauthCallback ?? "");
        setReturns(listed.oauthReturns ?? []);
        setHooks(listedHooks.hooks);
      },
      (failure) => { if (live) setReadFailed(settingsErrorMessage(failure, t("apps.readFailed"))); },
    );
    return () => { live = false; };
  }, [installId, readTurn, session, t]);

  async function change(secretName: string, value: string) {
    setBusy(true);
    setError("");
    try {
      const listed = value ? await setAppSecret(session, installId, secretName, value) : await clearAppSecret(session, installId, secretName);
      setSecrets(listed.secrets);
      setValues((current) => ({ ...current, [secretName]: "" }));
      toast?.notify(t("apps.saved"), "success");
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function changeReturn(provider: string, added: boolean) {
    setBusy(true);
    setError("");
    try {
      const listed = await setAppOAuthReturn(session, installId, provider, added);
      setReturns(listed.oauthReturns ?? []);
      toast?.notify(t("apps.saved"), "success");
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function changeAddress(key: string, value: string) {
    setBusy(true);
    setError("");
    setRefusedSetting("");
    try {
      const listed = await setAppSetting(session, installId, key, value.trim());
      setSettings(listed.settings);
      setAddresses(Object.fromEntries(listed.settings.map((setting) => [setting.key, setting.value])));
      toast?.notify(t("apps.saved"), "success");
    } catch (failure) {
      setRefusedSetting(key);
      setError(settingsErrorMessage(failure, t("apps.settings.invalid")));
    } finally {
      setBusy(false);
    }
  }

  async function changeCredentials(enabled: boolean | "revoke") {
    if (enabled === "revoke" && !await confirmAction({
      message: t("apps.credentials.revokeConfirm", { name }), detail: t("apps.credentials.revokeConfirmDetail"), confirmLabel: t("apps.credentials.revoke"), danger: true,
    })) return;
    setBusy(true);
    setError("");
    try {
      setCredentials(enabled === "revoke" ? await revokeAppCredentials(session, installId) : await setAppCredentialsPolicy(session, installId, enabled));
      // The switch shows its own state; revoking them all shows nothing, so it says so.
      if (enabled === "revoke") toast?.notify(t("apps.saved"), "success");
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  // A webhook by the name its app gives it in the reader's language, else its key.
  const hookName = (hook: AppHook) => appText(hook.name ?? {}, i18n.language) || hook.key;

  async function makeHookSecret(hook: AppHook) {
    if (hook.set && !await confirmAction({ message: t("apps.hooks.remakeConfirm", { key: hookName(hook) }), detail: t("apps.hooks.remakeConfirmDetail"), confirmLabel: t("apps.hooks.remake"), danger: true })) return;
    setBusy(true);
    setError("");
    try {
      const next = await makeAppHookSecret(session, installId, hook.key);
      setMade(next);
      setHooks((current) => current.map((item) => (item.key === hook.key ? next.hook : item)));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  const hosts = (relays: string[]) => relays.map((relay) => { try { return new URL(relay).host; } catch { return relay; } }).join(", ");
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-secrets>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("apps.settings.title", { name })}</h2>
        {settings.length ? <p className={styles.note}>{t("apps.settings.meta")}</p> : null}
        {settings.map((setting) => {
          const label = appText(setting.name, i18n.language);
          const value = addresses[setting.key] ?? "";
          return (
            <form key={setting.key} className={styles.secretRow} data-app-setting={setting.key}
              onSubmit={(event) => { event.preventDefault(); void changeAddress(setting.key, value); }}>
              <span className={styles.secretName}>
                <strong>{label}</strong>
                <span>{setting.value ? t("apps.settings.set") : t("apps.settings.empty")}</span>
                {setting.help ? <span>{appText(setting.help, i18n.language)}</span> : null}
              </span>
              <input type="text" autoComplete="off" spellCheck={false} disabled={busy} value={value}
                aria-label={label} placeholder={t("apps.settings.placeholder", { port: setting.defaultPort })}
                onChange={(event) => setAddresses((current) => ({ ...current, [setting.key]: event.target.value }))}
                {...(error && refusedSetting === setting.key ? { "aria-invalid": true as const, "aria-describedby": "app-secrets-error" } : {})} />
              <button type="submit" className={styles.primary} disabled={busy || !value.trim() || value.trim() === setting.value}
                data-app-setting-save>{t("apps.secrets.save")}</button>
              <button type="button" className={styles.secondary} disabled={busy || !setting.value} data-app-setting-clear
                aria-hidden={!setting.value || undefined} tabIndex={setting.value ? undefined : -1}
                style={setting.value ? undefined : { visibility: "hidden" }}
                onClick={() => void changeAddress(setting.key, "")}>{t("apps.secrets.clear")}</button>
            </form>
          );
        })}
        {credentials ? (
          <div className={credentialStyles.credentialsSection} data-app-credentials>
            {settings.length ? <h3 className={styles.permissionsTitle}>{t("apps.credentials.title")}</h3> : null}
            <SettingSwitch title={t("apps.credentials.enabled")} description={t("apps.credentials.meta")} on={credentials.enabled} disabled={busy}
              onChange={(enabled) => void changeCredentials(enabled)} control={{ "data-app-credentials-enabled": "" }} />
            {credentials.credentials.map((credential) => (
              <div key={credential.key} className={credentialStyles.credentialRow} data-app-credential={credential.key}>
                <span>{appText(credential.name, i18n.language)}</span>
                <span className={styles.note}>{t("apps.credentials.people", { count: credential.people })}</span>
              </div>
            ))}
            {credentials.credentials.some((item) => item.people) ? (
              <div>
                <button type="button" className={styles.secondary} disabled={busy}
                  data-app-credentials-revoke onClick={() => void changeCredentials("revoke")}>{t("apps.credentials.revoke")}</button>
              </div>
            ) : null}
          </div>
        ) : null}
        {(settings.length || credentials) && secrets.length ? <h3 className={styles.permissionsTitle}>{t("apps.secrets.open")}</h3> : null}
        {secrets.length ? <p className={styles.note}>{t("apps.secrets.meta")}</p> : null}
        {callback ? (
          <p className={styles.note} data-app-oauth-callback>
            {t("apps.secrets.callback")} <code className={styles.callback}>{callback}</code>
          </p>
        ) : null}
        {returns.map((moved) => (
          <div key={moved.provider} className={styles.returnMoved} data-app-oauth-return={moved.added ? "added" : "earlier"}>
            <p>{t(moved.added ? "apps.secrets.returnAdded" : "apps.secrets.returnMoved", { provider: moved.provider, earlier: moved.earlier })}</p>
            <button type="button" className={styles.secondary} disabled={busy} onClick={() => void changeReturn(moved.provider, !moved.added)}>
              {t(moved.added ? "providers.returnUndo" : "providers.returnAdd")}
            </button>
          </div>
        ))}
        {secrets.map((secret) => (
          <form key={secret.name} className={styles.secretRow} data-app-secret={secret.name}
            onSubmit={(event) => { event.preventDefault(); if (values[secret.name]) void change(secret.name, values[secret.name]); }}>
            <span className={styles.secretName}>
              <strong>{secret.name}</strong>
              <span>{[secret.set ? t("apps.secrets.set", { last4: secret.last4 }) : t("apps.secrets.empty"),
                t("apps.secrets.usedBy", { hosts: hosts(secret.relays) })].join(" · ")}</span>
              {secret.needsEntry ? <span className={styles.unreadable} data-app-secret-unreadable>{t("apps.secrets.needsEntry")}</span> : null}
            </span>
            <input type="password" autoComplete="off" spellCheck={false} disabled={busy} value={values[secret.name] ?? ""}
              aria-label={t("apps.secrets.value", { name: secret.name })} placeholder={t("apps.secrets.placeholder")}
              onChange={(event) => setValues((current) => ({ ...current, [secret.name]: event.target.value }))} />
            <button type="submit" className={styles.primary} disabled={busy || !values[secret.name]} data-app-secret-save>{t("apps.secrets.save")}</button>
            {/* Kept in place when empty, so every row's field lines up. */}
            <button type="button" className={styles.secondary} disabled={busy || !secret.set} data-app-secret-clear
              aria-hidden={!secret.set || undefined} tabIndex={secret.set ? undefined : -1}
              style={secret.set ? undefined : { visibility: "hidden" }}
              onClick={() => void change(secret.name, "")}>{t("apps.secrets.clear")}</button>
          </form>
        ))}
        {hooks.length ? <h3 className={styles.permissionsTitle}>{t("apps.hooks.title")}</h3> : null}
        {hooks.map((hook) => (
          <div key={hook.key} className={styles.hookRow} data-app-hook={hook.key}>
            <span className={styles.secretName}>
              <strong>{hookName(hook)}</strong>
              <span>{[t(hook.verify === "token" ? "apps.hooks.verifyToken" : "apps.hooks.verifyHmac"),
                hook.set ? t("apps.hooks.set", { last4: hook.last4 }) : t("apps.hooks.empty"),
                ...(hook.waiting ? [t("apps.hooks.waiting", { count: hook.waiting })] : []),
                ...(hook.lastReceivedAt ? [t("apps.hooks.last", { date: displayDateTime(hook.lastReceivedAt) })] : []),
              ].join(" · ")}</span>
              {hook.needsNewSecret ? <span className={styles.unreadable} data-app-hook-unreadable>{t("apps.hooks.needsNewSecret")}</span> : null}
              <code className={styles.callback}>{hook.url}</code>
              {hook.earlierAddress ? <span className={styles.note} data-app-hook-earlier>{t("apps.hooks.earlier", { earlier: hook.earlierAddress })}</span> : null}
            </span>
            <button type="button" className={hook.set ? styles.secondary : styles.primary} disabled={busy} data-app-hook-make
              onClick={() => void makeHookSecret(hook)}>{t(hook.set ? "apps.hooks.remake" : "apps.hooks.make")}</button>
            {made?.hook.key === hook.key && !made.tenancyKey ? (
              <div className={styles.hookOnce} data-app-hook-once>
                <span>{t("apps.hooks.once")}</span>
                <label>{t("apps.hooks.secret")}<input readOnly value={made.secret} onFocus={(event) => event.currentTarget.select()} /></label>
                <label>{t("apps.hooks.address")}<input readOnly value={made.url} onFocus={(event) => event.currentTarget.select()} /></label>
              </div>
            ) : null}
            <AppHookOrganizations session={session} installId={installId} hook={hook} tenancies={tenancies} busy={busy} onBusy={setBusy}
              onChange={(next) => setHooks((current) => current.map((item) => (item.key === next.key ? next : item)))} />
          </div>
        ))}
        {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        {error ? <div id="app-secrets-error" className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button ref={closeRef} type="button" className={styles.secondary} disabled={busy} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
