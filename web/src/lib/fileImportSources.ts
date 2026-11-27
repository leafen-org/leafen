import { readCSVRecords } from "./csvImportModel";
import { csvTitleAsPageName, withCsvPageMentions } from "./csvPageMentions";
import { readImportText } from "./importText";
import { exportedFileNames, namedWithExportId, titleFromFileName } from "./pageMarkdownImportModel";
import { FileImportTooLargeError } from "./importOutputBudget";
import { htmlReferenceHrefs, htmlToImportMarkdown } from "./importDocumentConverters";
import { withNotionDatabaseLinks } from "./htmlImportDatabases";
import { withExportedCommentsAsBlocks, withoutRowPropertyTable } from "./htmlExportExtras";
import { withCsvDataSourceMentions, withDataSourceMentions, withoutNestedDataSourceLinks, type ImportedDatabase } from "./importDataSourceMentions";
import { pageHeadHrefs, pageHeadLine } from "./htmlImportPageHead";
import { docxToImportMarkdown } from "./docxImportConverter";
import {
  isRelativeImportHref,
  markdownReferenceHrefs,
  resolveArchivePath,
  rewriteMarkdownReferences,
  type ImportAssetUploader,
  type ImportLeftOut,
  type ImportReferenceMap,
} from "./importReferences";
import { epubToImportMarkdown, opmlToImportMarkdown } from "./bookImport";
import { pdfToImportMarkdown } from "./pdfTextExtraction";
import { spreadsheetSheets, spreadsheetToCsv, type SpreadsheetKind } from "./spreadsheetImport";
import { readZipDirectory, type ZipFileEntry } from "./zipArchiveReader";

export type FileImportKind = "markdown" | "csv" | "html" | "docx" | "pdf" | SpreadsheetKind | "epub" | "opml";

/** The kinds that become databases; the others become pages. */
export function isDatabaseImportKind(kind: FileImportKind) {
  return kind === "csv" || kind === "tsv" || kind === "dsv" || kind === "xlsx" || kind === "xls" || kind === "ods";
}

/** A document larger than this is not read at all: Notion's largest file to import (50 MB on paid plans). */
export const FILE_IMPORT_SOURCE_MAX_BYTES = 50 * 1024 * 1024;
/** A ZIP holds at most what Notion's does (seen 2026-10-30: 5GB); its
 * entries are read one at a time. */
export const FILE_IMPORT_ZIP_MAX_BYTES = 5 * 1024 * 1024 * 1024;

export { FileImportTooLargeError };

/** One document to import as its own page (or database, for CSV). */
export type FileImportSource = {
  /** Where it came from: the file name, or its path inside a ZIP. */
  name: string;
  title: string;
  kind: FileImportKind;
  blob(): Promise<Blob>;
  /** For a document inside a ZIP: the archive and its path there. */
  archive?: { key: string; path: string };
  /** For a document inside a ZIP: the size it declares, checked before
   * it is inflated. */
  bytes?: number;
  /** For one sheet of a workbook: its position. */
  sheet?: number;
  /** For an export's database: the page its export draws it on (dropDatabaseDrawings). */
  drawing?: () => Promise<Blob>;
};

/** How a document's relative links and embedded files are resolved; files
 * that can't be brought in are added to `leftOut`. */
export type FileImportConversionContext = {
  references(hrefs: readonly string[], leftOut?: ImportLeftOut): Promise<ImportReferenceMap>;
  uploadAsset?: ImportAssetUploader;
  /** An exported page's title, whose heading the document begins with. */
  exportedTitle?: string;
  /** For an exported database's row page: its database's property names,
   * whose values the page lists under its title (withoutRowProperties). */
  rowProperties?: readonly string[];
  /** The address, from the document, of the database with this ID the
   * import brings, which Notion's HTML export names only by its ID
   * (withNotionDatabaseLinks). */
  databaseHref?(id: string): string | undefined;
  /** For an exported CSV: the page the import brings at a path its cell
   * names (withCsvPageMentions). */
  pageAt?(path: string): string | undefined;
  /** The database with a Notion ID the import brings, its data source's mentions its own (importDataSourceMentions.ts). */
  importedDatabase?: ImportedDatabase;
};

