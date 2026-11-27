import { ArrowLeft, CheckIcon, FileText, X } from "@/icons/leafen";
import {
  csvChoiceType,
  csvColumnNames,
  csvFileTitle,
  defaultCSVNewDatabaseChoices,
  readCSVRecords,
  type CSVNewDatabaseChoice,
  type CSVPreviewColumn,
} from "@/lib/csvImportModel";
import {
  csvMergeColumns,
  csvMergePreviewColumns,
  defaultCSVMergeChoices,
  type CSVMergeChoice,
} from "@/lib/csvMergeImportModel";
import { clearRememberedDirectWorkspaceImport, rememberDirectWorkspaceImport } from "@/lib/directWorkspaceImportMemory";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { isKoreanLocale } from "@/lib/i18n";
import { newId } from "@/lib/ids";
import { pageHref } from "@/lib/navigation";
import { readBoundedMarkdownImportText, utf8ByteLength } from "@/lib/pageMarkdownImportModel";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { productErrorMessage, productErrorReceiptPresentation } from "@/lib/productErrorPresentation";
import { pushRoute } from "@/lib/router";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { api } from "../../api";
import type { Session } from "../../types";
import {
  workspaceImportBounds,
  workspaceImportFormats,
  workspaceImportLocales,
  workspaceImportStatuses,
  type WorkspaceCSVMergeTarget,
  type WorkspaceCSVMergeTargetSummary,
  type WorkspaceImportJob,
} from "../../workspaceImportWire.generated";
import { configForType } from "../database/propertyTypes";
import { useProductPages } from "../product/productPagesContext";
import { CsvImportColumnsStep } from "./CsvImportColumnsStep";
import { CsvDatabasePicker, CsvLocationPicker, importDatabaseLabel, type CsvImportPlace } from "./CsvImportPlacePicker";
import { CsvImportPreview } from "./CsvImportPreview";
import { CsvImportSourceStep, type CsvImportMethod } from "./CsvImportSourceStep";
import { useImportTeamspaces } from "./useImportTeamspaces";
import styles from "./CsvImportDialog.module.css";

/** The rows the preview shows; the import takes them all. */
const PREVIEW_ROWS = 50;

type ParsedCSV = { text: string; records: string[][]; width: number };

/**
 * Notion's "CSV를 Notion으로 가져오기" window (seen 2026-10-01), for "/csv",
 * a database page's "CSV와 병합" and a CSV chosen to import: a CSV makes a
 * new database where the reader puts it, or adds its rows to a database,
 * once each column's property type is set. A new database is named by the
 * file, ".csv" and all, and opens once made; rows added to a database end in
 * "가져오기를 완료했습니다", whose "가져오기로 이동하기" opens that database.
 */
