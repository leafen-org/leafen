import type { TFunction } from "i18next";

const number = /^\d{1,2}$/u;

/**
 * A schedule's cron in plain words for the people it runs for — every few
 * minutes, every hour, every day, weekdays, some days of the week or a day
 * of the month at a time — or the cron itself when it says more.
 */
export function scheduleText(cron: string, language: string, t: TFunction<"common">) {
  const [minute, hour, day, month, weekday, ...rest] = cron.trim().split(/\s+/u);
  if (rest.length || month !== "*") return cron;
  const every = /^\*\/(\d{1,2})$/u.exec(minute ?? "");
  if (every && hour === "*" && day === "*" && weekday === "*") return t("appPeople.schedules.cron.minutes", { count: Number(every[1]) });
  if (!number.test(minute ?? "")) return cron;
  if (hour === "*" && day === "*" && weekday === "*") return t("appPeople.schedules.cron.hourly", { minute: minute.padStart(2, "0") });
  if (!number.test(hour ?? "")) return cron;
  const time = `${Number(hour)}:${minute.padStart(2, "0")}`;
  if (day === "*" && weekday === "*") return t("appPeople.schedules.cron.daily", { time });
  if (day === "*" && weekday === "1-5") return t("appPeople.schedules.cron.weekdays", { time });
  if (day === "*" && /^[0-7](,[0-7])*$/u.test(weekday ?? "")) {
    const names = new Intl.DateTimeFormat(language, { weekday: "short", timeZone: "UTC" });
    // 2026-10-04 was a Sunday, cron's day 0 (and 7).
    const days = weekday.split(",").map((value) => names.format(new Date(Date.UTC(2026, 9, 4 + (Number(value) % 7)))));
    return t("appPeople.schedules.cron.weekly", { days: days.join(", "), time });
  }
  if (number.test(day ?? "") && weekday === "*") return t("appPeople.schedules.cron.monthly", { day: Number(day), time });
  return cron;
}
