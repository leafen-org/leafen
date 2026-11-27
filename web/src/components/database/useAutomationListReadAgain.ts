import { useEffect, useRef, useState, type RefObject } from "react";
import type { DatabaseAutomationService } from "@/databaseAutomationApi";
import type { DatabaseAutomationDefinition } from "@/lib/types";
import { onAutomationRevision } from "@/lib/automationRevisionEvents";
import { useReadAgainAfterUncertainWrites } from "@/lib/uncertainWrites";

/**
 * An open automations' list read again, leaving one being edited as it is:
 * once the server answers after a write that went without its answer and
 * may have been taken (lib/uncertainWrites.ts), and as the page's room tells
 * of a change made elsewhere — one made, turned off or deleted on another
 * device shows in the list at once, as Notion's open phone list showed
 * "중단됨" within seconds of the switch on a desktop (seen 2026-11-18,
 * lib/automationRevisionEvents.ts). Nothing is taken while a write of its
 * own is on its way (`busy`).
 */
export function useAutomationListReadAgain(service: DatabaseAutomationService, busy: RefObject<boolean>,
  take: (automations: DatabaseAutomationDefinition[]) => void) {
  const readAgain = useReadAgainAfterUncertainWrites();
  const [told, setTold] = useState(0);
  const takeRef = useRef(take);
  takeRef.current = take;
  const pageId = service.pageId;
  useEffect(() => pageId ? onAutomationRevision(pageId, () => setTold((value) => value + 1)) : undefined, [pageId]);
  useEffect(() => {
    if (!readAgain && !told) return undefined;
    const controller = new AbortController();
    void service.list(controller.signal).then((result) => {
      if (controller.signal.aborted || busy.current) return;
      takeRef.current(result);
    }, () => undefined);
    return () => controller.abort();
  }, [busy, readAgain, service, told]);
}
