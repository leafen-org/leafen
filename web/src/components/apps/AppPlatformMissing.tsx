import { useTranslation } from "react-i18next";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { useServerAdministrator } from "@/lib/serverAdministrator";
import type { Session } from "@/types";
import styles from "./AppUnavailable.module.css";

// Where apps would be on a server whose build leaves the app platform out
// (the multi-server decision's 2026-10-20 "폰 앱의 자기 서버는 체험판", back
// 2026-11-01): a phone app's own server is a trial — apps run on an
// always-on server, which its administrator can move it to — and the Mac App
// Store edition's apps come with the app downloaded from Leafen.

/** Why apps aren't here, and for a phone server's administrator the way to move it. */
export function AppPlatformMissingNotice({ missing, session, compact = false }: {
  missing: "phone" | "store";
  session?: Session | null;
  compact?: boolean;
}) {
  const { t } = useTranslation("common");
  const canMove = useServerAdministrator(session, missing === "phone");
  return (
    <div className={compact ? styles.compact : styles.notice} data-app-platform-missing={missing}>
      <span>{t(missing === "phone" ? "appHost.platformMissing.phone" : "appHost.platformMissing.store")}</span>
      {canMove ? (
        <span className={styles.actions}>
          <button type="button" className={styles.action} data-app-platform-move data-focus-return="app-platform-move"
            onClick={() => requestSettingsWindow({ section: "server-restore", part: "server-move" })}>
            {t("appHost.platformMissing.move")}
          </button>
        </span>
      ) : null}
    </div>
  );
}
