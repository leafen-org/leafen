import { useTranslation } from "react-i18next";
import styles from "./AppHost.module.css";

/** In place of an app's frame Leafen stopped for flooding it with calls. */
export function AppFrameStopped({ onReopen }: { onReopen: () => void }) {
  const { t } = useTranslation("common");
  return (
    <div className={styles.stopped} role="alert" data-app-frame-stopped>
      <p>{t("appHost.flooded")}</p>
      <button type="button" className={styles.reopen} onClick={onReopen} data-app-frame-reopen>{t("appHost.reopen")}</button>
    </div>
  );
}
