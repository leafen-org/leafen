import { useCallback, useEffect, useState } from "react";
import { acknowledgeDeadlineNotice, readDeadlineNoticeStates } from "@/lib/deadlines";

/**
 * Whether the person has acknowledged the deadline notices the inbox's rows
 * are about (the server's deadline_notices.go), which the rows acknowledge.
 * A notice reads as unknown until the server says, and one its deadline's
 * change took away (null) offers nothing.
 */
export function useDeadlineAcknowledgements(noticeIds: string[]) {
  const [acknowledged, setAcknowledged] = useState<Record<string, boolean | null>>({});
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState("");
  const key = [...new Set(noticeIds)].slice(0, 200).join(",");

  useEffect(() => {
    if (!key) return;
    const controller = new AbortController();
    const ids = key.split(",");
    readDeadlineNoticeStates(ids, controller.signal).then((states) => {
      if (controller.signal.aborted) return;
      const known = new Set(states.acknowledged);
      const gone = new Set(states.gone);
      setAcknowledged((current) => ({
        ...current,
        ...Object.fromEntries(ids.map((id) => [id, gone.has(id) ? null : current[id] || known.has(id)])),
      }));
    }).catch(() => undefined);
    return () => controller.abort();
  }, [key]);

  const acknowledge = useCallback(async (noticeId: string) => {
    if (busy) return false;
    setBusy(noticeId);
    setFailed("");
    try {
      await acknowledgeDeadlineNotice(noticeId);
      setAcknowledged((current) => ({ ...current, [noticeId]: true }));
      return true;
    } catch {
      setFailed(noticeId);
      return false;
    } finally {
      setBusy("");
    }
  }, [busy]);

  return { acknowledged, acknowledge, busy, failed };
}
