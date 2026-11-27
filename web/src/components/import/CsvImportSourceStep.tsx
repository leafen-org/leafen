import { AddToPageIcon, ChevronUp, DatabaseCylinderIcon, FileText, PaperclipIcon, SaveForEveryoneIcon, X } from "@/icons/leafen";
import { useRef } from "react";
import { useTranslation } from "react-i18next";

import styles from "./CsvImportDialog.module.css";
import { ImportPlaceButton } from "./ImportPlaceButton";

export type CsvImportMethod = "new" | "merge";

/**
 * The CSV window's first step, as Notion's (seen 2026-10-01): the file, how
 * it imports when the window was opened to choose, where its database goes or
 * the database its rows join, then "CSV 헤더 매핑".
 */
export function CsvImportSourceStep({
  titleId, file, onFile, methodChoice, method, onMethod, place, database, canMap, onMap,
}: {
  titleId: string;
  file: File | null;
  onFile(file: File | null): void;
  /** Whether the window offers "가져오기 방법", as the "/csv" command opens it. */
  methodChoice: boolean;
  method: CsvImportMethod;
  onMethod(method: CsvImportMethod): void;
  /** Where a new database goes, named and marked as the other import windows mark it, and how to change it. */
  place: { label: string; isPrivate?: boolean; emoji?: string | null; teamspace?: { label: string; icon?: string }; onOpen(field: HTMLElement): void };
  /** The database rows join, named once chosen, and how to choose it unless it is fixed. */
  database: { label: string; onOpen?: (field: HTMLElement) => void };
  canMap: boolean;
  onMap(): void;
}) {
  const { t } = useTranslation(["importDialog"]);
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      {/* Its fields scroll in a short window, its button staying at the foot (Notion's, seen 2026-11-12). */}
      <div className={styles.sourceScroll}>
        <h2 id={titleId} className={styles.title}>{t("importDialog:csvWindow.title")}</h2>
        <p className={styles.hint}>{t("importDialog:csvWindow.hint")}</p>
        <div className={styles.label} data-strong="true">{t("importDialog:csvWindow.upload")}</div>
        <input ref={input} type="file" accept=".csv,text/csv" hidden data-csv-file-input="true"
          onChange={(event) => {
            onFile(event.currentTarget.files?.[0] ?? null);
            event.currentTarget.value = "";
          }} />
        {file ? (
          <div className={styles.file} data-csv-file={file.name}>
            <FileText size="regular" aria-hidden="true" />
            <span title={file.name}>{file.name}</span>
            <button type="button" aria-label={t("importDialog:csvWindow.removeFile")} onClick={() => onFile(null)}>
              <X size="regular" aria-hidden="true" />
            </button>
          </div>
        ) : (
          <button type="button" className={styles.fileButton} onClick={() => input.current?.click()}>
            <PaperclipIcon size="small" aria-hidden="true" />
            {t("importDialog:csvWindow.chooseFile")}
          </button>
        )}
        {methodChoice ? (
          <>
            <div className={styles.label}>{t("importDialog:csvMerge.method")}</div>
            <div role="radiogroup" aria-label={t("importDialog:csvMerge.method")}>
              {(["new", "merge"] as const).map((option) => (
                <button key={option} type="button" role="radio" aria-checked={method === option} className={styles.method}
                  data-csv-method={option} onClick={() => onMethod(option)}>
                  {option === "new" ? <AddToPageIcon size="regular" aria-hidden="true" /> : <SaveForEveryoneIcon size="regular" aria-hidden="true" />}
                  <span className={styles.methodText}>
                    <strong>{t(option === "new" ? "importDialog:csvMerge.newDatabase" : "importDialog:csvMerge.existingDatabase")}</strong>
                    <small>{t(option === "new" ? "importDialog:csvMerge.newDatabaseHint" : "importDialog:csvMerge.existingDatabaseHint")}</small>
                  </span>
                  {/* Its choice shows as Notion's radio at the card's end (seen 2026-11-09). */}
                  <span className={styles.methodRadio} aria-hidden="true" />
                </button>
              ))}
            </div>
          </>
        ) : null}
        <div className={styles.label}>{t("importDialog:csvWindow.location")}</div>
        {method === "new" ? (
          <ImportPlaceButton data-csv-place="true" label={place.label} isPrivate={place.isPrivate} emoji={place.emoji} teamspace={place.teamspace} onClick={(event) => place.onOpen(event.currentTarget)} />
        ) : (
          <button type="button" className={styles.place} data-database="true" data-empty={database.label ? undefined : "true"}
            aria-haspopup={database.onOpen ? "dialog" : undefined} disabled={!database.onOpen}
            data-csv-database="true" onClick={(event) => database.onOpen?.(event.currentTarget)}>
            {database.label ? <DatabaseCylinderIcon size="regular" aria-hidden="true" /> : null}
            <span>{database.label || t("importDialog:csvWindow.chooseDatabase")}</span>
            {database.onOpen ? <ChevronUp size="small" aria-hidden="true" /> : null}
          </button>
        )}
      </div>
      <div className={styles.foot}>
        <button type="button" className={styles.primary} disabled={!canMap} data-csv-map="true" onClick={onMap}>
          {t("importDialog:csvWindow.mapHeaders")}
        </button>
        <div className={styles.footSpace} aria-hidden="true" />
      </div>
    </>
  );
}