export type FileImportConversion = {
  format: "markdown" | "csv";
  content: string;
  /** Parts of the file left out (a PDF page past what is read of one). */
  omitted?: number;
  /** Files the document uses that weren't brought in, by name. */
  leftOut?: string[];
};

/**
 * The import kind of a file. As in Notion, spreadsheets other than CSV and
 * OPML outlines are read only inside a ZIP; an EPUB book is read alone too
 * (Notion's file imports name EPUB, seen 2026-10-30). A file chosen alone is
 * Markdown by the other names and type Notion's "콘텐츠 가져오기" takes as
 * Markdown too (.mdown, .mkd, text/x-markdown — seen 2026-11-19; a ZIP's
 * entries by them not measured).
 */
export function fileImportKind(name: string, type = "", inArchive = false): FileImportKind | null {
  const lower = name.toLowerCase();
  if (inArchive) {
    if (/\.tsv$/.test(lower)) return "tsv";
    if (/\.dsv$/.test(lower)) return "dsv";
    if (/\.xlsx$/.test(lower)) return "xlsx";
    if (/\.xls$/.test(lower)) return "xls";
    if (/\.ods$/.test(lower)) return "ods";
    if (/\.opml$/.test(lower)) return "opml";
  } else if (/\.(mdown|mkd)$/.test(lower) || type === "text/x-markdown") {
    return "markdown";
  }
  if (/\.epub$/.test(lower) || type === "application/epub+zip") return "epub";
  if (/\.(md|markdown|txt)$/.test(lower) || type === "text/markdown" || type === "text/plain") return "markdown";
  if (/\.csv$/.test(lower) || type === "text/csv" || type === "application/csv") return "csv";
  if (/\.html?$/.test(lower) || type === "text/html") return "html";
  if (/\.docx$/.test(lower)) return "docx";
  if (/\.pdf$/.test(lower) || type === "application/pdf") return "pdf";
  return null;
}

export function isZipImportFile(file: Pick<File, "name" | "type">) {
  return /\.zip$/i.test(file.name) || file.type === "application/zip" || file.type === "application/x-zip-compressed";
}

export function fileImportTitle(name: string, untitled: string, exported = false) {
  const base = name.split("/").pop() ?? name;
  return titleFromFileName(base, untitled, exported);
}

/**
 * An exported page's file begins with its title as a heading (Leafen's and
 * Notion's exports both): brought back, the title is the page's own and the
 * heading goes, its icon too. A title with page mentions is in the file's
 * name with each mention "@" and its name (Notion's, seen 2026-11-13); its
 * heading naming them stays for the server, which takes it as the title with
 * its mentions (workspace_import_field_pages.go), except a database row's,
 * whose title its database's CSV gives. A converted heading keeping its
 * title's edge spaces (Notion's HTML export keeps them, htmlImportSpacing.ts)
 * names the title without them, as its file does (2026-11-13: such a page's
 * title stayed as its first heading).
 */
function withoutTitleHeading(markdown: string, title: string, row = false) {
  const match = /^\s*#[ \t]+(.*)\n?/.exec(markdown);
  if (!match) return markdown;
  const words = match[1]!.trim().replace(/^\[\]\(leafen-spaces:\)/, "").replace(/&#(?:9|32);/g, " ");
  const heading = words.trim().replace(/^(?:\p{Extended_Pictographic}[\u{FE0F}\u{200D}\p{Extended_Pictographic}]*|\S{1,2}\u{FE0F})\s+/u, "");
  const mentioned = heading.replace(/\[((?:\\.|[^\]\\])*)\]\(leafen-(?:(?:sub)?page|(?:missing-)?data-source):[A-Za-z0-9_-]+\)/g, "@$1");
  const named = mentioned === heading ? heading : mentioned.replace(/\\(.)/g, "$1");
  // A converted document's heading is escaped to read as written (importMarkdownText.ts).
  const own = heading === title.trim() || heading.replace(/\\(.)/g, "$1") === title.trim() || row && named.trim() === title.trim();
  return own ? markdown.slice(match[0].length).replace(/^\s*\n/, "") : markdown;
}

