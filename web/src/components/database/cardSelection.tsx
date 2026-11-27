import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { pageHref } from "@/lib/navigation";
import { serverUrl } from "@/lib/serverOrigin";
import styles from "./PeekHalo.module.css";
import { ROWS_COPIED_EVENT } from "./viewRowOrder";
import { TABLE_ROWS_RESTORED_EVENT, VIEW_CHOICE_READ_EVENT, type StepRowsChosen, type ViewChoiceRead } from "@/lib/stepRowChoice";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { useViewMarquee } from "./viewMarquee";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { rowPagesClipboard } from "./rowPagesClipboard";
import { LEAFEN_BLOCKS_MIME, copyTextWithBlocks } from "@/lib/clipboard";
import { EDITOR_CLIPBOARD_BOUNDS } from "@/components/editor/editorClipboardPolicy";
import { VIEW_CHOICE_ROOT, chooseAllInView, chooseAllKey, choiceKeyFromControl, choiceStep, keepsViewChoice, useViewChoiceStart } from "./viewChoiceSteps";
import { useViewChoiceJoin } from "./viewChoiceJoin";
import { windowedLines } from "@/lib/useWindowedRows";
import { useKeptChoice } from "@/lib/selectionMemory";
import { usePeekSetsChoiceAside } from "@/lib/peekSetsChoiceAside";
import { openRowMenuByKey, rowMenuChoosesRow } from "./rowMenuByKey";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { escapeForWindowOver } from "./viewWindowEscape";
import { useCarried, useChoiceLetGo } from "@/lib/carriedChoice";
import { chosenAsLines, chosenRowsOf, groupLine, lineRowId } from "./viewGroupLines";
import { pageTookPress } from "@/components/editor/caretShiftPress";
import { useViewChoicePaste } from "./viewChoicePaste";

/**
 * The cards or list rows a view has chosen, as Notion's board and list do
 * (seen 2026-11-03): a card made from "+ 새 작업" and left with Escape is
 * chosen, wearing Notion's halo (a blue ground, rgba(35,131,226,.14)); Escape
 * or a press elsewhere lets it go — not one that keeps a view's choice
 * (keepsViewChoice), the toolbar's "필터" or a filter's pill — and a card a
 * filter hides stays chosen, chosen again once it shows (Notion's board, seen
 * 2026-11-17); Delete or Backspace moves the chosen
 * ones to the trash, as the table's chosen rows go.
 *
 * Shift+press chooses an item, then every item between it and the next one so
 * pressed, in the view's order and across a board's columns. ↑ ↓ move as a
 * table's rows do (viewChoiceSteps.ts): to the item before the first chosen
 * or after the last, Shift with them stretching the choice from where it
 * began (Notion's board, timeline and calendar, seen 2026-11-12: 일 5–일 2
 * chosen, ↑ chooses 일 8), and past an inline view's first or last item to
 * its page's blocks, Shift+↓ joining them (viewChoiceJoin.ts, the page
 * taking the keys while they are); with none chosen they start as
 * useViewChoiceStart says, ↓ after a box from the view's empty room choosing
 * its first item (Notion's
 * board, seen 2026-11-12). Enter opens the
 * item and leaves it chosen; ⌘D copies the chosen ones and chooses the copies;
 * ⌘A chooses every item the view shows, or hands the key to the page around
 * an inline view (chooseAllInView).
 * ← → do nothing (Notion's board, gallery and list, seen 2026-11-05). A card's
 * "⋯" or a list row's handle opens its menu with the item chosen, and it stays
 * chosen once the menu closes (seen 2026-11-05) — the views call `choose`. A
 * right press on a list's row or a gallery's card does the same, alone unless
 * it was among those chosen (menuChoose): Notion's list wears the chosen blue
 * on all its lines while its menu is open (seen 2026-11-24 in two tabs), and
 * its list, gallery and table leave it chosen after Escape; its board's card
 * and timeline's bar are not chosen by it (seen 2026-11-24).
 * ⌘C copies the chosen ones as Notion's does (rowPagesClipboard.ts): the first
 * as a heading, "# 제목", each other as a link to its page, a paragraph apart,
 * and pasted on a page in Leafen, copies of their pages as its sub-pages.
 * An item copied from its menu, or from a right press with none chosen,
 * leaves its copy chosen (Notion's board, seen 2026-11-08), copied from the
 * line the menu was opened on as ⌘D copies it (copyHere). A drag from the
 * view's empty room chooses the items it touches (viewMarquee.tsx). Rows ⌘Z
 * or ⌘⇧Z brings back — a copy's rows it was made from, rows from the trash, a
 * new item made again — are chosen once the view shows them, whole on every
 * line, as a table chooses its own (Notion's list and gallery, seen
 * 2026-11-24 in two tabs: 마 chosen again on both its lines as ⌘Z took its
 * copy; a list row trashed with Delete chosen as ⌘Z brought it back), and
 * so are items a move's ⌘Z puts back in their places; a ⌘⇧Z gives back
 * the lines chosen at its ⌘Z and lets go of the rest (stepRowChoice.ts).
 *
 * A grouped view's item is a row's line under one group (viewGroupLines.ts):
 * its lines are chosen one by one, as Notion's grouped views do, a row chosen
 * on any of them wearing the halo on all, and a row chosen whole (a copy, a
 * menu's) by its id. What they do goes to their rows: each once, a copy for
 * each line (seen 2026-11-24). A board's card is its row's line under the
 * column it stands in (`data-card-group`, cardLine): chosen from its "⋯" in a
 * column, Notion's board wears the halo on all its cards and ⌘D copies it
 * from that column as a list's line (seen 2026-11-24 in two tabs: 마 chosen
 * under y wrote y's cards down first, under x x's).
 */
