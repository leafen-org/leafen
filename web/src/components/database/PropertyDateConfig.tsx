import { useTranslation } from "react-i18next";
import { ALL_DAY_REMINDER_MINUTES } from "@/lib/dateReminders";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import { NotionSelect } from "./NotionSelect";
import { DATE_FORMATS, TIME_FORMATS } from "./PropertyDateEditorOptions";
import { AlarmIcon, CalendarViewIcon, ClockIcon } from "../icons";

/**
 * A date property's settings, as Notion's "속성 편집" (seen 2026-10-05):
 * its date format, its time format and the reminder a new date gets — none,
 * that day at 9:00, or a day, two days or a week before, its row marked by
 * an alarm clock (Notion's, seen 2026-11-11). The date format's row is marked
 * by the calendar view's dotted calendar, as Notion's (seen 2026-11-11). A
 * created or last edited time has the formats alone.
 */
export function PropertyDateConfig({ prop, reminder = true, onConfig }: {
  prop: DbProperty; reminder?: boolean; onConfig(config: PropertyConfig): void;
}) {
  const { t } = useTranslation(["propertyCell", "blockItem"]);
  const config = (prop.config ?? {}) as PropertyConfig & { defaultReminder?: string };
  function update(patch: Record<string, unknown>) {
    const next: Record<string, unknown> = { ...prop.config, ...patch };
    for (const [key, value] of Object.entries(patch)) if (value === undefined) delete next[key];
    onConfig(next as PropertyConfig);
  }
  const row = (marker: string, label: string, value: string, options: { value: string; label: string }[], change: (value: string) => void) => (
    <div data-date-property-setting={marker}>
      <NotionSelect ariaLabel={label} settingLabel={label}
        settingIcon={marker === "dateFormat" ? <CalendarViewIcon size="regular" /> : marker === "timeFormat" ? <ClockIcon size="small" /> : <AlarmIcon size="small" />}
        value={value} options={options} onChange={change} />
    </div>
  );
  return <>
    {row("dateFormat", t("propertyCell:dateFormat.label"), config.dateFormat || "full",
      DATE_FORMATS.map((format) => ({ value: format, label: t(`propertyCell:dateFormat.${format}`) })),
      (value) => update({ dateFormat: value === "full" ? undefined : value }))}
    {row("timeFormat", t("propertyCell:timeFormat.label"), config.timeFormat || "12",
      TIME_FORMATS.map((format) => ({ value: format, label: t(`propertyCell:timeFormat.${format}`) })),
      (value) => update({ timeFormat: value === "12" ? undefined : value }))}
    {reminder && row("defaultReminder", t("propertyCell:defaultReminder"), config.defaultReminder ?? "", [
      { value: "", label: t("propertyCell:reminderNone") },
      ...ALL_DAY_REMINDER_MINUTES.map((minutes) => ({ value: String(minutes), label: t(`blockItem:dateMention.reminder.allDay.${minutes}`) })),
    ], (value) => update({ defaultReminder: value || undefined }))}
  </>;
}
