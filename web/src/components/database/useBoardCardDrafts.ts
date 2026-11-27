import { useRef, useState } from "react";
import type { GraphRow } from "../../contentGraph";
import type { BoardDraft } from "./BoardColumn";

/**
 * A board's cards being made in place: the group (or a sub-group's cell)
 * whose "+ 새 페이지" or head "+" card is being named, the row each stands
 * for, made as it is pressed, and the cards made one after another with
 * Enter (BoardView).
 */
export function useBoardCardDrafts({ addRow, laneRows, draftTitle, titlePropertyId, writeTitle, templateIcon, choose, openRow, openRowMenu }: {
  /** Makes a card's row in a group (BoardView's addRow): `before` the card it goes before. */
  addRow(groupKey: string, title: string, laneKey?: string, atTop?: boolean, before?: string): Promise<GraphRow | null>;
  /** A column's cards, or a sub-group's cell's, in order. */
  laneRows(groupKey: string, laneKey?: string): readonly { id: string }[];
  /** The title a card made in a group starts with: a title's group's own (undefined where cards start untitled). */
  draftTitle(groupKey: string): string | undefined;
  /** The database's title property, whose value a row made from a template comes with. */
  titlePropertyId?: string;
  /** Writes a card's name, typed in it, to its row. */
  writeTitle(rowId: string, title: string): void;
  /** The icon of the template new cards are made from. */
  templateIcon?: string;
  /** Chooses a card left with Escape. */
  choose(rowId: string): void;
  openRow(rowId: string): void;
  openRowMenu(rowId: string, element: HTMLElement): void;
}) {
  // The group whose "+ 새 페이지" card is being named.
  const [draftGroup, setDraftGroup] = useState<string | null>(null);
  const [draftSerial, setDraftSerial] = useState(0);
  // The row a card being made stands for, made as its "+" is pressed.
  const draftRow = useRef<Promise<GraphRow | null> | null>(null);
  const [draftRowId, setDraftRowId] = useState<string | null>(null);
  const [draftMadeTitle, setDraftMadeTitle] = useState("");
  // Where the cards being made go: the head's "+" puts them at the group's
  // top, before the card first there as it was pressed; "+ 새 페이지" at its
  // end. Each after the first (Enter's) is made only once named, the card
  // before it chosen if it is left empty (Notion's, seen 2026-11-06).
  const draftPlace = useRef<{ before?: string; chained: boolean }>({ chained: false });
  const previousDraft = useRef<Promise<GraphRow | null> | null>(null);
  // Cards made one after another with Enter are written in turn, none dropped.
  const draftWrites = useRef<Promise<unknown>>(Promise.resolve());
  const latestAddRow = useRef(addRow);
  latestAddRow.current = addRow;
  // A draft card sits in one column, or in one sub-group's cell of it.
  const draftKey = (groupKey: string, laneKey?: string) => laneKey === undefined ? groupKey : `${groupKey}\n${laneKey}`;
  function makeDraftRow(groupKey: string, title: string, laneKey?: string) {
    const { before } = draftPlace.current;
    const made = draftWrites.current.then(() => latestAddRow.current(groupKey, title, laneKey, false, before));
    draftWrites.current = made.catch(() => null);
    return made;
  }
  // "+ 새 페이지" makes its row at once, counted and seen by others while its
  // name is typed, which is written apart; left empty, the row stays (Notion's,
  // seen 2026-11-04: the row there before a key, still there after Escape or
  // a press elsewhere). The card being made stands in for it meanwhile.
  function startDraftRow(groupKey: string, laneKey?: string) {
    const made = makeDraftRow(groupKey, draftTitle(groupKey) ?? "", laneKey);
    draftRow.current = made;
    setDraftRowId(null);
    setDraftMadeTitle("");
    void made.then((row) => {
      if (draftRow.current !== made) return;
      setDraftRowId(row?.id ?? null);
      // Its template's title shows in it, as Notion's card (seen 2026-11-05).
      const name = titlePropertyId && row?.values[titlePropertyId]?.value;
      setDraftMadeTitle(typeof name === "string" ? name : "");
    });
    return made;
  }
  // A name is written by the view as it is then, after the next card's row:
  // a row made while another write lands comes back without its id.
  const latestNameRow = useRef(writeTitle);
  latestNameRow.current = writeTitle;
  function startDraft(groupKey: string, laneKey?: string, atTop = false) {
    setDraftGroup(draftKey(groupKey, laneKey));
    draftPlace.current = { before: atTop ? laneRows(groupKey, laneKey)[0]?.id : undefined, chained: false };
    previousDraft.current = null;
    startDraftRow(groupKey, laneKey);
  }
  // Whatever ends the card, its row keeps the name typed.
  // `keepEmpty`: an empty card Enter started is made anyway, to be opened.
  function endDraft(groupKey: string, laneKey: string | undefined, title: string, next = false, keepEmpty = false) {
    const named = title !== (draftTitle(groupKey) ?? "");
    const lazy = !draftRow.current && draftPlace.current.chained;
    const made = draftRow.current ?? (lazy && (named || keepEmpty) ? makeDraftRow(groupKey, named ? title : draftTitle(groupKey) ?? "", laneKey) : Promise.resolve(null));
    const previous = previousDraft.current;
    if (next) {
      setDraftSerial((serial) => serial + 1);
      previousDraft.current = made; draftPlace.current = { ...draftPlace.current, chained: true };
      draftRow.current = null; setDraftRowId(null); setDraftMadeTitle("");
    } else { setDraftGroup(null); draftRow.current = null; setDraftRowId(null); previousDraft.current = null; }
    return made.then(async (row) => {
      if (!row) return lazy && previous ? previous.catch(() => null) : null;
      if (named && !lazy) {
        await new Promise((resolve) => requestAnimationFrame(resolve));
        latestNameRow.current(row.id, title);
      }
      return row;
    });
  }
  function draftFor(groupKey: string, laneKey?: string): BoardDraft {
    return {
      serial: draftSerial,
      before: draftPlace.current.before,
      rowId: draftRowId ?? undefined,
      // A slot pressed on a card Enter started makes its row first.
      ensureRow: async () => {
        if (!draftRow.current) startDraftRow(groupKey, laneKey);
        const row = await (draftRow.current ?? Promise.resolve(null)).catch(() => null);
        return row?.id ?? null;
      },
      title: draftTitle(groupKey),
      madeTitle: draftMadeTitle,
      templateIcon,
      onDone: (title, next, escaped) => void endDraft(groupKey, laneKey, title, next).then((row) => { if (row && escaped) choose(row.id); }),
      onOpen: (title) => void endDraft(groupKey, laneKey, title, false, true).then((row) => { if (row) openRow(row.id); }),
      onMenu: (title, element) => void endDraft(groupKey, laneKey, title, false, true).then((row) => { if (row) openRowMenu(row.id, element); }),
    };
  }
  return { draftGroup, draftRowId, draftKey, startDraft, draftFor };
}
