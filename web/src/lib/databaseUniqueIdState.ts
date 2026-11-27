import { useEffect, useState } from "react";
import { request } from "@/apiTransport";
import { databaseDataSourceOperations, type DatabaseUniqueIDState } from "@/databaseDataSourceWire.generated";
import { loadSession } from "@/lib/authSessionStore";

/**
 * What a data source's ID property may be, as the server keeps Notion's rules
 * (database_unique_id_rules.go): whether it has an ID property besides the
 * one named, one in Deleted properties too, and whether a prefix is another
 * data source's in the workspace. Null when it can't be asked.
 */
export async function readDatabaseUniqueIdState(
  dataSourceId: string,
  ask: { property?: string; prefix?: string },
  signal?: AbortSignal,
): Promise<DatabaseUniqueIDState | null> {
  const session = loadSession();
  if (!session || !dataSourceId) return null;
  const query = new URLSearchParams();
  if (ask.property) query.set("property", ask.property);
  if (ask.prefix) query.set("prefix", ask.prefix);
  const path = databaseDataSourceOperations.uniqueIdState.path.replace("{dataSourceId}", encodeURIComponent(dataSourceId));
  const search = query.toString();
  try {
    return await request<DatabaseUniqueIDState>(search ? `${path}?${search}` : path,
      { method: databaseDataSourceOperations.uniqueIdState.method, signal }, session);
  } catch {
    return null;
  }
}

/**
 * The prefix typed, once the server says another data source of the
 * workspace has it, as Notion's editor tells while it is typed (seen
 * 2026-11-11); null while it is free or not asked about yet.
 */
export function useUniqueIdPrefixTaken(dataSourceId: string, propertyId: string, prefix: string, ask: boolean) {
  const [taken, setTaken] = useState<string | null>(null);
  useEffect(() => {
    if (!ask || !prefix) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void readDatabaseUniqueIdState(dataSourceId, { property: propertyId, prefix }, controller.signal).then((state) => {
        if (!controller.signal.aborted) setTaken(state?.prefixTaken ? prefix : null);
      });
    }, 150);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [ask, dataSourceId, prefix, propertyId]);
  return ask && taken === prefix ? taken : null;
}

/**
 * The prefix Notion's editor offers an ID property with none, from its
 * database's title (seen 2026-11-11): the first three characters of a title
 * of one word, else the first letter of each of its first three words, words
 * being runs of Latin letters or of digits ("W412 H" offers "W4H", "Q3 OKRs
 * 2026" "Q3O", "my-team db" "MTD"), in capitals; "TSK" for a title with
 * neither ("작업 목록", none).
 */
export function idPrefixFromTitle(title: string): string {
  const words = title.match(/[A-Za-z]+|[0-9]+/g) ?? [];
  if (!words.length) return "TSK";
  const prefix = words.length === 1 ? words[0].slice(0, 3) : words.slice(0, 3).map((word) => word[0]).join("");
  return prefix.toUpperCase();
}

/**
 * The prefix offered from a database's title, with 1, 2, … after it while
 * another data source of the workspace has it ("TT1", then "TT2"), as
 * Notion's (seen 2026-11-11); null while it is worked out, or with no title
 * to work from.
 */
export function useSuggestedIdPrefix(dataSourceId: string, propertyId: string, title: string | undefined, ask: boolean) {
  const [suggested, setSuggested] = useState<{ from: string; prefix: string } | null>(null);
  const key = `${dataSourceId}\n${propertyId}\n${title ?? ""}`;
  useEffect(() => {
    if (!ask || title === undefined) return;
    const controller = new AbortController();
    const base = idPrefixFromTitle(title);
    void (async () => {
      for (let attempt = 0; attempt < 10; attempt += 1) {
        const prefix = attempt ? `${base}${attempt}` : base;
        const state = await readDatabaseUniqueIdState(dataSourceId, { property: propertyId, prefix }, controller.signal);
        if (controller.signal.aborted) return;
        // One the server can't be asked about is offered as it is.
        if (!state?.prefixTaken) {
          setSuggested({ from: key, prefix });
          return;
        }
      }
    })();
    return () => controller.abort();
  }, [ask, dataSourceId, key, propertyId, title]);
  return ask && suggested?.from === key ? suggested.prefix : null;
}

/** A data source a new property is added to, and whether it shows an ID property. */
export type UniqueIdSource = { dataSourceId: string; has: boolean };

/**
 * Whether a data source has an ID property already, so that a new one can't
 * be added, as Notion's "+" dims "ID" (seen 2026-11-11): one the app shows
 * says so at once, one in Deleted properties once the server answers.
 */
export function useDataSourceHasUniqueId(dataSourceId: string | undefined, shown: boolean, open: boolean) {
  const [deleted, setDeleted] = useState<{ source: string; has: boolean } | null>(null);
  useEffect(() => {
    if (!open || shown || !dataSourceId) return;
    const controller = new AbortController();
    void readDatabaseUniqueIdState(dataSourceId, {}, controller.signal).then((state) => {
      if (!controller.signal.aborted && state) setDeleted({ source: dataSourceId, has: state.otherUniqueId });
    });
    return () => controller.abort();
  }, [dataSourceId, open, shown]);
  return shown || (deleted !== null && deleted.source === dataSourceId && deleted.has);
}
