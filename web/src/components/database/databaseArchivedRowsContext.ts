import { createContext, useContext } from "react";
import { DatabaseRowsAddableContext } from "./databaseNewRowControl";

// A view shows its database's archived rows in place of the others while
// "보관된 페이지 보기" is on for it, as Notion's (seen 2026-10-11/12): its
// settings item turns to "보관된 페이지 보기에서 나가기", and nothing new is
// made in it.
export type DatabaseArchivedRowsSurface = {
  viewing: boolean;
  enter(): void;
  exit(): void;
};

export const DatabaseArchivedRowsContext = createContext<DatabaseArchivedRowsSurface | null>(null);

/** Whether the view shows its archived rows, where no new page is made. */
export function useViewingArchivedRows() {
  return useContext(DatabaseArchivedRowsContext)?.viewing === true;
}

/**
 * Whether a view offers a new row at all — every layout's "+ 새 …" line,
 * a column's, group's or lane's "+", a row's "+", a calendar day's "+", a
 * timeline's drawn bar and its no-results box alike: not while it shows its
 * archived rows, nor where its page says its rows take none ("내 작업" with
 * no source the reader may add to, as Notion's tracker they may only read,
 * seen 2026-11-08). Whether the reader may edit at all is the view's own.
 */
export function useViewOffersNewRows() {
  const addable = useContext(DatabaseRowsAddableContext);
  const archived = useViewingArchivedRows();
  return addable && !archived;
}
