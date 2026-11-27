import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ButtonClickIcon, X } from "@/icons/leafen";
import type { GraphProperty } from "@/contentGraph";
import type { DbProperty, DbView, Page } from "@/lib/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { DatabaseButtonActionsEditor } from "./DatabaseButtonActionsEditor";
import styles from "./DatabaseAutomationDialog.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A button property's "자동화 편집", as Notion's (seen 2026-11-02 at 1280px):
 * the automation dialog's 500×600 window in the middle, its head the
 * button's name ("이름 입력" while empty), which also names the window
 * (Notion's, seen 2026-11-09), "다음 경우에 사용:" over one
 * "버튼 클릭" card, the line, "작업" and its actions, and a blue "저장" across
 * its foot. What changes holds, so "×" keeps it too: its actions save as
 * their editor does (DatabaseButtonActionsEditor).
 */
export function DatabaseButtonAutomationDialog({ property, current, properties, pages, views, readOnly, onRename, onSave, onClose }: {
  property: GraphProperty;
  /** The property as it is now, which its actions are saved onto. */
  current(): GraphProperty | undefined;
  properties: DbProperty[];
  pages: Page[];
  views?: DbView[];
  readOnly: boolean;
  onRename(name: string): void;
  onSave(property: GraphProperty): void;
  onClose(): void;
}) {
  const name = property.name;
  const { t } = useTranslation(["databaseView", "common"]);
  const ref = useRef<HTMLDivElement>(null);
  const layer = useOverlayLayer(true, { kind: "dialog", ref, onEscape: onClose });
  const [draftName, setDraftName] = useState(name);
  const rename = () => { const next = draftName.trim(); if (next && next !== name) onRename(next); else setDraftName(name); };
  if (typeof document === "undefined") return null;
  return createPortal(
    <>
      <div className={styles.backdrop} style={layer.zIndex === undefined ? undefined : { zIndex: layer.zIndex - 1 }} aria-hidden="true" onClick={onClose} />
      <div ref={ref} className={styles.dialog} role="dialog" aria-modal="true" aria-label={draftName.trim() || name || t("databaseView:buttonNamePlaceholder")}
        style={layer.zIndex === undefined ? undefined : { zIndex: layer.zIndex }} data-automation-dialog data-button-automation-dialog onKeyDown={layer.onKeyDown}>
        <button type="button" className={`${styles.close} ${styles.buttonClose}`} aria-label={t("common:actions.close")} onClick={() => { rename(); onClose(); }}>
          <X size="tiny" aria-hidden="true" />
        </button>
        <header className={`${styles.head} ${styles.buttonHead}`}>
          <input className={`${styles.name} ${styles.buttonName}`} value={draftName} maxLength={100} readOnly={readOnly}
            aria-label={t("databaseView:buttonAutomationName")} placeholder={t("databaseView:buttonNamePlaceholder")}
            onChange={(event) => setDraftName(event.currentTarget.value)} onBlur={rename}
            onKeyDown={(event) => { if (fieldEnter(event)) event.currentTarget.blur(); }} />
        </header>
        <div className={`${styles.body} ${styles.buttonBody}`}>
          <section className={`${styles.section} ${styles.buttonTrigger}`} aria-label={t("databaseView:automationTrigger")}>
            <p className={styles.sectionLabel}>{t("databaseView:automationUseWhen")}</p>
            <div className={styles.card} data-automation-condition="button">
              {/* Notion's pointer with rays round its tip, as a page button's "버튼 클릭" (seen 2026-11-15 in a database
                  button's automation, Chrome 1440: its "cursorClick" mark). */}
              <span className={styles.cardIcon} aria-hidden="true"><ButtonClickIcon size="small" /></span>
              <span className={styles.cardLabel}>{t("databaseView:buttonClicked")}</span>
            </div>
          </section>
          <span className={`${styles.line} ${styles.buttonLine}`} aria-hidden="true" />
          <section className={styles.section} aria-label={t("databaseView:automationAction")}>
            <p className={styles.sectionLabel}>{t("databaseView:automationAction")}</p>
            <fieldset className={styles.actions} disabled={readOnly}>
              <DatabaseButtonActionsEditor key={property.id} property={property} current={current} onSave={onSave}
                properties={properties} pages={pages} views={views} newWhenEmpty />
            </fieldset>
          </section>
        </div>
        <footer className={`${styles.foot} ${styles.wideFoot}`}>
          <button type="button" className={`${styles.save} ${styles.wideSave}`} data-button-automation-save onClick={() => { rename(); onClose(); }}>
            {t("common:actions.save")}
          </button>
        </footer>
      </div>
    </>,
    document.body,
  );
}
