import { type CSSProperties, Fragment, type MouseEvent, type ReactNode, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AudioIcon, ChevronUp, FileText, FileUploadIcon, FilterIcon, ExpandDiagonalIcon, ImageIcon, LightningIcon, PdfFileIcon, Search,
  SlidersIcon, SortIcon, VideoIcon,
} from "@/icons/leafen";
import { useProductPages } from "@/components/product/productPagesContext";
import type { PageHistoryRowMark, PageHistoryTextPart } from "@/lib/pageHistoryChanges";
import { pageHistoryRowLayout } from "@/lib/pageHistoryRowLayout";
import {
  pageHistoryPreviewCallouts,
  pageHistoryPreviewRowGroups,
  pageHistoryPreviewRows,
  pageHistoryPreviewRowsWithRemoved,
  type PageHistoryPreviewItem,
} from "@/lib/pageHistoryRows";
import type { PageHistoryBlock, PageHistoryDatabase, PageHistorySnapshotResponse } from "@/pageHistoryWire.generated";
import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";
import { calloutGlyphSize, storedBlockIcon } from "./editor/blockIconModel";
import { blockColorStyle } from "./editor/blockHandleModel";
import { fileKindIcon, fileRowKindOf } from "./editor/fileKindIcon";
import { PageIconGlyph } from "./page/PageIcon";
import { PersonAvatar } from "./people/PersonAvatar";
import { PageHistoryDatabaseTable } from "./PageHistoryDatabaseTable";
import { PageHistoryDatabasePage } from "./PageHistoryDatabasePage";
import { historyMediaShown, PageHistoryMedia } from "./PageHistoryMedia";
import mediaStyles from "./PageHistoryMedia.module.css";
import { PageHistoryProperties, type PageHistoryProperty } from "./PageHistoryProperties";
import { PageHistoryWikiProperties } from "./PageHistoryWikiProperties";
import type { PageHistoryWikiBar } from "./usePageHistoryProperties";
import { pageHistoryPreviewColors, pageHistoryRowColorProps } from "./pageHistoryPreviewColors";
import { RichTextCellText } from "./database/RichTextCellText";
import {
  blockPreviewSpans, blockPreviewText, FILE_BLOCK_TYPES, fileBlockSize, previewSpansBetween, tableRowPreviewCells,
} from "./pageHistoryPreviewText";
import styles from "./PageHistoryPreview.module.css";
import changeMarks from "./PageHistoryChangeMarks.module.css";
import fontStyles from "./PageHistoryPreviewFont.module.css";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";
import { pageMapOf } from "@/lib/pageIndex";

// A file block reads as its row on the page: the block's icon, the file's
// name and its size, as Notion's preview shows it (seen 2026-10-18).
const FILE_BLOCK_ICONS = { file: FileText, image: ImageIcon, pdf: PdfFileIcon, video: VideoIcon, audio: AudioIcon } as const;

// Text blocks Notion's preview leaves as an empty line when they hold no
// text (seen 2026-10-01); other blocks with nothing to show say so.
const TEXT_BLOCK_TYPES = new Set([
  "paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item", "numbered_list_item", "to_do",
  "toggle", "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4", "quote", "callout", "code",
]);

// A callout's own words stand at its box's start with their own spacing, not
// the place its box takes among the rows (placeOf), which they would inherit.
const OWN_LINE = {
  "--history-indent": "0px", "--history-top-cut": "0px", "--history-bottom-cut": "0px", "--history-over": "0px", "--history-under": "0px",
} as CSSProperties;

// A toggle's caret, a toggle heading's too, turned down while it is open, as
// Notion's preview draws it as the page shows it (seen 2026-11-12).
const TOGGLE_TYPES = new Set(["toggle", "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4"]);

/**
 * A version of a page as it read then: its icon, title, properties and body,
 * with what it changed marked in its author's color when given.
 */
