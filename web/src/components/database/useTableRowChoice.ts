import { useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import type { GraphRow } from "@/contentGraph";
import { useStateUnlessSame } from "@/lib/useStateUnlessSame";
import { TABLE_ROWS_RESTORED_EVENT, VIEW_CHOICE_READ_EVENT, type StepRowsChosen, type ViewChoiceRead } from "@/lib/stepRowChoice";
import { ROWS_COPIED_EVENT } from "./viewRowOrder";
import { chosenRowsOf, lineRowId } from "./viewGroupLines";

/** A table's chosen rows, whose setter queues nothing for the rows it holds (useStateUnlessSame). */
export function useChosenRows() {
  return useStateUnlessSame<ReadonlySet<string>>(() => new Set());
}

// How long a row chosen before the table shows it is waited for.
const ARRIVING_MS = 5000;

/**
 * Keeps a table's chosen rows with its rows. A chosen row that goes away
 * from its source leaves the choice, and one its view's filter hides stays
 * chosen (`existing`: every row of the source, as Notion's, see
 * useChosenRowList); one chosen before the table shows it (a copy the
 * server makes, a row the page's ⌘Z brings back) stays chosen while it is
 * on its way, a few seconds at most, so every copy of ⌘D ends up chosen
 * however its rows arrive, as Notion's (2026-11-14, desktop app: of two
 * copies the second showed a frame after the first, and only the first
 * stayed chosen). Rows ⌘Z brought back are chosen in their places once the
 * table shows one of them, as Notion's, and so are the copies of the rows
 * chosen here made from a row's menu. A grouped table chooses lines, each
 * kept with its row (viewGroupLines.ts): rows brought back — trashed ones
 * ⌘Z restores, those whose copies (made of the lines `from`) it takes away —
 * are chosen on the lines they were chosen on, as Notion's (seen 2026-11-24:
 * 나 ticked under x and y, ⌘D then ⌘Z, or trashed then ⌘Z: both ticked
 * again). What a step of ⌘Z or ⌘⇧Z chooses — rows moved back in their
 * places too, and what was chosen at a ⌘Z given back by its ⌘⇧Z, the rest
 * let go — is named by the step (stepRowChoice.ts), which reads here what
 * the table has chosen. Gives back how to choose rows some of which may not
 * be shown yet.
 */
export function useTableRowChoice(rows: readonly { id: string }[], chosen: ReadonlySet<string>,
  setChosen: Dispatch<SetStateAction<ReadonlySet<string>>>, existing?: readonly { id: string }[]) {
  // Each row chosen on its way, with when it was chosen.
  const arriving = useRef(new Map<string, number>());
  const shown = useRef(rows);
  shown.current = rows;
  const chosenNow = useRef(chosen);
  chosenNow.current = chosen;
  const [restored, setRestored] = useState<readonly string[]>([]);
  // The lines the last copies were made of, and the last lines chosen, which their rows coming back stand for.
  const copiedLines = useRef<readonly string[]>([]), lastChosen = useRef<readonly string[]>([]);
  if (chosen.size) lastChosen.current = [...chosen];
  const choose = useRef((ids: readonly string[], from?: readonly string[]) => {
    const here = new Set(shown.current.map((row) => row.id)), at = Date.now();
    arriving.current.clear();
    for (const id of ids) if (!here.has(lineRowId(id))) arriving.current.set(id, at);
    if (from) copiedLines.current = from;
    setChosen(new Set(ids));
  }).current;
  useEffect(() => {
    const rowsOf = (ids: readonly string[]) => [...chosenRowsOf(ids)].sort().join("\n");
    const restoredRows = (event: Event) => {
      const { rowIds = [], only = false } = (event as CustomEvent<StepRowsChosen>).detail ?? {};
      // The lines given back as they were chosen, the others let go.
      if (only) {
        setChosen(new Set());
        setRestored(rowIds);
        return;
      }
      setRestored([copiedLines.current, lastChosen.current].find((lines) => lines.length && rowsOf(lines) === rowsOf(rowIds)) ?? rowIds);
    };
    const read = (event: Event) => { (event as CustomEvent<ViewChoiceRead>).detail?.chosen.push(...chosenNow.current); };
    const copied = (event: Event) => {
      const { from = [], copies = [] } = (event as CustomEvent<{ from?: readonly string[]; copies?: readonly string[] }>).detail ?? {};
      const rows = chosenRowsOf(chosenNow.current), copied = new Set(from);
      if (copied.size === rows.size && [...copied].every((id) => rows.has(id))) setRestored(copies);
    };
    window.addEventListener(TABLE_ROWS_RESTORED_EVENT, restoredRows);
    window.addEventListener(VIEW_CHOICE_READ_EVENT, read);
    window.addEventListener(ROWS_COPIED_EVENT, copied);
    return () => {
      window.removeEventListener(TABLE_ROWS_RESTORED_EVENT, restoredRows);
      window.removeEventListener(VIEW_CHOICE_READ_EVENT, read);
      window.removeEventListener(ROWS_COPIED_EVENT, copied);
    };
  }, [setChosen]);
  // This table's once it shows one of them.
  useEffect(() => {
    if (!restored.length) return;
    const here = new Set(rows.map((row) => row.id));
    if (!restored.some((id) => here.has(lineRowId(id)))) return;
    choose(restored);
    setRestored([]);
  }, [choose, restored, rows]);
  useEffect(() => setChosen((current) => {
    if (current.size === 0) return current;
    const here = new Set(rows.map((row) => row.id)), at = Date.now();
    for (const row of existing ?? []) here.add(row.id);
    const kept = [...current].filter((id) => {
      if (here.has(lineRowId(id))) { arriving.current.delete(id); return true; }
      const since = arriving.current.get(id);
      return since !== undefined && at - since < ARRIVING_MS;
    });
    return kept.length === current.size ? current : new Set(kept);
  }), [existing, rows, setChosen]);
  return choose;
}

const NO_ROWS: readonly GraphRow[] = [];

/**
 * A table's chosen rows, from its chosen lines (viewGroupLines.ts), each
 * once: those shown in the order shown (`order`) and then those its filter
 * hides, from its source (`existing`), in the source's order. A chosen row a filter hides stays chosen, as Notion's (seen
 * 2026-11-17 at 1440, two ticked rows, a filter hiding one and then both:
 * "2개 선택됨" stayed, a value picked from the bar went to the hidden row too
 * and brought it back ticked, and with the filter gone both showed ticked):
 * the selection bar counts it, and its picks, menu, copy and trash take it.
 * Gives back those rows, and the chosen lines the table shows (`shown`), which
 * its rows tick and its head's box reads: a head whose shown rows are none of
 * them has no box, as Notion's.
 */
export function useChosenRowList(chosen: ReadonlySet<string>, shown: readonly GraphRow[], order: readonly { row: { id: string } }[],
  existing?: readonly GraphRow[]): { rows: readonly GraphRow[]; shown: ReadonlySet<string> } {
  return useMemo(() => {
    if (chosen.size === 0) return { rows: NO_ROWS, shown: chosen };
    const at = new Map(order.map((item, index) => [item.row.id, index])), rows = chosenRowsOf(chosen);
    const list = shown.filter((row) => rows.has(row.id))
      .sort((left, right) => (at.get(left.id) ?? at.size) - (at.get(right.id) ?? at.size));
    if (list.length === rows.size || !existing) return { rows: list, shown: chosen };
    const listed = new Set(list.map((row) => row.id));
    return { rows: [...list, ...existing.filter((row) => rows.has(row.id) && !listed.has(row.id))],
      shown: new Set([...chosen].filter((id) => listed.has(lineRowId(id)))) };
  }, [chosen, existing, order, shown]);
}
