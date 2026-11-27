import { useContext, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import type { DatabaseToolbarTaskFeatureWire } from "@/databaseToolbarConfigWire.generated";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useProductToast } from "@/lib/productToast";
import { useRouter } from "@/lib/router";
import { propertyAfterTypeChange } from "./propertyEditModel";
import { DatabaseSprintContext } from "./databaseSprintContext";
import { DatabaseTaskConvertDialog } from "./DatabaseTaskConvertDialog";
import { taskConversionChange, type TaskDatabaseChoice } from "./taskConversionChange";

/**
 * Turning a database into a task database and back, as Notion's (seen
 * 2026-11-02): its window makes what is chosen as "새로 만들기", turns a select
 * chosen for the status into a status, and says so after ("작업 데이터베이스로
 * 전환됨", "내 작업으로 이동하기"); "작업 데이터베이스 되돌리기" asks "홈에서
 * 제거하시겠습니까?" and turns that status back into a select. Either way is
 * one change, as Notion's: the properties it makes or retypes go to the
 * server with the task settings in one request, so a refused one leaves none
 * of them (database_task_conversion.go; 2026-11-15: made one by one first, a
 * refused conversion left its 담당자, 마감일 and 상태).
 */
export function useTaskDatabaseConversion({ dataSourceId, properties, taskConfiguration, writable, onConfigure }: {
  dataSourceId: string;
  properties: readonly GraphProperty[];
  taskConfiguration: DatabaseToolbarTaskFeatureWire;
  writable: boolean;
  /** Saves the task settings with the properties the change makes or retypes, in one request. */
  onConfigure(configuration: DatabaseToolbarTaskFeatureWire, madeProperties?: readonly GraphProperty[]): Promise<boolean>;
}) {
  const { t } = useTranslation("databaseTasks");
  const toast = useProductToast(), router = useRouter();
  const sprints = useContext(DatabaseSprintContext);
  const [busy, setBusy] = useState(false);

  async function convert(choice: TaskDatabaseChoice, close: () => void) {
    if (!writable || busy) return;
    setBusy(true);
    try {
      const { tasks, made } = taskConversionChange(choice, properties, dataSourceId,
        { assignee: t("assignee"), dueDate: t("dueDate"), status: t("taskStatus") });
      const saved = await onConfigure({ ...taskConfiguration, tasks }, made);
      if (!saved) return;
      close();
      toast.notify(t("convertedTitle"), "default", { label: t("goToMyTasks"), onClick: () => router.push("/my-tasks"), autoDismissMs: null },
        undefined, t("convertedDetail"));
    } finally {
      setBusy(false);
    }
  }

  async function revert() {
    if (!writable || busy) return;
    if (sprints?.state?.enabled === true) { toast.notify(t("turnOffSprintsFirst"), "error"); return; }
    if (!await confirmAction({ message: t("revertTitle"), detail: t("revertDetail"), confirmLabel: t("revertConfirm"), danger: true })) return;
    const tasks = taskConfiguration.tasks ?? {};
    setBusy(true);
    try {
      const status = tasks.statusWasSelect === true ? properties.find((property) => property.id === tasks.statusPropertyId && property.type === "status") : undefined;
      await onConfigure({ ...taskConfiguration, tasks: { enabled: false } }, status ? [propertyAfterTypeChange(status, "select", dataSourceId)] : []);
    } finally {
      setBusy(false);
    }
  }

  return {
    renderTaskConversion: writable ? (close: () => void) => (
      <DatabaseTaskConvertDialog properties={properties} busy={busy} onClose={close} onConvert={(choice) => void convert(choice, close)} />
    ) : undefined,
    onRevertTaskDatabase: writable ? () => void revert() : undefined,
  };
}