export function PageHistoryPreview({
  preview, title, properties, wiki, databases, loading, onLoadMore, changes, rowChanges, renamedProperties, changedDatabases, changeColor,
  removedFrom, titleParts, changedValues, fullPageDatabase, currentDatabases, changeAuthor, returnedBlocks,
}: {
  preview: PageHistorySnapshotResponse | null;
  title: string;
  properties?: readonly PageHistoryProperty[];
  /** A wiki page's bar, whose properties stand side by side under its title as its page shows them. */
  wiki?: PageHistoryWikiBar;
  databases: readonly PageHistoryDatabase[];
  loading: boolean;
  onLoadMore?: () => void;
  changes?: ReadonlyMap<string, readonly PageHistoryTextPart[]>;
  /** The blocks a restore brought back, tinted whole (pageHistoryComparison). */
  returnedBlocks?: ReadonlySet<string>;
  rowChanges?: ReadonlyMap<string, readonly PageHistoryRowMark[]>;
  /** Each database's properties the version renamed, by its block. */
  renamedProperties?: ReadonlyMap<string, ReadonlySet<string>>;
  /** The databases whose own properties, views or rows' order the version changed, by their blocks. */
  changedDatabases?: ReadonlySet<string>;
  changeColor?: string;
  /** The entry before's blocks, whose blocks this version deleted show where they stood. */
  removedFrom?: readonly PageHistoryBlock[];
  /** The title's words the version added and took out, when it changed the title: outlined and marked as a block's. */
  titleParts?: readonly PageHistoryTextPart[];
  /** A database page's properties whose values the version changed, each outlined as a change. */
  changedValues?: ReadonlySet<string>;
  /** Whether the page is a full-page database, whose own database shows as its body. */
  fullPageDatabase?: boolean;
  /** The databases as they are now, which a restore keeps: each inline one shows so, saying it, and the version's others are left out. */
  currentDatabases?: readonly PageHistoryDatabase[];
  /** Who made the version's changes and when, said over a change the pointer is on. */
  changeAuthor?: { userId: string; name: string; avatar?: string; bot?: boolean; time: string };
}) {
  const { t } = useTranslation(["updatesPanel", "common", "wikiView", "blockItem"]);
  const articleRef = useRef<HTMLElement>(null);
  const [authorAt, setAuthorAt] = useState<{ top: number; left: number } | null>(null);
  // Over a change the pointer is on, who made it and when, as Notion's
  // preview (measured 2026-11-07): 4px over the block's first changed words
  // at their start, else 5px over the changed row; over a block a restore
  // brought back, 4px over its tint at its start (measured 2026-11-16, where
  // it covers the line above, as Notion's); nothing elsewhere.
  const pointAuthor = (event: MouseEvent) => {
    const article = articleRef.current;
    const change = (event.target as Element | null)?.closest?.('[data-history-changed="true"], tr[data-history-row-change]');
    if (!article || !change || !changeAuthor) {
      setAuthorAt(null);
      return;
    }
    const words = change.querySelector("[data-history-change]");
    const frame = article.getBoundingClientRect();
    if (change.hasAttribute("data-history-returned")) {
      // The tint is the block's ::before (PageHistoryChangeMarks.module.css).
      const row = change.getBoundingClientRect(), tint = getComputedStyle(change, "::before");
      setAuthorAt({ top: row.top - frame.top + parseFloat(tint.top) - 28, left: row.left - frame.left + parseFloat(tint.left) });
      return;
    }
    const box = (words ?? change).getBoundingClientRect();
    setAuthorAt({ top: box.top - frame.top - (words ? 28 : 29), left: box.left - frame.left });
  };
  // A page block shows its page's icon and name, as Notion's preview does
  // (seen 2026-10-01); one made elsewhere keeps its name only on the page.
  const pagesById = pageMapOf(useProductPages()?.pages ?? []);
  // The version's rows as the editor lays them out (pageHistoryRowLayout):
  // where each stands, its marker, its spacing, and closed toggles' children
  // left out.
  const { rows, layouts } = useMemo(() => {
    // A restore keeping the databases as they are leaves away those the page
    // doesn't hold now, a linked view too, as Notion's preview shows it
    // (seen 2026-11-25 twice).
    const now = currentDatabases ? new Set(currentDatabases.map((database) => database.blockId)) : null;
    const away = new Set(now ? databases.filter((database) => !database.later && !now.has(database.blockId)).map((database) => database.blockId) : []);
    const after = (preview?.blocks ?? []).filter((block) => !away.has(block.id));
    const kept = new Set(after.map((block) => block.id));
    const blocks = [...after, ...(removedFrom ?? []).filter((block) => !kept.has(block.id))];
    return pageHistoryRowLayout(removedFrom ? pageHistoryPreviewRowsWithRemoved([...removedFrom], after) : pageHistoryPreviewRows(after), blocks);
  }, [currentDatabases, databases, preview?.blocks, removedFrom]);
  const workspaceId = pagesById.get(preview?.pageId ?? "")?.workspaceId;
  const databaseOf = useMemo(() => {
    const now = new Map((currentDatabases ?? []).filter((database) => !database.fullPage).map((database) => [database.blockId, database]));
    return new Map(databases.map((database) => [database.blockId, database.fullPage ? database : now.get(database.blockId) ?? database]));
  }, [currentDatabases, databases]);
  const keptNow = useMemo(() => new Set((currentDatabases ?? []).map((database) => database.blockId)), [currentDatabases]);
  const colors = useMemo(() => pageHistoryPreviewColors(rows, layouts, [...(removedFrom ?? []), ...(preview?.blocks ?? [])]),
    [layouts, preview?.blocks, removedFrom, rows]);
  return (
    <article ref={articleRef} className={`${styles.historyPreviewDocument} ${changeMarks.document} ${fontStyles.font}`}
      data-history-full-page-database={fullPageDatabase ? "true" : undefined}
      // The version's own font and small text, as Notion's preview reads them (PageHistoryPreviewFont.module.css).
      data-font={preview?.font && preview.font !== "default" ? preview.font : undefined}
      data-small-text={preview?.smallText ? "true" : undefined}
      onMouseOver={pointAuthor} onMouseLeave={() => setAuthorAt(null)}>
      {authorAt && changeAuthor ? (
        <span className={styles.historyChangeAuthor} style={{ top: authorAt.top, left: authorAt.left }} aria-hidden="true" data-history-change-author>
          <PersonAvatar userId={changeAuthor.userId} name={changeAuthor.name} avatar={changeAuthor.avatar} bot={changeAuthor.bot} size={16} />
          <span className={styles.historyChangeAuthorName}>{changeAuthor.name}</span>
          <span className={styles.historyChangeAuthorTime}>· {changeAuthor.time}</span>
        </span>
      ) : null}
      {/* The version's icon as a page's head shows it: 78px, 8px in, 12px over
          the title, as Notion's preview (seen 2026-10-17). */}
      {preview?.icon ? (
        <div className={styles.historyPreviewIcon} aria-hidden="true">
          <PageIconGlyph page={{ icon: preview.icon, iconType: (preview.iconType || "emoji") as Page["iconType"] }} size={78} fallback="none" />
        </div>
      ) : null}
      <h1 data-history-changed={titleParts ? "true" : undefined}
        style={titleParts && changeColor ? { "--history-change-color": changeColor } as CSSProperties : undefined}>
        {titleParts && titleParts.filter((part) => part.change !== "removed").map((part) => part.text).join("") === title.trim()
          ? <span className={styles.historyPreviewBlockText}>{changedText(titleParts, null)}</span> : title}
      </h1>
      {properties && preview?.values ? (wiki ? <PageHistoryWikiProperties wiki={wiki} properties={properties} values={preview.values} pageId={preview.pageId}
        title={preview.title} changed={changedValues} changeColor={changeColor} />
        : <PageHistoryProperties properties={properties} values={preview.values} pageId={preview.pageId} title={preview.title}
          changed={changedValues} changeColor={changeColor} />) : null}
      {/* A version with no blocks shows its title alone, as Notion's
          (seen 2026-11-06). */}
      <div className={styles.historyPreviewBlocks} data-history-preview-blocks>
        {pageHistoryPreviewRowGroups(pageHistoryPreviewCallouts(rows)).map((group) => group.removed ? (
          <RemovedContent key={`removed:${group.rows[0].block.id}`}>
            {group.rows.map((row) => blockRow(row, 0))}
          </RemovedContent>
        ) : blockRow(group.row, 0))}
        {preview?.hasMore && preview.nextCursor && onLoadMore ? (
          <button type="button" className={styles.historyLoadMore} disabled={loading} onClick={onLoadMore}>
            {t("wikiView:loadMore")}
          </button>
        ) : null}
      </div>
    </article>
  );

  /**
   * Where a row stands and how it spaces (pageHistoryRowLayout), from `base`,
   * the start of the box it is drawn in.
   */
  function placeOf(id: string, base: number) {
    const layout = layouts.get(id);
    return {
      "--history-indent": `${(layout?.indent ?? 0) - base}px`,
      "--history-top-cut": `${layout?.topCut ?? 0}px`,
      "--history-bottom-cut": `${layout?.bottomCut ?? 0}px`,
      "--history-over": `${layout?.over ?? 0}px`,
      "--history-under": `${layout?.under ?? 0}px`,
    } as CSSProperties;
  }

  function blockRow({ block, depth, removed, inside }: PageHistoryPreviewItem, base: number, boxed = false): ReactNode {
    if (block.type === "callout") return calloutRow({ block, depth, removed, inside }, base);
    if (block.type === "divider") {
      return <hr key={block.id} className={styles.historyPreviewDivider} data-history-block={block.id} style={placeOf(block.id, base)} />;
    }
    // A simple table shows as its rows, each a line of cells, under it.
    if (block.type === "table") return null;
    if (block.type === "table_row") {
      return (
        <div key={block.id} className={styles.historyPreviewTableRow} data-history-block={block.id} data-type="table_row"
          style={placeOf(block.id, base)}>
          {tableRowPreviewCells(block).map((cell, index) => <span key={index} className={styles.historyPreviewTableCell}>{cell || "\u200b"}</span>)}
        </div>
      );
    }
    if (historyMediaShown(block)) {
      const changed = !removed && changes?.has(block.id);
      return (
        <div key={block.id} className={`${styles.historyPreviewBlock} ${mediaStyles.media}`} data-history-block={block.id}
          data-type={block.type} data-history-media="true" data-history-changed={changed ? "true" : undefined}
          style={{ ...placeOf(block.id, base), ...(changed && changeColor ? { "--history-change-color": changeColor } : {}) } as CSSProperties}>
          <PageHistoryMedia block={block} />
        </div>
      );
    }
    // A button reads as its outlined chip, its icon and name, muted and not
    // to be pressed, as Notion's preview draws it (measured 2026-11-12).
    if (block.type === "button") {
      const changed = !removed && changes?.has(block.id);
      const icon = storedBlockIcon(block);
      const name = typeof block.content.label === "string" ? block.content.label.trim() : "";
      return (
        <div key={block.id} className={styles.historyPreviewBlock} data-history-block={block.id} data-type="button"
          data-history-changed={changed ? "true" : undefined}
          style={{ ...placeOf(block.id, base), ...(changed && changeColor ? { "--history-change-color": changeColor } : {}) } as CSSProperties}>
          <span className={styles.historyPreviewButton}>
            {icon ? <PageIconGlyph page={{ ...icon, workspaceId }} size={16} fallback="" /> : null}
            <span className={styles.historyPreviewButtonName}>{name || t("blockItem:button.newButton")}</span>
          </span>
        </div>
      );
    }
    const database = removed ? undefined : databaseOf.get(block.id);
    if (fullPageDatabase && database && !database.fullPage && depth === 0) {
      return (
        <PageHistoryDatabasePage key={block.id} database={database} marks={rowChanges?.get(database.blockId)}
          renamed={renamedProperties?.get(database.blockId)} changed={changedDatabases?.has(database.blockId)}
          changeColor={changeColor} workspaceId={workspaceId} />
      );
    }
    const pageBlock = block.type === "child_page" || block.type === "link_to_page";
    const marker = layouts.get(block.id)?.marker;
    // A file shows the mark the page's file row shows for its kind (fileKindIcon).
    const FileIcon = block.type === "file"
      ? fileKindIcon(fileRowKindOf(String(block.content.name ?? ""), String(block.content.contentType ?? "")), FileUploadIcon)
      : FILE_BLOCK_TYPES.has(block.type) ? FILE_BLOCK_ICONS[block.type as keyof typeof FILE_BLOCK_ICONS] : null;
    const fileSize = fileBlockSize(block);
    const page = pageBlock ? pagesById.get(String(block.content.childPageId ?? block.id)) : undefined;
    const text = database ? plainTitleOf(database) || untitledPageDisplayTitle("database")
      : blockPreviewText(block) || (pageBlock ? plainTitleOf(page) || untitledPageDisplayTitle(page) : "");
    const parts = removed ? undefined : changes?.get(block.id);
    // The text reads as on the page: its formatting, mentions and equations.
    const spans = database ? null : blockPreviewSpans(block);
    const rich = spans && text && spans.map((span) => span.text).join("").trim() === text ? spans : null;
    const { style: colorStyle, ...colorMarks } = pageHistoryRowColorProps(database ? undefined : colors.get(block.id));
    const inlineDatabase = database && !database.fullPage ? database : undefined;
    const row = (
        <div
          className={styles.historyPreviewBlock}
          {...colorMarks}
          data-history-block={block.id}
          data-type={database ? (database.fullPage ? "full_page_database" : "inline_database") : block.type}
          data-history-changed={parts && !inlineDatabase && !boxed ? "true" : undefined}
          data-history-returned={parts && !inlineDatabase && !boxed && returnedBlocks?.has(block.id) ? "true" : undefined}
          data-history-file={FileIcon ? "true" : undefined}
          data-history-page-line={(pageBlock && !database) || database?.fullPage ? "true" : undefined}
          style={{
            ...placeOf(block.id, base),
            "--history-depth": depth,
            ...colorStyle,
            ...(parts && changeColor ? { "--history-change-color": changeColor } : {}),
          } as CSSProperties}
        >
          {colors.get(block.id)?.rails.map((rail, index) => (
            <span key={`rail:${index}`} className={styles.historyPreviewQuoteRail} data-place={rail.place} aria-hidden="true"
              style={{ "--history-rail-back": `${rail.back}px`, "--history-rail-end": `${rail.end}px`,
                color: rail.color ?? "var(--text-default)" } as CSSProperties} />
          ))}
          {/* A page's line, or a full-page database's with its table, as
              Notion's preview: the icon 1px in, the name 5px past it. */}
          {(pageBlock && !database) || database?.fullPage ? (
            <span className={styles.historyPreviewPageIcon} aria-hidden="true" data-history-page-icon>
              <PageIconGlyph page={page ?? { icon: "", iconType: "none", kind: database?.fullPage ? "database" : undefined }} size={18} />
            </span>
          ) : null}
          {FileIcon ? (
            <span className={styles.historyPreviewPageIcon} aria-hidden="true" data-history-file-icon>
              <FileIcon size="regular" />
            </span>
          ) : null}
          {marker ? (
            <span className={styles.historyPreviewMarker} data-roman={marker.roman || undefined}
              style={marker.width === 24 ? undefined : { flexBasis: marker.width === "auto" ? "auto" : `${marker.width}px` }}>{marker.text}</span>
          ) : null}
          {block.type === "to_do" && (
            <span
              className={styles.historyPreviewCheckbox}
              data-checked={block.content.checked === true ? "true" : undefined}
            >
              {block.content.checked === true ? "✓" : ""}
            </span>
          )}
          {TOGGLE_TYPES.has(block.type) ? (
            <span className={styles.historyPreviewMarker} data-toggle-caret data-open={block.content.collapsed === true ? undefined : "true"}>▸</span>
          ) : null}
          {block.type === "quote" && <span className={styles.historyPreviewQuote} aria-hidden="true" />}
          <span className={styles.historyPreviewBlockText}>
            {parts?.length && !database ? changedText(parts, rich)
              : rich ? <RichTextCellText spans={previewSpansBetween(rich, 0, text.length)} />
              : text || (TEXT_BLOCK_TYPES.has(block.type) ? "\u200b" : t("updatesPanel:versions.emptyBlock"))}
          </span>
          {fileSize ? <span className={styles.historyPreviewFileSize} data-history-file-size>{fileSize}</span> : null}
          {/* An inline database's tools stand at its name's end, muted and
              not to be pressed, as Notion's preview (seen 2026-11-07). */}
          {inlineDatabase ? (
            <span className={styles.historyPreviewDatabaseTools} aria-hidden="true">
              <FilterIcon size="small" />
              <SortIcon size="small" />
              <LightningIcon size="small" />
              <Search size="small" />
              <ExpandDiagonalIcon size="small" />
              <SlidersIcon size="small" />
            </span>
          ) : null}
        </div>
    );
    if (!inlineDatabase) return <Fragment key={block.id}>{row}</Fragment>;
    // An inline database is one block, its name and its table, which Notion's
    // preview outlines whole and counts as a change when the version changed
    // its words or the database itself (seen 2026-11-15).
    const changedDatabase = Boolean(parts) || changedDatabases?.has(block.id);
    return (
      <div key={block.id} className={changeMarks.database} data-history-changed={changedDatabase ? "true" : undefined}
        style={changedDatabase && changeColor ? { "--history-change-color": changeColor } as CSSProperties : undefined}>
        {row}
        <div style={{ "--history-indent": `${(layouts.get(block.id)?.indent ?? 0) - base}px` } as CSSProperties} className={styles.historyPreviewDatabase}>
          {keptNow.has(inlineDatabase.blockId) ? (
            <p className={styles.historyPreviewDatabaseNow} data-history-database-now>{t("updatesPanel:versions.databaseShowsCurrent")}</p>
          ) : null}
          <PageHistoryDatabaseTable
            database={inlineDatabase} marks={rowChanges?.get(inlineDatabase.blockId)} changeColor={changeColor}
            renamed={renamedProperties?.get(inlineDatabase.blockId)} workspaceId={workspaceId}
          />
        </div>
      </div>
    );
  }

  /**
   * A callout as Notion's preview draws it, as its editor does (measured
   * 2026-11-07 and 2026-11-12): a box, its background color's or lined,
   * with its icon and, beside it, its own words and the lines it holds. A
   * gray one's ground is the editor's gray callout's (EditorRichBlocks).
   */
  function calloutRow({ block, depth, removed, inside }: PageHistoryPreviewItem, base: number): ReactNode {
    const icon = storedBlockIcon(block);
    const tint = block.content.color === "gray_background" ? "var(--c-callout-bg-gray)"
      : typeof block.content.color === "string" && block.content.color.endsWith("_background")
        ? blockColorStyle(block.content.color)?.background : undefined;
    const text = blockPreviewText(block);
    const parts = removed ? undefined : changes?.get(block.id);
    const spans = blockPreviewSpans(block);
    const rich = spans && text && spans.map((span) => span.text).join("").trim() === text ? spans : null;
    // A callout written here keeps its words in its first line, a block of
    // its own; Notion's keeps them in the callout, which a change to them
    // outlines whole (seen 2026-11-15), so a change to that line marks the box.
    const firstLine = !text && !removed ? inside?.[0]?.block.id : undefined;
    const wordsChanged = Boolean(firstLine && changes?.has(firstLine));
    return (
      <div key={block.id} className={styles.historyPreviewCallout} data-history-block={block.id} data-type="callout"
        data-tinted={tint ? "true" : undefined} data-history-changed={parts || wordsChanged ? "true" : undefined}
        style={{ ...placeOf(block.id, base), ...(tint ? { background: tint } : {}),
          ...((parts || wordsChanged) && changeColor ? { "--history-change-color": changeColor } : {}) } as CSSProperties}>
        {block.content.iconType === "none" ? null : (
          <span className={styles.historyPreviewCalloutIcon} aria-hidden="true"
            data-emoji={!icon || icon.iconType === "emoji" ? "true" : undefined}>
            {icon ? <PageIconGlyph page={{ ...icon, workspaceId }} {...calloutGlyphSize(icon.iconType)} fallback="💡" /> : <span data-emoji-glyph="">💡</span>}
          </span>
        )}
        <div className={styles.historyPreviewCalloutBody}>
          {text || !inside?.length ? (
            <div className={styles.historyPreviewBlock} data-type="paragraph"
              style={{ ...OWN_LINE, color: colors.get(block.id)?.text } as CSSProperties}>
              <span className={styles.historyPreviewBlockText}>
                {parts?.length ? changedText(parts, rich)
                  : rich ? <RichTextCellText spans={previewSpansBetween(rich, 0, text.length)} /> : text || "\u200b"}
              </span>
            </div>
          ) : null}
          {(inside ?? []).map((row, index) => blockRow({ ...row, depth: Math.max(row.depth - depth - 1, 0) }, layouts.get(block.id)?.indent ?? 0,
            index === 0 && wordsChanged))}
        </div>
      </div>
    );
  }
}

