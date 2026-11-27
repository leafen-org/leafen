"use client";

import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { AppSelect } from "@/components/AppSelect";
import { confirmAction } from "@/components/ConfirmActionDialog";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { SettingsFormDialog } from "@/components/settings/SettingsFormDialog";
import local from "@/components/settings/SettingsRows.module.css";
import { SettingsRowMenu } from "@/components/settings/SettingsRowMenu";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import type { LegalHold, LegalHoldScopeKind } from "@/enterpriseLegalHoldWire.generated";
import { Plus } from "@/icons/leafen";
import type {
  EnterpriseActionRunner,
  EnterpriseSettingsService,
  EnterpriseSettingsSnapshot,
} from "@/lib/enterpriseSettingsAdapter";
import { EnterpriseActionFeedback, type EnterpriseActionResult } from "./EnterpriseActionFeedback";
import own from "./EnterpriseLegalHolds.module.css";
import { EnterpriseScopePicker, type EnterpriseScopeChoice } from "./EnterpriseScopePicker";

/** "법적 보존": what is held, and making and ending a hold. */
export function EnterpriseLegalHolds({ organizationId, busy, result, runAction, service, snapshot }: {
  organizationId: string;
  busy: string;
  result: EnterpriseActionResult;
  runAction: EnterpriseActionRunner;
  service: EnterpriseSettingsService;
  snapshot: EnterpriseSettingsSnapshot;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [holds, setHolds] = useState<readonly LegalHold[]>(snapshot.legalHolds);
  // What holds now, first, newest first; then those released (2026-11-06, this
  // screen being Leafen's own: an admin looks for what is in force).
  const ordered = [...holds].sort((left, right) => (
    Number(right.status === "active") - Number(left.status === "active") || right.createdAt - left.createdAt
  ));
  const workspaceNames = new Map(snapshot.workspaces.map((workspace) => [workspace.id, workspace.name]));
  // What a hold covers, said under its reason: the whole organization, the
  // workspaces by name, or how many people or pages.
  const scopeText = ({ scope }: LegalHold) => scope.all ? t("enterpriseEntireOrganization") : [
    scope.workspaceIds.length ? scope.workspaceIds.map((id) => workspaceNames.get(id) ?? t("enterpriseHoldScopeOtherWorkspace")).join(", ") : "",
    scope.userIds.length ? t("enterpriseHoldScopePeople", { count: scope.userIds.length }) : "",
    scope.pageIds.length ? t("enterpriseHoldScopePages", { count: scope.pageIds.length }) : "",
  ].filter(Boolean).join(" · ");
  const [creating, setCreating] = useState(false);
  useEffect(() => setHolds(snapshot.legalHolds), [snapshot.legalHolds]);
  const release = async (hold: LegalHold) => {
    if (!await confirmAction({
      message: t("enterpriseHoldReleaseConfirm", { name: hold.name }), detail: t("enterpriseHoldReleaseConfirmDetail"),
      confirmLabel: t("enterpriseRelease"), danger: true,
    })) return;
    const next = await runAction(`hold:${hold.id}`, (context) => service.releaseLegalHold(context, hold.id));
    if (next) setHolds(next);
  };
  return (
    <section className={styles.section} role="region" aria-label={t("enterpriseLegalTitle")} data-enterprise-legal-holds>
      <div className={styles.sectionTitle}>{t("enterpriseLegalTitle")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("enterpriseLegalHoldsRow")}</strong>
          <span>{t("enterpriseLegalDescription")}</span>
        </div>
        <button type="button" className={local.addLink} data-hold-add onClick={() => setCreating(true)}>
          <Plus size="small" aria-hidden="true" />{t("enterpriseCreateHold")}
        </button>
      </div>
      <table className={local.table}>
        <thead>
          <tr><th>{t("enterpriseHoldName")}</th><th aria-label={t("domainActionsColumn")} /></tr>
        </thead>
        <tbody>
          {ordered.length ? ordered.map((hold) => (
            <tr key={hold.id} data-legal-hold={hold.status} className={hold.status === "active" ? undefined : own.released}>
              <td>
                {/* No status column: a hold in force is what a hold is and says nothing (기준선 "기본값은
                    쓰지 않는다"); only a released one says so, by its name, as Notion marks an exception. */}
                <span className={own.nameLine}>
                  <strong>{hold.name}</strong>
                  {hold.status === "active" ? null : <span className={own.releasedTag} data-legal-hold-released>{t("enterpriseHoldStatusReleased")}</span>}
                </span>
                {hold.reason ? <span className={local.cellNote}>{hold.reason}</span> : null}
                <span className={local.cellNote} data-legal-hold-scope>{scopeText(hold)}</span>
              </td>
              <td className={local.actions}>
                {hold.status === "active" ? (
                  <SettingsRowMenu label={t("organizationMemberMenu", { name: hold.name })} data={{ "data-legal-hold-menu": hold.id }} rows={[
                    { key: "release", label: t("enterpriseRelease"), disabled: busy === `hold:${hold.id}`, run: () => void release(hold) },
                  ]} />
                ) : null}
              </td>
            </tr>
          )) : <tr><td colSpan={2} className={local.empty}>{t("enterpriseNoHolds")}</td></tr>}
        </tbody>
      </table>
      <EnterpriseActionFeedback result={creating ? { ...result, action: "" } : result} actions={["hold:"]} />
      {creating ? (
        <HoldDialog organizationId={organizationId} workspaces={snapshot.workspaces} busy={busy === "hold:create"} result={result}
          onClose={() => setCreating(false)}
          onCreate={async (input) => {
            const next = await runAction("hold:create", (context) => service.createLegalHold(context, input));
            if (!next) return false;
            setHolds(next);
            return true;
          }} />
      ) : null}
    </section>
  );
}

function HoldDialog({ organizationId, workspaces, busy, result, onCreate, onClose }: {
  organizationId: string;
  workspaces: readonly { id: string; name: string }[];
  busy: boolean;
  result: EnterpriseActionResult;
  onCreate(input: { name: string; reason: string; scope: Parameters<EnterpriseSettingsService["createLegalHold"]>[1]["scope"] }): Promise<boolean>;
  onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [name, setName] = useState("");
  const [reason, setReason] = useState("");
  const [kind, setKind] = useState<LegalHoldScopeKind>("all");
  const [choices, setChoices] = useState<EnterpriseScopeChoice[]>([]);
  const nameRef = useRef<HTMLInputElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const ids = choices.map((choice) => choice.id);
    const scope = kind === "all" ? { all: true }
      : kind === "workspace" ? { workspaceIds: ids }
        : kind === "page" ? { pageIds: ids }
          : { userIds: ids };
    if (await onCreate({ name, reason, scope })) onClose();
  };
  const scopeLabel = kind === "workspace" ? "enterpriseWorkspaces" : kind === "page" ? "enterprisePages" : "enterpriseCustodians";
  return (
    <SettingsFormDialog title={t("enterpriseCreateHold")} busy={busy} wide initialFocus={nameRef} onClose={onClose}>
      <form className={dialogs.dialogBody} onSubmit={(event) => void submit(event)} data-hold-dialog>
        <label className={local.dialogField}>
          {t("enterpriseHoldName")}
          <input ref={nameRef} required value={name} onChange={(event) => setName(event.target.value)} />
        </label>
        <label className={local.dialogField}>
          {t("enterpriseReason")}
          <textarea required rows={3} value={reason} onChange={(event) => setReason(event.target.value)} />
        </label>
        <div className={local.switchRow}>
          {t("enterpriseScope")}
          <AppSelect aria-label={t("enterpriseScope")} look="setting" value={kind} menuWidth={200} data-hold-scope
            onChange={(event) => { setKind(event.target.value as LegalHoldScopeKind); setChoices([]); }}>
            <option value="all">{t("enterpriseEntireOrganization")}</option>
            <option value="workspace">{t("enterpriseWorkspaces")}</option>
            <option value="custodian">{t("enterpriseCustodians")}</option>
            <option value="page">{t("enterprisePages")}</option>
          </AppSelect>
        </div>
        {kind !== "all" ? (
          <EnterpriseScopePicker kind={kind} organizationId={organizationId} workspaces={workspaces} label={t(scopeLabel)}
            selected={choices} onChange={setChoices} />
        ) : null}
        <EnterpriseActionFeedback result={result} actions={["hold:create"]} />
        <div className={dialogs.dialogActions}>
          <button type="button" className={dialogs.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="submit" className={dialogs.primary} disabled={busy || (kind !== "all" && choices.length === 0)}>
            {t("enterpriseCreateHold")}
          </button>
        </div>
      </form>
    </SettingsFormDialog>
  );
}
