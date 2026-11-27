import type { MouseEvent, ReactNode } from "react";
import { i18next } from "@/i18n";
import { shortAgoLabel } from "@/lib/relativeTime";
import { MENTION_SERVER_ATTRIBUTE, clipboardServer } from "@/lib/clipboardServer";
import type { CommentActorProfile, CommentBody } from "@/commentWire.generated";
import { commentBounds } from "@/commentWire.generated";
import type { NativeComment } from "@/lib/commentService";
import type { TextSpan } from "@/lib/types";
import { personName, unknownPersonLabel, withCurrentMentionName } from "@/lib/peopleDirectory";
import { escapeHtml, spansToHtml } from "@/components/editor/richtext";
import { openPageMention } from "@/components/editor/publicRichtext";
import { HtmlSpanWithCustomEmoji, InlineCustomEmoji } from "@/components/editor/InlineCustomEmoji";
import { trimCellChange } from "@/components/database/richTextCellModel";
import editorStyles from "@/components/editor/editorStyles.generated.module.css";
import styles from "../CommentsPanel.module.css";
import { commentQuotePlacesOf } from "./commentQuotePlaces";

const COMMENT_RICHTEXT_BOUNDS = {
  maxSpans: 4096,
  maxTextCharacters: commentBounds.bodyBytes,
  maxHtmlCharacters: commentBounds.bodyBytes,
  maxDomNodes: 8192,
  maxDomDepth: 64,
};


export function richTextSpans(body: unknown): TextSpan[] {
  if (typeof body === "string") return body ? [{ text: body }] : [];
  const rich = (body as { rich?: TextSpan[] } | undefined)?.rich;
  // A body stored as {"text": …} before the API kept to one shape reads as its text (2026-10-16).
  const text = (body as { text?: unknown } | null | undefined)?.text;
  if (!Array.isArray(rich) && typeof text === "string") return text ? [{ text }] : [];
  return Array.isArray(rich)
    ? rich.filter((span) => span && typeof span.text === "string")
    : [];
}

/**
 * A thread begun by a reaction to text, as Notion's toolbar reaction: its
 * first comment has no text, mention, attachment or suggestion.
 */
export function isReactionThread(comment: NativeComment) {
  if (comment.parentId || !comment.blockId) return false;
  const body = comment.body as { attachmentIds?: unknown; suggestion?: unknown } | null;
  if (body?.suggestion || (Array.isArray(body?.attachmentIds) && body.attachmentIds.length > 0)) return false;
  return richTextSpans(comment.body).every((span) => !span.text.trim() && !span.mention);
}

/** A run drawn beyond its text: marks, a color, a link, an equation or a mention other than a person's. */
function formattedCommentSpan(span: TextSpan) {
  return Boolean(span.bold || span.italic || span.underline || span.strikethrough || span.code || span.color
    || span.background || span.link || span.equation || (span.mention && span.mention !== "person"));
}

/** Opens a page a comment mentions in the app, and a link in a new tab. */
function openCommentRichTextTarget(event: MouseEvent<HTMLElement>) {
  openPageMention(event);
  if (event.defaultPrevented || event.button !== 0) return;
  const anchor = (event.target as HTMLElement).closest<HTMLAnchorElement>("a[href]");
  const href = anchor?.getAttribute("href");
  if (!anchor || !href || !event.currentTarget.contains(anchor)) return;
  event.preventDefault();
  window.open(href, "_blank", "noopener,noreferrer");
}

function formattedCommentHtml(span: TextSpan) {
  try {
    return spansToHtml([span], COMMENT_RICHTEXT_BOUNDS);
  } catch {
    return escapeHtml(span.text ?? "");
  }
}

/**
 * A comment's text with its formatting, links and mentions, as Notion's
 * comments keep them (2026-10-14: comments a migration brought kept only
 * their words).
 */
