import { useEffect, useState } from "react";
import type { DatabaseRowMoveTarget } from "./DatabaseRowMenu";

export type DatabaseRowMoveTargetSearch = (query: string, signal: AbortSignal) => Promise<DatabaseRowMoveTarget[]>;

/**
 * The places a row can move to: the databases on its own page, then the
 * workspace's other databases and pages that match what the mover types.
 */
export function useDatabaseRowMoveTargets(
  local: readonly DatabaseRowMoveTarget[],
  search: DatabaseRowMoveTargetSearch | undefined,
  open: boolean,
) {
  const [query, setQuery] = useState("");
  const [remote, setRemote] = useState<DatabaseRowMoveTarget[]>([]);
  useEffect(() => {
    if (!open || !search) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      search(query.trim(), controller.signal).then(
        (found) => { if (!controller.signal.aborted) setRemote(found); },
        () => { if (!controller.signal.aborted) setRemote([]); },
      );
    }, 150);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [open, query, search]);
  const needle = query.trim().toLocaleLowerCase();
  const seen = new Set<string>();
  const targets = [...local.filter((target) => !needle || target.title.toLocaleLowerCase().includes(needle)), ...remote]
    .filter((target) => {
      const key = `${target.kind ?? "database"}:${target.id}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  return { query, setQuery, targets };
}
