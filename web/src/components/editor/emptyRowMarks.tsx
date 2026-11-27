import type { ReactNode } from "react";
import {
  BookmarkBlockIcon, FileUploadBlockIcon, GlobeBlockIcon, ImageBlockIcon, PdfRowIcon, TexMarkIcon, VideoBlockIcon, WaveformBlockIcon,
} from "@/icons/leafen";

export type EmptyRowMarkKind = "image" | "video" | "audio" | "file" | "pdf" | "bookmark" | "embed" | "equation";

/**
 * The mark in an empty block's row (BlockEmptyPopover's 25px box), one place
 * for every empty block. Notion draws there the block's own 20px mark a
 * quarter larger, its lines with it (seen 2026-11-20 in Chrome at 1440, dpr 1:
 * the photo 19.13×14.75 from 2.94, 5.13 of the box, its "/" menu photo
 * 15.25×11.75 from 2.38, 4.13 at 20px; the play, bookmark, waveform, page with
 * an arrow up, PDF, globe and TeX marks the same way). So each is a mark on
 * the whole 20-unit box, at Notion's measure, drawn at its 20px size here and
 * at 25px by the row's box.
 */
export function emptyRowMark(kind: EmptyRowMarkKind): ReactNode {
  switch (kind) {
    case "image": return <ImageBlockIcon size="regular" />;
    case "video": return <VideoBlockIcon size="regular" />;
    case "audio": return <WaveformBlockIcon size="regular" />;
    case "file": return <FileUploadBlockIcon size="regular" />;
    case "pdf": return <PdfRowIcon size="regular" />;
    case "bookmark": return <BookmarkBlockIcon size="regular" />;
    case "embed": return <GlobeBlockIcon size="regular" />;
    case "equation": return <TexMarkIcon size="regular" />;
  }
}
