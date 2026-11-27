import type { TFunction } from "i18next";
import type { GraphBlock } from "@/contentGraph";
import { blockAnchor } from "@/lib/blockAddress";
import { copyText } from "@/lib/copyText";
import { absolutePageUrl } from "@/lib/navigation";
import type { useOptionalProductToast } from "@/lib/productToast";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { confirmBlockRemoval, removeBlocksAnnounced, removedDatabasePages } from "./databaseBlockRemoval";
import { blockMenuSeveral } from "./blockMenuSeveral";
import { duplicateChosenBlocks } from "./editorBlockDuplicate";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { NativeTiptapReactValue } from "./nativeTiptapReactContext";

type Tone = "default" | "success" | "error";

/** The block menu's copy, duplicate and delete actions on its chosen blocks, and what several of them show (BlockHandle.tsx). */
export function blockMenuActions({ adapter, block, selectedIds, close, notify, toast, editorContext, t }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  selectedIds: string[];
  /** Closes the block menu. */
  close: () => void;
  notify: (message: string, tone?: Tone) => void;
  toast: ReturnType<typeof useOptionalProductToast>;
  editorContext: NativeTiptapReactValue | null;
  t: TFunction<"blockHandle">;
}) {
  // A link that opens this page scrolled to the block, as Notion's block links
  // do. The menu closes and the block stays chosen, as Notion's (seen 2026-11-06).
  async function copyBlockLink() {
    close();
    const url = new URL(absolutePageUrl(adapter.pageId));
    url.hash = blockAnchor(adapter.pageId, block.id);
    const ok = await copyText(url.toString());
    notify(ok ? t("menu.copiedBlockLink") : t("notifications.couldNotCopyLink"), ok ? "success" : "error");
  }

  // Each selected block's link, a blank line between them, as Notion's
  // "모든 링크 복사" (seen 2026-10-01), which closes the menu.
  async function copyBlockLinks() {
    close();
    const links = selectedRootsInOrder(adapter.graph, new Set(selectedIds)).map((selected) => {
      const url = new URL(absolutePageUrl(adapter.pageId));
      url.hash = blockAnchor(adapter.pageId, selected.id);
      return url.toString();
    });
    const ok = await copyText(links.join("\n\n"));
    notify(ok ? t("notifications.copiedLinks") : t("notifications.couldNotCopyLink"), ok ? "success" : "error");
  }

  // Duplicating chooses the copies, one step of the page's history (editorBlockDuplicate.ts).
  function duplicate(withoutContent = false) {
    close();
    duplicateChosenBlocks(adapter, selectedIds, editorContext?.setSelection ?? (() => {}),
      editorContext?.controller, withoutContent ? { withoutContent: true } : undefined);
  }

  // No notice shows, as Notion's (seen 2026-11-06): the blocks are gone and ⌘Z brings them back;
  // a screen reader hears how many went (seen 2026-11-08). A sub-page among them, gone,
  // says it went to the trash with "복원하기" (removeBlocksAnnounced); a full-page
  // database's line asks first (Notion's, seen 2026-11-07).
  // `first` moves the caret within the same step, before the blocks go.
  async function deleteBlocks(first?: () => void) {
    const ids = selectedIds;
    close();
    const context = adapter.moveContext;
    const databases = removedDatabasePages(adapter.graph, ids, context?.pages ?? []);
    if (!await confirmBlockRemoval(adapter.graph, ids, { session: adapter.session, databases })) return false;
    const controller = editorContext?.controller;
    removeBlocksAnnounced(adapter, ids, (step) => { if (controller) controller.run(() => { first?.(); step(); }); else step(); }, toast);
    return true;
  }

  /** Several chosen blocks' head and the rows they all take, as the first of them leads (blockMenuSeveral). Asked as the menu opens. */
  const several = () => blockMenuSeveral(adapter, selectedIds, close);

  return { copyBlockLink, copyBlockLinks, duplicate, deleteBlocks, several };
}
