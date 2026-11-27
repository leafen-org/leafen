import { i18next } from "@/i18n";
import { dateMentionDisplayText } from "@/components/editor/dateMentions";
import type { TextSpan } from "@/lib/types";
import { withCurrentMentionName } from "./peopleDirectory";
import { knownPageName, typedPageTitle } from "./pageNameRecords";
import { dataSourceMentionId, dataSourceMentionNamesNothing } from "./dataSourceMentionNames";
import { markedMentionReach } from "./markedMentionTitles";

// A page or database mention its reader may not open comes from the server
// with no title: in its place, as many of a mark as the title had
// characters, so the page's text keeps its length and the reader's edits
// around it stay where they are (collaboration_reader_mentions.go) — one
// mark for a page closed to the reader, another for one nothing answers to.
// The app shows them as Notion does (seen 2026-11-09): a closed one as an
// eye crossed out and "사용 권한 없음", to a guest; a missing one as a page
// and "찾을 수 없음"; in a plain title either as "@찾을 수 없음".

const CLOSED_MENTION_MARK = "";
const MISSING_MENTION_MARK = "";
const MENTION_MARK_RUN = /+|+/gu;

function allMarks(text: string | undefined, mark: string) {
  return Boolean(text) && text!.length > 0 && [...text!].every((character) => character === mark);
}

/** Whether a mention's words are the marks of one its reader may not open, closed to them or missing. */
export function isClosedMentionText(text: string | undefined) {
  return allMarks(text, CLOSED_MENTION_MARK) || allMarks(text, MISSING_MENTION_MARK);
}

/** Whether a mention's words are the marks of one nothing answers to. */
export function isMissingMentionText(text: string | undefined) {
  return allMarks(text, MISSING_MENTION_MARK);
}

/**
 * A page's or a database's mention as it stands to its reader now when its
 * target closed to them or went since its words came
 * (markedMentionTitles.ts): "사용 권한 없음" for one closed, the marks of one
 * nothing answers to for one gone, as the server would send them now;
 * undefined while it opens, or the workspace's list (`listed`) has the page.
 */
export function mentionClosedSince<T extends { mention?: string; pageId?: string; databaseId?: string; text?: string; unavailable?: boolean }>(
  span: T, listed: ReadonlyMap<string, string>,
): T | undefined {
  const id = span.mention === "page" ? span.pageId : span.mention === "database" ? span.databaseId : undefined;
  if (!id || (span.mention === "page" && listed.has(id))) return undefined;
  const reach = markedMentionReach(span.mention!, id);
  if (reach === "missing") return { ...span, unavailable: true, text: MISSING_MENTION_MARK.repeat(Math.max(1, span.text?.length ?? 0)) };
  return reach === "closed" ? { ...span, unavailable: true, text: closedMentionLabel() } : undefined;
}

/** What a mention nothing answers to says. */
export function notFoundMentionLabel() {
  return String(i18next.t("blockItem:pageLink.mentionNotFound"));
}

/** What a mention its reader may not open says: "찾을 수 없음" for one whose marks say nothing answers to it. */
export function closedMentionLabel(text?: string) {
  return isMissingMentionText(text) ? notFoundMentionLabel() : String(i18next.t("blockItem:pageLink.mentionNoAccess"));
}

/**
 * A title as plain text, as its reader reads it — the one way every list,
 * path, window title and name read aloud says a title — as Notion's sidebar,
 * path and window title do (seen 2026-11-10: "W202 제목 @W202 대상 와 @〈사람〉
 * 날짜 @2026년 11월 10일"): a page or database mention "@" and its target's
 * name as it is now, as its chip draws it (withCurrentMentionName) — Notion's
 * sidebar, window title, path and search result follow a rename of the page a
 * title mentions within seconds (seen 2026-11-16 at 1440, the target renamed
 * through the API while the page was open: "… @w1452 대상 셋 끝") — one the
 * reader may not open "@찾을 수 없음", a data source's of nothing nothing
 * (seen 2026-11-22: "T1 ", that and " end" is the path's "T1  end") and one
 * there "@" and its name ("T1 @W2361 원본 가 end"), a person "@" and their
 * name, a date "@" and its date as its mention shows it, a custom emoji
 * the workspace hasn't nothing (seen 2026-11-11: "emo ", that "?" and " end"
 * is the window title "emo end"; the sidebar and path draw it,
 * TitleWithCustomEmoji). The title's spans, while they spell it, say where
 * its mentions are; without them only a closed mention's marks tell one.
 */
export function titleWithClosedMentions(title: string, spans?: readonly TextSpan[]) {
  const spelled = spans?.some((span) => span.mention) ? spans.map((span) => span.text).join("") : null;
  if (spelled !== null && (spelled === title || spelled.trim() === title)) {
    const said = spans!.map(plainMentionText).join("");
    return spelled === title ? said : said.trim();
  }
  if (!title.includes(CLOSED_MENTION_MARK) && !title.includes(MISSING_MENTION_MARK)) return title;
  return title.replace(MENTION_MARK_RUN, closedPlainMention());
}

