"use client";

import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { AppSelect } from "@/components/AppSelect";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { SettingsFormDialog } from "@/components/settings/SettingsFormDialog";
import local from "@/components/settings/SettingsRows.module.css";
import { SettingsSwitchRow } from "@/components/settings/SettingsSwitchRow";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import type { DLPContentScanMode, DLPPolicy, DLPPolicyInput } from "@/enterpriseDLPWire.generated";
import {
  enterpriseSplitLines,
  type EnterpriseActionRunner,
  type EnterpriseSettingsService,
} from "@/lib/enterpriseSettingsAdapter";
import { EnterpriseActionFeedback, type EnterpriseActionResult } from "./EnterpriseActionFeedback";

const BLOCKS = ["blockPublicSharing", "blockExternalSharing", "blockFileDownloads", "blockExports"] as const;

/**
 * "데이터 유출 방지": a switch for the whole, then what it does while on. Each
 * row saves at once, and what shows is what was saved.
 */
export function EnterpriseDataLossPrevention({ policy, busy, result, runAction, service }: {
  policy: DLPPolicy;
  busy: string;
  result: EnterpriseActionResult;
  runAction: EnterpriseActionRunner;
  service: EnterpriseSettingsService;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [dlp, setDlp] = useState(policy);
  const [editingTerms, setEditingTerms] = useState(false);
  useEffect(() => setDlp(policy), [policy]);
  const save = async (change: Partial<DLPPolicyInput>) => {
    const next = await runAction("dlp", (context) => service.updateDLP(context, { ...dlp, ...change }));
    if (next) setDlp(next);
    return Boolean(next);
  };
  const saving = busy === "dlp";
  return (
    <section className={styles.section} role="region" aria-label={t("enterpriseDlpTitle")} data-enterprise-dlp>
      <div className={styles.sectionTitle}>{t("enterpriseDlpTitle")}</div>
      <SettingsSwitchRow title={t("enterpriseEnableDlp")} description={t("enterpriseDlpDescription")} checked={dlp.enabled}
        disabled={saving} attributes={{ "data-dlp-enabled": "" }} onChange={(enabled) => void save({ enabled })} />
      {/* Off, it is one switch; what it does shows once it is on. */}
      {dlp.enabled ? <>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseContentMode")}</strong>
            <span>{t("enterpriseContentModeDescription")}</span>
          </div>
          <AppSelect aria-label={t("enterpriseContentMode")} look="setting" value={dlp.contentScanMode} disabled={saving}
            data-dlp-content-mode onChange={(event) => void save({ contentScanMode: event.target.value as DLPContentScanMode })}>
            <option value="block">{t("enterpriseBlock")}</option>
            <option value="off">{t("enterpriseOff")}</option>
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseSensitiveTerms")}</strong>
            <span>{dlp.sensitiveTerms.length
              ? t("enterpriseSensitiveTermsCount", { count: dlp.sensitiveTerms.length })
              : t("enterpriseSensitiveTermsNone")}</span>
          </div>
          <button type="button" className={local.rowButton} disabled={saving} data-dlp-terms-edit
            onClick={() => setEditingTerms(true)}>
            {t("enterpriseSensitiveTermsEdit")}
          </button>
        </div>
        {BLOCKS.map((key) => (
          <SettingsSwitchRow key={key} title={t(`enterprise_${key}`)} description={t(`enterprise_${key}Description`)}
            checked={dlp[key]} disabled={saving} attributes={{ "data-dlp-block": key }}
            onChange={(next) => void save({ [key]: next })} />
        ))}
      </> : null}
      <EnterpriseActionFeedback result={editingTerms ? { ...result, action: "" } : result} actions={["dlp"]} />
      {editingTerms ? (
        <TermsDialog terms={dlp.sensitiveTerms} busy={saving} result={result}
          onSave={(sensitiveTerms) => save({ sensitiveTerms })} onClose={() => setEditingTerms(false)} />
      ) : null}
    </section>
  );
}

function TermsDialog({ terms, busy, result, onSave, onClose }: {
  terms: readonly string[];
  busy: boolean;
  result: EnterpriseActionResult;
  onSave(terms: string[]): Promise<boolean>;
  onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [draft, setDraft] = useState(terms.join("\n"));
  const fieldRef = useRef<HTMLTextAreaElement>(null);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (await onSave(enterpriseSplitLines(draft))) onClose();
  };
  return (
    <SettingsFormDialog title={t("enterpriseSensitiveTermsEdit")} busy={busy} initialFocus={fieldRef} onClose={onClose}>
      <form className={dialogs.dialogBody} onSubmit={(event) => void submit(event)} data-dlp-terms-dialog>
        <p className={dialogs.dialogNote}>{t("enterpriseSensitiveTermsHelp")}</p>
        <label className={local.dialogField}>
          {t("enterpriseSensitiveTerms")}
          <textarea ref={fieldRef} rows={6} value={draft} onChange={(event) => setDraft(event.target.value)} />
        </label>
        <EnterpriseActionFeedback result={result} actions={["dlp"]} />
        <div className={dialogs.dialogActions}>
          <button type="button" className={dialogs.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="submit" className={dialogs.primary} disabled={busy}>{t("enterpriseSaveSso")}</button>
        </div>
      </form>
    </SettingsFormDialog>
  );
}
