import { createContext, useContext } from "react";

/** Where an inline database opens alone as a page, where it can be opened so. */
export const DatabaseFullPageLinkContext = createContext<string | null>(null);

export function useDatabaseFullPageLink() {
  return useContext(DatabaseFullPageLinkContext);
}

/**
 * Where an inline database opens alone at its first view, whichever view it
 * shows: as a board group preview's "〈DB〉 열기" opens it (Notion's, seen
 * 2026-11-11), while "전체 페이지로 열기" keeps the view shown.
 */
export const DatabaseFirstViewLinkContext = createContext<string | null>(null);

export function useDatabaseFirstViewLink() {
  return useContext(DatabaseFirstViewLinkContext);
}

/**
 * Whether the database is its page's: a full-page database, or one opened
 * alone, whose page title is the database's, as Notion's (seen 2026-10-01).
 */
export const DatabaseOwnPageContext = createContext(false);

export function useDatabaseOwnPage() {
  return useContext(DatabaseOwnPageContext);
}

/**
 * Whether the address is the database's own page's, which names its view
 * (`?v=`). The home's pinned database is shown away from its page: picking
 * a view, opening a row and closing it leave the home's address without one,
 * as Notion's home widget (seen 2026-11-14: `/<home>?p=<row>&pm=c`).
 */
export const DatabaseNamesAddressContext = createContext(true);

/** A linked full-page database keeps the original source title separate from its page title. */
export const DatabasePageTitleNamesSourceContext = createContext(false);
export function useDatabasePageTitleNamesSource() { return useContext(DatabasePageTitleNamesSourceContext); }