/**
 * An exported row page lists its properties under its title, as Notion's
 * export and Leafen's write them ("Status: Done" lines in Markdown, a
 * properties table in HTML's header, seen 2026-11-07): brought back, they
 * are the row's, from its database's CSV, and the page's body is what follows.
 */
function withoutRowProperties(markdown: string, names: readonly string[]) {
  const lines = markdown.split(/\r?\n/);
  let at = 0;
  while (at < lines.length && names.some((name) => name !== "" && lines[at]!.startsWith(`${name}:`))) at += 1;
  if (at === 0) return markdown;
  while (at < lines.length && lines[at]!.trim() === "") at += 1;
  return lines.slice(at).join("\n");
}

/**
 * What an exported database's CSV says of its row pages: its property names,
 * its title's aside, and the titles of its rows. A page whose row the CSV has
 * by title, or an untitled one, is that row (workspace_import_csv.go), whose
 * properties the CSV gives; a page with no row keeps its property lines.
 */
export async function databaseRowShape(source: FileImportSource) {
  const none = { properties: [] as string[], titles: new Set<string>(), untitled: false };
  if (source.kind !== "csv") return none;
  try {
    const [header = [], ...rows] = readCSVRecords(await readImportText(await source.blob()));
    const titles = new Set(rows.map((row) => csvTitleAsPageName(row[0] ?? "")));
    return { properties: header.slice(1).map((name) => name.trim()).filter(Boolean), titles, untitled: titles.has("") };
  } catch {
    return none;
  }
}

/**
 * The paths in a ZIP its Markdown and HTML documents show or link to,
 * resolved as their conversion resolves them (archiveImportPlan.ts), so an
 * image a page shows isn't also listed as not imported (2026-11-06: a page's
 * picture was listed as "지원하지 않는 형식"); and the styles and scripts an
 * HTML document draws itself with, which are no attachment of the import
 * (htmlDrawingHrefs).
 */
async function archiveEntriesUsed(entries: readonly ZipFileEntry[]) {
  const used = new Set<string>();
  for (const entry of entries) {
    const kind = fileImportKind(entry.name, "", true);
    if ((kind !== "markdown" && kind !== "html") || entry.bytes > FILE_IMPORT_SOURCE_MAX_BYTES) continue;
    let text: string;
    try {
      text = await readImportText(await entry.blob());
    } catch {
      continue;
    }
    const slash = entry.name.lastIndexOf("/");
    const directory = slash >= 0 ? entry.name.slice(0, slash) : "";
    for (const href of kind === "markdown" ? markdownReferenceHrefs(text) : [...htmlReferenceHrefs(text), ...htmlDrawingHrefs(text)]) {
      const path = resolveArchivePath(directory, href);
      if (path) used.add(path);
    }
  }
  return used;
}

/**
 * The relative styles and scripts an HTML document draws itself with: an
 * HTML export's KaTeX, which draws its equations when the file is opened
 * (workspace_export_document_html_equations.go).
 */
function htmlDrawingHrefs(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  return Array.from(document.querySelectorAll("link[rel~=stylesheet][href], script[src]"))
    .map((element) => (element.getAttribute("src") ?? element.getAttribute("href") ?? "").trim())
    .filter(isRelativeImportHref);
}

/**
 * An archive's folders, each of which becomes a page, as Notion's ZIP import
 * makes one even for a folder holding no document or nothing (seen
 * 2026-11-07); and its one folder at the top, when everything sits in it,
 * which Notion makes the import's own page rather than a page under it.
 */
export type ArchiveLayout = { folders: string[]; root: string };

export function archiveLayout(files: readonly string[], named: readonly string[]): ArchiveLayout {
  const folders = new Set<string>();
  for (const path of [...files, ...named.map((folder) => `${folder}/`)]) {
    for (let slash = path.indexOf("/"); slash > 0; slash = path.indexOf("/", slash + 1)) folders.add(path.slice(0, slash));
  }
  const tops = new Set([...files, ...folders].map((path) => path.split("/")[0]));
  const [top] = tops;
  const root = tops.size === 1 && folders.has(top) ? top : "";
  return { folders: [...folders].sort(), root };
}

