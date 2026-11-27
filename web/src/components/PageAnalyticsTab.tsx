import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { NotionSelect } from "@/components/database/NotionSelect";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { CalendarIcon, SparkleMagnifyingGlassIcon } from "@/icons/leafen";
import { activeDateLocale, activeNumberLocale } from "@/lib/i18n";
import {
  keptAnalyticsRanges,
  pageAnalyticsFailureMessage,
  type PageAnalyticsPanelService,
  type PageAnalyticsPreferences,
  type PageAnalyticsReadResult,
} from "@/lib/pageAnalyticsView";
import { PageAnalyticsChart } from "./PageAnalyticsChart";
import styles from "./PageAnalyticsPanel.module.css";
import { displayDateTime } from "@/lib/displayDate";

// The analytics tab as Notion's (seen 2026-10-18): "조회수 (총 N회)" with the
// period, the views by day, who viewed with the page's (or database item's,
// or inline database's opened alone) "내 조회 기록 표시",
// and the editors — who made the page and who edited it lately. With no
// views in the period it is only "페이지 애널리틱스 없음" in the middle of the
// tab, as Notion's (seen 2026-11-11: a page, a database opened alone, a page
// viewed only before the period); a period chosen here keeps its head so
// another can be chosen. The workspace's analytics switches are in Settings ›
// General, as Notion's; the account's own view history in My settings.

const RANGES = [7, 28, 90, 365] as const;

/** "8초 전", "3일 전": how long ago, in the reader's language. */
function ago(value: string, locale: string) {
  const at = new Date(value).getTime();
  if (Number.isNaN(at)) return "";
  const seconds = Math.max(0, Math.round((Date.now() - at) / 1000));
  const format = new Intl.RelativeTimeFormat(locale, { numeric: "always" });
  for (const [unit, size, below] of [["second", 1, 60], ["minute", 60, 60 * 60], ["hour", 3600, 86400], ["day", 86400, 30 * 86400],
    ["month", 30 * 86400, 365 * 86400]] as const) {
    if (seconds < below) return format.format(-Math.floor(seconds / size), unit);
  }
  return format.format(-Math.floor(seconds / (365 * 86400)), "year");
}

