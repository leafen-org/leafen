import { useContext, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { DragHandleIcon, Plus } from "@/icons/leafen";
import { optionKeyName } from "@/lib/keyboardShortcuts";
import { AddHereTip, HandleTip, IconTipButton } from "../IconTipButton";
import { commentOnOwnBlock } from "./blockOwnComment";
import type { GraphBlock } from "@/contentGraph";
import { copyText } from "@/lib/copyText";
import { absolutePageUrl } from "@/lib/navigation";
import { canPresentPage, requestPresentation } from "@/lib/pageSlides";
import { useOptionalProductToast } from "@/lib/productToast";
import { BlockActionMenu } from "./BlockActionMenu";
import { BlockMenuFooter } from "./BlockMenuFooter";
import { blockMenuCountedTexts, blockMenuOwnTexts } from "./blockHandleModel";
import { blockAnchor } from "@/lib/blockAddress";
import { childPageBlockPageId } from "./useBlockPageMenu";
import { useHandlesDrawn } from "./drawnHandles";
import { useEditorBlockCommand, type EditorBlockMenuAnchor } from "./editorCommands";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import { inlineStartX } from "@/lib/readingDirection";
import { NativeTiptapReactContext } from "./nativeTiptapReactContext";
import { suggestedRemovalBlocks } from "./suggestedRemoval";
import { suggestedNextBlock } from "./suggestingInputModel";
import { useSuggestingMode } from "./suggestingMode";

/**
 * A block's handle on a page the reader can't change, as Notion's on a
 * locked page (seen 2026-10-31): "⋮⋮" alone, no "+" and no drag, named
 * "클릭하여 메뉴를 여세요" with no drag line in its tip (HandleTip) — while
 * suggesting, a text block's "+" before it, which suggests a block of its
 * own after it and opens the "/" menu there (Notion's, measured 2026-11-18
 * for the owner and a guest with comment access); a press
 * selects the block and opens a short menu — the block's kind, its link,
 * comment and present from here. Comment is there for whoever may comment
 * (a view-only reader's has none). Whoever may suggest edits — while
 * suggesting, or with comment access — also has "편집 내용 제안 ⌘⇧⌥X",
 * which turns suggesting on or off, the block kept chosen; while
 * suggesting, a text block's menu has "삭제" after its link, which suggests
 * the block away. Who last edited the block shows at its foot only for
 * someone who may edit the page (suggesting, or under its lock), and its
 * search only while they suggest: a commenter's and a viewer's menu has
 * neither (Notion's, measured 2026-11-18 in Chrome at 1440, a guest given
 * comment and then view access, in two tabs once the page had taken the
 * access; the owner suggesting and on the locked page. Straight after the
 * access changed, Notion's menu was the editor's with 전환 ›, 색 › and a
 * greyed 옮기기 until the page was read again — not the commenter's menu).
 */
export function ReadOnlyBlockHandle({ adapter, block, row, onSelect }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  /** Its block's row, which says whether it draws its button (drawnHandles). */
  row: RefObject<HTMLElement | null>;
  onSelect?: () => void;
}) {
  const { t } = useTranslation("blockHandle");
  const toast = useOptionalProductToast();
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<EditorBlockMenuAnchor>();
  const button = useRef<HTMLButtonElement>(null);
  const suggesting = useSuggestingMode(), controller = useContext(NativeTiptapReactContext)?.controller;
  const removable = suggesting.active && controller && suggestedRemovalBlocks([block]).length > 0;
  const addable = suggesting.active && controller && suggestedNextBlock(block.type, true) !== null;
  const commentable = Boolean(adapter.session) && (adapter.database.canComment ?? !adapter.readOnly);
  // Someone who may edit the page, read only only while suggesting or under its lock.
  const editor = adapter.database.writableWhileSuggesting === true || adapter.database.lockableUnderPageLock === true;

  // The menu closes, as Notion's (seen 2026-11-06).
  async function copyBlockLink() {
    setOpen(false);
    const url = new URL(absolutePageUrl(adapter.pageId));
    url.hash = blockAnchor(adapter.pageId, block.id);
    const ok = await copyText(url.toString());
    const message = ok ? t("menu.copiedBlockLink") : t("notifications.couldNotCopyLink");
    if (toast) toast.notify(message, ok ? "success" : "error");
    else adapter.moveContext?.onStatus?.(message, ok ? "success" : "error");
  }

  // A "/" row picked while suggesting acts on its line's block here, the
  // page's own handle not being drawn then (suggestedSlashRows.ts).
  useEditorBlockCommand(block.id, (command) => {
    if (command === "copyLink") void copyBlockLink();
    else if (command === "present" && canPresentPage(adapter.pageId)) requestPresentation({ pageId: adapter.pageId, blockId: block.id });
  }, () => button.current ?? row.current);
  const drawn = useHandlesDrawn(row);
  if (!drawn && !open) return null;
  return (
    <div className={styles.gutter} contentEditable={false} data-menu-open={open ? "true" : undefined} data-read-only-handle data-resting={open ? undefined : ""}
      data-adds={addable ? "" : undefined}>
      {addable ? <IconTipButton tabIndex={-1} className={styles.gutterBtn} label={t("buttons.addBlockBelow", { key: optionKeyName() })}
        tip={<AddHereTip />} placement="bottom" wait="handle" onClick={() => controller?.suggestLineBelow(block.id)}><Plus size="regular" aria-hidden="true" /></IconTipButton> : null}
      <IconTipButton ref={button} tabIndex={-1} className={styles.gutterBtn} label={t("buttons.clickHint")} tip={<HandleTip drag={false} />} wait="handle"
        placement="bottom" aria-haspopup="menu" aria-expanded={open} onClick={() => {
          onSelect?.();
          // The menu stands beside its handle, as an editable page's does
          // (Notion's locked page, seen 2026-11-01).
          const rect = button.current?.getBoundingClientRect();
          setAnchor(rect ? { x: inlineStartX(rect, -10, button.current), y: rect.bottom + 4,
            handle: { top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right } } : undefined);
          setOpen((value) => !value);
        }}>
        <DragHandleIcon size="regular" aria-hidden="true" /></IconTipButton>
      {open ? (
        <BlockActionMenu readOnly searchable={editor && suggesting.active} anchor={anchor} returnFocusRef={button} activeColor="" activeType={block.type} selectionCount={1}
          footer={editor ? <BlockMenuFooter session={adapter.session ?? null} pageId={adapter.pageId} blockIds={[block.id]}
            texts={blockMenuCountedTexts(adapter.graph, [block.id], (item) => blockMenuOwnTexts(adapter, item,
              (page) => adapter.moveContext?.pages.find((entry) => entry.id === childPageBlockPageId(page))))} /> : undefined}
          onClose={() => setOpen(false)}
          onCopyLink={() => void copyBlockLink()}
          onSuggestRemoval={removable ? () => { setOpen(false); controller?.suggestRemoval([block.id]); } : undefined}
          onComment={commentable ? () => { setOpen(false); commentOnOwnBlock(adapter, block); } : undefined}
          onSuggest={suggesting.onChange ? () => { setOpen(false); suggesting.onChange?.(!suggesting.active); } : undefined}
          onPresent={canPresentPage(adapter.pageId) ? () => { setOpen(false); requestPresentation({ pageId: adapter.pageId, blockId: block.id }); } : undefined}
          onColor={() => {}} onDelete={() => {}} onDuplicate={() => {}} onMove={() => {}} toggleCaption={() => {}} turnInto={() => {}} />
      ) : null}
    </div>
  );
}
