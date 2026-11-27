import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { archiveConversionContext } from "@/lib/archiveImportPlan";
import {
  collectFileImportSources,
  convertFileImportSource,
  databaseRowShape,
  fileImportTitle,
  isDatabaseImportKind,
  type ArchiveLayout,
  type FileImportAttachment,
  type FileImportConversionContext,
  type FileImportSource,
} from "@/lib/fileImportSources";
import { sha256Hex } from "@/lib/browserCrypto";
import { workspaceImportAssetUploader } from "@/lib/importAssetUploads";
import { planImportBatch } from "@/lib/importBatchPlan";
import type { ImportReference } from "@/lib/importReferences";
import type { ZipFileEntry } from "@/lib/zipArchiveReader";
import { isKoreanLocale } from "@/lib/i18n";
import { utf8ByteLength } from "@/lib/pageMarkdownImportModel";
import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { activeDateLocale } from "@/lib/i18n";
import { newId } from "@/lib/ids";
import { finishImportBatch, importBatchKey, keepImportBatch, keepImportBatchJob } from "@/lib/importBatchMemory";
import { isServerUnreachable } from "@/lib/serverReach";
import type { Session } from "@/types";
import {
  workspaceImportBatchItemStates,
  workspaceImportBounds,
  workspaceImportLocales,
  workspaceImportPhases,
  workspaceImportStatuses,
  type WorkspaceImportBatchDocumentInput,
  type WorkspaceImportBatchItem,
  type WorkspaceImportJob,
} from "@/workspaceImportWire.generated";
import { fileImportFailure, fileImportNotes } from "./fileImportNotes";
import { ImportBatchProgress } from "./ImportBatchProgress";
import { ImportFailedWindow, ImportProgressWindow } from "./ImportFinishedWindow";
import styles from "./ImportDialog.module.css";

type RowStatus = "pending" | "converting" | "waiting" | "uploaded" | "failed" | "skipped" | "cancelled";

type Row = {
  key: string;
  name: string;
  status: RowStatus;
  message?: string;
  /** Every name a shortened message leaves out, shown on hover. */
  details?: string;
};

// Rows the server has not taken yet.
const UNSENT: ReadonlySet<RowStatus> = new Set<RowStatus>(["pending", "converting", "waiting"]);

const encoder = new TextEncoder();
// A row's changes reach the screen together: a ZIP's documents take
// milliseconds each, and redrawing its whole list for each would take longer.
const ROW_PAINT_MS = 120;

/** Waits before asking a server that didn't answer again: 2s, doubling to 30s, or at once when the browser is back online. */
function serverMayAnswer(attempt: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const done = () => {
      window.clearTimeout(timer);
      window.removeEventListener("online", done);
      signal.removeEventListener("abort", done);
      resolve();
    };
    const timer = window.setTimeout(done, Math.min(30_000, 2_000 * 2 ** attempt));
    window.addEventListener("online", done);
    signal.addEventListener("abort", done);
  });
}

/**
 * Imports several files, or the files inside a ZIP. Here the browser reads
 * and converts them, uploads the pictures and files they use, and sends the
 * text; once it is all sent the server goes on alone (ImportBatchProgress), so
 * a reload or a closed tab doesn't stop it, and the import window shows how
 * far it got when opened again. Each document becomes its own page (a CSV its
 * own database) and a ZIP's folders become pages, whose IDs the server gives
 * first so links between documents become page mentions.
 *
 * The same files to the same place are the same import: chosen again while
 * they are being sent, only what the server lacks is sent; chosen again after
 * it ended early, what came in stays and the rest goes in (2026-11-03 audit: a
 * server stopped mid-way showed every file "실패" though 229 pages were made,
 * and the same ZIP again made all 600 twice). A request the server didn't
 * answer waits for it.
 */
