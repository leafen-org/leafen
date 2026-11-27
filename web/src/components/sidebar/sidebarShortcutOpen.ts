import { pageHref } from "@/lib/navigation";
import { pagePeekHref } from "@/lib/pagePeek";
import { pushRoute } from "@/lib/router";
import { collectAuthenticatedContentGraph } from "@/lib/authenticatedContentGraph";
import { holdTypingForField, holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import type { Page, Session } from "@/types";
import type { SidebarLayoutShortcut, SidebarOpenMode } from "@/workspaceMembershipWire.generated";
import { sidebarShortcutOpenMode } from "./sidebarLayoutModel";
import { createSidebarViewRow, focusNewRowTitle, heldSidebarGraph, sidebarShortcutView } from "./sidebarViewNewRow";
import { markFreshRow } from "@/components/database/freshRows";

export type SidebarShortcutActions = {
  session: Session | null;
  /** The workspace the sidebar shows, where a database's new page is made here first. */
  workspaceId?: string;
  pages: readonly Page[];
  openSettings(): void;
  openLibrary(): void;
  /** Opens the library's documents. */
  openDocuments(): void;
  openMyTasks(): void;
  /** Opens a page as a full page. */
  openPage(page: Page): void;
  /** Makes a page at the top of the private pages without opening it; a database starts as one. */
  createRootPage(database: boolean): Promise<Page | null>;
  /** Makes a page in a page without opening it. */
  createSubpage(parentId: string): Promise<Page | null>;
};

/** Opens a page where a shortcut or section says: beside the open page, over its center or full. */
export function openSidebarPageIn(pageId: string, mode: SidebarOpenMode, actions: Pick<SidebarShortcutActions, "pages" | "openPage">) {
  const page = actions.pages.find((candidate) => candidate.id === pageId);
  if (mode !== "full") pushRoute(pagePeekHref(pageId, mode));
  else if (page) actions.openPage(page);
  else pushRoute(pageHref(pageId));
}

/**
 * A shortcut does what it names, as Notion's (seen 2026-10-01 and
 * 2026-10-02): a new page in its page (or at the top of the private pages)
 * and a page open where the shortcut says, a full page at first; a new
 * database starts at the top of the private pages and opens; a database's
 * new page is a row first in it, from the shortcut's template, opened where
 * the shortcut says with the cursor in its title (seen 2026-10-30); the settings,
 * the library, its documents, the tasks and an app open. A database's new
 * page is made on this device at the press from the database as read ahead
 * (useSidebarShortcutAhead), as a view section's "+" makes its row, and goes
 * to the server behind (user decision 2026-11-12: a press answers on the
 * next frame); read first at the press, it opened two round trips later and
 * not at all with the server out of reach (2026-11-14). Not read yet, the
 * database is read first, as before.
 */
export async function openSidebarShortcut(shortcut: SidebarLayoutShortcut, actions: SidebarShortcutActions) {
  if (shortcut.kind === "settings") actions.openSettings();
  else if (shortcut.kind === "library") actions.openLibrary();
  else if (shortcut.kind === "documents") actions.openDocuments();
  else if (shortcut.kind === "my_tasks") actions.openMyTasks();
  else if (shortcut.kind === "app") pushRoute(`/apps/${encodeURIComponent(shortcut.pageId)}`);
  else if (shortcut.kind === "page") openSidebarPageIn(shortcut.pageId, sidebarShortcutOpenMode(shortcut), actions);
  else if (shortcut.kind === "new_database_page") {
    const session = actions.session;
    if (!session) return;
    await holdTypingForField(async () => {
      const held = actions.workspaceId ? heldSidebarGraph(session, shortcut.pageId) : undefined;
      const read = held ?? await collectAuthenticatedContentGraph(session, shortcut.pageId, null, () => true).catch(() => null);
      if (!read?.canEdit) return false;
      const here = held && actions.workspaceId ? { workspaceId: actions.workspaceId, shown: () => {} } : undefined;
      const made = await createSidebarViewRow(session, sidebarShortcutView(shortcut), read.graph, here).catch(() => null);
      // One kept for a server out of reach waits to be sent; its page can't open meanwhile.
      if (!made || made.kept) return false;
      const { rowId } = made;
      // Left untouched it goes, as Notion's (freshRows.ts); a template's page stays.
      if (!shortcut.templateId) markFreshRow(rowId, shortcut.pageId, made.values);
      // Its title takes the caret with what was typed meanwhile, as any new page's.
      requestPageTitleFocus(rowId);
      openSidebarPageIn(rowId, sidebarShortcutOpenMode(shortcut), { pages: [], openPage: () => {} });
      focusNewRowTitle(rowId);
      return true;
    });
  } else if (shortcut.kind === "new_database") await holdTypingWhile(async () => {
    const database = await actions.createRootPage(true).catch(() => null);
    if (database) { requestPageTitleFocus(database.id); actions.openPage(database); }
  });
  else await holdTypingWhile(async () => {
    const page = await (shortcut.pageId ? actions.createSubpage(shortcut.pageId) : actions.createRootPage(false)).catch(() => null);
    if (page) {
      requestPageTitleFocus(page.id);
      openSidebarPageIn(page.id, sidebarShortcutOpenMode(shortcut), { ...actions, pages: [...actions.pages, page] });
    }
  });
}
