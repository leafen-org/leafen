import { useLayoutEffect, useRef, useState, type RefObject } from "react";
import { useStateUnlessSame } from "@/lib/useStateUnlessSame";
import { flushHeldStyleWrites } from "./heldStyleWrites";
import type { PropertyCellEnvironment } from "./propertyCellTypes";

// Notion draws a table's cells only some 400px past what shows of it (two
// 200px columns: at 1280, to 1380 of a table showing 843, and to 1580 once
// scrolled 300; seen 2026-11-06) and sizes each row from the cells it drew.
// So a wrapped value grows its row in a column that starts within that
// reach, and in one further right it is cut — a text's with "확장"
// (WrappedTextExpand) — whatever kind of value it is: files, choices,
// people or relations as much as text (seen 2026-11-14: a wrapped files
// column of three, a choices one and a text one past the reach left the row
// 38 high; 2026-11-16 in two tabs, a full page database opened afresh: a
// wrapped relation of three pages 2,784px along left its row 38). Scrolled
// sideways, the reach goes on with what shows and the rows grow as the cells
// it reaches are drawn, never shrinking back until the table is opened again
// (seen 2026-11-14: 38 → 79 once scrolled 100, the text in reach; → 96 at
// 1000, the files; still 96 scrolled back to 300; 2026-11-16: 38 → 155 at the
// relation, 149 back at 0) — a property duplicated or added meanwhile, its
// columns moving along, keeps them grown (2026-11-16 in two tabs: 149 after
// "속성 복제", the relation pushed 200px further off).
const REACH_PAST_VIEW = 400;

/**
 * Which wrapped cells are cut — those in the columns the reach has not come
 * to since the table opened, measured again as its box changes size, as its
 * columns change and as it is scrolled further — which columns were cut since
 * it opened, and the rows whose cut value was grown to its whole ("확장"),
 * until the table is opened again. `wrapped` are the wrapped columns, `ids`
 * every column's property: a column reached stays so wherever it moves. A
 * table with no wrapped column measures nothing, and the reach changes the
 * table only where it cuts one of them — measured and changed for every
 * table, it drew a 30-row, 40-property table twice as it opened (2026-11-12).
 *
 * A column cut once keeps its cells' "확장" where its value ran past its row
 * while cut, the column reached and the row grown to the value or not, until
 * the row's is pressed (WrappedTextExpand) — Notion's (seen 2026-11-24 in a
 * table and a table grouped by tags: a wrapped text column 2,000px along,
 * scrolled to, grew its rows 38 → 100 and each kept "확장" until pressed,
 * which took that row's only; one whose row the reach set taller than its
 * value showed none).
 */
export function useWrappedTextCut(scrollRef: RefObject<HTMLElement | null>, widths: readonly number[], wrapped: readonly number[],
  ids: readonly string[]) {
  // The wrapped columns' properties the reach has come to, and the farthest
  // the table was scrolled sideways, since it opened.
  const reached = useRef(new Set<string>());
  const farthest = useRef(0);
  // The wrapped columns cut, and those cut since it opened, as their properties joined.
  const [cut, setCut] = useStateUnlessSame("");
  const [cutOnce, setCutOnce] = useStateUnlessSame("");
  const everCut = useRef(new Set<string>());
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const key = widths.join(" ");
  const wrappedKey = wrapped.join(" ");
  const idsKey = ids.join(" ");
  useLayoutEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll || !wrappedKey) return undefined;
    const columns = wrappedKey.split(" ").map(Number), properties = idsKey.split(" ");
    const measure = () => {
      // The reach goes by the table's box as the page-wide table set it.
      flushHeldStyleWrites();
      const first = scroll.querySelector<HTMLElement>("[data-table-cell]");
      if (!first) return;
      const start = first.getBoundingClientRect().left - scroll.getBoundingClientRect().left + scroll.scrollLeft;
      farthest.current = Math.max(farthest.current, scroll.scrollLeft);
      const limit = scroll.scrollLeft + scroll.clientWidth - start + REACH_PAST_VIEW;
      let left = 0;
      key.split(" ").map(Number).forEach((width, column) => {
        if (left < limit) reached.current.add(properties[column]);
        left += width;
      });
      const cutNow = columns.map((column) => properties[column]).filter((id) => !reached.current.has(id));
      cutNow.forEach((id) => everCut.current.add(id));
      setCut(cutNow.join(" "));
      setCutOnce([...everCut.current].join(" "));
    };
    measure();
    // Resized, measured as the next frame starts, laid out by then: in its
    // observer, after the page's other measures had written (heldStyleWrites),
    // it laid the page out again in each frame of a side peek's slide.
    let frame = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    });
    observer.observe(scroll);
    // Scrolled sideways past where it was, as the next frame starts.
    const scrolled = () => {
      if (scroll.scrollLeft <= farthest.current) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    scroll.addEventListener("scroll", scrolled, { passive: true });
    return () => { observer.disconnect(); scroll.removeEventListener("scroll", scrolled); cancelAnimationFrame(frame); };
  }, [scrollRef, key, wrappedKey, idsKey, setCut, setCutOnce]);
  const cutIds = cut ? cut.split(" ") : [];
  const cutOnceIds = cutOnce ? cutOnce.split(" ") : [];
  const cuts = (columnIndex: number) => cutIds.includes(ids[columnIndex]);
  return {
    cuts,
    /** Each column as the rows draw it: cut ("1"), cut once and reached ("2"), or never cut ("0"). */
    cutMarks: (columns: number) => Array.from({ length: columns }, (_, index) =>
      cuts(index) ? "1" : cutOnceIds.includes(ids[index]) ? "2" : "0").join(""),
    expanded: (rowId: string) => expanded.has(rowId),
    expand: (rowId: string) => setExpanded((rows) => rows.has(rowId) ? rows : new Set(rows).add(rowId)),
  };
}

/**
 * The cells' ways, a value written as text in a cut column growing its row to
 * the whole value as "확장" does, until the table is opened again — Notion's
 * (seen 2026-11-14: three lines written in a wrapped text column past the
 * reach grew the row 36 → 78, cut again once the table was opened again; the
 * value opened and left as it was stayed cut).
 */
export function growingCutWrites(environment: PropertyCellEnvironment,
  cut: RefObject<{ grows(propertyId: string): boolean; expand(rowId: string): void }>): PropertyCellEnvironment {
  const { operations } = environment;
  return { ...environment, operations: { ...operations, setTextValue: (rowId, property, value, context, spans) => {
    if (cut.current?.grows(property.id)) cut.current.expand(rowId);
    return operations.setTextValue(rowId, property, value, context, spans);
  } } };
}
