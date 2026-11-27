import { useId, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CloudUploadIcon, FileText, X } from "@/icons/leafen";
import { useFileDrop } from "@/lib/fileDropTarget";
import { FILE_IMPORT_SOURCE_MAX_BYTES } from "@/lib/fileImportSources";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePageNames } from "@/lib/pageNames";
import type { Session } from "../../types";
import { useProductPages } from "../product/productPagesContext";
import { CsvLocationPicker, type CsvImportPlace } from "./CsvImportPlacePicker";
import { ImportPlaceButton } from "./ImportPlaceButton";
import { useImportTeamspaces } from "./useImportTeamspaces";
import styles from "./ImportKindWindow.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";

/** The most files the window takes at once, as Notion's says. */
const IMPORT_KIND_WINDOW_MAX_FILES = 10;

export type ImportFileKind = { key: "pdf" | "text" | "html" | "word" | "zip"; icon: ReactNode; accept: string };

/**
 * A file kind's import window, as Notion's "파일 기반 가져오기" cards open it
 * (seen 2026-10-11): the kind's mark, "Leafen으로 〈kind〉 가져오기" and a line
 * on what it does, a dashed place to drop the files or choose them with the
 * size and count it takes, "위치 가져오기" for the page they go under, and
 * "계속하기" once files are chosen — into a database chosen there, each file
 * becomes its row. The chosen files take the dashed place's
 * ("선택된 파일"); files chosen in "콘텐츠 가져오기" open the window with them
 * (seen 2026-11-04).
 */
