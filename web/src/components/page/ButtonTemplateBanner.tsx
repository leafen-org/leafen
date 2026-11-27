import { useTranslation } from "react-i18next";
import type { Page } from "@/types";
import styles from "./ButtonTemplateBanner.module.css";

/** Whether a page is, or sits under, a page made in a button's draft. */
export function inButtonTemplate(page: Page, pageById: (id: string) => Page | undefined) {
  const seen = new Set<string>();
  for (let current: Page | undefined = page; current && !seen.has(current.id);
    current = current.parentId ? pageById(current.parentId) : undefined) {
    if (current.buttonTemplate) return true;
    seen.add(current.id);
  }
  return false;
}

/**
 * The yellow line over a page made in a button's draft, as Notion's (seen
 * 2026-10-10): what is open is the template the button inserts, not a page
 * of its own. Notion's help link beside it is left out.
 */
export function ButtonTemplateBanner() {
  const { t } = useTranslation("pageView");
  return <div className={styles.banner} role="status" data-button-template-banner>{t("buttonTemplateEditing")}</div>;
}
