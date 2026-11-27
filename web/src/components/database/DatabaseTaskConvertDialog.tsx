import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { CheckboxIcon, ClockIcon, GridIcon, Plus } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { NotionSelect } from "./NotionSelect";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import styles from "./DatabaseTaskConvertDialog.module.css";
import { initialTaskRole, missingTaskRoles, NEW_TASK_PROPERTY, taskRoleCandidates, type TaskDatabaseChoice, type TaskRole } from "./taskConversionChange";

// A few tasks of Leafen's own for the picture at the left (the same idea as
// Notion's, in Leafen's words).
// Notion's example tasks (seen 2026-11-07): an emoji each, a status, a
// project cut short and a green or orange mark.
const SAMPLE = [
  ["📅", "sampleTask1", "sampleBlocked", "green"], ["📚", "sampleTask2", "sampleInProgress", "orange"],
  ["🎨", "sampleTask3", "sampleInProgress", "orange"], ["🛫", "sampleTask4", "sampleNotStarted", "green"],
  ["🚧", "sampleTask5", "sampleNotStarted", "orange"], ["👩‍💻", "sampleTask6", "sampleNotStarted", "green"],
] as const;

/**
 * "작업 데이터베이스로 전환" as Notion's (seen 2026-11-02): a 768×450 window
 * in the middle, rounded 12, 32px in — at the left what a task database is
 * for, at the right a line saying a task database needs those three
 * properties over the assignee, status and due date, each under a grey line
 * and picked from the database's properties (or "새로 만들기"), a select
 * chosen for the status saying it becomes one (a checkbox stays one); a blue "작업 데이터베이스로
 * 전환" under them. Its help link is left out.
 */
export function DatabaseTaskConvertDialog({ properties, busy, onConvert, onClose }: {
  properties: readonly GraphProperty[];
  busy: boolean;
  /** Converts with the choice, making what is chosen as "새로 만들기" in the same change. */
  onConvert(choice: TaskDatabaseChoice): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["databaseTasks", "homeView"]);
  const dialogRef = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "alertdialog", ref: dialogRef, onEscape: () => { if (!busy) onClose(); } });
  // Each starts on the database's one property that can be it, on "새로 만들기"
  // where none can, and waits to be chosen among more, whatever their names
  // (Notion's, seen 2026-11-03); none is ever a choice (taskConversionChange).
  const { assigneeId: people, statusId: statuses, dueId: dates } = taskRoleCandidates(properties);
  const [assigneeId, setAssigneeId] = useState(() => initialTaskRole(people));
  const [statusId, setStatusId] = useState(() => initialTaskRole(statuses));
  const [dueId, setDueId] = useState(() => initialTaskRole(dates));
  // Pressed with one not chosen, those say so in red until chosen; one with no
  // property of its kind is "새로 만들기", dimmed, and doesn't open (Notion's, seen 2026-11-18).
  const [missing, setMissing] = useState<readonly TaskRole[]>([]);
  const chosenStatus = statuses.find(({ id }) => id === statusId);
  const options = (list: readonly GraphProperty[]) => [
    ...list.map((property) => ({ value: property.id, label: property.name, icon: <PropertyTypeIcon type={property.type} size="small" /> })),
    { value: NEW_TASK_PROPERTY, label: t("newProperty"), icon: <Plus size="small" /> },
  ];
  const field = (key: string, role: TaskRole, lead: string, value: string, list: readonly GraphProperty[], set: (value: string) => void, note?: string) => {
    const required = missing.includes(role) && !value;
    return (
      <div className={styles.field} data-task-convert-field={key} data-required={required || undefined}>
        <span className={styles.lead}>{lead}</span>
        <span className={styles.pickerPlace}>
          <NotionSelect ariaLabel={lead} value={value} options={options(list)} placeholder={t("pickPlaceholder")}
            buttonClassName={styles.picker} disabled={busy || !list.length} onChange={set} />
          {required ? <span className={styles.required}>{t("requiredProperty")}</span> : null}
        </span>
        {note ? <span className={styles.note}>{note}</span> : null}
      </div>
    );
  };
  const convert = () => {
    const choice = { assigneeId, statusId, dueId };
    const lacking = missingTaskRoles(choice);
    setMissing(lacking);
    if (!lacking.length) onConvert(choice);
  };
  if (typeof document === "undefined") return null;
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} aria-label={t("cancel")} tabIndex={-1} disabled={busy} onClick={onClose} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-label={t("convertToTaskDatabase")} data-task-convert-dialog>
        <div className={styles.picture} aria-hidden="true">
          <div className={styles.pictureHead}>
            <span className={styles.pictureMark}>📋</span>
            <strong>{t("convertDialogLead")}</strong>
          </div>
          <div className={styles.sample}>
            <span className={styles.sampleHead}><CheckboxIcon size="small" />{t("homeView:myTasks")}</span>
            {SAMPLE.map(([emoji, task, status, mark]) => (
              <span key={task} className={styles.sampleRow}>
                <span className={styles.sampleEmoji}>{emoji}</span>
                <span className={styles.sampleTitle}>{t(task)}</span>
                <span className={styles.sampleStatus} data-status={status}>{t(status)}</span>
                <span className={styles.sampleProject}>{t("sampleProject")}</span>
                <span className={styles.sampleMark} data-mark={mark}>{mark === "green" ? <GridIcon size="tiny" /> : <ClockIcon size="tiny" />}</span>
              </span>
            ))}
          </div>
        </div>
        <div className={styles.form}>
          <p>{t("convertDialogNeeds")}</p>
          {field("assignee", "assigneeId", t("pickAssignee"), assigneeId, people, setAssigneeId)}
          {field("status", "statusId", t("pickStatus"), statusId, statuses, setStatusId,
            chosenStatus?.type === "select" ? t("becomesStatus") : undefined)}
          {field("due", "dueId", t("pickDue"), dueId, dates, setDueId)}
          <button type="button" className={styles.convert} disabled={busy} data-task-convert onClick={convert}>
            {t("convertToTaskDatabase")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
