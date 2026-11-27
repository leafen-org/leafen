import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { TableIcon, X } from "@/icons/leafen";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { databaseStarterLabel, databaseStarterMadeAtOnce, type DatabaseStarterSelection, type DatabaseStarterTemplate } from "@/lib/databaseStarterCatalog";
import { DatabaseStarterGallery } from "@/components/database/DatabaseStarterGallery";
import { DatabaseStarterCustomizer } from "@/components/database/DatabaseStarterCustomizer";
import { keepLayerKeys, useOverlayLayer } from "@/lib/overlayLayer";
import type { EditorPageStarterActions } from "./editorRuntimeTypes";
import windowStyles from "@/components/sidebar/AddPageWindow.module.css";
import styles from "./PageDatabaseStartPicker.module.css";

/** Opening, searching and customizing an empty page creates nothing. */
export function PageDatabaseStartPicker({ actions, onClose }: { actions: EditorPageStarterActions; onClose(): void }) {
  const { t } = useTranslation(["editor", "sidebar", "common"]);
  const labelId = useId();
  const dialogRef = useRef<HTMLElement>(null), overlayRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [template, setTemplate] = useState<DatabaseStarterTemplate | null>(null);
  const [failed, setFailed] = useState(false), [busy, setBusy] = useState(false);
  const work = useRef<AbortController | null>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], initialFocus: searchRef,
    onEscape: () => { if (!work.current) onClose(); },
  });
  useEffect(() => () => { work.current?.abort(); }, []);
  async function choose(source?: DatabaseContainerSource, selection?: DatabaseStarterSelection, name?: string) {
    if (work.current) return;
    const controller = new AbortController(); work.current = controller;
    setBusy(true); setFailed(false);
    try {
      const saved = selection ? await actions.onStartTemplate(selection, controller.signal, name)
        : source ? await actions.onStartLinkedDatabase(source, controller.signal)
        : await actions.onStartDatabase("blank", controller.signal);
      if (!controller.signal.aborted) { if (saved) onClose(); else setFailed(true); }
    } catch { if (!controller.signal.aborted) setFailed(true); }
    finally { if (!controller.signal.aborted) { work.current = null; setBusy(false); } }
  }
  return createPortal(<div ref={overlayRef} className={windowStyles.overlay} style={{ zIndex }} onKeyDown={keepLayerKeys}>
    <button className={windowStyles.backdrop} type="button" tabIndex={-1} aria-label={t("common:actions.close")}
      onClick={() => { if (!work.current) onClose(); }} />
    <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={template ? undefined : labelId}
      aria-label={template ? t("sidebar:addWindow.customize", { title: databaseStarterLabel(template.name) }) : undefined}
      className={windowStyles.window} aria-busy={busy || undefined} data-page-database-start-picker>
      {template ? <DatabaseStarterCustomizer key={template.key} template={template} busy={busy} failed={failed} previewAuthor={actions.previewAuthor}
        onCancel={() => { setTemplate(null); setFailed(false); }} onApply={selection => void choose(undefined, selection, databaseStarterLabel(template.name))} /> : <>
        <header className={styles.head}>
          <div className={styles.start}><button type="button" className={windowStyles.close} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose}><X size="tiny" /></button></div>
          <label className={windowStyles.search}><span id={labelId} className={styles.srOnly}>{t("editor:starter.chooseDatabase")}</span>
            <input ref={searchRef} value={query} disabled={busy} aria-label={t("sidebar:addWindow.search")} placeholder={t("sidebar:addWindow.search")} onChange={event => setQuery(event.target.value)} /></label>
          <div className={styles.end}><button type="button" className={styles.blank} disabled={busy} onClick={() => void choose()}><TableIcon size="regular" />{busy ? t("editor:starter.creating") : t("sidebar:addWindow.blankDatabase")}</button></div>
        </header>
        <div className={styles.body}>{failed ? <p role="alert">{t("editor:couldntCreateDatabase")}</p> : null}
          <DatabaseStarterGallery query={query} busy={busy} loadSources={actions.loadDatabaseSources} onSource={source => void choose(source)}
            onTemplate={picked => { const selection = databaseStarterMadeAtOnce(picked); if (selection) void choose(undefined, selection, databaseStarterLabel(picked.name)); else setTemplate(picked); }} />
        </div>
      </>}
    </section>
  </div>, document.body);
}
