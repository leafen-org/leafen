import { useTranslation } from "react-i18next";
import { Plus } from "@/icons/leafen";
import { usePageActionAccess } from "@/lib/pageActionAccessContext";
import panelStyles from "./LibraryPanel.module.css";
import styles from "./LibraryView.module.css";

export function LibraryAddRow({ parentId, indent, onAdd }: { parentId: string; indent: number; onAdd(): void }) {
  const { t } = useTranslation("library");
  // An expanded parent's final row is an explicit approach to that parent.
  const access = usePageActionAccess(parentId, true);
  if (!access.canEdit) return null;
  return (
    <div className={styles.row} role="row" data-library-add-row={parentId}>
      <span className={styles.nameColumn} role="cell" style={{ paddingInlineStart: indent }}>
        <button type="button" className={panelStyles.addInside} onClick={onAdd}>
          <Plus size="small" aria-hidden="true" /><span>{t("addPageInside")}</span>
        </button>
      </span>
    </div>
  );
}
