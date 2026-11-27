import { useRef } from "react";

/**
 * Keeps a new group shown though empty, once its option is saved. The option
 * shows at once and the view draws again, so the keeping is written as the
 * view is then — its settings and its writer's context of that moment — not
 * as it was at the press: that writer no longer shows what it saves, and a
 * second new group in a row would conflict with the first (seen 2026-11-01).
 */
export function useKeepNewGroup(keep: (key: string) => void) {
  const latest = useRef(keep);
  latest.current = keep;
  return (key: string) => latest.current(key);
}
