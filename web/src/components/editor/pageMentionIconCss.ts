import { builtInIcon, nativeIconColorVars } from "@/components/page/nativeIcons";
import { TRASH_MARK, TRASH_MARK_BACKGROUND, TRASH_MARK_COLOR } from "./trashMark";
import { nativeIconMarkup } from "@/components/page/nativeIconDrawing";
import type { Page } from "@/types";

// The most pages whose icon shows beside their mentions; past it a mention
// shows the plain page glyph.
export const MENTION_ICON_LIMIT = 2000;
const DATABASE_ICON = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 20 20' fill='none' stroke='black' stroke-width='1.25'%3E%3Crect x='3' y='4.75' width='14' height='10.5' rx='1.5'/%3E%3Cpath d='M3 8.25h14M3 11.75h14M10 4.75v10.5'/%3E%3C/svg%3E")`;
// A page with anything in it is the mention's document with two lines in
// it, as Notion's mention of one (seen 2026-11-13: the lines a third and two
// thirds across, at three fifths and three quarters down), its empty
// document (EditorInlineMarks) otherwise.
const LINED_PAGE_ICON = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' fill='none' stroke='black' stroke-width='1.2' stroke-linejoin='round'%3E%3Cpath d='M4.5 2.5h5l3 3v8h-8z'/%3E%3Cpath d='M9.5 2.5v3h3'/%3E%3Cpath d='M6.5 9h3.5M6.5 11h3.5' stroke-linecap='round'/%3E%3C/svg%3E")`;

export type MentionIconPage = Pick<Page, "id" | "icon" | "iconType" | "inTrash" | "workspaceId">
  & Partial<Pick<Page, "title" | "titleSpans" | "kind" | "hasContent" | "viewBlock" | "sourceTitles" | "untitledName">> & { notionIcon?: unknown };

/** Text as a CSS string made only of escapes, so no icon can end its rule. */
function cssString(value: string) {
  return `"${[...value].map((character) => `\\${(character.codePointAt(0) ?? 0).toString(16)} `).join("")}"`;
}

/** The icon slot before a page's mentions. */
export function mentionSelector(pageId: string) {
  return `:root :is(a[data-mention="page"], span[data-mention="page"][href])[data-page-id="${pageId}"][data-page-id]::before`;
}

/** The mentions of a database, by its ID (richtext.ts), but one closed or missing to its reader. */
function databaseMentionElement(databaseId: string) {
  return `:root :is(a, span)[data-mention="database"][data-database-id="${databaseId}"][data-database-id]:not([data-mention-unavailable])`;
}

/** The icon slot before a database's mentions, past the table EditorInlineMarks draws there. */
export function databaseMentionSelector(databaseId: string) {
  return `${databaseMentionElement(databaseId)}::before`;
}

// A little bin for the mark after a mention of a page in the trash, in the
// mark's gray.

/**
 * The mark after a mention of a page in the trash, as Notion's (seen
 * 2026-11-04). It takes the slot that draws the mention's arrow, which the
 * mention's own background draws instead.
 */
function trashedMentionRule(pageId: string, label: string) {
  return trashedMentionRuleAt(`:root :is(a[data-mention="page"], span[data-mention="page"][href])[data-page-id="${pageId}"][data-page-id]`, label);
}

/**
 * The mark after the mentions a selector names, of what is in the trash
 * (trashedMentionRule), in the size of the words it follows, as Notion's
 * (seen 2026-11-22 in Chrome at 1440, a title's 40px, a heading's 30, 24 and
 * 20, a line's 16: its words 0.9 of theirs on a line as high, 2px and 6px
 * around them; its bin 0.8 of its words, centred 6px in, the words 0.2 of
 * them after it; 6px after the mention's name).
 */
function trashedMentionRuleAt(mention: string, label: string) {
  return `${mention}{background:var(--mention-arrow) .5em .45em/1em 1em no-repeat}`
    + `${mention}::after{content:${cssString(label)};position:static;display:inline-flex;align-items:center;`
    + "width:auto;height:auto;margin-inline-start:6px;padding:2px 6px;padding-inline-start:calc(6px + 1em);"
    + "font-size:.9em;line-height:1em;border-radius:4px;"
    + `color:${TRASH_MARK_COLOR};background:${TRASH_MARK} left 6px center/.8em .8em no-repeat,${TRASH_MARK_BACKGROUND}}`;
}

