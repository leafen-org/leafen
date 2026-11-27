import type { GraphProperty } from "@/contentGraph";
import { WarningTriangleIcon } from "@/icons/leafen";
import { automationActionProblem } from "@/components/automation/automationActionModel";
import { databaseButtonActions } from "@/lib/databaseButtonConfig";
import styles from "./ButtonPropertyHeadMark.module.css";

/**
 * A button property's column head ends in "⚠" while any of its actions lacks
 * what it needs, as Notion's (seen 2026-11-21 in Chrome at 1440: "버튼 ⚠" the
 * moment an unfinished action was added, its editor still open, and after a
 * reload; no tip under the pointer, and none beside its name in a row page's
 * properties). Its editor saves an added or removed action at once
 * (DatabaseButtonActionsEditor), so the mark comes as the action does.
 */
export function ButtonPropertyHeadMark({ property }: { property: GraphProperty }) {
  if (property.type !== "button" || !databaseButtonActions(property).some((action) => automationActionProblem(action) !== null)) return null;
  return <span className={styles.mark} aria-hidden="true" data-button-property-unfinished><WarningTriangleIcon size="regular" /></span>;
}
