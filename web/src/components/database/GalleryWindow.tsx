import { Fragment, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { GraphRow } from "@/contentGraph";
import { useDataFindSource } from "@/lib/useDataFindSource";
import { useWindowedRows, windowGapStyle } from "@/lib/useWindowedRows";
import styles from "./databaseSheet";
import { groupLine, lineGroup, lineRowId } from "./viewGroupLines";

/** More places than this in a gallery (its cards and its "+ 새 페이지" cards) draw only the lines near the view. */
export const GALLERY_WINDOW_THRESHOLD = 100;

/** A gallery's cards, or one group's under its head, and the "+ 새 페이지" after them. */
export type GallerySection = { key: string; head?: ReactNode; rows: readonly GraphRow[]; tile?: ReactNode; collapsed?: boolean };

type Entry = { kind: "head"; section: number; key: string } | { kind: "line"; section: number; line: number; last: boolean; key: string };
const NO_PINS: readonly number[] = [];

/**
 * A long gallery, its groups' heads and its lines of cards, drawn only near
 * the view as TanStack Virtual places them in what scrolls the page; gaps
 * stand in for the rest, so the page scrolls as if every card were there
 * (2026-11-11: a view of 100,000 rows turned into a gallery drew every card
 * and the tab stopped answering). Each line is a grid of the gallery's own
 * columns (.gallery), as many cards to it as the gallery's grid holds, and
 * stands as far from the line or head before it as the whole gallery's
 * grids do; the card holding focus and the card dragged stay drawn, and
 * page find reads every card's text.
 */
export function GalleryWindow({ sections, size, layout, contentPreview, renderCard, dragging, findText }: {
  sections: readonly GallerySection[];
  size: string;
  layout?: string;
  contentPreview?: boolean;
  renderCard(row: GraphRow, section: string): ReactNode;
  dragging?: string | null;
  /** A card's text as it shows, which page find reads. */
  findText(row: GraphRow): string;
}) {
  const listRef = useRef<HTMLDivElement>(null);
  const probeRef = useRef<HTMLDivElement>(null);
  const grouped = sections.some((section) => section.head !== undefined);
  // How many cards a line holds: the columns of a gallery's grid as wide as
  // the lines, read again as it is drawn wider or narrower; none known draws no line.
  const [columns, setColumns] = useState(0);
  useLayoutEffect(() => {
    const probe = probeRef.current;
    if (!probe) return;
    const read = () => setColumns(Math.max(1, window.getComputedStyle(probe).gridTemplateColumns.split(" ").filter((track) => /^[\d.]+px$/u.test(track)).length));
    read();
    const observer = new ResizeObserver(read);
    observer.observe(probe);
    return () => observer.disconnect();
  }, []);
  const entries = useMemo(() => {
    const made: Entry[] = [];
    if (!columns) return made;
    sections.forEach((section, at) => {
      if (section.head !== undefined) made.push({ kind: "head", section: at, key: `head:${section.key}` });
      if (section.collapsed) return;
      const lines = Math.ceil((section.rows.length + (section.tile ? 1 : 0)) / columns);
      for (let line = 0; line < lines; line += 1) made.push({ kind: "line", section: at, line, last: line === lines - 1, key: `${section.key}:${columns}:${line}` });
    });
    return made;
  }, [columns, sections]);
  // Each card's line, for those kept drawn and those page find moves to.
  const lineStarts = useMemo(() => {
    const starts = new Map<string, number>();
    entries.forEach((entry, index) => { if (entry.kind === "line" && entry.line === 0) starts.set(sections[entry.section].key, index); });
    return starts;
  }, [entries, sections]);
  // A card dragged is named by its line, in its own group (viewGroupLines.ts).
  const entryOf = (line: string | null | undefined) => {
    if (!line || !columns) return -1;
    const rowId = lineRowId(line), group = lineGroup(line);
    for (const section of sections) {
      const at = section.collapsed || (group !== undefined && group !== section.key) ? -1 : section.rows.findIndex((row) => row.id === rowId);
      if (at >= 0) return (lineStarts.get(section.key) ?? -1) + Math.floor(at / columns);
    }
    return -1;
  };
  const [focused, setFocused] = useState<string | null>(null);
  const pinned = !focused && !dragging ? NO_PINS : [entryOf(focused), entryOf(dragging)].filter((index) => index >= 0);
  const windowed = useWindowedRows({
    listRef, count: entries.length, threshold: 0, pinned,
    // Each card's line, its group's where the gallery is grouped (GalleryView's lineOf), a folded group's none.
    lines: () => sections.flatMap((section) => section.collapsed ? [] : section.rows.map((row) => grouped ? groupLine(section.key, row.id) : row.id)),
    // A head 48px under the group before; a line of cards with their preview and a line of values, 16px under the one before.
    estimateSize: (index) => entries[index]?.kind === "head" ? 68 : 236,
    getItemKey: (index) => entries[index]?.key ?? index,
  });
  const cards = useMemo(() => sections.flatMap((section) => section.collapsed ? [] : section.rows), [sections]);
  useDataFindSource({
    listRef, items: cards,
    reveal: (row) => {
      const index = entryOf(row.id);
      if (index >= 0) windowed.reveal(index);
    },
    parts: (row) => [{
      text: findText(row),
      drawn: () => listRef.current?.querySelector<HTMLElement>(`[data-row-card][data-row-id="${CSS.escape(row.id)}"]`) ?? null,
    }],
  });

  const grid = { "data-size": size, "data-card-layout": layout, "data-page-content-preview": contentPreview ? "true" : undefined };
  const gridClass = grouped ? `${styles.gallery} ${styles.galleryGroupGrid}` : styles.gallery;
  const lineCards = (section: GallerySection, line: number) => {
    const drawn: ReactNode[] = [];
    const end = Math.min(section.rows.length + (section.tile ? 1 : 0), (line + 1) * columns);
    for (let slot = line * columns; slot < end; slot += 1) {
      drawn.push(slot < section.rows.length ? renderCard(section.rows[slot], section.key) : <Fragment key="tile">{section.tile}</Fragment>);
    }
    return drawn;
  };
  return (
    <div ref={listRef} data-gallery-window
      onFocus={(event) => setFocused((event.target as Element).closest("[data-row-card]")?.getAttribute("data-row-id") ?? null)}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFocused(null); }}>
      <div ref={probeRef} aria-hidden="true" className={gridClass} {...grid} style={{ height: 0, paddingBlock: 0, overflow: "hidden", visibility: "hidden" }} />
      {windowed.entries.map((slot) => {
        if (slot.kind === "gap") return <div key={slot.key} aria-hidden="true" style={windowGapStyle(slot.size)} />;
        const entry = entries[slot.index], section = sections[entry.section];
        if (entry.kind === "head") {
          // Groups stand 20px apart, as the gallery's groups do (.galleryGroups).
          return <div key={entry.key} ref={windowed.measure} data-index={slot.index} data-gallery-group-key={section.key}
            style={{ paddingBlockStart: entry.section === 0 ? 0 : 20 }}>{section.head}</div>;
        }
        // Its first line under its head, or 16px under the bar; the next 16px under it; 4px over what follows (.gallery, .galleryGroupGrid).
        return <div key={entry.key} ref={windowed.measure} data-index={slot.index} className={gridClass} {...grid} data-gallery-line
          style={{ paddingBlockStart: entry.line === 0 && grouped ? 1 : 16, paddingBlockEnd: entry.last ? 4 : 0 }}>{lineCards(section, entry.line)}</div>;
      })}
      {windowed.after > 0 && <div aria-hidden="true" style={windowGapStyle(windowed.after)} />}
    </div>
  );
}