export function ImportKindWindow({ kind, initialFiles, session, workspaceId, initialPlace, onContinue, onClose }: {
  kind: ImportFileKind;
  initialFiles?: File[];
  session: Session;
  workspaceId: string;
  initialPlace?: { pageId: string; title: string } | null;
  onContinue(files: File[], place: CsvImportPlace): void;
  onClose(): void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["importDialog", "moveToDialog", "common"]);
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null), input = useRef<HTMLInputElement>(null);
  const productPages = useProductPages();
  const pages = (productPages?.pages ?? []).filter((candidate) => candidate.workspaceId === workspaceId);
  const [files, setFiles] = useState<File[]>(initialFiles ?? []);
  const [over, setOver] = useState(false);
  const [error, setError] = useState("");
  // The place field while its list is open.
  const [picking, setPicking] = useState<HTMLElement | null>(null);
  // The words searched there, kept while the list is closed (CsvLocationPicker);
  // a teamspace made there is the place chosen.
  const [placeQuery, setPlaceQuery] = useState("");
  const [place, setPlace] = useState<CsvImportPlace>(initialPlace ? { kind: "page", pageId: initialPlace.pageId } : { kind: "private" });
  const teamspaces = useImportTeamspaces(session, workspaceId, (made) => { setPlace(made); setPlaceQuery(""); });
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement],
    onEscape: onClose, restoreFocus: true });
  const name = t(`importDialog:kindWindow.names.${kind.key}`);
  // The window "콘텐츠 가져오기" opens for a ZIP or an EPUB is Notion's
  // general importer, which doesn't say how many files it takes (its upload
  // step shows that line for the other kinds only); it takes one file at a
  // time, "Choose file" and "Selected file" (seen 2026-11-19).
  const single = kind.key === "zip";
  const maxFiles = single ? 1 : IMPORT_KIND_WINDOW_MAX_FILES;
  const mebibytes = Math.round(FILE_IMPORT_SOURCE_MAX_BYTES / (1024 * 1024));
  const add = (list: FileList | File[]) => {
    const next = [...files, ...Array.from(list)];
    if (next.length > maxFiles) setError(t("importDialog:kindWindow.tooMany", { count: maxFiles }));
    else if (next.some((file) => file.size > FILE_IMPORT_SOURCE_MAX_BYTES)) setError(t("importDialog:kindWindow.tooLarge", { size: mebibytes }));
    else { setError(""); setFiles(next); }
  };
  const dropRef = useFileDrop(add, { onOver: setOver });
  const placePage = place.kind === "page" ? pages.find((candidate) => candidate.id === place.pageId) : undefined;
  // A page the list doesn't hold is a row's, a page; the list names the rest by its kind.
  const placeLabel = placePage ? pageNames.title(placePage) : place.kind === "page" && initialPlace
    ? initialPlace.title || untitledPageDisplayTitle("page")
    : place.kind === "teamspace" || place.kind === "database" ? place.label : t("importDialog:csvWindow.privatePage");
  return (
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("importDialog:close")} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}
        data-import-kind-window={kind.key}>
        <button type="button" className={styles.close} aria-label={t("importDialog:close")} onClick={onClose}>
          <X size="small" aria-hidden="true" />
        </button>
        <div className={styles.mark} aria-hidden="true">{kind.icon}</div>
        <h2 id={titleId}>{kind.key === "text" ? t("importDialog:kindWindow.titleText") : t("importDialog:kindWindow.title", { name })}</h2>
        <p className={styles.lead}>{t(`importDialog:kindWindow.leads.${kind.key}`)}</p>
        {files.length ? (
          <>
            <div className={styles.label}>{t(single ? "importDialog:kindWindow.chosenOne" : "importDialog:kindWindow.chosen")}</div>
            <ul className={styles.files} data-import-kind-files>
              {files.map((file, index) => (
                <li key={`${file.name}:${index}`}>
                  <FileText size="small" aria-hidden="true" />
                  <span>{file.name}</span>
                  <button type="button" aria-label={t("importDialog:kindWindow.remove", { name: file.name })}
                    onClick={() => setFiles((current) => current.filter((_, position) => position !== index))}>
                    <X size="tiny" aria-hidden="true" />
                  </button>
                </li>
              ))}
            </ul>
          </>
        ) : (
          <div ref={dropRef} className={styles.drop} data-over={over ? "true" : undefined} data-import-kind-drop>
            <CloudUploadIcon size="large" aria-hidden="true" />
            <strong>{kind.key === "text" ? t("importDialog:kindWindow.dropTitleText") : t("importDialog:kindWindow.title", { name })}</strong>
            <span>{t("importDialog:kindWindow.drop", { name })}{" "}
              <button type="button" className={styles.choose} onClick={() => input.current?.click()}>{t(single ? "importDialog:kindWindow.chooseOne" : "importDialog:kindWindow.choose")}</button></span>
            <div className={styles.limits}>
              <small>{t("importDialog:kindWindow.maxSize", { size: mebibytes })}</small>
              {single ? null : <small>{t("importDialog:kindWindow.maxFiles", { count: IMPORT_KIND_WINDOW_MAX_FILES })}</small>}
            </div>
            <input ref={input} type="file" accept={kind.accept} multiple={!single} hidden
              onChange={(event) => { if (event.currentTarget.files?.length) add(event.currentTarget.files); event.currentTarget.value = ""; }} />
          </div>
        )}
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
        <div className={styles.label}>{t("importDialog:csvWindow.location")}</div>
        <ImportPlaceButton data-import-kind-place label={placeLabel} isPrivate={place.kind === "private"} isDatabase={place.kind === "database"}
          emoji={placePage?.iconType === "emoji" ? placePage.icon : null} teamspace={place.kind === "teamspace" ? place : undefined}
          onClick={(event) => setPicking(event.currentTarget)} />
        <button type="button" className={styles.continue} disabled={files.length === 0} data-import-kind-continue
          onClick={() => onContinue(files, place)}>{t("importDialog:kindWindow.continue")}</button>
        {picking ? (
          <CsvLocationPicker pages={pages} reader={{ session, workspaceId }} teamspaces={teamspaces.teamspaces} databases={{ session, workspaceId }} current={place} field={picking}
            query={placeQuery} onQuery={setPlaceQuery} onClose={() => setPicking(null)}
            onChoose={(next) => { setPlace(next); setPicking(null); }}
            onNewTeamspace={teamspaces.onNewTeamspace ? () => { setPicking(null); teamspaces.onNewTeamspace?.(); } : undefined} />
        ) : null}
        {teamspaces.window}
      </section>
    </div>
  );
}
