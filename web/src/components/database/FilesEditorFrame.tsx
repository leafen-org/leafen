import type { ReactNode, RefObject } from "react";
import { useTranslation } from "react-i18next";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "../shell/PhoneSheet";
import { CellMenuPortal } from "./PropertyCellMenu";
import { PickerOpened } from "./PropertySelectPicker";
import styles from "./databaseSheet";
import sheet from "./FilesEditorSheet.module.css";

/**
 * Where a files property's editor stands: Notion's files window over the
 * cell from its top left, 240 wide or the cell's width; on a touch screen
 * Notion's phone screen "파일 속성" (seen 2026-11-15 at 402pt in iOS Safari):
 * the whole screen under its title and a blue "완료", the files' lines and
 * "+ 파일 또는 이미지를 추가하세요" running across it between lines. In a
 * table, Tab leaves it for the cell beside (tableCellEditorTab.ts).
 */
export function FilesEditorFrame({ label, triggerRef, editorRef, onClose, children }: {
  label: string;
  triggerRef: RefObject<HTMLButtonElement | null>;
  editorRef: RefObject<HTMLDivElement | null>;
  onClose(): void;
  /** Or what it holds once placed (CellMenuPortal); a phone's sheet is placed at once. */
  children: ReactNode | ((placed: boolean) => ReactNode);
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  const { touch } = useSidebarLayout(true);
  if (touch) {
    return (
      <TouchSheetLayout>
        <PhoneSheet size="screen" title={t("propertyCell:filesSheetTitle")} label={label} sheetRef={editorRef} onClose={onClose}
          action={{ label: t("common:actions.done"), onPress: onClose }} data={{ "data-files-sheet": "" }}>
          <div className={sheet.sheet}>{typeof children === "function" ? children(true) : children}</div>
        </PhoneSheet>
        <PickerOpened />
      </TouchSheetLayout>
    );
  }
  return (
    <CellMenuPortal triggerRef={triggerRef} onClose={onClose} ariaLabel={label} className={styles.filesMenu} backdropClassName={styles.menuBackdrop}
      width={240} fitWidth scroll={false} cover popoverRef={editorRef} tabLeavesCell>
      {children}
    </CellMenuPortal>
  );
}
