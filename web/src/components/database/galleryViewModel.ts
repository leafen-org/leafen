import type { GraphProperty, GraphRow } from "../../contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { graphRowPropertySpans, graphRowTitleSpans } from "./richTextCellModel";
import { rowValuesLocked } from "./rowValuesLocked";

export type GalleryPagePresentation = Partial<Pick<
  Page,
  | "blockRevision"
  | "cover"
  | "icon"
  | "iconType"
  | "inTrash"
  | "notionIcon"
  | "subitemChildCount"
>>;

export type GalleryCreateContext = {
  signal: AbortSignal;
};

export type GalleryCreateOperations<Context extends GalleryCreateContext, Row> = {
  createRow(context: Context): Promise<Row | null>;
  isCurrent(context: Context): boolean;
  onError(error: unknown, context: Context): void;
  openRow(row: Row, context: Context): void;
};

export function createGalleryRowCoordinator<
  Context extends GalleryCreateContext,
  Row,
>() {
  let pending: Context | null = null;
  return Object.freeze({
    async run(context: Context, operations: GalleryCreateOperations<Context, Row>) {
      if (
        pending
        && !pending.signal.aborted
        && operations.isCurrent(pending)
      ) return false;
      pending = context;
      try {
        const row = await operations.createRow(context);
        if (row && operations.isCurrent(context) && !context.signal.aborted) {
          operations.openRow(row, context);
        }
      } catch (error) {
        if (operations.isCurrent(context) && !context.signal.aborted) {
          operations.onError(error, context);
        }
      } finally {
        if (pending === context) pending = null;
      }
      return true;
    },
  });
}

export function galleryPresentationProperty(
  property: GraphProperty,
  index: number,
): DbProperty {
  return {
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    type: property.type as DbProperty["type"],
    config: property.config as DbProperty["config"],
    position: index,
    ...(property.publicOrder !== undefined ? { publicOrder: property.publicOrder } : {}),
  };
}

/** A row page's created, edited and archived times and people, as its row carries them. */
export function graphRowPageTimes(row: GraphRow) {
  return {
    createdAt: row.createdAt || undefined,
    createdBy: row.createdBy || undefined,
    updatedAt: row.updatedAt || undefined,
    lastEditedBy: row.lastEditedBy || undefined,
    archivedAt: row.archivedAt || undefined,
    archivedBy: row.archivedBy || undefined,
  };
}

export function galleryPresentationPage({
  position,
  previewRevision,
  presentation,
  row,
  titleProperty,
  workspaceId,
}: {
  position: number;
  previewRevision?: number;
  presentation?: GalleryPagePresentation;
  row: GraphRow;
  titleProperty?: GraphProperty;
  workspaceId: string;
}): Page {
  return {
    id: row.id,
    workspaceId,
    parentId: row.dataSourceId,
    parentType: "database",
    kind: "page",
    title: String(titleProperty ? row.values[titleProperty.id]?.value ?? "" : ""),
    icon: presentation?.icon ?? row.icon,
    iconType: presentation?.iconType ?? row.iconType ?? "none",
    // Without an icon, a row shows an empty or a lined document by whether its page has content.
    hasContent: row.hasContent,
    notionIcon: presentation?.notionIcon,
    cover: presentation?.cover ?? row.cover,
    coverPosition: row.coverPosition,
    properties: Object.fromEntries(
      Object.entries(row.values).map(([propertyId, cell]) => [propertyId, cell.value]),
    ),
    propertySpans: graphRowPropertySpans(row),
    titleSpans: graphRowTitleSpans(row),
    subitemParentId: row.subitemParentId ?? undefined,
    ...graphRowPageTimes(row),
    subitemChildCount: presentation?.subitemChildCount,
    blockRevision: presentation?.blockRevision ?? previewRevision,
    inTrash: presentation?.inTrash,
    // A row whose database page is locked for the reader, or that they may
    // only view, keeps its values.
    isLocked: rowValuesLocked(row) || undefined,
    position,
  };
}
