import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { administratorWorkspaceContent } from "@/administratorWorkspaceContentApi";
import type { Session } from "@/types";
import { AdministratorWorkspaceContentLog } from "../admin/AdministratorWorkspaceContentLog";
import { styles } from "./workspaceSettingsOverviewStyles";

/**
 * "서버 관리자 기록" at the end of the workspace's security settings (Leafen's
 * own): what server
 * administrators opened or changed in the workspace, for its owner and its
 * organization's administrators; anyone else has none to see.
 */
export function WorkspaceServerAdministratorLog({ session, workspaceId }: { session: Session; workspaceId: string }) {
  const { t } = useTranslation("serverConsole");
  const [hidden, setHidden] = useState(false);
  const load = useCallback(async (cursor?: string, signal?: AbortSignal) => {
    try {
      return await administratorWorkspaceContent.ownerLog(session, workspaceId, cursor, signal);
    } catch (cause) {
      if ((cause as { status?: unknown } | null)?.status === 404) setHidden(true);
      throw cause;
    }
  }, [session, workspaceId]);
  if (hidden) return null;
  return (
    <section className={styles.section} aria-labelledby="workspace-server-admin-log-title" data-server-admin-log>
      <div className={styles.organizationSubhead}><span id="workspace-server-admin-log-title">{t("workspaceContent.log.title")}</span></div>
      <p className={styles.organizationMeta}>{t("workspaceContent.log.description")}</p>
      <AdministratorWorkspaceContentLog load={load} />
    </section>
  );
}
