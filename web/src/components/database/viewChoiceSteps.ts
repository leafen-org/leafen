import { useEffect, useRef, type RefObject } from "react";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { escapeClosing } from "@/lib/overlayLayer";
import { SIDE_PEEK } from "@/lib/peekBesideEscape";
import { holdsPlaced } from "@/lib/popoverLayer";
import { lineRowId } from "./viewGroupLines";

/** Asks a database view, from the page around it, to choose its first or last item; cancelled when it did. */
export const VIEW_CHOICE_IN = "leafen:view-choice-in";
/** Tells the page around a database view that ↑ or ↓ went past its first or last item; cancelled when the page took it. */
export const VIEW_CHOICE_OUT = "leafen:view-choice-out";
/** Hands ⌘A from an inline database's view to the page around it, which chooses all its blocks; cancelled when it did. */
export const VIEW_CHOICE_PAGE_ALL = "leafen:view-choice-page-all";
export type ViewChoiceIn = { edge: "first" | "last" };
/** `shift`: Shift+↑ or ↓ past it, the page joining its next block to the items chosen going down (viewChoiceJoin.ts). */
export type ViewChoiceOut = { down: boolean; shift?: boolean };
/**
 * Asks what a press on a database's view tab lets go, as the press goes
 * down (viewTabChoice.ts): the view inside, then the page's editor around
 * it. One with a choice lets it go and says where it ended (`at`): a row's
 * id, or a block's place.
 */
export const VIEW_TAB_PRESSED = "leafen:view-tab-pressed";
/**
 * Asks the view inside a database, or the page's editor around it, to choose
 * from a view tab's ↑ or ↓: again where a press on a view tab let its choice
 * go (`at`), or the page's first or last block (`edge`); cancelled when it did.
 */
export const VIEW_TAB_CHOOSE = "leafen:view-tab-choose";
export type ViewTabPressed = { at?: string };
export type ViewTabChoose = { at?: string; edge?: "first" | "last" };
/** Marks the element a view's choice events go to and come from. */
export const VIEW_CHOICE_ROOT = "data-view-choice";

/** Where a stretched choice began, and its moving end. */
export type ChoiceEnds = { anchor: string; head: string };

/**
 * Where ↑ or ↓ takes a view's chosen items: the item before the first chosen
 * or after the last, or with Shift the choice stretched from where it began
 * to one past its moving end; "out" past the view's first or last item, its
 * moving end there with Shift.
 */
export function choiceStep(all: readonly string[], chosen: ReadonlySet<string>, ends: ChoiceEnds | null, down: boolean, shift: boolean)
  : { ends: ChoiceEnds; ids: string[] } | "out" | null {
  if (shift) {
    // Chosen otherwise than by these keys, it stretches from the first chosen to the last.
    const list = [...chosen], kept = ends && chosen.has(ends.head) && all.includes(ends.anchor) ? ends : null;
    const head = kept?.head ?? list[list.length - 1], at = all.indexOf(head ?? "");
    const next = at < 0 ? undefined : all[at + (down ? 1 : -1)];
    if (!next) return at < 0 ? null : "out";
    const anchor = kept?.anchor ?? list[0]!;
    const from = all.indexOf(anchor), to = all.indexOf(next);
    if (from < 0) return { ends: { anchor: next, head: next }, ids: [next] };
    return { ends: { anchor, head: next }, ids: all.slice(Math.min(from, to), Math.max(from, to) + 1) };
  }
  const at = [...chosen].map((id) => all.indexOf(id)).filter((index) => index >= 0);
  if (!at.length) return null;
  const next = all[down ? Math.max(...at) + 1 : Math.min(...at) - 1];
  return next ? { ends: { anchor: next, head: next }, ids: [next] } : "out";
}

