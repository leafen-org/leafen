import type { GraphDataSource } from "@/contentGraph";
import type { Page } from "@/lib/types";

/**
 * Whether a data source is in the trash as a page's graph reads it: the
 * source itself, its database keeping it there (sourceInTrash), or the page
 * holding it, itself or with a page above it (sourcePageInTrash,
 * content_graph_window.go). A list of databases leaves such a source out, as
 * Notion's do (seen 2026-11-24 twice: an automation's "페이지 편집" list
 * offered neither a source in the trash nor one whose page was there).
 */
export function dataSourceInTrash(source: { sourceInTrash?: boolean; sourcePageInTrash?: boolean }): boolean {
  return source.sourceInTrash === true || source.sourcePageInTrash === true;
}

/**
 * A data source as the database page the views, cells and pickers name: its
 * title and icon, in the trash where it is (dataSourceInTrash). Every such
 * page is made here, so none loses the mark on its way to a list.
 */
export function dataSourcePage(source: GraphDataSource, workspaceId: string, position = 0, more?: Partial<Page>): Page {
  return {
    id: source.id,
    workspaceId,
    parentType: "workspace",
    kind: "database",
    title: source.title,
    icon: source.icon,
    iconType: source.iconType ?? "none",
    inTrash: dataSourceInTrash(source),
    position,
    ...more,
  };
}
