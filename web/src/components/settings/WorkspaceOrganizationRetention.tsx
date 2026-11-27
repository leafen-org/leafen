import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { AppSelect } from "@/components/AppSelect";
import type { OrganizationSettingsMutation } from "@/lib/workspaceOrganizationSettings";
import { organizationSettingsActions, type OrganizationSettingsSummary } from "@/organizationWire.generated";
import rows from "./SettingsRows.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";
import { useSettingsUnsaved } from "./settingsUnsaved";

const YEAR = 365;

type Mode = "year" | "period" | "keep";

function modeOf(days: number): Mode {
  return days === 0 ? "keep" : days === YEAR ? "year" : "period";
}

/**
 * How long the organization keeps its updates feed and its page analytics:
 * a year unless it sets a period or keeps them (organization_settings_mutation.go).
 * The feed is the audit log's keepers', the analytics their viewers'.
 */
export function WorkspaceOrganizationRetention({ organization, disabled, mutate }: {
  organization: OrganizationSettingsSummary;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { capabilities } = organization;
  if (!capabilities.canViewAuditLog && !capabilities.canViewAnalytics) return null;
  return (
    <section className={`${styles.section} ${rows.rowStack}`} role="region" aria-label={t("activityRetention.title")} data-activity-retention>
      <div className={styles.sectionTitle}>{t("activityRetention.title")}</div>
      {capabilities.canViewAuditLog ? (
        <RetentionRow kind="feed" days={organization.feedRetentionDays} disabled={disabled}
          save={(value) => mutate({ action: organizationSettingsActions.feedRetention, value })} />
      ) : null}
      {capabilities.canViewAnalytics ? (
        <RetentionRow kind="analytics" days={organization.analyticsRetentionDays} disabled={disabled}
          save={(value) => mutate({ action: organizationSettingsActions.analyticsRetention, value })} />
      ) : null}
    </section>
  );
}

function RetentionRow({ kind, days, disabled, save }: {
  kind: "feed" | "analytics";
  days: number;
  disabled: boolean;
  save: (value: string) => void | Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [mode, setMode] = useState<Mode>(modeOf(days));
  const [draft, setDraft] = useState(String(days || YEAR));
  useEffect(() => {
    setMode(modeOf(days));
    setDraft(String(days || YEAR));
  }, [days]);
  const choose = (next: Mode) => {
    setMode(next);
    if (next === "year") void save(String(YEAR));
    if (next === "keep") void save("0");
  };
  const valid = /^[1-9]\d*$/.test(draft);
  useSettingsUnsaved(mode === "period" && draft !== String(days));
  return (
    <div data-activity-retention-kind={kind} data-activity-retention-days={days}>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t(`activityRetention.${kind}`)}</strong>
          <span>{t(`activityRetention.${kind}Help`)}</span>
        </div>
        <AppSelect aria-label={t(`activityRetention.${kind}`)} look="setting" value={mode} disabled={disabled}
          onChange={(event) => choose(event.target.value as Mode)}>
          <option value="year">{t("activityRetention.year")}</option>
          <option value="period">{t("activityRetention.period")}</option>
          <option value="keep">{t("activityRetention.keep")}</option>
        </AppSelect>
      </div>
      {mode === "period" ? (
        <form className={rows.inlineField} onSubmit={(event) => {
          event.preventDefault();
          if (valid) void save(draft);
        }}>
          <label>
            {t("activityRetention.days")}
            <input type="number" min={1} step={1} required value={draft} data-arrows="none" data-activity-retention-input onChange={(event) => setDraft(event.target.value)} />
          </label>
          <button type="submit" className={rows.rowButton} disabled={disabled || !valid || draft === String(days)}>
            {t("enterpriseSave")}
          </button>
        </form>
      ) : null}
    </div>
  );
}
