import { createContext, useContext } from "react";

/**
 * Opens the "다중 출처 필터링" window of the dashboard filter whose condition
 * is open, from its "⋯" (Notion's, seen 2026-11-17: "필터 제거" and "⚙ 다중
 * 출처 필터링" where another filter has "고급 필터에 추가"); null elsewhere.
 */
export const MultiSourceEditContext = createContext<(() => void) | null>(null);

export function useMultiSourceEdit() {
  return useContext(MultiSourceEditContext);
}
