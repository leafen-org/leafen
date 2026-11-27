import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import type { AdministratorNetworkProxy as NetworkProxy } from "../../administratorWire.generated";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import type { Session } from "../../types";
import { NetworkProxyForm } from "../settings/NetworkProxyForm";
import styles from "./AdministratorSettingsPage.module.css";
import dataStyles from "./AdministratorDataManagement.module.css";
import { ReadFailed } from "../ReadFailed";

/**
 * The proxy the server goes out by (network_proxy_settings.go): the
 * system's settings and the environment unless an administrator sets one,
 * for its requests out.
 * The desktop app's own server is set in the app's This computer.
 */
export function AdministratorNetworkProxy({ session }: { session: Session }) {
  const { t } = useTranslation("networkProxy");
  const [saved, setSaved] = useState<NetworkProxy | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    api.networkProxy(session, controller.signal).then(setSaved).catch((cause) => {
      if (!controller.signal.aborted) setError(productErrorMessage(cause, "administratorNetworkProxy.read"));
    });
    return () => {
      mounted.current = false;
      controller.abort();
    };
  }, [readTurn, session]);

  return (
    <section className={styles.section} aria-labelledby="administrator-network-proxy-title" data-administrator-network-proxy>
      <div id="administrator-network-proxy-title" className={styles.sectionTitle}>{t("title")}</div>
      <p className={`${styles.settingNote} ${styles.lead}`}>{t("serverMeta")}</p>
      {saved?.managedByApp ? (
        <p className={styles.emailProviderMeta} data-network-proxy-app>{t("managedByApp")}</p>
      ) : saved ? (
        <div className={styles.emailProviderBlock}>
          <NetworkProxyForm setting={saved} inForce={{ source: saved.source, http: saved.http, https: saved.https, unfollowed: saved.unfollowed as "" }}
            busy={busy} onEdit={() => setMessage("")} onSave={async (input) => {
              setBusy(true);
              setMessage("");
              setError("");
              try {
                const next = await api.saveNetworkProxy(session, { ...input, revision: saved.revision });
                if (!mounted.current) return false;
                setSaved(next);
                setMessage(t("saved"));
                return true;
              } catch (cause) {
                if (mounted.current) setError(productErrorMessage(cause, "administratorNetworkProxy.save"));
                return false;
              } finally {
                if (mounted.current) setBusy(false);
              }
            }} />
        </div>
      ) : null}
      {/* Not read yet: what failed, with a way to read it again. */}
      {!saved && error ? <ReadFailed message={error} onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }} /> : (
        <div className={dataStyles.live} data-error={error ? "true" : undefined} role={error ? "alert" : "status"} aria-live="polite">
          {error || message || (saved ? "" : t("loading"))}
        </div>
      )}
    </section>
  );
}
