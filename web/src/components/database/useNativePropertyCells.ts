"use client";

import { useMemo, useRef } from "react";
import { useCommittedOperationScope } from "../product/useCommittedOperationScope";
import type { GraphDataSource, GraphProperty, GraphRow } from "@/contentGraph";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import { uploadWorkspaceFile } from "@/lib/storage";
import { FILE_DROP_BOUNDS } from "../editor/fileDrop";
import { buttonExecutionCanRestart, databaseButtonResultFromWire } from "@/lib/buttonExecution";
import { newId } from "@/lib/ids";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { useOptionalProductToast } from "@/lib/productToast";
import type { DbProperty, Page, TextSpan } from "@/lib/types";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { lookupById, lookupOver } from "@/lib/keptLookup";
import {
  databaseFilterGraphIdentity,
  nativeDatabaseFilterSnapshot,
  nativeDatabaseHydratedRelationTargetIds,
  type DatabaseFilterGraph,
} from "./databaseFilterSnapshot";
import { graphFilePropertyValue, normalizeFileAttachments } from "./files";
import type { PropertyValueReadContext } from "./PropValue";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { valueAsRead } from "./valueAsRead";

const propertyCellSchemaReadOwner = Object.freeze({ request: () => false, retire: () => {} });
const NO_ORGANIZATION_PROFILES: PropertyCellEnvironment["snapshot"]["organizationProfiles"] = [];
const NO_RELATION_PAGES: PropertyCellEnvironment["snapshot"]["relationPagesByDataSourceId"] = {};

type CellPages = { dataSources: readonly GraphDataSource[]; workspaceId: string; value: Record<string, Page> };
const cellPages = new WeakMap<object, CellPages>();

/**
 * The rows' pages and the data sources' over them as one lookup, made once
 * for the rows' pages whichever cells ask (a view's and its table's), and
 * reading through the rows' pages when they follow their list
 * (lib/keptLookup.ts): a page of 100,000 rows listed, filtered and paired
 * them anew, some 115 ms each time its graph was read, and twice for each
 * edit that arrived (2026-11-11).
 */
function cellPagesById(pages: Readonly<Record<string, Page | undefined>>, dataSources: readonly GraphDataSource[], workspaceId: string) {
  const kept = cellPages.get(pages);
  if (kept?.dataSources === dataSources && kept.workspaceId === workspaceId) return kept.value;
  const byId = lookupOver(pages, sourcePagesById(dataSources, workspaceId)) as Record<string, Page>;
  cellPages.set(pages, { dataSources, workspaceId, value: byId });
  return byId;
}

// The data sources' pages, kept for the data sources as read: made again
// with the rows' pages, a relation's and a rollup's cells read their
// databases as changed and each was drawn again for a row made, copied or
// sent to the trash (2026-11-14: 60 cells of a table of 30 rows).
const sourcePages = new WeakMap<readonly GraphDataSource[], { workspaceId: string; value: Readonly<Record<string, Page>> }>();

function sourcePagesById(dataSources: readonly GraphDataSource[], workspaceId: string) {
  const kept = sourcePages.get(dataSources);
  if (kept?.workspaceId === workspaceId) return kept.value;
  const value = lookupById(dataSources.map((source, position) => dataSourcePage(source, workspaceId, position)));
  sourcePages.set(dataSources, { workspaceId, value });
  return value;
}

/**
 * What a database view needs to show and edit property cells in place, as a
 * table does: the read context for values and relations and, with a writer,
 * the cell editors' operations, which save through the table writer.
 */
