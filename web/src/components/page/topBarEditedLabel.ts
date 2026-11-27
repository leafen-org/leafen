import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { recentTimeStep } from "@/lib/recentTime";

/** "Edited 5m ago", as Notion's top bar writes the time a page was last edited,
 * in the steps of its recent times (recentTimeStep: seen 2026-10-28 "1일 전
 * 편집", four days on "10월 24일 편집"). */
export function topBarEditedLabel(
  value: string, now: number, t: (key: string, options?: Record<string, unknown>) => string, language: string,
) {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return "";
  const recent = recentTimeStep(time, now, language);
  switch (recent.step) {
    case "now": return t("topBar:editedNow");
    case "minutes": return t("topBar:editedMinutesAgo", { minutes: recent.count });
    case "hours": return t("topBar:editedHoursAgo", { hours: recent.count });
    case "days": return t("topBar:editedDaysAgo", { days: recent.count });
    case "date": return t("topBar:editedOnDate", { date: recent.date });
  }
}

/** The top bar's edited label for a time, kept current each minute. */
export function useEditedLabel(value: string) {
  const { t, i18n } = useTranslation("topBar");
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, [value]);
  return topBarEditedLabel(value, now, (key, options) => String(t(key, options)), i18n.language);
}
