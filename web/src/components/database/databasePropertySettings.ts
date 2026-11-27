import { createContext, useContext } from "react";

/**
 * Opens the view settings at one property's "속성 편집", as a status cell's
 * picker does in Notion (seen 2026-10-10); none outside a database view.
 */
export const DatabasePropertySettingsContext = createContext<((propertyId: string) => void) | null>(null);

export function useDatabasePropertySettings() {
  return useContext(DatabasePropertySettingsContext);
}
