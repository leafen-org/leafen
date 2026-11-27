import { createContext, useContext } from "react";

/**
 * Whether a sheet opens inside the window in the middle of a touch screen
 * wider than a phone (DatabaseTouchMenuWindow), as Notion's iPad goes on in
 * its filter and sort window (seen 2026-11-15 in iPad Safari at 834): a
 * whole-screen pick there (a property, a filter's value) is a window of its
 * own in the same place over it, its rows the window's, while a choice of a
 * few still rises from the bottom ("A → Z로 정렬", a filter's condition).
 */
export const SheetWindowContext = createContext(false);

export function useInSheetWindow() {
  return useContext(SheetWindowContext);
}
