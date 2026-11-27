import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { pageHref } from "@/lib/navigation";
import { relativeAgoLabel, relativeEditedLabel } from "@/lib/relativeTime";
import type { Page } from "@/types";
import { PageIconGlyph } from "../page/PageIcon";
import {
  sortWorkspaceAnalyticsMembers,
  sortWorkspaceAnalyticsSearches,
  workspaceAnalyticsClickThroughRate,
  workspaceAnalyticsContentCsv,
  workspaceAnalyticsExportName,
  workspaceAnalyticsMembersCsv,
  workspaceAnalyticsSearchesCsv,
  type WorkspaceAnalytics,
  type WorkspaceAnalyticsContentFilters,
  type WorkspaceAnalyticsCreated,
  type WorkspaceAnalyticsMemberSort,
  type WorkspaceAnalyticsPage,
  type WorkspaceAnalyticsSearchSort,
} from "./workspaceAnalyticsModel";
import { styles } from "./workspaceSettingsOverviewStyles";
import analyticsStyles from "./WorkspaceAnalytics.module.css";
import { AppSelect } from "@/components/AppSelect";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";
import { plainTitleOf } from "@/lib/closedMentions";

function download(name: string, text: string) {
  void triggerBrowserFileDownload(new Blob([text], { type: "text/csv;charset=utf-8" }), name, { revokeAfterMs: 1000 });
}

function pageIconType(value: string): Page["iconType"] {
  return value === "emoji" || value === "image" || value === "custom_emoji" || value === "icon" ? value : "none";
}

function pageLink(page: WorkspaceAnalyticsPage) {
  // A database page opens full as its page, its database in its bar, as a
  // list of pages opens one in Notion (search, backlinks, home's visits).
  return page.containerPageId
    ? pageHref(page.containerPageId, new URLSearchParams({ p: page.pageId, pm: "f" }))
    : pageHref(page.pageId);
}

