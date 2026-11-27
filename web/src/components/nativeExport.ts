import { i18next } from "@/i18n";
import { leafenArchiveFileName, safeLeafenFileStem } from "@/lib/leafenExportDownload";
import {
  saveStreamedResponse,
  type StreamedResponseFileType,
} from "@/lib/nativeArchive";
import { pageMarkdownFileName } from "@/lib/pageMarkdownExportModel";
import {
  printExportDocument,
  readPrintableExport,
  type PrintPaperFormat,
} from "@/lib/printExportDocument";
import { workspaceExportService } from "@/lib/workspaceExportService";
import type { Page, Session } from "@/types";
import type { PageExportChoice } from "./PageExportDialog";
import {
  workspaceExportFormats,
  workspaceExportScopeKinds,
  type WorkspaceExportFormat,
  type WorkspaceExportJob,
  type WorkspaceExportScope,
} from "@/workspaceExportWire.generated";

type NativePageExportContext = {
  session: Session;
  signal?: AbortSignal;
};

type NativePageExportTarget = Pick<Page, "id" | "title" | "workspaceId">;

/**
 * The name an export's file takes from what it holds: its title, or with
 * none "제목 없음", whatever it is, as Notion's export names an untitled page
 * or database (seen 2026-11-14: "제목 없음 <id>.csv" for a database named
 * "새 데이터베이스" everywhere else) and as the export's own files are named
 * (workspace_export_document_paths.go).
 */
function exportFileTitle(target: NativePageExportTarget) {
  return target.title.trim() ? target.title : String(i18next.t("common:labels.untitled"));
}

type NativePageExportService = {
  start(
    session: Session,
    input: {
      workspaceId: string;
      format: WorkspaceExportFormat;
      scope: WorkspaceExportScope;
      options: Record<string, boolean>;
    },
  ): Promise<{ job: WorkspaceExportJob }>;
  wait(
    session: Session,
    workspaceId: string,
    job: WorkspaceExportJob,
    options: { signal: AbortSignal },
  ): Promise<WorkspaceExportJob>;
  download(
    session: Session,
    workspaceId: string,
    jobId: string,
    options: { signal: AbortSignal },
  ): Promise<Response>;
};

type NativePageExportDependencies = {
  service: NativePageExportService;
  saveStreamedResponse: typeof saveStreamedResponse;
  printExportDocument?: typeof printExportDocument;
};

type ExportDescriptor = {
  fileName: (target: NativePageExportTarget) => string;
  fileType: StreamedResponseFileType;
  format: WorkspaceExportFormat;
  options?: Record<string, boolean>;
  scopeKind: typeof workspaceExportScopeKinds.page
    | typeof workspaceExportScopeKinds.database;
};

// The save picker names each type in the screen's language, so each is read when a file is saved.
const markdownFileType = (): StreamedResponseFileType => ({
  description: i18next.t("common:fileTypes.markdown"),
  extensions: [".md"],
  mimeType: "text/markdown",
});

const archiveFileType = (): StreamedResponseFileType => ({
  description: i18next.t("common:fileTypes.leafenArchive"),
  extensions: [".leafen.zip"],
  mimeType: "application/vnd.leafen.archive+zip",
});

const csvFileType = (): StreamedResponseFileType => ({
  description: i18next.t("common:fileTypes.csv"),
  extensions: [".csv"],
  mimeType: "text/csv",
});

const zipFileType = (): StreamedResponseFileType => ({
  description: i18next.t("common:fileTypes.zip"),
  extensions: [".zip"],
  mimeType: "application/zip",
});

/** The page export dialog's readable formats and their choices. */
export type PageDocumentExportInput = {
  format: "html" | "markdown_csv";
  includeFiles: boolean;
  includeSubpages: boolean;
  createFolders: boolean;
  includeComments: boolean;
  defaultDatabaseViews: boolean;
};

export type PagePrintExportInput = {
  includeFiles: boolean;
  includeSubpages: boolean;
  defaultDatabaseViews: boolean;
  paper: PrintPaperFormat;
  scale: number;
};

function completedExportError(job: WorkspaceExportJob) {
  const message = job.errorCode || "LEAFEN_WORKSPACE_EXPORT_NOT_COMPLETED";
  return Object.assign(new Error(message), {
    code: job.errorCode,
    diagnosticReference: job.diagnosticReference,
  });
}

