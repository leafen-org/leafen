import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { Session, Workspace } from "@/types";
import { workspaceMembershipErrorCodes } from "@/workspaceMembershipWire.generated";
import { styles } from "./workspaceSettingsOverviewStyles";
import general from "./WorkspaceGeneral.module.css";
import { WorkspaceIdSection } from "./WorkspaceSettingsOverview";

/**
 * 일반 of someone who doesn't manage the workspace, as Notion's member's (seen
 * 2026-11-21 in two tabs of the business trial's member, 1440 dark): 위험 구역
 * with "워크스페이스 나가기" — its title 14/20 500, its words 13/18 grey and a
 * red-tinted 28-tall button — then the workspace's ID. Leaving asks first, in
 * Notion's words and buttons (seen 2026-11-04); one the identity provider keeps
 * here is told so. Notion's "사이드바에 다른 Notion 앱 표시" row above it is
 * Notion's own apps, which Leafen has none of.
 */
export function WorkspaceMemberGeneral({ session, workspace, onNavigate, onWorkspaceRefresh }: {
  session: Session;
  workspace: Workspace;
  /** Closes the settings window on another of the app's addresses. */
  onNavigate?: (path: string) => void;
  onWorkspaceRefresh: () => Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const titleId = useId();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function leave() {
    if (!await confirmAction({ message: t("memberLeaveConfirm"), detail: t("memberLeaveDetail"),
      confirmLabel: t("memberLeaveAction"), danger: true })) return;
    setBusy(true);
    setError("");
    try {
      await api.revokeWorkspaceMember(session, workspace.id, session.user.id);
      onNavigate?.("/");
      await onWorkspaceRefresh();
    } catch (reason) {
      setError(t((reason as { code?: string } | null)?.code === workspaceMembershipErrorCodes.memberLeaveManaged
        ? "memberLeaveManaged" : "memberRemoveFailed"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <section className={styles.section} aria-label={t("dangerZoneTitle")}>
        <div className={styles.sectionTitle}>{t("dangerZoneTitle")}</div>
        <div className={styles.securityRow} aria-labelledby={titleId} data-workspace-leave>
          <div className={styles.securityInstruction}>
            <strong id={titleId}>{t("memberLeaveWorkspace")}</strong>
            <span>{t("memberLeaveWorkspaceDesc")}</span>
          </div>
          <button type="button" className={general.danger} disabled={busy} onClick={() => void leave()}>
            {t("memberLeaveWorkspace")}
          </button>
        </div>
        {error ? <div className={general.message} data-tone="error" role="alert">{error}</div> : null}
      </section>
      <WorkspaceIdSection workspaceId={workspace.id} member />
    </>
  );
}
