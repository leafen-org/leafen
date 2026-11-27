import { useEffect } from "react";
import { SIDEBAR_HOME_TAB, changeSidebarSection, sidebarSection } from "@/components/sidebar/sidebarLayoutModel";
import type { LibraryViewId } from "@/lib/router";
import type { SidebarLayout, SidebarSectionSort } from "@/workspaceMembershipWire.generated";
import { defaultLibraryColumns, defaultLibrarySort, type LibrarySort } from "./libraryModel";
import { type LibraryViewSettings, useLibraryViewSettings } from "./useLibraryData";

/** The sidebar section's "최종 편집 일시" as a library sort. */
const EDITED_FIRST: LibrarySort = { column: "edited", direction: "descending" };

const sameSort = (left: LibrarySort | null | undefined, right: LibrarySort) =>
  Boolean(left && left.column === right.column && left.direction === right.direction);

/**
 * A library view's settings, the private and the shared pages' sort one
 * setting with the sidebar's section of the same pages, as Notion's (seen
 * 2026-11-16 at 1440, each way twice): the section's "정렬 · 수동" leaves the
 * view unsorted and its "최종 편집 일시" sorts it by last edit, newest first,
 * while the view has no sort of its own; one of its own (by name, say)
 * stays whatever the section is set to and doesn't change the section, and
 * taken off, the view follows the section again. The view's "수동으로 전환"
 * turns the section to "수동" (`setManual`). Here the view's sort by last edit
 * alone is the section's "최종 편집 일시" and taking it off its "수동"
 * (Notion's private view shows that sort with no pill to take off). The
 * section is the home tab's. A private view put by hand before the two were
 * one (its sort taken off, kept in this browser) moves to the section once.
 */
export function useLibrarySectionSort({ userId, workspaceId, view, sidebar, keepShownOrder }: {
  userId: string;
  workspaceId: string;
  view: LibraryViewId;
  sidebar: { layout: SidebarLayout; loaded: boolean; change(layout: SidebarLayout): void };
  /** Puts the shown order by hand before the section turns to "수동" (useLibraryRowOrder). */
  keepShownOrder?: () => void;
}) {
  const [stored, store] = useLibraryViewSettings(userId, `${workspaceId}:${view}`, defaultLibraryColumns(view), defaultLibrarySort(view));
  const sectionId = view === "private" || view === "shared" ? view : null;
  const sectionSort: SidebarSectionSort | null = sectionId ? sidebarSection(sidebar.layout, SIDEBAR_HOME_TAB, sectionId).sort : null;
  const follows = sectionId !== null && !stored.moreSorts?.length && (!stored.sort || sameSort(stored.sort, EDITED_FIRST));
  const settings: LibraryViewSettings = follows ? { ...stored, sort: sectionSort === "edited" ? EDITED_FIRST : null, moreSorts: [] } : stored;
  const setSection = (sort: SidebarSectionSort) => {
    if (sectionId && sort !== sectionSort) sidebar.change(changeSidebarSection(sidebar.layout, SIDEBAR_HOME_TAB, sectionId, { sort }));
  };
  const following = (next: LibraryViewSettings): LibraryViewSettings => ({ ...next, sort: null, moreSorts: [], section: true });

  // A private view put by hand before (its sort taken off, no mark of the
  // section) moves that to the section, once the section is known.
  const legacyManual = view === "private" && !stored.section && stored.sort === null && !stored.moreSorts?.length;
  useEffect(() => {
    if (!legacyManual || !sidebar.loaded) return;
    setSection("manual");
    store(following(stored));
  });

  function change(next: LibraryViewSettings, options: { reset?: boolean } = {}) {
    if (!sectionId) {
      store(next);
      return;
    }
    const sorts = [...(next.sort ? [next.sort] : []), ...(next.moreSorts ?? [])];
    if (options.reset) {
      store(following(next));
    } else if (sorts.length === 1 && sameSort(sorts[0], EDITED_FIRST)) {
      setSection("edited");
      store(following(next));
    } else if (sorts.length === 0) {
      store(following(next));
      // The section's own sort taken off is its "수동", the order shown kept.
      if (follows && sectionSort === "edited") {
        keepShownOrder?.();
        setSection("manual");
      }
    } else {
      store({ ...next, section: true });
    }
  }

  return {
    settings,
    change,
    /** The view follows its section, whose sort it shows as its own. */
    follows,
    /** The view's "수동으로 전환": its section turns to "수동" and it follows. */
    setManual() {
      setSection("manual");
      if (!follows) store(following(stored));
    },
  };
}
