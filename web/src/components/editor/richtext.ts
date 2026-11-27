import type { TextSpan } from "@/lib/types";
import { dateMentionDisplayText, dateMentionHtml, dateMentionLabel } from "./dateMentions";
import { escapeHtml } from "./richtextHtmlEscaping";
import { emptyInlineEquationHtml, inlineEquationHtml } from "./richtextEquation";
import { safeInlineEquationValue } from "./richtextEquationPolicy";
import {
  safeDateMentionTimeZoneValue,
  safeDateMentionValueValue,
  safeExternalMentionUrlValue,
  safeLinkMentionHref,
  safeMentionIdValue,
} from "./richtextMentionPolicy";
import {
  dataSourceMentionHtml, missingMentionHtml, pageMentionHtml, publicDatabaseMentionHtml, richtextLinkHref, unavailableMentionHtml,
  type RichtextPageLinks,
} from "./richtextPageLinks";
import { suggestionWrapped } from "./richtextSuggestionPolicy";
import { textLinkHref } from "./richtextUrlPolicy";
import { automationMentionIsVariable } from "./automationMentionVariable";
import { linkMentionHtml, linkMentionShownTitle } from "./linkMentionHtml";
import { templateMentionDescription } from "./templateMentions";
import {
  admitRichtextSpans,
  consumeRichtextHtml,
  consumeRichtextInput,
  consumeRichtextOutput,
  createRichtextBudget,
  type RichtextBounds,
} from "./richtextBudget";
import { textSpanColors } from "@/lib/textSpanColors";
import { closedMentionLabel, isClosedMentionText, isMissingMentionText, notFoundMentionLabel } from "@/lib/closedMentions";
import { mentionTitlePieces } from "@/lib/mentionTitlePieces";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { mentionServerAttribute } from "./richtextMentionServer";
import { keptAddressMention, keptAddressMentionHtml } from "./keptAddressMentions";
import { dataSourceMentionId } from "@/lib/dataSourceMentionNames";
import { coalesceWithin, safeColorTokenValue, safeCommentIdValue, templateMentionValue } from "./richtextSpanValues";

export { escapeHtml } from "./richtextHtmlEscaping";
export { safeUrl, textLinkHref } from "./richtextUrlPolicy";
export type { RichtextBounds } from "./richtextBudget";
export type { RichtextPageLinks } from "./richtextPageLinks";
export { htmlToSpans, isInlineRichtextElement } from "./richtextHtmlRead";

function admitScalar(value: string | undefined, bounds: RichtextBounds) {
  const budget = createRichtextBudget(bounds);
  if (value) consumeRichtextInput(value, bounds, budget);
}

export function safeColorToken(color: string | undefined, bounds: RichtextBounds) {
  admitScalar(color, bounds);
  return safeColorTokenValue(color);
}

export function safeMentionId(value: string | undefined, bounds: RichtextBounds) {
  admitScalar(value, bounds);
  return safeMentionIdValue(value);
}

export function safeDateMentionValue(value: string | undefined, bounds: RichtextBounds) {
  admitScalar(value, bounds);
  return safeDateMentionValueValue(value);
}

export function safeDateMentionTimeZone(value: string | undefined, bounds: RichtextBounds) {
  admitScalar(value, bounds);
  return safeDateMentionTimeZoneValue(value);
}

export function safeExternalMentionUrl(url: string | undefined, bounds: RichtextBounds) {
  admitScalar(url, bounds);
  return safeExternalMentionUrlValue(url);
}

export function safeLinkMention(url: string | undefined, bounds: RichtextBounds) {
  admitScalar(url, bounds);
  return safeLinkMentionHref(url);
}

export function spanToHtml(span: TextSpan, links?: RichtextPageLinks) {
  return suggestionWrapped(span, spanContentHtml(span, links));
}

