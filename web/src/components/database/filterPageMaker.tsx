import { createContext, useContext, useMemo, useRef, type ReactNode } from "react";
import type { Page } from "@/lib/types";
import { DatabaseSprintContext, type DatabaseSprintSurface } from "./databaseSprintContext";
import { capturePropertyCellRequest, type PropertyCellEnvironment } from "./propertyCellTypes";

/**
 * A view's cells' ways (useNativePropertyCells) for what its toolbar draws:
 * a relation filter's value field makes a page in the related database with
 * them, as a relation cell's "새 페이지 생성" does. None where the view is
 * read-only, and none outside a view's toolbar.
 */
const FilterPageMakerContext = createContext<PropertyCellEnvironment | undefined>(undefined);

/** A toolbar's sprints and its cells' ways, held for the menus and sheets it draws. */
export function ToolbarMountContexts({ sprints, cells, children }: {
  sprints: DatabaseSprintSurface | null;
  cells: PropertyCellEnvironment | undefined;
  children: ReactNode;
}) {
  return (
    <DatabaseSprintContext.Provider value={sprints}>
      <FilterPageMakerContext.Provider value={cells}>{children}</FilterPageMakerContext.Provider>
    </DatabaseSprintContext.Provider>
  );
}

export interface FilterPageMaker {
  /** The database a page is made in, its icon and name for the row offering it. */
  database: Page;
  /** Makes a page named `title` there: its id as soon as it is made on this device, or null. */
  make(title: string): Promise<string | null>;
}

/**
 * How a filter's value field makes a page in database `databaseId`, or null
 * where it cannot: the row is made here first and goes to the server behind
 * (createRelatedRow), then named, as a relation picker's "+ ⛁ T에 새 〈words〉
 * 페이지 생성" makes it (PropertyRelationCell).
 */
export function useFilterPageMaker(databaseId: string | undefined): FilterPageMaker | null {
  const cells = useContext(FilterPageMakerContext);
  const latest = useRef(cells);
  latest.current = cells;
  const database = databaseId ? cells?.snapshot.pagesById[databaseId] : undefined;
  return useMemo<FilterPageMaker | null>(() => {
    if (!databaseId || database?.kind !== "database") return null;
    return {
      database,
      async make(title) {
        const environment = latest.current;
        if (!environment) return null;
        const request = capturePropertyCellRequest(environment.context);
        try {
          const made = await environment.operations.createRelatedRow(databaseId, { signal: request.signal }, environment.context);
          if (!made || request.signal.aborted) return null;
          // Its name goes behind; the page is chosen as it is made.
          void Promise.resolve(environment.operations.renameRelatedRow(made.id, title, (latest.current ?? environment).context)).catch(() => undefined);
          return made.id;
        } finally {
          request.abort();
        }
      },
    };
  }, [database, databaseId]);
}
