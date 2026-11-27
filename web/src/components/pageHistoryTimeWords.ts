type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * The version history's words for a version's time (pageHistoryTimeLabel):
 * "오늘 오후 3:13", "어제 …", "토요일 오후 8:25", as Notion's history writes
 * them (seen 2026-11-16).
 */
export function pageHistoryTimeWords(t: Translate) {
  return {
    today: (time: string) => t("updatesPanel:versions.timeToday", { time }),
    yesterday: (time: string) => t("updatesPanel:versions.timeYesterday", { time }),
    weekday: (weekday: string, time: string) => t("updatesPanel:versions.timeWeekday", { weekday, time }),
  };
}
