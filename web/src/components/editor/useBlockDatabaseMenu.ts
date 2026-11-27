import { useEffect, useState } from "react";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import type { GraphBlock } from "@/contentGraph";
import { useProductPages } from "@/components/product/productPagesContext";
import { useOptionalProductToast } from "@/lib/productToast";
import { matchesKeyboardShortcut, type KeyboardShortcutEventLike, type KeyboardShortcutId } from "@/lib/keyboardShortcuts";

/** Runs the database menu's row whose keys were pressed while the menu is open, as Notion's ⌘⇧↵ and ⌘⇧R do. */
export function runDatabaseMenuShortcut(database: BlockMenuDatabase | undefined, event: KeyboardShortcutEventLike & { preventDefault(): void }) {
  const row = database?.rows.find((candidate) => candidate.shortcut && !candidate.disabled && matchesKeyboardShortcut(candidate.shortcut, event));
  if (!row) return false;
  event.preventDefault();
  row.run();
  return true;
}
import { copyText } from "@/lib/copyText";
import { addressToCopy } from "@/lib/databasePageLink";
import { chosenDatabaseView } from "@/lib/databaseViewChoice";
import { inlineDatabaseFullPage, inlineDatabaseFullPageHref, inlineDatabaseShownName } from "@/lib/inlineDatabaseFullPage";
import { inlineDatabasePeekHref } from "@/lib/pagePeek";
import { requestInlineDatabase } from "@/lib/inlineDatabaseRequests";
import { useRouter } from "@/lib/router";
import type { PageActionAccess } from "@/workspaceMembershipWire.generated";
import type { BlockTurnChoice } from "./blockTurnInto";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { inlineDatabaseViews } from "./InlineDatabaseBlock";
import { databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import { keepFreshDatabaseTurned } from "@/lib/freshDatabases";

/** A row of an inline database's own block menu: at its top, in "추가 옵션", among the ways to open it, or before "휴지통으로 이동". */
export type BlockMenuDatabaseRow = { id: string; label: string; en: string; place: "top" | "more" | "open" | "trash"; run(): void;
  attributes?: Record<string, unknown>;
  /** The keys that run it while the menu is open, shown beside it. */
  shortcut?: KeyboardShortcutId;
  /** What else opens it, shown beside it, as ⌥Click. */
  hint?: string;
  /** Why it can't be chosen now, shown over it while it is dimmed. */
  disabled?: string;
  /** Drawn with a switch, on or off, as Notion's lock row (seen 2026-11-05). */
  switched?: boolean };

/** What an inline database's own block menu offers beside a block's turn into a page, duplicate, move and delete. */
export type BlockMenuDatabase = {
  /**
   * Its "복제 ›" offers "콘텐츠 제외 복제" too, as Notion's database lines,
   * inline, full-page and linked (seen 2026-11-05 and 2026-11-25: a linked
   * one's copy without content is the same linked view, its source's rows shown).
   */
  copiesWithoutContent?: boolean;
  /** A linked database whose views are locked keeps no duplicate, move or trash in its menu (Notion's, seen 2026-10-13). */
  structureLocked?: boolean;
  /** "데이터베이스 · 표": the kind of its shown view. */
  header: string;
  rows: BlockMenuDatabaseRow[];
  /** Its link, which opens it alone; without one the menu copies the block's. */
  onCopyLink?: () => void;
  /** A sub-page block's menu (useBlockPageMenu), which turns and colors as a block does. */
  page?: true;
  /** What a sub-page block turns into, as a line does but for "페이지 위치". */
  turnChoices?: BlockTurnChoice[];
};

/**
 * An inline database's block menu, as Notion's (seen 2026-10-10, again
 * 2026-11-25): "간단한 표로 전환", its icon, a favorite, its lock, its name
 * (⌘⇧R) and a CSV merge — in that order, a linked database's without the
 * simple table and the merge; opening it alone as a page here, in a new tab or
 * in the side peek, its link, which opens it so. "보관" — "보관 취소" once
 * archived, whose notice "보관된 페이지" offers "보관 취소" (seen 2026-10-11) —
 * stays before "휴지통으로 이동" though Notion's menus dropped it.
 * "간단한 표로 전환" asks full access to the page and is dimmed with Notion's
 * reason without it (seen 2026-10-11).
 */
/**
 * Five rows stand above the opening rows: when there are more, the first four
 * and "추가 옵션" with the rest (Notion's in every view, seen 2026-11-05 and
 * 2026-11-25: a one-view table, with "간단한 표로 전환", folds its rename and
 * merge under it; a board, a table among other views or a favorite has them in the menu).
 */
function overflowingToMore(rows: BlockMenuDatabaseRow[], leftOut = 0) {
  const standing = rows.filter((row) => row.place === "top" || row.place === "more");
  const room = standing.length + leftOut > 5 ? 4 : 5;
  return rows.map((row) => {
    const index = standing.indexOf(row);
    return index < 0 ? row : { ...row, place: index < room ? "top" as const : "more" as const };
  });
}

/** The view an inline database shows its reader, whose kind its menu's head names ("데이터베이스 · 표"). */
export function inlineDatabaseShownView(adapter: NativeEditorGraphAdapter, block: GraphBlock, views = inlineDatabaseViews(adapter.graph, block).views) {
  return chosenDatabaseView(views, adapter.session?.user.id, block.id, block.content.viewId);
}

export function useBlockDatabaseMenu(adapter: NativeEditorGraphAdapter, block: GraphBlock, onDone: () => void,
  notify: (message: string, tone?: "default" | "success" | "error") => void, open = false): BlockMenuDatabase | undefined {
  const { t } = useTranslation(["blockHandle", "databaseView", "pageArchive", "rowMenu"]);
  const router = useRouter();
  const toast = useOptionalProductToast();
  const productPages = useProductPages();
  const [canManage, setCanManage] = useState(false);
  const session = adapter.session, pageId = adapter.pageId, isDatabase = block.type === "inline_database";
  useEffect(() => {
    if (!open || !session || !isDatabase) return;
    const controller = new AbortController();
    void request<PageActionAccess>(`/pages/${encodeURIComponent(pageId)}?accessOnly=1`, { signal: controller.signal }, session)
      .then((access) => setCanManage(access.canManage === true), () => undefined);
    return () => controller.abort();
  }, [isDatabase, open, pageId, session]);
  const viewAdapter = adapter.database.databaseViewAdapter;
  if (block.type !== "inline_database" || adapter.fixedBody || !viewAdapter) return undefined;
  // A view block's menu — a linked database's, a waiting link's or a
  // dashboard's (databaseViewBlock; Notion's, seen 2026-10-13, 2026-11-09 and
  // 2026-11-25) — keeps its icon, favorite, lock (named "데이터베이스 잠금" as
  // a database's, though it locks the block's views, whose settings end in
  // "보기 잠금 해제"), "이름 바꾸기 ⌘⇧R", the ways to open it and its link; it
  // has no simple table, CSV merge or archive. A linked
  // database's icon and "이름 바꾸기" are its own name's and icon's, its
  // source's staying as they are (seen 2026-10-15). Once locked, a database's
  // menu, linked or not, keeps only the favorite, the opens, the link and
  // "여기에서 발표"; the lock's switch goes too, the lock mark by the title
  // unlocking it (Notion's, seen 2026-11-05).
  const { memberIds, views } = inlineDatabaseViews(adapter.graph, block);
  const viewBlock = databaseViewBlock(block, adapter.graph.views);
  const view = inlineDatabaseShownView(adapter, block, views);
  const titleSource = adapter.graph.dataSources.find((candidate) => candidate.id === memberIds[0]);
  const locked = block.content.locked === true;
  const archived = block.content.archived === true;
  const editable = !adapter.readOnly && !adapter.database.structureLocked;
  const href = adapter.session && view ? inlineDatabaseFullPageHref(adapter.pageId, block.id, view.id) : undefined;
  const ask = (request: "icon" | "rename") => () => { onDone(); requestInlineDatabase(block.id, request); };
  const openImport = productPages?.openImport;
  const where = { pageId: adapter.pageId, workspaceId: adapter.workspaceId, currentUserId: adapter.session?.user.id };
  // Kept among the reader's favorites, as Notion's block menu does (seen 2026-10-10);
  // once it is one, the menu has no favorite row, so its rename and merge stand
  // in the menu with the room (Notion's, seen 2026-11-06).
  const setFavorite = productPages?.setDatabaseFavorite;
  const favorite = productPages?.databaseFavorites?.some((item) => item.containerId === block.id && item.favorite) === true;
  // Only a database whose one view is a table turns into a simple table, as
  // Notion's (seen 2026-11-05; 2026-11-25: a second view of any kind, a table
  // too, takes the row away, and it comes back once that view is gone).
  const table = views.length === 1 && view?.type === "table";
  const rows: (BlockMenuDatabaseRow | false)[] = [
    editable && !viewBlock && !locked && table && { id: "to-table", label: t("blockHandle:database.turnIntoSimpleTable"), en: "turn into simple table",
      place: "top", attributes: { "data-database-simple-table": true, title: canManage ? undefined : t("blockHandle:database.simpleTableNeedsFullAccess") },
      disabled: canManage ? undefined : t("blockHandle:database.simpleTableNeedsFullAccess"),
      run: () => { onDone(); void import("./simpleTableFromDatabase").then(({ simpleTableFromDatabase }) => {
        let tooLarge = false;
        adapter.changeLayout((graph) => {
          const turned = simpleTableFromDatabase(graph, block, view!, where);
          tooLarge = !turned;
          // One just made comes back with the turn's Undo as it was made (freshDatabases.ts).
          if (turned) keepFreshDatabaseTurned(view!.dataSourceId);
          return turned;
        });
        if (tooLarge) notify(t("blockHandle:database.tooLargeToTurn"), "error");
      }); } },
    editable && !locked && !!titleSource && { id: "edit-icon", label: t("blockHandle:database.editIcon"), en: "edit icon", place: "top", run: ask("icon") },
    !!session && !!setFavorite && !favorite && { id: "favorite", label: t("rowMenu:actions.addToFavorites"),
      en: "add to favorites", place: "top", attributes: { "data-database-favorite": "off" },
      run: () => {
        onDone();
        // Listed at once by the name it goes by (inlineDatabaseShownName), one of several unnamed by its sources'.
        const opened = inlineDatabaseFullPage(adapter.graph, block.id);
        void setFavorite!({ containerId: block.id, pageId: adapter.pageId, workspaceId: adapter.workspaceId, title: opened ? inlineDatabaseShownName(opened).title : "" }, true)
          .catch(() => notify(t("rowMenu:toast.couldntUpdateFavorites"), "error"));
      } },
    // Switched on, it stays open, now as a locked database's (Notion's, seen 2026-11-05).
    editable && !locked && { id: "lock", label: t("databaseView:lockDatabase"), en: "lock database", place: "top", switched: false,
      run: () => viewAdapter.setLocked(block, true), attributes: { "data-database-lock": "unlocked" } },
    editable && !locked && (viewBlock || !!titleSource) && { id: "rename", label: t("blockHandle:media.rename"), en: "rename", place: "more",
      run: ask("rename"), shortcut: "renamePage" },
    editable && !viewBlock && !locked && !!openImport && !!view && { id: "merge-csv", label: t("blockHandle:database.mergeCSV"), en: "merge with csv", place: "more", run: () => {
      onDone();
      openImport!({ pageId: adapter.pageId, title: titleSource?.title ?? "", format: "csv", mergeIntoPage: true, mergeDataSourceId: view!.dataSourceId });
    } },
    !!href && { id: "open-page", label: t("blockHandle:database.openAsPage"), en: "open as page", place: "open", run: () => { onDone(); router.push(href!); } },
    !!href && { id: "open-tab", label: t("blockHandle:database.openInNewTab"), en: "open in new tab", place: "open",
      run: () => { onDone(); window.open(href, "_blank", "noopener,noreferrer"); }, shortcut: "openInNewTab" },
    !!href && { id: "open-side", label: t("blockHandle:database.openInSidePeek"), en: "open in side peek", place: "open",
      hint: menuKeyHint("⌥Click"),
      run: () => { onDone(); router.push(inlineDatabasePeekHref(adapter.pageId, block.id, "side", view?.id)); } },
    !adapter.readOnly && !viewBlock && !locked && { id: "archive", label: t(archived ? "pageArchive:unarchive" : "blockHandle:database.archive"),
      en: archived ? "unarchive" : "archive", place: "trash", attributes: { "data-database-archive": archived ? "archived" : "shown" },
      run: () => {
        onDone();
        viewAdapter.setArchived(block, !archived);
        if (!archived) {
          toast?.notify(t("pageArchive:archivedPages"), "default", {
            label: t("pageArchive:unarchive"), onClick: () => viewAdapter.setArchived({ ...block, content: { ...block.content, archived: true } }, false),
          });
        }
      } },
  ];
  return {
    header: view ? t("blockHandle:database.header", { view: t(`databaseView:viewTypes.${view.type}`) }) : t("blockHandle:database.title"),
    copiesWithoutContent: true,
    // A calendar's or timeline's "날짜 지우기" isn't here; what stands around it stands as in Notion.
    rows: overflowingToMore(rows.filter((row): row is BlockMenuDatabaseRow => row !== false), view?.type === "calendar" || view?.type === "timeline" ? 1 : 0),
    ...(locked ? { structureLocked: true } : {}),
    ...(href ? { onCopyLink: () => {
      onDone();
      // The view the address names rides on it, else the one shown (addressToCopy).
      void copyText(addressToCopy(block.id, view?.id ?? null)).then((ok) => notify(ok ? t("blockHandle:database.copiedLink") : t("blockHandle:notifications.couldNotCopyLink"),
        ok ? "success" : "error"));
    } } : {}),
  };
}
