import type { DatabasePageLayout, PageLayoutTab } from "@/lib/databasePageLayout";
import { pageLayoutId } from "./pageLayoutModel";

/** The content tab among a layout's tabs, which no data tab's id is. */
export const CONTENT_TAB_KEY = "~content";

/**
 * A tabbed layout's tabs in the order they stand, the content tab's key
 * among the data tabs' ids: after as many of them as its position says,
 * first when unset, as Notion's builder moves "콘텐츠" among them by its
 * handle (seen 2026-11-24 in Chrome at 1440: dragged under the first tab,
 * the row's pills read "R2 콘텐츠 R2 …"). Every row of the tabs — a page's,
 * the builder's, their lists and a phone's sheets — goes by this one order.
 */
export function layoutTabKeys(layout: Pick<DatabasePageLayout, "tabs" | "contentTab">): string[] {
  const keys = layout.tabs.map(({ id }) => id);
  keys.splice(Math.max(0, Math.min(layout.contentTab?.position ?? 0, keys.length)), 0, CONTENT_TAB_KEY);
  return keys;
}

/** The layout with its tabs (the content tab among them) in this order. */
function withTabOrder(layout: DatabasePageLayout, keys: readonly string[]): DatabasePageLayout {
  const tabs = keys.flatMap((key) => layout.tabs.find(({ id }) => id === key) ?? []);
  const position = keys.indexOf(CONTENT_TAB_KEY);
  const { position: _kept, ...contentTab } = layout.contentTab ?? {};
  const placed = position > 0 ? { ...contentTab, position } : contentTab;
  return { ...layout, tabs, contentTab: Object.keys(placed).length > 0 ? placed : undefined };
}

/** A tab, or the content tab, dropped before or after another. */
export function moveLayoutTab(layout: DatabasePageLayout, source: string, target: string, side: "before" | "after"): DatabasePageLayout {
  if (source === target) return layout;
  const keys = layoutTabKeys(layout).filter((key) => key !== source);
  const at = keys.indexOf(target);
  if (at < 0) return layout;
  keys.splice(side === "after" ? at + 1 : at, 0, source);
  return withTabOrder(layout, keys);
}

/** A data tab taken out, the content tab keeping its place among the rest. */
export function removeLayoutTab(layout: DatabasePageLayout, tabId: string): DatabasePageLayout {
  return withTabOrder(layout, layoutTabKeys(layout).filter((key) => key !== tabId));
}

/** The simple structure: no data tabs, the content tab's own settings kept. */
export function removeLayoutTabs(layout: DatabasePageLayout): DatabasePageLayout {
  return { ...withTabOrder(layout, [CONTENT_TAB_KEY]), structure: "simple" };
}

// A tab's copy joins the end of the tabs named "〈name〉 (1)", a name already
// ending in "(n)" going on to "(n+1)", whatever names the tabs have, as
// Notion's builder (seen 2026-11-12: 대상 → 대상 (1), again → 대상 (1),
// 대상 (1) → 대상 (2)).
function duplicatedTabName(name: string | undefined) {
  if (!name) return name;
  const numbered = /^(.*) \((\d+)\)$/u.exec(name);
  return numbered ? `${numbered[1]} (${Number(numbered[2]) + 1})` : `${name} (1)`;
}

/**
 * A builder's data tab's copy and removal, and the moves of its tabs, on the
 * layout being made: a copy is chosen (its ID given back); a tab removed
 * leaves the first tab chosen, as Notion's builder (seen 2026-11-24: the
 * shown tab deleted from its list, the first tab ticked).
 */
export function layoutTabActions(draft: DatabasePageLayout | null, setDraft: (change: (current: DatabasePageLayout | null) => DatabasePageLayout | null) => void,
  choose: (tabId: string | null) => void) {
  return {
    tab: (tabId: string) => ({
      duplicate: () => {
        const id = pageLayoutId("tab");
        setDraft((current) => {
          const tab = current?.tabs.find((candidate) => candidate.id === tabId);
          return current && tab ? { ...current, tabs: [...current.tabs, { ...tab, id, name: duplicatedTabName(tab.name) } satisfies PageLayoutTab] } : current;
        });
        choose(id);
        return id;
      },
      remove: () => {
        setDraft((current) => current && removeLayoutTab(current, tabId));
        const first = draft ? layoutTabKeys(removeLayoutTab(draft, tabId))[0] : CONTENT_TAB_KEY;
        choose(first === CONTENT_TAB_KEY ? null : first);
      },
    }),
    move: (source: string, target: string, side: "before" | "after") => setDraft((current) => current && moveLayoutTab(current, source, target, side)),
  };
}
