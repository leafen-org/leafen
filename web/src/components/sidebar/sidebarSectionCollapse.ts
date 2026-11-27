import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";

/** The sections a person folded, kept in this browser, as theirs alone. */

const SIDEBAR_SECTION_COLLAPSED_KEY = "leafen:sidebar-section-collapsed";

/** A known section, or a teamspace's own section by its key ("teamspace:<id>"). */
export type CollapsibleSection = "favorites" | "private" | "shared" | "teamspaces" | "recents" | `teamspace:${string}`;

const COLLAPSIBLE_SECTIONS: readonly string[] = ["favorites", "private", "shared", "teamspaces", "recents"];

export function readCollapsedSections(): Set<CollapsibleSection> {
  const parsed = parseStored(personValue(SIDEBAR_SECTION_COLLAPSED_KEY, { previous: [SIDEBAR_SECTION_COLLAPSED_KEY] }));
  return new Set(Array.isArray(parsed)
    ? parsed.filter((section): section is CollapsibleSection => typeof section === "string"
      && (COLLAPSIBLE_SECTIONS.includes(section) || /^teamspace:.{1,255}$/u.test(section)))
    : []);
}

export function writeCollapsedSections(sections: ReadonlySet<CollapsibleSection>) {
  storePersonValue(SIDEBAR_SECTION_COLLAPSED_KEY, JSON.stringify([...sections]));
}
