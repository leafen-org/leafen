import { FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { FileUsageReportMaintenanceRun } from "@/fileUsageReportWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session, Workspace } from "@/types";
import { displayDateTime } from "@/lib/displayDate";
import rows from "./SettingsRows.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";
import {
  formatStorageLimitDraft,
  parseStorageLimitDraft,
} from "./workspaceSettingsStorageLimitModel";
import { useWorkspaceUsageReport } from "./useWorkspaceUsageReport";
import { useSettingsUnsaved } from "./settingsUnsaved";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { formatFileSize } from "@/components/database/files";
import { ListNotice } from "./SettingsRowNotice";

/** A size in the one way the app writes sizes (files.ts, as Notion: KiB, MiB). */
function formatBytes(bytes: number | undefined) {
  return formatFileSize(Number.isFinite(bytes) ? Math.max(0, bytes ?? 0) : 0);
}

function latestMaintenanceRun(runs: FileUsageReportMaintenanceRun[]) {
  return [...runs].sort((left, right) => right.startedAt - left.startedAt)[0];
}

export function WorkspaceUsageReport({
  session,
  workspace,
}: {
  session: Session;
  workspace: Workspace;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const usage = useWorkspaceUsageReport({
    session,
    workspace,
    errorFallback: t("errLoadStorageReport"),
  });
  const canManageOrganizationBilling = usage.organization?.capabilities?.canManageBilling === true;
  const [storageLimitDraft, setStorageLimitDraft] = useState(
    formatStorageLimitDraft(usage.organization?.storageLimitBytes),
  );
  // A limit typed and not saved is asked about before settings close or move on.
  useSettingsUnsaved(storageLimitDraft !== formatStorageLimitDraft(usage.organization?.storageLimitBytes), (section) => section === "usage");
  const [cleanupError, setCleanupError] = useState("");
  const [storageLimitError, setStorageLimitError] = useState("");
  const report = usage.report;
  const organizationReport = usage.organizationReport;
  const latestRun = latestMaintenanceRun(report?.maintenanceRuns ?? []);
  const topScopes = [...(report?.byScope ?? [])]
    .sort((left, right) => right.bytes - left.bytes)
    .slice(0, 4);
  const organizationStorageLimit = organizationReport?.storageLimitBytes
    ?? usage.organization?.storageLimitBytes
    ?? null;
  const topWorkspaces = organizationReport?.byWorkspace.slice(0, 4) ?? [];
  // A limit reached stops uploads, and one nearly reached says what is left
  // (2026-10-13 decision on limits that stop people; 2026-10-16: 1.92 MB of a
  // 1 MB limit showed nothing).
  const organizationStorageState = organizationReport && organizationStorageLimit
    ? organizationReport.totals.activeStorageBytes >= organizationStorageLimit
      ? { kind: "reached" as const, remaining: 0 }
      : organizationReport.totals.activeStorageBytes >= organizationStorageLimit * 0.9
        ? { kind: "near" as const, remaining: organizationStorageLimit - organizationReport.totals.activeStorageBytes }
        : null
    : null;
  const organizationBusy = usage.storageLimitBusy ? "policy:storageLimit" : "";

  useEffect(() => {
    setStorageLimitDraft(formatStorageLimitDraft(usage.organization?.storageLimitBytes));
    setStorageLimitError("");
  }, [usage.organization?.id, usage.organization?.storageLimitBytes]);

  async function cleanupExpiredUploads() {
    setCleanupError("");
    try {
      const cleanup = await usage.runCleanup();
      if (cleanup.status !== "completed" || cleanup.failedObjects > 0) {
        setCleanupError(t("errCleanupUploads"));
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setCleanupError(settingsErrorMessage(error, t("errCleanupUploads")));
    }
  }

  async function saveOrganizationStorageLimit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!usage.organization?.id || !canManageOrganizationBilling) return;
    setStorageLimitError("");
    let storageLimitBytes: number | null;
    try {
      storageLimitBytes = parseStorageLimitDraft(
        storageLimitDraft,
        t("storageLimitInvalid"),
      );
    } catch {
      setStorageLimitError(t("storageLimitInvalid"));
      return;
    }
    try {
      const organization = await usage.updateStorageLimit(storageLimitBytes);
      if (organization) {
        setStorageLimitDraft(formatStorageLimitDraft(organization.storageLimitBytes));
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setStorageLimitError(settingsErrorMessage(error, t("errUpdateStorageLimit")));
    }
  }

  return (
    <>
      <section id="usage" className={styles.section} aria-label={t("navUsage")} aria-busy={usage.loading || usage.cleanupBusy || usage.storageLimitBusy}>
        <SettingsSectionTitle className={styles.sectionTitle} restates>{t("navUsage")}</SettingsSectionTitle>
        {storageLimitError || cleanupError ? (
          <div className={styles.notice} role="alert">
            {storageLimitError || cleanupError}
          </div>
        ) : null}
        {/* A failed read of the report, with a way to read it again. */}
        {usage.error ? <ListNotice role="alert" onRetry={usage.refresh} data-usage-read-failed>{usage.error}</ListNotice> : null}
        {!report && !usage.error ? <p className={rows.lead} role="status">{t("loadingUsage")}</p> : null}
      </section>

      {organizationReport ? (
        <section className={styles.section} aria-label={t("orgUsageTitle")} data-usage-organization>
          <div className={styles.sectionTitle}>{t("orgUsageTitle")}</div>
          <UsageRow label={t("usageStorage")} value={organizationStorageLimit
            ? t("usageOfLimit", { used: formatBytes(organizationReport.totals.activeStorageBytes), limit: formatBytes(organizationStorageLimit) })
            : formatBytes(organizationReport.totals.activeStorageBytes)}
            description={t("usageOrganizationStorageDescription")} />
          {organizationStorageState ? (
            <p className={styles.storageLimitState} role="status" data-storage-limit-state={organizationStorageState.kind}>
              {organizationStorageState.kind === "reached"
                ? t("storageLimitReached")
                : t("storageLimitNear", { remaining: formatBytes(organizationStorageState.remaining) })}
            </p>
          ) : null}
          <UsageRow label={t("filesTile")} value={String(organizationReport.totals.files)} />
          <UsageRow label={t("workspacesTile")} value={String(organizationReport.workspaceCount)} />
          {canManageOrganizationBilling ? (
            <form className={styles.securityRow} onSubmit={saveOrganizationStorageLimit} data-usage-limit>
              <div className={styles.securityInstruction}>
                <strong>{t("storageLimitField")}</strong>
                <span>{t("usageLimitDescription")}</span>
              </div>
              <span className={rows.inlineField}>
                <label>
                  <input value={storageLimitDraft} aria-label={t("ariaOrgStorageLimit")} inputMode="decimal" placeholder={t("noLimit")}
                    disabled={organizationBusy === "policy:storageLimit" || usage.cleanupBusy}
                    onChange={(event) => setStorageLimitDraft(event.target.value)} />
                  MiB
                </label>
                <button type="submit" className={rows.rowButton}
                  disabled={organizationBusy === "policy:storageLimit" || usage.cleanupBusy
                    || storageLimitDraft === formatStorageLimitDraft(usage.organization?.storageLimitBytes)}>
                  {t("common:actions.save")}
                </button>
              </span>
            </form>
          ) : null}
          {topWorkspaces.length ? (
            <table className={rows.table}>
              <thead><tr><th>{t("workspacesTile")}</th><th>{t("filesTile")}</th><th>{t("usageStorage")}</th></tr></thead>
              <tbody>
                {topWorkspaces.map((item) => (
                  <tr key={item.workspaceId}>
                    <td>{item.name || item.domain || t("untitledWorkspace")}</td>
                    <td>{item.totals.files}</td>
                    <td>{formatBytes(item.totals.activeStorageBytes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
        </section>
      ) : null}

      {report ? (
        <section className={styles.section} aria-label={t("workspaceUsageTitle")} data-usage-workspace>
          <div className={styles.sectionTitle}>{t("workspaceUsageTitle")}</div>
          <UsageRow label={t("usageStorage")} value={formatBytes(report.totals.activeStorageBytes)} />
          <UsageRow label={t("filesTile")} value={String(report.totals.files)} />
          <UsageRow label={t("pendingTile")} value={String(report.pending.active)} description={t("usagePendingDescription")} />
          <div className={styles.securityRow} data-usage-cleanup>
            <div className={styles.securityInstruction}>
              <strong>{t("usageExpiredRow", { count: report.pending.expired })}</strong>
              <span>
                {latestRun
                  ? t("usageLastCleanup", {
                    date: displayDateTime(latestRun.startedAt),
                    result: t("cleanupResultSummary", { expired: latestRun.expired, failed: latestRun.failedObjects }),
                  })
                  : t("usageNoCleanup")}
              </span>
            </div>
            <button type="button" className={rows.rowButton} onClick={() => void cleanupExpiredUploads()}
              disabled={usage.loading || usage.cleanupBusy || usage.storageLimitBusy}>
              {t("cleanUp")}
            </button>
          </div>
          {topScopes.length ? (
            <table className={rows.table}>
              <thead><tr><th>{t("usageScopeColumn")}</th><th>{t("filesTile")}</th><th>{t("usageStorage")}</th></tr></thead>
              <tbody>
                {topScopes.map((scope) => (
                  <tr key={scope.key}>
                    <td>{scope.key === "uploads" ? t("scopeUploads") : scope.key}</td>
                    <td>{scope.count}</td>
                    <td>{formatBytes(scope.bytes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : <p className={rows.emptyLine}>{t("noFilesYet")}</p>}
        </section>
      ) : null}
    </>
  );
}

function UsageRow({ label, value, description }: { label: string; value: string; description?: string }) {
  return (
    <div className={styles.securityRow}>
      <div className={styles.securityInstruction}>
        <strong>{label}</strong>
        {description ? <span>{description}</span> : null}
      </div>
      <span className={rows.rowValue}>{value}</span>
    </div>
  );
}
