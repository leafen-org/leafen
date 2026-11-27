import { useMemo } from "react";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DatabaseSubitemsFeature } from "@/lib/types";
import { useProductPagesSelect } from "@/components/product/productPagesStore";

/** Whether a data source is a wiki's: its pages, its root among them, name it. */
export function useWikiSource(sourceId: string | undefined) {
  // One answer from the page list, which a list read again leaves as it was (productPagesStore.tsx).
  return useProductPagesSelect((pages) => Boolean(sourceId && pages?.pages.some((page) => page.wikiDataSourceId === sourceId)));
}

/**
 * A wiki's pages sit in the wiki under the page above them by their rows'
 * sub-item parent, which the sidebar shows; Notion's wikis list them in their
 * views flat (seen 2026-11-11: "모든 페이지" lists a page's sub-page as a row
 * of its own, beside the others, with no toggle and no "새 하위 항목"). Its
 * views show its pages so, until its Sub-pages are on: then they nest by its
 * Parent page, as Notion's "하위 페이지" do, a relation of their own that
 * leaves the pages' places in the wiki as they are (the server reads a
 * nesting made in a view as the Parent page it names).
 */
export function useWikiSourceShape(
  sourceId: string,
  rows: GraphRow[],
  properties: GraphProperty[],
  subitems: DatabaseSubitemsFeature | null,
) {
  const wiki = useWikiSource(sourceId);
  const parentId = wiki ? subitems?.parentPropertyId : undefined;
  const shownRows = useMemo(() => {
    if (!wiki) return rows;
    const ids = parentId ? new Set(rows.map(({ id }) => id)) : undefined;
    return rows.map((row) => {
      const held = parentId ? row.values[parentId]?.value : undefined;
      const above = Array.isArray(held) ? held.find((id): id is string => typeof id === "string" && Boolean(ids?.has(id))) ?? null : null;
      return (row.subitemParentId ?? null) === above ? row : { ...row, subitemParentId: above };
    });
  }, [parentId, rows, wiki]);
  return { rows: shownRows, properties, subitems: wiki && !parentId ? null : subitems };
}
