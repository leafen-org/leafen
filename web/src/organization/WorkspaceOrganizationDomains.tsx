import { FormEvent, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { confirmAction } from "@/components/ConfirmActionDialog";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { Plus } from "@/icons/leafen";
import { displayDateTime } from "@/lib/displayDate";
import { useOverlayLayer } from "@/lib/overlayLayer";
import type { OrganizationSettingsMutation } from "@/lib/workspaceOrganizationSettings";
import {
  organizationSettingsActions,
  type OrganizationSettingsDomain,
} from "@/organizationWire.generated";
import { CopyableValue } from "@/components/settings/CopyableValue";
import local from "@/components/settings/SettingsRows.module.css";
import { SettingsRowMenu } from "@/components/settings/SettingsRowMenu";
import own from "./WorkspaceOrganizationDomains.module.css";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/** What the last check of a domain found, in words (2026-10-16: "DNS 확인" said nothing). */
function domainCheckText(item: OrganizationSettingsDomain, t: Translate) {
  const time = displayDateTime(item.verificationCheckedAt);
  const reason = item.verificationErrorCode === "record_mismatch"
    ? t("domainCheckMismatch", { found: item.verificationFound })
    : item.verificationErrorCode === "lookup_failed" ? t("domainCheckLookupFailed")
      : t("domainCheckMissing");
  return `${t("domainCheckedAt", { time })} ${reason}`;
}

/** "새 도메인 추가", as Notion's (seen 2026-11-05; 2026-11-08 its one button):
 *  what it is for, the domain, and only "다음". */
function DomainAddDialog({ onAdd, onClose }: { onAdd: (domain: string) => void | Promise<void>; onClose: () => void }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [domain, setDomain] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose, initialFocus: inputRef,
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!domain.trim()) return;
    void onAdd(domain.trim());
    onClose();
  };
  return createPortal(
    <div ref={overlayRef} className={dialogs.overlay} style={{ zIndex }} data-domain-add-dialog>
      <button type="button" className={dialogs.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={onClose} />
      <section ref={dialogRef} className={`${dialogs.dialog} ${own.dialog}`} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <h2 id={titleId} className={own.title}>{t("domainAddTitle")}</h2>
        <form onSubmit={submit}>
          <p className={own.words}>{t("domainAddWords")}</p>
          <label className={own.field}>
            <input ref={inputRef} type="text" value={domain} aria-label={t("ariaOrgDomain")} placeholder="example.com"
              autoComplete="off" onChange={(event) => setDomain(event.target.value)} />
          </label>
          <button type="submit" className={own.next} disabled={!domain.trim()}>{t("domainAddNext")}</button>
        </form>
      </section>
    </div>,
    document.body,
  );
}

/**
 * Notion's "인증된 도메인" (seen 2026-11-05): the words and "+ 도메인 추가" in a
 * row, then a table of domains and their states. A domain still to verify says
 * what to publish under its row.
 */
export function WorkspaceOrganizationDomains({
  domains,
  canManage,
  disabled,
  mutate,
}: {
  domains: OrganizationSettingsDomain[];
  canManage: boolean;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [adding, setAdding] = useState(false);
  const remove = async (item: OrganizationSettingsDomain) => {
    if (!await confirmAction({
      message: t("domainRemoveConfirm", { domain: item.domain }), detail: t("domainRemoveConfirmDetail"),
      confirmLabel: t("remove"), danger: true,
    })) return;
    void mutate({ action: organizationSettingsActions.domainRemove, targetId: item.id });
  };
  return (
    <div className={local.block}>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("verifiedDomainsTitle")}</strong>
          <span>{t("verifiedDomainsDescription")}</span>
        </div>
        {canManage ? (
          <button type="button" className={local.addLink} disabled={disabled} data-domain-add onClick={() => setAdding(true)}>
            <Plus size="small" aria-hidden="true" />{t("addDomain")}
          </button>
        ) : null}
      </div>
      <table className={`${local.table} ${own.domains}`}>
        <thead>
          <tr><th>{t("domainsTitle")}</th><th>{t("domainStatusColumn")}</th><th aria-label={t("domainActionsColumn")} /></tr>
        </thead>
        <tbody>
          {domains.length ? domains.map((item) => {
            const verified = item.status === "verified";
            return (
              <tr key={item.id}>
                <td>
                  <strong>{item.domain}</strong>
                  {!verified && item.recordName && item.recordValue ? (
                    <span className={local.verification}>
                      {t("dnsTxtInstructions")}
                      <CopyableValue value={item.recordName} />
                      <CopyableValue value={item.recordValue} />
                      {item.verificationCheckedAt ? (
                        <span role="status" data-domain-check={item.verificationErrorCode || "pending"}>
                          {domainCheckText(item, t)}
                        </span>
                      ) : null}
                    </span>
                  ) : null}
                </td>
                <td>{verified ? t("statusVerified") : t("statusPending")}</td>
                <td className={local.actions}>
                  {canManage ? (
                    <SettingsRowMenu label={t("organizationMemberMenu", { name: item.domain })} data={{ "data-domain-menu": item.id }} rows={[
                      ...(!verified ? [{ key: "check", label: t("checkVerification"), disabled,
                        run: () => void mutate({ action: organizationSettingsActions.domainVerify, targetId: item.id }) }] : []),
                      { key: "remove", label: t("remove"), danger: true, disabled, run: () => void remove(item) },
                    ]} />
                  ) : null}
                </td>
              </tr>
            );
          }) : <tr><td colSpan={3} className={local.empty}>{t("noDomains")}</td></tr>}
        </tbody>
      </table>
      {adding ? (
        <DomainAddDialog onClose={() => setAdding(false)}
          onAdd={(domain) => mutate({ action: organizationSettingsActions.domainAdd, value: domain })} />
      ) : null}
    </div>
  );
}
