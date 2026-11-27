import type { ReactNode } from "react";
import {
  ArrowBendUpRightIcon, ArrowDown, ArrowsLeftRight, ArrowUpRightIcon, BracesIcon, CaptionIcon, CodeCopyIcon, CodeWrapIcon, CommentIcon, Copy, RenameInPlaceIcon, DotsHorizontal, DownloadCircleIcon,
  FileText, GridIcon, ImageReplaceIcon, LinkIcon, MergeCsvIcon, LinkPlusIcon, LockIcon, NewWindowIcon, OpenAsPage, OpenInNew, PaintRollerIcon, PresentIcon, SmileIcon, SuggestEditIcon, SyncIcon,
  TableFitWidthIcon, Trash, TurnIntoIcon, ZoomInIcon, ArchiveIcon, SidePeekIcon, Star, ImageAlignIcon, QuoteSizeIcon,
} from "@/icons/leafen";
import { LightningIcon } from "@/icons/leafen/databaseIcons";

// Each row's mark, as Notion's block menu draws it (seen 2026-10-07): the loop
// to turn into, a roller for color, link, duplicate, ↱ to move, the bin, a
// speech bubble to comment; an image's: a loop to replace, duplicate to copy
// it, an arrow in a circle to download, its caption, a chain with a plus, ….
export const ACTION_ICONS: Record<string, ReactNode> = {
  turn: <TurnIntoIcon size="regular" />,
  color: <PaintRollerIcon size="regular" />,
  "copy-link": <LinkIcon size="regular" />,
  "copy-links": <LinkIcon size="regular" />,
  duplicate: <Copy size="regular" />,
  "duplicate-empty": <Copy size="regular" />,
  move: <ArrowBendUpRightIcon size="regular" />,
  delete: <Trash size="regular" />,
  comment: <CommentIcon size="regular" />,
  suggest: <SuggestEditIcon size="regular" />,
  present: <PresentIcon size="regular" />,
  "turn-database": <TurnIntoIcon size="regular" />,
  "to-table": <TurnIntoIcon size="regular" />,
  "table-database": <TurnIntoIcon size="regular" />,
  "fit-width": <TableFitWidthIcon size="regular" />,
  synced: <SyncIcon size="regular" />,
  "unsync-all": <SyncIcon size="regular" />,
  download: <DownloadCircleIcon size="regular" />,
  "copy-image": <Copy size="regular" />,
  caption: <CaptionIcon size="regular" />,
  "copy-source": <LinkIcon size="regular" />,
  imageLink: <LinkPlusIcon size="regular" />,
  more: <DotsHorizontal size="regular" />,
  openWith: <GridIcon size="regular" />,
  "all-documents": <FileText size="regular" />,
  replace: <ImageReplaceIcon size="regular" />,
  "view-original": <ArrowUpRightIcon size="regular" />,
  "full-screen": <ZoomInIcon size="regular" />,
  rename: <RenameInPlaceIcon size="regular" />,
  "edit-icon": <SmileIcon size="regular" />,
  lock: <LockIcon size="regular" />,
  "open-page": <OpenAsPage size="regular" />,
  "open-tab": <OpenInNew size="regular" />, "open-window": <NewWindowIcon size="regular" />,
  // A page or database block's favorite, side peek and archive rows, drawn as the sidebar menu draws them.
  favorite: <Star size="regular" />,
  "open-side": <SidePeekIcon size="regular" />,
  archive: <ArchiveIcon size="regular" />,
  "turn-mention": <TurnIntoIcon size="regular" />,
  // A code block's rows, as Notion's (seen 2026-11-06): "</>" in a square, ↵, { }, a bolt.
  "copy-code": <CodeCopyIcon size="regular" />,
  "code-wrap": <CodeWrapIcon size="regular" />,
  language: <BracesIcon size="regular" />,
  "code-format": <LightningIcon size="regular" />,
  // A file block's "맞춤": a box at the left between two lines, whatever its alignment (Notion's, seen 2026-11-06).
  align: <ImageAlignIcon align="left" size="regular" />,
  "merge-csv": <MergeCsvIcon size="regular" />,
  // "텍스트 방향", arrows both ways as its "자동 감지" (Notion's in Arabic and Hebrew, seen 2026-11-19), in every language.
  direction: <ArrowsLeftRight size="regular" />,
  // The phone sheet's "아래에 삽입", an arrow down (Notion's, seen 2026-11-11).
  "insert-below": <ArrowDown size="regular" />,
  // A quote's "인용 크기", which takes a list's "목록 형식" place; a list draws its own (Notion's, seen 2026-11-11).
  listFormat: <QuoteSizeIcon size="regular" />,
};
