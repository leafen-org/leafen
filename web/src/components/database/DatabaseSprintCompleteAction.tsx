import { useContext, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DatabaseSprintChanges } from "@/databaseToolbarConfigWire.generated";
import type { GraphProperty, GraphRow } from "@/contentGraphWire.generated";
import { pageHref } from "@/lib/navigation";
import { useOptionalProductToast } from "@/lib/productToast";
import { useRouter } from "@/lib/router";
import { DatabaseSprintContext, DatabaseSprintRowContext } from "./databaseSprintContext";
import { DatabaseSprintCompleteDialog, type DatabaseSprintCompletionChoice } from "./DatabaseSprintCompleteDialog";
import styles from "./DatabaseSprintComplete.module.css";
import { statusCheckboxState } from "./propertyStatusPolicy";
import { valueAsRead } from "./valueAsRead";

function localDay() {
  const day = new Date();
  return `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, "0")}-${String(day.getDate()).padStart(2, "0")}`;
}

function addDays(value: string, days: number) {
  const [year, month, date] = value.split("-").map(Number);
  const day = new Date(Date.UTC(year, month - 1, date + days));
  return day.toISOString().slice(0, 10);
}

// The current sprint's tasks and those done: a status in its last group, or
// a checked box.
function sprintProgress(rows: readonly GraphRow[], properties: readonly GraphProperty[], sprintPropertyId: string,
  sprintId: string, statusPropertyId: unknown) {
  const status = properties.find(({ id }) => id === statusPropertyId);
  const tasks = rows.filter((row) => {
    const sprints = row.values[sprintPropertyId]?.value;
    return Array.isArray(sprints) && sprints.includes(sprintId);
  });
  const done = status ? tasks.filter((row) => {
    const value = valueAsRead(row, status, row.values[status.id]?.value);
    return status.type === "checkbox" ? value === true : statusCheckboxState(status.config, value) === "checked";
  }).length : 0;
  return { done, total: tasks.length };
}

function ProgressRing({ done, total }: { done: number; total: number }) {
  const length = 2 * Math.PI * 6.25;
  return (
    <svg width="15" height="15" viewBox="0 0 14 14" fill="none" strokeWidth="1.5" aria-hidden="true">
      <circle className={styles.ringTrack} cx="7" cy="7" r="6.25" />
      <circle className={styles.ringDone} cx="7" cy="7" r="6.25" strokeLinecap="round" transform="rotate(-90 7 7)"
        strokeDasharray={length} strokeDashoffset={total > 0 ? length * (1 - done / total) : length} />
    </svg>
  );
}

// What a completion made and changed, as Notion's note says it (seen
// 2026-11-03): "1개의 스프린트가 생성되었습니다. 및 4개의 스프린트가
// 편집되었습니다." — a task count joins when tasks moved.
function sprintChangesNote(changes: DatabaseSprintChanges, t: (key: string, options?: Record<string, unknown>) => string, locale: string) {
  const parts = [
    changes.createdSprints ? t("sprintChanges.created", { count: changes.createdSprints }) : "",
    changes.editedSprints ? t("sprintChanges.editedSprints", { count: changes.editedSprints }) : "",
    changes.editedTasks ? t("sprintChanges.editedTasks", { count: changes.editedTasks }) : "",
  ].filter(Boolean);
  return new Intl.ListFormat(locale, { type: "conjunction" }).format(parts);
}

// The Sprint board's Current sprint view ends its filter bar's row with its
// tasks done and "스프린트 완료", which opens the sprint's summary
// (DatabaseSprintCompleteDialog); a completion says what it changed, with
// the sprint it planned to open, as Notion's "열기" (seen 2026-11-03).
export function DatabaseSprintCompleteAction({ activeViewId, writable, rows, properties, statusPropertyId, onSettings }: {
  activeViewId: string; writable: boolean; rows: readonly GraphRow[]; properties: readonly GraphProperty[]; statusPropertyId: unknown;
  /** Opens the sprints' settings, where automatic sprints turn on. */
  onSettings(): void;
}) {
  const { t, i18n } = useTranslation("databaseTasks");
  const surface = useContext(DatabaseSprintContext);
  const row = useContext(DatabaseSprintRowContext);
  const toast = useOptionalProductToast(), router = useRouter();
  const state = surface?.state;
  const [initial, setInitial] = useState<DatabaseSprintCompletionChoice | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  const current = state?.sprints.find(({ status }) => status === "current");
  if (!surface || !state?.enabled || state.currentViewId !== activeViewId || !row) return null;
  const progress = current ? sprintProgress(rows, properties, state.taskSprintPropertyId, current.id, statusPropertyId) : null;

  // The next sprint starts on the first start day after the current one's
  // planned end, for the sprint length.
  function begin() {
    let first = addDays(current?.end || localDay(), 1);
    while (new Date(`${first}T00:00:00Z`).getUTCDay() !== state!.settings.startWeekday) first = addDays(first, 1);
    setInitial({ start: first, end: addDays(first, state!.settings.durationWeeks * 7 - 1), incompleteTasks: state!.settings.incompleteTasks });
    setFailed(false);
  }

  async function complete(choice: DatabaseSprintCompletionChoice) {
    if (!surface || busy) return;
    setBusy(true);
    const sprintsPageId = state!.sprintsPageId;
    const outcome = await surface.runWithChanges({ action: "complete", incompleteTasks: choice.incompleteTasks,
      ...(choice.start ? { sprintStart: choice.start, sprintEnd: choice.end || choice.start } : { sprintUndated: true }) });
    setBusy(false);
    // Unanswered, it was said, and the sprint shows as it is once read again.
    const answered = outcome.outcome !== "unanswered";
    setFailed(answered && outcome.outcome !== "done");
    if (answered && outcome.outcome !== "done") return;
    setInitial(null);
    if (outcome.changes) toast?.notify(sprintChangesNote(outcome.changes, t, i18n.language), "default",
      { label: t("sprintChanges.open"), onClick: () => router.push(pageHref(outcome.changes!.createdSprintId || sprintsPageId)) });
  }

  return (
    <>
      {createPortal(<>
        {progress ? <span className={styles.progress} data-sprint-progress>
          <ProgressRing {...progress} />{t("sprintTasksDone", progress)}
        </span> : null}
        {writable && current ? <button type="button" className={styles.trigger} data-sprint-complete onClick={begin}>
          {t("completeSprint")}
        </button> : null}
      </>, row)}
      {initial && current ? <DatabaseSprintCompleteDialog current={current} initial={initial}
        next={state.sprints.find(({ status }) => status === "next")} done={progress?.done ?? 0} total={progress?.total ?? 0}
        automatic={state.settings.automatic} busy={busy} failed={failed}
        onSettings={() => { setInitial(null); onSettings(); }} onCancel={() => setInitial(null)} onComplete={(choice) => void complete(choice)} /> : null}
    </>
  );
}
