import {
  archiveLayout,
  exportAttachments,
  exportFolderOf,
  fileImportTitle,
  type ArchiveLayout,
  type FileImportConversionContext,
} from "./fileImportSources";
import {
  isRelativeImportHref,
  resolveArchivePath,
  type ImportAssetUploader,
  type ImportReference,
} from "./importReferences";
import { exportedFileNames } from "./pageMarkdownImportModel";
import type { ZipFileEntry } from "./zipArchiveReader";

/**
 * A ZIP keeps its folder structure as pages, as Notion's ZIP import does:
 * each folder becomes a page holding the documents and folders inside it,
 * even one holding no document or nothing (seen 2026-11-07), and a document
 * named like its folder ("Page.md" beside "Page/", or in an export
 * "Page <id>.md" beside "Page/") is that folder's page. In an export a
 * database's folder holds its rows' pages ("Tasks <id>.csv" beside "Tasks/",
 * or "Tasks <id>/" in older ones, Notion's and Leafen's alike, seen
 * 2026-11-07): brought back, they are its rows, not pages of a folder. The one
 * folder at the top that holds everything is the import's own page, not a
 * page under it (seen 2026-11-07). Pages are created before the documents
 * are imported, so links between documents become page mentions and the
 * archive's other files are uploaded once when a document uses them.
 */

/** Makes one page of the archive: `place` is the document's or folder's path it stands for. */
export type ArchivePageCreator = (parentId: string, title: string, place: string) => Promise<string>;

const DOCUMENT_EXTENSION = /\.(md|markdown|txt|html?|docx)$/i;
const EXTENSION = /\.[^./]+$/;

function directoryOf(path: string) {
  const index = path.lastIndexOf("/");
  return index < 0 ? "" : path.slice(0, index);
}

/** A page's own come folders first, then files, each by name, as Notion's (seen 2026-11-05). */
function archiveOrder(left: string, right: string) {
  const leftParts = left.split("/"), rightParts = right.split("/");
  for (let depth = 0; ; depth += 1) {
    const leftFolder = depth < leftParts.length - 1, rightFolder = depth < rightParts.length - 1;
    if (leftFolder !== rightFolder) return leftFolder ? -1 : 1;
    const order = leftParts[depth].localeCompare(rightParts[depth]);
    if (order || !leftFolder) return order;
  }
}

/**
 * Creates the pages of an archive's documents and folders in the order a page
 * shows them, parents first, including the folders of its databases, which are
 * imported under their folder's page, each given to `database` in its place
 * and its rows' pages right after it, under it.
 * Returns what `create` and `database` gave for each, by document path and by
 * folder path, and the database each row page is of.
 */
export async function createArchivePages(
  documentPaths: readonly string[],
  create: ArchivePageCreator,
  untitled: string,
  databasePaths: readonly string[] = [],
  // The archive's folders and its one top folder: from all its entries when
  // given (collectFileImportSources), else from these paths.
  layout: ArchiveLayout = archiveLayout([...documentPaths, ...databasePaths], []),
  database?: (path: string, parentId: string) => string,
) {
  const documents = new Set(documentPaths);
  const exported = exportedFileNames(documentPaths);
  const documentPages = new Map<string, Promise<string>>();
  const folderPages = new Map<string, Promise<string>>();
  const databasePages = new Map<string, Promise<string>>();
  // The top folder holding everything stands for the import's own page ("").
  if (layout.root) folderPages.set(layout.root, Promise.resolve(""));

  // An export's folder may be named by its page's title (exportFolderOf).
  const allFolders = new Set(layout.folders);
  const titledLike = (path: string, folder: string) => exported && exportFolderOf(path, allFolders) === folder;
  const inOrder = [...documentPaths].sort(archiveOrder);
  const namedPage = (folder: string) => documentPaths.find((path) => path.replace(DOCUMENT_EXTENSION, "") === folder && documents.has(path))
    ?? inOrder.find((path) => DOCUMENT_EXTENSION.test(path) && titledLike(path, folder));
  // An export's database owns its rows' folder, unless a page does.
  const rowFolders = new Map<string, string>();
  if (exported) {
    for (const folder of layout.folders) {
      const owner = databasePaths.find((path) => path.replace(EXTENSION, "") === folder || titledLike(path, folder));
      if (owner && folder !== layout.root && !namedPage(folder)) rowFolders.set(folder, owner);
    }
  }
  const folderPage = (folder: string): Promise<string> => {
    const rows = rowFolders.get(folder);
    if (rows) return databasePage(rows);
    let page = folderPages.get(folder);
    if (!page) {
      const named = namedPage(folder);
      page = named ? documentPage(named)
        : (async () => create(await parentOf(folder), fileImportTitle(folder, untitled, exported), folder))();
      folderPages.set(folder, page);
    }
    return page;
  };
  const parentOf = async (path: string) => {
    const directory = directoryOf(path);
    return directory ? folderPage(directory) : "";
  };
  const documentPage = (path: string): Promise<string> => {
    let page = documentPages.get(path);
    if (!page) {
      page = (async () => create(await parentOf(path), fileImportTitle(path, untitled, exported), path))();
      documentPages.set(path, page);
    }
    return page;
  };
  const databasePage = (path: string): Promise<string> => {
    let page = databasePages.get(path);
    if (!page) {
      page = (async () => database?.(path, await parentOf(path)) ?? "")();
      databasePages.set(path, page);
    }
    return page;
  };

  const result = new Map<string, string>();
  const folders = new Map<string, string>();
  const rows = new Map<string, string>();
  // What is in a database's rows' folder comes right after the database.
  const inRows = new Map<string, string[]>();
  const place = async (path: string) => {
    if (documents.has(path)) {
      result.set(path, await documentPage(path));
      const of = rowFolders.get(directoryOf(path));
      if (of) rows.set(path, of);
      return;
    }
    if (path.endsWith("/")) {
      const folder = path.slice(0, -1);
      folders.set(folder, await folderPage(folder));
      return;
    }
    const directory = directoryOf(path);
    if (directory) folders.set(directory, await folderPage(directory));
    await databasePage(path);
    for (const inside of inRows.get(path) ?? []) await place(inside);
  };
  // A folder is listed as "Folder/", so it sorts as a folder among its siblings.
  const everyFolder = layout.folders.filter((folder) => folder !== layout.root && !rowFolders.has(folder)).map((folder) => `${folder}/`);
  for (const path of [...new Set([...documentPaths, ...databasePaths, ...everyFolder])].sort(archiveOrder)) {
    let rowsOf: string | undefined;
    for (let folder = directoryOf(path.replace(/\/$/, "")); folder && !rowsOf; folder = directoryOf(folder)) rowsOf = rowFolders.get(folder);
    if (!rowsOf || databasePages.has(rowsOf)) await place(path);
    else if (inRows.has(rowsOf)) inRows.get(rowsOf)!.push(path);
    else inRows.set(rowsOf, [path]);
  }
  return { documents: result, folders, rows };
}

