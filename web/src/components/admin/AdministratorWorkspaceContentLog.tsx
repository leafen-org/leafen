import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AdministratorWorkspaceContentEvent, AdministratorWorkspaceContentLog as Log } from "@/administratorWire.generated";
import { userErrorText } from "@/lib/settingsErrors";
import { ReadFailed } from "../ReadFailed";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorWorkspaceContent.module.css";
import { displayDateTime } from "@/lib/displayDate";
import { titleWithClosedMentions } from "@/lib/closedMentions";

/**
 * What server administrators did in a workspace, newest first
 *: read by the
 * administrators in the server's workspace view and by the workspace's owner
 * in its settings, each with its own load.
 */
export function AdministratorWorkspaceContentLog({ load }: { load: (cursor?: string, signal?: AbortSignal) => Promise<Log> }) {
  const { t } = useTranslation("serverConsole");
  const [events, setEvents] = useState<AdministratorWorkspaceContentEvent[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  // A failed read and the window it was for, said with a way to read it again.
  const [error, setError] = useState<{ text: string; at?: string } | null>(null);

  async function more(next?: string, signal?: AbortSignal) {
    try {
      const page = await load(next, signal);
      setEvents((current) => next ? [...(current ?? []), ...page.items] : page.items);
      setCursor(page.nextCursor);
    } catch (cause) {
      if (!signal?.aborted) setError({ text: userErrorText(cause, t("workspaceContent.loadFailed")), at: next });
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    void more(undefined, controller.signal);
    return () => controller.abort();
    // The log loads once for whatever load reads.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load]);

  return (
    <div className={own.list}>
      {(events ?? []).map((event) => (
        <div key={event.id} className={own.item} data-admin-log={event.action}>
          <span className={own.itemText}>
            <strong>{[event.actorName, t(`workspaceContent.log.actions.${event.action}`), titleWithClosedMentions(event.targetTitle)].filter(Boolean).join(" · ")}</strong>
            <span>{displayDateTime(event.occurredAt)}</span>
          </span>
        </div>
      ))}
      {events && !events.length ? <div className={own.hint}>{t("workspaceContent.log.empty")}</div> : null}
      {cursor ? <button type="button" className={styles.secondaryButton} onClick={() => void more(cursor)}>{t("workspaceContent.more")}</button> : null}
      {error ? <ReadFailed message={error.text} onRetry={() => { setError(null); void more(error.at); }} /> : null}
    </div>
  );
}
