import { Fragment, type HTMLAttributes, type ReactNode } from "react";
import type { Place } from "@/lib/pagePath";
import styles from "./PlaceLine.module.css";

/**
 * Where a page or a data source sits, under its title in every list that
 * names it, as Notion's (seen 2026-11-12: the search, its scope list, the @
 * menu, "옮기기", the trash, a relation's "+ 신규 그룹" and every list of
 * sources): one line of its place's titles (pagePlace, databaseSourcePlace)
 * with "/" between them; while the line is too narrow each title is cut with
 * "…" by the same share of its width, the "/"s and the "..." left whole.
 * Each title's words by drawTitle, where a list marks them.
 */
export function PlaceLine({ place, drawTitle, className, ...rest }: {
  place: Place;
  drawTitle?: (title: string) => ReactNode;
} & HTMLAttributes<HTMLSpanElement>) {
  return (
    <span {...rest} className={className ? `${styles.place} ${className}` : styles.place}>
      {place.map((piece, index) => (
        <Fragment key={index}>
          {index > 0 ? <span className={styles.slash}>/</span> : null}
          {piece === null ? <span className={styles.whole}>...</span> : <span className={styles.piece}>{drawTitle ? drawTitle(piece) : piece}</span>}
        </Fragment>
      ))}
    </span>
  );
}
