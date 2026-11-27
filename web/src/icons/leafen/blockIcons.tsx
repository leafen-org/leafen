import { useId } from "react";
import {
  ArrowsMerge as PhosphorArrowsMerge,
  FileCsv as PhosphorFileCsv,
  FilePdf as PhosphorFilePdf,
  FileZip as PhosphorFileZip,
  SquareSplitHorizontal as PhosphorSquareSplitHorizontal,
  Tabs as PhosphorTabs,
  TextAlignJustify as PhosphorTextAlignJustify,
} from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";
import { ServiceIcon } from "./serviceIcons";

export * from "./mediaBlockMarks";

// The blocks' marks in the "/" and "+" menus and "전환", drawn after the
// shapes Notion uses there (seen 2026-10-07): each names what the block
// looks like, as a toggle list's triangles beside lines or a callout's
// boxed T. Leafen draws them itself on its 20-unit grid.

// Notion's feed, map and dashboard views, drawn here from its measures
// (seen 2026-11-03): a newspaper with a folded flap at its left, lines and a
// box on it; a map folded in three; two wide and two narrow boxes crosswise.
export const FeedIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="feed">
    <path d="M6.9 10V5.8a.8.8 0 0 1 .8-.8h6.6a.8.8 0 0 1 .8.8v8a1.2 1.2 0 0 1-1.2 1.2H5.7a.7.7 0 0 1-.7-.7V10h1.9v4.3a.7.7 0 0 1-.7.7" />
    <path d="M8.9 7h4.2" />
    <path d="M8.9 9h4.2" />
    <rect x="8.9" y="11" width="4.2" height="1.6" rx=".2" />
  </IconSvg>
);

export const MapIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="map">
    <path d="M4.4 6.6 8.13 5l3.74 1.6L15.6 5v8.4l-3.73 1.6-3.74-1.6L4.4 15z" />
    <path d="M8.13 5v8.4" />
    <path d="M11.87 6.6V15" />
  </IconSvg>
);

export const DashboardIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="dashboard">
    <rect x="4.4" y="5.8" width="5.6" height="3.2" rx=".8" />
    <rect x="12" y="5.8" width="3.6" height="3.2" rx=".8" />
    <rect x="4.4" y="11" width="3.6" height="3.2" rx=".8" />
    <rect x="10" y="11" width="5.6" height="3.2" rx=".8" />
  </IconSvg>
);

export const TabsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTabs} />
);

export const PdfFileIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFilePdf} />
);

// A PDF's file row, as Notion's (seen 2026-11-07): a page whose lower right
// side gives way to the letters "PDF", drawn here in Leafen's own lines.
export const PdfRowIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="pdf-row">
    <path strokeWidth="1.25" d="M15.25 9.25V7.68L10.58 3H6.25a1.5 1.5 0 0 0-1.5 1.5v11a1.5 1.5 0 0 0 1.5 1.5h7.5a1.5 1.5 0 0 0 1.2-.6" />
    <path strokeWidth="1.25" d="M10.58 3.4v2.78a1.5 1.5 0 0 0 1.5 1.5h2.77" />
    <path strokeWidth="1.15" d="M7.02 14.83V11.21h1.7c.66 0 1.05.46 1.05 1.03s-.39 1.04-1.05 1.04h-1.7M11.52 14.85V11.23h1.33c1.1 0 1.94.66 1.94 1.8s-.84 1.82-1.94 1.82h-1.33M18.98 11.21h-2.37v3.62M16.61 13h2.32" />
  </IconSvg>
);

// A spreadsheet's file row: a table with a round "···" mark at its lower
// right, as Notion's xlsx row (seen 2026-11-07; its pptx and docx rows have
// no mark). The table stops short of the mark.
export const SheetRowIcon = (p: LeafenIconProps) => {
  const cut = `sheet-mark-${useId().replace(/:/g, "")}`;
  // Notion's spreadsheet row (seen 2026-11-07): a two-column, three-row table
  // whose lower right corner gives way to a round "···" badge.
  return (
    <IconSvg {...p} viewBox="0 0 20 20" glyph="sheet-row">
      <defs>
        <mask id={`${cut}-table`}>
          <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
          <circle cx="16" cy="16" r="4.6" fill="#000" stroke="none" />
        </mask>
        <mask id={`${cut}-dots`}>
          <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
          {[14.1, 16, 17.9].map((x) => <circle key={x} cx={x} cy="16" r=".75" fill="#000" stroke="none" />)}
        </mask>
      </defs>
      <g mask={`url(#${cut}-table)`} strokeWidth="1.25">
        <rect x="3" y="4.75" width="14" height="10.5" rx="1.5" />
        <path d="M3 8.25h14M3 11.75h14M10 4.75v10.5" />
      </g>
      <circle cx="16" cy="16" r="3.5" fill="currentColor" stroke="none" mask={`url(#${cut}-dots)`} />
    </IconSvg>
  );
};

