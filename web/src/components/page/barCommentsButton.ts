/**
 * The bar's comments button and edited time beside the panel the page docks,
 * one rule for every bar that holds them — a page's (TopBar), a database
 * page's or template's opened whole and their peeks (DatabaseRowPageActions),
 * a page's peek (PagePeekActions) — as Notion's (seen 2026-11-25 in Chrome at
 * 1440, dpr 1, two tabs at different times: a page, a database template
 * opened whole and its window beside its database).
 */

/** The panel docked beside the page, as its bar reads it: all its discussions, another (its updates, an app), or none. */
export type BarDockedPanel = "discussions" | "other" | null;

/** Which panel the page docks, from its bar's "»" (`closePanel`, there while one is docked) and whether that is all its discussions. */
export function barDockedPanel(closePanel: unknown, discussionsDocked: boolean | undefined): BarDockedPanel {
  return !closePanel ? null : discussionsDocked ? "discussions" : "other";
}

/**
 * What the comments button does: over its docked "모든 토론" it closes it;
 * over another docked panel (its updates, an app) it turns that panel into
 * "모든 토론" at once, with no "토론" list over it; with none docked it opens
 * or closes the "토론" list (seen 2026-11-25, on a page and a database row
 * opened whole).
 */
export function barCommentsPress(docked: BarDockedPanel, act: {
  closeDocked?: () => void; openAll?: () => void; toggleList: () => void;
}) {
  if (docked === "discussions") act.closeDocked?.();
  else if (docked && act.openAll) act.openAll();
  else act.toggleList();
}

/**
 * How the comments button shows: lit with the hover tint (white .055 in the
 * dark) while its "토론" list is open or "모든 토론" is docked beside the
 * page, the pointer away; its tip "모든 토론 닫기" over docked discussions,
 * "모든 토론 보기" over another docked panel, which it turns into them, and
 * "모든 새 토론 보기" otherwise. The tint is the bar's (TopBarPanel.module.css
 * `panelButton`), the tip a topBar key.
 */
export function barCommentsLook(docked: BarDockedPanel, listOpen: boolean) {
  return {
    open: listOpen || docked === "discussions",
    tip: docked === "discussions" ? "topBar:closeDiscussionsTip" : docked ? "topBar:viewAllDiscussionsTip" : "topBar:discussionsTip",
  } as const;
}

/** The edited time, which opens the page's updates, is left out while a panel is docked beside the page. */
export function barEditedTimeShown(docked: BarDockedPanel) {
  return docked === null;
}