export function useNativePropertyCells({
  adapter,
  contextIdentity,
  currentUserId,
  dataSource,
  dataSources,
  filterGraph,
  pageId,
  properties,
  readOnly = false,
  requestGeneration,
  rows,
  workspaceId,
}: {
  adapter?: NativeTableProductAdapter;
  /**
   * The cells only show their values (a locked page's table): they keep their
   * ways, so locking draws no other cells, and write nothing (cellReadOnly.ts).
   */
  readOnly?: boolean;
  contextIdentity: string;
  currentUserId?: string;
  dataSource: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  pageId: string;
  properties: readonly GraphProperty[];
  requestGeneration: number;
  rows: readonly GraphRow[];
  workspaceId: string;
}) {
  const filterSnapshotIdentity = useMemo(
    () => databaseFilterGraphIdentity({ properties: filterGraph.properties, rows: filterGraph.rows }),
    [filterGraph.properties, filterGraph.rows],
  );
  const filterSnapshot = useMemo(() => nativeDatabaseFilterSnapshot({
    currentUserId: currentUserId ?? "",
    graph: filterGraph,
    snapshotIdentity: filterSnapshotIdentity,
    workspaceId,
  }), [currentUserId, filterGraph, filterSnapshotIdentity, workspaceId]);
  const pagesById = useMemo(() => cellPagesById(filterSnapshot.pagesById, dataSources, workspaceId),
    [dataSources, filterSnapshot.pagesById, workspaceId]);
  const propertiesByDataSourceId = useMemo<Record<string, DbProperty[]>>(() => Object.fromEntries(
    Object.entries(filterSnapshot.propertiesByDatabase).flatMap(([dataSourceId, values]) => (
      values ? [[dataSourceId, [...values]]] : []
    )),
  ), [filterSnapshot.propertiesByDatabase]);
  const hydratedRelationTargetIds = useMemo(
    () => nativeDatabaseHydratedRelationTargetIds(filterGraph),
    [filterGraph],
  );
  const loadedDataSourceIds = useMemo(
    () => new Set(dataSources.map(({ id }) => id)),
    [dataSources],
  );
  const propertyValueContext = useMemo<PropertyValueReadContext>(() => ({
    bounds: nativeBoardQueryBounds.formula,
    contextIdentity,
    hydratedRelationTargetIds,
    pagesById,
    propsByDatabaseId: propertiesByDataSourceId,
    schemaReadOwner: propertyCellSchemaReadOwner,
    snapshotIdentity: filterSnapshotIdentity,
    userId: currentUserId,
  }), [contextIdentity, currentUserId, filterSnapshotIdentity, hydratedRelationTargetIds, pagesById, propertiesByDataSourceId]);
  const latest = useRef({ adapter, contextIdentity, properties, readOnly, requestGeneration, rows, graphRows: filterGraph.rows });
  latest.current = { adapter, contextIdentity, properties, readOnly, requestGeneration, rows, graphRows: filterGraph.rows };
  const valueWrites = useRef(new Map<string, symbol>());
  const buttonRuns = useRef(new Map<string, string>());
  // Rows a relation cell just made, named next by what was typed.
  const relatedRows = useRef(new Map<string, GraphRow>());
  // A cell's message (a button that couldn't run) shows as the page's toasts do.
  const toast = useOptionalProductToast();
  const notify = toast?.notify;
  const valueScope = useCommittedOperationScope([contextIdentity, Boolean(adapter)]);
  const cellWays = useMemo<Pick<PropertyCellEnvironment, "context" | "operations"> | undefined>(() => {
    if (!adapter) return undefined;
    const currentGeneration = () => latest.current.requestGeneration;
    // A value typed in (`typed`) is saved once the typing ends (lib/typedWrites.ts).
    const commitValue = (rowId: string, propertyId: string, value: unknown, generationValue: number, spans?: TextSpan[], typed = false) => {
      const current = latest.current;
      const row = current.rows.find((candidate) => candidate.id === rowId);
      const property = current.properties.find((candidate) => candidate.id === propertyId);
      if (!row || !property || current.readOnly || valueScope.signal.aborted || current.requestGeneration !== generationValue) return;
      const canonicalValue = property.type === "files" ? graphFilePropertyValue(value) : value;
      // A page verified without owners is its verifier's, as the server keeps
      // it (Notion: a verified page has an owner); the Verification property
      // names its Owner, whose owners read as the page's creator while it
      // defaults to the creator and names no one (valueAsRead).
      const ownerId = property.type === "verification" ? String((property.config as { ownerPropertyId?: unknown } | undefined)?.ownerPropertyId ?? "") : "";
      const ownerProperty = ownerId ? current.properties.find((candidate) => candidate.id === ownerId) : undefined;
      const owners = ownerProperty ? valueAsRead(row, ownerProperty, row.values[ownerId]?.value) : undefined;
      const ownerFill = ownerProperty && currentUserId && (canonicalValue as { state?: unknown } | null)?.state === "verified"
        && (!Array.isArray(owners) || owners.length === 0)
        ? { [ownerId]: { type: "person" as const, value: [currentUserId] } } : {};
      const key = `${rowId}:${propertyId}`, intent = Symbol(key);
      valueWrites.current.set(key, intent);
      // Snapshot changes include this writer's own receipts and peer cells.
      // Admit against the shown generation, then publish only the latest
      // admitted value in the same live surface and native field type.
      return adapter.commitRows([{
        ...row,
        values: { ...row.values, [property.id]: { type: property.type, value: canonicalValue, ...(spans?.length ? { spans } : {}) }, ...ownerFill },
      }], () => !valueScope.signal.aborted && valueWrites.current.get(key) === intent
        && latest.current.contextIdentity === contextIdentity && Boolean(latest.current.adapter)
        && latest.current.rows.some(candidate => candidate.id === rowId)
        && latest.current.properties.some(candidate => candidate.id === propertyId && candidate.type === property.type), typed)
        .finally(() => { if (valueWrites.current.get(key) === intent) valueWrites.current.delete(key); });
    };
    return {
      context: {
        workspaceId,
        pageId,
        dataSourceId: dataSource.id,
        sessionIdentity: contextIdentity,
        generation: requestGeneration,
        currentGeneration,
      },
      operations: {
        setValue: (rowId, property, value, context) => commitValue(rowId, property.id, value, context.generation),
        setTextValue: (rowId, property, value, context, spans) => commitValue(rowId, property.id, value, context.generation, spans, true),
        updateProperty: (propertyId, patch, context) => {
          const current = latest.current;
          const property = current.properties.find((candidate) => candidate.id === propertyId);
          if (!property || current.readOnly || current.requestGeneration !== context.generation) return;
          return adapter.commitProperties([{
            ...property,
            name: patch.name ?? property.name,
            type: patch.type ?? property.type,
            config: patch.config ? { ...patch.config } : property.config,
          }], () => latest.current.requestGeneration === context.generation);
        },
        // A person cell picks from the organization's people.
        searchPlaces: adapter.searchPlaces ? async (query,signal,context) => {
 if (latest.current.requestGeneration !== context.generation) throw new Error("The place editor changed.");
 return adapter.searchPlaces!(query,signal);
 } : undefined,
        searchPeople: adapter.searchPeople
          ? (input, options) => adapter.searchPeople!(input, options)
          : async (input) => ({ organization: undefined, query: input.query, limit: input.limit, people: [] }),
        // A files cell uploads through the shared workspace file owner; the
        // cell then saves the uploaded file as a managed files value.
        uploadFile: async (file, _row, property, onProgress, options) => {
          const uploaded = await uploadWorkspaceFile(file, "database/files", {
            workspaceId, pageId, databaseId: dataSource.id, propertyId: property.id,
          }, { onProgress, signal: options.signal });
          return {
            id: uploaded.fileId, fileId: uploaded.fileId, name: uploaded.name, url: uploaded.url,
            type: uploaded.type, contentType: uploaded.type, size: uploaded.size, sha256: uploaded.sha256,
          };
        },
        // A button's run keeps its id while its question waits, so the answer
        // confirms that run; cancelling forgets it.
        runButton: async (rowId, propertyId, confirmationToken, options, context) => {
          if (!adapter.runButton || latest.current.readOnly || latest.current.requestGeneration !== context.generation) return null;
          const key = `${rowId}:${propertyId}`;
          const executionId = buttonRuns.current.get(key) ?? newId();
          buttonRuns.current.set(key, executionId);
          try {
          const response = await adapter.runButton(rowId, propertyId, {
            executionId, ...(confirmationToken ? { confirmationToken } : {}), ...(options.cancel ? { cancel: true } : {}),
          }, options.signal);
          if (!response.confirmation) buttonRuns.current.delete(key);
          return databaseButtonResultFromWire(response, executionId);
          } catch (error) {
            if (buttonExecutionCanRestart(error)) buttonRuns.current.delete(key);
            throw error;
          }
        },
        undoButton: async (rowId, propertyId, executionId) => {
          if (!adapter.runButton) throw new Error("This view can't run buttons.");
          await adapter.runButton(rowId, propertyId, { executionId, undo: true });
        },
        discardButton: (rowId, propertyId) => { buttonRuns.current.delete(`${rowId}:${propertyId}`); },
        loadRelation: () => {},
        setRelation: (rowId, property, relatedRowIds, context) => commitValue(rowId, property.id, relatedRowIds, context.generation),
        // A relation's "새로 만들기" makes a row in the database it relates
        // to, then names it what was typed (PropertyRelationCell.tsx).
        createRelatedRow: async (dataSourceId, options, context) => {
          const source = dataSources.find((candidate) => candidate.id === dataSourceId);
          const current = () => !options.signal?.aborted && latest.current.requestGeneration === context.generation;
          if (!source || !current() || latest.current.readOnly) return null;
          const row = await adapter.createRow(source, null, null, current, {});
          if (!row) return null;
          relatedRows.current.set(row.id, row);
          const made: Page = { id: row.id, workspaceId, parentType: "database", parentId: source.id, kind: "page", title: "", iconType: "none", position: 0 };
          return made;
        },
        // A row it just made, or one it shows, as a page section's "페이지 이름 바꾸기" renames (Notion's, seen 2026-11-07).
        renameRelatedRow: async (rowId, title, context) => {
          const row = relatedRows.current.get(rowId) ?? latest.current.graphRows.find(({ id }) => id === rowId);
          relatedRows.current.delete(rowId);
          const titleId = row && Object.keys(row.values).find((id) => row.values[id]?.type === "title");
          if (!row || !titleId || latest.current.readOnly || latest.current.requestGeneration !== context.generation) return;
          await adapter.commitRows([{ ...row, values: { ...row.values, [titleId]: { type: "title", value: title } } }],
            () => !valueScope.signal.aborted && latest.current.requestGeneration === context.generation);
        },
        discardRelatedRow: async (rowId, context) => {
          relatedRows.current.delete(rowId);
          const current = () => !valueScope.signal.aborted && latest.current.requestGeneration === context.generation;
          if (latest.current.readOnly || !current()) return;
          await adapter.deleteRows([rowId], current);
        },
        notify: (message, kind) => notify?.(message, kind === "info" ? "default" : kind),
        getCurrentFiles: (rowId, propertyId) => {
          const row = latest.current.rows.find((candidate) => candidate.id === rowId);
          return normalizeFileAttachments(row?.values[propertyId]?.value);
        },
      },
    };
  }, [adapter, contextIdentity, currentUserId, dataSource.id, dataSources, notify, pageId, requestGeneration, valueScope, workspaceId]);
  // The cells' snapshot is made again as rows change, their ways only as the
  // view does, so a cell that reads its own row alone is not drawn again for
  // another row's change (PropertyCell).
  const organizationId = adapter?.organizationId;
  const organization = useMemo(() => organizationId ? { id: organizationId, name: "" } : null, [organizationId]);
  const cellSnapshot = useMemo<PropertyCellEnvironment["snapshot"]>(() => ({
    fileDropBounds: FILE_DROP_BOUNDS,
    formulaBounds: nativeBoardQueryBounds.formula,
    userId: currentUserId,
    organization,
    organizationProfiles: NO_ORGANIZATION_PROFILES,
    pagesById,
    propertiesByDataSourceId,
    hydratedRelationTargetIds,
    loadedDataSourceIds,
    relationPagesByDataSourceId: NO_RELATION_PAGES,
  }), [currentUserId, hydratedRelationTargetIds, loadedDataSourceIds, organization, pagesById, propertiesByDataSourceId]);
  const propertyCellEnvironment = useMemo<PropertyCellEnvironment | undefined>(
    () => cellWays && { snapshot: cellSnapshot, ...cellWays }, [cellSnapshot, cellWays]);
  return { filterSnapshot, propertyCellEnvironment, propertyValueContext };
}
