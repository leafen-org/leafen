import { useState, type KeyboardEvent } from "react";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * Which row of a property condition's list the keys light on a desktop, and
 * what ↑, ↓ and Enter do there, as Notion's window does (seen 2026-11-18 in
 * Chrome at 1440 in two tabs at different times, judged by the screen after
 * each key, no script in the page before it):
 *
 * - people (`round`): the first person lit from the start, dimmed and unlit
 *   under "모든 옵션" yet the one Enter picks; the arrows go round the people
 *   ("모든 옵션" is no stop of theirs); a pick, or words typed, light the
 *   first again.
 * - an option list: none lit as the window opens or words are typed; ↓
 *   lights "모든 옵션" first, then the options; past either end none again
 *   (the search's own step), then the other end; Enter ticks or unticks the
 *   lit one, which stays lit.
 *
 * The keys work wherever the window holds them (its search, "모든 옵션" or
 * the window itself); the pointer lights the row it moves onto. `reset`
 * changing (the words typed) leaves the list at its start again.
 */
export function useConditionListKeys({ keys, round, reset, onPress, onWalk }: {
  keys: readonly string[];
  round: boolean;
  reset: unknown;
  onPress(key: string): void;
  /** The row the arrows moved onto, to bring into view. */
  onWalk?(key: string): void;
}) {
  const [moved, setMoved] = useState<{ key: string | null; reset: unknown }>({ key: null, reset });
  const chosen = Object.is(moved.reset, reset) && moved.key !== null && keys.includes(moved.key) ? moved.key : null;
  const lit = chosen ?? (round ? keys[0] ?? null : null);
  const light = (key: string | null) => setMoved({ key, reset });
  const keyDown = (event: KeyboardEvent) => {
    if (event.defaultPrevented || imeComposing(event) || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    if (fieldEnter(event)) {
      if (lit === null) return;
      event.preventDefault();
      onPress(lit);
      return;
    }
    if ((event.key !== "ArrowDown" && event.key !== "ArrowUp") || !keys.length) return;
    event.preventDefault();
    const down = event.key === "ArrowDown", count = keys.length, at = lit === null ? -1 : keys.indexOf(lit);
    const next = round ? keys[(at + (down ? 1 : count - 1)) % count]
      : at < 0 ? down ? keys[0] : keys[count - 1] : down ? keys[at + 1] ?? null : keys[at - 1] ?? null;
    light(next);
    if (next !== null) onWalk?.(next);
  };
  return { lit, light, keyDown };
}
