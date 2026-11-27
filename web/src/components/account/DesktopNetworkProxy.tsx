import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { desktopNetworkProxy, desktopNetworkProxyAvailable, type DesktopNetworkProxyState } from "@/lib/desktopNetworkProxy";
import { userErrorText } from "@/lib/settingsErrors";
import { NetworkProxyForm } from "@/components/settings/NetworkProxyForm";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { ListNotice } from "@/components/settings/SettingsRowNotice";
import { styles } from "./accountSettingsStyles";

/**
 * This computer's network proxy, in the desktop app's This computer
 * settings: what the app and its own server go out by.
 */
/** `initial` is the state read with the page's other parts (ThisComputerSettings). */
export function DesktopNetworkProxy({ initial }: { initial?: DesktopNetworkProxyState | null } = {}) {
  const { t } = useTranslation("networkProxy");
  const shown = desktopNetworkProxyAvailable();
  const [state, setState] = useState<DesktopNetworkProxyState | null>(initial ?? null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    if (!shown || initial !== undefined) return;
    void desktopNetworkProxy.state().then(setState, (cause) => setError(userErrorText(cause, t("readFailed"))));
  }, [initial, readTurn, shown, t]);

  if (!shown) return null;
  // Not read yet: nothing, or what failed with a way to read it again.
  if (!state) return error ? (
    <section className={styles.section} data-desktop-network-proxy>
      <div className={styles.sectionTitle}>{t("title")}</div>
      <ListNotice role="alert" onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }}>{error}</ListNotice>
    </section>
  ) : null;
  return (
    <section className={styles.section} data-desktop-network-proxy>
      <div className={styles.sectionHeader}>
        <div>
          <div className={styles.sectionTitle}>{t("title")}</div>
          <div className={styles.organizationMeta}>{t("computerMeta")}</div>
        </div>
      </div>
      {error ? <ListNotice role="alert">{error}</ListNotice> : null}
      <NetworkProxyForm setting={state} inForce={state.inForce} busy={busy} onEdit={() => setMessage("")} onSave={async (input) => {
        setBusy(true);
        setError("");
        setMessage("");
        try {
          setState(await desktopNetworkProxy.set(input));
          setMessage(t("saved"));
          return true;
        } catch (cause) {
          setError(userErrorText(cause, t("saveFailed")));
          return false;
        } finally {
          setBusy(false);
        }
      }} />
      {message ? <div className={settingsStyles.noticeText} role="status">{message}</div> : null}
    </section>
  );
}
