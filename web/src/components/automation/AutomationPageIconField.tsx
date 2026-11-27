import { useTranslation } from "react-i18next";
import { IconChoiceButton } from "@/components/page/IconChoiceButton";
import { useProductPages } from "@/components/product/productPagesContext";
import type { SetPageIconAutomationAction } from "@/lib/types";
import styles from "./automationPageIconField.module.css";

/**
 * "페이지 아이콘 설정"'s one line, as Notion's card (seen 2026-11-21 in a database's automation and a button
 * property, Chrome 1440 dark, two tabs at different times): "아이콘" and Notion's "아이콘 추가" button
 * (IconChoiceButton). "제거" leaves the step asking for an icon again; what it lacks shows in its card's mark alone,
 * its words staying as they are.
 */
export function AutomationPageIconField({ action, onChange }: {
  action: SetPageIconAutomationAction;
  onChange: (action: SetPageIconAutomationAction) => void;
}) {
  const { t } = useTranslation(["databaseView"]);
  const workspaceId = useProductPages()?.workspaceId ?? "";
  const chosen = action.icon && action.iconType ? { icon: action.icon, iconType: action.iconType } : null;
  return <div className={styles.line}>
    <span className={styles.label}>{t("databaseView:pageIconAction.label")}</span>
    <IconChoiceButton icon={chosen} label={t("databaseView:pageIconAction.add")} workspaceId={workspaceId}
      marker="automation-page-icon-picker"
      onChoose={(next) => onChange({ ...action, icon: next?.icon ?? "", iconType: next?.iconType ?? "" })} />
  </div>;
}