export const SlidesRowIcon = (p: LeafenIconProps) => {
  const cut = `slides-mark-${useId().replace(/:/g, "")}`;
  // Notion's slides row (seen 2026-11-07): a card tilted behind a level one.
  return (
    <IconSvg {...p} viewBox="0 0 20 20" glyph="slides-row">
      <defs>
        <mask id={cut}>
          <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
          <rect x="4.66" y="6.68" width="14.25" height="10.37" rx="2.13" fill="#000" stroke="none" />
        </mask>
      </defs>
      <g strokeWidth="1.25">
        <g mask={`url(#${cut})`}><rect x="2.29" y="4.11" width="13" height="9.5" rx="1.5" transform="rotate(-7.59 8.79 8.86)" /></g>
        <rect x="5.29" y="7.3" width="13" height="9.5" rx="1.5" />
      </g>
    </IconSvg>
  );
};

export const CsvFileIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFileCsv} />
);

export const ZipFileIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFileZip} />
);

export const MarkdownTextIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTextAlignJustify} />
);

export const WordDocumentIcon = (p: LeafenIconProps) => (
  <ServiceIcon {...p} service="word" name="Word" />
);

export const CalloutBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="callout-block">
    <rect x="3.8" y="3.8" width="12.4" height="12.4" rx="2.2" />
    <path d="M7.4 7.4h5.2" />
    <path d="M10 7.4v5.4" />
  </IconSvg>
);

export const PageLinkIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="page-link">
    <path d="M12.6 8V6.6L10 3.8H5.6a1 1 0 0 0-1 1v10.4a1 1 0 0 0 1 1h3" />
    <path d="M10 3.8v2.8h2.6" />
    <path d="M11.2 16.4 16.4 11.2" />
    <path d="M12.6 11.2h3.8V15" />
  </IconSvg>
);

// Notion's form (seen 2026-11-03, drawn here from its measures): a page with
// two lines, its right side open where a slanted pencil crosses it.
export const FormBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="form-block">
    <path d="M14.2 7.16V5.6A1.2 1.2 0 0 0 13 4.4H7A1.2 1.2 0 0 0 5.8 5.6v8.8A1.2 1.2 0 0 0 7 15.6h6a1.2 1.2 0 0 0 1.2-1.2v-2.82" />
    <path d="M8 6.87h4" />
    <path d="M8 8.87h2.22" />
    <path d="M15.68 7.88 10.56 13" />
    <path d="M16.72 6.84l-.32.32" />
  </IconSvg>
);

// Notion's lined page (docPlainText, seen 2026-11-16 on a new page's
// "시작하기" 폼 chip, 20px — not the form's pencil page, FormBlockIcon): a tall
// rounded page with three lines, the last shorter, drawn on Notion's whole
// 20-unit box, its page in a 1.25 line and its lines in 1.1.
export const DocPlainTextIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="doc-plain-text" viewBox="0 0 20 20" line={1.25}>
    <rect x="4.75" y="3" width="10.5" height="14" rx="1.5" />
    <path d="M7.25 5.75h5.5M7.25 8.25h5.5M7.25 10.75H10" strokeWidth={1.1} />
  </IconSvg>
);

// Notion's mark for a submitted answer (seen 2026-11-15 on a phone, 56px, as
// on a computer): a page with two lines, open at its lower right where a
// check crosses it, drawn on Notion's whole 20-unit box with its 1.25 line.
export const FormSubmittedIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="form-submitted" viewBox="0 0 20 20" line={1.25}>
    <path d="M15.25 10.9V4.5a1.5 1.5 0 0 0-1.5-1.5h-7.5a1.5 1.5 0 0 0-1.5 1.5v11a1.5 1.5 0 0 0 1.5 1.5h2.7" />
    <path d="M7.5 6.1h5" />
    <path d="M7.5 8.6h2.7" />
    <path d="M10.4 15.2l2.6 2.5 4.5-6.2" />
  </IconSvg>
);

// A table standing in a frame's four corners, as Notion's "데이터베이스 -
// 인라인" (seen 2026-11-07, its 20-unit art read on this 16-unit box): two
// columns of three cells, taller than wide.
export const InlineDatabaseIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="inline-database">
    <path d="M4.64 6.96V5.92a1.28 1.28 0 0 1 1.28-1.28h1.04" />
    <path d="M13.04 4.64h1.04a1.28 1.28 0 0 1 1.28 1.28v1.04" />
    <path d="M15.36 13.04v1.04a1.28 1.28 0 0 1-1.28 1.28h-1.04" />
    <path d="M6.96 15.36H5.92a1.28 1.28 0 0 1-1.28-1.28v-1.04" />
    <rect x="7.64" y="6.28" width="4.72" height="7.36" rx="1" />
    <path d="M10 6.28v7.36M7.64 8.73h4.72M7.64 11.19h4.72" />
  </IconSvg>
);