/**
 * The folder an export gives a page's or database's own files, by its file:
 * its title, or, for a second one of that title, its title and its ID's first
 * and last four characters ("Page <id>.md" beside "Page/" or "Page 4111-8d5d/",
 * as Notion's and Leafen's exports name them, seen 2026-11-07), when that
 * distinct folder is in the archive.
 */
export function exportFolderOf(path: string, folders: ReadonlySet<string>) {
  const file = path.slice(path.lastIndexOf("/") + 1);
  const title = titleFromFileName(file, "", true);
  const id = file.replace(/\.[^./]+$/, "").slice(title.length).trim().replaceAll("-", "");
  const slash = path.lastIndexOf("/");
  const at = (name: string) => slash < 0 ? name : `${path.slice(0, slash)}/${name}`;
  const distinct = id.length > 8 ? at(`${title} ${id.slice(0, 4)}-${id.slice(-4)}`) : "";
  return distinct && folders.has(distinct) ? distinct : at(title);
}

/**
 * Whether a ZIP's names are an export's, and its attachments if so. An
 * export names each page and database with its ID (exportedFileNames, its
 * database's "_all" CSV too) and keeps what a page has attached in the
 * page's own folder under the name it was attached with: such a file is an
 * attachment its page links to, not a page or database of its own
 * (2026-11-13: one attached text file made a whole export read as loose
 * files, each page split from its folder and its rows' pages made pages).
 * Null when the names are not an export's.
 */
export function exportAttachments(paths: readonly string[]): ReadonlySet<string> | null {
  const kinds = new Map<string, FileImportKind>();
  for (const path of paths) {
    const kind = fileImportKind(path, "", true);
    if (kind && kind !== "pdf") kinds.set(path, kind);
  }
  const named = new Set([...kinds.keys()].filter((path) => namedWithExportId(path.replace(/_all(\.csv)$/i, "$1"))));
  if (!exportedFileNames([...named].filter((path) => !isDatabaseImportKind(kinds.get(path)!)))) return null;
  const folders = new Set(archiveLayout(paths, []).folders);
  const owned = new Set([...named].map((path) => exportFolderOf(path, folders)));
  const attachments = new Set<string>();
  for (const [path, kind] of kinds) {
    if (named.has(path)) continue;
    const slash = path.lastIndexOf("/");
    if (slash > 0 && owned.has(path.slice(0, slash))) attachments.add(path);
    // A page named otherwise is not an export's; a database named otherwise
    // stays one, as before.
    else if (!isDatabaseImportKind(kind)) return null;
  }
  return attachments;
}

/** A ZIP's file no document shows or links to, which comes along as an
 * uploaded attachment, as Notion's ZIP import keeps it (seen 2026-11-07). */
export type FileImportAttachment = { name: string; blob: () => Promise<Blob> };

/**
 * The documents in the chosen files: each supported file, and each
 * supported file inside a ZIP. A ZIP's other files are its attachments;
 * anything else is listed as skipped. A ZIP
 * that can't be opened is one document whose reading fails, so it fails on
 * its own row with why and the other files still go (2026-11-07: one broken
 * ZIP left nothing to import and named no file).
 */
