import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { AdministratorSettings } from "@/administratorWire.generated";
import type { Session } from "@/types";
import styles from "./AdministratorSettingsPage.module.css";
import { useCopyNotice } from "@/lib/copyNotice";

const LOCAL_HOST = /^https?:\/\/(localhost|127\.\d+\.\d+\.\d+|\[::1\])(:\d+)?(\/|$)/iu;

/**
 * The address a sign-in provider sends people back to, which its
 * administrator registers with the provider's OAuth client (Google's
 * "authorized redirect URI"), with a copy (2026-10-19: it showed nowhere, so
 * Google sign-in couldn't be set up without reading the source). An address
 * only this computer opens says so. After the server's address changed, the
 * provider still has the old one: sign-ins go back through it while it
 * forwards here, until the administrator says the new one is added (the
 * multi-server decision's 2026-10-20 change).
 */
export function AdministratorProviderRedirect({ uri, earlier = "", added = false, provider, session, onSettings }: {
  uri: string;
  /** The earlier return address the provider has, which sign-ins go back through while it forwards here. */
  earlier?: string;
  /** The administrator said the new one is added at the provider. */
  added?: boolean;
  provider?: "google" | "oidc";
  session?: Session;
  onSettings?: (settings: AdministratorSettings) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState(false);
  if (!uri) return <p className={`${styles.providerRedirect} ${styles.emailProviderFieldHint}`} data-provider-redirect>{t("providers.redirectUnknown")}</p>;
  // After an address change: the new value to add, then saying it's added
  // (sign-ins come back to it from then), or going back when it's refused.
  const setAdded = (next: boolean) => {
    if (!provider || !session) return;
    setSaving(true);
    setFailed(false);
    void api.setAdministratorProviderReturn(session, provider, next).then(onSettings, () => setFailed(true)).finally(() => setSaving(false));
  };
  return (
    <div className={styles.providerRedirect} data-provider-redirect>
      {earlier ? (
        <div className={styles.providerReturnMoved} data-provider-return-moved={added ? "added" : "earlier"}>
          <p>{t(added ? "providers.returnAdded" : "providers.returnMoved", { earlier })}</p>
          <div>
            <button type="button" className={styles.secondaryButton} disabled={saving} onClick={() => setAdded(!added)}>
              {t(added ? "providers.returnUndo" : "providers.returnAdd")}
            </button>
            {failed ? <small role="alert">{t("providers.returnFailed")}</small> : null}
          </div>
        </div>
      ) : null}
      <div className={styles.providerRedirectHead}>
        <strong>{t("providers.redirectUri")}</strong>
        <button type="button" className={styles.secondaryButton} onClick={() => void copy(uri)}>{t("providers.copy")}</button>
      </div>
      <code className={styles.providerRedirectUri} data-provider-redirect-uri>{uri}</code>
      <small className={styles.emailProviderFieldHint}>{t(LOCAL_HOST.test(uri) ? "providers.redirectLocal" : "providers.redirectHint")}</small>
    </div>
  );
}