export function renderCommentRichText(spans: TextSpan[]): ReactNode {
  // A person mention reads with the person's name now (its card redraws as names change).
  return spans.map(withCurrentMentionName).map((span, index) => {
    const text = span.text ?? "";
    if (!text) return null;
    // A workspace emoji shows its picture, as in the page's text.
    if (span.mention === "custom_emoji" && span.customEmojiId) {
      return <InlineCustomEmoji key={index} emojiId={span.customEmojiId} name={text} publicToken={span.customEmojiPublic} />;
    }
    if (formattedCommentSpan(span)) {
      return (
        <HtmlSpanWithCustomEmoji key={index} className={editorStyles.richMarks} data-comment-rich-text=""
          onClick={openCommentRichTextTarget} html={formattedCommentHtml(span)} />
      );
    }
    // A person reads as the body's mention does — grey, its "@" lighter, no
    // chip — as Notion's posted comments (seen 2026-10-27), and resting on it
    // shows their card (PersonHoverCard), with no tip of its own (seen
    // 2026-11-11); copied, it is its words (copyCommentText).
    if (span.mention === "person" && span.userId?.trim()) {
      const userId = span.userId.trim();
      return (
        <span key={`${userId}-${index}`} className={editorStyles.richMarks}>
          <span className={styles.commentMention} data-mention="person" data-user-id={userId}
            {...{ [MENTION_SERVER_ATTRIBUTE]: clipboardServer() }}>
            <span data-mention-at="">@</span>{text.replace(/^@/u, "")}
          </span>
        </span>
      );
    }
    return <span key={index}>{text}</span>;
  });
}

/**
 * A quote longer than fifty characters is cut there with "…", as Notion's
 * (seen 2026-11-09): characters are code points, an emoji one, and the line
 * between the blocks of words chosen across blocks one.
 */
const QUOTE_LENGTH = 50;

export function quoteText(body: unknown, { words = false }: {
  /** The words alone, without a property's name: as a page's updates show them (Notion's, seen 2026-11-12). */
  words?: boolean;
} = {}): string {
  const anchor = body as { quote?: unknown; propertyName?: unknown } | undefined;
  const quote = typeof anchor?.quote === "string" ? anchor.quote.trim() : "";
  // A thread on a database page's property quotes the property and the
  // value it was left on, as Notion's ("Notes: Probe note", seen 2026-10-01).
  const property = !words && commentPropertyId(body) && typeof anchor?.propertyName === "string" ? anchor.propertyName.trim() : "";
  const text = property && quote ? `${property}: ${quote}` : property || quote;
  const characters = Array.from(text);
  return characters.length > QUOTE_LENGTH ? `${characters.slice(0, QUOTE_LENGTH).join("")}…` : text;
}

/** The database page's property a thread was left on, which its first comment names. */
export function commentPropertyId(body: unknown): string {
  const id = (body as { propertyId?: unknown } | null | undefined)?.propertyId;
  return typeof id === "string" ? id : "";
}

export function quoteRange(body: unknown) {
  const range = body as { quoteStart?: unknown; quoteEnd?: unknown } | undefined;
  if (typeof range?.quoteStart !== "number" || typeof range?.quoteEnd !== "number") return null;
  if (range.quoteEnd <= range.quoteStart) return null;
  return { start: range.quoteStart, end: range.quoteEnd };
}

/** A draft as a comment keeps it: its marks, links and mentions, without the spaces around it. */
export function sentCommentSpans(spans: readonly TextSpan[]): TextSpan[] {
  const trimmed = trimCellChange({ value: spans.map((span) => span.text).join(""), spans: [...spans] });
  return trimmed.spans ?? (trimmed.value ? [{ text: trimmed.value }] : []);
}

export function commentDraftEmpty(spans: readonly TextSpan[]) {
  return !spans.some((span) => span.text.trim());
}

/**
 * An edit's spans to save: null while it is empty, which can't be saved,
 * and false when it says what the comment says. Files being added save it
 * whatever its words say.
 */
export function editedCommentSpans(draft: readonly TextSpan[], shown: readonly TextSpan[], adding = false): TextSpan[] | null | false {
  const next = sentCommentSpans(draft);
  if (adding) return next;
  if (commentDraftEmpty(next)) return null;
  return JSON.stringify(next) === JSON.stringify(sentCommentSpans(shown)) ? false : next;
}

/** An edited comment keeps its formatting and mentions, as Notion's (seen 2026-10-15). */
/**
 * A stored body's parts the API takes again — comment-wire's CommentBody —
 * with its text as runs: a body of another shape keeps its text and nothing
 * else (2026-10-16: the API keeps bodies to one shape).
 */
export function keptCommentBody(body: unknown): CommentBody {
  const parts = body && typeof body === "object" ? body as Record<string, unknown> : {};
  const kept: CommentBody = { rich: richTextSpans(body) as CommentBody["rich"] };
  if (Array.isArray(parts.attachmentIds)) kept.attachmentIds = parts.attachmentIds.filter((id): id is string => typeof id === "string");
  if (typeof parts.quote === "string") kept.quote = parts.quote;
  if (typeof parts.quoteStart === "number") kept.quoteStart = parts.quoteStart;
  if (typeof parts.quoteEnd === "number") kept.quoteEnd = parts.quoteEnd;
  // Its caption and the blocks it runs on into stay named, so its marks stay its own.
  Object.assign(kept, commentQuotePlacesOf(body));
  if (typeof parts.propertyId === "string") kept.propertyId = parts.propertyId;
  if (typeof parts.propertyName === "string") kept.propertyName = parts.propertyName;
  return kept;
}

