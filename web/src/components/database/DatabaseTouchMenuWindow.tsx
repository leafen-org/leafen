import type { CSSProperties, ReactNode, RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { appleTouchDevice } from "@/lib/iosViewport";
import { phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { SheetWindowContext } from "../shell/sheetWindow";
import styles from "./DatabaseTouchMenuWindow.module.css";

/**
 * A database toolbar's filter or sort on a touch screen wider than a phone,
 * as Notion's iPad (seen 2026-11-12 in iPad Safari 1210 and 834 wide, dark):
 * not a list under its button but a window in the screen's middle over the
 * sheets' shade (DatabaseTouchMenuWindow.module.css), headed as a sheet ("필터 추가" or "새 정렬" on the
 * head's top line, a blue "완료" at its end, a line under its 44), then the
 * desktop's list — "필터 기준" and the properties in 28px rows — with its
 * search waiting for a tap, so no keyboard rises over it
 * (DatabasePropertyPicker).
 */
export function DatabaseTouchMenuWindow({ title, label, menuRef, layer, shadeLayer, onClose, data, children }: {
  title: ReactNode;
  label: string;
  menuRef: RefObject<HTMLDivElement | null>;
  /** The menu's layer, as the toolbar placed its list. */
  layer?: CSSProperties["zIndex"];
  shadeLayer?: CSSProperties["zIndex"];
  onClose(): void;
  data?: Record<`data-${string}`, string | undefined>;
  children: ReactNode;
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  if (typeof document === "undefined") return null;
  return createPortal(
    <>
      <button type="button" className={`${sheetStyles.shade} ${styles.shade}`} style={{ zIndex: shadeLayer }} tabIndex={-1}
        aria-label={t("closeMenu")} onClick={onClose} data-toolbar-menu-backdrop data-apple={appleTouchDevice() ? "true" : "false"} />
      <div ref={menuRef} className={styles.window} style={{ zIndex: layer }} role="dialog" aria-label={label} data-touch-menu-window="" {...data}>
        <div className={`${sheetStyles.head} ${styles.head}`}>
          <span className={sheetStyles.headSide} />
          <strong className={sheetStyles.title}>{title}</strong>
          <span className={`${sheetStyles.headSide} ${sheetStyles.headEnd}`}>
            <button type="button" className={sheetStyles.headButton} data-touch-menu-done onClick={onClose}>{t("common:actions.done")}</button>
          </span>
        </div>
        {/* A whole-screen pick opened from it is a window of its own in its place (sheetWindow.ts). */}
        <div className={styles.body}><SheetWindowContext.Provider value={true}>{children}</SheetWindowContext.Provider></div>
      </div>
    </>,
    document.body,
  );
}
