"use client";

import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { detectKeyboardShortcutPlatform, type KeyboardShortcutPlatform } from "@/lib/keyboardShortcuts";
import { SHORTCUTS_HELP, shortcutsHelpCaps, type ShortcutsHelpSection, type ShortcutsHelpSectionId } from "@/lib/keyboardShortcutsHelp";
import { Search, XCircleIcon } from "@/icons/leafen";
import { keyCameLast } from "@/lib/lastPress";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { ShortcutsTabRow } from "./ShortcutsTabRow";
import styles from "./KeyboardShortcutsDialog.module.css";

/**
 * The search's words marked where they show in a row's name or in one of its
 * key caps, as Notion's (its shortcuts window marks every place the words
 * fall, in any case, on findHighlightMatch.selectedBackground in the main
 * text colour, 0 2px in, 2 round; seen 2026-11-21 at 1440: "**" in a name and
 * twice in its cap, "cmd" and "@" in caps). A row found by words that do not
 * show — its English name under another language, its keys' search terms,
 * words that run across two caps — has no mark, as Notion's rows
 * found by their English names under Korean have none.
 */
function marked(text: string, needle: string): ReactNode {
  if (!needle) return text;
  const parts = text.split(new RegExp(`(${needle.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "giu"));
  if (parts.length < 2) return text;
  return <span>{parts.map((part, at) => (at % 2 ? <mark key={at} className={styles.found}>{part}</mark> : part))}</span>;
}

/**
 * The keyboard shortcuts window (⌘⌥/), Notion's (measured 2026-11-19 in
 * Chrome at 1440 and 800, ko, en and ar, light and dark): a big title, a
 * row of tabs over one
 * list of sections that scrolls under them, the tab of the section in view
 * lit, and a search behind the row's end. Each row's keys are word caps in
 * the screen's direction ("F Cmd" from the left in Arabic). It opens with its
 * first tab focused; Escape closes it, from its search too.
 */
export function KeyboardShortcutsDialog({
  onClose,
  platform = detectKeyboardShortcutPlatform(),
  restoreFocusTo,
}: {
  onClose: () => void;
  platform?: KeyboardShortcutPlatform;
  restoreFocusTo?: HTMLElement | null;
}) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const firstTabRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const jumpedTo = useRef<number | null>(null);
  const titleId = useId();
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [active, setActive] = useState<ShortcutsHelpSectionId>("popular");
  const { t } = useTranslation("keyboardShortcuts");
  const close = useCallback(() => onClose(), [onClose]);

  // A modal layer: Escape closes it once nothing opened over it takes the key;
  // focus returns to its opener, or the place it was asked to return to.
  restoreFocusRef.current = restoreFocusTo ?? null;
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: close,
    initialFocus: dialogRef, returnFocusRef: restoreFocusTo ? restoreFocusRef : undefined });
  // Its first tab takes the focus, ringed when keys opened it — ⌘⌥/ too,
  // a chord that leaves other controls ringless (lib/lastPress.ts) — as
  // Notion's (seen 2026-11-19, ⌘⌥/ in four tabs: "인기" ringed, ko and ar).
  const [keyOpened, setKeyOpened] = useState(false);
  useEffect(() => {
    const keys = keyCameLast();
    setKeyOpened(keys);
    firstTabRef.current?.focus({ preventScroll: true, focusVisible: keys } as FocusOptions);
  }, []);

  useEffect(() => { if (searching) searchRef.current?.focus(); }, [searching]);

  // The field opens and closes as Notion's: the column takes its fifth of the
  // head at once (its width goes between auto and 20%, which do not move),
  // and the field's 160 box grows from nothing and fades in over 200ms ease
  // (@starting-style), and back out the same way when its ✕ closes it — the
  // search already closed for the tab row, the box left drawn until it has
  // gone (seen 2026-11-21 at 1440 in two tabs, dark: Notion's box mounted at
  // width 0 and opacity 0, then 160 and 1 with "width, opacity 200ms ease",
  // and set back to 0 and 0 before it is removed).
  const [closing, setClosing] = useState(false);
  const closingTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(closingTimer.current), []);
  const openSearch = () => {
    window.clearTimeout(closingTimer.current);
    setClosing(false);
    setSearching(true);
  };
  const closeSearch = () => {
    setQuery("");
    setSearching(false);
    setClosing(true);
    window.clearTimeout(closingTimer.current);
    closingTimer.current = window.setTimeout(() => setClosing(false), 200);
  };

  // A row is found by its words, in the screen's language and in English, and
  // by its keys; while there are words "인기" is left out, as Notion's (its
  // rows stand in the other sections too).
  const needle = query.trim().toLowerCase();
  const sections = useMemo(() => SHORTCUTS_HELP.filter((section) => !needle || section.id !== "popular").map((section): ShortcutsHelpSection => ({
    ...section,
    groups: section.groups.map((group) => ({ ...group, rows: group.rows.filter((row) => {
      if (!needle) return true;
      const key = `rows.${section.id}.${row.label}`;
      return [t(key), i18next.t(`keyboardShortcuts:${key}`, { lng: "en" }), shortcutsHelpCaps(row, platform).join(" "),
        row.shortcut ? t(`searchTerms.${row.shortcut}`, { defaultValue: "" }) : ""].join(" ").toLowerCase().includes(needle);
    }) })).filter((group) => group.rows.length > 0),
  })).filter((section) => section.groups.length > 0), [needle, platform, t]);

  // Where the list stands with a section's head at its top, as far as it goes.
  const headAt = (body: HTMLElement, section: ShortcutsHelpSectionId) => {
    const head = body.querySelector(`[data-shortcuts-section="${section}"]`);
    return head ? Math.min(body.scrollHeight - body.clientHeight, body.scrollTop + head.getBoundingClientRect().top - body.getBoundingClientRect().top) : null;
  };
  // Where the list stood before its last change, for new words (below).
  const lastTop = useRef(0);
  const scrollTo = (body: HTMLElement, to: number) => {
    jumpedTo.current = to;
    lastTop.current = to;
    body.scrollTop = to;
  };

  // The lit tab is the last section whose head has reached the list's top.
  const follow = () => {
    const body = bodyRef.current;
    if (!body) return;
    lastTop.current = body.scrollTop;
    if (jumpedTo.current !== null && Math.abs(body.scrollTop - jumpedTo.current) < 2) { jumpedTo.current = null; return; }
    jumpedTo.current = null;
    const top = body.getBoundingClientRect().top + 1;
    let reached: ShortcutsHelpSectionId | null = null;
    for (const element of body.querySelectorAll<HTMLElement>("[data-shortcuts-section]")) {
      if (element.getBoundingClientRect().top <= top) reached = element.dataset.shortcutsSection as ShortcutsHelpSectionId;
    }
    setActive(reached ?? sections[0]?.id ?? "popular");
  };
  // A tab scrolls its section's head to the list's top and is lit, as far as
  // the list goes; one whose section the list does not hold does nothing.
  const jump = (section: ShortcutsHelpSectionId) => {
    const body = bodyRef.current, to = body ? headAt(body, section) : null;
    if (!body || to === null) return;
    setActive(section);
    scrollTo(body, to);
  };
  // The window opens with its first head at the list's top, the shade's 16
  // scrolled away above it, as Notion's (seen 2026-11-21 at 1440: scrollTop 16).
  useLayoutEffect(() => {
    const body = bodyRef.current, to = body ? headAt(body, SHORTCUTS_HELP[0].id) : null;
    if (body && to !== null) scrollTo(body, to);
  }, []);

  // New words take the list to the first section with found rows, as
  // Notion's, whose lit tab follows the list's scrolling: it moves there only
  // when the list moves — when it had been scrolled elsewhere, or when the
  // found rows fit and the list goes back to its top, their first head 16
  // under the shade; otherwise the tab lit before stays (seen 2026-11-21 at
  // 1440 and 800: "bold" and "@" light their first section, "page" none).
  const lastNeedle = useRef("");
  useEffect(() => {
    const was = lastNeedle.current, body = bodyRef.current, first = sections[0];
    lastNeedle.current = needle;
    if (!needle || needle === was || !first || !body) return;
    const to = headAt(body, first.id);
    if (to === null || Math.abs(lastTop.current - to) < 1) return;
    setActive(first.id);
    scrollTo(body, to);
  });
  const found = useMemo(() => (needle ? sections.map((section) => section.id) : undefined), [needle, sections]);

  // The list keeps 32 at its end, and 16 only while it scrolls beside a bar
  // that takes room (the bar fills the rest), as Notion's (its list's end is
  // 16 when scrollHeight passes clientHeight and the system's bars take room,
  // else 32, watched on every resize — read in its window code 2026-11-21 and
  // seen at 1440 and 800: the full list and "page" 16 beside a 15 bar, "bold",
  // "@" and no rows 32 with no bar, so "검색 결과 없음" stands in the middle).
  const [scrollBar, setScrollBar] = useState(false);
  const measureBar = useCallback(() => {
    const body = bodyRef.current;
    if (body) setScrollBar(body.scrollHeight > body.clientHeight && body.offsetWidth > body.clientWidth);
  }, []);
  useLayoutEffect(() => measureBar(), [measureBar, sections]);
  useEffect(() => {
    const body = bodyRef.current;
    if (!body) return;
    const watch = new ResizeObserver(measureBar);
    watch.observe(body);
    return () => watch.disconnect();
  }, [measureBar]);

  return (
    <div className={styles.overlay} style={{ zIndex }} data-keyboard-shortcuts-overlay>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("closeBackdrop")} onClick={close} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} data-keyboard-shortcuts-dialog>
        <h1 id={titleId} className={styles.title}>{t("title")}</h1>
        <div className={styles.head}>
          <ShortcutsTabRow active={active} list={found} searching={searching} firstTabRef={firstTabRef} keyOpened={keyOpened}
            onFirstLeft={() => setKeyOpened(false)} onPick={jump} />
          <div className={styles.searchColumn} data-open={searching ? "true" : undefined}>
            {/* The round button stays at the open field's start, shrinking
                with it; pressed again it takes the focus and leaves the field
                open, a letter typed after it landing nowhere, as Notion's
                (seen 2026-11-21 in two tabs at different times, 800 and 1440). */}
            <button type="button" className={styles.searchButton} aria-label={t("search")} data-shortcuts-search
              onClick={openSearch}>
              <Search size="regular" aria-hidden="true" />
            </button>
            {/* Its ✕ clears the words and closes the field back to the round
                button, the row then counted with the search closed; the focus
                goes with the field (data-focus-leaves: the window does not hand
                it to the button), as Notion's (seen 2026-11-21 at 1440: a
                letter typed after it lands nowhere, no ring shows and Escape
                still closes the window). */}
            {searching || closing ? <label className={styles.searchField} data-focus-leaves="" data-closing={searching ? undefined : ""}>
              <input ref={searchRef} type="search" className={styles.searchInput} value={query} aria-label={t("searchLabel")}
                placeholder={t("search")} onChange={(event) => setQuery(event.target.value)} />
              <button type="button" className={styles.clearSearch} aria-label={t("clearSearch")} title={t("clearSearch")}
                onClick={closeSearch}>
                <XCircleIcon size="regular" aria-hidden="true" />
              </button>
            </label> : null}
          </div>
        </div>
        <div ref={bodyRef} className={styles.body} onScroll={follow} data-scroll-bar={scrollBar ? "" : undefined}>
          <div className={styles.shade} aria-hidden="true" />
          {sections.length ? sections.map((section) => (
            <section key={section.id} className={styles.section} data-shortcuts-section={section.id} aria-label={t(`sections.${section.id}`)}>
              <h2 className={styles.sectionTitle}>{t(`sections.${section.id}`)}</h2>
              {section.groups.map((group, index) => (
                <div key={group.id ?? index}>
                  {group.id ? <h3 className={styles.groupTitle}>{t(`groups.${group.id}`)}</h3> : null}
                  {group.rows.map((row) => {
                    const label = t(`rows.${section.id}.${row.label}`);
                    return <div key={row.label} className={styles.row} data-shortcut-row data-shortcut-id={row.shortcut}>
                      <span className={styles.label}>{marked(label, needle)}</span>
                      <span className={styles.keys} aria-label={t("keysFor", { action: label })}>
                        {shortcutsHelpCaps(row, platform).map((cap, at) => <kbd key={at} className={styles.key}>{marked(cap, needle)}</kbd>)}
                      </span>
                    </div>;
                  })}
                </div>
              ))}
            </section>
          )) : <div className={styles.empty} role="status">
            <h2>{t("noResults")}</h2>
            <p>{t("noResultsHint")}</p>
          </div>}
        </div>
      </div>
    </div>
  );
}