function spanContentHtml(span: TextSpan, links?: RichtextPageLinks) {
  const unavailable = (span.unavailable === true || isClosedMentionText(span.text)) && (span.mention === "page" || span.mention === "database");
  const notFound = unavailable && isMissingMentionText(span.text);
  const dateMentionValue = span.mention === "date" ? safeDateMentionValueValue(span.date) : undefined;
  // A date whose start was never given, or Markdown kept as written that
  // reads as no date ("hello"), shows "@" alone, and an arrow for its end and
  // the end's day when that reads as one ("@ → 2026년 1월 2일"), as Notion's
  // (seen 2026-11-11).
  const unreadDate = span.mention === "date" && !dateMentionValue;
  const dateMentionEnd = span.mention === "date" ? safeDateMentionValueValue(span.dateEnd) : undefined;
  const unreadDateEnd = unreadDate && dateMentionEnd
    ? ` ${dateMentionLabel(dateMentionEnd, undefined, span.dateFormat, span.dateTimeFormat)}` : "";
  const dateMentionTimeZone = dateMentionValue
    ? safeDateMentionTimeZoneValue(span.dateTimeZone)
    : undefined;
  const pageMentionId = span.mention === "page" && !unavailable ? safeMentionIdValue(span.pageId) : undefined;
  // A mention its reader can't open reads "사용 권한 없음" (links, or the
  // words its view gave it), or "찾을 수 없음" when nothing answers to it,
  // never its target's name.
  const text = notFound
    ? notFoundMentionLabel()
    : unavailable
    ? (links?.unavailable ?? closedMentionLabel())
    : dateMentionValue
    ? dateMentionDisplayText(dateMentionValue, span.text ?? "", undefined, dateMentionEnd, span.dateFormat, span.dateTimeFormat)
    : unreadDate ? `@${span.dateEnd ? ` →${unreadDateEnd}` : ""}`
    // An untitled page by its kind, as the server read it for the visitor:
    // a full-page database's "새 데이터베이스", one of several sources "A 및
    // B" by their titles (untitledPageDisplayTitle).
    : pageMentionId && !span.text && links ? untitledPageDisplayTitle({
      kind: span.mentionPageKind, viewBlock: span.mentionViewBlock === true, sourceTitles: span.mentionSourceTitles })
    // A database's mention its way: "새 데이터베이스" untitled, "A 및 B" of several sources.
    : span.mention === "database" && !span.text && links && span.mentionPageId
      ? untitledPageDisplayTitle({ kind: "database", sourceTitles: span.mentionSourceTitles })
    : span.mention === "external" && safeLinkMentionHref(span.link) ? linkMentionShownTitle(span.text ?? "")
    : (span.text ?? "");
  const equation = safeInlineEquationValue(span.equation);
  // A date, a person or a template's mention reads "@" first, drawn lighter
  // than its name, as Notion's always do (seen 2026-10-31, 2026-11-09) —
  // whatever text the writer (an API, an import) stored with it.
  const leadingAt = Boolean(text) && Boolean(dateMentionValue || unreadDate || (span.mention === "person" && safeMentionIdValue(span.userId))
    || (span.mention === "template" && templateMentionValue(span.template)));
  // One kept by an ID no person here has — the API's "@Anonymous" — shows
  // "@" and a grey bar for the name, as Notion's (seen 2026-11-11); its words
  // stay under the bar so the text reads back the same.
  const missingPerson = leadingAt && span.mention === "person" && personMentionMissing(span.userId, text);
  const kept = keptAddressMention(span);
  let html = kept ? keptAddressMentionHtml(kept, text)
    : span.emptyPiece === "equation" ? emptyInlineEquationHtml()
    : equation === text
    ? `<span data-equation="${escapeHtml(equation)}" data-text="${escapeHtml(equation)}" contenteditable="false">${inlineEquationHtml(equation)}</span>`
    : missingPerson ? `<span data-mention-at="">@</span><span data-person-missing="">${escapeHtml(text.replace(/^@/u, ""))}</span>`
    : leadingAt ? `<span data-mention-at="">@</span>${escapeHtml(text.replace(/^@/u, ""))}`
    : mentionTitleHtml(span, unavailable, text) ?? escapeHtml(text);
  // An underline and a strikethrough sit inside the code and the colors, so
  // their lines take the words' color, as the editor nests them (nativeTiptapSchema.ts).
  if (span.underline) html = `<u>${html}</u>`;
  if (span.strikethrough) html = `<s>${html}</s>`;
  if (span.code) html = `<code>${html}</code>`;
  if (span.bold) html = `<strong>${html}</strong>`;
  if (span.italic) html = `<em>${html}</em>`;
  // A text color and a background are each drawn, the background outside.
  const colors = textSpanColors(span);
  const color = safeColorTokenValue(colors.color);
  if (color) html = `<span data-color="${escapeHtml(color)}">${html}</span>`;
  const background = safeColorTokenValue(colors.background);
  if (background) html = `<span data-color="${escapeHtml(background)}">${html}</span>`;
  const commentId = safeCommentIdValue(span.commentId);
  if (commentId) html = `<span data-comment-id="${escapeHtml(commentId)}">${html}</span>`;
  if (equation === text || span.emptyPiece === "equation") return html;
  if (kept) return `<span data-mention="${kept}" contenteditable="false">${html}</span>`;
  if (notFound) return missingMentionHtml(span, html);
  if (unavailable) return unavailableMentionHtml(span, html);
  if (pageMentionId) return pageMentionHtml(span, pageMentionId, html, links);
  const dataSourceId = safeMentionIdValue(dataSourceMentionId(span));
  if (dataSourceId) return dataSourceMentionHtml(span, dataSourceId, html, links);
  const databaseMentionId = span.mention === "database"
    ? safeMentionIdValue(span.databaseId)
    : undefined;
  const publicDatabase = databaseMentionId && publicDatabaseMentionHtml(span, databaseMentionId, html, links);
  if (publicDatabase) return publicDatabase;
  if (databaseMentionId) {
    return `<span data-mention="database" data-database-id="${escapeHtml(databaseMentionId)}"${mentionServerAttribute()} contenteditable="false">${html}</span>`;
  }
  const customEmojiMentionId = span.mention === "custom_emoji"
    ? safeMentionIdValue(span.customEmojiId)
    : undefined;
  if (customEmojiMentionId) {
    const publicToken = span.customEmojiPublic ? ` data-custom-emoji-public="${escapeHtml(span.customEmojiPublic)}"` : "";
    return `<span data-mention="custom_emoji" data-custom-emoji-id="${escapeHtml(customEmojiMentionId)}"${publicToken}${mentionServerAttribute()} `
      + `contenteditable="false">${html}</span>`;
  }
  if (dateMentionValue || unreadDate) {
    return dateMentionHtml({
      value: dateMentionValue ?? span.date ?? "", end: dateMentionEnd, timeZone: dateMentionTimeZone,
      reminder: span.dateReminder, format: span.dateFormat, timeFormat: span.dateTimeFormat, displayText: text, storedText: span.text ?? "", html,
    });
  }
  const templateMention = span.mention === "template" ? templateMentionValue(span.template) : undefined;
  if (templateMention) {
    // Pointing at it says what it becomes in a quiet tip under its start, as Notion's (seen 2026-11-09; "@나" says nothing).
    const description = templateMentionDescription(templateMention);
    const tip = description ? ` data-icon-tip="${escapeHtml(description)}" data-tip-side="start" data-tip-quiet=""` : "";
    return `<span data-mention="template" data-template="${templateMention}"${tip} contenteditable="false">${html}</span>`;
  }
  // An automation message's mention: a chip, its icon drawn where it is edited (richTextCellEngine.ts);
  // a variable's is blue, as Notion's.
  if (span.mention === "automation" && span.automation) {
    const variable = automationMentionIsVariable(span.automation) ? " data-automation-variable=\"\"" : "";
    return `<span data-mention="automation" data-automation="${escapeHtml(span.automation)}" data-automation-type="${escapeHtml(span.automationType ?? "")}"`
      + `${variable} contenteditable="false"><span data-automation-icon=""></span>${html}</span>`;
  }
  const personMentionId = span.mention === "person" ? safeMentionIdValue(span.userId) : undefined;
  if (personMentionId) {
    return `<span data-mention="person" data-user-id="${escapeHtml(personMentionId)}"${mentionServerAttribute()} contenteditable="false">${html}</span>`;
  }
  const externalMentionHref = span.mention === "external"
    ? safeLinkMentionHref(span.link)
    : "";
  if (externalMentionHref) return linkMentionHtml(span, externalMentionHref, html);
  const linkHref = richtextLinkHref(textLinkHref(span.link), links);
  if (linkHref) html = `<a href="${escapeHtml(linkHref)}">${html}</a>`;
  return html;
}

