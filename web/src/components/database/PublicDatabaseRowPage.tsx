import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { materializeContentGraph, type GraphProperty, type GraphRow } from "@/contentGraph";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { usePublicPageDocument, usePublicPageSource } from "@/lib/publicPageSource";
import type { Page } from "@/lib/types";
import { PublicPageBody } from "../page/PublicContentPage";
import { DatabaseRowPage, type DatabaseRowPagePreviewMode } from "./DatabaseRowPage";
import type { PropertyValueReadContext } from "./PropValue";

/**
 * A published database's row as the page it opens, in the font, text size
 * and width its page was given once the page's own read tells them.
 */
function publicRowPageSnapshot(
  row: GraphRow, properties: readonly GraphProperty[], containerPageId: string, workspaceId: string,
  style?: Pick<Page, "font" | "smallText" | "fullWidth" | "tableOfContentsDisplay">,
): DatabaseRowPageSnapshot {
  return {
    snapshotIdentity: `public-row:${row.id}`,
    containerSnapshotRevision: 0,
    bodySnapshotRevision: 0,
    row: {
      id: row.id, containerPageId, dataSourceId: row.dataSourceId, subitemParentId: row.subitemParentId,
      position: row.position, values: row.values as DatabaseRowPageSnapshot["row"]["values"],
      icon: row.icon ?? "", iconType: (row.iconType ?? "none") as DatabaseRowPageSnapshot["row"]["iconType"], cover: row.cover ?? "", coverPosition: row.coverPosition ?? 50,
      font: style?.font || "default", smallText: style?.smallText ?? false, fullWidth: style?.fullWidth ?? false,
      tableOfContentsDisplay: style?.tableOfContentsDisplay ?? "default",
      favorite: false, locked: false, unlockedForMe: false,
      archivedAt: row.archivedAt ?? "", archivedBy: row.archivedBy ?? "",
      trashedAt: "", trashedBy: "", createdAt: row.createdAt ?? "", createdBy: row.createdBy ?? "",
      updatedAt: row.updatedAt ?? "", lastEditedBy: row.lastEditedBy ?? "", authorityRevision: 0,
    },
    body: [],
    nextCursor: null,
    hasMore: false,
    properties: properties.map(({ id, name, type, config }) => ({ id, name, type, config: config ?? {} })),
    accessRole: "view",
    workspaceId,
  };
}

/**
 * A published page's database page opened in a peek, as Notion's public
 * pages open one: its properties from the database's row, laid out as its
 * database lays its pages out, and its body read through the share link or
 * site the page was opened from.
 */
export function PublicDatabaseRowPage({
  mode,
  row,
  properties,
  containerPageId,
  containerTitle,
  workspaceId,
  valueRow,
  propertyValueContext,
  onClose,
  onOpenMode,
}: {
  mode: DatabaseRowPagePreviewMode;
  row: GraphRow;
  properties: readonly GraphProperty[];
  containerPageId: string;
  containerTitle: string;
  workspaceId: string;
  valueRow: Page;
  propertyValueContext: PropertyValueReadContext;
  onClose: () => void;
  onOpenMode: (mode: "full" | "new_tab") => void;
}) {
  const { t } = useTranslation("databaseView");
  const source = usePublicPageSource();
  const { document, failed } = usePublicPageDocument(row.id);
  const graph = useMemo(() => document ? materializeContentGraph(document.graph) : null, [document]);
  const style = document?.page;
  const snapshot = useMemo(
    () => publicRowPageSnapshot(row, properties, containerPageId, workspaceId, style),
    [containerPageId, properties, row, style, workspaceId],
  );
  const body = document && graph
    ? <PublicPageBody document={document} graph={graph} staticReadOnlySemantics={source?.site ?? false} />
    : failed
      ? <p role="alert">{t("publicRowPageUnavailable")}</p>
      : <div aria-busy="true" data-public-row-body-loading />;
  return (
    <DatabaseRowPage mode={mode} properties={properties} snapshot={snapshot} containerTitle={containerTitle}
      propertyValueContext={propertyValueContext}
      valueRow={valueRow} onClose={onClose} onOpenMode={onOpenMode}
      workspaceId={workspaceId} body={body} layout={document?.pageLayout ?? null} />
  );
}
