import { createArchivePages, archiveDatabaseParent } from "./archiveImportPlan";
import { isDatabaseImportKind, type ArchiveLayout, type FileImportSource } from "./fileImportSources";
import { workspaceImportBatchItemKinds, workspaceImportBounds, type WorkspaceImportBatchItemInput } from "@/workspaceImportWire.generated";
import { graphWireBounds } from "@/contentGraphWire.generated";
import { firstCharacters } from "./pasteBound";

/**
 * What a batch of files brings, in the order the server makes it: an
 * archive's folders, documents and databases, parents first and in the order a
 * page shows them, then the files chosen on their own, then what isn't
 * imported, to be reported.
 * Each item has its place in the batch; the same files give the same plan.
 */
export type ImportBatchPlan = {
  items: WorkspaceImportBatchItemInput[];
  /** A source's place in the batch, by its index among the sources. */
  places: Map<number, number>;
  /** An archive's documents' places, by archive and path, for links between them. */
  documents: Map<string, Map<string, number>>;
  /** An export's row pages, by archive and path: the database's path each is a row of. */
  rows: Map<string, ReadonlyMap<string, string>>;
  /** How many items are documents or databases, which `total` counts. */
  total: number;
};

export async function planImportBatch(
  sources: readonly FileImportSource[], skipped: readonly string[], untitled: string, archiveRootTitle: (name: string) => string,
  layouts: ReadonlyMap<string, ArchiveLayout> = new Map(),
  attachments: ReadonlyArray<{ name: string; fileId: string }> = [],
): Promise<ImportBatchPlan> {
  const items: WorkspaceImportBatchItemInput[] = [];
  const places = new Map<number, number>();
  const documents = new Map<string, Map<string, number>>();
  const rows = new Map<string, ReadonlyMap<string, string>>();
  const add = (item: Omit<WorkspaceImportBatchItemInput, "position">) => {
    const position = items.length;
    // A ZIP may name a path longer than a page's title or what is kept of its
    // name: its first characters, never half of one, as the server counts them.
    items.push({
      position, ...item,
      title: firstCharacters(item.title, graphWireBounds.titleRunes),
      name: firstCharacters(item.name, workspaceImportBounds.batchNameRunes),
    });
    return position;
  };
  const nestedAt = (source: FileImportSource) => source.archive && !isDatabaseImportKind(source.kind);
  const folders = new Map<string, ReadonlyMap<string, string>>();
  const roots = new Map<string, number>();

  for (const key of new Set(sources.map((source) => source.archive?.key).filter((value): value is string => Boolean(value)))) {
    const inArchive = sources.filter((source) => source.archive?.key === key);
    // A ZIP comes in under a page of its own, as Notion's (seen 2026-11-04).
    const archiveName = key.slice(key.indexOf(":") + 1);
    const root = add({ parentPosition: -1, kind: workspaceImportBatchItemKinds.folder, title: archiveRootTitle(archiveName), name: archiveName });
    roots.set(key, root);
    const paths = inArchive.filter(nestedAt).map((source) => source.archive!.path);
    const sourceAt = new Map(inArchive.filter(nestedAt).map((source) => [source.archive!.path, source]));
    const created = await createArchivePages(
      paths,
      async (parentId, title, place) => {
        const source = sourceAt.get(place);
        return String(add({
          parentPosition: parentId === "" ? root : Number(parentId),
          kind: source ? workspaceImportBatchItemKinds.document : workspaceImportBatchItemKinds.folder,
          title, name: source?.name ?? place,
        }));
      },
      untitled,
      inArchive.filter((source) => isDatabaseImportKind(source.kind)).map((source) => source.archive!.path),
      layouts.get(key),
      (path, parentId) => {
        const index = sources.findIndex((source) => source.archive?.key === key && source.archive.path === path && isDatabaseImportKind(source.kind));
        const source = sources[index]!;
        const position = add({
          parentPosition: parentId === "" ? root : Number(parentId),
          kind: workspaceImportBatchItemKinds.database, title: source.title, name: source.name,
        });
        places.set(index, position);
        return String(position);
      },
    );
    documents.set(key, new Map([...created.documents].map(([path, position]) => [path, Number(position)])));
    folders.set(key, created.folders);
    rows.set(key, created.rows);
  }

  const taken = new Set<number>();
  for (const [index, source] of sources.entries()) {
    if (places.has(index)) continue;
    const archive = source.archive;
    if (archive && !isDatabaseImportKind(source.kind)) {
      const position = documents.get(archive.key)?.get(archive.path);
      // A ZIP naming one path twice gets a page for each of them.
      if (position !== undefined && !taken.has(position)) {
        taken.add(position);
        places.set(index, position);
        continue;
      }
    }
    const parent = archive ? archiveDatabaseParent(folders.get(archive.key) ?? new Map(), archive.path) : "";
    places.set(index, add({
      parentPosition: parent !== "" ? Number(parent) : archive ? roots.get(archive.key) ?? -1 : -1,
      kind: archive && isDatabaseImportKind(source.kind) ? workspaceImportBatchItemKinds.database : workspaceImportBatchItemKinds.file,
      title: source.title, name: source.name,
    }));
  }
  const total = sources.length;
  for (const name of skipped) add({ parentPosition: -1, kind: workspaceImportBatchItemKinds.skipped, title: name, name });
  // A ZIP's attachment no document uses is no page; its import's log keeps it.
  for (const { name, fileId } of attachments) {
    add({ parentPosition: -1, kind: workspaceImportBatchItemKinds.skipped, title: name, name, fileId });
  }
  return { items, places, documents, rows, total };
}