type CardSelection = { chosen: ReadonlySet<string>; chosenRows: ReadonlySet<string>; root: RefObject<HTMLDivElement | null>; marquee?: ReactNode;
  /** Chooses one item alone, as a press on it does. */
  choose(rowId: string): void;
  /** Chooses the items given, as a range of a timeline's table's cells left with Escape does (useTimelineTableRange). */
  chooseMany(rowIds: readonly string[]): void };

/** An item's line: its own, or a board card's under the column it stands in. */
export function cardLine(element: Element | null | undefined): string | undefined {
  const item = element?.closest<HTMLElement>("[data-card-choice]"), id = item?.dataset.cardChoice;
  const group = item?.closest<HTMLElement>("[data-card-group]")?.dataset.cardGroup;
  return id !== undefined && group !== undefined && lineRowId(id) === id ? groupLine(group, id) : id;
}

/** An item's menu opener, given what its "복제" does instead of the page menu's (useRowContextMenu). */
type ItemMenu = (rowId: string, element: HTMLElement, duplicate?: () => void) => void;

const CardSelectionContext = createContext<CardSelection | null>(null);

export function useCardSelection({ onTrash, onMenu, onOpen, onDuplicate, room, marqueeIn, keep }: {
  /** The view whose choice comes back with the way back or forward (selectionMemory.ts). */
  keep?: string;
  /** Its own parts a drag from its empty room starts from though they are a grid's (viewMarquee.tsx). */
  room?: string;
  /** The part of the view a drag from its empty room starts in, where not all of it (a timeline's canvas, not its table). */
  marqueeIn?: string;
  onTrash?: (rowIds: string[]) => void;
  onMenu?: ItemMenu;
  onOpen?: (rowId: string) => void;
  /** Copies the chosen lines' rows, a row once for each line (copiesPlace), and gives back their copies. */
  onDuplicate?: (lines: string[]) => Promise<readonly string[] | void> | void;
}) {
  const [chosen, setChosen] = useState<ReadonlySet<string>>(new Set());
  const chosenNow = useRef(chosen);
  chosenNow.current = chosen;
  const duplicateNow = useRef(onDuplicate);
  duplicateNow.current = onDuplicate;
  const root = useRef<HTMLDivElement>(null);
  // The item standing for a line (cardLine).
  const itemOf = (line: string) => Array.from(root.current?.querySelectorAll<HTMLElement>(
    `[data-card-choice="${CSS.escape(line)}"], [data-card-choice="${CSS.escape(lineRowId(line))}"]`) ?? []).find((item) => cardLine(item) === line);
  const shown = (rowId: string) => Boolean(itemOf(rowId) ?? root.current?.querySelector(`[data-card-choice-for="${CSS.escape(rowId)}"]`));
  // A peek opened beside or over the view sets them aside while it is open (peekSetsChoiceAside.ts).
  usePeekSetsChoiceAside(root, () => chosenNow.current, setChosen, shown);
  // Where a stretched choice began, and its moving end.
  const ends = useRef<{ anchor: string; head: string } | null>(null);
  const order = () => Array.from(root.current?.querySelectorAll<HTMLElement>("[data-card-choice]") ?? [], (item) => cardLine(item) ?? "");
  // Chosen items drawn only by a part that stands for them (`data-card-choice-for`: a timeline's row of no date, in its table alone).
  const apart = (all: readonly string[]) => [...chosenNow.current].filter((id) => !all.includes(id)
    && root.current?.querySelector(`[data-card-choice-for="${CSS.escape(id)}"]`));
  const stretch = (anchor: string, head: string) => {
    const all = order(), from = all.indexOf(anchor), to = all.indexOf(head);
    ends.current = { anchor, head };
    setChosen(new Set([...apart(all), ...(from < 0 || to < 0 ? [head] : all.slice(Math.min(from, to), Math.max(from, to) + 1))]));
  };
  const chooseOne = (rowId: string) => {
    ends.current = { anchor: rowId, head: rowId };
    setChosen(new Set([rowId]));
  };
  // An item a key reaches is shown and takes the keys that follow.
  const reach = (rowId: string) => {
    const item = itemOf(rowId);
    item?.scrollIntoView({ block: "nearest", inline: "nearest" });
    item?.focus({ preventScroll: true });
  };
  const ownPage = useDatabaseOwnPage();
  // Every item it shows: a long list's undrawn too (useWindowedRows `lines`), and a timeline's rows of no date.
  const chooseAll = () => {
    ends.current = null;
    setChosen(new Set([...windowedLines(root.current) ?? order(), ...Array.from(root.current?.querySelectorAll<HTMLElement>("[data-card-choice-for]") ?? [],
      (item) => item.dataset.cardChoiceFor ?? "")].filter(Boolean)));
  };
  // From a view tab's key the tab keeps the focus, as Notion's list kept the ring on its tab (viewTabChoice.ts).
  const start = useViewChoiceStart({ root, order, empty: !chosen.size, ownPage, afterBox: true, chooseAll,
    head: () => chosenNow.current.size ? ends.current?.head ?? [...chosenNow.current][0] : undefined,
    choose: (rowId, fromTab) => { chooseOne(rowId); if (fromTab) itemOf(rowId)?.scrollIntoView({ block: "nearest", inline: "nearest" }); else reach(rowId); } });
  // Escape lets them go, ↓ then starting from the one it reached (viewChoiceSteps.ts); so does a drag of them given up with it (carriedChoice.ts).
  const letGo = () => { start.escaped(ends.current?.head ?? [...chosenNow.current][0]); setChosen(new Set()); };
  useChoiceLetGo(() => { if (chosenNow.current.size) letGo(); });
  // A paste of blocks with them chosen goes on the page after the database (viewChoicePaste.ts).
  useViewChoicePaste(root, !!onTrash && chosen.size > 0, () => setChosen(new Set()));
  const join = useViewChoiceJoin({ root, order, chosen, ends, clear: () => setChosen(new Set()),
    trash: () => { onTrash?.([...chosenRowsOf(chosenNow.current)]); setChosen(new Set()); },
    take: (ids, next, own) => { ends.current = next; setChosen(new Set(ids)); if (own) reach(next.head); else itemOf(next.head)?.scrollIntoView({ block: "nearest", inline: "nearest" }); } });
  // A box's choice stretches from the item nearest where it began (viewMarquee.tsx).
  const chooseMany = (rowIds: readonly string[]) => {
    const first = rowIds[0];
    ends.current = first ? { anchor: first, head: rowIds[rowIds.length - 1] } : null;
    setChosen(new Set(rowIds));
  };
  // Shift+press: not the item's opening, and not the page's text selected — but in a field being written in the item,
  // and the page's when its text around an inline view holds the caret (caretShiftPress.ts), which chooses its blocks.
  useEffect(() => {
    const within = (event: MouseEvent) => {
      if (pageTookPress(event)) return null;
      const target = event.target as HTMLElement | null;
      // Text being written in keeps the press; text only awake under the pointer, not written in (a timeline table cell's, RichTextCellShown), does not.
      // Only the item's own editable text counts: an inline view stands in its page's editor behind a
      // contenteditable="false" frame, and the page's text around it, holding the focus, is not the item's.
      const text = target?.isContentEditable ? target.closest<HTMLElement>('[contenteditable]:not([contenteditable="false"])') : null;
      if (!event.shiftKey || event.button !== 0 || text?.contains(document.activeElement) || target?.closest("input, textarea, select")) return null;
      // An item, or a part that stands for one (a timeline's table row: its name and its cells, as a table's row).
      const item = target?.closest<HTMLElement>("[data-card-choice], [data-card-choice-for]");
      return item && root.current?.contains(item) ? cardLine(item) ?? item.dataset.cardChoiceFor ?? null : null;
    };
    const down = (event: MouseEvent) => { if (within(event)) event.preventDefault(); };
    const click = (event: MouseEvent) => {
      const rowId = within(event);
      if (!rowId) return;
      event.preventDefault();
      event.stopPropagation();
      // One not among the items in order (a timeline's row of no date, which has no bar) joins the choice as it
      // is, and stays through the stretches after (Notion's timeline table, seen 2026-11-24 in two tabs: 가
      // chosen, Shift+바 of no date — its name or a cell — leaves 바 and 가; 바 and 나, Shift+마 leaves 바 and 마–나).
      if (!order().includes(rowId)) { setChosen(new Set([...chosenNow.current, rowId])); return; }
      // Within what is chosen it stretches from the far end it last reached
      // (Notion's, seen 2026-11-12: 일 7–일 3 chosen, Shift+일 5 leaves 일 7–일 5).
      const reached = ends.current;
      if (reached && chosenNow.current.has(rowId) && reached.head !== rowId) stretch(reached.head, rowId);
      else if (reached?.anchor) stretch(reached.anchor, rowId); else chooseOne(rowId);
      // The keys follow it, even from a page's text around an inline view.
      (event.target as HTMLElement).closest<HTMLElement>("[data-card-choice]")?.focus({ preventScroll: true });
    };
    document.addEventListener("mousedown", down, true);
    document.addEventListener("click", click, true);
    return () => {
      document.removeEventListener("mousedown", down, true);
      document.removeEventListener("click", click, true);
    };
  }, []);
  useEffect(() => {
    if (!chosen.size) { ends.current = null; return; }
    // On a chosen item, or on the handle beside a chosen list row, where its menu gives focus back.
    const rows = chosenRowsOf(chosen), onChosen = (target: HTMLElement) => rows.has(lineRowId(target.closest<HTMLElement>("[data-card-choice]")?.dataset.cardChoice
      ?? target.closest<HTMLElement>("[data-card-handle-for]")?.dataset.cardHandleFor ?? ""));
    // What copying them gives: each row once, though a calendar draws a card going on into the next week twice, a grouped view a row under each of its groups.
    const chosenPages = (cut = false) => {
      const rowOf = (item: HTMLElement) => lineRowId(item.dataset.cardChoice ?? "");
      const items = Array.from(root.current?.querySelectorAll<HTMLElement>("[data-card-choice]") ?? [])
        .filter((item, index, all) => rows.has(rowOf(item)) && all.findIndex((other) => rowOf(other) === rowOf(item)) === index);
      return items.length ? rowPagesClipboard(items.map((item) => ({ id: rowOf(item), title: item.dataset.cardTitle ?? "", href: serverUrl(pageHref(rowOf(item))) })), cut) : null;
    };
    const keys = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      // Only with focus on the page, a chosen item or a control of the view's that keeps them
      // chosen (choiceKeyFromControl, as a table's rows): a field, a menu or a peek keeps its keys.
      const typing = Boolean(target?.isContentEditable || target?.closest("input, textarea, select"));
      const onPage = !target || target === document.body || target === document.documentElement || (!typing && onChosen(target))
        || choiceKeyFromControl(target, event.key, root.current);
      if (!onPage || join.joinedNow() || event.defaultPrevented || imeComposing(event) || event.altKey) return;
      if (chooseAllKey(event)) {
        event.preventDefault();
        event.stopPropagation();
        chooseAllInView(root.current, ownPage, chooseAll, () => setChosen(new Set()));
        return;
      }
      // ⌘/ opens its menu, as a chosen row's does: one chosen, its page
      // item menu (rowMenuByKey.ts).
      if (primaryModifier(event) && event.key === "/" && onMenu) {
        const rowId = [...chosen][0];
        const item = itemOf(rowId);
        if (item) {
          event.preventDefault();
          if (chosen.size === 1) openRowMenuByKey(() => onMenu(lineRowId(rowId), item, copyHere(item, lineRowId(rowId))));
          else onMenu(lineRowId(rowId), item, copyHere(item, lineRowId(rowId)));
        }
        return;
      }
      // ⌘X copies them as cut and moves them to the trash, one ⌘Z bringing them back chosen, as Notion's (seen 2026-11-27, a gallery and a table);
      // pasted on a page, they leave the trash and their database for it (cutPagePaste.ts).
      if (primaryModifier(event) && !event.shiftKey && chordLetter(event) === "x" && onTrash && !window.getSelection()?.toString()) {
        event.preventDefault();
        event.stopPropagation();
        const copied = chosenPages(true);
        if (copied) void copyTextWithBlocks(copied.text, copied.blocks, copied.html, EDITOR_CLIPBOARD_BOUNDS);
        onTrash([...rows]);
        setChosen(new Set());
        return;
      }
      if (primaryModifier(event) && chordLetter(event) === "d" && onDuplicate) {
        event.preventDefault();
        event.stopPropagation();
        void Promise.resolve(onDuplicate([...chosen])).then((copies) => { if (copies?.length) chooseMany(copies); });
        return;
      }
      if (event.metaKey || event.ctrlKey) return;
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        const step = choiceStep(order(), chosen, ends.current, event.key === "ArrowDown", event.shiftKey);
        event.preventDefault();
        event.stopPropagation();
        // Past the first or last item an inline view's page takes the step on.
        if (step === "out") { join.out(event.key === "ArrowDown", event.shiftKey); return; }
        if (!step) return;
        ends.current = step.ends;
        setChosen(new Set(step.ids));
        reach(step.ends.head);
        return;
      }
      if (fieldEnter(event) && onOpen) {
        event.preventDefault();
        event.stopPropagation();
        onOpen(lineRowId(ends.current?.head ?? [...chosen][0]));
        return;
      }
      // Escape lets them go and goes on to whatever else it closes; a window open over the view takes it alone (viewWindowEscape.ts).
      if (event.key === "Escape") { if (!escapeForWindowOver(root.current, event)) letGo(); }
      else if ((event.key === "Delete" || event.key === "Backspace") && onTrash) {
        event.preventDefault();
        onTrash([...rows]);
        setChosen(new Set());
      }
    };
    // A press anywhere but on a chosen item lets them go, but one that keeps
    // a view's choice, as a table's rows (keepsViewChoice: a toolbar control
    // that opens a window, a filter's pill, a menu or dialog over the view).
    // A press in an item's own menu still lets them go, as it did: the item
    // it trashes leaves no choice behind (the views keep no list of theirs here).
    const press = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (event.shiftKey || (target && keepsViewChoice(target) && !target.closest("[data-row-menu-variant]"))) return;
      // On a chosen item, or on the handle beside a chosen list row, which drags them all (useDragReorder's `carry`), they stay.
      if (!target || !onChosen(target as HTMLElement)) setChosen(new Set());
    };
    // ⌘C on the page while they're chosen: the browser's own copy, written here.
    const copy = (event: ClipboardEvent) => {
      const target = document.activeElement as HTMLElement | null;
      const onPage = !target || target === document.body || onChosen(target) || choiceKeyFromControl(target, "c", root.current);
      const copied = chosenPages();
      if (!onPage || !event.clipboardData || !copied || window.getSelection()?.toString()) return;
      // Before the page editor around an inline view writes its own selection.
      event.preventDefault();
      event.stopPropagation();
      event.clipboardData.setData("text/plain", copied.text);
      event.clipboardData.setData("text/html", copied.html);
      event.clipboardData.setData(LEAFEN_BLOCKS_MIME, copied.blocks);
    };
    document.addEventListener("keydown", keys, true);
    document.addEventListener("pointerdown", press, true);
    document.addEventListener("copy", copy, true);
    return () => {
      document.removeEventListener("keydown", keys, true);
      document.removeEventListener("pointerdown", press, true);
      document.removeEventListener("copy", copy, true);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chosen, onDuplicate, onMenu, onOpen, onTrash, ownPage]);
  // A row chosen whole, as its layout switch carries it (selectionMemory.ts), is shown by any of its lines and comes back as the
  // first of them (chosenAsLines), so a Shift+press stretches from it as from a choice made here (chooseMany) —
  // taken after the effect above lets the ends of none chosen go, as the view first shows.
  const lines = () => windowedLines(root.current) ?? order();
  const rowShown = (id: string) => shown(id) || (lineRowId(id) === id && lines().some((line) => lineRowId(line) === id));
  useKeptChoice(keep ? `cards:${keep}` : null, chosen, (ids) => chooseNow.current.many([...chosenAsLines(ids, lines())]), rowShown,
    () => Boolean(root.current?.querySelector("[data-card-choice]")), false, keep);
  useEffect(() => {
    const copied = (event: Event) => {
      const { from = [], copies = [] } = (event as CustomEvent<{ from?: readonly string[]; copies?: readonly string[] }>).detail ?? {};
      const items = Array.from(root.current?.querySelectorAll<HTMLElement>("[data-card-choice]") ?? [], (item) => lineRowId(item.dataset.cardChoice ?? ""));
      const rows = chosenRowsOf(chosen), copied = new Set(from), here = from.some((id) => items.includes(id));
      if (copies.length && (chosen.size ? copied.size === rows.size && [...copied].every((id) => rows.has(id)) : here)) chooseMany(copies);
    };
    window.addEventListener(ROWS_COPIED_EVENT, copied);
    return () => window.removeEventListener(ROWS_COPIED_EVENT, copied);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chosen]);
  // Rows or lines a step names (stepRowChoice.ts), chosen once this view draws one of them; `only`, the others let go at once.
  const [brought, setBrought] = useState<readonly string[]>([]);
  useEffect(() => {
    const restored = (event: Event) => {
      const { rowIds = [], only = false } = (event as CustomEvent<StepRowsChosen>).detail ?? {};
      if (only) setChosen(new Set());
      setBrought(rowIds);
    };
    const read = (event: Event) => { (event as CustomEvent<ViewChoiceRead>).detail?.chosen.push(...chosenNow.current); };
    window.addEventListener(TABLE_ROWS_RESTORED_EVENT, restored);
    window.addEventListener(VIEW_CHOICE_READ_EVENT, read);
    return () => {
      window.removeEventListener(TABLE_ROWS_RESTORED_EVENT, restored);
      window.removeEventListener(VIEW_CHOICE_READ_EVENT, read);
    };
  }, []);
  useEffect(() => {
    if (!brought.length) return;
    const shownRows = new Set(order().map(lineRowId)), here = brought.filter((id) => shownRows.has(lineRowId(id)));
    if (!here.length) return;
    setBrought([]);
    chooseMany(here);
  });
  const marqueeRoot = useMemo(() => marqueeIn ? { get current() { return root.current?.querySelector<HTMLElement>(marqueeIn) ?? null; } } : root, [marqueeIn]);
  const marquee = useViewMarquee({ root: marqueeRoot, item: "[data-card-choice]", room, idOf: (item) => cardLine(item),
    chosen: () => chosenNow.current, choose: (rowIds) => { if (rowIds.length) chooseMany(rowIds); else setChosen(new Set()); },
    onBox: start.boxed });
  const chooseNow = useRef({ one: chooseOne, many: chooseMany });
  chooseNow.current = { one: chooseOne, many: chooseMany };
  // An item's menu's "복제" copies the item from the line it was opened on —
  // its item's, the list row's handle's, or a board card's under the column
  // it stands in (`data-card-group`) — as ⌘D copies the chosen lines
  // (copiesPlace), and chooses the copies: Notion's list, gallery, timeline
  // and board, on 마 shown under x and y in a view no row of which was placed
  // (seen 2026-11-24 in two tabs: from y's 마, y's rows were written first and
  // the copy went before 다; from x's, x's rows and before 라; "마 (1)" chosen
  // on both its lines, ⌘Z taking the copy and the order back). Without a way
  // of the view's to copy, the page menu's own "복제" stays.
  function copyHere(element: Element | null | undefined, rowId: string): (() => void) | undefined {
    if (!duplicateNow.current) return undefined;
    const item = element?.closest<HTMLElement>("[data-card-choice], [data-card-choice-for]");
    const group = item?.closest<HTMLElement>("[data-card-group]")?.dataset.cardGroup;
    const line = group !== undefined ? groupLine(group, rowId)
      : item?.dataset.cardChoice ?? item?.dataset.cardChoiceFor ?? (element instanceof HTMLElement ? element.dataset.cardHandleFor : undefined);
    return () => void Promise.resolve(duplicateNow.current?.([line && lineRowId(line) === rowId ? line : rowId]))
      .then((copies) => { if (copies?.length) chooseMany(copies); });
  }
  // A right press's item menu: its item chosen alone, unless its row was chosen with others; none on a phone (rowMenuChoosesRow).
  function menuChoose(element: Element) {
    const line = cardLine(element);
    if (line && !chosenRowsOf(chosenNow.current).has(lineRowId(line)) && rowMenuChoosesRow()) chooseOne(line);
  }
  const value = useMemo<CardSelection>(() => ({ chosen, chosenRows: chosenRowsOf(chosen), root, marquee, choose: (rowId) => chooseNow.current.one(rowId),
    chooseMany: (rowIds) => chooseNow.current.many(rowIds) }), [chosen, marquee]);
  return { value, choose: chooseOne, chooseMany, copyHere, menuChoose };
}

