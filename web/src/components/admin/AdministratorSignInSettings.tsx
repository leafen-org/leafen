import { useTranslation } from "react-i18next";
import type { AdministratorSettings, AdministratorSettingsInput } from "../../administratorWire.generated";
import { AppSelect } from "@/components/AppSelect";
import type { Session } from "../../types";
import { AdministratorProviderRedirect } from "./AdministratorProviderRedirect";
import { SettingField, SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import styles from "./AdministratorSettingsPage.module.css";

export type ProviderField = "googleClientId" | "googleSecret" | "oidcName" | "oidcClientId" | "oidcSecret" | "oidcScopes";

type Providers = AdministratorSettingsInput["providers"];

/**
 * 설정 › 가입 및 로그인: who may make an account and add members, then each way
 * to sign in — off, one switch; on, what it needs, with the address to give
 * the provider. A secret that stays behind a switched-off provider can be let go.
 */
export function AdministratorSignInSettings({
  session, settings, draft, busy, providerError, issuerError, onDraft, onGoogle, onOIDC, onProviderReturn,
}: {
  session: Session;
  settings: AdministratorSettings;
  draft: AdministratorSettingsInput;
  busy: boolean;
  providerError: { field: ProviderField; message: string } | null;
  issuerError: string;
  onDraft(change: Partial<AdministratorSettingsInput>): void;
  onGoogle(change: Partial<Providers["google"]>): void;
  onOIDC(change: Partial<Providers["oidc"]>): void;
  onProviderReturn(settings: AdministratorSettings): void;
}) {
  const { t } = useTranslation("serverConsole");
  const errorOf = (field: ProviderField) => providerError?.field === field ? providerError.message : "";
  const { google, oidc } = draft.providers;
  const switcherLists = draft.switcherLists?.enabled ?? settings.switcherLists.enabled;
  return (
    <>
      <section className={styles.section} data-administrator-signup aria-labelledby="administrator-signup-title">
        <div id="administrator-signup-title" className={styles.sectionTitle}>{t("email.signupSection")}</div>
        <div className={styles.settingRows}>
          <SettingRow title={t("email.signup")} description={t(draft.signupPolicy === "public" ? "email.signupPublicHelp" : "email.signupInvitedHelp")}>
            <AppSelect look="setting" aria-label={t("email.signupPolicy")} value={draft.signupPolicy} disabled={busy}
              onChange={(event) => onDraft({ signupPolicy: event.target.value as AdministratorSettingsInput["signupPolicy"] })}>
              <option value="closed">{t("email.signupInvited")}</option>
              <option value="public">{t("email.signupPublic")}</option>
            </AppSelect>
          </SettingRow>
          <SettingField title={t("email.signupContact")} description={t("email.signupContactHelp")}>
            <input value={draft.signupContact ?? ""} maxLength={300} disabled={busy} onChange={(event) => onDraft({ signupContact: event.target.value })} />
          </SettingField>
          <SettingSwitch title={t("email.memberAdd")} description={t("email.memberAddHelp")} on={draft.memberAddPolicy === "enabled"} disabled={busy}
            onChange={(on) => onDraft({ memberAddPolicy: on ? "enabled" : "disabled" })} />
          <SettingSwitch title={t("email.switcherLists")} description={t("email.switcherListsHelp")} on={switcherLists} disabled={busy}
            onChange={(enabled) => onDraft({ switcherLists: { enabled } })} data-administrator-switcher-lists="" />
        </div>
      </section>
      <section className={styles.section} aria-labelledby="administrator-google-title" data-administrator-provider="google">
        <div id="administrator-google-title" className={styles.sectionTitle}>Google</div>
        <div className={styles.settingRows}>
          <SettingSwitch title={t("providers.useGoogle")} on={google.enabled} disabled={busy} onChange={(enabled) => onGoogle({ enabled })} />
          {google.enabled ? (
            <>
              <AdministratorProviderRedirect uri={settings.providers.google.redirectUri} earlier={settings.providers.google.registeredRedirectUri}
                added={settings.providers.google.redirectAdded} provider="google" session={session} onSettings={onProviderReturn} />
              <SettingField title={t("providers.clientId")} error={errorOf("googleClientId")}>
                <input value={google.clientId} onChange={(event) => onGoogle({ clientId: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title={t("providers.clientSecret")} error={errorOf("googleSecret")}>
                <input type="password" autoComplete="new-password" value={google.clientSecret ?? ""} placeholder={settings.providers.google.secretConfigured ? t("providers.secretSet") : ""}
                  onChange={(event) => onGoogle({ clientSecret: event.target.value, clearSecret: false })} disabled={busy} />
              </SettingField>
            </>
          ) : settings.providers.google.secretConfigured ? (
            <SettingSwitch title={t("providers.clearSecret")} on={google.clearSecret} disabled={busy} onChange={(clearSecret) => onGoogle({ clearSecret, clientSecret: null })} />
          ) : null}
        </div>
      </section>
      <section className={styles.section} aria-labelledby="administrator-oidc-title" data-administrator-provider="oidc">
        <div id="administrator-oidc-title" className={styles.sectionTitle}>{t("providers.genericOidc")}</div>
        <div className={styles.settingRows}>
          <SettingSwitch title={t("providers.useOidc")} on={oidc.enabled} disabled={busy} onChange={(enabled) => onOIDC({ enabled })} />
          {oidc.enabled ? (
            <>
              <AdministratorProviderRedirect uri={settings.providers.oidc.redirectUri} earlier={settings.providers.oidc.registeredRedirectUri}
                added={settings.providers.oidc.redirectAdded} provider="oidc" session={session} onSettings={onProviderReturn} />
              <SettingField title={t("providers.displayName")} error={errorOf("oidcName")}>
                <input value={oidc.name} onChange={(event) => onOIDC({ name: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title="Issuer" error={issuerError}>
                <input type="url" value={oidc.issuer} placeholder="https://login.example.com" onChange={(event) => onOIDC({ issuer: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title={t("providers.clientId")} error={errorOf("oidcClientId")}>
                <input value={oidc.clientId} onChange={(event) => onOIDC({ clientId: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title={t("providers.clientSecret")} error={errorOf("oidcSecret")}>
                <input type="password" autoComplete="new-password" value={oidc.clientSecret ?? ""} placeholder={settings.providers.oidc.secretConfigured ? t("providers.secretSet") : ""}
                  onChange={(event) => onOIDC({ clientSecret: event.target.value, clearSecret: false })} disabled={busy} />
              </SettingField>
              <SettingField title="Scopes" error={errorOf("oidcScopes")}>
                <input value={oidc.scopes.join(" ")} onChange={(event) => onOIDC({ scopes: event.target.value.split(/[\s,]+/u).filter(Boolean) })} disabled={busy} />
              </SettingField>
            </>
          ) : settings.providers.oidc.secretConfigured ? (
            <SettingSwitch title={t("providers.clearSecret")} on={oidc.clearSecret} disabled={busy} onChange={(clearSecret) => onOIDC({ clearSecret, clientSecret: null })} />
          ) : null}
        </div>
      </section>
    </>
  );
}
