import { createContext, useEffect, useMemo, useSyncExternalStore } from "react";

/**
 * One database view opens the database page a URL names, as Notion opens one
 * page preview: the view the URL names, else the first view on the page that
 * holds the page. A view inside a database page's own preview doesn't open
 * that page again.
 */

/** The database pages open above a view, which its views don't open again. */
export const OpenDatabasePagesContext = createContext<readonly string[]>([]);

/**
 * Where a view's rows open whatever its own choice, while the place showing
 * it decides: Notion's home database widget opens every row, a table's or a
 * list's, ⌥-pressed too, in the middle over the home (seen 2026-11-14; its
 * view settings have no "페이지 열기 방식"). None elsewhere, nor inside a
 * row opened from there.
 */
export const RowsOpenInContext = createContext<"center" | undefined>(undefined);

/**
 * Whether the databases drawn here are in a side peek — the body of a page
 * or row opened beside its page, or a database opened alone there — where
 * a row's "다음에서 열기 ›" also offers its full page (useDatabaseRowViewMenu).
 */
export const SidePeekContext = createContext(false);

type RowOpenerOffer = { rowId: string; preferred: boolean };

// Offers in the order the views first made them, which is the page's order
// as the views come in.
const offers = new Map<symbol, RowOpenerOffer>();
const listeners = new Set<() => void>();

/** Offers a view to open a page, or withdraws it, and tells the other views. */
function offerRowOpener(key: symbol, offer: RowOpenerOffer | null) {
  const current = offers.get(key);
  if (offer ? current?.rowId === offer.rowId && current.preferred === offer.preferred : !current) return;
  if (offer) offers.set(key, offer);
  else offers.delete(key);
  for (const listener of [...listeners]) listener();
}

/**
 * A view's place among the views that open a page, while the view is on the
 * screen; call it before the effects that offer. A view that has gone offers
 * nothing: its going tells the views that listen, itself among them until
 * its own effects are gone, and an offer it made then would stay with no view
 * to open the page, holding it from every view after it (2026-11-11: a
 * reader's page drew its body again once its access was read, and the item
 * its address named opened nowhere).
 */
export function useRowOpener() {
  const opener = useMemo(() => {
    const key = Symbol("database-view-row-opener");
    let present = true;
    return {
      offer(offer: RowOpenerOffer | null) {
        if (present || !offer) offerRowOpener(key, offer);
      },
      isOpener: () => isRowOpener(key),
      enter() {
        present = true;
      },
      leave() {
        present = false;
        offerRowOpener(key, null);
      },
    };
  }, []);
  useEffect(() => {
    opener.enter();
    return () => opener.leave();
  }, [opener]);
  return opener;
}

/** Reports whether a view offering to open a page is the one that opens it. */
function isRowOpener(key: symbol) {
  const mine = offers.get(key);
  if (!mine) return false;
  let first: symbol | undefined;
  for (const [other, offer] of offers) {
    if (offer.rowId !== mine.rowId) continue;
    if (offer.preferred) return other === key;
    first ??= other;
  }
  return first === key;
}

/** Tells the views an opened row changed in the address, so each reads it again. */
export function announceRowSelection() {
  for (const listener of [...listeners]) listener();
}

export function subscribeRowOpeners(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

let version = 0;
listeners.add(() => { version += 1; });

/**
 * Whether a view on the screen opens this page, so the peek that opens any
 * page the address names leaves it to that view; rereads as views change.
 */
export function useRowHasOpener(rowId: string | null) {
  useSyncExternalStore(subscribeRowOpeners, () => version, () => 0);
  return rowId ? [...offers.values()].some((offer) => offer.rowId === rowId) : false;
}