export function editedCommentBody(comment: NativeComment, spans: readonly TextSpan[]): CommentBody {
  return { ...keptCommentBody(comment.body), rich: sentCommentSpans(spans) as CommentBody["rich"] };
}

/** When a comment was made, as Notion writes it by the writer's name. */
export function timeLabel(value?: string) {
  return shortAgoLabel(value);
}

/**
 * The name a comment a connection wrote through the Notion API shows instead
 * of its person, as Notion's screen shows an integration's comment (seen
 * 2026-11-12): the integration's name, or the custom name it asked for with
 * the integration named beside it ("알림봇 (〈연결〉에서 제공)"), on the
 * integration's face; "" for a person's, and for one it asked to show as its
 * person's. The server keeps how it was written (the body's "notion" part).
 */
export function commentConnectionAuthor(comment: Pick<NativeComment, "body">) {
  return connectionAuthorName(connectionWriting(comment));
}

/** How a connection wrote a comment: its body's "notion" part, or the actorConnection the server reads from it for a notice. */
type ConnectionWritingParts = { displayNameType?: unknown; resolvedName?: unknown; connectionName?: unknown };

/** How a connection wrote a comment, as its body keeps it; null for a person's. */
function connectionWriting(comment: Pick<NativeComment, "body">) {
  return connectionWritingOf((comment.body as { notion?: ConnectionWritingParts } | null)?.notion);
}

/** The name a connection wrote as, and the connection named beside a custom one; null for a person's. */
function connectionWritingOf(shown: ConnectionWritingParts | null | undefined) {
  const name = typeof shown?.resolvedName === "string" ? shown.resolvedName.trim() : "";
  const connection = typeof shown?.connectionName === "string" ? shown.connectionName.trim() : "";
  const custom = shown?.displayNameType === "custom";
  if (!name || (!custom && shown?.displayNameType !== "integration")) return null;
  return { name, connection: custom ? connection : name, custom };
}

function connectionAuthorName(shown: ReturnType<typeof connectionWritingOf>) {
  if (!shown) return "";
  return shown.custom && shown.connection
    ? i18next.t("commentsPanel:card.customAuthor", { name: shown.name, connection: shown.connection })
    : shown.name;
}

/**
 * Who a notice a connection's comment made came from, named and drawn as
 * the comment's header (commentConnectionAuthor, commentAuthorFace): Notion's
 * inbox names an integration's mention by the integration on its rounded
 * square (seen 2026-11-12). The server reads it from the comment; null for
 * a person's notice.
 */
export function noticeConnectionAuthor(actorConnection: ConnectionWritingParts | null | undefined) {
  const shown = connectionWritingOf(actorConnection);
  return shown ? { name: connectionAuthorName(shown), face: shown.connection || shown.name } : null;
}

/**
 * How a comment's writer is drawn: its person's face, which opens their card,
 * or a connection's (commentConnectionAuthor) — the connection's first letter
 * on a rounded square, a custom name's too, with no card, as Notion draws an
 * integration's comment. Spread after the shown name: a connection's face
 * names the connection.
 */
export function commentAuthorFace(comment: NativeComment, profiles: Readonly<Record<string, CommentActorProfile>>) {
  const shown = connectionWriting(comment);
  return shown
    ? { userId: null, avatar: null, bot: true as const, name: shown.connection || shown.name }
    : { userId: comment.authorId, avatar: profiles[comment.authorId]?.avatar, bot: undefined };
}

export function authorLabel(
  comment: NativeComment,
  userId: string,
  profiles: Readonly<Record<string, CommentActorProfile>>,
) {
  const connection = commentConnectionAuthor(comment);
  if (connection) return connection;
  // Their own comment names them, as Notion's does, before its profile comes.
  if (comment.authorId === userId) {
    return profiles[comment.authorId]?.displayName?.trim() || personName(comment.authorId)
      || i18next.t("peopleDirectory:you");
  }
  return profiles[comment.authorId]?.displayName?.trim() || unknownPersonLabel();
}

export function spansToPlainTextSafe(spans?: TextSpan[]): string {
  return (spans ?? []).map((span) => span.text).join("");
}