export function createNativePageExportAdapter(
  dependencies: NativePageExportDependencies = {
    saveStreamedResponse,
    service: workspaceExportService,
  },
) {
  async function runScoped(
    target: NativePageExportTarget,
    context: NativePageExportContext,
    descriptor: Omit<ExportDescriptor, "fileName" | "fileType">,
  ) {
    const signal = context.signal ?? new AbortController().signal;
    signal.throwIfAborted();
    const started = await dependencies.service.start(context.session, {
      workspaceId: target.workspaceId,
      format: descriptor.format,
      scope: { kind: descriptor.scopeKind, targetId: target.id },
      options: descriptor.options ?? { includeFiles: true },
    });
    signal.throwIfAborted();
    const completed = await dependencies.service.wait(
      context.session,
      target.workspaceId,
      started.job,
      { signal },
    );
    signal.throwIfAborted();
    if (completed.status !== "completed") throw completedExportError(completed);
    const response = await dependencies.service.download(
      context.session,
      target.workspaceId,
      completed.id,
      { signal },
    );
    signal.throwIfAborted();
    return { completed, response, signal };
  }

  async function exportScoped(
    target: NativePageExportTarget,
    context: NativePageExportContext,
    descriptor: ExportDescriptor,
  ) {
    const { completed, response, signal } = await runScoped(target, context, descriptor);
    const saved = await dependencies.saveStreamedResponse(
      response,
      descriptor.fileName(target),
      descriptor.fileType,
    );
    signal.throwIfAborted();
    // A save window closed without saving ends the export as a cancel does,
    // saying nothing was downloaded.
    if (saved === "cancelled") throw new DOMException("The save was canceled.", "AbortError");
    return completed;
  }

  function exportDocument(
    target: NativePageExportTarget,
    input: PageDocumentExportInput,
    context: NativePageExportContext,
    scopeKind: ExportDescriptor["scopeKind"],
  ) {
    const suffix = input.format === "html" ? "-html.zip" : "-markdown-csv.zip";
    return exportScoped(target, context, {
      fileName: (named) => `${safeLeafenFileStem(exportFileTitle(named))}${suffix}`,
      fileType: zipFileType(),
      format: input.format === "html" ? workspaceExportFormats.html : workspaceExportFormats.markdownCsv,
      options: {
        includeFiles: input.includeFiles,
        includeSubpages: input.includeSubpages,
        createFolders: input.createFolders,
        defaultDatabaseViews: input.defaultDatabaseViews,
        ...(input.format === "html" ? { includeComments: input.includeComments } : {}),
      },
      scopeKind,
    });
  }

  return {
    /** HTML or Markdown & CSV: a ZIP of the page, its subpages, databases and files. */
    exportPageDocument(page: NativePageExportTarget, input: PageDocumentExportInput, context: NativePageExportContext) {
      return exportDocument(page, input, context, workspaceExportScopeKinds.page);
    },

    /**
     * A database's HTML or Markdown & CSV, as Notion exports a database (seen
     * 2026-11-07): its CSV (and its "_all" CSV), or its table's HTML and CSV,
     * its row pages with subpages. The target is its data source.
     */
    exportDatabaseDocument(database: NativePageExportTarget, input: PageDocumentExportInput, context: NativePageExportContext) {
      return exportDocument(database, input, context, workspaceExportScopeKinds.database);
    },

    /** PDF: prints the page's printable export, or a database's, through the browser. */
    async printPage(
      page: NativePageExportTarget,
      input: PagePrintExportInput,
      context: NativePageExportContext,
      scopeKind: typeof workspaceExportScopeKinds.page | typeof workspaceExportScopeKinds.database = workspaceExportScopeKinds.page,
    ) {
      const { completed, response, signal } = await runScoped(page, context, {
        format: workspaceExportFormats.printHtml,
        options: {
          includeFiles: input.includeFiles,
          includeSubpages: input.includeSubpages,
          defaultDatabaseViews: input.defaultDatabaseViews,
        },
        scopeKind,
      });
      const html = await readPrintableExport(response);
      signal.throwIfAborted();
      await (dependencies.printExportDocument ?? printExportDocument)(html, {
        paper: input.paper, scale: input.scale, signal,
      });
      return completed;
    },

    exportPageAsMarkdown(page: NativePageExportTarget, context: NativePageExportContext) {
      return exportScoped(page, context, {
        fileName: (target) => pageMarkdownFileName(exportFileTitle(target), String(i18next.t("common:labels.untitled"))),
        fileType: markdownFileType(),
        format: workspaceExportFormats.pageMarkdown,
        scopeKind: workspaceExportScopeKinds.page,
      });
    },

    /** The page as Markdown text, for "페이지 내용 복사하기", rather than as a file. */
    async readPageMarkdown(page: NativePageExportTarget, context: NativePageExportContext) {
      const { response, signal } = await runScoped(page, context, {
        format: workspaceExportFormats.pageMarkdown,
        scopeKind: workspaceExportScopeKinds.page,
      });
      const text = await response.text();
      signal.throwIfAborted();
      return text;
    },

    exportPageAsNotionMarkdown(page: NativePageExportTarget, context: NativePageExportContext) {
      return exportScoped(page, context, {
        fileName: (target) => `${safeLeafenFileStem(exportFileTitle(target))}-notion.md`,
        fileType: markdownFileType(),
        format: workspaceExportFormats.notionMarkdown,
        scopeKind: workspaceExportScopeKinds.page,
      });
    },

    exportPageAsNativeArchive(page: NativePageExportTarget, context: NativePageExportContext) {
      return exportScoped(page, context, {
        fileName: (target) => leafenArchiveFileName(exportFileTitle(target)),
        fileType: archiveFileType(),
        format: workspaceExportFormats.pageNativeArchive,
        scopeKind: workspaceExportScopeKinds.page,
      });
    },

    /** A database's Leafen archive: the database alone, which comes back as a full-page database. */
    exportDatabaseAsNativeArchive(database: NativePageExportTarget, context: NativePageExportContext) {
      return exportScoped(database, context, {
        fileName: (target) => leafenArchiveFileName(exportFileTitle(target)),
        fileType: archiveFileType(),
        format: workspaceExportFormats.pageNativeArchive,
        scopeKind: workspaceExportScopeKinds.database,
      });
    },

    exportDatabaseAsCsv(database: NativePageExportTarget, context: NativePageExportContext) {
      return exportScoped(database, context, {
        fileName: (target) => `${safeLeafenFileStem(exportFileTitle(target))}.csv`,
        fileType: csvFileType(),
        format: workspaceExportFormats.databaseCsv,
        scopeKind: workspaceExportScopeKinds.database,
      });
    },
  };
}