export async function collectFileImportSources(files: readonly File[], untitled: string) {
  const sources: FileImportSource[] = [];
  const skipped: string[] = [];
  const attachments: FileImportAttachment[] = [];
  const archives = new Map<string, ZipFileEntry[]>();
  const layouts = new Map<string, ArchiveLayout>();
  for (const [fileIndex, file] of files.entries()) {
    if (isZipImportFile(file)) {
      let directory: Awaited<ReturnType<typeof readZipDirectory>>;
      try {
        if (file.size > FILE_IMPORT_ZIP_MAX_BYTES) throw new FileImportTooLargeError("The ZIP file is too large to import.");
        directory = await readZipDirectory(file);
      } catch (error) {
        sources.push({ name: file.name, title: fileImportTitle(file.name, untitled), kind: "markdown", blob: () => Promise.reject(error) });
        continue;
      }
      const { entries, folders } = directory;
      const key = `${fileIndex}:${file.name}`;
      archives.set(key, entries);
      layouts.set(key, archiveLayout(entries.map((entry) => entry.name), folders));
      const notDocuments: ZipFileEntry[] = [];
      // An export's attachments are its pages' files, not documents (exportAttachments).
      const attached = exportAttachments(entries.map((entry) => entry.name));
      for (const entry of entries) {
        const kind = fileImportKind(entry.name, "", true);
        // As in Notion, a ZIP's PDFs are not imported; PDFs are chosen as files.
        if (!kind || kind === "pdf" || attached?.has(entry.name)) {
          notDocuments.push(entry);
          continue;
        }
        const source: FileImportSource = {
          name: `${file.name}/${entry.name}`,
          title: fileImportTitle(entry.name, untitled),
          kind,
          blob: () => entry.blob(),
          archive: { key, path: entry.name },
          bytes: entry.bytes,
        };
        // A workbook too large to import isn't opened; its one row says so.
        if ((kind !== "xlsx" && kind !== "xls" && kind !== "ods") || entry.bytes > FILE_IMPORT_SOURCE_MAX_BYTES) {
          sources.push(source);
          continue;
        }
        // Each sheet of a workbook becomes its own database.
        try {
          const sheets = await spreadsheetSheets(await source.blob(), kind);
          if (sheets.length === 0) skipped.push(source.name);
          for (const sheet of sheets) {
            sources.push({
              ...source,
              name: sheets.length > 1 ? `${source.name} · ${sheet.name}` : source.name,
              title: sheets.length > 1 ? `${source.title} - ${sheet.name}` : source.title,
              sheet: sheet.index,
            });
          }
        } catch {
          skipped.push(source.name);
        }
      }
      foldAllCsv(sources, key);
      // An export's database is named without its ID, as its pages are.
      if (attached) {
        dropDatabaseDrawings(sources, key);
        for (const source of sources.filter((source) => source.archive?.key === key)) {
          if (source.kind === "csv") source.title = fileImportTitle(source.archive!.path, untitled, true);
        }
      }
      // What the ZIP's documents show or link to goes into their pages; what
      // none of them uses comes along as an attachment of the import.
      const used = notDocuments.length > 0 ? await archiveEntriesUsed(entries) : new Set<string>();
      for (const entry of notDocuments) {
        if (!used.has(entry.name)) attachments.push({ name: `${file.name}/${entry.name}`, blob: () => entry.blob() });
      }
      continue;
    }
    const kind = fileImportKind(file.name, file.type);
    if (!kind) {
      skipped.push(file.name);
      continue;
    }
    sources.push({ name: file.name, title: fileImportTitle(file.name, untitled), kind, blob: async () => file });
  }
  return { sources, skipped, attachments, archives, layouts };
}

/**
 * An export's database comes as its view's CSV and, beside it, an "_all" CSV
 * with every property, those the view hides too (Notion's Markdown & CSV
 * export, and Leafen's, seen 2026-11-07). They are one database: it keeps
 * the view CSV's place and name, which the pages link to, and reads the
 * "_all" file, so no property is lost.
 */
function foldAllCsv(sources: FileImportSource[], key: string) {
  for (let index = sources.length - 1; index >= 0; index -= 1) {
    const all = sources[index]!;
    if (all.archive?.key !== key || all.kind !== "csv" || !/_all\.csv$/i.test(all.archive.path)) continue;
    const viewPath = all.archive.path.replace(/_all\.csv$/i, ".csv");
    const view = sources.find((source) => source.archive?.key === key && source.kind === "csv" && source.archive.path === viewPath);
    if (!view) continue;
    view.blob = all.blob;
    view.bytes = all.bytes;
    sources.splice(index, 1);
  }
}

/**
 * Notion's HTML export writes a full-page database both as its CSV and as a
 * page of the same name drawing its rows' table (seen 2026-11-13). Brought
 * back, the database is the CSV's, and the page drawing it is left out:
 * links to it lead to the database (archiveConversionContext); its header's
 * icon and cover are the database page's (convertBlob).
 */
