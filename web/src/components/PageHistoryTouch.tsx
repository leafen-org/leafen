import { type ReactNode, type RefObject, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { activeDateLocale } from "@/lib/i18n";
import {
  pageHistoryDayAndTime, pageHistoryFullTimeLabel, pageHistoryRecentDay, pageHistoryTimeLabel, type PageHistoryGroup,
} from "@/lib/pageHistoryGroups";
import sheet from "./shell/PhoneSheet.module.css";
import styles from "./PageHistoryTouch.module.css";

/** The version a touch history shows over its list, with what restores it. */
export type PageHistoryTouchVersion = {
  /** Its whole date and time, the head's title ("2026년 11월 7일 오후 3:13"). */
  title: string;
  restoring?: boolean;
  restoreDisabled?: boolean;
  error: string | null;
  /** Left out where the history only shows, as a database's own. */
  onRestore?: () => void;
  onDone: () => void;
  /** Leafen's "〈연결〉 변경 되돌리기", in a line under the head. */
  actions?: ReactNode;
  preview: ReactNode;
};

/**
 * How a touch history names its versions, as Notion's iPad (seen
 * 2026-11-14): a line as its day and time ("오늘 · 오전 7:08", "11월 7일 ·
 * 오후 5:29"), a version's head by its day word today and yesterday ("오늘
 * 오전 7:08"), else whole with its year ("2026년 11월 7일 오후 3:13").
 */
export function usePageHistoryTouchTimes() {
  const { t } = useTranslation("updatesPanel");
  const locale = activeDateLocale();
  const words = { today: t("updatesPanel:versions.touchDayToday"), yesterday: t("updatesPanel:versions.touchDayYesterday") };
  return {
    line: (createdAt: number) => t("updatesPanel:versions.touchTime", pageHistoryDayAndTime(createdAt, locale, words)),
    title: (createdAt: number) => (pageHistoryRecentDay(createdAt) ? pageHistoryTimeLabel(createdAt, locale, {
      today: (time) => t("updatesPanel:versions.timeToday", { time }),
      yesterday: (time) => t("updatesPanel:versions.timeYesterday", { time }),
    }) : pageHistoryFullTimeLabel(createdAt, locale, false)),
  };
}

/**
 * A page's version history on a touch screen, as Notion's iPad (seen
 * 2026-11-14 in Safari 834 × 1124 and 1210 × 748, light): no list and
 * preview side by side, but "기록" in a card in the middle, its versions
 * one time a line ("오늘 · 오전 7:08", "11월 7일 · 오후 5:29") with no
 * names, bookmarks or grouping, and "완료" at its right. A line opens its
 * version over the whole screen, unmarked, under its time with "복원" at the left,
 * which asks first and then restores and closes, and "완료" back to the
 * list at the right. A phone's card is the whole screen
 * (PageHistoryTouch.module.css).
 */
export function PageHistoryTouch({
  panelRef, zIndex, titleId, doneRef, groups, label, loading, hasMore, onLoadMore, onChoose, onClose, version,
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  zIndex: number | undefined;
  titleId: string;
  doneRef: RefObject<HTMLButtonElement | null>;
  groups: readonly PageHistoryGroup[];
  label: (createdAt: number) => string;
  loading: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  onChoose: (group: PageHistoryGroup, index: number) => void;
  onClose: () => void;
  version: PageHistoryTouchVersion | null;
}) {
  const { t } = useTranslation(["updatesPanel", "common", "wikiView"]);
  // The focus goes to what is in front: a version's "완료" as it opens, the
  // list's again as it closes.
  const backRef = useRef<HTMLButtonElement>(null);
  const shown = version !== null;
  const mounted = useRef(false);
  useEffect(() => {
    if (!mounted.current) {
      mounted.current = true;
      return;
    }
    (shown ? backRef : doneRef).current?.focus();
  }, [shown, doneRef]);
  return (
    <div ref={panelRef} className={styles.root} style={{ zIndex }} data-page-history="true" data-page-history-touch="true"
      role="dialog" aria-modal="true" aria-labelledby={titleId}>
      <div className={styles.shade} onClick={onClose} aria-hidden="true" />
      <section className={styles.card} aria-hidden={version ? true : undefined}>
        <div className={`${sheet.head} ${styles.head}`}>
          <span className={sheet.headSide} />
          <strong id={titleId} className={sheet.title}>{t("updatesPanel:versions.touchTitle")}</strong>
          <span className={`${sheet.headSide} ${sheet.headEnd}`}>
            <button ref={doneRef} type="button" className={sheet.headButton} onClick={onClose} data-page-history-touch-done>
              {t("common:actions.done")}
            </button>
          </span>
        </div>
        <div className={`${styles.list} nscroll`} role="list" aria-label={t("updatesPanel:versions.versionsAria")}>
          {!loading && groups.length === 0 ? (
            <p className={styles.empty}>{t("updatesPanel:empty.noPageHistoryYet")}</p>
          ) : null}
          {groups.map((group, index) => (
            <div key={group.version.id} role="listitem">
              <button type="button" className={styles.row} onClick={() => onChoose(group, index)} data-page-history-touch-version={group.version.id}>
                <span className={styles.rowText}>{label(group.version.createdAt)}</span>
              </button>
            </div>
          ))}
          {hasMore ? (
            <button type="button" className={`${styles.row} ${styles.more}`} disabled={loading} onClick={onLoadMore}>
              <span className={styles.rowText}>{t("wikiView:loadMore")}</span>
            </button>
          ) : null}
        </div>
      </section>
      {version ? (
        <section className={styles.version} aria-label={version.title} data-page-history-touch-preview>
          <div className={`${sheet.head} ${styles.versionHead}`}>
            <span className={sheet.headSide}>
              {version.onRestore ? (
                <button type="button" className={sheet.headButton} disabled={version.restoreDisabled || version.restoring}
                  onClick={version.onRestore} data-page-history-touch-restore>
                  {t("updatesPanel:versions.touchRestore")}
                </button>
              ) : null}
            </span>
            <strong className={sheet.title}>{version.title}</strong>
            <span className={`${sheet.headSide} ${sheet.headEnd}`}>
              <button ref={backRef} type="button" className={sheet.headButton} onClick={version.onDone} data-page-history-touch-back>
                {t("common:actions.done")}
              </button>
            </span>
          </div>
          {version.actions ? <div className={styles.actions}>{version.actions}</div> : null}
          {version.error ? <p className={styles.error} role="alert">{version.error}</p> : null}
          <div className={`${styles.versionBody} nscroll`} data-page-history-preview>{version.preview}</div>
          {version.restoring ? (
            <div className={styles.progress}>
              <p className={styles.progressCard} role="status">
                {t("updatesPanel:versions.restoring")}<span className={styles.spinner} aria-hidden="true" />
              </p>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}