/** Chooses one of a view's items alone, from what stands for it elsewhere (TimelineOffRangeMarkers). */
export function useChooseCard(): ((rowId: string) => void) | undefined {
  return useContext(CardSelectionContext)?.choose;
}

export function useChooseCards(): ((rowIds: readonly string[]) => void) | undefined {
  return useContext(CardSelectionContext)?.chooseMany;
}

/** The rows a view's items have chosen, read by what marks them elsewhere (TimelineRangePill). */
export function useChosenCards(): ReadonlySet<string> | undefined {
  return useContext(CardSelectionContext)?.chosenRows;
}

/** Gives a view's items what it has chosen. */
export function CardSelectionProvider({ value, children }: { value: CardSelection; children: ReactNode }) {
  return <CardSelectionContext.Provider value={value}><div ref={value.root} style={{ display: "contents" }} {...{ [VIEW_CHOICE_ROOT]: "" }}>{children}{value.marquee}</div></CardSelectionContext.Provider>;
}

/**
 * The halo a chosen card or list row wears; its item is positioned and marked
 * `data-card-choice`. A timeline's bar wears it 2px in, its row in the table
 * beside it 2px in over a 32px line, and a calendar's card 1px in, rounded
 * 6px (Notion's, seen 2026-11-12). Not drawn while a drag carries the item,
 * which stays chosen (carriedChoice.ts).
 */
export function CardChoiceHalo({ itemId, inset }: { itemId: string; inset?: number | string }) {
  const chosen = useContext(CardSelectionContext)?.chosenRows.has(lineRowId(itemId));
  const carried = useCarried(lineRowId(itemId));
  return chosen ? <span className={styles.chosen} data-card-chosen aria-hidden="true" hidden={carried || undefined}
    style={inset === undefined ? undefined : { inset, borderRadius: 6 }} /> : null;
}