export function CsvImportDialog({
  session, workspaceId, page, merge = false, mergeDataSourceId, methodChoice = false, initialFile = null, onImported, onNavigate, onClose,
}: {
  session: Session;
  workspaceId: string;
  /** The page a new database goes under at first, or, for "CSV와 병합", the database page rows join. */
  page: { pageId: string; title: string } | null;
  merge?: boolean;
  /** For an inline database's "CSV와 병합": the database rows join, among the page's. */
  mergeDataSourceId?: string;
  methodChoice?: boolean;
  initialFile?: File | null;
  onImported?: () => void;
  /** Leaves the screen the window opened over for what it made. */
  onNavigate?: (path: string) => void;
  onClose(): void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["importDialog", "moveToDialog", "common"]);
  const titleId = useId();
  const productPages = useProductPages();
  const pages = (productPages?.pages ?? []).filter((candidate) => candidate.workspaceId === workspaceId);
  const [step, setStep] = useState<"source" | "columns">("source");
  const [file, setFile] = useState<File | null>(null);
  const [csv, setCSV] = useState<ParsedCSV | null>(null);
  const [method, setMethod] = useState<CsvImportMethod>(merge ? "merge" : "new");
  const [place, setPlace] = useState<CsvImportPlace>(page && !merge ? { kind: "page", pageId: page.pageId } : { kind: "private" });
  // The words searched in "위치 가져오기", kept while its list is closed
  // (CsvLocationPicker); a teamspace made there is the place chosen.
  const [placeQuery, setPlaceQuery] = useState("");
  const teamspaces = useImportTeamspaces(session, workspaceId, (made) => { setPlace(made); setPlaceQuery(""); });
  // The list open over the window, and the field it opened from.
  const [picker, setPicker] = useState<{ kind: "location" | "database"; field: HTMLElement } | null>(null);
  const [sources, setSources] = useState<WorkspaceCSVMergeTargetSummary[] | null>(null);
  const [databaseQuery, setDatabaseQuery] = useState("");
  const [target, setTarget] = useState<WorkspaceCSVMergeTarget | null>(null);
  const [header, setHeader] = useState(true);
  const [newChoices, setNewChoices] = useState<CSVNewDatabaseChoice[]>([]);
  const [mergeChoices, setMergeChoices] = useState<CSVMergeChoice[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  /** The database page rows were added to, once they were. */
  const [merged, setMerged] = useState<string | null>(null);
  const requestId = useRef(newId());
  const controller = useRef<AbortController | null>(null);

  useEffect(() => () => controller.current?.abort(), []);
  useEffect(() => {
    if (initialFile) void chooseFile(initialFile);
    // The file chosen to open the window is read once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // A database page's "CSV와 병합" fills that page's own database, an inline
  // database's the database it was chosen from.
  useEffect(() => {
    if (!merge || !page) return;
    if (mergeDataSourceId) { void chooseDatabase(mergeDataSourceId); return; }
    const read = new AbortController();
    api.csvMergeTargets(session, workspaceId, { pageId: page.pageId, signal: read.signal })
      .then((response) => {
        if (response.items[0]) void chooseDatabase(response.items[0].dataSourceId);
      })
      .catch((caught: unknown) => {
        if (!read.signal.aborted) setError(productErrorMessage(caught, "workspaceImport.start"));
      });
    return () => read.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [merge, mergeDataSourceId, page?.pageId, session, workspaceId]);

  useEffect(() => {
    if (picker?.kind !== "database") return;
    const read = new AbortController();
    const timer = window.setTimeout(() => {
      api.csvMergeTargets(session, workspaceId, { query: databaseQuery.trim(), signal: read.signal })
        .then((response) => setSources(response.items))
        .catch((caught: unknown) => {
          if (!read.signal.aborted) setError(productErrorMessage(caught, "workspaceImport.start"));
        });
    }, 150);
    return () => {
      window.clearTimeout(timer);
      read.abort();
    };
  }, [databaseQuery, picker?.kind, session, workspaceId]);

  // A modal layer: Escape closes it once nothing opened over it (a column's
  // type menu, the place picker) takes the key.
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose });

  async function chooseFile(next: File | null) {
    requestId.current = newId();
    setFile(next);
    setCSV(null);
    setError("");
    setStep("source");
    if (!next) return;
    const read = await readBoundedMarkdownImportText(next, workspaceImportBounds.rawTextBytes);
    if (!read.ok) {
      setError(t("importDialog:job.fileTooLarge", { kind: "CSV" }));
      return;
    }
    const records = readCSVRecords(read.text);
    const width = Math.max(0, ...records.map((record) => record.length));
    // An empty file stays chosen and the window's button stays off, as Notion's (seen 2026-11-05).
    if (records.length === 0 || width === 0) return;
    setCSV({ text: read.text, records, width });
  }

  async function chooseDatabase(dataSourceId: string) {
    setPicker(null);
    setError("");
    try {
      setTarget(await api.csvMergeTarget(session, workspaceId, dataSourceId));
      requestId.current = newId();
    } catch (caught) {
      setError(productErrorMessage(caught, "workspaceImport.start"));
    }
  }

  const names = csv ? csvColumnNames(csv.records[0] ?? [], csv.width, {
    title: t("importDialog:csvWindow.nameColumn"),
    column: (index) => t("importDialog:csvMerge.untitledColumn", { index }),
  }) : [];
  const merging = method === "merge";

  function openColumns() {
    if (!csv) return;
    setNewChoices(defaultCSVNewDatabaseChoices(csv.records, csv.width));
    setMergeChoices(target ? defaultCSVMergeChoices(names, target.properties, csv.records) : []);
    setHeader(true);
    setStep("columns");
  }

  async function finish(job: WorkspaceImportJob, signal: AbortSignal) {
    const done = await waitForDurableJob({
      initial: job,
      isSameJob: (next) => next.id === job.id && next.workspaceId === workspaceId,
      isTerminal: (next) => next.status === workspaceImportStatuses.completed
        || next.status === workspaceImportStatuses.cancelled || next.status === workspaceImportStatuses.failed,
      onProgress() {},
      read: () => api.workspaceImportJob(session, workspaceId, job.id, { signal }),
      signal,
    });
    // This window says how it ended, so the import page doesn't say it again
    // as one that ended while away (2026-11-06: a CSV opened as its database
    // came back on the import page as "Import complete" under an empty title).
    clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
    if (done.status !== workspaceImportStatuses.completed) {
      throw new Error(done.errorCode
        ? productErrorReceiptPresentation({ code: done.errorCode, reference: done.diagnosticReference, status: 500 }, "workspaceImport.job").message
        : t("importDialog:csvWindow.failed"));
    }
    onImported?.();
    await productPages?.refreshPages?.();
    if (merging && target) {
      setMerged(target.pageId);
      return;
    }
    onClose();
    // A new database opens, as Notion's does once made.
    if (done.rootPageId) (onNavigate ?? pushRoute)(pageHref(done.rootPageId));
  }

  async function startImport() {
    if (!csv || busy) return;
    controller.current?.abort();
    const run = new AbortController();
    controller.current = run;
    setBusy(true);
    setError("");
    try {
      let job: WorkspaceImportJob;
      if (merging) {
        if (!target) return;
        const input = {
          requestId: requestId.current,
          csv: csv.text,
          ...csvMergeColumns(names, mergeChoices, {
            newId,
            columnName: (index) => t("importDialog:csvMerge.untitledColumn", { index: index + 1 }),
            configFor: (type) => ({ ...configForType(type, undefined, target.dataSourceId) }) as Record<string, unknown>,
          }),
          ...(header ? {} : { keepFirstRow: true }),
        };
        if (utf8ByteLength(JSON.stringify(input)) > workspaceImportBounds.requestBytes) {
          throw new Error(t("importDialog:job.requestTooLarge", { kind: "CSV" }));
        }
        job = await api.mergeCSVIntoDatabase(session, workspaceId, target.dataSourceId, input);
      } else {
        const input = {
          requestId: requestId.current,
          format: workspaceImportFormats.csv,
          locale: isKoreanLocale() ? workspaceImportLocales.ko : workspaceImportLocales.en,
          title: file?.name.trim() || t("common:labels.untitled"),
          markdown: "",
          csv: csv.text,
          csvColumnTypes: newChoices,
          ...(header ? {} : { csvKeepFirstRow: true }),
          ...(place.kind === "page" ? { parentPageId: place.pageId } : {}),
          ...(place.kind === "teamspace" ? { teamspaceId: place.teamspaceId } : {}),
        };
        if (utf8ByteLength(JSON.stringify(input)) > workspaceImportBounds.requestBytes) {
          throw new Error(t("importDialog:job.requestTooLarge", { kind: "CSV" }));
        }
        job = await api.createWorkspaceImport(session, workspaceId, input);
      }
      rememberDirectWorkspaceImport(session.user.id, workspaceId, job.id);
      await finish(job, run.signal);
    } catch (caught) {
      if (!run.signal.aborted) setError(productErrorMessage(caught, "workspaceImport.start"));
    } finally {
      if (controller.current === run) setBusy(false);
    }
  }

  const placePage = place.kind === "page" ? pages.find((candidate) => candidate.id === place.pageId) : undefined;
  // A page the list doesn't hold is a row's, a page; the list names the rest by its kind.
  const placeLabel = placePage ? pageNames.title(placePage) : place.kind === "page" && page
    ? page.title || untitledPageDisplayTitle("page")
    : place.kind === "teamspace" ? place.label : t("importDialog:csvWindow.privatePage");
  // The preview is named by the file, rows added to a database too.
  const previewTitle = file ? csvFileTitle(file.name) : "";
  // The preview leaves the first row out whatever the switch says, as Notion's does.
  const rows = csv ? csv.records.slice(1, 1 + PREVIEW_ROWS) : [];
  // A new database's preview shows its title first, the rest in the CSV's
  // order, as Notion's does (seen 2026-11-12).
  const newColumns: CSVPreviewColumn[] = newChoices.flatMap((choice, index) => (choice ? [{
    key: `column-${index}`, name: names[index] ?? "", type: csvChoiceType(choice).type, source: index,
    title: choice === "title",
  }] : []));
  const previewColumns: CSVPreviewColumn[] | undefined = step !== "columns" ? undefined : merging && target
    ? csvMergePreviewColumns(names, mergeChoices, target.properties)
    : [...newColumns.filter((column) => column.title), ...newColumns.filter((column) => !column.title)];

  return (
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("importDialog:close")} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} data-csv-dialog="true"
        data-done={merged ? "true" : undefined}>
        {step === "columns" && !merged ? (
          <button type="button" className={styles.close} aria-label={t("importDialog:wizard.back")} data-csv-back="true" onClick={() => setStep("source")}>
            <ArrowLeft size="small" aria-hidden="true" />
          </button>
        ) : (
          <button type="button" className={styles.close} aria-label={t("importDialog:close")} onClick={onClose}>
            <X size="small" aria-hidden="true" />
          </button>
        )}
        {merged ? (
          <div className={styles.done} data-csv-done="true">
            <div className={styles.doneMarks} aria-hidden="true">
              <span className={styles.doneTile}><FileText size="regular" /></span>
              <span className={styles.doneCheck}><CheckIcon size="small" /></span>
              <img src="/mark-128.png" alt="" width={48} height={48} />
            </div>
            <h2 id={titleId} className={styles.doneTitle}>{t("importDialog:csvWindow.doneTitle")}</h2>
            <p className={styles.doneHint}>{t("importDialog:csvWindow.doneHint")}</p>
            <button type="button" className={styles.doneOpen} data-csv-open-import="true" onClick={() => {
              onClose();
              (onNavigate ?? pushRoute)(pageHref(merged));
            }}>
              {t("importDialog:csvWindow.openImport")}
            </button>
          </div>
        ) : (
          <>
            <div className={styles.side} data-step={step}>
              {step === "source" ? (
                <CsvImportSourceStep
                  titleId={titleId}
                  file={file}
                  onFile={(next) => void chooseFile(next)}
                  methodChoice={methodChoice && !merge}
                  method={method}
                  onMethod={(next) => {
                    setMethod(next);
                    setError("");
                  }}
                  place={{ label: placeLabel, isPrivate: place.kind === "private", emoji: placePage?.iconType === "emoji" ? placePage.icon : null,
                    teamspace: place.kind === "teamspace" ? place : undefined, onOpen: (field) => setPicker({ kind: "location", field }) }}
                  database={{
                    label: target ? importDatabaseLabel(target.title) : "",
                    onOpen: merge ? undefined : (field) => setPicker({ kind: "database", field }),
                  }}
                  canMap={Boolean(csv) && (!merging || Boolean(target))}
                  onMap={openColumns}
                />
              ) : (
                <CsvImportColumnsStep
                  names={names}
                  header={header}
                  onHeader={setHeader}
                  columns={merging && target
                    ? { mode: "merge", target, choices: mergeChoices, onChoices: setMergeChoices }
                    : { mode: "new", choices: newChoices, onChoices: setNewChoices }}
                  busy={busy}
                  error={error}
                  onImport={() => void startImport()}
                  onCancel={onClose}
                />
              )}
              {step === "source" && error ? <p className={styles.error} role="alert">{error}</p> : null}
            </div>
            <CsvImportPreview title={previewTitle} columns={previewColumns} rows={rows} />
          </>
        )}
        {picker?.kind === "location" ? (
          <CsvLocationPicker pages={pages} reader={{ session, workspaceId }} teamspaces={teamspaces.teamspaces} current={place} field={picker.field}
            query={placeQuery} onQuery={setPlaceQuery} onClose={() => setPicker(null)} onChoose={(next) => {
            setPlace(next);
            setPicker(null);
          }} onNewTeamspace={teamspaces.onNewTeamspace ? () => { setPicker(null); teamspaces.onNewTeamspace?.(); } : undefined} />
        ) : picker?.kind === "database" ? (
          <CsvDatabasePicker sources={sources} query={databaseQuery} field={picker.field} onQuery={setDatabaseQuery}
            onClose={() => setPicker(null)} onChoose={(source) => void chooseDatabase(source.dataSourceId)} />
        ) : null}
        {teamspaces.window}
      </section>
    </div>
  );
}