export function PageAnalyticsTab({ pageId, rowId, containerId, workspaceId, service }: {
  pageId: string;
  rowId?: string;
  /** An inline database opened alone, whose own analytics these are (Notion's, seen 2026-11-11). */
  containerId?: string;
  workspaceId: string;
  service: PageAnalyticsPanelService;
}) {
  const { t } = useTranslation("pageAnalytics");
  const [range, setRange] = useState(28);
  const [rangeChosen, setRangeChosen] = useState(false);
  const [analytics, setAnalytics] = useState<PageAnalyticsReadResult | null>(null);
  const [preferences, setPreferences] = useState<PageAnalyticsPreferences | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const locale = activeDateLocale();

  useEffect(() => {
    let cancelled = false;
    setError(null);
    service.getAnalytics(pageId, range, { rowId, containerId }).then((next) => {
      if (!cancelled) setAnalytics(next);
    }).catch((caught) => {
      if (!cancelled) setError(pageAnalyticsFailureMessage(caught, "analytics.read"));
    });
    return () => { cancelled = true; };
  }, [containerId, pageId, range, rowId, service]);

  // A range longer than the organization keeps isn't offered.
  const ranges = keptAnalyticsRanges(RANGES, analytics?.retentionDays ?? 0);
  useEffect(() => {
    if (!(ranges as readonly number[]).includes(range)) setRange(ranges[ranges.length - 1]);
  }, [range, ranges]);

  useEffect(() => {
    let cancelled = false;
    service.getPreferences(workspaceId, pageId, { rowId, containerId }).then((next) => {
      if (!cancelled) setPreferences(next);
    }).catch((caught) => {
      if (!cancelled) setError(pageAnalyticsFailureMessage(caught, "preferences.read"));
    });
    return () => { cancelled = true; };
  }, [containerId, pageId, rowId, service, workspaceId]);

  // "허용하기" / "허용하지 않음" for this page, which stays as chosen with the
  // account's own setting off, as in Notion (seen 2026-11-05).
  const allowOnPage = async (allowed: boolean) => {
    if (!preferences || saving) return;
    const previous = preferences;
    setPreferences({ ...preferences, pageIdentifiedHistoryEnabled: allowed });
    setSaving(true);
    setError(null);
    try {
      await service.updatePreferences({ workspaceId, pageId, ...(rowId ? { rowId } : containerId ? { containerId } : {}),
        identifiedHistoryEnabled: previous.identifiedHistoryEnabled, pageIdentifiedHistoryEnabled: allowed });
    } catch (caught) {
      setPreferences(previous);
      setError(pageAnalyticsFailureMessage(caught, "preferences.update"));
    } finally {
      setSaving(false);
    }
  };

  const name = (displayName: string | null) => displayName || t("memberFallback");
  const person = (key: string, userId: string, displayName: string | null, at: string) => (
    <li key={key}>
      <PersonAvatar className={styles.avatar} userId={userId} name={name(displayName)} size={20} />
      <span className={styles.personName}>{name(displayName)}</span>
      <time className={styles.personAgo} dateTime={at} title={displayDateTime(at, locale)}>{ago(at, locale)}</time>
    </li>
  );

  const empty = analytics !== null && analytics.totalViews === 0;
  const emptyNote = (
    <div className={styles.empty}>
      <div className={styles.emptyHead}>
        <SparkleMagnifyingGlassIcon size="card" />
        <p className={styles.emptyHeading}>{t("empty.heading")}</p>
      </div>
      <p className={styles.emptyDescription}>{t("empty.description")}</p>
    </div>
  );
  if (empty && !rangeChosen) {
    return <div className={styles.content} role="tabpanel" data-empty="alone">{emptyNote}</div>;
  }

  return (
    <div className={styles.content} role="tabpanel" data-empty={empty ? "period" : undefined}>
      <div className={styles.viewsHead}>
        <h3>{t("views")} <span>{t("viewsTotal", { count: analytics?.totalViews ?? 0, total: (analytics?.totalViews ?? 0).toLocaleString(activeNumberLocale()) })}</span></h3>
        <NotionSelect ariaLabel={t("period")} value={String(range)} className={styles.selectFit} buttonClassName={styles.rangeButton}
          options={ranges.map((days) => ({ value: String(days), label: t(`ranges.${days}`), icon: <CalendarIcon size="small" /> }))}
          onChange={(value) => { setRangeChosen(true); setRange(Number(value)); }} />
      </div>
      {error && <p className={styles.error} role="alert">{error}</p>}
      {!analytics && !error && <p className={styles.muted}>{t("loading")}</p>}
      {empty ? emptyNote : <>
        {/* A new period starts with no day pointed at. */}
        {analytics && <PageAnalyticsChart key={analytics.rangeDays} days={analytics.days} range={analytics.rangeDays} today={analytics.today} made={analytics.creator?.at} />}

        <section className={styles.section}>
          <h3>{t("viewers")}</h3>
          {/* A database item has its own, as Notion's (seen 2026-10-30); where the organization names no one, there is none to choose. */}
          {preferences && preferences.viewDisplay !== "hidden" ? (
            <div className={styles.historyCard}>
              <span>
                <strong>{t("pageIdentifiedHistory")}</strong>
                <small>{preferences.viewDisplay === "shown" ? t("organizationShowsEveryone") : t("pageIdentifiedHistoryDescription")}</small>
              </span>
              <NotionSelect ariaLabel={t("pageIdentifiedHistory")} className={styles.selectFit} buttonClassName={styles.historyButton}
                value={preferences.pageIdentifiedHistoryEnabled ? "allow" : "deny"}
                disabled={saving || preferences.viewDisplay !== "person"}
                options={[{ value: "allow", label: t("allow") }, { value: "deny", label: t("deny") }]}
                onChange={(value) => void allowOnPage(value === "allow")} />
            </div>
          ) : null}
          {analytics && (analytics.viewersHidden ? <p className={styles.muted} data-viewers-hidden>{t("viewersHidden")}</p>
            : analytics.recentViewers.length === 0 ? <p className={styles.muted}>{t("noRecentViewers")}</p> : (
            <ul className={styles.people}>
              {analytics.recentViewers.map((viewer) => person(viewer.userId, viewer.userId, viewer.displayName, viewer.lastViewedAt))}
            </ul>
          ))}
          {analytics && analytics.uniqueAnonymousViews > 0 && (
            <p className={styles.anonymousNote}>{t("anonymousSummary", { count: analytics.uniqueAnonymousViews })}</p>
          )}
        </section>

        {analytics && (analytics.creator || analytics.recentEditors.length > 0) ? (
          <section className={styles.section}>
            <h3>{t("editors")}</h3>
            {analytics.creator ? <>
              <h4>{t("createdBy")}</h4>
              <ul className={styles.people}>{person("creator", analytics.creator.userId, analytics.creator.displayName, analytics.creator.at)}</ul>
            </> : null}
            {analytics.recentEditors.length ? <>
              <h4>{t("recentEditors")}</h4>
              <ul className={styles.people}>
                {analytics.recentEditors.map((editor) => person(editor.userId, editor.userId, editor.displayName, editor.at))}
              </ul>
            </> : null}
          </section>
        ) : null}
      </>}
    </div>
  );
}
