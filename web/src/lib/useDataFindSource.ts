import { type RefObject, useEffect, useRef } from "react";
import { drawnText, rangeInDrawnText, registerPageFindSource, type PageFindSourceMatch } from "./pageFindSources";
import { pageFindTextRanges } from "./pageFindText";

/** A piece of an item's text as it shows, and where it is drawn. */
export type DataFindPart = { text: string; drawn(): HTMLElement | null };

/**
 * Page find reads a windowed list from its data, drawn or not: each item's
 * text pieces, in order. A match is marked once its piece is drawn; moving
 * to it draws the item first.
 */
export function useDataFindSource<Item>({ listRef, items, parts, reveal, enabled = true }: {
  listRef: RefObject<HTMLElement | null>;
  items: readonly Item[];
  parts(item: Item): readonly DataFindPart[];
  reveal(item: Item): void;
  enabled?: boolean;
}) {
  const latest = useRef({ items, parts, reveal });
  latest.current = { items, parts, reveal };
  useEffect(() => {
    const list = listRef.current;
    if (!list || !enabled) return;
    return registerPageFindSource(list, {
      find(query) {
        const matches: PageFindSourceMatch[] = [];
        for (const item of latest.current.items) {
          for (const part of latest.current.parts(item)) {
            pageFindTextRanges(part.text, query).forEach((_, occurrence) => matches.push({
              reveal: () => latest.current.reveal(item),
              range: () => {
                const element = part.drawn();
                const hit = element ? pageFindTextRanges(drawnText(element), query)[occurrence] : undefined;
                return element && hit ? rangeInDrawnText(element, hit.start, hit.end) : null;
              },
            }));
          }
        }
        return matches;
      },
    });
  }, [enabled, listRef]);
}
