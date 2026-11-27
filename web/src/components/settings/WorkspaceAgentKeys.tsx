import { Fragment, useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { activeDateLocale } from "@/lib/i18n";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useRowNotice } from "@/lib/useNoticeFor";
import { disconnectWorkspaceAgentKey, listWorkspaceAgentKeys, type WorkspaceAgentKey } from "@/lib/workspaceAgentKeys";
import type { Session, Workspace } from "@/types";
import table from "./DeveloperSettings.module.css";
import styles from "./SettingsItemRows.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice, TableRowNotice } from "./SettingsRowNotice";

/**
 * "모든 에이전트 연결 키" under the connections' "관리", beside Notion's "모든
 * 개인 액세스 토큰" (agent key decision of 2026-11-16): every agent key that
 * reaches the workspace — whose, what it may do, made, last used, when it
 * ends or that it renews itself — each ended at once; never a key's value.
 */
export function WorkspaceAgentKeys({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [keys, setKeys] = useState<WorkspaceAgentKey[] | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // An end that failed sits under its key's row until that row reads
  // otherwise or the next one starts.
  const [rowNotice, setRowNotice] = useRowNotice<string>((id) => keys?.find((key) => key.id === id)?.status);
  const load = useCallback((signal?: AbortSignal) => listWorkspaceAgentKeys(session, workspace.id, signal)
    .then((result) => setKeys(result.items))
    .catch((failure) => {
      if (!signal?.aborted) setError(settingsErrorMessage(failure, t("allAgentKeysLoadFailed")));
    }), [session, t, workspace.id]);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function end(key: WorkspaceAgentKey) {
    if (!await confirmAction({ message: t("agentDisconnectConfirm", { name: key.name }), detail: t("allAgentKeysEndDetail"),
      confirmLabel: t("disconnect"), danger: true })) return;
    setBusy(key.id);
    setRowNotice(key.id, null);
    try {
      await disconnectWorkspaceAgentKey(session, workspace.id, key.id);
      setKeys((current) => current?.filter((item) => item.id !== key.id) ?? current);
    } catch (failure) {
      setRowNotice(key.id, settingsErrorMessage(failure, t("connectionDisconnectFailed", { name: key.name })));
    } finally {
      setBusy("");
    }
  }

  const date = (value: number) => new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(value));
  return (
    <section className={settingsStyles.section} role="region" aria-label={t("allAgentKeysTitle")}>
      <div className={settingsStyles.sectionTitle}>{t("allAgentKeysTitle")}</div>
      {keys && keys.length === 0 ? <div className={styles.empty}>{t("allAgentKeysEmpty")}</div> : null}
      {keys && keys.length > 0 ? (
        <table className={table.tokens} data-workspace-agent-keys>
          <thead>
            <tr>
              <th scope="col">{t("personalAccessTokenColumnName")}</th>
              <th scope="col">{t("personalAccessTokenColumnCreator")}</th>
              <th scope="col">{t("personalAccessTokenColumnCreated")}</th>
              <th scope="col">{t("personalAccessTokenColumnLastUsed")}</th>
              <th scope="col">{t("personalAccessTokenExpiryLabel")}</th>
              <th scope="col"><span className={table.hidden}>{t("disconnect")}</span></th>
            </tr>
          </thead>
          <tbody>
            {keys.map((key) => (
              <Fragment key={key.id}>
                <tr data-workspace-agent-key={key.id} data-status={key.status}>
                  <td className={table.name}>{key.name}</td>
                  <td>{key.person.name || t("connectionUnknownPerson")}</td>
                  <td>{date(key.createdAt)}</td>
                  <td>{key.lastUsedAt ? date(key.lastUsedAt) : t("personalAccessTokenNeverUsed")}</td>
                  <td>{key.oauth ? t("keyRenewsShown") : key.expiresAt ? date(key.expiresAt) : ""}</td>
                  <td className={table.actions}>
                    <button type="button" className={table.secondary} disabled={busy === key.id}
                      aria-label={t("connectionDisconnectNamed", { name: key.name })} onClick={() => void end(key)}>{t("disconnect")}</button>
                  </td>
                </tr>
                <TableRowNotice text={rowNotice(key.id)} columns={6} />
              </Fragment>
            ))}
          </tbody>
        </table>
      ) : null}
      {error ? <ListNotice role="alert" onRetry={() => { setError(""); void load(); }} data-agent-keys-read-failed>{error}</ListNotice> : null}
    </section>
  );
}
