import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// The blocks' marks in the "/" and "+" menus, the block menu's "전환" and the
// selection bar's turn menu, drawn where Notion's own put their ink on its
// 20-unit box at a menu row's 20px (read 2026-11-27 at 1440 in dark, dpr 1,
// its "/" menu's every block row): lines 1.25 wide, a framed mark's frame
// 13.75 across (3.125–16.875), a wide frame 15.2 by 11.8, a page 11.75 by
// 15.25 with its top corner folded. Leafen's own lines, not Notion's paths.
// A mark drawn on the 16-unit box (IconSvg's default view) would come out a
// quarter larger in the same row, as the callout's boxed T and the columns
// did (2026-11-27).

/** A page's sheet on the 20-unit box: the page glyph's own (PageSheet) at 0.7625 of it, centred. */
const SHEET = "M6.26 3H10.73L15.25 7.52V15.49A1.51 1.51 0 0 1 13.74 17H6.26A1.51 1.51 0 0 1 4.75 15.49V4.51A1.51 1.51 0 0 1 6.26 3Z";
const SHEET_FOLD = "M10.49 3V6.49A1.22 1.22 0 0 0 11.71 7.71H15.25";

/** A frame as Notion's callout, equation and tabs marks have it. */
const FRAME = <rect x="3.75" y="3.75" width="12.5" height="12.5" rx="1.5" />;

/** Text: a serifed T. */
export const TextIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text" line={1.3}>
    <path d="M5.55 6.6V4.85h8.9V6.6M10 4.85v10.3M8.55 15.15h2.9" />
  </IconSvg>
);

/** A divider in the menus: one line 12 long. */
export const DividerBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="divider-block" line={1.3}>
    <path d="M4 10.05h12" />
  </IconSvg>
);

/** A callout: a frame round a serifed T. */
export const CalloutBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="callout-block" line={1.25}>
    {FRAME}
    <path d="M6.47 7.01V6.38h6.31v.63M9.62 6.38v7.24M8.6 13.62h2.05" />
  </IconSvg>
);

/** An equation block: a frame round a Σ. */
export const MathBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="math-block" line={1.25}>
    {FRAME}
    <path d="M12.88 6.23H7.35L11 10l-3.65 3.77h5.53" />
  </IconSvg>
);

/** Tabs: a frame whose head holds three tabs, the first open into it. */
export const TabsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="tabs" line={1.25}>
    {FRAME}
    <path d="M8.55 3.75V5.7a.6.6 0 0 0 .6.6h7.1M12.25 3.75V6.3" />
  </IconSvg>
);

/** Columns: a wide frame split into as many parts as it has columns, 1.2 between them. */
export function ColumnsSplitIcon({ count = 2, ...p }: LeafenIconProps & { count?: number }) {
  const parts = Math.min(Math.max(count, 2), 5);
  const cell = (12.8 - 1.2 * (parts - 1)) / parts;
  const dividers = Array.from({ length: parts - 1 }, (_, index) => 3.6 + (index + 1) * cell + index * 1.2 + 0.6);
  return (
    <IconSvg {...p} viewBox="0 0 20 20" glyph={`columns-${parts}`} line={1.25}>
      <rect x="3" y="4.75" width="14" height="10.5" rx="1.5" />
      {dividers.map((x) => <path key={x} d={`M${x.toFixed(2)} 4.75v10.5`} />)}
    </IconSvg>
  );
}

/** A button: a wide frame with an arrow inside it pointing right. */
export const ButtonBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="button-block" line={1.25}>
    <rect x="3" y="4.82" width="14" height="10.46" rx="1.5" />
    <path d="M6.73 9.95H13M10.7 7.45l2.5 2.5-2.5 2.5" />
  </IconSvg>
);

/** A table of contents: three dashes and lines, each a step further in. */
export const TableOfContentsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="table-of-contents" line={1.25}>
    <path d="M1.13 5.25h1.5M5.2 5.25h10.1M2.9 10.05h1.5M7 10.05h10.1M4.6 14.85h1.5M8.7 14.85h10.1" />
  </IconSvg>
);

/** The list view in the "/" menu: three lines, a dash before each. */
export const ListDashIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="list-dash" line={1.25}>
    <path d="M2.9 5.05h1.5M7 5.05h10.1M2.9 10.05h1.5M7 10.05h10.1M2.9 15.05h1.5M7 15.05h10.1" />
  </IconSvg>
);

/** A form in the "/" menu: two questions, each a line over its answer's field, the second shorter. */
export const FormFieldsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="form-fields" line={1.25}>
    <path d="M3.8 3.55h9.5M3.8 11.35h9.5" />
    <rect x="3.78" y="6.1" width="12.42" height="2.6" rx=".65" />
    <rect x="3.75" y="13.8" width="8" height="2.6" rx=".65" />
  </IconSvg>
);

