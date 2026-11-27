"use client";

import { type FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { AppSelect } from "@/components/AppSelect";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { SettingsFormDialog } from "@/components/settings/SettingsFormDialog";
import local from "@/components/settings/SettingsRows.module.css";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import {
  enterpriseDownloadText,
  type EnterpriseActionRunner,
  type EnterpriseSettingsService,
  type EnterpriseSettingsSnapshot,
} from "@/lib/enterpriseSettingsAdapter";
import { EnterpriseActionFeedback, type EnterpriseActionResult } from "./EnterpriseActionFeedback";
import { EnterpriseScopePicker, type EnterpriseScopeChoice } from "./EnterpriseScopePicker";

type Shared = {
  busy: string;
  result: EnterpriseActionResult;
  runAction: EnterpriseActionRunner;
  service: EnterpriseSettingsService;
  snapshot: EnterpriseSettingsSnapshot;
  setNotice(notice: string): void;
};

/** 0 keeps every record, which the keep choice says; the field asks for a period. */
function daysDraft(days: number) {
  return days === 0 ? "" : String(days);
}

/**
 * "감사 로그": how long its records stay, and where to take them out — the
 * organization's audit log, filtered as it is read (one place to export,
 * decided 2026-11-06).
 */
export function EnterpriseAuditExports({ busy, result, runAction, service, snapshot, setNotice, onOpenAuditLog }: Shared & {
  onOpenAuditLog?(): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const retention = snapshot.auditRetention.retentionDays;
  const [saved, setSaved] = useState(retention);
  const [keep, setKeep] = useState(retention === 0);
  const [days, setDays] = useState(daysDraft(retention));
  const [daysError, setDaysError] = useState("");
  useEffect(() => {
    setSaved(retention);
    setKeep(retention === 0);
    setDays(daysDraft(retention));
  }, [retention]);

  const save = async (retentionDays: number) => {
    const next = await runAction("audit", (context) => service.updateAuditRetention(context, { retentionDays }));
    if (!next) return;
    setSaved(next.retentionDays);
    setKeep(next.retentionDays === 0);
    setDays(daysDraft(next.retentionDays));
    setNotice(t("enterpriseSaved"));
  };
  return (
    <section className={styles.section} role="region" aria-label={t("enterpriseAuditTitle")} data-enterprise-audit>
      <div className={styles.sectionTitle}>{t("enterpriseAuditTitle")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("enterpriseRetention")}</strong>
          <span>{t("enterpriseRetentionDescription")}</span>
        </div>
        {/* The days stay inside their row, under the choice, so the next row keeps its spacing. */}
        <div className={local.retentionControls}>
          <AppSelect aria-label={t("enterpriseRetention")} look="setting" value={keep ? "keep" : "period"} data-audit-retention-mode
            disabled={busy === "audit"} menuWidth={220} onChange={(event) => {
              const nextKeep = event.target.value === "keep";
              setKeep(nextKeep);
              if (nextKeep && saved !== 0) void save(0);
            }}>
            <option value="keep">{t("enterpriseRetentionKeep")}</option>
            <option value="period">{t("enterpriseRetentionPeriod")}</option>
          </AppSelect>
          {!keep ? (
            <form className={local.inlineField} noValidate onSubmit={(event) => {
              event.preventDefault();
              // Said under the field in the page's own words, not by the browser's bubble.
              const value = Number(days);
              if (!Number.isInteger(value) || value < 1) { setDaysError(t("enterpriseRetentionDaysMin")); return; }
              setDaysError("");
              void save(value);
            }}>
              <label>
                {t("enterpriseRetentionDays")}
                <input type="number" min={1} step={1} required value={days} data-audit-retention-days
                  aria-invalid={daysError ? true : undefined} onChange={(event) => { setDays(event.target.value); setDaysError(""); }} />
              </label>
              <button type="submit" className={local.rowButton} disabled={busy === "audit" || !days || days === daysDraft(saved)}>
                {t("enterpriseSave")}
              </button>
            </form>
          ) : null}
          {!keep && daysError ? <div className={local.fieldError} role="alert">{daysError}</div> : null}
        </div>
      </div>
      {onOpenAuditLog ? (
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("enterpriseExportAudit")}</strong>
            <span>{t("enterpriseExportAuditWhere")}</span>
          </div>
          <button type="button" className={local.rowButton} data-audit-export-open onClick={onOpenAuditLog}>
            {t("enterpriseOpenAuditLog")}
          </button>
        </div>
      ) : null}
      <EnterpriseActionFeedback result={result} actions={["audit"]} />
    </section>
  );
}

/** "eDiscovery 내보내기": a search over the organization's content, its answer as a file. */
export function EnterpriseDiscovery({ organizationId, busy, result, runAction, service, snapshot, setNotice }: Shared & { organizationId: string }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [open, setOpen] = useState(false);
  const [count, setCount] = useState(snapshot.discoveryExports.length);
  const [query, setQuery] = useState("");
  const [custodians, setCustodians] = useState<EnterpriseScopeChoice[]>([]);
  const queryRef = useRef<HTMLInputElement>(null);
  useEffect(() => setCount(snapshot.discoveryExports.length), [snapshot.discoveryExports.length]);
  const working = busy === "discovery";
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const next = await runAction("discovery", (context) => service.exportDiscovery(context, {
      query: query || undefined,
      userIds: custodians.map((choice) => choice.id),
      includeTrashed: true,
      format: "jsonl",
    }));
    if (!next) return;
    const saved = await enterpriseDownloadText(next);
    setCount((current) => current + (next.record.duplicate ? 0 : 1));
    if (saved === "saved") setNotice(t("enterpriseDiscoveryExported", { count: next.record.itemCount }));
    setQuery("");
    setCustodians([]);
    setOpen(false);
  };
  return (
    <section className={styles.section} role="region" aria-label={t("enterpriseDiscoveryTitle")} data-enterprise-discovery>
      <div className={styles.sectionTitle}>{t("enterpriseDiscoveryTitle")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("enterpriseExportDiscovery")}</strong>
          <span>{t("enterpriseDiscoveryDescription")}</span>
          <span>{t("enterpriseDiscoveryCount", { count })}</span>
        </div>
        <button type="button" className={local.rowButton} data-discovery-open onClick={() => setOpen(true)}>
          {t("enterpriseExportDiscovery")}
        </button>
      </div>
      <EnterpriseActionFeedback result={open ? { ...result, action: "" } : result} actions={["discovery"]} />
      {open ? (
        <SettingsFormDialog title={t("enterpriseExportDiscovery")} busy={working} wide initialFocus={queryRef} onClose={() => setOpen(false)}>
          <form className={dialogs.dialogBody} onSubmit={(event) => void submit(event)} data-discovery-dialog>
            <p className={dialogs.dialogNote}>{t("enterpriseDiscoveryDescription")}</p>
            <label className={local.dialogField}>
              {t("enterpriseSearchTerms")}
              <input ref={queryRef} value={query} onChange={(event) => setQuery(event.target.value)} />
            </label>
            <EnterpriseScopePicker kind="custodian" organizationId={organizationId} workspaces={snapshot.workspaces}
              label={t("enterpriseCustodianIds")} selected={custodians} onChange={setCustodians} />
            <EnterpriseActionFeedback result={result} actions={["discovery"]} />
            <div className={dialogs.dialogActions}>
              <button type="button" className={dialogs.cancel} disabled={working} onClick={() => setOpen(false)}>{t("common:actions.cancel")}</button>
              <button type="submit" className={dialogs.primary} disabled={working}>{t("enterpriseExportDiscoveryRun")}</button>
            </div>
          </form>
        </SettingsFormDialog>
      ) : null}
    </section>
  );
}