/**
 * The presses that keep a view's chosen rows or cards, besides its own: a
 * database toolbar's control that opens a window — "필터" (its bar shown or
 * hidden too), "정렬" (the bar's sort window opened with it too), "⚡", the
 * settings, "새로 만들기 ⌄" — and its filter bar's filter pills, the windows
 * they open being menus and dialogs of their own, as Notion's (seen
 * 2026-11-17 at 1440, a table's row ticked: "1개 선택됨" and the tick stay
 * through each of those presses, a filter's value picked and Escape —
 * "필터" and a pill twice, in two tabs; the toolbar's search, the bar's
 * "+ 필터" (twice), a column's head and the page around let them go; a card
 * chosen in a board, the same through each, a filter's "필터 제거" and the
 * automation window's "취소" too, and a gallery's, a list's and a calendar's
 * through "필터", a pill and the settings, their search and a calendar's
 * "오늘" letting it go). The bar's sort pill is not one: pressed, it lets
 * them go as it opens its window, while "정렬" opening that same window keeps
 * them (seen 2026-11-17 at 1440 in two tabs at different times, "↑ 점수" and
 * "↑ 상태 / 정렬 2개": "1개 선택됨" gone at the pill's press, kept at the
 * filter pill's and at "정렬"'s). So do the view bar's: "N개 더 보기" and the
 * window of views it opens, and the chosen tab opening its menu — a table's
 * chosen cell drawn on under either window, and still chosen with the ring
 * back on the tab once the tab's menu closed, ↓ then choosing the cell under
 * it (seen 2026-11-26 in Chrome at 1440 in two tabs at different times,
 * judged after the press only; the ↓ once); a ticked row kept through the
 * tab's menu too (seen once). Another tab's press chooses another view,
 * leaving the choice behind (useChosenCellOutlivesRow.ts).
 */
export const VIEW_CHOICE_KEEPING_PRESS = "[data-database-toolbar] [aria-haspopup], "
  + "[data-quick-filter-bar] :is([data-quick-filter], [data-quick-advanced-filter]), "
  + "[data-view-tabs] :is([data-view-tab], [data-view-overflow-wrap] button)[aria-haspopup], [data-view-overflow-menu]";

/**
 * The view bar's "+" (새 보기 추가), by the tabs or by a lone view's title,
 * and its window: a press there keeps a view's choice as the presses above
 * do — ticked rows, and a table's chosen cell drawn on while the window is
 * open, still chosen once Escape or a press beside it closed the window, ↓
 * then choosing the cell under it and a letter going into it
 * (chosenCellHeld.ts) — as Notion's whose window has no field to describe a
 * view to its AI (seen 2026-11-26 and 2026-11-27 in Chrome at 1440 under
 * a Notion test page, a Plus workspace, judged after the press only: by the
 * tabs and by a lone view's title, in two tabs at different times). Where
 * Notion's AI is on (its business trial, seen 2026-11-26), that window opens
 * with the caret in its "또는 보기를 설명하세요..." field and the cell let go;
 * Leafen has no built-in AI, so its window is the one without the field. Not
 * one of the controls the chosen items' keys go on from
 * (VIEW_CHOICE_KEEPING_PRESS): it takes no focus as it is pressed.
 */
export const VIEW_ADD_PRESS = "[data-add-view-opener], [data-add-view-menu]";

/**
 * Whether a press keeps a view's choice: one of VIEW_CHOICE_KEEPING_PRESS
 * or VIEW_ADD_PRESS, one in a menu, dialog or list open over the view, a
 * row's "열기" in the middle or beside, or one beside a menu or window
 * open on top — a row's center peek, a template's being edited, the
 * automation window, a toolbar's menu, the view bar's menus — that closes
 * only it (pressBesideWindow). A press beside one of the view's own windows
 * (필터, 정렬, ⚡, 설정, 새로 만들기 ⌄, the chosen tab's menu, "N개 더 보기",
 * "+") closes it and keeps a chosen cell, ticked rows and chosen cards, ↓ and
 * a letter going on to them, as Notion's (seen 2026-11-27 in Chrome at 1440,
 * judged after the press only, a press on the page's margin beside each
 * window: the cell in two tabs at different times, the rows in two, a card
 * once). As Notion's (seen 2026-11-18 at 1440 in two tabs at different
 * times, judged after the press only: a row ticked, another row's "열기"
 * opening its center peek and a template's window opened from "새로 만들기
 * ⌄", each closed with a press beside it, "1개 선택됨" stayed; closed with
 * Escape too, the template's in one tab and the peek's in the other). The
 * peek a row's "열기" opens, in the middle or beside, sets them aside while it
 * is open and gives them back as it closes (peekSetsChoiceAside.ts). Any press
 * while an item's menu is open closes only the menu: a right press's on a
 * list's row, a gallery's card or a table's row, closed by a press on the
 * page's margin, left the item chosen and focused (seen 2026-11-24 at 1685
 * in two tabs at different times; chosenItemFocus.ts).
 */
