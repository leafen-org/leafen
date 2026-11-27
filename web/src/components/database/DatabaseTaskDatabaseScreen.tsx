import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import type { DatabaseToolbarTaskFeatureWire } from "@/databaseToolbarConfigWire.generated";
import { DatabaseTaskSettingsHeader } from "./DatabaseTaskSettingsHeader";
import {
  initialTaskRole, missingTaskRoles, NEW_TASK_PROPERTY, taskConversionChange, taskRoleCandidates,
  type TaskDatabaseChoice, type TaskRole,
} from "./taskConversionChange";
import styles from "./DatabaseTaskSettings.module.css";
import ownStyles from "./DatabaseTaskDatabaseScreen.module.css";
import { AppSelect } from "@/components/AppSelect";

function stringValue(value: unknown, fallback: string) {
  return typeof value === "string" && value ? value : fallback;
}

// A task database names its status, assignee and due date properties; tasks
// and sprints read them from here. All three are asked for as Notion's
// conversion window asks them — a property of the kind or "새로 만들기", never
// none, the window saying which are not chosen yet (taskConversionChange) —
// and made with the settings in one change (database_task_conversion.go).
export function DatabaseTaskDatabaseScreen({
  backLabel,
  properties,
  sprintsOn,
  taskConfiguration,
  writable,
  onBack,
  onChange,
}: {
  backLabel: string;
  properties: readonly GraphProperty[];
  sprintsOn: boolean;
  taskConfiguration: DatabaseToolbarTaskFeatureWire;
  writable: boolean;
  onBack(): void;
  /** Saves the task settings with the properties the change makes or retypes, in one request. */
  onChange(next: DatabaseToolbarTaskFeatureWire, madeProperties?: readonly GraphProperty[]): Promise<boolean>;
}) {
  const { t } = useTranslation("databaseTasks");
  const tasks = taskConfiguration.tasks ?? {};
  const enabled = tasks.enabled === true;
  const candidates = taskRoleCandidates(properties);
  const [choice, setChoice] = useState<TaskDatabaseChoice>(() => ({
    assigneeId: stringValue(tasks.assigneePropertyId, initialTaskRole(candidates.assigneeId)),
    statusId: stringValue(tasks.statusPropertyId, initialTaskRole(candidates.statusId)),
    dueId: stringValue(tasks.dueDatePropertyId, initialTaskRole(candidates.dueId)),
  }));
  const [missing, setMissing] = useState<readonly TaskRole[]>([]);
  const [busy, setBusy] = useState(false);

  async function save(next: Record<string, unknown>) {
    if (!writable || busy) return;
    setBusy(true);
    try {
      await onChange({ ...taskConfiguration, tasks: next });
    } finally {
      setBusy(false);
    }
  }

  // Each property chosen as "새로 만들기" is made with the settings; a role
  // not chosen yet stops it and says so.
  async function convert() {
    if (!writable || busy) return;
    const lacking = missingTaskRoles(choice);
    setMissing(lacking);
    if (lacking.length) return;
    const dataSourceId = properties[0]?.dataSourceId ?? "";
    const change = taskConversionChange(choice, properties, dataSourceId,
      { assignee: t("assignee"), dueDate: t("dueDate"), status: t("taskStatus") });
    setBusy(true);
    try {
      await onChange({ ...taskConfiguration, tasks: change.tasks }, change.made);
    } finally {
      setBusy(false);
    }
  }

  const field = (label: string, role: TaskRole) => (
    <label className={`${styles.selectField} ${ownStyles.field}`} data-required={missing.includes(role) && !choice[role] || undefined}>
      <span>{label}</span>
      <AppSelect aria-label={label} value={choice[role]} disabled={!writable || busy || !candidates[role].length}
        onChange={(event) => { const value = event.currentTarget.value; setChoice((current) => ({ ...current, [role]: value })); }}>
        {choice[role] ? null : <option value="" disabled>{t("pickPlaceholder")}</option>}
        {candidates[role].map((property) => <option key={property.id} value={property.id}>{property.name}</option>)}
        <option value={NEW_TASK_PROPERTY}>{t("newProperty")}</option>
      </AppSelect>
      {missing.includes(role) && !choice[role] ? <span className={ownStyles.required}>{t("requiredProperty")}</span> : null}
    </label>
  );

  return (
    <div className={styles.panel} data-database-task-settings data-task-screen="task-database" aria-busy={busy}>
      <DatabaseTaskSettingsHeader title={t("taskDatabase")} backLabel={backLabel} onBack={onBack} />
      <div className={styles.body}>
        <p>{t("taskDatabaseDescription")}</p>
        {field(t("taskStatus"), "statusId")}
        {field(t("assignee"), "assigneeId")}
        {field(t("dueDate"), "dueId")}
        <button type="button" className={styles.primaryAction} disabled={!writable || busy}
          onClick={() => void convert()}>
          {enabled ? t("saveChanges") : t("convertToTaskDatabase")}
        </button>
        {enabled && (
          <button type="button" className={styles.secondaryAction} disabled={!writable || busy || sprintsOn}
            title={sprintsOn ? t("turnOffSprintsFirst") : undefined}
            onClick={() => void save({ enabled: false })}>
            <span>{t("undoTaskDatabase")}</span>
          </button>
        )}
        {enabled && sprintsOn && <p>{t("turnOffSprintsFirst")}</p>}
      </div>
    </div>
  );
}
