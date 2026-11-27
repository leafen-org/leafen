import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { listedPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import { PagesStackIcon, Search } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { pageHref } from "@/lib/navigation";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { PersonActivity, PersonProfileData } from "@/lib/peopleProfiles";
import { relativeAgoLabel } from "@/lib/relativeTime";
import { pushRoute } from "@/lib/router";
import { pagesByIdOf } from "@/lib/pageIndex";
import { PersonAvatar } from "./PersonAvatar";
import styles from "./PeopleView.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";

const ACTIVITY_ROWS = 5;

type ActivityRow = { key: string; pageId: string; label: PersonActivity; created: boolean; edited: boolean; commented: boolean; at: number };

/**
 * What a member worked on, as Notion's profile lists it (seen 2026-10-12):
 * "최근 활동" — a page a day, "생성 및 편집함", "생성함" or "편집함", newest
 * first, the five latest — and "〈이름〉 님의 페이지", the pages they made,
 * last edited first, as cards a search narrows. Only pages the reader may
 * open come from the server.
 */
export function PersonProfileWork({ profile }: { profile: PersonProfileData }) {
  const { t } = useTranslation("peopleProfiles");
  const pagesById = pagesByIdOf(useProductPages()?.pages ?? []);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const rows = useMemo(() => activityRows(profile.activity).slice(0, ACTIVITY_ROWS), [profile.activity]);
  const needle = query.trim().toLocaleLowerCase();
  const pages = needle ? profile.pages.filter((page) => page.title.toLocaleLowerCase().includes(needle)) : profile.pages;

  return (
    <div className={styles.work}>
      {profile.activityShown ? (
        <section data-person-activity>
          <div className={`${styles.workHeading} ${styles.activityHeading}`}>{t("recentActivity")}</div>
          {/* None in the last 30 days reads so in the middle, without the list's box, as Notion's (seen 2026-11-15). */}
          {rows.length ? <div className={styles.activity}>
            {rows.map((row) => (
              <button key={row.key} type="button" className={styles.activityRow} onClick={() => pushRoute(pageHref(row.pageId))}>
                <span className={styles.activityKind}>
                  {t(activityVerb(row))}
                </span>
                <span className={styles.activityPage}>
                  <PageIconGlyph page={listedPageIcon(pagesById[row.pageId], row.label)} size={16} />
                  <span className={styles.activityTitle}>{row.label.title || untitledPageDisplayTitle(listedPageNameKind(row.label))}</span>
                </span>
                <span className={styles.activityTime}>{shortAgo(row.at, t)}</span>
              </button>
            ))}
          </div> : <div className={styles.activityEmpty} data-person-activity-empty>{t("noActivity")}</div>}
        </section>
      ) : null}
      <section data-person-pages>
        <div className={styles.workHeadingRow}>
          <span className={styles.workHeading}>{t("pagesOf", { name: profile.displayName })}</span>
          <button type="button" className={styles.workSearchButton} aria-label={t("searchPages")} aria-pressed={searching}
            onClick={() => { setSearching((open) => !open); setQuery(""); }}>
            <Search size="small" aria-hidden="true" />
          </button>
        </div>
        {searching ? (
          <input className={styles.workSearch} autoFocus={fieldFocusOnOpen()} value={query} placeholder={t("searchPages")} aria-label={t("searchPages")}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => { if (event.key === "Escape") { setSearching(false); setQuery(""); } }} />
        ) : null}
        {pages.length ? (
          <div className={styles.gallery}>
            {pages.map((page) => (
              <button key={page.pageId} type="button" className={styles.galleryCard} data-person-page={page.pageId}
                onClick={() => pushRoute(pageHref(page.pageId))}>
                <span className={styles.galleryPreview}><PageIconGlyph page={listedPageIcon(pagesById[page.pageId], page)} size={22} /></span>
                <span className={styles.galleryTitle}>{page.title || untitledPageDisplayTitle(listedPageNameKind(page))}</span>
                <span className={styles.galleryFoot}>
                  <PersonAvatar userId={profile.userId} avatar={profile.avatar} name={profile.displayName} size={16} />
                  {relativeAgoLabel(page.editedAt)}
                </span>
              </button>
            ))}
          </div>
        ) : profile.pages.length ? <div className={styles.workEmpty}>{t("noPagesFound")}</div> : (
          // One who made none has a box saying their pages will show there, as Notion's (seen 2026-11-15).
          <div className={styles.pagesEmpty} data-person-pages-empty>
            <PagesStackIcon pixels={32} aria-hidden="true" />
            <span>{t("noPages", { name: profile.displayName })}</span>
          </div>
        )}
      </section>
    </div>
  );
}

/** The activity as rows: a page a day, what the person did to it then. */
function activityRows(activity: PersonActivity[]): ActivityRow[] {
  const rows = new Map<string, ActivityRow>();
  for (const event of activity) {
    const day = new Date(event.at).toDateString();
    const key = `${event.pageId}:${day}`;
    const row = rows.get(key) ?? { key, pageId: event.pageId, label: event, created: false, edited: false, commented: false, at: event.at };
    if (event.kind === "created") row.created = true;
    else if (event.kind === "commented") row.commented = true;
    else row.edited = true;
    row.at = Math.max(row.at, event.at);
    rows.set(key, row);
  }
  return [...rows.values()].sort((left, right) => right.at - left.at);
}

/**
 * What the person did to the page that day, as Notion words it (seen
 * 2026-10-12): "편집함", "생성 및 편집함", "편집 및 댓글 작성함", "댓글 작성함".
 */
function activityVerb(row: ActivityRow) {
  if (row.commented) return row.created ? "createdEditedAndCommented" : row.edited ? "editedAndCommented" : "commented";
  return row.created && row.edited ? "createdAndEdited" : row.created ? "created" : "edited";
}

/**
 * "3분", "2시간" today, "1일", "2일" for the days before — counted in days of
 * the calendar, so each day's row reads apart — as the activity's right edge
 * says it; past a week, the date.
 */
function shortAgo(at: number, t: (key: string, options?: Record<string, unknown>) => string) {
  const minute = 60_000;
  const now = new Date();
  const days = Math.round((startOfDay(now) - startOfDay(new Date(at))) / (24 * 60 * minute));
  if (days <= 0) {
    const diff = Math.max(0, now.getTime() - at);
    return diff < 60 * minute ? t("agoMinutes", { count: Math.max(1, Math.floor(diff / minute)) })
      : t("agoHours", { count: Math.floor(diff / (60 * minute)) });
  }
  if (days < 7) return t("agoDays", { count: days });
  return new Date(at).toLocaleDateString(activeDateLocale(), { month: "long", day: "numeric" });
}

function startOfDay(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}