export function keepsViewChoice(target: Element) {
  return Boolean(target.closest(`[role="menu"], [role="dialog"], [role="listbox"], [data-row-peek-backdrop], [data-table-row-open="center"], [data-table-row-open="side"], ${VIEW_CHOICE_KEEPING_PRESS}, ${VIEW_ADD_PRESS}`)
    || itemMenuDrawn(target.ownerDocument) || pressBesideWindow(target));
}

// A press beside the menu or window Escape would close now — a menu, dialog or list; not a side peek, beside which the
// page goes on taking presses — that closes only it: on its shade, the page's shield (pageHoverShield.ts) or the page
// itself. A view tab's press still chooses its view, letting the choice go as any other tab's does (viewTabChoice.ts).
function pressBesideWindow(target: Element) {
  if (target.closest("[data-view-tab]")) return false;
  const top = escapeClosing({ target }), window = '[role="menu"], [role="dialog"], [role="listbox"]';
  // Its box may only hold the window, deeper in: "N개 더 보기" draws its list of views inside a plain box.
  return Boolean(top && (top.matches(window) || top.querySelector(window)) && !holdsPlaced(top, target) && !top.matches(SIDE_PEEK));
}

// An item's menu open now: one closed stays mounted, unseen and drawing no box
// (RowContextMenuSurface keeps it in a hidden Activity), and asked as mounted it
// kept every view's choice from any press (2026-11-24: two on a page holding an
// inline database, three on a row's page).
function itemMenuDrawn(document: Document) {
  for (const menu of document.querySelectorAll("[data-row-menu-variant]")) if (menu.getClientRects().length > 0) return true;
  return false;
}

/**
 * Whether a key pressed on `target` goes on to the chosen items of the view
 * `root` is in though the focus is not on them: the focus on one of that
 * view's own controls that keep its choice (VIEW_CHOICE_KEEPING_PRESS), where
 * Escape gives it back as it closes a filter's window, and the key not one
 * the control takes itself (Enter, Space, Escape). As Notion's (seen
 * 2026-11-17 at 1440 after the key only, in two tabs, a table's row ticked
 * and a filter's editor closed with Escape: with the ring on the toolbar's
 * "필터", ⌘D copied the row and chose its copy, ↓ chose the row under it and
 * Backspace moved that to the trash, the ring staying; with it on the
 * filter's pill, ⌘D the same).
 */
export function choiceKeyFromControl(target: EventTarget | null, key: string, root: Element | null) {
  if (!(target instanceof Element) || !root || key === "Enter" || key === " " || key === "Escape") return false;
  const control = target.closest(VIEW_CHOICE_KEEPING_PRESS);
  return Boolean(control?.closest("[data-database-view]")?.contains(root));
}

/**
 * The control Enter was pressed on, where Enter is that control's own and
 * also the chosen rows' of the view `root` is in: one of that view's controls
 * that keep its choice (VIEW_CHOICE_KEEPING_PRESS), no key held with it. As
 * Notion's table (seen 2026-11-17 at 1440, after the key only, in two tabs at
 * different times: a row ticked and a filter's pill ringed after Escape
 * closed its window, Enter opened the pill's window and chose the row's title
 * cell, "1개 선택됨" gone; Escape then closed the window and a typed letter
 * went into that title; the toolbar's "필터" ringed, Enter hid the filter bar
 * and chose the title cell the same, the letter going into it).
 */
export function controlTakingRowsEnter(event: Pick<KeyboardEvent, "key" | "target" | "shiftKey" | "altKey" | "metaKey" | "ctrlKey">, root: Element | null) {
  if (!fieldEnter(event) || event.shiftKey || event.altKey || event.metaKey || event.ctrlKey || !(event.target instanceof Element) || !root) return null;
  const control = event.target.closest<HTMLElement>(VIEW_CHOICE_KEEPING_PRESS);
  return control?.closest("[data-database-view]")?.contains(root) ? control : null;
}