/**
 * A changed block's text, its added and removed words marked; what it holds
 * keeps its formatting where the history kept it.
 */
function changedText(parts: readonly PageHistoryTextPart[], spans: readonly TextSpan[] | null) {
  let offset = 0;
  return parts.map((part, index) => {
    if (part.change === "removed") return <span key={index} data-history-change="removed">{part.text}</span>;
    const from = offset;
    offset += part.text.length;
    const shown = spans ? <RichTextCellText spans={previewSpansBetween(spans, from, offset)} /> : part.text;
    return part.change ? <span key={index} data-history-change={part.change}>{shown}</span> : <Fragment key={index}>{shown}</Fragment>;
  });
}

/**
 * Blocks a version deleted, kept where they stood as Notion's preview keeps
 * them (seen 2026-10-01, its marks 2026-11-07): a light red box across the
 * column under a red erase tag and "콘텐츠를 삭제함", whose chevron, up while
 * open, folds it to that heading.
 */
function RemovedContent({ children }: { children: ReactNode }) {
  const { t } = useTranslation("updatesPanel");
  const [open, setOpen] = useState(true);
  return (
    <div className={styles.historyPreviewRemoved} data-history-removed={open ? "open" : "closed"}>
      <button type="button" className={styles.historyPreviewRemovedHead} aria-expanded={open}
        aria-label={t(open ? "updatesPanel:versions.removedCollapse" : "updatesPanel:versions.removedExpand")}
        onClick={() => setOpen((value) => !value)}>
        <svg className={styles.historyPreviewRemovedIcon} width="14" height="12" viewBox="0 0 14 12" fill="none" aria-hidden="true">
          <path d="M4.3.5h8.2a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4.3a1 1 0 0 1-.77-.36L.45 6.64a1 1 0 0 1 0-1.28L3.53.86A1 1 0 0 1 4.3.5Z" fill="currentColor" />
          <path d="m6.6 3.9 4.2 4.2m0-4.2-4.2 4.2" stroke="white" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
        <span>{t("updatesPanel:versions.removedContent")}</span>
        <ChevronUp size="small" className={styles.historyPreviewRemovedChevron} aria-hidden="true" />
      </button>
      {open ? <div className={styles.historyPreviewRemovedBody} data-history-removed-body>{children}</div> : null}
    </div>
  );
}
