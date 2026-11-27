import type { MouseEvent } from "react";
import { useSideRowPeekShows } from "@/lib/sideRowPeekLayout";
import { CenterPeekIcon, OpenAsPage, SidePeekIcon } from "../icons";
import styles from "./databaseSheet";

/** What a table's title cells say on their open button, worked out once for the table. */
export type TableRowOpenLabels = { open: string; close: string; openAria?: string };

/**
 * A row's "열기" at its title's end, which reads "닫기" and closes the side
 * peek while its row is open there, as Notion's. It says where it opens the
 * row (`side`, `center`, `full`): a press that opens a peek keeps the rows
 * chosen (keepsViewChoice) for the peek to set aside while it is open
 * (peekSetsChoiceAside.ts). It follows the peek itself,
 * so a peek opening or closing draws again only the rows it changes, not its
 * tables (each drew all its rows again: some 45ms as a peek opened over a
 * page of three tables of 40, 60 and 120 rows, seen 2026-11-12).
 */
export function TableRowOpenButton({ rowId, openIn, labels, onOpen, onClose }: {
  rowId: string;
  openIn?: "side" | "center" | "full";
  labels: TableRowOpenLabels;
  onOpen: (event: MouseEvent<HTMLButtonElement>) => void;
  onClose?: () => void;
}) {
  const peeked = useSideRowPeekShows(rowId);
  if (peeked && onClose) {
    return <button type="button" className={styles.openRow} data-table-row-open="close" onClick={onClose}>
      <SidePeekIcon size="regular" aria-hidden="true" /><span>{labels.close}</span></button>;
  }
  return <button type="button" className={styles.openRow} data-table-row-open={openIn ?? "side"} aria-label={labels.openAria} onClick={onOpen}>
    {openIn === "center" ? <CenterPeekIcon size="regular" aria-hidden="true" /> : openIn === "full" ? <OpenAsPage size="regular" aria-hidden="true" />
      : <SidePeekIcon size="regular" aria-hidden="true" />}<span>{labels.open}</span>
  </button>;
}
