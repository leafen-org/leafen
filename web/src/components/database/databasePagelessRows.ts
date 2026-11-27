import { createContext } from "react";
import type { RowMenuFoot } from "./rowMenuFoot";

/**
 * A source whose rows are its people's pages, where its page says so:
 * People's (PeopleDatabaseView), opened as their profiles. As Notion's People
 * shows its owner (seen 2026-11-15 twice in a free test workspace), a row's
 * ⋮⋮ and its selection's "⋯" open a menu that adds it to the favourites,
 * edits its icon and the values its reader writes ("속성 편집 ›"), opens the
 * row ("다음에서 열기 ›") and copies its link, with the foot its page gives —
 * nothing that duplicates, moves, archives or trashes a person's page, or
 * comments on it; and its selection bar has buttons for those values alone
 * and no trash.
 */
export type DatabasePagelessRows = {
  /** The properties the reader writes on a row, none for one who writes none. */
  writable(rowId: string): ReadonlySet<string>;
  /** The row's address, which "링크 복사" copies. */
  link(rowId: string): string;
  /** The menu's foot: when the row was last edited and by whom, and its title's words and letters. */
  foot(rowId: string): RowMenuFoot | undefined;
};

export const DatabasePagelessRowsContext = createContext<DatabasePagelessRows | null>(null);
