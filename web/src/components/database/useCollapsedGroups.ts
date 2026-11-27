import { useCallback, useEffect, useState, type Dispatch, type SetStateAction } from "react";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";

const KEY = "leafen:v2:collapsed-groups";

function stored(): Record<string, string[]> {
  const value = parseStored(personValue(KEY, { previous: [KEY] }));
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, string[]> : {};
}

/**
 * A view's folded groups, kept for this viewer alone across visits, as
 * Notion keeps a folded table group folded after a reload (seen
 * 2026-11-05). Without storage the groups still fold for this visit.
 */
export function useCollapsedGroups(viewId: string): [ReadonlySet<string>, Dispatch<SetStateAction<ReadonlySet<string>>>] {
  const read = useCallback(() => new Set<string>(Array.isArray(stored()[viewId]) ? stored()[viewId] : []), [viewId]);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(read);
  // Read again for another view; the same groups kept as they are, which drew the whole view again as it opened.
  useEffect(() => setCollapsed((current) => {
    const next = read();
    return next.size === current.size && [...next].every((key) => current.has(key)) ? current : next;
  }), [read]);
  const change: Dispatch<SetStateAction<ReadonlySet<string>>> = useCallback((next) => {
    setCollapsed((current) => {
      const value = typeof next === "function" ? next(current) : next;
      const all = stored();
      if (value.size) all[viewId] = [...value].slice(0, 200); else delete all[viewId];
      const views = Object.keys(all);
      for (const old of views.slice(0, Math.max(0, views.length - 200))) delete all[old];
      storePersonValue(KEY, JSON.stringify(all));
      return value;
    });
  }, [viewId]);
  return [collapsed, change];
}
