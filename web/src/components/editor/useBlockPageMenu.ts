import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { useProductPages } from "@/components/product/productPagesContext";
import type { GraphBlock } from "@/contentGraph";
import { copyPageLink } from "./pageLinkClipboard";
import { usePageNames } from "@/lib/pageNames";
import { requestInlineDatabase } from "@/lib/inlineDatabaseRequests";
import { databasePageHref, openPageInNewTab, readDatabasePageViews, readPageLinkToCopy } from "@/lib/databasePageLink";
import { openPagePeek } from "@/lib/pagePeek";
import { hasDesktopTabs } from "@/lib/desktopMenuCommands";
import { openPageInNewWindow } from "@/lib/newWindowRequests";
import { blockTurnChoices } from "./blockTurnInto";
import { syncedBlockMayGoIn } from "./syncedBlockModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { BlockMenuDatabase, BlockMenuDatabaseRow } from "./useBlockDatabaseMenu";

/** The page a sub-page block shows, by its own ID or, in older content, its page ID. */
export function childPageBlockPageId(block: GraphBlock) {
  for (const value of [block.content.childPageId, block.content.pageId]) {
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

/** Where a link block's "새 탭에서 열기" opens its page: a full-page database at its view, as its line's press (databasePageHref). */
export function linkedPageHref(adapter: NativeEditorGraphAdapter, pageId: string) {
  return databasePageHref(adapter.moveContext?.pages.find((candidate) => candidate.id === pageId) ?? { id: pageId }, adapter.session?.user.id);
}

/**
 * A sub-page block's own menu, as Notion's (seen 2026-10-12): "페이지" over
 * turn and color, its icon, a favorite, its name (⌘⇧R); opening it in a new
 * tab (⌘⇧↵) or the side peek (⌥Click); its link, duplicate, move, "보관"
 * and "휴지통으로 이동"; a computer's app opens it in a new window too, under
 * the new tab, as Notion's app (seen 2026-11-16). Its icon and name change on the block itself. A
 * locked page's keeps the favorite, the opens and its link only (Notion,
 * seen 2026-10-31). A full-page database's link, and the new tab or window
 * it opens, name its view (databasePageLink.ts), read as its menu opens.
 */
export function useBlockPageMenu(adapter: NativeEditorGraphAdapter, block: GraphBlock, onDone: () => void,
  notify: (message: string, tone?: "default" | "success" | "error") => void, open = false): BlockMenuDatabase | undefined {
  const pageNames = usePageNames();
  const { t } = useTranslation(["blockHandle", "rowMenu", "pageArchive", "databaseView"]);
  const openImport = useProductPages()?.openImport;
  const context = adapter.moveContext;
  const pageId = block.type === "child_page" ? childPageBlockPageId(block) : "";
  const page = pageId ? context?.pages.find((candidate) => candidate.id === pageId) : undefined;
  const pageMenu = context?.pageMenu;
  const session = adapter.session;
  useEffect(() => {
    if (open && page) void readDatabasePageViews(session, page);
  }, [open, page?.id, page?.kind, session]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!page || !pageMenu || !context || adapter.fixedBody) return undefined;
  const editable = !adapter.readOnly && !page.isLocked;
  const favorite = pageMenu.favoritePageIds.includes(page.id);
  const archived = Boolean(page.archivedAt);
  const ask = (request: "icon" | "rename") => () => { onDone(); requestInlineDatabase(block.id, request); };
  const change = (work: (signal: AbortSignal) => Promise<unknown>, failure: string, success?: string, stayOpen = false) => {
    if (!stayOpen) onDone();
    void work(new AbortController().signal)
      .then(() => { if (success) notify(t(success), "success"); })
      .catch(() => notify(t(failure), "error"));
  };
  // A full-page database's line is the database's, as Notion's (seen 2026-11-05): no turn but into an inline one (BlockHandle),
  // its lock, "추가 옵션" for its name and CSV, no "보관". Once a favorite it has no favorite row, and its name and CSV
  // stand in the menu with the room (seen 2026-11-06).
  const database = page.kind === "database";
  const folded = database && !favorite ? "more" as const : "top" as const;
  const lockable = database && !adapter.readOnly;
  const rows: (BlockMenuDatabaseRow | false)[] = [
    editable && { id: "edit-icon", label: t("blockHandle:database.editIcon"), en: "edit icon", place: "top", run: ask("icon") },
    !(database && favorite) && { id: "favorite", label: t(favorite ? "rowMenu:actions.removeFromFavorites" : "rowMenu:actions.addToFavorites"),
      en: favorite ? "remove from favorites" : "add to favorites", place: "top",
      run: () => change(() => pageMenu.onFavoriteChange(page.id, !favorite), "rowMenu:toast.couldntUpdateFavorites") },
    // Switched on, the menu stays open as a locked database's, which has no lock row: the lock
    // beside the database's title takes it off (Notion's line, seen 2026-11-05, as an inline one's).
    lockable && !page.isLocked && { id: "lock", label: t("databaseView:lockDatabase"), en: "lock database", place: "top",
      attributes: { "data-database-lock": "unlocked" }, switched: false,
      run: () => change(async (signal) => {
        const result = await pageMenu.onPageMetadataChange(page, { isLocked: true }, signal);
        if (result === false) throw new Error("Page lock failed.");
        if (typeof result === "object") context.onPageChange(result);
      }, "rowMenu:toast.couldntChangePageLock", undefined, true) },
    editable && { id: "rename", label: t("blockHandle:media.rename"), en: "rename", place: folded, run: ask("rename"), shortcut: "renamePage" },
    database && editable && !!openImport && { id: "merge-csv", label: t("blockHandle:database.mergeCSV"), en: "merge with csv", place: folded,
      run: () => { onDone(); openImport({ pageId: page.id, title: page.title ?? "", format: "csv", mergeIntoPage: true }); } },
    { id: "open-tab", label: t("blockHandle:database.openInNewTab"), en: "open in new tab", place: "open", shortcut: "openInNewTab",
      run: () => { onDone(); openPageInNewTab(page, session?.user.id); } },
    hasDesktopTabs() && { id: "open-window", label: t("blockHandle:database.openInNewWindow"), en: "open in new window", place: "open",
      run: () => { onDone(); openPageInNewWindow(databasePageHref(page, session?.user.id)); } },
    { id: "open-side", label: t("blockHandle:database.openInSidePeek"), en: "open in side peek", place: "open",
      hint: menuKeyHint("⌥Click"),
      run: () => { onDone(); openPagePeek(page.id); } },
    editable && !database && { id: "archive", label: t(archived ? "pageArchive:unarchive" : "blockHandle:database.archive"),
      en: archived ? "unarchive" : "archive", place: "trash",
      run: () => change(async (signal) => {
        const result = await pageMenu.onPageMetadataChange(page, { isArchived: !archived }, signal);
        if (result === false) throw new Error("Page archive failed.");
        if (typeof result === "object") context.onPageChange(result);
      }, archived ? "rowMenu:toast.couldntUnarchivePage" : "rowMenu:toast.couldntArchivePage") },
  ];
  return {
    header: t(database ? "blockHandle:database.title" : "blockHandle:page.header"),
    page: true,
    copiesWithoutContent: database,
    structureLocked: page.isLocked,
    // "전환" lists a line's choices but "페이지 위치", "페이지" checked, and
    // turns the page back into a line holding its blocks (Notion's, seen
    // 2026-10-13 and 2026-10-14).
    turnChoices: editable && !database ? blockTurnChoices(1, syncedBlockMayGoIn(adapter.graph, block.parentId)).filter((choice) => !choice.placeIn) : undefined,
    rows: rows.filter((row): row is BlockMenuDatabaseRow => row !== false),
    onCopyLink: () => {
      onDone();
      void readPageLinkToCopy(session, page).then((url) => copyPageLink(url, page.id, pageNames.title(page)))
        .then((ok) => notify(ok ? t("rowMenu:toast.copiedLink") : t("rowMenu:toast.couldntCopyLink"), ok ? "success" : "error"));
    },
  };
}
