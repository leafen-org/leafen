import { useEffect, useRef } from "react";
import { ROW_FILLED_FROM_TEMPLATE_EVENT } from "@/lib/databaseTemplateRequests";
import { onRowChanged } from "@/lib/rowChangeWords";
import { followAccess } from "@/lib/workspaceChangeCheck";

/**
 * A row's open page is read again when what it shows changed elsewhere: its
 * values — its database, another tab or person, the API, an automation, a
 * two-way relation's other side, or the rows its rollups read — and its lock
 * (lib/rowChangeWords.ts), or the server filled it from a template
 * (applyDatabaseRowTemplate). It is read unless its own writes are on their way
 * (`writing`), after which it is read anyway. What the
 * person may do there — a share of it or its database raised or dropped —
 * the workspace's change follow says so (lib/workspaceChangeCheck.ts), and
 * it is read at once, so it turns editable or read-only as an open page does,
 * without a reload (2026-11-15: a guest's row page open whole stayed
 * read-only after its database was raised to "편집 허용", until reloaded).
 */
export function useRowPageReadAgain(rowId: string, read: () => void, writing: () => boolean) {
  const latest = useRef({ read, writing });
  latest.current = { read, writing };
  useEffect(() => {
    const changed = () => { if (!latest.current.writing()) latest.current.read(); };
    const filled = (event: Event) => { if ((event as CustomEvent<string>).detail === rowId) changed(); };
    const stopRows = onRowChanged(rowId, changed);
    const stopAccess = followAccess(() => latest.current.read());
    window.addEventListener(ROW_FILLED_FROM_TEMPLATE_EVENT, filled);
    return () => { stopRows(); stopAccess(); window.removeEventListener(ROW_FILLED_FROM_TEMPLATE_EVENT, filled); };
  }, [rowId]);
}
