import { CaretRightFill, ChevronDown, FileText, FilterIcon, StatusIcon, SubitemCountIcon, SubitemParentIcon, UserIcon } from "@/icons/leafen";
import styles from "./DatabaseTaskSettings.module.css";

export type SubitemsPictureRow = {
  label: string; sub?: boolean; open?: boolean; faded?: boolean;
  /** A flattened parent's sub-items, after its name. */
  count?: number;
  /** A flattened sub-item's parent property, after its name. */
  parent?: string;
};

/**
 * A sub-items setting's picture, as Notion's (seen 2026-11-03): a white
 * framed box 9 in above and below, of 24px rows 8 in from its sides — a
 * 24×20 toggle, 12 further in for a sub-item and turned down when open,
 * then 🐔 for a top row or 🐣 for a sub-item 5 before its 12px/500 name;
 * rows the filter leaves out at half strength under the filter's blue pill.
 * A merged list has no toggles, its rows 22 high, a parent's sub-items
 * counted after its name and a sub-item's parent property named after its
 * own (seen 2026-11-11).
 */
export function SubitemsPicture({ rows, pill, toggles = true, display, slots = rows.length }: {
  rows: readonly SubitemsPictureRow[]; pill?: string; toggles?: boolean;
  /** The rows its box keeps room for, the most any choice draws, so it keeps its height as Notion's (seen 2026-11-11). */
  slots?: number;
  /** The display option's picture, 2 further in than the filter's. */
  display?: boolean;
}) {
  return (
    <div className={styles.subitemsPicture} data-display={display || undefined} data-flat={toggles ? undefined : true} aria-hidden="true">
      {pill ? <span className={styles.subitemsPicturePill}><FilterIcon size="tiny" />{pill}<ChevronDown size="tiny" /></span> : null}
      {rows.map((row, index) => (
        <div key={index} data-faded={row.faded || undefined}>
          {toggles ? <span className={styles.subitemsPictureToggle} data-sub={row.sub || undefined} data-open={row.open || undefined}>
            <CaretRightFill />
          </span> : null}
          <span className={styles.subitemsPictureIcon}>{row.sub ? "🐣" : "🐔"}</span>
          <span>{row.label}</span>
          {row.count !== undefined ? <span className={styles.subitemsPictureMeta}><SubitemCountIcon size="small" />{row.count}</span> : null}
          {row.parent !== undefined ? <span className={styles.subitemsPictureMeta}><SubitemParentIcon size="small" />{row.parent}</span> : null}
        </div>
      ))}
      {Array.from({ length: Math.max(0, slots - rows.length) }, (_, index) => <div key={`slot-${index}`} data-slot />)}
    </div>
  );
}

/**
 * A board's picture: a card naming its sub-items under its title, then a
 * person and a status, as Notion's (seen 2026-11-03).
 */
export function SubitemsCardPicture({ parent, child, person, status }: { parent: string; child: (number: number) => string; person: string; status: string }) {
  return (
    <div className={styles.subitemsPicture} data-card aria-hidden="true">
      <div className={styles.subitemsPictureCard}>
        <div><span className={styles.subitemsPictureIcon}>🐔</span><span>{parent}</span></div>
        <div>{[1, 2].map((number) => <span key={number}><FileText size="tiny" />{child(number)}</span>)}</div>
        <div><UserIcon size="tiny" /><span>{person}</span></div>
        <div><StatusIcon size="tiny" /><span>{status}</span></div>
      </div>
    </div>
  );
}
