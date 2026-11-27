import { createContext, useContext, type ReactNode } from "react";

/**
 * The page a view's filters name "this page", offered first among the pages
 * of its data source, as Notion's (seen 2026-11-12): a page layout tab's
 * open page, "이 페이지"; a database template's own page, by its name and
 * icon — the views in its body then filter each page made from it by that
 * page. Anywhere else a relation filter lists the pages alone and "this
 * page" is the page the view sits in. A page previewed from a view has none
 * (PageLayoutTabProviders).
 */
export type FilterThisPage = {
  rowId: string;
  dataSourceId: string;
  /** A template's name and icon, 20px in the list and FILTER_PAGE_CHIP_ICON in its chip; a tab's reads "이 페이지". */
  name?: string;
  icon?: ReactNode;
  chipIcon?: ReactNode;
  /**
   * A template's own page: picked, it is marked "이 템플릿" beside its name,
   * which an untitled row's "새 페이지" shares (a difference from Notion's,
   * whose list names both alike — seen 2026-11-20). No row holds it in a
   * relation, so the views in its body keep none by it and a row made there
   * takes it as no relation (NativeDatabaseViewMount).
   */
  template?: boolean;
  /**
   * A template's own value of a two-way relation takes a row made in a view
   * of its body that the view keeps to it on the relation's other side
   * (templateBodyRows.ts); an editor that can't write it gives none.
   */
  takeRow?(propertyId: string, rowId: string): void;
};

/**
 * A chosen page's icon in its filter chip: a box as tall as the line of the
 * chip's 12px words, 14.4px, which the page's glyph fills top to bottom, as
 * Notion's (seen 2026-11-16 three times, a template's "이 페이지" chip: the
 * glyph 11.1×14.4 in a 14.39 box).
 */
export const FILTER_PAGE_CHIP_ICON = 14.4;

export const FilterThisPageContext = createContext<FilterThisPage | null>(null);

export function useFilterThisPage() {
  return useContext(FilterThisPageContext);
}