/** The folder page a database at `path` is imported under, if any. */
export function archiveDatabaseParent(folders: ReadonlyMap<string, string>, path: string) {
  return folders.get(directoryOf(path)) ?? "";
}

/**
 * Resolves one archive document's relative links: to imported pages, or to
 * the archive's files, each uploaded once however many documents use it.
 * A file that can't be read or uploaded is named in the document's report.
 */
export function archiveConversionContext({
  documentPath,
  pages,
  entries,
  uploads,
  uploadAsset,
  rowProperties,
}: {
  documentPath: string;
  pages: ReadonlyMap<string, string>;
  entries: readonly ZipFileEntry[];
  uploads: Map<string, Promise<ImportReference | null>>;
  uploadAsset: ImportAssetUploader;
  /** For a database's row page: the database's property names. */
  rowProperties?: readonly string[];
}): FileImportConversionContext {
  const byPath = new Map(entries.map((entry) => [entry.name, entry]));
  // An export brought back keeps its pages as they were (2026-11-05: its
  // sub-pages came first and again as mentions, its title again as a
  // heading): each sub-page stands where its page names it alone on a line.
  // Notion's own ZIP import keeps them apart; Leafen's export is its own.
  const exported = exportAttachments(entries.map((entry) => entry.name)) !== null;
  // Its own folder: named like it, or in an export by its title (exportFolderOf).
  const folders = new Set(archiveLayout(entries.map((entry) => entry.name), []).folders);
  const owns = (folder: string) => folder === documentPath.replace(DOCUMENT_EXTENSION, "")
    || exported && exportFolderOf(documentPath, folders) === folder;
  const databasePath = (id: string) => {
    const compact = id.replaceAll("-", "").toLowerCase();
    if (!/^(?:[0-9a-f]{32}|[a-z0-9]{15})$/.test(compact)) return undefined;
    return [...pages.keys()].find((candidate) => /\.csv$/i.test(candidate)
      && candidate.replace(/(_all)?\.csv$/i, "").toLowerCase().endsWith(` ${compact}`));
  };
  return {
    uploadAsset,
    ...(exported ? { exportedTitle: fileImportTitle(documentPath, "", true) } : {}),
    ...(rowProperties ? { rowProperties } : {}),
    // An exported database's CSV is named with its ID ("Tasks <id>.csv"):
    // Notion's, or Leafen's block ID (namedWithExportId).
    databaseHref(id) {
      const path = databasePath(id);
      return path ? `/${path.split("/").map(encodeURIComponent).join("/")}` : undefined;
    },
    importedDatabase(id) {
      const path = databasePath(id);
      return path ? { pageId: pages.get(path)!, title: fileImportTitle(path, "", true) } : undefined;
    },
    // An export's CSV names a page by its path from the export's top, above
    // the CSV's own folder (seen 2026-11-13).
    pageAt(href) {
      for (let directory = directoryOf(documentPath); ; directory = directoryOf(directory)) {
        const path = resolveArchivePath(directory, href);
        const pageId = path ? pages.get(path) ?? (/\.html?$/i.test(path) ? pages.get(path.replace(/\.html?$/i, ".csv")) : undefined) : undefined;
        if (pageId || !directory) return pageId;
      }
    },
    async references(hrefs, leftOut) {
      const references = new Map<string, ImportReference>();
      for (const href of hrefs) {
        if (!isRelativeImportHref(href)) continue;
        const path = resolveArchivePath(directoryOf(documentPath), href);
        if (!path) continue;
        // A database's page in Notion's HTML export is the database (dropDatabaseDrawings).
        const pageId = pages.get(path) ?? (/\.html?$/i.test(path) ? pages.get(path.replace(/\.html?$/i, ".csv")) : undefined);
        if (pageId) {
          references.set(href, { kind: "page", pageId, ...(exported && owns(directoryOf(path)) ? { sub: true } : {}) });
          continue;
        }
        const entry = byPath.get(path);
        if (!entry) continue;
        let upload = uploads.get(path);
        if (!upload) {
          upload = entry.blob().then((file) => uploadAsset(path.split("/").pop() ?? path, file)).catch(() => null);
          uploads.set(path, upload);
        }
        const reference = await upload;
        if (reference) references.set(href, reference);
        else leftOut?.add(path.split("/").pop() ?? path);
      }
      return references;
    },
  };
}
