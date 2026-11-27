import { useTranslation } from "react-i18next";
import { activeDateLocale } from "@/lib/i18n";
import { workspaceAnalyticsChange, type WorkspaceAnalytics } from "./workspaceAnalyticsModel";
import analyticsStyles from "./WorkspaceAnalytics.module.css";

/**
 * Notion's user engagement, on the overview and the members tab: how many
 * members and guests viewed or edited a page in the range, each against the
 * range before it ("118% more than previous 90 days").
 */
export function WorkspaceAnalyticsEngagementCards({ data }: { data: WorkspaceAnalytics }) {
  const { t } = useTranslation(["workspaceAnalytics"]);
  const engagement = data.engagement;
  if (!engagement) return null;
  const card = (kind: "members" | "guests", current: number, previous: number) => {
    const change = workspaceAnalyticsChange(current, previous, data.days, data.retentionDays);
    const tone = change.kind === "up" || change.kind === "down" ? change.kind : "same";
    return (
      <div className={analyticsStyles.statCard} data-engagement={kind}>
        <div className={analyticsStyles.statValue}>
          {t(kind === "members" ? "workspaceAnalytics:activeMembersCount" : "workspaceAnalytics:activeGuestsCount", { count: current })}
        </div>
        {change.kind === "unknown" ? null : (
          <div className={analyticsStyles.statChange} data-tone={tone}>
            {change.kind === "up" || change.kind === "down"
              ? `${change.kind === "up" ? "↗" : "↘"} ${t(`workspaceAnalytics:change.${change.kind}`, { count: data.days, percent: change.percent })}`
              : t(`workspaceAnalytics:change.${change.kind}`, { count: data.days })}
          </div>
        )}
      </div>
    );
  };
  return (
    <div className={analyticsStyles.stats}>
      {card("members", engagement.members, engagement.previousMembers)}
      {card("guests", engagement.guests, engagement.previousGuests)}
    </div>
  );
}

/** The members tab's "Active members over time": members active each day, on a scale. */
export function WorkspaceAnalyticsActiveMembersChart({ data }: { data: WorkspaceAnalytics }) {
  const { t } = useTranslation(["workspaceAnalytics"]);
  const peak = Math.max(1, ...data.activeMembers.map((day) => day.count));
  const dayLabel = (value: string) => new Intl.DateTimeFormat(activeDateLocale(), { month: "short", day: "numeric" })
    .format(new Date(`${value}T00:00:00`));
  const first = data.activeMembers[0]?.day;
  const last = data.activeMembers.at(-1)?.day;
  return (
    <div className={analyticsStyles.card}>
      <div className={analyticsStyles.cardTitle}>{t("workspaceAnalytics:activeMembersOverTime")}</div>
      <div className={analyticsStyles.chartFrame}>
        <div className={analyticsStyles.chartScale} aria-hidden>
          <span>{peak}</span>
          <span>0</span>
        </div>
        <svg
          className={analyticsStyles.chart}
          viewBox={`0 0 ${data.activeMembers.length * 4} 40`}
          preserveAspectRatio="none"
          role="img"
          aria-label={t("workspaceAnalytics:activeMembersChart", { count: data.activeMembers.at(-1)?.count ?? 0 })}
        >
          {data.activeMembers.map((day, index) => {
            const height = Math.max(day.count ? 2 : 0, (day.count / peak) * 38);
            return (
              <rect key={day.day} data-active-day={day.day} x={index * 4 + 0.5} y={40 - height} width={3} height={height}>
                <title>{`${dayLabel(day.day)}: ${t("workspaceAnalytics:membersCount", { count: day.count })}`}</title>
              </rect>
            );
          })}
        </svg>
        <div className={analyticsStyles.chartDays} aria-hidden>
          <span>{first ? dayLabel(first) : ""}</span>
          <span>{last ? dayLabel(last) : ""}</span>
        </div>
      </div>
    </div>
  );
}