/** The database a page's screen shows, and whether it goes out as that database in every format: an inline one opened on its own, or a linked one. */
export type MenuExportDatabase = NativePageExportTarget & { inline: boolean };

/**
 * Exports what the page menu's screen shows, in the format chosen: an inline
 * database opened on its own is its database in every format, and so is a
 * linked database, opened alone or as a full page, whose database is its
 * source, named after it (Notion's Markdown & CSV of one holds its rows under
 * its own name and its source's "_all" where the source stands, not the page
 * holding it, seen 2026-11-11); a database
 * page's HTML and Markdown & CSV are its database's, as Notion's (seen
 * 2026-11-07), and a full-page database's PDF and archive its page's, which
 * holds only the database.
 */
export async function exportMenuChoice(
  page: NativePageExportTarget, shown: MenuExportDatabase | null, choice: PageExportChoice, context: NativePageExportContext,
) {
  const database = shown && (shown.inline || (choice.format !== "pdf" && choice.format !== "leafen_archive"))
    ? { id: shown.id, title: shown.title, workspaceId: shown.workspaceId } : null;
  if (choice.format === "leafen_archive") {
    if (database) await nativePageExportAdapter.exportDatabaseAsNativeArchive(database, context);
    else await nativePageExportAdapter.exportPageAsNativeArchive(page, context);
  } else if (choice.format === "pdf") {
    await nativePageExportAdapter.printPage(database ?? page, choice, context,
      database ? workspaceExportScopeKinds.database : workspaceExportScopeKinds.page);
  } else if (database) {
    await nativePageExportAdapter.exportDatabaseDocument(database, { ...choice, format: choice.format }, context);
  } else await nativePageExportAdapter.exportPageDocument(page, { ...choice, format: choice.format }, context);
}

const nativePageExportAdapter = createNativePageExportAdapter();

export const exportPageDocument = nativePageExportAdapter.exportPageDocument;
export const printPage = nativePageExportAdapter.printPage;
export const readPageMarkdown = nativePageExportAdapter.readPageMarkdown;
