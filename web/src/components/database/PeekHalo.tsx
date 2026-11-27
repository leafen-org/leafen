import type { CSSProperties } from "react";
import { useSideRowPeekShows } from "@/lib/sideRowPeekLayout";
import styles from "./PeekHalo.module.css";

/**
 * The ring an item open in the side peek wears in every view that shows it,
 * as Notion's (seen 2026-11-03): a 1.5px blue ring inside the item's own
 * corners on a faint blue ground, gone when the peek closes or shows
 * another item. Its item is positioned.
 */
export function PeekHalo({ itemId, style }: { itemId: string; style?: CSSProperties }) {
  const open = useSideRowPeekShows(itemId);
  return open ? <span className={styles.halo} style={style} data-peek-halo aria-hidden="true" /> : null;
}
