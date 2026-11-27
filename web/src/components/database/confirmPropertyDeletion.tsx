import { i18next } from "@/i18n";
import { confirmAction, confirmChoice } from "@/components/ConfirmActionDialog";
import { Trash } from "@/icons/leafen";
import { chooseRelatedPropertyDeletion } from "@/lib/relatedPropertyDeletion";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";

type DeletedProperty = {
  id: string;
  type: string;
  /** The database it belongs to. */
  dataSourceId: string;
  config?: Record<string, unknown> | null;
};

/**
 * Asks before a property goes to the database's deleted properties, as
 * Notion's does (seen 2026-10-06). A two-way relation to another database
 * asks what happens to its other side (seen 2026-11-11): "관련 속성도 함께
 * 삭제", chosen first, or "이 속성만 삭제", which ends the two-way sync — each
 * a card naming that database, under a bin, "삭제하기" in solid red. True
 * once the person confirms; the choice goes with the deletion
 * (lib/relatedPropertyDeletion.ts).
 */
export async function confirmPropertyDeletion({ property, database, relatedDatabase }: {
  property: DeletedProperty;
  /** The database's own name. */
  database: string;
  /** The name of the database a relation points to, as the reader sees it. */
  relatedDatabase?: (dataSourceId: string) => string | undefined;
}): Promise<boolean> {
  const t = i18next.t.bind(i18next);
  const target = typeof property.config?.relationDatabaseId === "string" ? property.config.relationDatabaseId : "";
  const twoWay = property.type === "relation" && typeof property.config?.relatedPropertyId === "string"
    && Boolean(property.config.relatedPropertyId) && Boolean(target) && target !== property.dataSourceId;
  const message = t("tableView:deletePropertyConfirm", { database });
  if (!twoWay) {
    return confirmAction({ message, confirmLabel: t("common:actions.delete"), danger: true, roomy: true });
  }
  const other = relatedDatabase?.(target)?.trim() || inlineDatabasePlaceholderTitle();
  const choice = await confirmChoice({
    message, emphasis: database, icon: <Trash size="card" aria-hidden="true" />, dangerChoices: true,
    confirmLabel: t("tableView:deleteRelatedPropertyConfirm"), danger: "filled",
    choices: [
      { value: "delete", label: t("tableView:deleteRelatedPropertyBoth"), description: t("tableView:deleteRelatedPropertyBothDetail", { database: other }) },
      { value: "keep", label: t("tableView:deleteRelatedPropertyOnly"), description: t("tableView:deleteRelatedPropertyOnlyDetail", { database: other }) },
    ],
  });
  if (choice !== "delete" && choice !== "keep") return false;
  chooseRelatedPropertyDeletion(property.id, choice);
  return true;
}
