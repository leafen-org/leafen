import { api } from "@/api";
import { i18next } from "@/i18n";
import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { databaseBlockMembers } from "@/lib/nativeDataSourceManagement";
import { readDatabaseSprints } from "@/databaseToolbarConfigApi";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { Page, Session } from "@/types";
import { confirmAction, confirmActions } from "../ConfirmActionDialog";
import type { useOptionalProductToast } from "@/lib/productToast";
import type { EditorPageMenu, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { childPageBlockPageId } from "./useBlockPageMenu";
import { pageMapOf } from "@/lib/pageIndex";

const DATABASE_BLOCKS = new Set(["inline_database", "child_database"]);

/**
 * Whether a database block holds a source of its own that the page shows.
 * One that only links others' sources goes without asking, as Notion's
 * linked databases do (seen 2026-10-12): nothing but the view goes with it;
 * so does one whose own source the page doesn't hold ("데이터베이스를 사용할
 * 수 없습니다", InlineDatabaseBlock), which takes nothing but its block.
 */
function holdsOwnSource(block: GraphBlock, shown: ReadonlySet<string>) {
  return DATABASE_BLOCKS.has(block.type) && databaseBlockMembers(block).own.some((id) => shown.has(id));
}

function shownSources(graph: Pick<ContentGraph, "dataSources">) {
  return new Set(graph.dataSources.map(({ id }) => id));
}

/** These blocks with what's inside them. */
function removedBlockIds(graph: Pick<ContentGraph, "blocks">, ids: readonly string[]) {
  const removing = new Set(ids);
  // Each pass takes in the children of what's already taken; a page's blocks nest only so deep.
  for (let grew = true; grew;) {
    grew = false;
    for (const block of graph.blocks) {
      if (!removing.has(block.id) && block.parentId && removing.has(block.parentId)) {
        removing.add(block.id);
        grew = true;
      }
    }
  }
  return removing;
}

/** Whether removing these blocks, with what's inside them, takes a database's own source along. */
export function removesDatabase(graph: Pick<ContentGraph, "blocks" | "dataSources">, ids: readonly string[]) {
  const removing = removedBlockIds(graph, ids);
  const shown = shownSources(graph);
  return graph.blocks.some((block) => removing.has(block.id) && holdsOwnSource(block, shown));
}

/**
 * The pages whose lines these blocks, with what's inside them, take off the
 * page — sub-pages and full-page databases: they go to the trash with them.
 */
export function removedPages(graph: Pick<ContentGraph, "blocks">, ids: readonly string[], pages: readonly Page[]) {
  const removing = removedBlockIds(graph, ids);
  const byId = pageMapOf(pages);
  return graph.blocks.flatMap((block) => {
    if (!removing.has(block.id) || (block.type !== "child_page" && block.type !== "child_database")) return [];
    const page = byId.get(childPageBlockPageId(block));
    return page ? [page] : [];
  });
}

/** The full-page databases among them, which ask before they go (confirmBlockRemoval). */
export function removedDatabasePages(graph: Pick<ContentGraph, "blocks">, ids: readonly string[], pages: readonly Page[]) {
  return removedPages(graph, ids, pages).filter((page) => page.kind === "database");
}

/**
 * Pages whose lines were taken off the page went to the trash — sub-pages and
 * full-page databases alike: a notice says so with "복원하기", which brings
 * them back, each line last in the block it was in (the server's restore), as
 * Notion's (seen 2026-11-07 for a database; 2026-11-26 in Chrome, two tabs at
 * different times, for sub-pages: one or two from the block menu, with a line
 * before or after them whatever the row's name, the Delete key and ⌘X, each
 * "휴지통으로 이동됨 · 복원하기"; ⌘Z meanwhile brings the line back in place
 * and the notice stays). The one place a page's line going announces it;
 * the sidebar, the page's "⋯" and a database's rows say their own.
 */
export function announcePagesTrashed(
  toast: ReturnType<typeof useOptionalProductToast>, pageMenu: EditorPageMenu | undefined, pages: readonly Page[],
) {
  if (!pages.length || !pageMenu || !toast) return;
  toast.notify(i18next.t("rowMenu:toast.movedToTrash"), "success", {
    label: i18next.t("rowMenu:trashDatabase.restore"),
    onClick: () => {
      void Promise.all(pages.map((page) => pageMenu.onPageMetadataChange(
        { ...page, inTrash: true }, { inTrash: false }, new AbortController().signal,
      ).then((restored) => { if (restored === false) throw new Error("Restore failed."); })))
        .catch(() => toast.notify(i18next.t("rowMenu:toast.couldntRestorePage"), "error"));
    },
  });
}

/**
 * Takes chosen blocks off the page in one step (`run`) and says what went:
 * how many to a screen reader, and the pages among them to the trash
 * (announcePagesTrashed) — from the block menu, the Delete key and ⌘X alike.
 */
export function removeBlocksAnnounced(adapter: Pick<NativeEditorGraphAdapter, "graph" | "remove" | "moveContext">, ids: readonly string[],
  run: (step: () => void) => void, toast: ReturnType<typeof useOptionalProductToast>) {
  const pages = removedPages(adapter.graph, ids, adapter.moveContext?.pages ?? []);
  run(() => adapter.remove(ids));
  announceBlocksDeleted(toast, ids.length);
  announcePagesTrashed(toast, adapter.moveContext?.pageMenu, pages);
}

/** Blocks gone from a page are said to a screen reader only, "1개 블록 삭제됨", as Notion's (seen 2026-11-08). */
export function announceBlocksDeleted(toast: ReturnType<typeof useOptionalProductToast>, count: number) {
  if (!toast || count < 1) return;
  toast.announce(count === 1 ? i18next.t("blockItem:deletedBlock") : i18next.t("blockItem:deletedBlocks", { count }));
}

function confirmDatabaseTrash() {
  return confirmAction({
    message: i18next.t("rowMenu:trashDatabase.title"), detail: i18next.t("rowMenu:trashDatabase.detail"),
    confirmLabel: i18next.t("rowMenu:actions.moveToTrash"), danger: "filled",
  });
}

/**
 * Removing a database block asks first, as Notion's inline database does
 * (seen 2026-10-08: "데이터베이스를 휴지통으로 이동하시겠어요?") and its
 * full-page database's line (seen 2026-11-07); a linked database and other
 * blocks go at once.
 */
export async function confirmBlockRemoval(
  graph: Pick<ContentGraph, "blocks" | "dataSources">, ids: readonly string[],
  fullPage?: { session: Session | null; databases: readonly Page[] },
) {
  if (removesDatabase(graph, ids)) return confirmDatabaseTrash();
  const database = fullPage?.databases[0];
  if (!database) return true;
  return fullPage.session ? confirmDatabasePageTrash(fullPage.session, database.id) : confirmDatabaseTrash();
}

/**
 * Moving a full-page database to the trash asks the same, unless its page
 * only links other databases' sources, which Notion moves at once (seen
 * 2026-10-12). A page that can't be read asks.
 */
export async function confirmDatabasePageTrash(session: Session, pageId: string) {
  const read = await api.contentGraph(session, pageId).catch(() => null);
  const blocks = read?.items.flatMap((item) => item.kind === "block" ? [item.value] : []) ?? [];
  const shown = new Set(read?.items.flatMap((item) => item.kind === "dataSource" ? [item.value.id] : []) ?? []);
  const linkedOnly = blocks.some((block) => DATABASE_BLOCKS.has(block.type)) && !blocks.some((block) => holdsOwnSource(block, shown)) && !read?.hasMore;
  return linkedOnly || confirmDatabaseTrash();
}

/**
 * The sources of its own a full-page database's page holds, which go to the
 * trash with it: none where it only links others' sources; one where it
 * can't be read, so that it asks.
 */
async function databasePageOwnSources(session: Session, pageId: string) {
  const read = await api.contentGraph(session, pageId).catch(() => null);
  const blocks = read?.items.flatMap((item) => item.kind === "block" ? [item.value] : []) ?? [];
  if (!read || read.hasMore || !blocks.some((block) => DATABASE_BLOCKS.has(block.type))) return 1;
  const shown = new Set(read.items.flatMap((item) => item.kind === "dataSource" ? [item.value.id] : []));
  return new Set(blocks.flatMap((block) => DATABASE_BLOCKS.has(block.type) ? databaseBlockMembers(block).own.filter((id) => shown.has(id)) : [])).size;
}

/**
 * Moving several full-page databases to the trash at once asks once, as
 * Notion's library does (seen 2026-11-16 at 1440, two ways): that N data
 * sources and all their pages go to the trash, for their sources,
 * a database's own question where they hold one between them, and nothing
 * where they only link others'.
 */
export async function confirmDatabasePagesTrash(session: Session, pageIds: readonly string[]) {
  const sources = (await Promise.all(pageIds.map((id) => databasePageOwnSources(session, id)))).reduce((sum, count) => sum + count, 0);
  if (sources === 0) return true;
  if (sources === 1) return confirmDatabaseTrash();
  return confirmAction({
    message: i18next.t("rowMenu:trashDatabase.title"), detail: i18next.t("rowMenu:trashDatabase.detailSources", { count: sources }),
    confirmLabel: i18next.t("rowMenu:actions.moveToTrash"), danger: "filled",
  });
}

/**
 * A task database with sprints then asks whether they go with it, as Notion's
 * (seen 2026-11-04): "{이름}만 삭제", "{이름} 스프린트 삭제" or 취소. The answer
 * is the pages to send too; null when cancelled.
 */
export async function confirmDatabaseSprintsTrash(session: Session, pageId: string, title: string): Promise<string[] | null> {
  const read = await api.contentGraph(session, pageId).catch(() => null);
  const sources = read?.items.flatMap((item) => item.kind === "dataSource" ? [item.value] : []) ?? [];
  for (const source of sources) {
    const sprints = await readDatabaseSprints(session, pageId, source.id).catch(() => null);
    if (!sprints?.enabled) continue;
    const name = title || untitledPageDisplayTitle({ kind: "database", viewBlock: false });
    const answer = await confirmActions({
      message: i18next.t("rowMenu:trashDatabase.sprintsTitle", { name }), detail: i18next.t("rowMenu:trashDatabase.sprintsDetail", { name }),
      actions: [
        { value: "alone", label: i18next.t("rowMenu:trashDatabase.sprintsAlone", { name }), danger: true },
        { value: "both", label: i18next.t("rowMenu:trashDatabase.sprintsBoth", { name }) },
      ],
    });
    if (!answer) return null;
    return answer === "both" ? [sprints.sprintsPageId, sprints.boardPageId].filter(Boolean) : [];
  }
  return [];
}

