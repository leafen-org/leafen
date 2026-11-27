import { Trans, useTranslation } from "react-i18next";
import styles from "./DateViewMissing.module.css";

/**
 * A calendar or timeline with no date property to place its pages by, as
 * Notion's (seen 2026-11-08): "날짜 속성 없음" over one centred sentence whose
 * "날짜 속성을 추가" adds one; without the right to change the database, the
 * sentence only says the view needs one.
 */
export function DateViewMissing({ view, onAdd }: {
  view: "calendarView" | "timelineView";
  /** Adds a date property and places the view by it; none where the database can't change. */
  onAdd?: () => void;
}) {
  const { t } = useTranslation(view);
  return (
    <div className={styles.root} data-date-view-missing={view}>
      <div className={styles.title}>{t(`${view}:empty.title`)}</div>
      <div className={styles.description}>
        {onAdd ? (
          <Trans t={t} i18nKey={`${view}:empty.add`}
            components={{ add: <button type="button" className={styles.add} onClick={onAdd} /> }} />
        ) : t(`${view}:empty.description`)}
      </div>
    </div>
  );
}
