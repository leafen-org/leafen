import { SwitchMark } from "@/components/Switch";
import { Fragment, isValidElement, startTransition, useCallback, useEffect, useRef, useState, type PointerEvent, type ReactNode } from "react";
import { CheckIcon } from "@/icons/leafen";
import { afterNextPaint } from "@/lib/afterPress";
import { NativeActionMenu, NativeActionMenuItem, NativeMenuHeader, NativeMenuSection, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { menuRowLine, menuRowSearchWords } from "@/lib/menuEnglishNames";
import { actionMenuScore } from "@/lib/menuSearch";
import { pointerMoved, pointerStill } from "@/lib/pointerHover";
import { keyCameLast } from "@/lib/lastPress";
import { lastPointerMoveAt } from "@/lib/menuInputLead";
import { keepLayerKeys } from "@/lib/overlayLayer";
import { arrivalHoldsLight, besideListOf, pointerHeadsFor, SUBMENU_SETTLE_MS } from "@/lib/submenuHoverHold";
import { RowMenuBesideSubmenu, RowMenuSubmenuRow } from "./RowMenuSubmenus";
import styles from "./RowMenu.module.css";
import valueStyles from "./RowMenuValue.module.css";

// A page's "…" menu as Notion lays it out (seen 2026-10-17): a short list
// whose rows with › open more rows beside it — "테마와 레이아웃 ›", "더 보기 ›",
// "알림 받기 ›" — under the pointer or when chosen, and a search that lists
// every list's rows in the menu itself, under the lists' names.

/** Which rows a list makes, by their names: all of them, as the menu's search ranks every row itself (usePageMenu). */
export type PageMenuShow = (label: string) => boolean;
export const everyRow: PageMenuShow = () => true;

/** Rows under one name, beside the menu and in a search's list. */
export type PageMenuSection = {
  header?: string;
  /** Its one row draws its own section and name (the font tiles). */
  own?: boolean;
  /** The words a search finds its name by, where they aren't its name's (menuSearchName). */
  searchHeader?: string;
  rows: PageMenuEntry[];
};

/** A page menu row whose rows open beside it. */
export type PageMenuSubmenu = {
  submenu: string;
  label: string;
  /** The words a search finds it by, where they aren't its name's (menuSearchName). */
  search?: string;
  icon: ReactNode;
  /** The value now, in gray before the arrow. */
  value?: string;
  disabled?: boolean;
  data?: Record<string, string>;
  width: number;
  /**
   * Its rows; `searching` asks for those a search lists in its place, under
   * the sections' names (or its own), and `show` says which a search leaves.
   * Without them a search shows the row itself when its name matches.
   */
  sections?: (show: PageMenuShow, searching: boolean) => PageMenuSection[];
  /** Choices of its own beside the row, in place of a list of the menu's rows; onCloseAll closes the lists it stands in too. */
  panel?: (row: HTMLElement, onClose: () => void, onCloseAll: () => void) => ReactNode;
  /** Chosen in place of opening (a list that couldn't be read reads again); the pointer doesn't open it. */
  onChoose?: () => void;
};
export type PageMenuEntry = ReactNode | PageMenuSubmenu;

function isSubmenu(entry: PageMenuEntry): entry is PageMenuSubmenu {
  return typeof entry === "object" && entry !== null && !isValidElement(entry) && "submenu" in entry;
}

export type PageMenuItemOptions = {
  hint?: string; disabled?: boolean; data?: Record<string, string>; role?: "menuitem" | "menuitemcheckbox" | "menuitemradio";
  checked?: boolean; trailing?: ReactNode; className?: string; badge?: string;
  /** The words a search finds the row by, where they aren't its name's (menuSearchName). */
  search?: string;
};

/** The page menu's rows: one shows where it has an action and `show` leaves its name. */
export function pageMenuItem(busy: boolean) {
  return (show: PageMenuShow, key: string, label: string, icon: ReactNode, onClick: (() => void) | undefined, options: PageMenuItemOptions = {}) => {
    if (!onClick || !show(label)) return null;
    const content = (on?: boolean) => <>
      {icon}
      <span>{label}{options.badge ? <span className={styles.itemBadge}>{options.badge}</span> : null}</span>
      {options.hint ? <span className={styles.itemHint}>{options.hint}</span> : null}
      {on === undefined ? options.trailing : menuSwitch(on)}
    </>;
    const props = { className: `${styles.item} ${options.className ?? ""}`, "data-menu-item": true, "data-menu-label": label, "data-menu-search": options.search,
      role: options.role ?? "menuitem", disabled: busy || options.disabled, ...options.data } as const;
    // A row with a switch turns on the press's own frame (PageMenuSwitchItem).
    if (options.role === "menuitemcheckbox" && typeof options.checked === "boolean" && isSwitch(options.trailing)) {
      return <PageMenuSwitchItem key={key} {...props} checked={options.checked} content={content} onPress={onClick} />;
    }
    return (
      <NativeActionMenuItem key={key} type="button" {...props} aria-checked={options.checked} onClick={onClick}>
        {content()}
      </NativeActionMenuItem>
    );
  };
}

/** A row's switch, on or off. */
export function menuSwitch(on: boolean) {
  return <SwitchMark on={on} className={styles.menuSwitch} />;
}

/**
 * What "편집 제안" says at its end while the page is being suggested in: no
 * switch but "중지하기" in red, the row's press turning suggesting off, as
 * Notion's page menu (its code and Chrome at 1440, 2026-11-18: the row reads
 * "편집 제안" alone before, and closes the menu as it turns either way).
 */
export function suggestingStop(label: string) {
  return <span className={`${styles.itemValue} ${valueStyles.stop}`}>{label}</span>;
}

function isSwitch(node: ReactNode) {
  return isValidElement(node) && node.type === SwitchMark;
}

/**
 * A switch row of a page menu: it turns as it is pressed, and what it changes
 * — a page locked, its text made small, its width full, each drawing the
 * whole page again (2026-11-13: a lock pressed on a page with a table of 40
 * rows took 650–900 ms to show, on a NAS's 100 ms round trip) — follows once
 * that frame is painted, as a transition a newer press can pass (user
 * decision 2026-11-12: a press answers on the next frame). It then shows
 * what the page holds: turned, or back where it was.
 */
function PageMenuSwitchItem({ checked, content, onPress, ...props }: {
  checked: boolean; content(on: boolean): ReactNode; onPress(): void; className: string; role: "menuitem" | "menuitemcheckbox" | "menuitemradio";
  disabled?: boolean; "data-menu-label": string; "data-menu-search"?: string;
}) {
  const [pressed, setPressed] = useState<boolean | null>(null);
  const on = pressed ?? checked;
  return (
    <NativeActionMenuItem type="button" {...props} aria-checked={on} onClick={() => {
      setPressed(!on);
      afterNextPaint(() => startTransition(() => {
        onPress();
        setPressed(null);
      }));
    }}>
      {content(on)}
    </NativeActionMenuItem>
  );
}

/** The mark of the chosen choice, where a search lists a list's choices. */
export function menuCheck(checked: boolean) {
  return checked ? <CheckIcon size="small" aria-hidden="true" /> : null;
}

/**
 * The pointer moving onto a row with › opens its rows, and onto another row
 * of the same list closes them, or opens that row's — once it has rested
 * there a moment (SUBMENU_SETTLE_MS), unless the pointer came onto the row on
 * its way to the rows open beside the list (pointerHeadsFor, the one rule of
 * every list beside a row): then they stay, however long the pointer rests
 * there or moves along that row, as Notion's page "⋯" (seen 2026-11-18 at
 * 1440 in Chrome, the pointer moved in 12px steps: from "테마와 레이아웃 ›"
 * across "AI로 사용 ›" onto "편집 제안" toward its list, it stayed 5 s, both
 * rows grey, 28px back along the row too; away from it, or in one jump, it
 * closed; "더 보기 ›" and its "연결 ›" the same). Passed over quickly, a row
 * opens nothing and the open rows stay. `hold` says the pointer is in a list
 * beside the menu; `submenuRowMarks` marks the rows it reads.
 *
 * In a list beside a row that came up with a row of its own lit, the row
 * the pointer first comes onto opens nothing while it stays there; the next
 * row it comes onto does (arrivalHoldsLight).
 *
 * A menu a press opened that comes up with a › row under the still pointer
 * opens that row's list once the pointer has rested there (`onPointerOver`,
 * the browser telling the row the pointer came as the menu is drawn): Notion's
 * column menu, drawn up over its header where the window ends, opened
 * "속성 편집 ›" 135 ms after the row heard the pointer (measured 2026-11-27 in
 * Chrome at 1440 in three tabs, a select and a status column). One the keys
 * opened, or any move or key since, opens nothing there (⌘/ under a still
 * pointer, seen 2026-11-07; useNativeSubmenu).
 */
export function useSubmenuHover(open: string | null, onOpen: (kind: string, row: HTMLElement) => void, onClose: () => void) {
  // The row the pointer is on, decided as it came onto it, and what it does once the pointer has rested there.
  const came = useRef<Element | null>(null);
  const settling = useRef<number | undefined>(undefined);
  const [openedAt] = useState(() => performance.now());
  const settle = (then?: () => void) => {
    window.clearTimeout(settling.current);
    settling.current = then ? window.setTimeout(then, SUBMENU_SETTLE_MS) : undefined;
  };
  useEffect(() => () => window.clearTimeout(settling.current), []);
  const hold = useCallback(() => { came.current = null; window.clearTimeout(settling.current); }, []);
  const rowOf = (target: EventTarget | null) => target instanceof Element ? target.closest<HTMLElement>("[data-native-menu-item]") : null;
  const kindOf = (row: HTMLElement) => row.getAttribute("aria-disabled") === "true" ? undefined : row.dataset.pageSubmenuRow;
  const onPointerMove = (event: PointerEvent<HTMLElement>) => {
    if (!pointerMoved(event)) return;
    const row = rowOf(event.target);
    if (!row || row === came.current) return;
    came.current = row;
    if (arrivalHoldsLight(row)) return settle();
    const kind = kindOf(row);
    if ((kind ?? null) === open) return settle();
    if (!open) return settle(kind ? () => onOpen(kind, row) : undefined);
    // The row whose rows are open, in the list the pointer came into (drawn apart from what hears it, a list beside a list).
    const opened = row.closest("[data-native-menu]")?.querySelector('[data-page-submenu-row][aria-expanded="true"]');
    const list = besideListOf(opened);
    if (list && pointerHeadsFor(list, event.clientX, event.clientY)) return settle();
    settle(kind ? () => onOpen(kind, row) : onClose);
  };
  const onPointerOver = (event: PointerEvent<HTMLElement>) => {
    if (event.pointerType === "touch" || !pointerStill(event) || keyCameLast() || lastPointerMoveAt() > openedAt) return;
    const row = rowOf(event.target), kind = row ? kindOf(row) : undefined;
    if (!row || !kind || row === came.current || kind === open) return;
    came.current = row;
    settle(() => onOpen(kind, row));
  };
  return { onPointerMove, onPointerOver, hold };
}

/** Marks a menu row for useSubmenuHover: a row, and the list it opens beside it (none for a row that only acts). */
export function submenuRowMarks(opens: string | undefined) {
  return { "data-native-menu-item": "", "data-page-submenu-row": opens };
}

type Opened = { kind: string; row: HTMLElement } | null;
/** A row a search found: the line of words it is found by, and the list name it stands under. */
type Found = { node: ReactNode; line: string; header?: string };

/** A menu row's name and the words a search finds it by, as `pageMenuItem` marks them. */
function namesOf(node: ReactNode): { label: string; search?: string } | null {
  if (!isValidElement(node)) return null;
  const props = node.props as Record<string, unknown>;
  const label = props["data-menu-label"], search = props["data-menu-search"];
  return typeof label === "string" ? { label, search: typeof search === "string" ? search : undefined } : null;
}

function sectionOf({ header, own, rows }: { header?: string; own?: boolean; rows: ReactNode[] }) {
  return header && !own ? (
    <NativeMenuSection aria-label={header}>
      <NativeMenuHeader className={styles.sectionLabel}>{header}</NativeMenuHeader>
      {rows}
    </NativeMenuSection>
  ) : rows;
}

function find(entries: PageMenuEntry[], kind: string): PageMenuSubmenu | null {
  for (const entry of entries) if (isSubmenu(entry) && entry.submenu === kind) return entry;
  return null;
}

function nestedEntries(entry: PageMenuSubmenu): PageMenuEntry[] {
  return entry.sections?.(everyRow, false).flatMap((section) => section.rows) ?? [];
}

/**
 * A page menu's rows and the rows beside them: `list` draws the menu's own
 * (its groups between dividers, or a search's matches in sections), `overlay`
 * the open lists beside it, and the menu passes its pointer moves on.
 */
export function usePageMenu(query: string) {
  // As Notion's action menus read a search: trimmed, in lower case, without a "/" before it.
  const needle = query.trim().toLowerCase().replace(/^\//u, "");
  const [open, setOpen] = useState<Opened>(null);
  const [inner, setInner] = useState<Opened>(null);
  const openOuter = useCallback((kind: string, row: HTMLElement) => {
    setInner(null);
    setOpen((current) => (current?.kind === kind && current.row === row ? current : { kind, row }));
  }, []);
  const closeOuter = useCallback(() => { setInner(null); setOpen(null); }, []);
  const openInner = useCallback((kind: string, row: HTMLElement) => {
    setInner((current) => (current?.kind === kind && current.row === row ? current : { kind, row }));
  }, []);
  const closeInner = useCallback(() => setInner(null), []);
  const outerHover = useSubmenuHover(open?.kind ?? null, openOuter, closeOuter);
  const innerHover = useSubmenuHover(inner?.kind ?? null, openInner, closeInner);
  // A search lists the rows beside the menu in the menu itself.
  useEffect(() => { closeOuter(); }, [needle, closeOuter]);

  const rowOf = (entry: PageMenuSubmenu, opened: Opened, onOpen: (kind: string, row: HTMLElement) => void) => (
    <RowMenuSubmenuRow key={entry.submenu} kind={entry.onChoose ? undefined : entry.submenu} icon={entry.icon} label={entry.label}
      value={entry.value ?? ""} open={opened?.kind === entry.submenu} disabled={Boolean(entry.disabled)}
      onOpen={(element) => (entry.onChoose ? entry.onChoose() : onOpen(entry.submenu, element))} data={entry.data ?? {}} />
  );

  const list = (groups: PageMenuEntry[][]): ReactNode => {
    if (!needle) {
      const shown = groups.map((group) => group.filter(Boolean)).filter((group) => group.length > 0);
      return shown.map((group, index) => (
        <Fragment key={index}>
          {index > 0 ? <NativeMenuSeparator className={styles.divider} /> : null}
          {group.map((entry) => (isSubmenu(entry) ? rowOf(entry, open, openOuter) : entry))}
        </Fragment>
      ));
    }
    // A search's matches as Notion's action menus rank them (read 2026-11-16
    // from Notion's web app, and checked against its page "⋯" menu in Chrome
    // on nine searches, menuSearch.ts actionMenuScore): each row is found by
    // one line — its words, the words of the lists it stands in, its words
    // again, its shown name parted at the end (menuRowLine) — and a list's
    // rows stand before the rows of the group holding the list (알림 받기's
    // choices before 편집 제안, 인라인 댓글's before 작은 텍스트), lists two
    // deep at most. The rows go from the best score down, in that order where
    // they tie, gathered under their lists' names in the order each name
    // first comes; the menu's own rows, from every group, under none ("m":
    // 더 보기 first, by "Mac 앱에서 열기").
    const rowsOf = (entries: PageMenuEntry[], header: string | undefined, words: string, above: string, depth: number): Found[] => {
      const lists: Found[] = [], own: Found[] = [];
      for (const entry of entries) {
        if (!entry) continue;
        if (isSubmenu(entry) && entry.sections && depth < 2) {
          const name = menuRowSearchWords(entry.label, entry.search);
          for (const section of entry.sections(everyRow, true)) {
            const named = section.header ? menuRowSearchWords(section.header, section.searchHeader) : "";
            lists.push(...rowsOf(section.rows, section.header ?? entry.label, [above, name, named].filter(Boolean).join(" "),
              depth === 0 ? [above, name].filter(Boolean).join(" ") : "", depth === 0 ? 1 : 2));
          }
          continue;
        }
        const names = isSubmenu(entry) ? { label: entry.label, search: entry.search } : namesOf(entry);
        // A row that names nothing a search could find stays out of it.
        if (!names) continue;
        own.push({ node: isSubmenu(entry) ? rowOf(entry, open, openOuter) : entry, line: menuRowLine(names.label, names.search, words), header });
      }
      return [...lists, ...own];
    };
    const found = groups.flatMap((group) => rowsOf(group, undefined, "", "", 0))
      .map((row, order) => ({ ...row, order, score: actionMenuScore(needle, row.line) }))
      .filter((row) => row.score > 0).sort((left, right) => right.score - left.score || left.order - right.order);
    const named = new Map<string | undefined, ReactNode[]>();
    for (const row of found) named.set(row.header, [...(named.get(row.header) ?? []), row.node]);
    return [...named].map(([header, rows], index) => (
      <Fragment key={header ?? ""}>
        {index > 0 ? <NativeMenuSeparator className={styles.divider} /> : null}
        {sectionOf({ header, rows })}
      </Fragment>
    ));
  };

  const panelRows = (entry: PageMenuSubmenu, opened: Opened, onOpen: (kind: string, row: HTMLElement) => void) => {
    const sections = (entry.sections?.(everyRow, false) ?? [])
      .map((section) => ({ ...section, rows: section.rows.filter(Boolean) })).filter((section) => section.rows.length > 0);
    // A list's groups part with a divider; a named one with its name.
    return sections.map((section, index) => (
      <Fragment key={index}>
        {index > 0 && !section.header ? <NativeMenuSeparator className={styles.divider} /> : null}
        {sectionOf({ ...section, rows: section.rows.map((part) => (isSubmenu(part) ? rowOf(part, opened, onOpen) : part)) })}
      </Fragment>
    ));
  };
  const panel = (entry: PageMenuSubmenu, row: HTMLElement, onClose: () => void, opened: Opened, onOpen: (kind: string, row: HTMLElement) => void) =>
    entry.panel ? entry.panel(row, onClose, closeOuter) : (
      <RowMenuBesideSubmenu row={row} width={entry.width} label={entry.label} onClose={onClose} ownMenu>
        <NativeActionMenu aria-label={entry.label}>{panelRows(entry, opened, onOpen)}</NativeActionMenu>
      </RowMenuBesideSubmenu>
    );

  const overlay = (groups: PageMenuEntry[][]): ReactNode => {
    if (!open) return null;
    const top = groups.flat();
    const outer = find(top, open.kind) ?? find(top.filter(isSubmenu).flatMap(nestedEntries), open.kind);
    if (!outer) return null;
    const nested = inner ? find(nestedEntries(outer), inner.kind) : null;
    // The lists keep their keys from the page under the menu, all but Escape,
    // which closes the list on top (keepLayerKeys): a list of buttons
    // (Notion's "인라인 댓글 ›", "알림 받기 ›") has no Escape of its own.
    return (
      <>
        <span className={styles.submenuHost} onKeyDown={keepLayerKeys}
          onPointerMove={(event) => { outerHover.hold(); innerHover.onPointerMove(event); }}>
          {panel(outer, open.row, closeOuter, inner, openInner)}
        </span>
        {nested && inner ? (
          <span className={styles.submenuHost} onKeyDown={keepLayerKeys}
            onPointerMove={() => { outerHover.hold(); innerHover.hold(); }}>
            {panel(nested, inner.row, closeInner, null, () => undefined)}
          </span>
        ) : null}
      </>
    );
  };

  return { list, overlay, onPointerMove: outerHover.onPointerMove, matches: everyRow };
}