function dropDatabaseDrawings(sources: FileImportSource[], key: string) {
  const databases = new Map(sources.filter((source) => source.archive?.key === key && source.kind === "csv").map((source) => [source.archive!.path, source]));
  for (let index = sources.length - 1; index >= 0; index -= 1) {
    const source = sources[index]!;
    const database = source.archive?.key === key && source.kind === "html" ? databases.get(source.archive.path.replace(/\.html?$/i, ".csv")) : undefined;
    if (!database) continue;
    database.drawing = source.blob;
    sources.splice(index, 1);
  }
}

/** Reads one document as the Markdown or CSV text the importer takes. */
export async function convertFileImportSource(
  source: FileImportSource,
  context?: FileImportConversionContext,
): Promise<FileImportConversion> {
  if ((source.bytes ?? 0) > FILE_IMPORT_SOURCE_MAX_BYTES) throw new FileImportTooLargeError("The file is too large to import.");
  const blob = await source.blob();
  if (blob.size > FILE_IMPORT_SOURCE_MAX_BYTES) throw new FileImportTooLargeError("The file is too large to import.");
  const leftOut: ImportLeftOut = new Set();
  const converted = await convertBlob(source, blob, leftOut, context);
  return leftOut.size ? { ...converted, leftOut: [...leftOut] } : converted;
}

async function convertBlob(
  source: FileImportSource,
  blob: Blob,
  leftOut: ImportLeftOut,
  context?: FileImportConversionContext,
): Promise<FileImportConversion> {
  switch (source.kind) {
    case "docx":
      return { format: "markdown", content: await docxToImportMarkdown(blob, context?.uploadAsset, leftOut) };
    case "pdf": {
      const { markdown, omitted } = await pdfToImportMarkdown(blob);
      return { format: "markdown", content: markdown, ...(omitted ? { omitted } : {}) };
    }
    case "html": {
      const text = await readImportText(blob);
      const own = withExportedCommentsAsBlocks(context?.rowProperties ? withoutRowPropertyTable(text) : text);
      const html = context?.databaseHref ? withNotionDatabaseLinks(own, context.databaseHref) : own;
      const references = context ? await context.references(htmlReferenceHrefs(html), leftOut) : undefined;
      const markdown = withDataSourceMentions(htmlToImportMarkdown(html, references), context?.importedDatabase);
      const body = context?.exportedTitle ? withoutTitleHeading(markdown, context.exportedTitle, Boolean(context.rowProperties)) : markdown;
      // An exported page's icon and cover open its Markdown as its own line.
      const head = pageHeadLine(html, references);
      return { format: "markdown", content: head ? `${head}\n\n${body}` : body };
    }
    case "csv": {
      const text = await readImportText(blob);
      const content = withCsvDataSourceMentions(context?.pageAt ? withCsvPageMentions(text, context.pageAt) : text, context?.importedDatabase);
      // An export's database page's icon and cover open its CSV as their own line (workspace_import_page_head_rows.go).
      const drawn = source.drawing && context ? await readImportText(await source.drawing()) : "";
      const head = drawn ? pageHeadLine(drawn, await context!.references(pageHeadHrefs(drawn), leftOut)) : "";
      return { format: "csv", content: head ? `${head}\n${content}` : content };
    }
    case "tsv":
    case "dsv":
    case "xlsx":
    case "xls":
    case "ods":
      return { format: "csv", content: await spreadsheetToCsv(blob, source.kind, source.sheet ?? 0) };
    case "epub":
      return { format: "markdown", content: await epubToImportMarkdown(blob, context?.uploadAsset, leftOut) };
    case "opml":
      return { format: "markdown", content: opmlToImportMarkdown(await readImportText(blob)) };
    default: {
      const text = await readImportText(blob);
      if (!context) return { format: "markdown", content: text };
      const titled = context.exportedTitle ? withoutTitleHeading(text, context.exportedTitle, Boolean(context.rowProperties)) : text;
      const markdown = withoutNestedDataSourceLinks(context.rowProperties ? withoutRowProperties(titled, context.rowProperties) : titled);
      const referenced = rewriteMarkdownReferences(markdown, await context.references(markdownReferenceHrefs(markdown), leftOut));
      return { format: "markdown", content: withDataSourceMentions(referenced, context.importedDatabase) };
    }
  }
}
