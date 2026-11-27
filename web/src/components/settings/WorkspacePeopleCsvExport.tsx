import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { Download } from "@/icons/leafen";
import { useOptionalProductToast } from "@/lib/productToast";
import type { Session, Workspace } from "@/types";
import { styles } from "../account/accountSettingsStyles";
import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";

/** Downloads the workspace's members and guests as CSV at once. */
function useWorkspacePeopleCsvDownload(session: Session, workspace: Workspace) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const toast = useOptionalProductToast();
  const [busy, setBusy] = useState(false);
  async function download() {
    setBusy(true);
    try {
      const response = await api.downloadWorkspacePeopleCsv(session, workspace.id);
      const saved = await triggerBrowserFileDownload(await response.blob(),
        t("peopleCsvFilename", { name: workspace.name || t("untitledWorkspace") }), { revokeAfterMs: 60_000 });
      if (saved === "failed") toast?.notify(t("peopleCsvFailed"), "error");
    } catch {
      toast?.notify(t("peopleCsvFailed"), "error");
    } finally {
      setBusy(false);
    }
  }
  return { busy, download };
}

/**
 * "〈워크스페이스〉의 모든 멤버와 게스트를 CSV로 내보내기", as Notion's 일반 →
 * 내보내기 (seen 2026-10-11): the button downloads the file at once.
 */
export function WorkspacePeopleCsvExport({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const csv = useWorkspacePeopleCsvDownload(session, workspace);
  return (
    <div className={styles.securityRow} data-workspace-people-csv>
      <div className={styles.securityInstruction}>
        <strong>{t("peopleCsvMembers")}</strong>
        <span>{t("peopleCsvDescription")}</span>
      </div>
      <button type="button" className={styles.secondaryButton} disabled={csv.busy} onClick={() => void csv.download()}>
        {t("exportTitle")}
      </button>
    </div>
  );
}

/** The same download from 사람's tab row, as Notion's export icon there (seen 2026-10-12). */
export function WorkspacePeopleCsvIconButton({ session, workspace, className }: {
  session: Session;
  workspace: Workspace;
  className: string;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const csv = useWorkspacePeopleCsvDownload(session, workspace);
  return (
    <button type="button" className={className} disabled={csv.busy} aria-label={t("peopleCsvButton")} title={t("peopleCsvButton")}
      data-people-csv onClick={() => void csv.download()}>
      <Download size="small" aria-hidden="true" />
    </button>
  );
}
