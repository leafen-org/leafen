import { useTranslation } from "react-i18next";
import type { AppSchedule } from "@/appPlatformWire.generated";
import { appText } from "./administratorAppsApi";
import { describeAppCron } from "./appPermissionWords";
import styles from "./AdministratorApps.module.css";
import rowStyles from "./AppScheduleRow.module.css";
import { AppSelect } from "@/components/AppSelect";
import { SwitchMark } from "@/components/Switch";
import { gmtOffsetText } from "@/lib/timeZones";

const zones = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf?.("timeZone") ?? [];
  } catch {
    return [];
  }
})();

// The server's "UTC+09:00" as the time zone choices write offsets
// ("(GMT+9:00) 서울", "GMT+05:30"; 2026-10-19: the two forms stood side by side).
function gmtOffset(zone: string) {
  const match = /^UTC([+-])(\d{2}):(\d{2})$/u.exec(zone);
  return match ? gmtOffsetText(match[1], match[2], match[3]) : zone;
}

/**
 * One of an app's schedules for the administrator (docs/app-platform.md
 * 11): on or off, and the time zone its cron runs in — the app's own
 * choice (its declared zone, else each person's for a per-person schedule,
 * else the server's), the server's, or another.
 */
export function AppScheduleRow({ schedule, serverZone, busy, when, onChange }: {
  schedule: AppSchedule;
  serverZone: string;
  busy: boolean;
  when: (at: number) => string;
  onChange: (change: { enabled?: boolean; timeZone?: string }) => void;
}) {
  const { t, i18n } = useTranslation("serverConsole");
  const server = t("apps.jobs.zone.server", { offset: gmtOffset(serverZone) });
  const appDefault = schedule.declaredTimeZone || (schedule.perPerson ? t("apps.jobs.zone.eachPerson") : server);
  const zone = schedule.timeZone === "server" ? server : schedule.timeZone || appDefault;
  const name = appText(schedule.name ?? {}, i18n.language) || schedule.key;
  const choices = schedule.timeZone && schedule.timeZone !== "server" && !zones.includes(schedule.timeZone) ? [schedule.timeZone, ...zones] : zones;
  return (
    <div className={styles.jobRow} data-app-schedule={schedule.key}>
      <span className={styles.secretName}>
        <strong>{name}</strong>
        <span title={schedule.cron}>{[t("apps.jobs.cronZone", { cron: describeAppCron(schedule.cron, t, i18n.language), zone }), ...(schedule.perPerson && (schedule.timeZone || schedule.declaredTimeZone) ? [t("apps.jobs.perPerson")] : []),
          ...(schedule.optedOut ? [t("apps.jobs.optedOut", { count: schedule.optedOut })] : []),
          schedule.lastQueuedAt ? t("apps.jobs.lastQueued", { date: when(schedule.lastQueuedAt) }) : t("apps.jobs.never")].join(" · ")}</span>
        <label className={rowStyles.zone}>
          <span>{t("apps.jobs.zone.label")}</span>
          <AppSelect aria-label={t("apps.jobs.zone.label")} value={schedule.timeZone} disabled={busy} data-app-schedule-zone={schedule.key}
            onChange={(event) => onChange({ timeZone: event.target.value })}>
            <option value="">{t("apps.jobs.zone.default", { zone: appDefault })}</option>
            <option value="server">{server}</option>
            {choices.map((choice) => <option key={choice} value={choice}>{choice}</option>)}
          </AppSelect>
        </label>
      </span>
      {/* On or off as it turns, at the row's end as 설정's switches (2026-11-06). */}
      <button type="button" role="switch" aria-checked={schedule.enabled} aria-label={name} disabled={busy} className={rowStyles.toggle}
        onClick={() => onChange({ enabled: !schedule.enabled })}>
        <SwitchMark on={schedule.enabled} />
      </button>
    </div>
  );
}
