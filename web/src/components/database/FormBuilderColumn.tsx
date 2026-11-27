import { type ReactNode, type RefObject, useLayoutEffect, useRef } from "react";
import { watchPageSize } from "../page/pageSizeWatch";
import { holdStyleWrite } from "./heldStyleWrites";
import styles from "./FormView.module.css";

/** A page's frame: its blocks keep this far from each edge of what it shows (pageColumn.module.css). */
const PAGE_EDGE = 96;

/**
 * The builder's column — the form's head, its questions and "+" — at most
 * 600 wide and 40 in from each side of the room it is centred in
 * (FormView.module.css .builderColumn). In a page's text that room is its
 * database's box. On its database's own page it is the page itself, 96 in
 * from each edge of what the page shows inside its scrollbar, as Notion's
 * (seen 2026-11-17 in two tabs, dark, sidebar open: question cards at 555
 * and 547.5 at 1440 without and with a scrollbar, at 406 — 482 and 467 wide —
 * at 1024), not the view's box, which there starts 104 in and ends where the
 * page's column lets it (PageView .doc).
 */
export function FormBuilderColumn({ label, hasCover, children }: { label: string; hasCover: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useFormPageRoom(ref);
  return (
    <div ref={ref} className={styles.builderColumn} aria-label={label} role="region" data-has-cover={hasCover ? "true" : undefined}>
      {children}
    </div>
  );
}

/** Tells the column where its database's own page's room starts from its box's start, and how wide it is (--form-page-start, --form-page-width). */
function useFormPageRoom(ref: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const column = ref.current;
    const box = column?.parentElement;
    const page = column?.closest<HTMLElement>('[data-page-scroll][data-database-canvas="true"]');
    if (!column || !box || !page || column.closest('[data-row-page="true"], [data-database-view-embedded]')) return undefined;
    function measure(write: (change: () => void) => void = holdStyleWrite) {
      if (!column || !box || !page) return;
      const shown = page.getBoundingClientRect().left + page.clientLeft;
      const own = box.getBoundingClientRect();
      const start = getComputedStyle(box).direction === "rtl"
        ? own.right - (shown + page.clientWidth - PAGE_EDGE)
        : shown + PAGE_EDGE - own.left;
      const width = Math.max(0, page.clientWidth - 2 * PAGE_EDGE);
      write(() => {
        column.style.setProperty("--form-page-start", `${Math.round(start * 10) / 10}px`);
        column.style.setProperty("--form-page-width", `${width}px`);
      });
    }
    measure((change) => change());
    // Again as the page or the box changes size: a scrollbar coming or going, the page's column settling as it opens.
    const stops = [watchPageSize(page, measure), watchPageSize(box, measure)];
    return () => {
      for (const stop of stops) stop();
      column.style.removeProperty("--form-page-start");
      column.style.removeProperty("--form-page-width");
    };
  }, [ref]);
}
