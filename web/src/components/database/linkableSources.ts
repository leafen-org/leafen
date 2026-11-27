import { useEffect, useRef, useState } from "react";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { readSourcePages } from "@/lib/databaseSourceReading";
import type { EditorDatabaseStartActions } from "@/components/editor/editorRuntimeTypes";

/**
 * Notion's "기존 데이터 소스 연결하기" (seen 2026-11-11 at 1440px, from a
 * view among others and from "새 데이터베이스"): five sources, then "⋯ N개
 * 더 보기" counting the rest of the hundred it holds, twenty more a press;
 * past the hundred a search finds them.
 */
export const EXISTING_FIRST_SHOWN = 5, EXISTING_MORE_SHOWN = 20, EXISTING_COUNTED = 100;
/** A search there shows the first twenty it finds, then the same "N개 더 보기" (seen 2026-11-11). */
export const EXISTING_SEARCH_FIRST_SHOWN = 20;
/**
 * A waiting view's "새 데이터베이스" (seen 2026-11-02, 2026-11-07,
 * 2026-11-10 and 2026-11-11) and a new chart's (seen 2026-11-18 twice):
 * three sources before "⋯ 자세히 표시", which shows five and no more,
 * searched or not.
 */
export const LINK_FIRST_SHOWN = 3, LINK_MOST_SHOWN = 5;

/**
 * How many of what it read a source window shows, as Notion's (seen
 * 2026-11-18 in Chrome 1440 in the view settings' "데이터베이스", "데이터
 * 소스 관리" › "기존 데이터 소스 연결하기", a layout tab's "+" and "관계형
 * 대상"): five, a search's first twenty, then "N개 더 보기" twenty past what
 * shows a press — a reach kept while the window stays open, through another
 * search or none (searched 20, pressed 40, searched again 40, emptied 40 and
 * "60개 더 보기"; pressed unsearched 25, then searched 25; searched and
 * emptied unpressed, five again). `reset` starts it over, as a window back at
 * its start does.
 */
export function useSourceListLimit(query: string) {
  const [reached, setReached] = useState(EXISTING_FIRST_SHOWN);
  const limit = query.trim() ? Math.max(reached, EXISTING_SEARCH_FIRST_SHOWN) : reached;
  return { limit, more: () => setReached(limit + EXISTING_MORE_SHOWN), reset: () => setReached(EXISTING_FIRST_SHOWN) };
}

/**
 * What a source window shows under "기존 데이터 소스" of what it read —
 * `read`, the keys of those, in the server's order, up to the hundred —
 * `listed` those it lists there, in that order, and `ahead` how many it
 * counts first though it lists them elsewhere or not at all (a database's
 * own sources and those it links: seen 2026-11-15, 4 by one source, 3 by
 * two; 2026-11-18, the same from "데이터 소스 관리"). Unsearched, the first `limit`
 * of the hundred less those ahead; searched, those among the first `limit` it
 * found, whatever else it lists them under (seen 2026-11-18: "DB", which the
 * view's own source answers too, 18 and "64개 더 보기"). `hidden` counts the
 * rest "N개 더 보기" names.
 */
export function existingSourcesShown<T>(read: readonly string[], listed: readonly T[], { searched, limit, ahead = 0, key }: {
  searched: boolean; limit: number; ahead?: number; key(item: T): string;
}) {
  if (searched) {
    const first = new Set(read.slice(0, limit));
    const shown = listed.filter((item) => first.has(key(item)));
    return { shown, hidden: listed.length - shown.length };
  }
  const counted = listed.slice(0, Math.max(0, EXISTING_COUNTED - ahead));
  const shown = counted.slice(0, Math.max(0, limit - ahead));
  return { shown, hidden: counted.length - shown.length };
}

/**
 * The sources a link window lists, read again 120ms after its search last
 * changed, the view's own empty source left out, the server's order (the
 * reader's latest visits first, a search's best found first,
 * database_container_picker.go) with `first` — those a block links, its
 * "추천" — ahead, however many there are. With `upTo`, searched or not, it
 * reads that many at once, a page after another, so a count of those it
 * hides is of all it holds, not of the first page; without, it reads a
 * page, `more` the next.
 */
export function useLinkableSources(load: EditorDatabaseStartActions["loadDatabaseSources"], query: string, own: string, upTo?: number, first?: readonly string[]) {
  const [sources, setSources] = useState<DatabaseContainerSource[]>([]);
  const [cursor, setCursor] = useState<string>(), [loading, setLoading] = useState(true), [failed, setFailed] = useState(false);
  // The search the sources shown were read for (none before the first read).
  const [shownFor, setShownFor] = useState<string>();
  const reader = useRef<AbortController | null>(null);
  async function read(after?: string) {
    reader.current?.abort(); const controller = new AbortController(); reader.current = controller;
    setLoading(true); setFailed(false);
    const searched = query.trim(), want = after ? undefined : upTo;
    try {
      const { items, nextCursor } = await readSourcePages((cursor, limit) => load(searched, cursor, controller.signal, limit, first),
        { upTo: want, after, keep: (item) => item.dataSourceId !== own });
      if (controller.signal.aborted) return;
      setSources((current) => [...(after ? current : []), ...items]);
      setCursor(want === undefined ? nextCursor : undefined);
      if (!after) setShownFor(query);
    } catch { if (!controller.signal.aborted) setFailed(true); }
    finally { if (reader.current === controller && !controller.signal.aborted) setLoading(false); }
  }
  useEffect(() => {
    const timer = window.setTimeout(() => void read(), 120);
    return () => { window.clearTimeout(timer); reader.current?.abort(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);
  return { sources, cursor, loading, failed, shownFor, more: () => { if (cursor) void read(cursor); } };
}