/**
 * A page's, row's or database's title as plain text (titleWithClosedMentions),
 * by its own spans, the spaces at its ends too: one of spaces alone shows
 * blank (pageTitle.ts).
 */
export function plainTitleOf(named: { title?: string | null; titleSpans?: readonly unknown[] } | null | undefined) {
  return titleWithClosedMentions(named?.title ?? "", named?.titleSpans as readonly TextSpan[] | undefined);
}

function closedPlainMention() {
  return `@${String(i18next.t("pageView:notFoundTopBarTitle"))}`;
}

/** Whether a title's spans mention a page or a database, whose names it reads as they are now. */
export function namesOtherPages(spans: readonly TextSpan[] | undefined) {
  return Boolean(spans?.some((span) => span.mention === "page" || span.mention === "database"));
}

/**
 * Whether a mention is a data source's, kept by its address — one here
 * (dataSourceMentionNames.ts) or one nothing here answers to
 * (notion_markdown_kept_data_sources.go).
 */
export function isKeptDataSourceMention(span: { mention?: string; databaseId?: string }) {
  return dataSourceMentionId(span) !== undefined;
}

/** A span of a title as plain text: a mention as its reader reads it, other words as they are. */
function plainMentionText(span: TextSpan) {
  switch (span.mention) {
    case "page": case "database": {
      // A data source's of nothing says nothing, one there "@" and its name, as Notion's path and window (seen 2026-11-22).
      if (dataSourceMentionNamesNothing(span, isMissingMentionText(span.text))) return "";
      const now = withCurrentMentionName(span);
      return now.unavailable || isClosedMentionText(now.text) ? closedPlainMention() : `@${now.text}`;
    }
    case "person":
      return span.text.startsWith("@") ? span.text : `@${span.text}`;
    case "date":
      return span.date ? `@${dateMentionDisplayText(span.date, "", undefined, span.dateEnd, span.dateFormat, span.dateTimeFormat)}` : span.text;
    case "custom_emoji":
      return missingCustomEmoji(span) ? "" : span.text;
    default:
      return span.text;
  }
}

// An emoji ID as Leafen gives one; a mention an app wrote of an emoji the
// workspace hasn't keeps any other as written, or its words ":?:"
// (notion_missing_custom_emoji.go).
const EMOJI_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function missingCustomEmoji(span: TextSpan) {
  return customEmojiNeverHad(span.customEmojiId ?? "", span.text);
}

/**
 * Whether a custom emoji mention names one the workspace never had, known
 * without asking: its words ":?:" or an ID Leafen doesn't give — drawn as the
 * dashed "?" for any reader, a published page's visitor too.
 */
export function customEmojiNeverHad(emojiId: string, text?: string) {
  return text === ":?:" || !EMOJI_ID.test(emojiId);
}

/** A title's pieces as its plain title says them, each custom emoji in it apart for a screen that draws it; null for a title without one. */
export function titleCustomEmojiPieces(title: string, spans?: readonly TextSpan[]): (string | TextSpan)[] | null {
  if (!spans?.some((span) => span.mention === "custom_emoji" && span.customEmojiId)) return null;
  const spelled = spans.map((span) => span.text).join("");
  if (spelled !== title && spelled.trim() !== title) return null;
  const pieces: (string | TextSpan)[] = [];
  for (const span of spans) {
    const last = pieces.length - 1;
    if (span.mention === "custom_emoji" && span.customEmojiId) pieces.push(span);
    else if (typeof pieces[last] === "string") pieces[last] += plainMentionText(span);
    else pieces.push(plainMentionText(span));
  }
  if (spelled !== title) {
    if (typeof pieces[0] === "string") pieces[0] = pieces[0].trimStart();
    if (typeof pieces[pieces.length - 1] === "string") pieces[pieces.length - 1] = (pieces[pieces.length - 1] as string).trimEnd();
  }
  return pieces;
}

/** What a title's pieces say as plain text. */
export function plainTitlePieces(pieces: readonly (string | TextSpan)[]) {
  return pieces.map((piece) => typeof piece === "string" ? piece : plainMentionText(piece)).join("");
}

/** Whether a line holds a mention its reader may not open, in marks. */
export function holdsClosedMentions(line: string) {
  return line.includes(CLOSED_MENTION_MARK) || line.includes(MISSING_MENTION_MARK);
}

/** A plain line as its reader reads it: a mention they may not open as "사용 권한 없음" or "찾을 수 없음". */
export function lineWithClosedMentions(line: string) {
  return holdsClosedMentions(line) ? line.replace(MENTION_MARK_RUN, (run) => closedMentionLabel(run)) : line;
}

/**
 * A page's name as the owner of pages' names knows it apart from the list
 * kept for mentions: a title being typed for it, at each key, and a row's
 * the list doesn't hold, named in a body drawn or its page (pageNames.ts).
 */
export function namedMentionTitle(pageId: string): string | undefined {
  const known = knownPageName(pageId);
  if (!known) return undefined;
  const typed = typedPageTitle(pageId, known.title);
  if (typed !== undefined) return typed || i18next.t("common:labels.newPage");
  return known.listed ? undefined : titleWithClosedMentions(known.title, known.titleSpans) || undefined;
}
