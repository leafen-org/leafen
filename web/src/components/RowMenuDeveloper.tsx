import { NativeActionMenuItem, NativeMenuSection, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, CodeIcon } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import {
  readDeveloperIds,
  shortDeveloperId,
  toggleDeveloperBar,
  useDeveloperMode,
} from "@/lib/developerMode";
import { useProductToast } from "@/lib/productToast";
import type { Page, Session } from "@/types";
import styles from "./RowMenu.module.css";

/**
 * The page menu's "개발자" while "개발자 기능 활성화" is on, as Notion's
 * (seen 2026-10-02), at the menu's end: the page's API ID to copy, shown
 * shortened, and the developer bar's toggle, ⌘⇧.
 */
export function RowMenuDeveloper({ page, session, busy, onClose }: {
  page: Page;
  session: Session;
  busy: boolean;
  onClose(): void;
}) {
  const on = useDeveloperMode();
  const { t } = useTranslation("rowMenu");
  const { notify } = useProductToast();
  const [open, setOpen] = useState(false);
  const [pageId, setPageId] = useState("");

  useEffect(() => {
    if (!on || !open) return;
    const controller = new AbortController();
    readDeveloperIds(session, { pageId: page.id }, controller.signal)
      .then((ids) => setPageId(ids.page ?? ""))
      .catch(() => undefined);
    return () => controller.abort();
  }, [on, open, page.id, session]);

  if (!on) return null;

  async function copyPageId() {
    if (await copyText(pageId)) {
      notify(t("developer.copiedPageId"), "success");
      onClose();
    } else {
      notify(t("developer.couldntCopyPageId"), "error");
    }
  }

  return (
    <>
      <NativeMenuSeparator className={styles.divider} />
      <NativeActionMenuItem
        type="button"
        className={styles.item}
        data-menu-item
        data-page-developer-menu
        role="menuitem"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <CodeIcon size="regular" aria-hidden="true" />
        <span>{t("developer.label")}</span>
        <ChevronRight className={styles.menuDisclosure} data-open={open ? "true" : undefined} size="small" aria-hidden="true" />
      </NativeActionMenuItem>
      {open ? (
        <NativeMenuSection aria-label={t("developer.label")} data-page-developer-group>
          <NativeActionMenuItem type="button" className={styles.item} data-menu-item data-copy-page-id role="menuitem"
            disabled={busy || !pageId} onClick={() => void copyPageId()}>
            <span>{t("developer.copyPageId")}</span>
            <span className={styles.itemHint}>{pageId ? shortDeveloperId(pageId) : "…"}</span>
          </NativeActionMenuItem>
          <NativeActionMenuItem type="button" className={styles.item} data-menu-item data-toggle-developer-bar role="menuitem"
            onClick={() => { toggleDeveloperBar(); onClose(); }}>
            <span>{t("developer.toggleBar")}</span>
            <span className={styles.itemHint}>{menuKeyHint("⌘⇧.")}</span>
          </NativeActionMenuItem>
        </NativeMenuSection>
      ) : null}
    </>
  );
}
