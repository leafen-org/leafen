import { useEffect, useRef, useState } from "react";
import type { DatabaseToolbarTaskFeatureWire } from "@/databaseToolbarConfigWire.generated";
import type { GraphProperty } from "@/contentGraph";
import type { createDatabaseToolbarConfigService } from "@/lib/databaseToolbarConfigService";
import { onTemplateDraftSent } from "@/lib/templateDraftSend";
import { aborted, useSettingsReadUnanswered, useSettingsWriteUnanswered } from "./useSettingsWriteUnanswered";

/**
 * Reads a database's toolbar settings for its toolbar (NativeDatabaseToolbarMount):
 * once the database may be written, kept while the page is locked and
 * unlocked again (2026-11-13: unlocking a page read its databases' settings
 * once more, three reads for one press), and again as a template's editor
 * sends its draft or the dependency pair leaves. A read the server didn't
 * answer goes again when it may answer, the first one too (a database opened
 * while the server was away); a change it didn't answer is said and read
 * again once it answers (useSettingsWriteUnanswered). Gives that check for
 * the changes.
 */
export function useToolbarSettingsRead({
  service, pageId, dataSourceId, readOnly, configurationReady, dependencies, properties, onReset, onError,
}: {
  service: ReturnType<typeof createDatabaseToolbarConfigService> | null;
  pageId: string;
  dataSourceId: string;
  readOnly: boolean;
  configurationReady: boolean;
  dependencies: DatabaseToolbarTaskFeatureWire["dependencies"] | undefined;
  properties: readonly GraphProperty[];
  /** Leaves the settings read for another database, or while it may not be written. */
  onReset(): void;
  onError(error: unknown): void;
}) {
  const latest = useRef({ onReset, onError });
  latest.current = { onReset, onError };
  // Whose settings were read, "" until a read of them is answered or under way.
  const readFor = useRef("");
  const [readTurn, setReadTurn] = useState(0);
  const readUnanswered = useSettingsReadUnanswered(() => {
    if (readFor.current) void service?.refresh(pageId, dataSourceId).catch(readUnanswered);
    else setReadTurn((turn) => turn + 1);
  });
  const unanswered = useSettingsWriteUnanswered(() => void service?.refresh(pageId, dataSourceId).catch(readUnanswered));
  useEffect(() => service && !readOnly ? onTemplateDraftSent(dataSourceId, () => void service.refresh(pageId, dataSourceId).catch(readUnanswered))
    : undefined, [dataSourceId, pageId, readOnly, readUnanswered, service]);

  useEffect(() => {
    const key = `${pageId}\u0000${dataSourceId}`;
    if (readFor.current !== key || !configurationReady) {
      readFor.current = "";
      service?.dispose();
      latest.current.onReset();
    }
    if (!service || !configurationReady || readOnly || readFor.current === key) return;
    readFor.current = key;
    let current = true;
    // Settings no longer there are a database gone from the page (a wiki
    // turned back into a page, 2026-11-10): its view leaves with the page's
    // next read, and nothing failed to save.
    void service.refresh(pageId, dataSourceId).catch((error) => {
      if (readFor.current === key) readFor.current = "";
      const gone = (error as { status?: unknown } | null)?.status === 404;
      if (current && !gone && !aborted(error) && !readUnanswered(error)) latest.current.onError(error);
    });
    return () => { current = false; };
  }, [configurationReady, dataSourceId, pageId, readOnly, readTurn, readUnanswered, service]);

  // Deleting a property of the dependency pair turns dependencies off on the
  // server: once the pair is gone from the database, the settings are read again.
  const dependencyPairGone = dependencies?.enabled === true
    && [dependencies.predecessorPropertyId, dependencies.successorPropertyId]
      .some((id) => typeof id === "string" && !properties.some((property) => property.id === id));
  useEffect(() => {
    if (dependencyPairGone && service && !readOnly) void service.refresh(pageId, dataSourceId).catch(readUnanswered);
  }, [dataSourceId, dependencyPairGone, pageId, readOnly, readUnanswered, service]);
  return unanswered;
}
