import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { followVisualViewport } from "@/lib/visualViewportPlace";
import styles from "./PhoneMenu.module.css";

/**
 * The phone's full-screen menu retains the ordinary menu's close action. The
 * menu fills what shows above the soft keyboard (lib/visualViewportPlace.ts),
 * which it follows while it is open.
 */
export function PhoneMenuHeader({ title, onClose }: { title: string; onClose: () => void }) {
  const { t } = useTranslation("common");
  const header = useRef<HTMLDivElement>(null);
  useEffect(() => followVisualViewport(header.current?.closest<HTMLElement>(`.${styles.menu}`) ?? header.current?.parentElement), []);
  return <div ref={header} className={styles.header} data-phone-menu-header
    onFocus={event => event.stopPropagation()} onKeyDown={event => {
      event.stopPropagation();
      if (event.key === "Escape") { event.preventDefault(); onClose(); }
    }}
    onKeyUp={event => event.stopPropagation()}>
    <strong>{title}</strong><button type="button" onClick={onClose}>{t("actions.cancel")}</button>
  </div>;
}
