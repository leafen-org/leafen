import { Suspense, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { lazyScreen } from "@/lib/lazyScreen";
import { useWorkspaceFileUrl } from "@/lib/fileUrls";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { CreditedPicture } from "@/components/page/CoverCredit";
import { RichTextCellText } from "@/components/database/RichTextCellText";
import { calloutIconType } from "@/components/editor/blockIconModel";
import { closedMentionLabel, isMissingMentionText, titleWithClosedMentions } from "@/lib/closedMentions";
import { inlineEquation, richTextSummary } from "@/lib/richTextSummary";
import type { TextSpan } from "@/lib/types";
import { type PagePreviewBlock, workspaceSearchBounds } from "@/workspaceSearchWire.generated";
import styles from "./SearchPagePreview.module.css";

// Without its code a line of code stays plain.
const CodeHighlight = lazyScreen(() => import("@/components/editor/CodeHighlight").then(({ CodeHighlight: View }) => ({ default: View })),
  { standIn: ({ code }) => <code>{code}</code> });

// A block Notion's preview draws as a grey bar with its kind's mark and name.
const BARS: Record<string, { mark: string; label: string }> = {
  equation: { mark: "📈", label: "equation" }, pdf: { mark: "📄", label: "pdf" }, file: { mark: "📎", label: "file" },
  audio: { mark: "🎵", label: "audio" }, video: { mark: "🎥", label: "video" },
  bookmark: { mark: "🔗", label: "bookmark" }, link_preview: { mark: "🔗", label: "bookmark" }, embed: { mark: "🔗", label: "embed" },
};

// Kinds neither search's preview nor a page's card draws, as Notion's (seen
// 2026-10-31); a synced block shows its own blocks instead.
const LEFT_OUT = new Set(["breadcrumb", "table_of_contents", "divider", "button", "synced_block"]);

const GRID_ROWS = 6;

/** A block as a preview draws it: the server's (page_preview_blocks.go), or a public page's line with its own spans. */
export type PreviewLineBlock = Omit<PagePreviewBlock, "spans"> & { spans?: readonly TextSpan[] };

// A line keeps a line's worth of characters and "…" after, as its plain
// text is cut (page_preview_blocks.go).
const LINE_CHARACTERS = workspaceSearchBounds.previewLineRunes + 1;

/**
 * A page's body as search's preview draws it, as Notion's (seen 2026-10-31):
 * its blocks small — 13px lines, headings 17, 15 and 14px — a list's items
 * marked, a to-do's box, a toggle closed, a callout or code on its tint,
 * files, sound and sums as grey bars, a table or a database as an empty grid
 * under its kind's name. A page's card leaves the bars out, draws a table's
 * first cells and a database as a picture of a table.
 */
export function SearchPagePreview({ blocks, workspaceId, variant }: {
  blocks: readonly PreviewLineBlock[];
  workspaceId: string;
  /** "card": smaller, as a page's card draws it (PageMentionPreview.module.css). */
  variant?: "card";
}) {
  const numbers = listNumbers(blocks);
  return (
    <div className={styles.blocks} data-search-page-preview data-variant={variant}>
      {blocks.map((block, index) => (
        <PreviewBlock key={index} block={block} number={numbers[index]} workspaceId={workspaceId} card={variant === "card"} />
      ))}
    </div>
  );
}

function PreviewBlock({ block, number, workspaceId, card }: { block: PreviewLineBlock; number: number; workspaceId: string; card: boolean }) {
  const { t } = useTranslation("searchDialog");
  const indent = block.depth ? { marginInlineStart: `${block.depth * 18}px` } : undefined;
  // Its mentions, marks and equations as in the page, as Notion's preview
  // draws them (seen 2026-11-01, 2026-11-13), cut as a short line
  // (lib/richTextSummary.ts); a mention of a page the reader can't open as
  // Notion's page draws it, "사용 권한 없음" (seen 2026-11-09); one nothing
  // answers to keeps the marks its drawing reads "찾을 수 없음" by.
  const spans = block.spans && richTextSummary(block.spans.map((span) => span.unavailable && !isMissingMentionText(span.text)
    ? { ...span, text: closedMentionLabel() } : span), LINE_CHARACTERS);
  const text = spans?.length ? <PreviewText spans={spans} card={card} /> : block.text;
  const line = (className: string, body: ReactNode = text) => (
    <div className={className} data-preview-block={block.type} style={indent}>{body}</div>
  );
  if (LEFT_OUT.has(block.type)) return null;
  const bar = BARS[block.type];
  if (bar) {
    return card ? null : line(styles.bar, <><span aria-hidden="true">{bar.mark}</span><span>{t(`preview.${bar.label}`)}</span></>);
  }
  switch (block.type) {
    case "heading_1": case "toggle_heading_1": return line(`${styles.heading} ${styles.heading1}`);
    case "heading_2": case "toggle_heading_2": return line(`${styles.heading} ${styles.heading2}`);
    case "heading_3": case "toggle_heading_3": case "heading_4": case "toggle_heading_4":
      return line(`${styles.heading} ${styles.heading3}`);
    case "bulleted_list_item": case "numbered_list_item":
      return line(styles.listItem, <><span className={styles.marker} aria-hidden="true">
        {block.type === "numbered_list_item" ? `${number}.` : ["•", "◦", "▪"][block.depth % 3]}</span><span>{text}</span></>);
    case "to_do":
      return line(styles.todo, <><span className={styles.box} data-checked={block.checked === true} aria-hidden="true" /><span className={styles.todoText} data-checked={block.checked === true}>{text}</span></>);
    case "toggle":
      return line(styles.toggle, <><span className={styles.caret} aria-hidden="true">
        <svg viewBox="0 0 10 10" width="10" height="10"><path d="M3 1.5 7.5 5 3 8.5Z" fill="currentColor" /></svg>
      </span><span>{text}</span></>);
    case "quote": return line(styles.quote);
    case "callout":
      return line(styles.callout, <><span className={styles.calloutIcon} aria-hidden="true">
        {block.icon ? <PageIconGlyph page={{ icon: block.icon, iconType: calloutIconType(block.iconType), workspaceId }}
          size={16} fallback="💡" /> : "💡"}</span><span>{text}</span></>);
    case "code":
      return line(styles.code, <Suspense fallback={<code>{block.text}</code>}>
        <CodeHighlight code={block.text} spans={[{ text: block.text }]} language={block.language ?? ""} />
      </Suspense>);
    case "image": return <PagePreviewImage block={block} className={styles.image} />;
    case "table": return card ? <PreviewTable cells={block.cells ?? []} widths={block.widths ?? []} /> : <PreviewGrid name={t("preview.table")} columns={3} />;
    case "inline_database": case "child_database": return card ? <DatabasePicture /> : <PreviewGrid name={t("preview.database")} columns={4} />;
    // A sub-page or a page link is drawn on a page's card, its title naming a
    // page closed to the reader as a plain title does; search's preview
    // leaves both out, as Notion's (seen 2026-11-14: the parent's lines and
    // its databases' bars, no sub-page or link line; its card draws them).
    case "child_page": case "link_to_page": return card ? line(styles.pageLink, titleWithClosedMentions(block.text)) : null;
    // A sub-page that is a full-page database or a wiki is a grey bar there,
    // "🗂️ Database" (seen 2026-11-14), and a sub-page's line on a card.
    case "database_page": return card ? line(styles.pageLink, titleWithClosedMentions(block.text))
      : line(styles.bar, <><span aria-hidden="true">🗂️</span><span>{t("preview.databasePage")}</span></>);
    case "paragraph": return line(styles.text);
    default: return block.text ? line(styles.text) : null;
  }
}

/**
 * A line's formatted text, as the page draws it; in search's preview an
 * inline equation is a chip naming it, "Σ Equation", as Notion's (seen
 * 2026-11-13), where a page's card draws it.
 */
function PreviewText({ spans, card }: { spans: readonly TextSpan[]; card: boolean }) {
  const { t } = useTranslation("searchDialog");
  if (card || !spans.some(inlineEquation)) return <RichTextCellText spans={spans} linksAsText />;
  const parts: ReactNode[] = [];
  let run: TextSpan[] = [];
  const flush = () => {
    if (run.length) parts.push(<RichTextCellText key={parts.length} spans={run} linksAsText />);
    run = [];
  };
  for (const span of spans) {
    if (!inlineEquation(span)) { run.push(span); continue; }
    flush();
    parts.push(<span key={parts.length} className={styles.equation} data-preview-equation>
      <span aria-hidden="true">Σ</span><span>{t("preview.inlineEquation")}</span></span>);
  }
  flush();
  return <>{parts}</>;
}

/**
 * A picture block's image, or its place while it has none; a page's card shows its page's first one. A photo from
 * Unsplash stands in a box with its credit (CreditedPicture), `frameClassName` that box's room.
 */
export function PagePreviewImage({ block, className, frameClassName }: { block: PreviewLineBlock; className: string; frameClassName?: string }) {
  const stored = useWorkspaceFileUrl(block.fileId ? nativeWorkspaceFileLocator(block.fileId) : undefined, [], { enabled: Boolean(block.fileId), width: 1024 });
  const src = block.fileId ? stored : block.url && /^https?:/iu.test(block.url) ? block.url : "";
  return src ? <CreditedPicture cover={block.url} fill className={frameClassName}><img className={className} src={src} alt="" data-preview-block="image" /></CreditedPicture>
    : <div className={`${className} ${styles.imagePlace}`} data-preview-block="image" />;
}

// A table or a database in search's preview: its kind's name over an empty
// grid — a table's three even columns, a database's wide one and three.
function PreviewGrid({ name, columns }: { name: string; columns: 3 | 4 }) {
  return (
    <div className={styles.grid} data-preview-block="grid">
      <div className={styles.gridName}>{name}</div>
      <div className={styles.gridCells} data-columns={columns} aria-hidden="true">
        {Array.from({ length: GRID_ROWS * columns }, (_, cell) => <span key={cell} />)}
      </div>
    </div>
  );
}

// A table in a page's card: its first rows' cells, ruled as its page draws
// them, each column as wide as there (120px unless set).
function PreviewTable({ cells, widths }: { cells: readonly (readonly string[])[]; widths: readonly number[] }) {
  const columns = Math.max(0, ...cells.map((row) => row.length));
  if (!columns) return null;
  return (
    <div data-preview-block="table">
      <div className={styles.table} style={{ gridTemplateColumns: Array.from({ length: columns }, (_, column) => `${widths[column] || 120}px`).join(" ") }}>
        {cells.flatMap((row, rowIndex) => Array.from({ length: columns }, (_, column) => <span key={`${rowIndex}:${column}`}>{row[column] ?? ""}</span>))}
      </div>
    </div>
  );
}

// A database in a page's card: a picture of a table, as Notion's — a button
// at the top right over five ruled rows, each a dot and a bar.
function DatabasePicture() {
  return (
    <svg className={styles.databasePicture} viewBox="0 0 512 256" aria-hidden="true" data-preview-picture="database">
      <rect className={styles.pictureNew} x="404" y="48" width="80" height="28" rx="3" />
      <rect className={styles.pictureNewMark} x="420" y="58" width="48" height="8" rx="4" />
      {[182, 204, 182, 146, 102].map((bar, row) => (
        <g key={row}>
          <line className={styles.pictureRule} x1="0" x2="512" y1={96 + row * 32} y2={96 + row * 32} />
          <circle className={styles.pictureDot} cx="28" cy={112 + row * 32} r="5.6" />
          <rect className={styles.pictureBar} x="42" y={108 + row * 32} width={bar} height="8" rx="4" />
        </g>
      ))}
      {[274, 374, 474].map((x) => <line key={x} className={styles.pictureRule} x1={x} x2={x} y1="96" y2="256" />)}
    </svg>
  );
}

// Each numbered item's number: its place in the run of numbered items at its depth.
function listNumbers(blocks: readonly PreviewLineBlock[]) {
  const counts: number[] = [];
  return blocks.map((block) => {
    counts.length = block.depth + 1;
    if (block.type !== "numbered_list_item") { counts[block.depth] = 0; return 0; }
    counts[block.depth] = (counts[block.depth] ?? 0) + 1;
    return counts[block.depth];
  });
}
