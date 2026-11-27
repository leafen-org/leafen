import { createContext } from "react";

/**
 * Whether a row is still among the rows its table is drawn from, whatever
 * its view's filter keeps: a row trashed, archived or deleted is not
 * (useHeldCellEditor). None where the table has no such list.
 */
export const HeldRowSourceContext = createContext<((rowId: string) => boolean) | null>(null);
