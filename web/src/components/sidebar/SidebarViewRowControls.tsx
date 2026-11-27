import { useTranslation } from "react-i18next";
import { DotsHorizontal, Plus } from "@/icons/leafen";
import { styles } from "./sidebarStyles";
import { pageActionHintKey, usePageActionAccess } from "@/lib/pageActionAccessContext";
import { treeMenuPoint } from "./pageTreeModel";

export function SidebarViewRowControls({ rowId, title, onMenu, onAdd }: {
  rowId: string;
  title: string;
  /** Opens the row's menu at a point (treeMenuPoint), from its "⋯". */
  onMenu(trigger: HTMLElement, point: { x: number; y: number }): void;
  onAdd(): void;
}) {
  const { t } = useTranslation("pageTreeItem");
  const access = usePageActionAccess(rowId);
  return (
    <span className={styles.treeActions} onMouseEnter={access.approach} onFocusCapture={access.approach}>
      {access.canEdit ? (
        <button type="button" className={styles.iconBtn} title={t("addInsideTitle")}
          aria-label={t("addInside", { title })} data-sidebar-view-row-add onClick={onAdd}><Plus size="regular" /></button>
      ) : null}
      <button type="button" className={styles.iconBtn} title={t(pageActionHintKey(access.canEdit), { title })}
        aria-label={t("openActions", { title })} data-sidebar-view-row-more
        onClick={(event) => onMenu(event.currentTarget, treeMenuPoint(event))}><DotsHorizontal size="regular" /></button>
    </span>
  );
}
