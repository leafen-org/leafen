import { useEffect, useRef } from "react";
import { ROW_VALUES_STEPPED_EVENT, type ValueChange } from "@/lib/nativeTableProductAdapter";
import { sameCell } from "@/lib/valueSteps";

/**
 * A timeline's new bar after its title closes and through ⌘Z, as Notion's
 * (seen 2026-11-15 at 1440, a bar made by pressing an empty lane, judged by
 * the screen and the next key typed): its title closed with Enter or Esc
 * leaves the bar chosen; ⌘⇧Z bringing a bar back chooses it once it shows,
 * as any view's items brought back (cardSelection.tsx); ⌘Z taking
 * back a title typed into a new bar opens that title again, empty, with the
 * caret in it, the bar no longer chosen. A title closed by pressing
 * elsewhere leaves no choice.
 */
export function useTimelineNewBarChoice({ titlePropertyId, choose, reopen }: {
  titlePropertyId?: string;
  choose(rowIds: readonly string[]): void;
  /** Opens a bar's title as a new bar's. */
  reopen(rowId: string): void;
}) {
  // New bars given a title here: ⌘Z taking it back opens it again.
  const titled = useRef(new Set<string>());
  const latest = useRef({ titlePropertyId, reopen, choose });
  latest.current = { titlePropertyId, reopen, choose };
  useEffect(() => {
    const stepped = (event: Event) => {
      const { changes = [], forward = true } = (event as CustomEvent<{ changes?: readonly ValueChange[]; forward?: boolean }>).detail ?? {};
      const back = forward ? undefined : changes.find(({ rowId, propertyId, before }) =>
        titled.current.has(rowId) && propertyId === latest.current.titlePropertyId && sameCell(before, undefined));
      if (!back) return;
      titled.current.delete(back.rowId);
      // Its bar leaves the choice as its title opens, as a new bar's.
      latest.current.choose([]);
      latest.current.reopen(back.rowId);
    };
    window.addEventListener(ROW_VALUES_STEPPED_EVENT, stepped);
    return () => window.removeEventListener(ROW_VALUES_STEPPED_EVENT, stepped);
  }, []);
  return {
    /** A new bar's title closed: by Enter or Esc its bar is chosen; a title given is one ⌘Z opens again. */
    closed(rowId: string, title: string, byKey: boolean) {
      if (title) titled.current.add(rowId);
      if (byKey) latest.current.choose([rowId]);
    },
  };
}