export const LinkedViewIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="linked-view">
    <rect x="3.6" y="3.6" width="9.6" height="9.6" rx="1.4" />
    <path d="M3.6 7.2h9.6" />
    <path d="M7.2 3.6v9.6" />
    <path d="M11.6 16.4 16.4 11.6" />
    <path d="M13.2 11.6h3.2v3.2" />
  </IconSvg>
);

export const TableOfContentsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="table-of-contents">
    <path d="M4 5.4h12" />
    <path d="M6.8 10h9.2" />
    <path d="M9.6 14.6H16" />
  </IconSvg>
);

export const MathBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="math-block">
    <rect x="3.6" y="3.6" width="12.8" height="12.8" rx="2.2" />
    <path d="M12.6 6.8H7.6l2.8 3.2-2.8 3.2h5" />
  </IconSvg>
);

// An empty math block's row, as Notion's (seen 2026-11-20): the TeX logo in
// solid serif letters — a T, an E dropped half its height, an X — drawn
// here in Leafen's own outlines at the places Notion's letters stand in the
// 20-unit box (stems 0.8–0.9 wide, hairlines about 0.3), not the "/" menu's Σ.
export const TexMarkIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="tex-mark">
    <g fill="currentColor" stroke="none">
      <path d="M2.4 5.45 2.75 5.3l.45.25h4.25l.6-.17-.05.57-.12.9h-.28l-.15-.85h-1.9v4.9l.75.2.2.15v.17H3.85v-.17l.25-.15.65-.2V6h-1.8l-.33.9H2.4Z" />
      <path d="M6.8 8.9h3.9l-.1 1.1h-.2l-.35-.7-.2-.08H8.4v2.33h1.1l.4-.15.15-.45h.25v1.65h-.25l-.15-.5-.4-.15H8.4v2.43h1.85L11 13.5h.3l-.4 1.2H6.6v-.2l.6-.2.35-.3V9.5l-.25-.2-.5-.18Z" />
      <path d="M12.37 5.92h1l3.42 5.16h-1Z" />
      <path d="M16.27 5.92h.5l-4.05 5.16h-.5Z" />
      <path d="M11.6 5.6h2.35v.32H11.6ZM15.4 5.6h2.25v.32H15.4ZM11.3 11.08h2.35v.34H11.3ZM15.05 11.08h2.55v.34h-2.55Z" />
    </g>
  </IconSvg>
);

// The embed block's mark in the "/" menu, as Notion's (seen 2026-11-20): a
// capture frame's three rounded corners with a chain link across the fourth,
// drawn here in Leafen's own 1.25 lines at the places Notion's stand in the
// 20-unit box.
export const LinkCaptureIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="link-capture">
    <path strokeWidth="1.25" d="M3.75 7.25v-1.5a2 2 0 0 1 2-2h1.5M12.75 3.75h1.5a2 2 0 0 1 2 2v1.5M3.75 12.75v1.5a2 2 0 0 0 2 2h1.5" />
    <path strokeWidth="1.25" d="M15.78 14.09 16.88 12.99A2 2 0 0 0 14.05 10.16L12.95 11.26M11.26 12.95 10.16 14.05A2 2 0 0 0 12.99 16.88L14.09 15.78M12.28 14.76 14.76 12.28" />
  </IconSvg>
);

export const ButtonBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="button-block">
    <path d="M11.6 6V5.6a1.2 1.2 0 0 0-1.2-1.2H4.8a1.2 1.2 0 0 0-1.2 1.2v8.8a1.2 1.2 0 0 0 1.2 1.2h5.6a1.2 1.2 0 0 0 1.2-1.2V14" />
    <path d="M7.6 10h8.6" />
    <path d="m13.8 7.6 2.4 2.4-2.4 2.4" />
  </IconSvg>
);

export const BreadcrumbIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="breadcrumb">
    <path d="M14.6 7.8v7.4a1 1 0 0 1-1 1H6.4a1 1 0 0 1-1-1V4.8a1 1 0 0 1 1-1h4.6Z" />
    <path d="M8.6 9.2 8 14" />
    <path d="M11.6 9.2 11 14" />
    <path d="M7.4 10.8h5.2" />
    <path d="M7 12.6h5.2" />
  </IconSvg>
);

/** Columns: a box split into as many parts as it has columns. */
export function ColumnsSplitIcon({ count = 2, ...p }: LeafenIconProps & { count?: number }) {
  const parts = Math.min(Math.max(count, 2), 5);
  const dividers = Array.from({ length: parts - 1 }, (_, index) => 3.6 + (12.8 * (index + 1)) / parts);
  return (
    <IconSvg {...p} glyph={`columns-${parts}`}>
      <rect x="3.6" y="4.4" width="12.8" height="11.2" rx="1.8" />
      {dividers.map((x) => <path key={x} d={`M${x.toFixed(2)} 4.4v11.2`} />)}
    </IconSvg>
  );
}