/** The members tab: each member's page views, edits and last activity. */
export function WorkspaceAnalyticsMembersTable({ data, busy }: { data: WorkspaceAnalytics; busy: boolean }) {
  const { t } = useTranslation(["workspaceAnalytics"]);
  const [sort, setSort] = useState<WorkspaceAnalyticsMemberSort>("views");
  const members = useMemo(() => sortWorkspaceAnalyticsMembers(data.members, sort), [data.members, sort]);
  const header = (key: WorkspaceAnalyticsMemberSort, label: string) => (
    <th scope="col" aria-sort={sort === key ? "descending" : "none"}>
      <button type="button" className={analyticsStyles.sortButton} data-member-sort={key} onClick={() => setSort(key)}>
        {label}
      </button>
    </th>
  );
  return (
    <div className={analyticsStyles.tableBlock} aria-busy={busy}>
      <div className={analyticsStyles.tableActions}>
        {data.membersHasMore ? <span className={analyticsStyles.empty}>{t("workspaceAnalytics:membersTruncated")}</span> : null}
        <button
          type="button"
          className={styles.secondaryButton}
          data-analytics-export="members"
          onClick={() => download(
            workspaceAnalyticsExportName("members", data.days, data.to),
            workspaceAnalyticsMembersCsv(members, {
              name: t("workspaceAnalytics:columns.member"), email: t("workspaceAnalytics:columns.email"),
              views: t("workspaceAnalytics:columns.pageViews"), edits: t("workspaceAnalytics:columns.pageEdits"),
              lastActive: t("workspaceAnalytics:columns.lastActive"),
            }),
          )}
        >
          {t("workspaceAnalytics:exportCsv")}
        </button>
      </div>
      <table className={analyticsStyles.table}>
        <thead>
          <tr>
            {header("name", t("workspaceAnalytics:columns.member"))}
            {header("views", t("workspaceAnalytics:columns.pageViews"))}
            {header("edits", t("workspaceAnalytics:columns.pageEdits"))}
            {header("lastActive", t("workspaceAnalytics:columns.lastActive"))}
          </tr>
        </thead>
        <tbody>
          {members.map((member) => (
            <tr key={member.userId} data-analytics-member={member.userId}>
              <td>
                <span className={analyticsStyles.memberName}>{member.name || member.email}</span>
                {member.name && member.email ? <span className={analyticsStyles.memberEmail}>{member.email}</span> : null}
                {member.viewHistoryHidden ? (
                  <span
                    className={analyticsStyles.hiddenMark}
                    data-view-history-hidden="true"
                    title={t("workspaceAnalytics:viewHistoryHidden")}
                    aria-label={t("workspaceAnalytics:viewHistoryHidden")}
                  >
                    {t("workspaceAnalytics:notRecorded")}
                  </span>
                ) : null}
              </td>
              <td>{member.pageViews}</td>
              <td>{member.pageEdits}</td>
              <td>{member.lastActiveAt ? relativeAgoLabel(member.lastActiveAt) : t("workspaceAnalytics:never")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * The search tab: queries at least two members searched at least twice,
 * with their searches, unique searchers and click-through rate, sortable
 * by each; never who searched.
 */
export function WorkspaceAnalyticsSearchTable({ data, busy }: { data: WorkspaceAnalytics; busy: boolean }) {
  const { t } = useTranslation(["workspaceAnalytics"]);
  const [sort, setSort] = useState<WorkspaceAnalyticsSearchSort>("searches");
  const searches = useMemo(() => sortWorkspaceAnalyticsSearches(data.searches ?? [], sort), [data.searches, sort]);
  const header = (key: WorkspaceAnalyticsSearchSort, label: string) => (
    <th scope="col" aria-sort={sort === key ? "descending" : "none"}>
      <button type="button" className={analyticsStyles.sortButton} data-search-sort={key} onClick={() => setSort(key)}>
        {label}
      </button>
    </th>
  );
  return (
    <div className={analyticsStyles.tableBlock} aria-busy={busy}>
      <div className={analyticsStyles.tableActions}>
        <span className={analyticsStyles.empty}>{t("workspaceAnalytics:searchesNote")}</span>
        {data.searchesHasMore ? <span className={analyticsStyles.empty}>{t("workspaceAnalytics:searchesTruncated")}</span> : null}
        <button
          type="button"
          className={styles.secondaryButton}
          data-analytics-export="searches"
          onClick={() => download(
            workspaceAnalyticsExportName("searches", data.days, data.to),
            workspaceAnalyticsSearchesCsv(searches, {
              query: t("workspaceAnalytics:columns.searchQuery"), searches: t("workspaceAnalytics:columns.searches"),
              uniqueSearches: t("workspaceAnalytics:columns.uniqueSearches"),
              clickThroughRate: t("workspaceAnalytics:columns.clickThroughRate"),
            }),
          )}
        >
          {t("workspaceAnalytics:exportCsv")}
        </button>
      </div>
      {searches.length ? (
        <table className={analyticsStyles.table}>
          <thead>
            <tr>
              <th scope="col">{t("workspaceAnalytics:columns.searchQuery")}</th>
              {header("searches", t("workspaceAnalytics:columns.searches"))}
              {header("uniqueSearches", t("workspaceAnalytics:columns.uniqueSearches"))}
              {header("clickThroughRate", t("workspaceAnalytics:columns.clickThroughRate"))}
            </tr>
          </thead>
          <tbody>
            {searches.map((search) => (
              <tr key={search.query} data-analytics-search={search.query}>
                <td>{search.query}</td>
                <td>{search.searches}</td>
                <td>{search.uniqueSearches}</td>
                <td>{`${Math.round(workspaceAnalyticsClickThroughRate(search) * 100)}%`}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : <div className={analyticsStyles.empty}>{t("workspaceAnalytics:noSearches")}</div>}
    </div>
  );
}

/** The content tab: pages the viewer can read, with filters and search. */
export function WorkspaceAnalyticsContentTable({
  data, busy, filters, onFiltersChange, teamspaces, viewerId,
}: {
  data: WorkspaceAnalytics;
  busy: boolean;
  filters: WorkspaceAnalyticsContentFilters;
  onFiltersChange: (next: WorkspaceAnalyticsContentFilters) => void;
  teamspaces: Array<{ id: string; name: string }>;
  viewerId: string;
}) {
  const { t } = useTranslation(["workspaceAnalytics"]);
  const creators = data.canSeeMembers
    ? data.members.map((member) => ({ id: member.userId, name: member.name || member.email }))
    : [{ id: viewerId, name: t("workspaceAnalytics:me") }];
  return (
    <div className={analyticsStyles.tableBlock} aria-busy={busy}>
      <div className={analyticsStyles.filters} role="group" aria-label={t("workspaceAnalytics:filters")}>
        <input
          className={analyticsStyles.search}
          type="search"
          value={filters.query}
          placeholder={t("workspaceAnalytics:searchPages")}
          aria-label={t("workspaceAnalytics:searchPages")}
          onChange={(event) => onFiltersChange({ ...filters, query: event.target.value })}
        />
        <AppSelect
          aria-label={t("workspaceAnalytics:createdOn")}
          value={String(filters.created)}
          onChange={(event) => onFiltersChange({
            ...filters, created: (event.target.value === "any" ? "any" : Number(event.target.value)) as WorkspaceAnalyticsCreated,
          })}
        >
          <option value="any">{t("workspaceAnalytics:createdAny")}</option>
          {[7, 30, 90].map((range) => (
            <option key={range} value={range}>{t("workspaceAnalytics:createdWithin", { count: range })}</option>
          ))}
        </AppSelect>
        <AppSelect
          aria-label={t("workspaceAnalytics:createdBy")}
          value={filters.createdBy}
          onChange={(event) => onFiltersChange({ ...filters, createdBy: event.target.value })}
        >
          <option value="">{t("workspaceAnalytics:anyone")}</option>
          {creators.map((creator) => <option key={creator.id} value={creator.id}>{creator.name}</option>)}
        </AppSelect>
        {teamspaces.length ? (
          <AppSelect
            aria-label={t("workspaceAnalytics:teamspace")}
            value={filters.teamspace}
            onChange={(event) => onFiltersChange({ ...filters, teamspace: event.target.value })}
          >
            <option value="">{t("workspaceAnalytics:allTeamspaces")}</option>
            {teamspaces.map((teamspace) => <option key={teamspace.id} value={teamspace.id}>{teamspace.name}</option>)}
          </AppSelect>
        ) : null}
        <button
          type="button"
          className={styles.secondaryButton}
          data-analytics-export="content"
          onClick={() => download(
            workspaceAnalyticsExportName("content", data.days, data.to),
            workspaceAnalyticsContentCsv(data.content, {
              title: t("workspaceAnalytics:columns.page"), views: t("workspaceAnalytics:columns.views"),
              uniqueViewers: t("workspaceAnalytics:columns.uniqueViewers"),
              lastEdited: t("workspaceAnalytics:columns.lastEdited"), created: t("workspaceAnalytics:columns.created"),
            }),
          )}
        >
          {t("workspaceAnalytics:exportCsv")}
        </button>
      </div>
      {data.content.length ? (
        <table className={analyticsStyles.table}>
          <thead>
            <tr>
              <th scope="col">{t("workspaceAnalytics:columns.page")}</th>
              <th scope="col">{t("workspaceAnalytics:columns.views")}</th>
              <th scope="col">{t("workspaceAnalytics:columns.uniqueViewers")}</th>
              <th scope="col">{t("workspaceAnalytics:columns.lastEdited")}</th>
            </tr>
          </thead>
          <tbody>
            {data.content.map((page) => (
              <tr key={page.pageId} data-analytics-page={page.pageId}>
                <td>
                  <a className={analyticsStyles.pageLink} href={pageLink(page)}>
                    {/* Drawn as the sidebar draws it: a database or wiki its table, an empty page its empty document. */}
                    <PageIconGlyph size={15} page={{
                      icon: page.icon, iconType: pageIconType(page.iconType), kind: page.kind,
                      isWiki: page.isWiki, wikiDataSourceId: page.wikiDataSourceId, hasContent: page.hasContent,
                    }} />
                    <span>{plainTitleOf(page) || untitledPageDisplayTitle(page)}</span>
                  </a>
                </td>
                <td>{page.views}</td>
                <td>{page.uniqueViewers}</td>
                <td>{page.lastEditedAt ? relativeEditedLabel(page.lastEditedAt) : ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : <div className={analyticsStyles.empty}>{t("workspaceAnalytics:noPages")}</div>}
      {data.contentHasMore ? <div className={analyticsStyles.empty}>{t("workspaceAnalytics:contentTruncated")}</div> : null}
    </div>
  );
}
