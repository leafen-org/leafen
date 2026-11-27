import { useEffect, useMemo, useRef, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import { ArrowUpRightIcon, CaptionIcon, CommentSmallIcon, CropIcon, DotsHorizontal, ExpandDiagonalIcon, ImageAlignIcon, ImportIcon, ZoomInIcon } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import { useBlockCommentCount } from "./blockCommentCounts";
import { openComments } from "@/components/comments/commentPanelEvents";
import { useOpenCommentCard } from "@/components/comments/openCommentCard";
import { openBlockToolMenu, requestEditorBlockCommand } from "./editorCommands";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { focusBlockControlSettled } from "./focus";
import { mediaColumnRatio, type MediaToolsMode, type MediaToolsRoom, useMediaToolsFold } from "./mediaToolsFold";
import styles from "./editorStyles.generated.module.css";

const ALIGNMENTS = ["left", "center", "right"] as const;

/**
 * An image's own tools on hover, as Notion's (seen 2026-10-07): a small white
 * group at its top right with a new comment, the alignment (left, center or
 * right below it), the caption, full screen, download and the block's menu,
 * each named in Notion's short dark tip above it ("댓글", "정렬", "캡션", "자르기",
 * "펼치기", "다운로드하기", "원본 보기", "더 보기"; the alignments none, seen 2026-11-06).
 * Where its block is narrow they fold to the menu alone (useMediaToolsFold).
 */
export function ImageBlockTools({ adapter, block, align, mode = "image", canAlign = true, foldRoom }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  align: (typeof ALIGNMENTS)[number];
  /**
   * "web" is a link's embed and "embed" an uploaded file's: no full screen or
   * download, its original instead (Notion's, seen 2026-10-31; an uploaded
   * text or other file's, 2026-11-12: comment, alignment, caption, "원본
   * 미디어 소스 보기" and the menu).
   */
  mode?: MediaToolsMode;
  /** A picture as wide as its column has nothing to align, as Notion's. */
  canAlign?: boolean;
  /** Where a picture's room that folds its tools is read: its block's, or its frame's (useMediaToolsFold). */
  foldRoom?: MediaToolsRoom;
}) {
  const [aligning, setAligning] = useState(false);
  const groupRef = useRef<HTMLDivElement>(null);
  const ratio = useMemo(() => mediaColumnRatio(adapter.graph, block), [adapter.graph, block]);
  const folded = useMediaToolsFold(groupRef, mode, block, ratio, foldRoom);
  // A picture's open discussions are counted on its comment tool, not beside
  // the block, and the tool then opens them (Notion's, seen 2026-11-09).
  const discussions = useBlockCommentCount(block.id);
  const counted = mode === "image" && discussions > 0;
  // Counted, it opens the picture's card keeping the focus, as a line's mark (BlockFrame).
  const cardOpen = Boolean(useOpenCommentCard(block.id)) && counted;
  const original = mode === "video" || mode === "web" || mode === "embed";

  // The alignment choices close on Escape or a press outside the tools; folded tools have none.
  const choosing = aligning && !folded;
  useEffect(() => {
    if (!choosing) return undefined;
    const outside = (event: PointerEvent) => {
      if (!groupRef.current?.contains(event.target as Node)) setAligning(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.stopPropagation(); setAligning(false); }
    };
    document.addEventListener("pointerdown", outside, true);
    document.addEventListener("keydown", escape, true);
    return () => {
      document.removeEventListener("pointerdown", outside, true);
      document.removeEventListener("keydown", escape, true);
    };
  }, [choosing]);

  function toggleCaption() {
    adapter.updateBlock(block, { captionVisible: true });
    focusBlockControlSettled(block.id, '[data-block-control="block-caption"]');
  }

  return (
    <div ref={groupRef} className={styles.imageActions} contentEditable={false} data-image-tools={mode}
      data-open={choosing ? "true" : undefined} data-folded={folded ? "true" : undefined}>
      {folded ? null : <>
      {adapter.session ? (
        <button type="button" className={styles.imageToolComment} aria-label={blockItemText("image.tools.comment")}
          data-icon-tip={blockItemText("image.tips.comment")} data-tip-side="top" data-image-tool="comment"
          data-count={counted ? discussions : undefined} aria-haspopup={counted ? "dialog" : undefined} aria-expanded={counted ? cardOpen : undefined}
          onClick={(event) => counted ? openComments({ pageId: adapter.pageId, blockId: block.id })
            : requestEditorBlockCommand(block.id, "comment", undefined, event.currentTarget)}>
          <CommentSmallIcon lines={counted} />
          {counted ? <span>{discussions}</span> : null}
        </button>
      ) : null}
      {canAlign ? <span className={styles.imageAlignWrap}>
        <button type="button" aria-label={blockItemText("image.tools.align")} data-icon-tip={blockItemText("image.tips.align")} data-tip-side="top"
          aria-expanded={aligning} data-image-tool="align" onClick={() => setAligning((open) => !open)}>
          <ImageAlignIcon align={align} size="small" aria-hidden="true" />
        </button>
        {aligning ? (
          <span className={styles.imageAlignMenu} role="group" aria-label={blockItemText("image.tools.align")} data-image-align-menu>
            {ALIGNMENTS.map((value) => (
              <button key={value} type="button" aria-pressed={align === value} data-image-align={value}
                aria-label={blockItemText(`image.tools.align_${value}`)}
                onClick={() => { adapter.updateBlock(block, { align: value }); setAligning(false); }}>
                <ImageAlignIcon align={value} size="small" aria-hidden="true" />
              </button>
            ))}
          </span>
        ) : null}
      </span> : null}
      <button type="button" aria-label={blockItemText("image.tools.caption")} data-icon-tip={blockItemText("image.tips.caption")} data-tip-side="top"
        data-image-tool="caption" onClick={toggleCaption}>
        <CaptionIcon size="small" aria-hidden="true" />
      </button>
      {mode === "image" && block.content.fileId ? <button type="button" aria-label={blockItemText("image.tools.crop")}
        data-icon-tip={blockItemText("image.tips.crop")} data-tip-side="top" data-image-tool="crop"
        onClick={event => requestEditorBlockCommand(block.id, "imageCrop", undefined, event.currentTarget)}>
        <CropIcon size="small" aria-hidden="true" />
      </button> : null}
      {!original ? <button type="button" aria-label={blockItemText("image.tools.fullScreen")} data-icon-tip={blockItemText("image.tips.fullScreen")} data-tip-side="top"
        data-image-tool="full-screen" onClick={(event) => requestEditorBlockCommand(block.id, "imageFullScreen", undefined, event.currentTarget)}>
        {/* A picture's is a magnifier, a PDF's two arrows out (Notion's, seen 2026-10-31). */}
        {mode === "pdf" ? <ExpandDiagonalIcon size="small" aria-hidden="true" /> : <ZoomInIcon size="small" aria-hidden="true" />}
      </button> : null}
      {!original ? <button type="button" aria-label={blockItemText("image.tools.download")} data-icon-tip={blockItemText("image.tips.download")} data-tip-side="top"
        data-image-tool="download" onClick={(event) => requestEditorBlockCommand(block.id, "imageDownload", undefined, event.currentTarget)}>
        <ImportIcon size="small" aria-hidden="true" />
      </button> : null}
      {original ? <button type="button" aria-label={blockItemText("media.viewOriginal")}
        data-icon-tip={blockItemText("image.tips.original")} data-tip-side="top" data-image-tool="original"
        onClick={event => requestEditorBlockCommand(block.id, "mediaOriginal", undefined, event.currentTarget)}>
        <ArrowUpRightIcon size="small" aria-hidden="true" />
      </button> : null}
      </>}
      <button type="button" aria-label={blockItemText("image.tools.menu")} data-icon-tip={blockItemText("image.tips.menu")} data-tip-side="top"
        data-image-tool="menu" onClick={(event) => openBlockToolMenu(block.id, event)}>
        <DotsHorizontal size="small" aria-hidden="true" />
      </button>
    </div>
  );
}

/**
 * A bookmark's way to its block menu on a touch screen, as Notion's iPad (seen
 * 2026-11-13 in iPad Safari 834 wide): a "⋯" in the media tools' white group
 * 4px inside the card's top right corner, there without a pointer. A
 * desktop's pointer has the block's handle and no "⋯" over the card (Notion's,
 * seen the same day), so it shows on a touch screen alone
 * (EditorBookmarkBlocks.module.css).
 */
export function BookmarkBlockTools({ block }: { block: GraphBlock }) {
  return (
    <div className={styles.imageActions} contentEditable={false} data-image-tools="bookmark">
      <button type="button" aria-label={blockItemText("image.tools.menu")} data-image-tool="menu"
        onClick={(event) => openBlockToolMenu(block.id, event)}>
        <DotsHorizontal size="small" aria-hidden="true" />
      </button>
    </div>
  );
}
