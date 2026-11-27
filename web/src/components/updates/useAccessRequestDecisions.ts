import { useCallback, useEffect, useState } from "react";
import { api, LeafenApiError } from "@/api";
import { accessRequestBounds, accessRequestErrorCodes, type AccessRequest, type AccessRequestDecisionInput } from "@/accessRequestWire.generated";
import { isPageBusyRefusal } from "@/lib/pageBusy";
import { userErrorText } from "@/lib/settingsErrors";
import type { Session } from "@/types";

/**
 * The requests the inbox's rows are about, as they stand — waiting ones the
 * rows approve or decline, decided ones that say who decided them and when —
 * as Notion's inbox does (seen 2026-11-09).
 */
export function useAccessRequestDecisions({ requestIds, session, workspaceId }: {
  requestIds: string[];
  session: Session;
  workspaceId: string;
}) {
  const [records, setRecords] = useState<Record<string, AccessRequest>>({});
  // A decision the server refused, which leaves the request waiting and its row says so: why, for a page an
  // operation holds a while ("" for any other refusal, said in general).
  const [failed, setFailed] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const key = requestIds.join(",");

  useEffect(() => {
    setLoadFailed(false);
    if (!key || !workspaceId) {
      setRecords({});
      return;
    }
    const controller = new AbortController();
    const ids = key.split(",");
    const chunks: string[][] = [];
    for (let start = 0; start < ids.length; start += accessRequestBounds.listWindow) {
      chunks.push(ids.slice(start, start + accessRequestBounds.listWindow));
    }
    Promise.all(chunks.map((chunk) => api.readAccessRequests(session, workspaceId, chunk, controller.signal))).then((lists) => {
      if (controller.signal.aborted) return;
      setRecords(Object.fromEntries(lists.flatMap((list) => list.requests).map((request) => [request.id, request])));
    }).catch(() => {
      // Without the requests the rows have no Approve or Decline; they say so.
      if (!controller.signal.aborted) setLoadFailed(true);
    });
    return () => controller.abort();
  }, [attempt, key, session, workspaceId]);

  /** Approves (with the role chosen) or declines; the request as it then stands, or null when it wasn't. */
  const decide = useCallback(async (requestId: string, approve: boolean, role?: AccessRequestDecisionInput["role"]) => {
    if (busy) return null;
    setBusy(requestId);
    setFailed(({ [requestId]: _earlier, ...rest }) => rest);
    let result: AccessRequest | null = null;
    try {
      result = await api.decideAccessRequest(session, requestId, approve, role);
      const decided = result;
      setRecords((current) => ({ ...current, [requestId]: decided }));
    } catch (error) {
      // Someone else decided it meanwhile: it reads again, saying who.
      if (error instanceof LeafenApiError && error.code === accessRequestErrorCodes.decided) setAttempt((current) => current + 1);
      else setFailed((current) => ({ ...current, [requestId]: isPageBusyRefusal(error) ? userErrorText(error, "") : "" }));
    }
    setBusy("");
    return result;
  }, [busy, session]);

  const retry = useCallback(() => setAttempt((current) => current + 1), []);
  return { busy, decide, failed, loadFailed, records, retry };
}
