import { createContext, useContext } from "react";

/**
 * Inside a customize list — the sidebar's customize mode, a preset's editor
 * in the settings — a row's menu opens from a ⚙ sliders button and leaves
 * removing it to the row's ×, as Notion's (seen 2026-11-09).
 */
export const SidebarCustomizingContext = createContext(false);

export function useSidebarCustomizing() {
  return useContext(SidebarCustomizingContext);
}