/** Whether a key is ⌘A (Ctrl+A off a Mac) alone. */
export function chooseAllKey(event: Pick<KeyboardEvent, "key" | "code" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey">) {
  return primaryModifier(event) && !event.shiftKey && !event.altKey && chordLetter(event) === "a";
}

/**
 * ⌘A on a view's chosen items, or on a part of one of its items: a database
 * that is its page's chooses every item it shows (`chooseAll`), each line of
 * a grouped view's row, as Notion's do (seen 2026-11-24 at 1440, dpr 2, in
 * two tabs at different times, after the key only: a table grouped by tags
 * "7개 선택됨" from a ticked row, 4 with a group folded, a plain one 5, a
 * list, a board's every card in every column and a gallery, a second ⌘A the
 * same and Escape letting all go; with none chosen, from the page itself or
 * a row's box Escape left the ring on, the same). An inline one hands ⌘A to
 * the page around it, which chooses all its blocks as its own ⌘A does, and
 * lets its own go (`letGo`): Notion's inline table and list, a row ticked or
 * an item chosen from its handle's menu, chose the page's blocks, the
 * database's among them, and no row (seen the same day in two tabs); a row's
 * box keeping the ring with none chosen, the same. Where no page takes it (a
 * view drawn apart from a page's blocks), the view chooses its items.
 */
export function chooseAllInView(root: HTMLElement | null, ownPage: boolean, chooseAll: () => void, letGo: () => void) {
  if (!ownPage && root && !root.dispatchEvent(new CustomEvent(VIEW_CHOICE_PAGE_ALL, { bubbles: true, cancelable: true }))) letGo();
  else chooseAll();
}

/** An item's own parts ⌘A with none chosen goes from as from a chosen one: a row's box or handle, a card or list row, a list row's handle. */
const VIEW_ITEM_PART = "[data-table-row-select], [data-row-menu-handle], [data-card-choice], [data-card-handle-for]";

/** Tells the page around the view that ↑ or ↓, Shift with them or not, went past its items; whether the page took it. */
export function choiceOut(root: HTMLElement | null, down: boolean, shift = false) {
  return Boolean(root) && !root!.dispatchEvent(new CustomEvent<ViewChoiceOut>(VIEW_CHOICE_OUT, { bubbles: true, cancelable: true, detail: { down, shift } }));
}

/** Asks the view inside an element to choose its first or last item; whether it did. */
export function chooseInView(holder: Element | null, edge: ViewChoiceIn["edge"]) {
  const view = holder?.querySelector(`[${VIEW_CHOICE_ROOT}]`);
  return Boolean(view) && !view!.dispatchEvent(new CustomEvent<ViewChoiceIn>(VIEW_CHOICE_IN, { cancelable: true, detail: { edge } }));
}

/**
 * How ↑ and ↓ start a view's choice with nothing chosen, as Notion's views
 * do (seen 2026-11-12): from the page itself, ↓ chooses the first item and ↑
 * the last on a database that is its page's, a press on its empty room
 * included; after Escape let a choice go,
 * either chooses again the item it ended on (Notion's table: 행 3 let go,
 * ↓ chooses 행 3); and, where `afterBox` says so, ↓ chooses the first item
 * after a box drawn from the view's empty room chose nothing, inline too
 * (Notion's board). An inline view leaves the rest to its page, whose
 * blocks ↑ ↓ walk into and out of it (VIEW_CHOICE_IN, VIEW_CHOICE_OUT): an
 * inline table's empty box hands ↓ to the page, which chooses its first
 * block (Notion's, seen 2026-11-12). `choose` chooses one item and shows it.
 * ⌘A with none chosen goes as chooseAllInView says (`chooseAll` choosing
 * every item): from the page itself on a database that is its page's, or
 * from an item's own part, as a row's box Escape left the focus on.
 */
