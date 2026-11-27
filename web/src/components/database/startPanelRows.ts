import { type KeyboardEvent, type PointerEvent, type RefObject, createContext, useLayoutEffect, useRef, useState } from "react";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { scrollKeyWalkedRow } from "@/lib/menuKeyScroll";
import { keysMovedHighlight, pointerTakenRow } from "@/lib/pointerHover";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/** The key a start panel's row of a source carries (`data-key`). */
export const sourceRowKey = (source: DatabaseContainerSource) => `${source.id}:${source.dataSourceId}`;

/** The row a start panel's list lights, for the rows drawn apart from it
 * (ExistingSources), and its check that the lit row is still there, which
 * they run as they draw. */
export const StartRowLit = createContext<{ lit: string | null; check(): void }>({ lit: null, check: () => {} });

/**
 * The rows of a start panel's list — its sources, "+ 비어 있는 새 데이터
 * 소스", "자세히 표시" or "N개 더 보기" — lit and pressed from its search
 * as Notion's (seen 2026-11-14 at 1440, "새 데이터베이스" of a waiting view
 * and "기존 데이터 소스 연결하기"): none lit as it opens, ↓ the first and ↑
 * the last; past an end round to the other ("round") or through no row lit
 * first ("throughNone"); Enter presses the lit row and does nothing with
 * none; the pointer lights the row it moves onto, after the arrows only
 * once it moves into another row (`pointerHover`). No keyboard ring: the
 * lit row is shaded as the pointer's is. A lit row that goes ("자세히 표시"
 * showing more) leaves the light on the row in its place. A row is any
 * enabled `[data-key]` in the list, in order; the list's element takes
 * `onPointerMove`.
 */
export function useStartPanelRows(listRef: RefObject<HTMLElement | null>, wrap: "round" | "throughNone") {
  const [lit, light] = useState<string | null>(null);
  const litAt = useRef(-1);
  const check = () => {
    if (lit === null) return;
    const keys = [...(listRef.current?.querySelectorAll<HTMLElement>("[data-key]") ?? [])].map((row) => row.dataset.key);
    const at = keys.indexOf(lit);
    if (at >= 0) litAt.current = at;
    else light(keys[litAt.current] ?? null);
  };
  useLayoutEffect(check);
  const rows = () => [...(listRef.current?.querySelectorAll<HTMLButtonElement>("[data-key]") ?? [])].filter((row) => !row.disabled);
  function onKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (imeComposing(event) || event.altKey || event.ctrlKey || event.metaKey) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const shown = rows(), keys = shown.map((row) => row.dataset.key ?? "");
      if (!keys.length) return;
      const at = lit === null ? -1 : keys.indexOf(lit), down = event.key === "ArrowDown";
      const end = down ? keys.length - 1 : 0, start = down ? 0 : keys.length - 1;
      const next = at < 0 ? keys[start] : at === end ? (wrap === "round" ? keys[start] : null) : keys[at + (down ? 1 : -1)];
      keysMovedHighlight(listRef.current);
      light(next);
      if (next !== null) scrollKeyWalkedRow(shown[keys.indexOf(next)], listRef.current);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (lit !== null) rows().find((row) => row.dataset.key === lit)?.click();
    }
  }
  const onPointerMove = (event: PointerEvent<HTMLElement>) => {
    const key = pointerTakenRow(event);
    if (key !== null) light(key);
  };
  return { lit, light, check, onKeyDown, onPointerMove };
}
