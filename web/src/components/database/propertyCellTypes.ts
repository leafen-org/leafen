import type { PlaceSearchResponse } from "@/administratorWire.generated";
import type { ExecuteDatabaseButtonResult } from "@/lib/buttonExecutionContract";
import type { SearchOrganizationPeopleInput, SearchOrganizationPeopleResult } from "@/lib/typeaheadSearch";
import type { UploadProgress } from "@/lib/storage";
import type { FileDropBounds } from "../editor/fileDrop";
import type { FormulaBounds } from "./formula";
import type {
  DbProperty,
  FileAttachment,
  Organization,
  OrganizationProfile,
  Page,
  TextSpan,
} from "@/lib/types";

export interface PropertyCellOperationContext {
  workspaceId: string;
  pageId: string;
  dataSourceId: string;
  sessionIdentity: string;
  generation: number;
  currentGeneration: () => number;
}

export interface PropertyCellSnapshot {
  fileDropBounds: FileDropBounds;
  formulaBounds: FormulaBounds;
  userId?: string | null;
  organization?: Organization | null;
  organizationProfiles: OrganizationProfile[];
  pagesById: Record<string, Page>;
  propertiesByDataSourceId: Record<string, DbProperty[]>;
  hydratedRelationTargetIds: ReadonlySet<string>;
  loadedDataSourceIds: ReadonlySet<string>;
  relationPagesByDataSourceId: Record<string, { error?: string | null } | undefined>;
  /**
   * A database template's values, where a date may be today or now and a
   * person me "when duplicated", taking their value when the template is used.
   */
  templateValues?: boolean;
}

export interface PropertyCellOperations {
 searchPlaces?: (query: string, signal: AbortSignal, context: PropertyCellOperationContext) => Promise<PlaceSearchResponse>;
  /**
   * The property takes only the new options its values name; its other
   * changes belong to the database, as on a database page opened alone.
   */
  addOptionsOnly?: boolean;
  /** Who can't change the database sees what a relation's picker shows but can't choose it. */
  relationShownFixed?: boolean;
  setValue: (
    rowId: string,
    property: DbProperty,
    value: unknown,
    context: PropertyCellOperationContext,
  ) => Promise<void> | void;
  /** Writes a text value; a title or text value's spans keep its formatting. */
  setTextValue: (
    rowId: string,
    property: DbProperty,
    value: string,
    context: PropertyCellOperationContext,
    spans?: TextSpan[],
  ) => Promise<void> | void;
  updateProperty: (
    propertyId: string,
    patch: Partial<DbProperty>,
    context: PropertyCellOperationContext,
  ) => Promise<void> | void;
  searchPeople: (
    input: SearchOrganizationPeopleInput,
    options: { signal?: AbortSignal },
    context: PropertyCellOperationContext,
  ) => Promise<SearchOrganizationPeopleResult>;
  uploadFile: (
    file: File,
    row: Page,
    property: DbProperty,
    onProgress: (progress: UploadProgress) => void,
    options: { signal?: AbortSignal },
    context: PropertyCellOperationContext,
  ) => Promise<FileAttachment>;
  runButton: (
    rowId: string,
    propertyId: string,
    confirmationToken: string | undefined,
    options: { signal?: AbortSignal; cancel?: boolean },
    context: PropertyCellOperationContext,
  ) => Promise<ExecuteDatabaseButtonResult | null>;
  /** Takes back what a button's run made and changed, from its notice. */
  undoButton: (rowId: string, propertyId: string, executionId: string) => Promise<void>;
  discardButton: (
    rowId: string,
    propertyId: string,
    context: PropertyCellOperationContext,
  ) => void;
  loadRelation: (
    dataSourceId: string,
    /** `values`: a rollup reads what its relation's rows hold, without a picker open. */
    options: { rows: boolean; values?: boolean; signal?: AbortSignal },
    context: PropertyCellOperationContext,
  ) => Promise<void> | void;
  setRelation: (
    rowId: string,
    property: DbProperty,
    relatedRowIds: string[],
    context: PropertyCellOperationContext,
  ) => Promise<void> | void;
  createRelatedRow: (
    dataSourceId: string,
    options: { signal?: AbortSignal },
    context: PropertyCellOperationContext,
  ) => Promise<Page | null>;
  renameRelatedRow: (
    rowId: string,
    title: string,
    context: PropertyCellOperationContext,
  ) => Promise<void> | void;
  /** Takes back a row just made here: a page section's new page left unnamed by Enter or Escape (Notion's, seen 2026-11-14). */
  discardRelatedRow?: (rowId: string, context: PropertyCellOperationContext) => Promise<void> | void;
  notify: (message: string, kind: "error" | "info" | "success") => void;
  getCurrentFiles: (rowId: string, propertyId: string) => FileAttachment[];
}

/** Property types an in-place cell editor (`PropertyCell`) edits. */
export const LOCAL_PROPERTY_CELL_TYPES: ReadonlySet<string> = new Set([
  "title", "rich_text", "number", "select", "multi_select", "status", "date", "checkbox",
  "url", "email", "phone", "person", "created_time", "last_edited_time", "created_by", "last_edited_by",
  "files", "relation", "rollup", "formula", "unique_id", "location", "last_visited_time", "place", "verification",
  "button",
]);

export interface PropertyCellEnvironment {
  snapshot: PropertyCellSnapshot;
  operations: PropertyCellOperations;
  context: PropertyCellOperationContext;
}

export interface PropertyCellRequest {
  signal: AbortSignal;
  isCurrent: () => boolean;
  abort: () => void;
}

export function capturePropertyCellRequest(
  context: PropertyCellOperationContext,
): PropertyCellRequest {
  const controller = new AbortController();
  return {
    signal: controller.signal,
    isCurrent: () => !controller.signal.aborted && context.currentGeneration() === context.generation,
    abort: () => controller.abort(),
  };
}

/**
 * The cleanup of the reads a cell starts as it shows: aborts them only while
 * one still runs. A page's relation and rollup cells each aborted theirs as
 * the page went, long done or never started (a table's own cells read
 * nothing there), an abort event a cell: 9 ms of a page of a thousand such
 * cells leaving (2026-11-13).
 */
export function abortWhileRunning(request: PropertyCellRequest, reads: readonly (Promise<void> | void)[]): () => void {
  let running = 0;
  const done = () => { running -= 1; };
  for (const read of reads) {
    if (!read) continue;
    running += 1;
    // A read that fails is left to fail as it did: only its end is noted.
    void read.then(done, (error: unknown) => { done(); throw error; });
  }
  return () => { if (running > 0) request.abort(); };
}

export type PropertyCellPresentation = "default" | "rowDetail";

/**
 * Which icon a title shows before it: the page's own icon only, the page's
 * icon or the plain page glyph (a table's title column, as Notion's), or
 * none (a table with "show page icon" off).
 */
export type TitleIconMode = "own" | "page" | "none";
