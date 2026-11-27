import { useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { MinusIcon, Plus, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { databaseToolbarConfigBounds, type DatabaseMyTaskSource } from "@/databaseToolbarConfigWire.generated";
import styles from "./MyTaskSourcesDialog.module.css";

/**
 * A task database's icon wherever "내 작업" names one — its sources window,
 * "새 작업" menu and rows: an emoji, a built-in icon (a task tracker's green
 * check) or a picture drawn as everywhere else, its table when it has none;
 * 20px in each, as Notion's (seen 2026-11-08).
 */
export function MyTaskDatabaseIcon({ icon, iconType }: Pick<DatabaseMyTaskSource, "icon" | "iconType">) {
  return <span aria-hidden="true" style={{ display: "inline-flex", flex: "none", alignItems: "center", justifyContent: "center", width: 20, height: 20 }}>
    <PageIconGlyph page={{ icon, iconType: iconType || "none" }} size={18} box={20} fallback="database" />
  </span>;
}

/**
 * "작업 소스 추가", as Notion's (seen 2026-11-03, 2026-11-08): up to ten task
 * databases, those to pick on the left under a search, the picked ones on the
 * right under their count, each with its icon and the path to it, "+" or "−"
 * at its end under the pointer; "완료" keeps them. Only task databases can be
 * picked.
 */
export function MyTaskSourcesDialog({ candidates, picked: initial, onSave, onClose }: {
  candidates: DatabaseMyTaskSource[];
  picked: DatabaseMyTaskSource[];
  onSave: (ids: string[]) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation("myTasks");
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const [picked, setPicked] = useState(() => initial.map((source) => source.dataSourceId));
  const [query, setQuery] = useState("");
  const [helpOpen, setHelpOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose });
  const max = databaseToolbarConfigBounds.myTaskMaxSources;
  const byId = useMemo(() => new Map(candidates.map((source) => [source.dataSourceId, source])), [candidates]);
  // A task database with no name is called as an untitled database is.
  const name = (source: DatabaseMyTaskSource) => source.title || untitledPageDisplayTitle({ kind: "database", viewBlock: false });
  // The pages above it hold a database in their bodies, so each is a page.
  const path = (source: DatabaseMyTaskSource) => source.path.map((title) => title || untitledPageDisplayTitle("page")).join(" / ");
  const words = query.trim().toLowerCase();
  const choices = candidates.filter((source) => !picked.includes(source.dataSourceId)
    && (!words || `${name(source)} ${path(source)}`.toLowerCase().includes(words)));

  async function save() {
    setBusy(true);
    setFailed(false);
    try {
      await onSave(picked);
      onClose();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  }

  const dialog = (
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("close")} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} data-my-task-sources>
        <button type="button" className={styles.close} aria-label={t("close")} onClick={onClose}><X size="small" aria-hidden="true" /></button>
        <h2 id={titleId} className={styles.title}>{t("sourcesTitle")}</h2>
        <p className={styles.lead}>{t("sourcesLead", { count: max })}</p>
        <div className={styles.columns}>
          <div className={styles.pick}>
            <input type="search" className={styles.search} value={query} placeholder={t("searchDatabases")} aria-label={t("searchDatabases")}
              onChange={(event) => setQuery(event.target.value)} data-my-task-sources-search />
            <ul className={styles.list}>
              {choices.map((source) => (
                <li key={source.dataSourceId}>
                  <button type="button" className={styles.item} disabled={picked.length >= max} data-described={source.path.length ? "true" : undefined}
                    onClick={() => setPicked((current) => [...current, source.dataSourceId])} data-my-task-source-choice={source.dataSourceId}>
                    <span className={styles.itemIcon}><MyTaskDatabaseIcon icon={source.icon} iconType={source.iconType} /></span>
                    <span className={styles.itemText}><span className={styles.itemName}>{name(source)}</span>
                      {source.path.length ? <span className={styles.itemPath}>{path(source)}</span> : null}</span>
                    <span className={styles.mark} aria-hidden="true"><Plus size="small" /></span>
                  </button>
                </li>
              ))}
              {/* With every one picked or none matching the search, the column stays empty, as Notion's (seen 2026-11-08). */}
              {candidates.length === 0 ? <li className={styles.none}>{t("noTaskDatabases")}</li> : null}
            </ul>
          </div>
          <div className={styles.picked} data-my-task-sources-picked>
            <p className={styles.count}>{t("pickedCount", { count: picked.length, max })}</p>
            <ul className={styles.list}>
              {picked.map((id) => byId.get(id)).filter((source): source is DatabaseMyTaskSource => Boolean(source)).map((source) => (
                <li key={source.dataSourceId}>
                  <button type="button" className={styles.item} aria-label={t("removeSource", { name: name(source) })} data-described={source.path.length ? "true" : undefined}
                    onClick={() => setPicked((current) => current.filter((id) => id !== source.dataSourceId))} data-my-task-source-picked={source.dataSourceId}>
                    <span className={styles.itemIcon}><MyTaskDatabaseIcon icon={source.icon} iconType={source.iconType} /></span>
                    <span className={styles.itemText}><span className={styles.itemName}>{name(source)}</span>
                      {source.path.length ? <span className={styles.itemPath}>{path(source)}</span> : null}</span>
                    <span className={styles.mark} aria-hidden="true"><MinusIcon size="small" /></span>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        </div>
        {helpOpen ? <p className={styles.help} role="note">{t("missingTasksHelp")}</p> : null}
        {failed ? <p className={styles.error} role="alert">{t("couldntSave")}</p> : null}
        <div className={styles.foot}>
          <button type="button" className={styles.helpLink} aria-expanded={helpOpen} onClick={() => setHelpOpen((open) => !open)}>{t("missingTasks")}</button>
          <button type="button" className={styles.done} disabled={busy} onClick={() => void save()} data-my-task-sources-done>{t("done")}</button>
        </div>
      </section>
    </div>
  );
  return createPortal(dialog, document.body);
}
