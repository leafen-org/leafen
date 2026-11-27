import { createContext, useContext } from "react";

/**
 * A view whose pages are a preview to look at (NativeDatabaseViewMount
 * `preview`, a layout builder's tab): its rows take no press, hover or
 * choosing, as Notion's (seen 2026-11-12), while its head and tools work.
 */
export const DatabaseRowsPreviewContext = createContext(false);

export function useDatabaseRowsPreview() {
  return useContext(DatabaseRowsPreviewContext);
}

/**
 * A preview whose columns are still set from their heads: a layout builder's
 * tab of a source the database reads, its head's menu open to every column
 * setting, as Notion's (seen 2026-11-12: a property renamed there waits with
 * the layout's draft).
 */
export const DatabasePreviewColumnsContext = createContext(false);

export function useDatabasePreviewColumns() {
  return useContext(DatabasePreviewColumnsContext);
}

/** Whether a view's column heads are read only: with its rows, but in a preview whose columns are set. */
export function useColumnHeadsReadOnly(readOnly: boolean) {
  const preview = useDatabaseRowsPreview();
  const columns = useDatabasePreviewColumns();
  return readOnly && !(preview && columns);
}
