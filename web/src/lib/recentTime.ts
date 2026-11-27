export type RecentTimeStep =
  | { step: "now" }
  | { step: "minutes" | "hours" | "days"; count: number }
  | { step: "date"; date: string };

/**
 * How long ago a time was, in the steps Notion's recent times take (its
 * timeUtils, read 2026-11-16 — its top bar's "1일 전 편집", its updates
 * tab's "1일 전" and its search results' "편집 시간: 1일 전" all come from
 * it): a time in another calendar year by its date with the year, from three
 * whole days by its month and day ("11월 11일"), one or two days, hours or
 * minutes ago in those, else now. `withYear` dates always with the year.
 */
export function recentTimeStep(time: number, now: number, locale: string, withYear = false): RecentTimeStep {
  const ago = Math.max(0, now - time);
  const otherYear = new Date(time).getFullYear() !== new Date(now).getFullYear();
  const days = Math.floor(ago / 86_400_000);
  if (otherYear || days >= 3) {
    return { step: "date", date: new Date(time).toLocaleDateString(locale, {
      month: "short", day: "numeric", ...(otherYear || withYear ? { year: "numeric" } : {}),
    }) };
  }
  if (days >= 1) return { step: "days", count: days };
  const hours = Math.floor(ago / 3_600_000);
  if (hours >= 1) return { step: "hours", count: hours };
  const minutes = Math.floor(ago / 60_000);
  return minutes >= 1 ? { step: "minutes", count: minutes } : { step: "now" };
}