export function useViewChoiceStart({ root, order, empty, choose, chooseAll, head, ownPage, afterBox = false, enabled = true }: {
  root: RefObject<HTMLElement | null>;
  order: () => string[];
  empty: boolean;
  /** Chooses one item and shows it; from a view tab's key the focus stays on the tab (viewTabChoice.ts). */
  choose: (id: string, fromTab?: boolean) => void;
  chooseAll: () => void;
  /** The item the choice ended on, none chosen none. */
  head: () => string | undefined;
  ownPage: boolean;
  afterBox?: boolean;
  enabled?: boolean;
}) {
  const latest = useRef({ order, choose, chooseAll, head });
  latest.current = { order, choose, chooseAll, head };
  // A box drawn from the view's empty room, or the item Escape let go, since the last press.
  const boxed = useRef(false);
  const escaped = useRef<string | null>(null);
  useEffect(() => {
    const press = () => { boxed.current = false; escaped.current = null; };
    document.addEventListener("pointerdown", press, true);
    return () => document.removeEventListener("pointerdown", press, true);
  }, []);
  useEffect(() => {
    if (!empty || !enabled) return;
    const start = (event: KeyboardEvent) => {
      const down = event.key === "ArrowDown", target = event.target as HTMLElement | null;
      // Only from the page itself, a field's, a menu's or a peek's focus keeping its keys; a
      // database that is its page holds its page's focus from a press on its empty room.
      const onPage = !target || target === document.body || target === document.documentElement
        || (ownPage && Boolean(root.current && target.contains(root.current)) && !target.isContentEditable);
      if (chooseAllKey(event) && !event.defaultPrevented && !imeComposing(event)) {
        // Not text being written: a database drawn in a page's text stands in its editable root.
        const onItem = target instanceof HTMLElement && root.current?.contains(target) && target.closest(VIEW_ITEM_PART)
          && !target.isContentEditable && !target.closest("input:not([type=checkbox]), textarea, select");
        if (!(ownPage && onPage) && !onItem) return;
        event.preventDefault();
        event.stopPropagation();
        chooseAllInView(root.current, ownPage, () => latest.current.chooseAll(), () => {});
        return;
      }
      if ((!down && event.key !== "ArrowUp") || event.shiftKey || event.altKey || event.metaKey || event.ctrlKey
        || event.defaultPrevented || imeComposing(event)) return;
      if (!onPage) return;
      const all = latest.current.order(), again = escaped.current;
      const next = again && all.includes(again) ? again
        : ownPage || (afterBox && boxed.current && down) ? (down ? all[0] : all[all.length - 1]) : undefined;
      if (!next) return;
      event.preventDefault();
      event.stopPropagation();
      escaped.current = null;
      latest.current.choose(next);
    };
    document.addEventListener("keydown", start, true);
    return () => document.removeEventListener("keydown", start, true);
  }, [afterBox, empty, enabled, ownPage, root]);
  // The page around the view hands it ↑ or ↓, and a view tab what its press let go.
  useEffect(() => {
    const view = root.current;
    if (!view || !enabled) return;
    const into = (event: Event) => {
      const all = latest.current.order(), edge = (event as CustomEvent<ViewChoiceIn>).detail?.edge;
      const next = edge === "last" ? all[all.length - 1] : all[0];
      if (!next) return;
      event.preventDefault();
      latest.current.choose(next);
    };
    // A press on its database's view tab lets the choice go, saying the row it
    // ended on; ↑ or ↓ on a tab chooses that row again in whichever view shows it.
    const pressed = (event: Event) => {
      const at = latest.current.head();
      if (at) (event as CustomEvent<ViewTabPressed>).detail.at = lineRowId(at);
    };
    const again = (event: Event) => {
      const at = (event as CustomEvent<ViewTabChoose>).detail?.at;
      const line = at ? latest.current.order().find((one) => lineRowId(one) === at) : undefined;
      if (!line) return;
      event.preventDefault();
      latest.current.choose(line, true);
    };
    view.addEventListener(VIEW_CHOICE_IN, into);
    view.addEventListener(VIEW_TAB_PRESSED, pressed);
    view.addEventListener(VIEW_TAB_CHOOSE, again);
    return () => {
      view.removeEventListener(VIEW_CHOICE_IN, into);
      view.removeEventListener(VIEW_TAB_PRESSED, pressed);
      view.removeEventListener(VIEW_TAB_CHOOSE, again);
    };
  });
  return {
    /** A box began in the view's empty room. */
    boxed: () => { boxed.current = true; },
    /** Escape let the choice go, ending on this item. */
    escaped: (head: string | undefined) => { escaped.current = head ?? null; },
  };
}
