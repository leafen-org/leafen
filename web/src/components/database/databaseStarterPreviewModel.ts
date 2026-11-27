import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import type { DbView, ViewConfig } from "@/lib/types";
import { galleryPresentationPage, galleryPresentationProperty } from "./galleryViewModel";
import { evaluateFormula, formulaDisplay } from "./formula";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { applyView } from "./query";
import { pagesByIdOf } from "@/lib/pageIndex";

/** The exported examples use the product's existing formula/filter/sort model. */
export function databaseStarterPreviewModel(properties: readonly GraphProperty[], rows: readonly GraphRow[], view?: GraphView, authorId?: string) {
  const actor = authorId ?? "database-starter-preview-author";
  const props = properties.map(galleryPresentationProperty), title = properties.find(p => p.type === "title");
  const pages = rows.map((row, position) => ({ ...galleryPresentationPage({ row, position, titleProperty: title, workspaceId: "" }),
    createdBy: actor, lastEditedBy: actor }));
  const pagesById = pagesByIdOf(pages);
  const ordered = view ? applyView(pages, props, { ...view, position: 0, databaseId: view.dataSourceId, config: view.config as ViewConfig } as DbView,
    pagesById, { currentUserId: actor }, nativeBoardQueryBounds) : pages;
  const byId = new Map(rows.map(row => [row.id, row]));
  return {
    rows: ordered.flatMap(page => byId.get(page.id) ?? []),
    formula: (property: GraphProperty, row: GraphRow) => {
      const page = pagesById[row.id], prop = props.find(p => p.id === property.id)!;
      return formulaDisplay(page, prop, nativeBoardQueryBounds.formula, "", () => evaluateFormula({
        row: page, prop, props, pagesById, bounds: nativeBoardQueryBounds.formula,
      })).text;
    },
  };
}
