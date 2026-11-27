import { useEffect, useRef, useState } from "react";
import { dropTypedPageTitle, useOwnPageName, useTypedPageTitle } from "@/lib/pageNames";
import { pageLeaving } from "@/lib/pageLeaving";
import { writeRowTitle } from "@/lib/rowTitleSave";
import { sendAsPageGoes } from "@/lib/writesAsPageGoes";
import type { TitleValue } from "@/lib/titleMerge";
import type { TextSpan } from "@/lib/types";
import type { Session } from "@/types";
import type { RichTextCellChange } from "./RichTextCellEditor";
import { cellSpans } from "./richTextCellModel";

type Cell = { value?: unknown; spans?: unknown } | undefined;
/**
 * Writes the row's title; given the title the server held (`read`), only
 * over that one, failing with the server's refusal when another was saved
 * since (rowTitleSave.ts). One that can't hold the server to it writes it.
 */
export type RowTitleWrite = (value: string, spans?: TextSpan[], read?: TitleValue | null) => Promise<unknown> | void;

/**
 * A row page's title (DatabaseRowPage): the heading shows a rename from the
 * path's popover as it is typed, then the row's title as it is saved, here
 * or elsewhere; what is typed in the heading is written one save after
 * another, merged with the title the server holds then and written only
 * over that one (rowTitleSave.ts) — as typed, the spaces at its ends too
 * (titleMerge.ts).
 */
export function useRowPageTitle({ rowId, cell, session, write, named }: {
  rowId: string;
  cell: Cell;
  /** The row's icon and last edit as its page holds them, which name it everywhere with its title (useOwnPageName). */
  named?: { icon?: string; iconType?: string; updatedAt?: string };
  /** Who reads the server's title when a save goes; none writes as typed. */
  session: Session | undefined;
  write: RowTitleWrite | undefined;
}) {
  const [renamed, setRenamed] = useState<{ rowId: string; title: string; spans?: TextSpan[] } | null>(null);
  // A rename shows until the row's title is written again, by it or after
  // it: the heading then follows the row's title as others save it too.
  useEffect(() => setRenamed(null), [cell?.value, cell?.spans]);
  const shown = renamed?.rowId === rowId ? renamed : null;
  const latest = useRef({ rowId, session, write });
  latest.current = { rowId, session, write };
  const queue = useRef(Promise.resolve());
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
    save(change: RichTextCellChange) {
      const { rowId: id, session: reader, write: send } = latest.current;
      const typed = { value: change.value, spans: change.spans };
      // A window going away writes at once: a read first might not finish.
      if (!send || !reader || !change.typedOn || document.visibilityState === "hidden" || pageLeaving()) {
        send?.(typed.value, typed.spans);
        return;
      }
      const base = change.typedOn;
      // Waiting behind an earlier save as the page goes, it goes at once
      // then; one on its way goes again (writesAsPageGoes.ts).
      const going = sendAsPageGoes(() => send(typed.value, typed.spans));
      queue.current = queue.current.then(() => {
        going();
        return writeRowTitle(reader, id, { title: typed.value, spans: typed.spans },
          { title: base.value, spans: base.spans }, async (title, read) => send(title.title, title.spans ? [...title.spans] : undefined, read));
      })
        // Refused, the row is named as it was again wherever it showed typed.
        .then(() => undefined, () => dropTypedPageTitle(id));
    },
  };
}
