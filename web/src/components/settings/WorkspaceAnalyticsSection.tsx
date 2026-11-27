import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session, Workspace } from "@/types";
import { useSidebarTeamspaces } from "../sidebar/useSidebarTeamspaces";
import { WorkspaceAnalyticsActiveMembersChart, WorkspaceAnalyticsEngagementCards } from "./WorkspaceAnalyticsEngagement";
import {
  WorkspaceAnalyticsContentTable,
  WorkspaceAnalyticsMembersTable,
  WorkspaceAnalyticsSearchTable,
} from "./WorkspaceAnalyticsTables";
import {
  EMPTY_WORKSPACE_ANALYTICS_FILTERS,
  WORKSPACE_ANALYTICS_RANGES,
  readWorkspaceAnalytics,
  topWorkspaceAnalyticsMembers,
  workspaceAnalyticsParams,
  type WorkspaceAnalytics,
  type WorkspaceAnalyticsContentFilters,
  type WorkspaceAnalyticsRange,
} from "./workspaceAnalyticsModel";
import { styles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import analyticsStyles from "./WorkspaceAnalytics.module.css";
import { activeDateLocale } from "@/lib/i18n";
import { keptAnalyticsRanges } from "@/lib/pageAnalyticsView";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { AppSelect } from "@/components/AppSelect";

type AnalyticsTab = "overview" | "members" | "content" | "search";

/**
 * Settings → Analytics, as Notion's workspace analytics: the owner sees the
 * overview, members, content and searches; everyone else sees the content
 * they can read.
 */
export function WorkspaceAnalyticsSection({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation(["workspaceAnalytics"]);
  const [days, setDays] = useState<WorkspaceAnalyticsRange>(30);
  const [chosenTab, setChosenTab] = useState<AnalyticsTab | null>(null);
  const [filters, setFilters] = useState<WorkspaceAnalyticsContentFilters>(EMPTY_WORKSPACE_ANALYTICS_FILTERS);
  const [data, setData] = useState<WorkspaceAnalytics | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const teamspaces = useSidebarTeamspaces({ session, workspace }).joinedTeamspaces;
  const filterKey = JSON.stringify(filters);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setError("");
      readWorkspaceAnalytics(session, workspace.id, workspaceAnalyticsParams(days, filters, new Date()), controller.signal)
        .then((result) => {
          if (controller.signal.aborted) return;
          setData(result);
        })
        .catch((cause: unknown) => {
          if (controller.signal.aborted) return;
          setError(settingsErrorMessage(cause, t("workspaceAnalytics:loadFailed")));
          // Nothing of another range's or filter's figures stays (2026-11-20).
          setData(null);
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    }, 200);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
    // `filters` follows `filterKey`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, filterKey, readTurn, session, t, workspace.id]);

  // A range longer than the organization keeps isn't offered.
  const ranges = keptAnalyticsRanges(WORKSPACE_ANALYTICS_RANGES, data?.retentionDays ?? 0);
  useEffect(() => {
    if (!ranges.includes(days)) setDays(ranges[ranges.length - 1]);
  }, [days, ranges]);

  // An owner opens on the overview; everyone else sees only content.
  const canSeeMembers = data?.canSeeMembers ?? false;
  const tab: AnalyticsTab = canSeeMembers ? (chosenTab ?? "overview") : "content";

  const tabs: AnalyticsTab[] = canSeeMembers ? ["overview", "members", "content", "search"] : ["content"];
  const tabListRef = useRef<HTMLDivElement>(null);
  const tabList = useNativeTabs<AnalyticsTab>({
    tabs: tabs.map((item) => ({ key: item, text: t(`workspaceAnalytics:tabs.${item}`) })),
    selected: tab, onSelect: setChosenTab, label: t("workspaceAnalytics:title"), listRef: tabListRef,
  });
  const dayLabel = (value: string) => new Intl.DateTimeFormat(activeDateLocale(), { month: "short", day: "numeric" })
    .format(new Date(`${value}T00:00:00`));

  return (
    <section id="analytics" className={styles.section} aria-label={t("workspaceAnalytics:title")}>
      <div className={styles.sectionHeader}>
        <div>
          <SettingsSectionTitle className={styles.sectionTitle}>{t("workspaceAnalytics:title")}</SettingsSectionTitle>
          <div className={styles.organizationMeta}>
            {data ? t("workspaceAnalytics:rangeSummary", { from: dayLabel(data.from), to: dayLabel(data.to) }) : null}
          </div>
        </div>
        <div className={styles.sectionActions}>
          <AppSelect
            aria-label={t("workspaceAnalytics:range")}
            value={days}
            onChange={(event) => setDays(Number(event.target.value) as WorkspaceAnalyticsRange)}
          >
            {ranges.map((range) => (
              <option key={range} value={range}>{t("workspaceAnalytics:lastDays", { count: range })}</option>
            ))}
          </AppSelect>
        </div>
      </div>

      <div {...tabList.tabListProps} ref={tabListRef} className={analyticsStyles.tabs}>
        {tabs.map((item) => (
          <NativeTab key={item} state={tabList.state} tabKey={item} data-analytics-tab={item}
            className={analyticsStyles.tab}>
            {t(`workspaceAnalytics:tabs.${item}`)}
          </NativeTab>
        ))}
      </div>

      {error ? <ListNotice role="alert" onRetry={() => { setError(""); setLoading(true); setReadTurn((turn) => turn + 1); }} data-analytics-read-failed>{error}</ListNotice> : null}
      {!data && loading ? <div className={styles.notice} data-tone="neutral">{t("workspaceAnalytics:loading")}</div> : null}

      {data && tab === "overview" && canSeeMembers ? (
        <div className={analyticsStyles.overview} aria-busy={loading}>
          <div className={analyticsStyles.groupTitle}>{t("workspaceAnalytics:engagement")}</div>
          <WorkspaceAnalyticsEngagementCards data={data} />
          <div className={analyticsStyles.rankings}>
            <div className={analyticsStyles.card}>
              <div className={analyticsStyles.cardTitle}>{t("workspaceAnalytics:topTeamspaces")}</div>
              {data.teamspaces.length ? (
                <ol className={analyticsStyles.ranking}>
                  {data.teamspaces.slice(0, 5).map((teamspace) => (
                    <li key={teamspace.teamspaceId}>
                      <span>{teamspace.name}</span>
                      <strong>{t("workspaceAnalytics:viewsCount", { count: teamspace.views })}</strong>
                    </li>
                  ))}
                </ol>
              ) : <div className={analyticsStyles.empty}>{t("workspaceAnalytics:none")}</div>}
            </div>
            {(["pageViews", "pageEdits"] as const).map((key) => {
              const top = topWorkspaceAnalyticsMembers(data.members, key);
              return (
                <div key={key} className={analyticsStyles.card}>
                  <div className={analyticsStyles.cardTitle}>
                    {t(key === "pageViews" ? "workspaceAnalytics:topViewers" : "workspaceAnalytics:topEditors")}
                  </div>
                  {top.length ? (
                    <ol className={analyticsStyles.ranking}>
                      {top.map((member) => (
                        <li key={member.userId}>
                          <span>{member.name || member.email}</span>
                          <strong>
                            {t(key === "pageViews" ? "workspaceAnalytics:viewsCount" : "workspaceAnalytics:editsCount", { count: member[key] })}
                          </strong>
                        </li>
                      ))}
                    </ol>
                  ) : <div className={analyticsStyles.empty}>{t("workspaceAnalytics:none")}</div>}
                </div>
              );
            })}
          </div>
        </div>
      ) : null}

      {data && tab === "members" && canSeeMembers ? (
        <div className={analyticsStyles.overview} aria-busy={loading}>
          <div className={analyticsStyles.groupTitle}>{t("workspaceAnalytics:engagement")}</div>
          <WorkspaceAnalyticsEngagementCards data={data} />
          <WorkspaceAnalyticsActiveMembersChart data={data} />
          <WorkspaceAnalyticsMembersTable data={data} busy={loading} />
        </div>
      ) : null}

      {data && tab === "search" && canSeeMembers ? (
        <WorkspaceAnalyticsSearchTable data={data} busy={loading} />
      ) : null}

      {data && tab === "content" ? (
        <WorkspaceAnalyticsContentTable
          data={data}
          busy={loading}
          filters={filters}
          onFiltersChange={setFilters}
          teamspaces={teamspaces.map((teamspace) => ({ id: teamspace.id, name: teamspace.name }))}
          viewerId={session.user.id}
        />
      ) : null}
    </section>
  );
}
