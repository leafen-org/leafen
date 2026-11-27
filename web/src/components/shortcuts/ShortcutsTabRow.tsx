import { useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { MoreChevronDown } from "@/icons/leafen/shortcutsIcons";
import type { ShortcutsHelpSectionId } from "@/lib/keyboardShortcutsHelp";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { SHORTCUTS_SECTIONS as ALL, ShortcutsTabMark } from "./ShortcutsTabMark";
import styles from "./KeyboardShortcutsDialog.module.css";

type Widths = Record<ShortcutsHelpSectionId, number>;

// Notion's count of shown tabs, read from its shortcuts window and measured
// 2026-11-21 (Chrome, ko, 800 to 1440 wide):
// the row has 0.325 of the window's width, and each tab is
// reckoned by its English name in 14px medium sans-serif plus 16, at most
// 100 — whatever the screen's language, so ko, en and ar show as many. Tabs
// go in while they fit, each needing 100 more while the search is open; when
// all would fit the last still goes into "더 보기…". A window resized while
// open reckons with 0.325 of the dialog's width instead (Notion's own
// resize step: 1440 shows three until it is opened again).
const ROW_SHARE = 0.325, OPEN_SEARCH = 100, MOST = 100;

function reckon(): Widths {
  const context = document.createElement("canvas").getContext("2d");
  if (context) context.font = "500 14px sans-serif";
  return Object.fromEntries(ALL.map((id) => {
    const key = `keyboardShortcuts:tabs.${id}`, english = i18next.t(key, { lng: "en", defaultValue: i18next.t(key) });
    return [id, Math.min((context?.measureText(english).width ?? english.length * 7.5) + 16, MOST)];
  })) as Widths;
}

function shownOf(order: readonly ShortcutsHelpSectionId[], room: number, searching: boolean, widths: Widths) {
  const shown: ShortcutsHelpSectionId[] = [];
  let used = 0;
  for (const id of order) {
    if (used + widths[id] + (searching ? OPEN_SEARCH : 0) > room) return shown;
    shown.push(id);
    used += widths[id];
  }
  shown.pop();
  return shown;
}

// The lit tab moves into the last shown place, the tabs after it one along —
// with no place shown it stays in the menu, whose button then carries its
// name; the row goes back to its list's order once the lit tab shows there.
// As Notion's, a moved order outlives a new search until then (seen
// 2026-11-21 at 1440 and 800: "@" then its ✕ kept the row at "명령어 ⌄" over
// the whole list until the list was scrolled).
function settle(order: ShortcutsHelpSectionId[] | null, active: ShortcutsHelpSectionId, room: number, searching: boolean, widths: Widths,
  list: readonly ShortcutsHelpSectionId[]) {
  for (let step = 0; step < ALL.length; step++) {
    const now = order ?? list, shown = shownOf(now, room, searching, widths).length, at = now.indexOf(active);
    if (at >= shown) {
      const next = now.filter((id) => id !== active);
      next.splice(shown - 1, 0, active);
      if (order && next.join() === order.join()) return order;
      order = next;
    } else return order && shownOf(list, room, searching, widths).includes(active) ? null : order;
  }
  return order;
}

/**
 * The window's tabs, Notion's: as many as its count gives before "더 보기…",
 * whose menu holds the rest; the section in view is the lit one. Notion's
 * list glides to a pressed tab's section and its row takes each section the
 * list passes in turn, so a tab chosen from the menu stands in the last
 * place and the tab there goes into the menu (seen 2026-11-21: at 1440
 * "슬래시 명령어" stood where "명령어" was; at 800 it stood where "편집 및
 * 이동" was, the menu then "명령어", "Markdown", "편집 및 이동"). This list
 * jumps, so the row walks the sections between. While the search has words
 * the row holds only the sections with found rows, in the list's order, and
 * counts them as always (seen 2026-11-21 at 1440 and 800: "bold" leaves
 * "생성 및 스타일 지정" with "Markdown" in the menu, "dark" neither tabs nor
 * "더 보기…"); a lit section left in the menu names the menu's button, with
 * a chevron ("@": "명령어 ⌄" alone).
 */
export function ShortcutsTabRow({ active, list = ALL, searching, firstTabRef, keyOpened, onFirstLeft, onPick }: {
  active: ShortcutsHelpSectionId;
  /** The sections the row takes its tabs from, in the list's order: those with found rows while the search has words. */
  list?: readonly ShortcutsHelpSectionId[];
  searching: boolean;
  firstTabRef: RefObject<HTMLButtonElement | null>;
  /** The window opened by keys: its first tab, focused, keeps the ring until the focus leaves it. */
  keyOpened: boolean;
  onFirstLeft(): void;
  onPick(section: ShortcutsHelpSectionId): void;
}) {
  const { t } = useTranslation("keyboardShortcuts");
  const rowRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLDivElement>(null), moreMenuRef = useRef<HTMLDivElement>(null);
  const widths = useMemo(reckon, []);
  const [room, setRoom] = useState(() => ROW_SHARE * window.innerWidth);
  const [order, setOrder] = useState<ShortcutsHelpSectionId[] | null>(null);
  const [menu, setMenu] = useState(false);
  const passed = useRef(active);
  const listKey = list.join();
  const base = useMemo(() => (listKey ? listKey.split(",") : []) as ShortcutsHelpSectionId[], [listKey]);
  const now = order ?? base;
  const shown = shownOf(now, room, searching, widths).length;
  const held = now.slice(shown);
  const named = held.includes(active) ? active : null;
  const menuOpen = menu && held.length > 0;

  useEffect(() => {
    const resized = () => {
      const dialog = rowRef.current?.closest("[role=dialog]");
      if (dialog) setRoom(ROW_SHARE * dialog.getBoundingClientRect().width);
    };
    window.addEventListener("resize", resized);
    return () => window.removeEventListener("resize", resized);
  }, []);

  // Before the paint, so a chosen tab shows in its place on the press's frame.
  // A lit section the list no longer holds (new words that left the list at
  // its top) walks from the list's first section, as Notion's list glides
  // from there (seen 2026-11-21 at 800: "page", then "슬래시 명령어" from the
  // menu leaves "명령어", "생성 및 스타일 지정" in it).
  useLayoutEffect(() => {
    const from = base.indexOf(passed.current), to = base.indexOf(active);
    passed.current = active;
    const way = to < 0 ? [] : from < 0 ? base.slice(0, to + 1) : from < to ? base.slice(from + 1, to + 1) : base.slice(to, from).reverse();
    setOrder((now) => (way.length ? way : [active]).reduce((next, id) => settle(next, id, room, searching, widths, base), now));
  }, [active, base, room, searching, widths]);

  useOverlayLayer(menuOpen, { kind: "menu", ref: moreRef, restoreFocus: false, onEscape: () => setMenu(false), onOutsidePress: () => setMenu(false) });
  usePopoverAppear(menuOpen, moreMenuRef);

  const label = (id: ShortcutsHelpSectionId) => t(`tabs.${id}`);
  const tab = (id: ShortcutsHelpSectionId, index: number) => {
    return <button key={id} ref={index === 0 ? firstTabRef : undefined} type="button" className={styles.tab}
      data-shortcuts-tab={id} data-active={id === active ? "true" : undefined} aria-current={id === active ? "true" : undefined}
      data-key-opened={index === 0 && keyOpened ? "true" : undefined} onBlur={index === 0 && keyOpened ? onFirstLeft : undefined}
      onClick={() => onPick(id)}>
      <ShortcutsTabMark section={id} fitted />{label(id)}
    </button>;
  };
  return <div ref={rowRef} className={styles.tabs} role="group" aria-label={t("categoriesLabel")}>
    {now.slice(0, shown).map(tab)}
    {held.length ? <div ref={moreRef} className={styles.moreHolder}>
      <button type="button" className={styles.tab} aria-haspopup="menu" aria-expanded={menuOpen} data-shortcuts-more
        data-active={named ? "true" : undefined} aria-current={named ? "true" : undefined} onClick={() => setMenu((open) => !open)}>
        {named ? <>{label(named)}<MoreChevronDown className={styles.moreChevron} aria-hidden="true" /></> : t("more")}
      </button>
      {menuOpen ? <div ref={moreMenuRef} className={styles.moreMenu} role="menu">
        {held.map((id) => <button key={id} type="button" role="menuitem" className={styles.moreItem} data-shortcuts-tab={id}
          onClick={() => { setMenu(false); onPick(id); }}>
          <ShortcutsTabMark section={id} />{label(id)}
        </button>)}
      </div> : null}
    </div> : null}
  </div>;
}
