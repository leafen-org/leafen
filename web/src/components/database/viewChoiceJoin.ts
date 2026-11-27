import { useEffect, useRef, useState, type RefObject } from "react";
import { choiceOut, choiceStep, type ChoiceEnds } from "./viewChoiceSteps";

/**
 * A choice an inline database's view shares with its page's blocks, as
 * Notion's (seen 2026-11-27 in Chrome at 1440 under a Notion test page, after
 * each key only, a table, a grouped table, a board, a list and a gallery):
 * from the line under the view, Shift+↑ chooses the view's last item with
 * the line still chosen, each Shift+↑ after it one item more, and past the
 * first item the database's block with the line; Shift+↓ gives the items
 * back one by one to the line alone. From the view's own last item, Shift+↓
 * joins the line under it to the items, and on down. ↑ then chooses the item
 * before the first chosen alone, ↓ the block after the last, Escape lets all
 * go and Backspace moves the items to the trash and takes the line away,
 * one ⌘Z bringing both back joined again and ⌘⇧Z taking both again with
 * nothing chosen (seen 2026-11-27 in Chrome at 1440, table, board and list,
 * Backspace, Delete and ⌘X, judged after each key: pageViewJoin.ts). A
 * view with no item is passed as one block. The page around it holds its
 * blocks' half and takes the keys (pageViewJoin.ts), asking the view, by
 * this event on its root, to act on its half; cancelled when it did.
 */
export const VIEW_CHOICE_JOIN = "leafen:view-choice-join";
export type ViewChoiceJoin = {
  /**
   * "in": choose the last item, joined; "stretch": Shift+↑ or ↓ moving the
   * end in the view, `out` set when it went past the edge and let the items
   * go; "step": ↑, the item before the first chosen alone, `out` when there
   * is none and the items went; "end": the join over, the items kept alone
   * (`keep`) or let go; "trash": the items to the trash, `ids` set to
   * them; "rejoin": `ids` chosen again, joined (a ⌘Z brought them back).
   */
  act: "in" | "stretch" | "step" | "end" | "trash" | "rejoin";
  down?: boolean;
  keep?: boolean;
  out?: boolean;
  ids?: string[];
};

/**
 * A view's half of a joined choice: whether its chosen items are joined to
 * the page's blocks now — its own keys leave them to the page then — and
 * `out`, which hands ↑ or ↓ past its first or last item to the page, Shift
 * with them or not: the items let go, or with Shift+↓ joined to the page's
 * next block. `take` chooses items, `own` with the view's own reach.
 */
export function useViewChoiceJoin({ root, order, chosen, ends, take, clear, trash }: {
  root: RefObject<HTMLElement | null>;
  order: () => string[];
  chosen: ReadonlySet<string>;
  ends: RefObject<ChoiceEnds | null>;
  take: (ids: string[], ends: ChoiceEnds, own: boolean) => void;
  clear: () => void;
  trash: () => void;
}) {
  const [joined, setJoined] = useState(false);
  const joinedNow = useRef(false);
  const join = (next: boolean) => { joinedNow.current = next; setJoined(next); };
  const latest = useRef({ order, take, clear, trash });
  latest.current = { order, take, clear, trash };
  // What the items are now, ahead of the view's next drawing: keys pressed quickly come before it.
  const drawn = useRef(chosen), now = useRef(chosen);
  if (drawn.current !== chosen) drawn.current = now.current = chosen;
  // Items let go any way end it.
  useEffect(() => { if (!chosen.size && joinedNow.current) join(false); }, [chosen]);
  useEffect(() => {
    const view = root.current;
    if (!view) return;
    const act = (event: Event) => {
      const detail = (event as CustomEvent<ViewChoiceJoin>).detail;
      const { order, take: takeNow, clear: clearNow, trash } = latest.current;
      const take = (ids: string[], next: ChoiceEnds, own: boolean) => { now.current = new Set(ids); takeNow(ids, next, own); };
      const clear = () => { now.current = new Set(); clearNow(); };
      const all = order();
      if (detail?.act === "in") {
        const last = all[all.length - 1];
        if (!last) return;
        take([last], { anchor: last, head: last }, false);
        join(true);
      } else if (detail?.act === "stretch" || detail?.act === "step") {
        const step = choiceStep(all, now.current, ends.current, detail.act === "stretch" && detail.down === true, detail.act === "stretch");
        if (step && step !== "out") take(step.ids, step.ends, detail.act === "step");
        else { detail.out = true; clear(); }
        if (detail.out || detail.act === "step") join(false);
      } else if (detail?.act === "end") {
        join(false);
        if (!detail.keep) clear();
      } else if (detail?.act === "trash") {
        detail.ids = [...now.current];
        join(false);
        trash();
      } else if (detail?.act === "rejoin" && detail.ids?.length) {
        take(detail.ids, { anchor: detail.ids[0], head: detail.ids[detail.ids.length - 1] }, false);
        join(true);
      } else return;
      event.preventDefault();
    };
    view.addEventListener(VIEW_CHOICE_JOIN, act);
    return () => view.removeEventListener(VIEW_CHOICE_JOIN, act);
  });
  return {
    joined,
    /** Whether the items are joined now, read in a key's handler. */
    joinedNow: () => joinedNow.current,
    out: (down: boolean, shift: boolean) => {
      if (!choiceOut(root.current, down, shift)) return;
      if (shift && down) join(true);
      else latest.current.clear();
    },
  };
}
