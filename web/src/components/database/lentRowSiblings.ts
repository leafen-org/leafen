import { useEffect, useMemo, useSyncExternalStore } from "react";
import type { RowPeekSiblings } from "./rowPeekSiblingKeys";

/**
 * A view that opens its rows its own way — "내 작업", Home's "내 작업" —
 * opens them in the app's page peek, not its own (useDatabaseViewController
 * `opensRowsItself`), yet the peek steps through the view's rows as the
 * view shows them: the view lends its order here while it is on the screen,
 * and the peek showing one of its rows reads it (AuthenticatedPagePeek).
 */
export type LentRowOrder = {
  /** The rows before and after one, as the view shows them; none for a row not its own. */
  siblingsOf(rowId: string): { previous?: string; next?: string } | null;
  /** Opens a row its own way, as a press on it does. */
  open(rowId: string): void;
};

let lent: LentRowOrder | null = null;
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
const notify = () => listeners.forEach((listener) => listener());

/** Lends a view's order while it is mounted; the last one lent stands, as the view on the screen. */
export function useLendRowOrder(order: LentRowOrder | null) {
  useEffect(() => {
    if (!order) return;
    lent = order;
    notify();
    return () => {
      if (lent !== order) return;
      lent = null;
      notify();
    };
  }, [order]);
}

/** The siblings a peek showing `rowId` steps to, from the view that lent its order; none while no view holds the row. */
export function useLentRowSiblings(rowId: string | null): RowPeekSiblings | null {
  const order = useSyncExternalStore(subscribe, () => lent, () => null);
  return useMemo(() => {
    const siblings = order && rowId ? order.siblingsOf(rowId) : null;
    return order && siblings ? { ...siblings, onOpen: order.open } : null;
  }, [order, rowId]);
}
