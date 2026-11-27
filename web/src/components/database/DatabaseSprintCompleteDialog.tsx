import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DatabaseSprintSettings, DatabaseSprintSummary } from "@/databaseToolbarConfigWire.generated";
import { ReplaceIcon, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { DatabaseSprintDates, sprintDay } from "./DatabaseSprintDates";
import { NotionSelect } from "./NotionSelect";
import styles from "./DatabaseSprintComplete.module.css";

type IncompleteTasks = DatabaseSprintSettings["incompleteTasks"];

export type DatabaseSprintCompletionChoice = { start: string; end: string; incompleteTasks: IncompleteTasks };

/**
 * "스프린트 완료"'s window, as Notion's (seen 2026-11-03): 400 wide in the
 * middle, "스프린트 3 요약" over its dates and tasks done, a blue note about
 * automatic sprints unless they are on, the next sprint, its dates and —
 * with unfinished tasks — where they go, then "스프린트 3 완료하기" and
 * "취소" the window's width.
 */
export function DatabaseSprintCompleteDialog({ current, next, done, total, automatic, initial, busy, failed, onSettings, onCancel, onComplete }: {
  current: DatabaseSprintSummary; next?: DatabaseSprintSummary; done: number; total: number; automatic: boolean;
  initial: DatabaseSprintCompletionChoice; busy: boolean; failed: boolean;
  onSettings(): void; onCancel(): void; onComplete(choice: DatabaseSprintCompletionChoice): void;
}) {
  const { t } = useTranslation("databaseTasks");
  const [choice, setChoice] = useState(initial);
  const [hint, setHint] = useState(!automatic);
  // A modal layer: Escape cancels it, and only it, unless it is completing.
  const dialogRef = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "alertdialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onCancel(); } });
  const unfinished = total - done;
  const name = current.title || t("untitledSprint");
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} aria-label={t("cancel")} disabled={busy} onClick={onCancel} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-label={t("sprintSummary", { name })}>
        <strong className={styles.summaryTitle}>{t("sprintSummary", { name })}</strong>
        <div className={styles.summaryLine}>
          {current.start ? <span>{sprintDay(current.start)} → {sprintDay(current.end || current.start)}</span> : null}
          {current.start ? <span className={styles.summaryDot} aria-hidden="true" /> : null}
          <span>{t("sprintSummaryTasks", { done, total })}</span>
        </div>
        {hint ? (
          <div className={styles.hint} data-sprint-auto-hint>
            <ReplaceIcon size="small" />
            <p>{t("autoSprintHintBefore")}<button type="button" className={styles.hintLink} onClick={onSettings}>{t("autoSprintHintLink")}</button>{t("autoSprintHintAfter")}</p>
            <button type="button" className={styles.hintClose} aria-label={t("common:actions.close")} onClick={() => setHint(false)}>
              <X size="tiny" />
            </button>
          </div>
        ) : null}
        {next ? (
          <div className={styles.row}>
            <span>{t("nextSprintChoice")}</span>
            <span className={styles.rowValue} data-next-sprint>{next.title || t("untitledSprint")}</span>
          </div>
        ) : null}
        <div className={styles.row}>
          <span>{t("sprintDates")}</span>
          <DatabaseSprintDates label={t("sprintDates")} start={choice.start} end={choice.end} disabled={busy}
            onChange={(start, end) => setChoice((now) => ({ ...now, start, end }))} />
        </div>
        {unfinished > 0 ? (
          <div className={styles.row}>
            <span>{t("unfinishedTasks", { count: unfinished })}</span>
            <NotionSelect ariaLabel={t("unfinishedTasks", { count: unfinished })} value={choice.incompleteTasks} disabled={busy}
              className={styles.rowSelect} buttonClassName={styles.rowValue} menuSize={{ width: 230, maxHeight: 300, gap: 0 }} showCheck={false}
              options={(["next", "backlog", "keep"] as const).map((value) => ({ value, label: t(`carryTasks.${value}`) }))}
              onChange={(value) => setChoice((now) => ({ ...now, incompleteTasks: value as IncompleteTasks }))} />
          </div>
        ) : null}
        {failed && <p role="alert" className={styles.failed}>{t("sprintActionFailed")}</p>}
        <button type="button" className={styles.primary} disabled={busy || (Boolean(choice.start) && choice.end < choice.start)}
          data-sprint-complete-confirm onClick={() => onComplete(choice)}>{t("completeSprintNamed", { name })}</button>
        <button type="button" className={styles.cancel} disabled={busy} onClick={onCancel}>{t("cancel")}</button>
      </div>
    </div>,
    document.body,
  );
}
