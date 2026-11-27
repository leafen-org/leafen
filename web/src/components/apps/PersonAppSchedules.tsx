import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppPersonSchedule, AppPersonScheduleList } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { keptAppsList, useAppsListReading } from "@/lib/appsChangedEvents";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { readPersonSchedules, setPersonSchedule } from "./personAppsApi";
import { scheduleText } from "./scheduleText";
import adminStyles from "@/components/admin/AdministratorApps.module.css";
import styles from "./PersonAppsPage.module.css";

/**
 * The schedules apps run for the person (docs/app-platform.md 11): each
 * per-person schedule of an app they use, with its time, to be turned off
 * for themselves and back on.
 */
export function PersonAppSchedules({ session }: { session: Session }) {
  const { t, i18n } = useTranslation("common");
  // The list read last in this tab shows at once, the server's answer replacing it.
  const [schedules, setSchedules] = useState<AppPersonSchedule[]>(
    () => keptAppsList<AppPersonScheduleList>(session, "schedules")?.schedules ?? []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Read again when an app is installed, removed or given to the person.
  const adopt = useAppsListReading(session, "schedules", readPersonSchedules, (list) => setSchedules(list.schedules));

  async function change(schedule: AppPersonSchedule, on: boolean) {
    setBusy(true);
    setError("");
    try {
      adopt(await setPersonSchedule(session, schedule.installId, schedule.key, on));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appPeople.schedules.failed")));
    } finally {
      setBusy(false);
    }
  }

  if (!schedules.length && !error) return null;
  return (
    <section className={styles.credentials} data-person-app-schedules>
      <h2 className={styles.heading}>{t("appPeople.schedules.title")}</h2>
      <p className={styles.note}>{t("appPeople.schedules.note")}</p>
      {error ? <div className={adminStyles.error} role="alert">{error}</div> : null}
      <ul className={styles.credentialList}>
        {schedules.map((schedule) => {
          const name = `${appText(schedule.appName, i18n.language)} · ${appText(schedule.name, i18n.language) || schedule.key}`;
          return (
            <li key={`${schedule.installId}/${schedule.key}`} className={styles.credential}>
              <label className={styles.credentialText}>
                <span className={styles.credentialName}>
                  <input type="checkbox" checked={schedule.on} disabled={busy} data-person-app-schedule={`${schedule.installId}/${schedule.key}`}
                    onChange={(event) => void change(schedule, event.target.checked)} />{" "}{name}
                </span>
                <span className={styles.note}>{t("appPeople.schedules.when", {
                  cron: scheduleText(schedule.cron, i18n.language, t), zone: schedule.timeZone || t("appPeople.schedules.myZone"),
                })}</span>
              </label>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