export function DirectFileBatchImport({
  files,
  parentPageId: destination = "",
  teamspaceId = "",
  dataSourceId = "",
  autoStart = false,
  onImported,
  onNavigate,
  onEnded,
  session,
  workspaceId,
}: {
  files: File[];
  /** The page the files go into; without it, the workspace's top level. */
  parentPageId?: string;
  /** The teamspace whose top the files go to, when no page is given. */
  teamspaceId?: string;
  /** The database whose rows the files become, when no page or teamspace is given. */
  dataSourceId?: string;
  /** Starts as soon as the files are read, for a kind window's "계속하기". */
  autoStart?: boolean;
  /** The import finished with at least one page. */
  onImported?: () => void;
  /** Leaves the import screen for what was made. */
  onNavigate?: (path: string) => void;
  /** The import ended, or was refused before it began, and its window closed: nothing of it shows any more. */
  onEnded?: () => void;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [sources, setSources] = useState<FileImportSource[]>([]);
  const [skipped, setSkipped] = useState<string[]>([]);
  const [attachments, setAttachments] = useState<FileImportAttachment[]>([]);
  const [archives, setArchives] = useState<Map<string, ZipFileEntry[]>>(() => new Map());
  const [layouts, setLayouts] = useState<Map<string, ArchiveLayout>>(() => new Map());
  const [rows, setRows] = useState<Row[]>([]);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const [started, setStarted] = useState(false);
  const [job, setJob] = useState<WorkspaceImportJob | null>(null);
  // Started from a kind's window it goes on in a window of its own until it
  // ends or the person goes back.
  const [ended, setEnded] = useState(false);
  const [wentBack, setWentBack] = useState(false);
  const batchRef = useRef("");
  const abortRef = useRef<AbortController | null>(null);
  const jobRef = useRef<string | null>(null);
  const changes = useRef(new Map<string, Partial<Row>>());
  const paint = useRef<number | null>(null);

  useEffect(() => {
    let current = true;
    setError("");
    setStarted(false);
    setJob(null);
    setEnded(false);
    setWentBack(false);
    void collectFileImportSources(files, activePersistentGeneratedLabels().untitled).then((collected) => {
      if (!current) return;
      setSources(collected.sources);
      setSkipped(collected.skipped);
      setAttachments(collected.attachments);
      setArchives(collected.archives);
      setLayouts(collected.layouts);
      setRows([
        ...collected.sources.map((source, index) => ({ key: `source:${index}`, name: source.name, status: "pending" as const })),
        ...collected.skipped.map((name, index) => ({ key: `skipped:${index}`, name, status: "skipped" as const })),
        ...collected.attachments.map((attachment, index) => ({ key: `attachment:${index}`, name: attachment.name, status: "pending" as const })),
      ]);
    }).catch(() => {
      if (current) setError(t("importDialog:batch.unreadable"));
    });
    return () => {
      current = false;
    };
  }, [files, t]);

  useEffect(() => () => {
    abortRef.current?.abort();
    if (paint.current !== null) window.clearTimeout(paint.current);
  }, []);
  useEffect(() => {
    if (autoStart && !started && !running && sources.length > 0) void run();
    // Once, when the files are read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoStart, sources]);

  function paintRows() {
    if (paint.current !== null) window.clearTimeout(paint.current);
    paint.current = null;
    if (changes.current.size === 0) return;
    const pending = changes.current;
    changes.current = new Map();
    setRows((current) => current.map((row) => {
      const change = pending.get(row.key);
      return change ? { ...row, ...change } : row;
    }));
  }

  function update(index: number, change: Partial<Row>, kind: "source" | "attachment" = "source") {
    const key = `${kind}:${index}`;
    changes.current.set(key, { ...changes.current.get(key), ...change });
    if (paint.current === null) paint.current = window.setTimeout(paintRows, ROW_PAINT_MS);
  }

  async function send(controller: AbortController, first: WorkspaceImportJob): Promise<WorkspaceImportJob> {
    // A request the server didn't answer waits for it and goes again.
    const answered = async <T,>(waiting: () => void, attempt: () => Promise<T>): Promise<T> => {
      for (let tries = 0; ; tries += 1) {
        try {
          return await attempt();
        } catch (caught) {
          if (controller.signal.aborted || !isServerUnreachable(caught)) throw caught;
          waiting();
          await serverMayAnswer(tries, controller.signal);
        }
      }
    };
    const waitingAll = () => sources.forEach((_, index) => update(index, { status: "waiting", message: undefined }));
    const signal = controller.signal;
    const today = new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date());
    const uploadAsset = workspaceImportAssetUploader(workspaceId);
    // A ZIP's attachments no document uses go up first, for its log to keep;
    // one that can't is left out as skipped.
    const kept: Array<{ name: string; fileId: string }> = [];
    const left: string[] = [];
    for (const [index, attachment] of attachments.entries()) {
      if (signal.aborted) break;
      const uploaded = await attachment.blob().then((blob) => uploadAsset(attachment.name.split("/").pop() ?? attachment.name, blob)).catch(() => null);
      if (uploaded && uploaded.kind !== "page") {
        kept.push({ name: attachment.name, fileId: uploaded.fileId });
        update(index, { status: "uploaded" }, "attachment");
      } else {
        left.push(attachment.name);
        update(index, { status: "failed" }, "attachment");
      }
    }
    const plan = await planImportBatch(sources, [...skipped, ...left], activePersistentGeneratedLabels().untitled,
      (name) => t("importDialog:batch.archiveRoot", { date: today, name: name.replace(/\.zip$/iu, "") }), layouts, kept);
    const items = new Map<number, WorkspaceImportBatchItem>();
    for (let at = 0; at < plan.items.length; at += workspaceImportBounds.batchItemsPerRequest) {
      const window = plan.items.slice(at, at + workspaceImportBounds.batchItemsPerRequest);
      const declared = await answered(waitingAll, () => api.declareWorkspaceImportBatchItems(session, workspaceId, first.id, { items: window }, signal));
      for (const item of declared.items) items.set(item.position, item);
    }
    const pageAt = (position: number | undefined) => (position === undefined ? undefined : items.get(position)?.pageId);
    const pagesOf = new Map<string, Map<string, string>>();
    for (const [key, documents] of plan.documents) {
      const pages = new Map<string, string>();
      for (const [path, position] of documents) {
        const pageId = pageAt(position);
        if (pageId) pages.set(path, pageId);
      }
      pagesOf.set(key, pages);
    }
    // A database's page is what the archive's documents mention by its path.
    sources.forEach((source, index) => {
      if (!source.archive || !isDatabaseImportKind(source.kind)) return;
      const only = sources.filter((other) => other.archive?.key === source.archive!.key && other.archive.path === source.archive!.path).length === 1;
      const pageId = pageAt(plan.places.get(index));
      if (only && pageId) pagesOf.get(source.archive.key)?.set(source.archive.path, pageId);
    });

    // An export's row page drops the property lines its database's CSV gives.
    const shapes = new Map<string, ReturnType<typeof databaseRowShape>>();
    const rowProperties = async (key: string, database: string, path: string) => {
      let shape = shapes.get(`${key}\0${database}`);
      if (!shape) {
        const source = sources.find((other) => other.archive?.key === key && other.archive.path === database && isDatabaseImportKind(other.kind));
        shape = source ? databaseRowShape(source) : Promise.resolve({ properties: [], titles: new Set<string>(), untitled: false });
        shapes.set(`${key}\0${database}`, shape);
      }
      const { properties, titles, untitled } = await shape;
      return titles.has(fileImportTitle(path, "", true)) || untitled ? properties : undefined;
    };
    const looseContext: FileImportConversionContext = { uploadAsset, references: async () => new Map() };
    const uploads = new Map<string, Map<string, Promise<ImportReference | null>>>();
    let window: Array<{ index: number; document: WorkspaceImportBatchDocumentInput }> = [];
    let windowBytes = 0;
    let latest = first;
    const flush = async () => {
      if (window.length === 0) return;
      const sent = window;
      window = [];
      windowBytes = 0;
      latest = await answered(
        () => sent.forEach(({ index }) => update(index, { status: "waiting", message: undefined })),
        () => api.sendWorkspaceImportBatchDocuments(session, workspaceId, first.id, { documents: sent.map(({ document }) => document) }, signal),
      );
      for (const { index, document } of sent) {
        update(index, document.failed ? { status: "failed", message: document.message } : { status: "uploaded", message: document.message, details: document.details });
      }
    };
    for (const [index, source] of sources.entries()) {
      if (signal.aborted) return latest;
      // Another tab may have finished sending the same files.
      if (latest.phase !== workspaceImportPhases.batchUpload) return latest;
      const position = plan.places.get(index)!;
      const item = items.get(position);
      if (!item) {
        const fresh = await api.workspaceImportJob(session, workspaceId, first.id, { signal });
        if (fresh.phase !== workspaceImportPhases.batchUpload) return fresh;
        throw new Error("The import's plan wasn't accepted.");
      }
      // Already here, or already through, from before.
      if (item.state !== workspaceImportBatchItemStates.pending) {
        update(index, item.state === workspaceImportBatchItemStates.failed
          ? { status: "failed", message: item.message }
          : { status: "uploaded", message: item.message, details: item.details });
        continue;
      }
      // A database's CSV in a ZIP names pages by their paths, as a document does.
      const archive = source.archive && (!isDatabaseImportKind(source.kind) || source.kind === "csv") ? source.archive : undefined;
      update(index, { status: "converting", message: undefined });
      let document: WorkspaceImportBatchDocumentInput;
      try {
        let archiveUploads = archive ? uploads.get(archive.key) : undefined;
        if (archive && !archiveUploads) uploads.set(archive.key, archiveUploads = new Map());
        const rowOf = archive ? plan.rows.get(archive.key)?.get(archive.path) : undefined;
        const rowNames = archive && rowOf ? await rowProperties(archive.key, rowOf, archive.path) : undefined;
        const converted = await answered(() => update(index, { status: "waiting", message: undefined }), () => convertFileImportSource(source, archive ? archiveConversionContext({
          documentPath: archive.path, pages: pagesOf.get(archive.key) ?? new Map(), entries: archives.get(archive.key) ?? [],
          uploads: archiveUploads!, uploadAsset, rowProperties: rowNames,
        }) : looseContext));
        document = utf8ByteLength(converted.content) > workspaceImportBounds.rawTextBytes
          ? { position, failed: true, message: t("importDialog:batch.tooLarge") }
          : { position, format: converted.format, content: converted.content, ...fileImportNotes(converted, t) };
      } catch (caught) {
        if (signal.aborted) return latest;
        document = { position, failed: true, message: fileImportFailure(caught, t) };
      }
      let bytes = utf8ByteLength(JSON.stringify(document));
      if (bytes > workspaceImportBounds.requestBytes) {
        document = { position, failed: true, message: t("importDialog:batch.tooLarge") };
        bytes = utf8ByteLength(JSON.stringify(document));
      }
      if (window.length > 0 && (windowBytes + bytes > workspaceImportBounds.batchUploadWindowBytes
        || window.length >= workspaceImportBounds.batchItemsPerRequest)) await flush();
      window.push({ index, document });
      windowBytes += bytes;
    }
    await flush();
    if (signal.aborted || latest.phase !== workspaceImportPhases.batchUpload) return latest;
    return answered(waitingAll, () => api.startWorkspaceImportBatch(session, workspaceId, first.id, signal));
  }

  async function run() {
    if (running || sources.length === 0) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setRunning(true);
    setStarted(true);
    setError("");
    try {
      const picked = await sha256Hex(encoder.encode(JSON.stringify([workspaceId, destination, teamspaceId, ...(dataSourceId ? [dataSourceId] : []),
        files.map((file) => [file.name, file.size, file.lastModified])])));
      const batch = importBatchKey(picked);
      batchRef.current = batch;
      const open = async () => {
        const kept = keepImportBatch(batch, {
          workspaceId, names: files.slice(0, 3).map((file) => file.name), fileCount: files.length, total: sources.length,
        }, newId);
        const requestId = `ib-${(await sha256Hex(encoder.encode(JSON.stringify([picked, kept])))).slice(0, 40)}`;
        for (let tries = 0; ; tries += 1) {
          try {
            return await api.createWorkspaceImportBatch(session, workspaceId, {
              requestId,
              title: files.slice(0, 3).map((file) => file.name).join(", ") + (files.length > 3 ? ", …" : ""),
              total: sources.length,
              locale: isKoreanLocale() ? workspaceImportLocales.ko : workspaceImportLocales.en,
              // Opened on a page, its one document fills that page while it is empty, as Notion's import does.
              ...(destination ? { parentPageId: destination, fillEmptyPage: true } : teamspaceId ? { teamspaceId } : dataSourceId ? { dataSourceId } : {}),
            }, controller.signal);
          } catch (caught) {
            if (controller.signal.aborted || !isServerUnreachable(caught)) throw caught;
            sources.forEach((_, index) => update(index, { status: "waiting", message: undefined }));
            await serverMayAnswer(tries, controller.signal);
          }
        }
      };
      let opened = await open();
      // All in before: chosen again, they are imported again, as in Notion.
      if (opened.status === workspaceImportStatuses.completed && opened.processedItems >= opened.totalItems) {
        finishImportBatch(batch);
        opened = await open();
      }
      keepImportBatchJob(batch, opened.id);
      jobRef.current = opened.id;
      const sent = opened.phase === workspaceImportPhases.batchUpload ? await send(controller, opened) : opened;
      if (controller.signal.aborted) return;
      paintRows();
      setJob(sent);
    } catch (caught) {
      if (controller.signal.aborted) return;
      paintRows();
      // Refused before the server took them, the files not yet through end
      // failed, the refusal said under them, not "변환 중" (2026-11-21: a
      // refusal for the server's disk left its row converting).
      setRows((current) => current.map((row) => UNSENT.has(row.status) ? { ...row, status: "failed", message: undefined } : row));
      setError(productErrorMessage(caught, "workspaceImport.start"));
    } finally {
      if (abortRef.current === controller) abortRef.current = null;
      setRunning(false);
    }
  }

  // Stops the files not yet sent; what the server has goes when it is told to stop.
  function cancel() {
    abortRef.current?.abort();
    const id = jobRef.current;
    if (id) void api.cancelWorkspaceImport(session, workspaceId, id).catch(() => undefined);
    changes.current.clear();
    setRows((current) => current.map((row) => ["pending", "converting", "waiting", "uploaded"].includes(row.status)
      ? { ...row, status: "cancelled", message: undefined } : row));
  }

  const progressWindow = autoStart && started && !ended && !wentBack && !error
    ? <ImportProgressWindow names={files.map((file) => file.name)} onBack={() => setWentBack(true)} /> : null;
  if (job) {
    return (
      <div data-file-batch-import>
        {progressWindow}
        <ImportBatchProgress
          initial={job}
          session={session}
          workspaceId={workspaceId}
          onOpen={onNavigate}
          onLeave={onEnded}
          onFinished={(finished) => {
            setEnded(true);
            if (finished.status === workspaceImportStatuses.completed && finished.processedItems >= finished.totalItems) finishImportBatch(batchRef.current);
            if (finished.processedItems > 0) onImported?.();
          }}
        />
      </div>
    );
  }
  return (
    <div className={styles.jobPanel} data-file-batch-import>
      {progressWindow}
      {autoStart && error && !wentBack ? <ImportFailedWindow names={files.map((file) => file.name)} message={error} onClose={() => { setWentBack(true); onEnded?.(); }} /> : null}
      <strong>{t("importDialog:batch.title", { count: sources.length })}</strong>
      <p>{t("importDialog:batch.lossNote")}</p>
      <ul className={styles.batchList}>
        {rows.map((row) => (
          <li key={row.key} data-batch-status={row.status} title={row.details}>
            <span>{row.name}</span>
            <span>
              {t(`importDialog:batch.${row.status}`)}
              {row.message ? ` · ${row.message}` : ""}
            </span>
          </li>
        ))}
      </ul>
      {error ? <p role="alert">{error}</p> : null}
      {!started ? (
        <button type="button" className={styles.primary} disabled={running || sources.length === 0} onClick={() => void run()}>
          {t("importDialog:batch.start")}
        </button>
      ) : running ? (
        <button type="button" className={styles.secondary} onClick={cancel}>
          {t("common:actions.cancel")}
        </button>
      ) : null}
    </div>
  );
}
