"use client";

import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { AppSelect } from "@/components/AppSelect";
import { confirmAction } from "@/components/ConfirmActionDialog";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { SettingsFormDialog } from "@/components/settings/SettingsFormDialog";
import local from "@/components/settings/SettingsRows.module.css";
import { SettingsRowMenu } from "@/components/settings/SettingsRowMenu";
import { SettingsSwitchRow } from "@/components/settings/SettingsSwitchRow";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import type { MCPGovernanceApprovedClient, MCPGovernancePolicy } from "@/enterpriseMCPGovernanceWire.generated";
import { Plus } from "@/icons/leafen";
import type {
  EnterpriseActionRunner,
  EnterpriseSettingsService,
  EnterpriseSettingsSnapshot,
} from "@/lib/enterpriseSettingsAdapter";
import { EnterpriseActionFeedback, type EnterpriseActionResult } from "./EnterpriseActionFeedback";

/**
 * "엔터프라이즈 관리 연결": per workspace, whether only the clients approved
 * here may connect over MCP, and those clients.
 */
export function EnterpriseMcpConnections({ busy, result, runAction, service, snapshot }: {
  busy: string;
  result: EnterpriseActionResult;
  runAction: EnterpriseActionRunner;
  service: EnterpriseSettingsService;
  snapshot: EnterpriseSettingsSnapshot;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [workspaceId, setWorkspaceId] = useState(snapshot.workspaces[0]?.id ?? "");
  const [policies, setPolicies] = useState(snapshot.mcpPolicies);
  const [approving, setApproving] = useState(false);
  const [renaming, setRenaming] = useState<MCPGovernanceApprovedClient | null>(null);
  useEffect(() => {
    setPolicies(snapshot.mcpPolicies);
    setWorkspaceId((current) => (
      snapshot.workspaces.some((workspace) => workspace.id === current) ? current : snapshot.workspaces[0]?.id ?? ""
    ));
  }, [snapshot.mcpPolicies, snapshot.workspaces]);
  const policy = policies[workspaceId];
  const clients = policy?.approvedClients ?? [];
  const publish = (next: MCPGovernancePolicy, target = workspaceId) => setPolicies((current) => ({ ...current, [target]: next }));

  const selectWorkspace = async (next: string) => {
    setWorkspaceId(next);
    if (!next || policies[next]) return;
    const read = await runAction(`mcp:read:${next}`, (context) => service.readMCPPolicy(context, next));
    if (read) publish(read, next);
  };
  const setEnabled = async (enabled: boolean) => {
    // On with none approved, every client stops at once: asked first.
    if (enabled && clients.length === 0 && !await confirmAction({
      message: t("enterpriseMcpEnableConfirm"), detail: t("enterpriseMcpBlockAllWarning"), confirmLabel: t("enterpriseMcpEnableConfirmLabel"), danger: true,
    })) return;
    const next = await runAction("mcp:policy", (context) => service.updateMCPPolicy(context, workspaceId, { enabled }));
    if (next) publish(next);
  };
  const approve = async (clientId: string, name: string) => {
    const next = await runAction("mcp:approve", (context) => service.approveMCPClient(context, workspaceId, { clientId, name: name || clientId }));
    if (next) publish(next);
    return Boolean(next);
  };
  const rename = async (client: MCPGovernanceApprovedClient, name: string) => {
    const next = await runAction(`mcp:rename:${client.clientId}`, (context) => service.renameMCPClient(context, workspaceId, client.clientId, { name }));
    if (next) publish(next);
    return Boolean(next);
  };
  const remove = async (client: MCPGovernanceApprovedClient) => {
    if (!await confirmAction({
      message: t("enterpriseMcpRemoveConfirm", { name: client.name }), detail: t("enterpriseMcpRemoveConfirmDetail"),
      confirmLabel: t("enterpriseRemove"), danger: true,
    })) return;
    const next = await runAction(`mcp:remove:${client.clientId}`, (context) => service.removeMCPClient(context, workspaceId, client.clientId));
    if (next) publish(next);
  };
  return (
    <section className={styles.section} role="region" aria-label={t("enterpriseMcpTitle")} data-enterprise-mcp>
      <div className={styles.sectionTitle}>{t("enterpriseMcpTitle")}</div>
      {snapshot.workspaces.length > 1 ? (
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseMcpWorkspace")}</strong>
            <span>{t("enterpriseMcpWorkspaceDescription")}</span>
          </div>
          <AppSelect aria-label={t("enterpriseMcpWorkspace")} look="setting" value={workspaceId} onChange={(event) => void selectWorkspace(event.target.value)}>
            {snapshot.workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
          </AppSelect>
        </div>
      ) : null}
      <SettingsSwitchRow title={t("enterpriseMcpEnable")} description={t("enterpriseMcpDescription")} checked={policy?.enabled ?? false}
        disabled={!workspaceId || busy === "mcp:policy"} attributes={{ "data-mcp-enabled": "" }} onChange={(next) => void setEnabled(next)} />
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("enterpriseMcpClientsTitle")}</strong>
          <span>{t("enterpriseMcpClientsDescription")}</span>
        </div>
        <button type="button" className={local.addLink} disabled={!workspaceId} data-mcp-client-add onClick={() => setApproving(true)}>
          <Plus size="small" aria-hidden="true" />{t("enterpriseMcpApprove")}
        </button>
      </div>
      <table className={local.table}>
        <thead>
          <tr><th>{t("enterpriseMcpClientName")}</th><th>{t("enterpriseMcpClientId")}</th><th aria-label={t("domainActionsColumn")} /></tr>
        </thead>
        <tbody>
          {clients.length ? clients.map((client) => (
            <tr key={client.clientId}>
              <td><strong>{client.name}</strong></td>
              <td>{client.clientId}</td>
              <td className={local.actions}>
                <SettingsRowMenu label={t("organizationMemberMenu", { name: client.name })} data={{ "data-mcp-client-menu": client.clientId }} rows={[
                  { key: "rename", label: t("enterpriseRename"), run: () => setRenaming(client) },
                  { key: "remove", label: t("enterpriseRemove"), danger: true, disabled: busy === `mcp:remove:${client.clientId}`, run: () => void remove(client) },
                ]} />
              </td>
            </tr>
          )) : <tr><td colSpan={3} className={local.empty}>{t("enterpriseMcpNoClients")}</td></tr>}
        </tbody>
      </table>
      <EnterpriseActionFeedback result={approving || renaming ? { ...result, action: "" } : result} actions={["mcp:"]} />
      {approving ? (
        <ClientDialog title={t("enterpriseMcpApprove")} busy={busy === "mcp:approve"} result={result} withId action="mcp:approve"
          onSave={approve} onClose={() => setApproving(false)} />
      ) : null}
      {renaming ? (
        <ClientDialog title={t("enterpriseRename")} initialName={renaming.name} busy={busy === `mcp:rename:${renaming.clientId}`} result={result}
          action="mcp:rename:" onSave={(_id, name) => rename(renaming, name)} onClose={() => setRenaming(null)} />
      ) : null}
    </section>
  );
}

function ClientDialog({ title, initialName = "", withId, busy, result, action, onSave, onClose }: {
  title: string;
  initialName?: string;
  withId?: boolean;
  busy: boolean;
  result: EnterpriseActionResult;
  action: string;
  onSave(clientId: string, name: string): Promise<boolean>;
  onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [clientId, setClientId] = useState("");
  const [name, setName] = useState(initialName);
  const idRef = useRef<HTMLInputElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (await onSave(clientId.trim(), name.trim())) onClose();
  };
  return (
    <SettingsFormDialog title={title} busy={busy} initialFocus={withId ? idRef : nameRef} onClose={onClose}>
      <form className={dialogs.dialogBody} onSubmit={(event) => void submit(event)} data-mcp-client-dialog>
        {withId ? (
          <label className={local.dialogField}>
            {t("enterpriseMcpClientId")}
            <input ref={idRef} required value={clientId} placeholder={t("enterpriseMcpClientIdPlaceholder")}
              onChange={(event) => setClientId(event.target.value)} />
          </label>
        ) : null}
        <label className={local.dialogField}>
          {t("enterpriseMcpClientName")}
          <input ref={nameRef} required={!withId} value={name} onChange={(event) => setName(event.target.value)} />
        </label>
        <EnterpriseActionFeedback result={result} actions={[action]} />
        <div className={dialogs.dialogActions}>
          <button type="button" className={dialogs.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="submit" className={dialogs.primary} disabled={busy}>{withId ? t("enterpriseMcpApprove") : t("enterpriseSaveSso")}</button>
        </div>
      </form>
    </SettingsFormDialog>
  );
}