/** "데이터 소스 보기 연결": a table of two columns whose lower right gives way to an arrow out. */
export const DataSourceLinkIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="data-source-link" line={1.25}>
    <path d="M17 9.2V6.2a1.5 1.5 0 0 0-1.5-1.5h-11A1.5 1.5 0 0 0 3 6.2v7.65a1.5 1.5 0 0 0 1.5 1.5h6.9M10 4.7v10.65M3 8.3h14M3 11.8h8.9" />
    <path strokeWidth="1.3" d="M15.7 11.75h2.85v4.45M18.45 11.85l-6.4 6.1" />
  </IconSvg>
);

/** A link to a page: the page sheet, open at its lower right where an arrow leaves it. A page itself is the page glyph on this box (FileText `artboard`). */
export const PageLinkIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="page-link" line={1.25}>
    <path d="M7.7 17H6.26a1.51 1.51 0 0 1-1.51-1.51V4.51A1.51 1.51 0 0 1 6.26 3h4.47l4.52 4.52V8.9" />
    <path strokeWidth="1.1" d={SHEET_FOLD} />
    <path strokeWidth="1.3" d="M11.65 11.65h4.4v4.4M16.05 11.65l-6.15 6.2" />
  </IconSvg>
);

/** A breadcrumb: the page sheet with a # on it. */
export const BreadcrumbIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="breadcrumb" line={1.25}>
    <path d={SHEET} />
    <path strokeWidth="1.1" d={`${SHEET_FOLD}M7.6 11.05h5.2M7.2 13.05h5.25M9.45 9.6l-1.05 4.95M11.65 9.6l-1.05 4.95`} />
  </IconSvg>
);

/** A Mermaid code block: a fish's tail, two flukes over a short stem. */
export const MermaidIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="mermaid" line={1.25}>
    <path d="M10 8.05C9 6.5 7.5 5.45 5.65 5.1l-3-.55.5 2.95c.4 1.9 1.7 3.35 3.55 3.8.85.25 1.45 1.05 1.45 1.9v3.3h3.7v-3.3c0-.85.6-1.65 1.45-1.9 1.85-.45 3.15-1.9 3.55-3.8l.5-2.95-3 .55C12.5 5.45 11 6.5 10 8.05Z" />
  </IconSvg>
);

/** An audio block: a speaker with two sound arcs. */
export const AudioBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="audio-block" line={1.25}>
    <path d="M2.7 7.15h3.15l3.55-3.6a.55.55 0 0 1 .95.4v12.4a.55.55 0 0 1-.95.4l-3.55-3.6H2.7Z" />
    <path d="M12.85 7.1a4.6 4.6 0 0 1 0 5.8M15.7 5.25a8.7 8.7 0 0 1 0 9.5" />
  </IconSvg>
);

/** A file block: a paper clip. */
export const PaperclipBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="file-block" line={1.25}>
    <path d="M16 10.8l-4.7 4.7a4.24 4.24 0 0 1-6-6l5.35-5.35a2.83 2.83 0 0 1 4 4l-5.4 5.4a1.41 1.41 0 0 1-2-2l2.7-2.7" />
  </IconSvg>
);

/** "CSV" under "가져오기": the page sheet giving way at its lower right to the letters. */
export const CsvFileIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="csv-file" line={1.25}>
    <path d="M15.25 9.25V7.52L10.73 3H6.26a1.51 1.51 0 0 0-1.51 1.51v10.98A1.51 1.51 0 0 0 6.26 17h7.48a1.5 1.5 0 0 0 1.2-.6" />
    <path strokeWidth="1.1" d={SHEET_FOLD} />
    <path strokeWidth="1.15" d="M10.35 11.55a2 2 0 1 0 0 2.65M14.25 11.55c-.35-.5-.85-.65-1.35-.65-.8 0-1.3.45-1.3 1 0 1.2 2.7.65 2.7 1.95 0 .6-.55 1-1.35 1-.6 0-1.1-.25-1.4-.7M15.3 10.9l1.6 3.95 1.6-3.95" />
  </IconSvg>
);

/** "ZIP" under "가져오기": the page with a zipper down it, its pull at the head. */
export const ZipFileIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="zip-file" line={1.25}>
    <path d="M8.6 3H6.26a1.51 1.51 0 0 0-1.51 1.51v11a1.51 1.51 0 0 0 1.51 1.51h7.48a1.51 1.51 0 0 0 1.51-1.51v-11A1.51 1.51 0 0 0 13.74 3H11.4" />
    <path fill="currentColor" stroke="none" fillRule="evenodd"
      d="M10 8.45a2.3 2.3 0 0 1-2.22-2.9l.78-2.85a1.5 1.5 0 0 1 2.88 0l.78 2.85A2.3 2.3 0 0 1 10 8.45Zm0-1.3a.95.95 0 1 0 0-1.9.95.95 0 0 0 0 1.9Z" />
    <path strokeWidth="1" d="M10 8.4v8.3M8.1 10H10M10 11h1.9M8.1 12H10M10 13h1.9M8.1 14H10M10 15h1.9" />
  </IconSvg>
);
