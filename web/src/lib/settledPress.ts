import { useCallback, useRef } from "react";

// How long a button that just took its place lets a press go by.
const SETTLE_MS = 1_000;

/**
 * Whether a button that just came where another stood — a job's "취소" turned
 * into "다운로드" as it finished, "에이전트 연결" into "새 코드" — has been there
 * long enough to take a press. Until then a press meant for what stood there
 * would land on it: a cancel became a download, a second press made the code
 * just shown anew (2026-11-06). `identity` names what stands in the place; the
 * answer is asked as the press comes, so nothing is drawn differently.
 */
export function useSettledPress(identity: unknown, ms = SETTLE_MS): () => boolean {
  const since = useRef(0);
  const shown = useRef<{ identity: unknown } | null>(null);
  if (!shown.current || shown.current.identity !== identity) {
    shown.current = { identity };
    since.current = performance.now();
  }
  return useCallback(() => performance.now() - since.current >= ms, [ms]);
}