/**
 * A page's or database's mention whose title holds a custom emoji draws it
 * in its words, as Notion's (seen 2026-11-11: one the workspace hasn't as the
 * dashed "?" in the mention); its picture is shown where the markup is put
 * (showCustomEmojiPictures). Null for any other mention.
 */
function mentionTitleHtml(span: TextSpan, unavailable: boolean, text: string) {
  if (unavailable || span.code || span.blockId) return null;
  const id = span.mention === "page" ? safeMentionIdValue(span.pageId) : span.mention === "database" ? safeMentionIdValue(span.databaseId) : undefined;
  const pieces = id ? mentionTitlePieces(span.mention as "page" | "database", id, text) : null;
  return pieces?.map((piece) => typeof piece === "string" ? escapeHtml(piece)
    : `<span data-mention="custom_emoji" data-custom-emoji-id="${escapeHtml(piece.customEmojiId ?? "")}"${piece.customEmojiPublic
      ? ` data-custom-emoji-public="${escapeHtml(piece.customEmojiPublic)}"` : ""} contenteditable="false">${escapeHtml(piece.text)}</span>`).join("") ?? null;
}

// The words a mention of an ID no person here has is kept with
// (notion_markdown_person_mentions.go), by a UUID, as no Leafen account's ID is.
const missingPersonText = "@Anonymous";
const uuidShape = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/iu;

