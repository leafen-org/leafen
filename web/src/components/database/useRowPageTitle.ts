import { useEffect, useRef, useState } from "react";
import { dropTypedPageTitle, useOwnPageName, useTypedPageTitle } from "@/lib/pageNames";
import type { TitleValue } from "@/lib/titleMerge";
import { rowValueRecord } from "@/lib/typedDraftSender";
import type { TextSpan } from "@/lib/types";
import type { FieldKept } from "./fieldTypedSave";
import type { RichTextCellChange } from "./RichTextCellEditor";
import { cellSpans } from "./richTextCellModel";

type Cell = { value?: unknown; spans?: unknown } | undefined;
/**
 * Writes the row's title; given the title it was typed on (`read`), over
 * that one, merged with another the server holds by then (rowValuesKept.ts).
 */
export type RowTitleWrite = (value: string, spans?: TextSpan[], read?: TitleValue | null) => Promise<unknown> | void;

/**
 * A row page's title (DatabaseRowPage): the heading shows a rename from the
 * path's popover as it is typed, then the row's title as it is saved, here
 * or elsewhere. What is typed in the heading is kept on this device at each
 * key (`kept`, as the row's value `record` names it, its changes going with
 * the page `record` names) and written as the typing pauses or ends, each
 * save in its turn in the outbox behind the ones before it, over the title
 * it was typed on — merged with another the server holds by then
 * (rowValuesKept.ts), as typed, the spaces at its ends too (titleMerge.ts).
 */
export function useRowPageTitle({ rowId, cell, write, named, record }: {
  rowId: string;
  cell: Cell;
  /** The row's icon and last edit as its page holds them, which name it everywhere with its title (useOwnPageName). */
  named?: { icon?: string; iconType?: string; updatedAt?: string };
  write: RowTitleWrite | undefined;
  /** The row's title property and the page its changes go with: what the heading keeps typed on the device. */
  record?: { propertyId: string; hostPageId: string };
}) {
  const [renamed, setRenamed] = useState<{ rowId: string; title: string; spans?: TextSpan[] } | null>(null);
  // A rename shows until the row's title is written again, by it or after
  // it: the heading then follows the row's title as others save it too.
  useEffect(() => setRenamed(null), [cell?.value, cell?.spans]);
  const shown = renamed?.rowId === rowId ? renamed : null;
  const latest = useRef({ rowId, write });
  latest.current = { rowId, write };
  const held = shown ? shown.title : cell?.value ?? "";
  const heldSpans = shown ? cellSpans(shown.spans, shown.title) : cellSpans(cell?.spans as TextSpan[] | undefined, cell?.value);
  // The row is named everywhere as its page holds it: a title typed in its
  // path's rename or saved here or elsewhere, its icon chosen here.
  useOwnPageName(cell ? rowId : null, { title: String(held), titleSpans: heldSpans, icon: named?.icon, iconType: named?.iconType, updatedAt: named?.updatedAt });
  // A title being typed for the row — here, in its table cell, in a sidebar
  // rename — shows at each key over it, a syllable still composing too, as
  // Notion's row shows it everywhere (seen 2026-11-21 in macOS Chrome, Korean).
  const typed = useTypedPageTitle(cell ? rowId : null, String(held));
  const value = typed ?? held, spans = typed !== undefined ? cellSpans(undefined, typed) : heldSpans;
  return {
    value,
    spans,
    rename(title: string, spans?: TextSpan[]) {
      setRenamed({ rowId, title, spans });
      write?.(title, spans);
    },
    /** The row's title as typed, over the title it was typed on, as the heading keeps it on the device. */
    kept: record ? (change: RichTextCellChange): FieldKept => ({ recordKey: rowValueRecord(rowId, record.propertyId), kind: "rowValue",
      hostPageId: record.hostPageId, row: { rowId, propertyId: record.propertyId, type: "title" },
      value: { title: change.value, ...(change.spans?.length ? { spans: [...change.spans] } : {}) },
      base: change.typedOn ? { type: "title", value: change.typedOn.value, ...(change.typedOn.spans?.length ? { spans: [...change.typedOn.spans] } : {}) } : null })
      : undefined,
    save(change: RichTextCellChange) {
      const { rowId: id, write: send } = latest.current, base = change.typedOn;
      // Refused, the row is named as it was again wherever it showed typed.
      void Promise.resolve(send?.(change.value, change.spans, base ? { title: base.value, spans: base.spans } : null))
        .catch(() => dropTypedPageTitle(id));
    },
  };
}
