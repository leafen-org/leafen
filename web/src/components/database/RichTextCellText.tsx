import type { MouseEvent, ReactNode } from "react";
import type { Page, TextSpan } from "@/lib/types";
import { openPageMention, usePublicRichtextLinks } from "../editor/publicRichtext";
import { opensLink } from "../editor/linkOpening";
import { escapeHtml, spansToHtml } from "../editor/richtext";
import { withCurrentMentionName, withCurrentPublishedMentionName } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { HtmlSpanWithCustomEmoji, InlineCustomEmoji } from "../editor/InlineCustomEmoji";
import editorStyles from "../editor/editorStyles.generated.module.css";
import { CELL_RICHTEXT_BOUNDS, cellSpans } from "./richTextCellModel";
import styles from "./databaseSheet";
import { ArchivedRowTitle } from "./DatabaseArchivedRowsMode";

/** A cell's formatted text as HTML; text the bounds refuse shows plain. */
export function richTextCellHtml(spans: readonly TextSpan[], links?: Parameters<typeof spansToHtml>[2], linksAsText = false) {
  try {
    // A person mention reads with the person's name now.
    const html = spansToHtml(spans.map((span) => links ? withCurrentPublishedMentionName(span) : withCurrentMentionName(span)), CELL_RICHTEXT_BOUNDS, links);
    // A preview inside a link keeps mention styling without nesting anchors.
    return linksAsText ? html.replace(/<a\b/gu, "<span").replace(/<\/a>/gu, "</span>") : html;
  } catch {
    return escapeHtml(spans.map((span) => span.text).join(""));
  }
}

/**
 * Opens what a clicked mention or link in a cell's text names: a page in the
 * app, a link in a new tab.
 */
function openRichTextCellTarget(event: MouseEvent<HTMLElement>) {
  openPageMention(event);
  if (event.defaultPrevented || !opensLink(event)) return;
  const anchor = (event.target as HTMLElement).closest<HTMLAnchorElement>("a[href]");
  const href = anchor?.getAttribute("href");
  if (!anchor || !href || anchor.dataset.mention === "page" || !event.currentTarget.contains(anchor)) return;
  event.preventDefault();
  event.stopPropagation();
  window.open(href, "_blank", "noopener,noreferrer");
}

const customEmojiSpan = (span: TextSpan) => span.mention === "custom_emoji" && Boolean(span.customEmojiId);

/**
 * A database title or text value with its formatting, links and mentions.
 * On a card the whole card opens the row, so its links stay text.
 */
export function RichTextCellText({ spans, className, interactive = false, linksAsText = false }: {
  spans: readonly TextSpan[];
  className?: string;
  interactive?: boolean;
  /** This preview is inside another link, which owns its destination. */
  linksAsText?: boolean;
}) {
  const links = usePublicRichtextLinks();
  // Its person mentions redraw as names change.
  usePeopleDirectoryRevision();
  const classes = `${editorStyles.richMarks} ${styles.richCellText} ${className ?? ""}`;
  const onClick = interactive ? openRichTextCellTarget : undefined;
  const kind = interactive ? "interactive" : "static";
  // A workspace emoji shows its picture, as in the page's text; the text
  // around it keeps its formatting.
  if (spans.some(customEmojiSpan)) {
    const parts: ReactNode[] = [];
    let run: TextSpan[] = [];
    const flush = () => {
      if (run.length) parts.push(<HtmlSpanWithCustomEmoji key={parts.length} html={richTextCellHtml(run, links ?? undefined, linksAsText)} />);
      run = [];
    };
    for (const span of spans) {
      if (!customEmojiSpan(span)) { run.push(span); continue; }
      flush();
      parts.push(<InlineCustomEmoji key={parts.length} emojiId={span.customEmojiId!} name={span.text} publicToken={span.customEmojiPublic} />);
    }
    flush();
    return <span className={classes} data-rich-cell-text={kind} onClick={onClick}>{parts}</span>;
  }
  return (
    <HtmlSpanWithCustomEmoji className={classes} data-rich-cell-text={kind} onClick={onClick}
      html={richTextCellHtml(spans, links ?? undefined, linksAsText)} />
  );
}

/**
 * A short line of formatted text (lib/richTextSummary.ts) — a changed line in
 * the updates, a first line in the publish preview — drawn as the page draws
 * it where its spans are known, else its plain text; links in it stay text,
 * as the line is a link or a picture of its own.
 */
export function RichTextLine({ text, spans }: { text: string; spans?: readonly TextSpan[] }) {
  return spans?.length ? <RichTextCellText spans={spans} linksAsText /> : <>{text}</>;
}

/** A row's title on a card or list, with its formatting when it has any. */
export function RowTitleText({ row, title }: { row: Page; title: string }) {
  const spans = cellSpans(row.titleSpans, row.title);
  const text = spans ? <RichTextCellText spans={spans} /> : <>{title}</>;
  // An archived row, shown while its view shows the archived ones, is faint
  // with the archive mark after it, as Notion's (seen 2026-10-12).
  return <ArchivedRowTitle archived={Boolean(row.archivedAt)}><span data-search-text>{text}</span></ArchivedRowTitle>;
}
