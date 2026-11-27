"use client";

import { useEffect, useRef, useState } from "react";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import { ROUTE_CHANGE_EVENT } from "@/lib/router";
import type { Session } from "@/types";
import { databaseViewUrlState } from "./databaseViewModel";

const NONE: readonly string[] = [];

type AddressRow = { id: string; dataSourceId: string; trashedAt?: string | null };

// The address's row as read for the page's views: one read for all of them,
// kept while a view is open at that address and read again for another.
let asked: { key: string; row: Promise<AddressRow | null> } | null = null;
let following = 0;

function addressRow(session: Session, rowId: string) {
  const key = `${session.user.id}\u0000${rowId}`;
  if (asked?.key !== key) {
    asked = { key, row: readDatabaseRowPage(session, rowId, { limit: 1 }).then(({ row }) => row, () => null) };
  }
  return asked.row;
}

/**
 * A row in trash is out of every view, so no view holds the row the trash
 * opens; the views of its database take it, as Notion's trash opens a
 * trashed database page (seen 2026-10-30). The address's row is read once
 * for the page's views when none of a view's rows, and no row open above
 * it, is that row; each view whose database it is takes it.
 */
export function useTrashedAddressRow(
  session: Session | null,
  dataSourceId: string,
  rows: readonly { id: string }[],
  enabled: boolean,
) {
  const [trashed, setTrashed] = useState(NONE);
  const rowsRef = useRef(rows);
  rowsRef.current = rows;
  useEffect(() => {
    if (!session || !enabled || typeof window === "undefined") return;
    let open = true;
    let checked = "";
    following += 1;
    function check() {
      const { rowId, openAbove } = databaseViewUrlState(new URL(window.location.href));
      if (!rowId || rowId === checked || openAbove.length > 0 || rowsRef.current.some(({ id }) => id === rowId)) return;
      checked = rowId;
      void addressRow(session!, rowId).then((row) => {
        if (open && row?.trashedAt && row.dataSourceId === dataSourceId) setTrashed([row.id]);
      });
    }
    check();
    window.addEventListener("popstate", check);
    window.addEventListener(ROUTE_CHANGE_EVENT, check);
    return () => {
      open = false;
      following -= 1;
      if (following === 0) asked = null;
      window.removeEventListener("popstate", check);
      window.removeEventListener(ROUTE_CHANGE_EVENT, check);
    };
  }, [dataSourceId, enabled, session]);
  return trashed;
}
