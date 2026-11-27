import { useEffect, type RefObject } from "react";
import styles from "./SettingsWindow.module.css";

/**
 * A row picked from the settings search, by the section it is in and its
 * title, then the titles of the rows that draw it, shown while it isn't drawn.
 */
export type FoundSettingsRow = { section: string; titles: string[]; picked: number };

// A row is lit for as long as Notion's (seen 2026-11-07: it fades in for a
// quarter second, holds about four and a half, and fades out).
const LIT_MS = 5000;

/**
 * The row a title names in a section as drawn: its name is a row's or a
 * field's heading (`strong`), or a heading of its own. The row is the
 * heading's line and its control together, unless that line sits among
 * other rows (a field in a form), when it is the line alone.
 */
function settingsRowNamed(content: Element, title: string): HTMLElement | null {
  const wanted = title.trim();
  if (!wanted) return null;
  const named = (element: Element) => element.textContent?.trim() === wanted;
  const candidates = [...content.querySelectorAll<HTMLElement>("h1, h2, h3, h4, legend, div, span, label, button")];
  // Its own words alone, or failing that the innermost element that holds
  // them with a mark or a field beside them (a card's title by its icon, a
  // field's label around its input).
  const heading = [...content.querySelectorAll("strong")].find(named)
    ?? candidates.find((element) => element.childElementCount === 0 && named(element))
    ?? candidates.filter(named).find((element) => ![...element.children].some(named));
  // A part named only by its label (a tab list, a region) is its own row.
  if (!(heading instanceof HTMLElement)) {
    return [...content.querySelectorAll<HTMLElement>('[role="tablist"][aria-label], [role="region"][aria-label]')]
      .find((element) => element.getAttribute("aria-label")?.trim() === wanted) ?? null;
  }
  // A tab is a row of its own.
  const tab = heading.closest<HTMLElement>('[role="tab"]');
  if (tab) return tab;
  // A row's button named as the search names it is that row.
  const owner = heading.parentElement;
  if (heading.tagName === "BUTTON" && owner && owner !== content && owner.firstElementChild?.querySelector("strong")) return owner;
  const line = heading.tagName === "STRONG" ? heading.parentElement : heading;
  if (!line || line === content) return heading;
  const row = line.parentElement;
  const alone = row && row !== content && row.firstElementChild === line
    && [...row.children].every((child) => child === line || !child.querySelector("strong"));
  return alone ? row : line;
}

/**
 * A row picked from the settings search opens in sight and lit for a moment,
 * as Notion's does: a row out of sight comes into it with room above or
 * below, one already in sight stays where it is (seen 2026-11-07). It waits
 * for the section's reads to draw the row, until the person scrolls or acts.
 * `beforeReveal` puts the section at its top first if it isn't yet, the row
 * measured from there.
 */
export function useFoundSettingsRow(panelRef: RefObject<HTMLElement | null>, found: FoundSettingsRow | null, section: string, beforeReveal: () => void) {
  useEffect(() => {
    const panel = panelRef.current;
    if (!panel || !found || found.section !== section) return undefined;
    let lit: HTMLElement | null = null;
    let unlit = 0;
    const reveal = () => {
      const content = panel.querySelector("[data-settings-content]") ?? panel;
      const row = found.titles.reduce<HTMLElement | null>((seen, title) => seen ?? settingsRowNamed(content, title), null);
      if (!row) return;
      // A row on a tab not shown is found by its tab, which is chosen: the
      // row is lit once the tab draws it.
      if (row.getAttribute("role") === "tab" && row.getAttribute("aria-selected") !== "true") {
        row.click();
        return;
      }
      stop();
      row.classList.add(styles.foundRow);
      beforeReveal();
      row.scrollIntoView({ block: "nearest" });
      lit = row;
      unlit = window.setTimeout(() => row.classList.remove(styles.foundRow), LIT_MS);
    };
    const observer = new MutationObserver(reveal);
    const events = ["wheel", "touchstart", "keydown", "pointerdown"] as const;
    const stop = () => {
      observer.disconnect();
      window.clearTimeout(timer);
      for (const name of events) panel.removeEventListener(name, stop);
    };
    observer.observe(panel, { childList: true, subtree: true });
    const timer = window.setTimeout(stop, 3000);
    for (const name of events) panel.addEventListener(name, stop, { passive: true });
    reveal();
    return () => {
      stop();
      window.clearTimeout(unlit);
      lit?.classList.remove(styles.foundRow);
    };
  }, [beforeReveal, found, panelRef, section]);
}