export const MermaidIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="mermaid">
    <path d="M10 16V10.4" />
    <path d="M10 10.4C8.8 8 6.4 6.6 3.8 6.8c.9 2.6 3.4 4.1 6.2 3.6Z" />
    <path d="M10 10.4c1.2-2.4 3.6-3.8 6.2-3.6-.9 2.6-3.4 4.1-6.2 3.6Z" />
  </IconSvg>
);

export const SquareRootIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="square-root">
    <path d="M3.6 11h2.2l2.2 4.6L11.4 4.4h5" />
  </IconSvg>
);

/** A chart's "누적": bars rising one after another (Notion's, seen 2026-11-06). */
export const RisingBarsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="chart-bars-rising">
    <rect x="4.4" y="11.6" width="2.4" height="4" rx="0.6" />
    <rect x="8.8" y="8.4" width="2.4" height="7.2" rx="0.6" />
    <rect x="13.2" y="4.4" width="2.4" height="11.2" rx="0.6" />
  </IconSvg>
);

// A simple table's header row and header column switches, and its "너비에
// 맞추기", drawn after where Notion's own put their ink (read 2026-11-16 at
// 1440 in dark, on their 20 box at 10 times their size; overlap 0.98 and
// more), not from Notion's paths: a table of two columns and three rows with
// its first row or column filled, and two arrows pointing apart. Lines 1.25.
const TABLE_FRAME = <rect x="3" y="4.75" width="14" height="10.45" rx="1.4" />;

export const TableHeaderRowIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="table-header-row" strokeWidth="1.25">
    {TABLE_FRAME}
    <path d="M3 8.2V6.15a1.4 1.4 0 0 1 1.4-1.4h11.2a1.4 1.4 0 0 1 1.4 1.4V8.2Z" fill="currentColor" />
    <path d="M3 11.75H17M10 8.2V15.2" />
  </IconSvg>
);

export const TableHeaderColumnIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="table-header-column" strokeWidth="1.25">
    {TABLE_FRAME}
    <path d="M7.6 4.75H4.4A1.4 1.4 0 0 0 3 6.15v7.65a1.4 1.4 0 0 0 1.4 1.4h3.2Z" fill="currentColor" />
    <path d="M7.6 8.25H17M7.6 11.75H17" />
  </IconSvg>
);

export const TableFitWidthIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="table-fit-width" strokeWidth="1.25">
    <path d="M1.4 10H8.6M5 6.4 1.4 10 5 13.6M18.6 10H11.4M15 6.4 18.6 10 15 13.6" />
  </IconSvg>
);

/**
 * The "전환" mark, a block's "전환" and a simple table's "데이터베이스로
 * 전환" in Notion's menus (seen 2026-11-16, the same glyph in both), also
 * drawn for a sidebar view section's source row: a line running right and
 * down into an arrow, and one rising from an arrow and running right along
 * the bottom. Drawn after where Notion's puts its ink (overlap 0.93), not
 * from its path.
 */
export const TurnIntoIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="turn-into" strokeWidth="1.25">
    <path d="M6.4 3.75h8.5a1.4 1.4 0 0 1 1.4 1.4v8.05M13.8 10.7l2.5 2.5 2.5-2.5M3.7 6.9v7.9a1.4 1.4 0 0 0 1.4 1.4h8.3M1.2 9.4l2.5-2.5 2.5 2.5" />
  </IconSvg>
);

/** Two arrows joining into one upward, as a table cell menu's "셀 병합" shows (Notion's, seen
 * 2026-11-10): Phosphor's meet downward, so it is turned over. */
export const TableMergeCellsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} style={{ ...p.style, transform: "scaleY(-1)" }} icon={PhosphorArrowsMerge} glyph="arrows-merge" />
);

/** A box split down its middle, as a table cell menu's "셀 병합 해제" shows (Notion's, seen 2026-11-10). */
export const TableUnmergeCellsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSquareSplitHorizontal} />
);

/** A speech bubble with a plus inside, its tail at the lower start, as a block line's "💬+" shows (Notion's, seen 2026-11-12). */
export const CommentAddIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="comment-add">
    <path d="M5.4 4.2h9.2a1.8 1.8 0 0 1 1.8 1.8v6.4a1.8 1.8 0 0 1-1.8 1.8H10l-3.3 2.6v-2.6H5.4a1.8 1.8 0 0 1-1.8-1.8V6a1.8 1.8 0 0 1 1.8-1.8z" />
    <path d="M10 6.8v4.4" />
    <path d="M7.8 9h4.4" />
  </IconSvg>
);