/** A built-in icon's glyph as a mask, drawn in its color and painted as the icon shows elsewhere (nativeIconDrawing.tsx). */
function builtInIconRule(page: MentionIconPage, selector = mentionSelector(page.id)) {
  const icon = builtInIcon(page);
  if (!icon) return "";
  const svg = nativeIconMarkup(icon.name, "black");
  const mask = `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;
  return `${selector}{content:"";background:${nativeIconColorVars[icon.color]};`
    + `-webkit-mask:${mask} center/contain no-repeat;mask:${mask} center/contain no-repeat}`;
}

/**
 * An emoji in a mention's icon slot: on the text's baseline as Notion's
 * emoji, its line a pixel taller than a plain one (seen 2026-11-01: 41 to 40).
 */
function emojiIconRule(selector: string, emoji: string) {
  return `${selector}{content:${cssString(emoji)};`
    + "background:none;-webkit-mask:none;mask:none;font-size:1em;line-height:1.25em;text-align:center;vertical-align:-0.1875em}";
}

/**
 * The rules that draw each page's emoji or built-in icon before its
 * mentions. A mention is text in the page's body, so its icon comes from the
 * page it names and follows that page's icon rather than being stored in the
 * text.
 */
export function pageMentionIconCss(pages: readonly MentionIconPage[], trashedLabel = "") {
  const rules: string[] = [];
  for (const page of pages) {
    if (rules.length >= MENTION_ICON_LIMIT) break;
    if (!/^[A-Za-z0-9_-]+$/u.test(page.id)) continue;
    if (page.inTrash) {
      if (trashedLabel) rules.push(trashedMentionRule(page.id, trashedLabel));
      continue;
    }
    if (page.iconType === "emoji" && page.icon) {
      rules.push(emojiIconRule(mentionSelector(page.id), page.icon));
      continue;
    }
    const builtIn = builtInIconRule(page);
    if (builtIn) rules.push(builtIn);
    // A database with no icon of its own shows a database's, as a mention of it does (Notion, seen 2026-11-03).
    else if (page.kind === "database" && page.iconType !== "image" && page.iconType !== "custom_emoji") {
      rules.push(`${mentionSelector(page.id)}{-webkit-mask:${DATABASE_ICON} center/100% 100% no-repeat;mask:${DATABASE_ICON} center/100% 100% no-repeat}`);
    } else if (page.hasContent === true && page.iconType !== "image" && page.iconType !== "custom_emoji") {
      rules.push(`${mentionSelector(page.id)}{-webkit-mask-image:${LINED_PAGE_ICON};mask-image:${LINED_PAGE_ICON}}`);
    }
  }
  return rules.join("\n");
}

/**
 * The rules that draw each database's own icon before its mentions — an
 * emoji or a built-in icon (People's person in a ring) where a page's
 * sits, as Notion's database mention draws it (seen 2026-11-22 in Chrome at
 * 1440) — `id` the database's. One with none keeps its table
 * (EditorInlineMarks); a picture or workspace emoji is drawn by its own rule
 * (MentionImageIconRule). After the mentions of one in the trash, "휴지통에
 * 있음", as Notion's (seen 2026-11-24 in Chrome at 1440: a full-page database
 * sent there, an inline one sent there alone and one under a page sent there,
 * each with its icon), as a page's.
 */
export function databaseMentionIconCss(databases: readonly MentionIconPage[], trashedLabel = "") {
  const rules: string[] = [];
  for (const database of databases) {
    if (rules.length >= MENTION_ICON_LIMIT) break;
    if (!/^[A-Za-z0-9_-]+$/u.test(database.id)) continue;
    const selector = databaseMentionSelector(database.id);
    if (database.iconType === "emoji" && database.icon) rules.push(emojiIconRule(selector, database.icon));
    else {
      const builtIn = builtInIconRule(database, selector);
      if (builtIn) rules.push(builtIn);
    }
    if (database.inTrash && trashedLabel) rules.push(trashedMentionRuleAt(databaseMentionElement(database.id), trashedLabel));
  }
  return rules.join("\n");
}

/** A data source mentioned, as its mention reads it (DataSourceMentionStyles.tsx). */
export type MentionIconDataSource = Pick<MentionIconPage, "id" | "icon" | "iconType" | "inTrash">;

/** The mentions of a data source, by its ID (richtextPageLinks.ts dataSourceMentionHtml), named past the cylinder's rule. */
export function dataSourceMentionElement(dataSourceId: string) {
  return `:root :is(a, span)[data-mention="database"][data-mention-data-source="${dataSourceId}"][data-mention-data-source]`
    + ":not([data-mention-unavailable])";
}

/**
 * The rules that draw each data source's own icon before its mentions — an
 * emoji or a built-in icon in place of the cylinder (EditorInlineDecorations),
 * as Notion's data source mention draws its source's (seen 2026-11-22 in
 * Chrome at 1440: 🍎, its database's own icon not) — and "휴지통에 있음" after
 * the mentions of one in the trash, as Notion's (seen 2026-11-22: the
 * source of a database in the trash, and one sent there alone), as a page's.
 * A picture or workspace emoji is drawn by its own rule (MentionImageIconRule).
 */
export function dataSourceMentionCss(sources: readonly MentionIconDataSource[], trashedLabel: string) {
  const rules: string[] = [];
  for (const source of sources) {
    if (rules.length >= MENTION_ICON_LIMIT) break;
    if (!/^[A-Za-z0-9_-]+$/u.test(source.id)) continue;
    const mention = dataSourceMentionElement(source.id);
    if (source.iconType === "emoji" && source.icon) rules.push(emojiIconRule(`${mention}::before`, source.icon));
    else {
      const builtIn = builtInIconRule({ ...source, workspaceId: "" }, `${mention}::before`);
      if (builtIn) rules.push(builtIn);
    }
    if (source.inTrash && trashedLabel) rules.push(trashedMentionRuleAt(mention, trashedLabel));
  }
  return rules.join("\n");
}