function personMentionMissing(userId: string | undefined, text: string) {
  return text === missingPersonText && uuidShape.test(userId ?? "");
}

export function spansToHtml(spans: TextSpan[] | undefined, bounds: RichtextBounds, links?: RichtextPageLinks) {
  const budget = createRichtextBudget(bounds);
  const admitted = admitRichtextSpans(spans, bounds, budget);
  const fragments: string[] = [];
  for (const span of admitted) {
    const fragment = spanToHtml(span, links);
    consumeRichtextHtml(fragment, bounds, budget);
    fragments.push(fragment);
  }
  return fragments.join("");
}

export function spansPlainText(spans: TextSpan[] | undefined, bounds: RichtextBounds) {
  const budget = createRichtextBudget(bounds);
  const admitted = admitRichtextSpans(spans, bounds, budget);
  const parts: string[] = [];
  for (const span of admitted) {
    consumeRichtextOutput(span.text, bounds, budget);
    parts.push(span.text);
  }
  return parts.join("");
}

export function coalesce(spans: TextSpan[], bounds: RichtextBounds) {
  const budget = createRichtextBudget(bounds);
  const admitted = admitRichtextSpans(spans, bounds, budget);
  return coalesceWithin([...admitted], bounds, budget);
}

export function splitSpans(spans: TextSpan[], offset: number, bounds: RichtextBounds) {
  const budget = createRichtextBudget(bounds);
  admitRichtextSpans(spans, bounds, budget);
  const before: TextSpan[] = [];
  const after: TextSpan[] = [];
  let accumulated = 0;
  for (const span of spans) {
    const length = span.text.length;
    if (accumulated >= offset) after.push(span);
    else if (accumulated + length <= offset) before.push(span);
    else {
      const cut = offset - accumulated;
      before.push({ ...span, text: span.text.slice(0, cut) });
      after.push({ ...span, text: span.text.slice(cut) });
    }
    accumulated += length;
  }
  return [
    coalesceWithin(before, bounds, budget),
    coalesceWithin(after, bounds, budget),
  ] as [TextSpan[], TextSpan[]];
}
