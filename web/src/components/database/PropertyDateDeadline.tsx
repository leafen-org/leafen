import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DeadlineNotice } from "@/deadlineWire.generated";
import { FlagIcon } from "@/icons/leafen";
import { acknowledgeDeadlineNotice, canonicalDeadlineDays, DEFAULT_DEADLINE_DAYS, readDeadlineNotices } from "@/lib/deadlines";
import { activeDateLocale } from "@/lib/i18n";
import { DateSwitchRow } from "./PropertyDateEditorOptions";
import editorStyles from "./PropertyDateEditor.module.css";
import styles from "./PropertyDateDeadline.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/** The mark of a date that is a deadline not to miss, beside its text in the cell. */
export function DeadlineFlag() {
  const { t } = useTranslation("propertyCell");
  return <FlagIcon size="tiny" className={styles.flag} aria-label={t("deadline.label")} data-date-deadline />;
}

function shownDays(days: string) {
  return days ? days.split(",").join(", ") : DEFAULT_DEADLINE_DAYS.split(",").join(", ");
}

function moment(at: number) {
  return new Date(at).toLocaleString(activeDateLocale(), { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

/**
 * The date editor's deadline rows, Leafen's own (docs: "놓치면 안 되는 기한"):
 * a switch marking the date as a deadline not to miss, the days before it
 * its notices go and, once the server keeps it, each notice with who
 * acknowledged it — and Acknowledge for the person a sent one waits on.
 */
export function PropertyDateDeadline({ deadline, disabled, rowId, propertyId, valueKey, onChange }: {
  /** The days, as "7,3,1,0"; "" when the date isn't a deadline. */
  deadline: string;
  disabled?: boolean;
  /** The item, when the date is an item's (not a template's). */
  rowId?: string;
  propertyId: string;
  /** Changes when the date does, so its notices are read again. */
  valueKey: string;
  onChange: (next: string) => void;
}) {
  const { t } = useTranslation("propertyCell");
  const [draft, setDraft] = useState(shownDays(deadline));
  const [notices, setNotices] = useState<DeadlineNotice[] | null>(null);
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState(false);
  useEffect(() => setDraft(shownDays(deadline)), [deadline]);
  useEffect(() => {
    setNotices(null);
    if (!deadline || !rowId) return;
    const controller = new AbortController();
    // The server makes the notices as the change lands; read them after it.
    const timer = window.setTimeout(() => {
      readDeadlineNotices(rowId, propertyId, controller.signal).then(setNotices).catch(() => undefined);
    }, 400);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [deadline, propertyId, rowId, valueKey]);

  function commitDraft() {
    const next = canonicalDeadlineDays(draft);
    if (!next || next === deadline) {
      setDraft(shownDays(deadline));
      return;
    }
    onChange(next);
  }

  async function acknowledge(noticeId: string) {
    setBusy(noticeId);
    setFailed(false);
    try {
      const updated = await acknowledgeDeadlineNotice(noticeId);
      setNotices((current) => current?.map((notice) => notice.id === updated.id ? updated : notice) ?? null);
    } catch {
      setFailed(true);
    }
    setBusy("");
  }

  return (
    <>
      <DateSwitchRow label={t("deadline.label")} checked={!!deadline} disabled={disabled}
        onChange={(on) => onChange(on ? DEFAULT_DEADLINE_DAYS : "")} />
      {deadline ? (
        <label className={styles.daysRow}>
          <span className={editorStyles.choiceLabel}>{t("deadline.days")}</span>
          <input className={`${editorStyles.input} ${styles.daysInput}`} type="text" inputMode="numeric" value={draft}
            title={t("deadline.daysHint")} data-date-deadline-days={deadline}
            onChange={(event) => setDraft(event.target.value)} onBlur={commitDraft}
            onKeyDown={(event) => {
              if (imeComposing(event)) return;
              if (fieldEnter(event)) {
                event.preventDefault();
                commitDraft();
              } else if (event.key === "Escape") {
                setDraft(shownDays(deadline));
              }
            }} />
        </label>
      ) : null}
      {deadline && notices?.length ? (
        <ul className={styles.notices} aria-label={t("deadline.notices")} data-date-deadline-notices>
          {notices.map((notice) => (
            <li key={notice.id} className={styles.notice} data-deadline-notice={notice.days} data-status={notice.status}>
              <span className={styles.noticeHead}>
                <span>{notice.days === 0 ? t("deadline.dayOf") : t("deadline.daysBefore", { count: notice.days })} · {moment(notice.dueAt)}</span>
                <span className={styles.noticeStatus}>
                  {notice.status === "pending" ? t("deadline.status.pending")
                    : notice.status === "past" ? t("deadline.status.past")
                      : notice.status === "done" && notice.rounds === 0 ? t("deadline.status.done")
                        : t(notice.status === "done" ? "deadline.status.sentDone" : "deadline.status.sent", { count: notice.rounds })}
                </span>
              </span>
              {notice.acknowledgements.map((acknowledgement) => (
                <span key={acknowledgement.userId} className={styles.acknowledgement}>
                  {t("deadline.acknowledgedBy", { name: acknowledgement.name || t("deadline.someone"), at: moment(acknowledgement.at) })}
                </span>
              ))}
              {notice.recipient && !notice.acknowledgedAt && notice.rounds > 0 ? (
                <button type="button" className={styles.acknowledge} disabled={busy === notice.id}
                  data-deadline-acknowledge={notice.id} onClick={() => void acknowledge(notice.id)}>
                  {t("deadline.acknowledge")}
                </button>
              ) : null}
            </li>
          ))}
          {failed ? <li className={styles.acknowledgement} role="alert" data-error>{t("deadline.acknowledgeFailed")}</li> : null}
        </ul>
      ) : null}
    </>
  );
}
