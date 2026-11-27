import { createContext, useContext } from "react";

/** The properties a compact card shows on lines of their own ("전체 줄 표시"). */
export function compactFullLineIds(viewConfig: object | undefined): ReadonlySet<string> {
  const config = viewConfig as Record<string, unknown> | undefined;
  return new Set(config?.cardLayout === "compact" && Array.isArray(config.compactFullLineProperties)
    ? config.compactFullLineProperties.filter((id): id is string => typeof id === "string") : []);
}

const NONE: ReadonlySet<string> = new Set();

/** A board's or gallery's full-line properties, for its cards. */
export const CardFullLineContext = createContext<ReadonlySet<string>>(NONE);

export function useCardFullLine() {
  return useContext(CardFullLineContext);
}
