import { Fragment, useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { confirmAction } from "@/components/ConfirmActionDialog";
import {
  appRegistrationOperations,
  type WorkspaceApp,
  type WorkspaceAppListResponse,
} from "@/appRegistrationWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useRowNotice } from "@/lib/useNoticeFor";
import type { Session, Workspace } from "@/types";
import styles from "./SettingsItemRows.module.css";
import { AppSelect } from "@/components/AppSelect";
import { ListNotice, RowNotice } from "./SettingsRowNotice";

function appPath(path: string, workspaceId: string, appId = "") {
  return path.replace("{workspaceId}", encodeURIComponent(workspaceId)).replace("{appId}", encodeURIComponent(appId));
}

/** The workspace's apps listed with the session's members. */
export async function listWorkspaceApps(session: Session, workspaceId: string, signal?: AbortSignal) {
  const listed = await request<WorkspaceAppListResponse>(appPath(appRegistrationOperations.workspaceList.path, workspaceId),
    { method: appRegistrationOperations.workspaceList.method, signal }, session);
  return listed.apps;
}

/**
 * Apps installed in the workspace as their own accounts, under "설치됨": each
 * one's name, whether it is a member or a guest that sees only the pages
 * shared with it, and — for the workspace's owner — changing that or
 * removing the app, which ends its connections.
 */
export function WorkspaceAppAccounts({ session, workspace, canManage, onCount }: {
  session: Session;
  workspace: Workspace;
  canManage: boolean;
  /** Tells how many apps are installed, so an empty list says so only when none are. */
  onCount?: (count: number) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [apps, setApps] = useState<WorkspaceApp[]>([]);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState("");
  // A change or removal that failed sits under its app's row until that row
  // reads otherwise or the next one starts.
  const [rowNotice, setRowNotice] = useRowNotice<string>((appId) => apps.find((app) => app.appId === appId)?.role);
  const load = useCallback(async () => {
    try {
      setApps(await listWorkspaceApps(session, workspace.id));
      setLoadError("");
    } catch (failure) {
      setLoadError(settingsErrorMessage(failure, t("appLoadFailed")));
    }
  }, [session, t, workspace.id]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => { onCount?.(apps.length); }, [apps.length, onCount]);
  if (apps.length === 0 && !loadError) return null;
  async function run(operation: { method: string; path: string }, app: WorkspaceApp, body?: unknown) {
    setBusy(true);
    setRowNotice(app.appId, null);
    try {
      await request<void>(appPath(operation.path, workspace.id, app.appId), {
        method: operation.method, body: body === undefined ? undefined : JSON.stringify(body),
      }, session);
      await load();
    } catch (failure) {
      setRowNotice(app.appId, settingsErrorMessage(failure, t("appFailed")));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div data-workspace-app-accounts>
      {apps.map((app) => (
        <Fragment key={app.appId}>
          <div className={styles.row} data-workspace-app={app.appId}>
            <span className={styles.icon} aria-hidden="true">{app.name.slice(0, 1).toUpperCase()}</span>
            <span className={styles.text}>
              <strong>{t("appAccountName", { name: app.name })}</strong>
              <span>{app.role === "member" ? t("appAccountMember") : t("appAccountGuest")}</span>
            </span>
            <span className={styles.actions}>
              {canManage ? (
                <>
                  <AppSelect value={app.role} disabled={busy} aria-label={t("appAccountRole", { name: app.name })}
                    onChange={(event) => void run(appRegistrationOperations.workspaceUpdate, app, { role: event.target.value })}>
                    <option value="guest">{t("appAccountGuest")}</option>
                    <option value="member">{t("appAccountMember")}</option>
                  </AppSelect>
                  <button type="button" disabled={busy} data-workspace-app-remove
                    onClick={() => void confirmAction({ message: t("appWorkspaceRemoveConfirm", { name: app.name }), detail: t("appWorkspaceRemoveConfirmDetail"),
                      confirmLabel: t("appRemove"), danger: true })
                      .then((confirmed) => { if (confirmed) void run(appRegistrationOperations.workspaceRemove, app); })}>{t("appRemove")}</button>
                </>
              ) : <span className={styles.badge}>{t("connectionInstalled")}</span>}
            </span>
          </div>
          <RowNotice text={rowNotice(app.appId)} />
        </Fragment>
      ))}
      {loadError ? <ListNotice role="alert" onRetry={() => { setLoadError(""); void load(); }} data-app-accounts-read-failed>{loadError}</ListNotice> : null}
    </div>
  );
}
