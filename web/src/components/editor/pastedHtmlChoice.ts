import { LEAFEN_BLOCKS_MIME } from "@/lib/clipboard";
import { bigFontSize, clipboardHtmlFragment } from "@/lib/clipboardHtml";
import { EDITOR_PASTE_BOUNDS } from "./editorClipboardPolicy";
import { notionCopyTitles, type NotionCopy } from "./notionPasteShapes";
import { holdsNotionHighlight } from "./richtextStyleColors";

/*
 * Which of an outside paste's two copies is read, as Notion's paste chooses
 * (its paste code, read 2026-11-13): the HTML only when it holds something
 * its text/plain loses; otherwise the text/plain, as if it came alone — one
 * line goes in the line, more after it, Markdown read (seen there: '<p>가줄
 * </p><p>나줄</p>' with '가줄↵나줄' is one block '가줄↵나줄'; '<p>**굵게**
 * 글</p>' is '굵게 글' in bold; with '<b>' it is the HTML's two blocks).
 */

// What holds words alone; section and article are read as a div.
const WORD_HOLDERS = new Set(["span", "div", "p", "section", "article"]);
const WORDPRESS = /<!-- wp:/;

/**
 * Words this element's own style makes bold (bold, or 500 and up), italic,
 * struck through or big type (bigFontSize). Colors, underline, fonts and
 * smaller sizes by style are not counted.
 */
function styled(style: CSSStyleDeclaration) {
  const { fontWeight } = style;
  if (style.textDecoration === "line-through" || style.fontStyle === "italic") return true;
  return fontWeight === "bold" || Number.parseInt(fontWeight, 10) >= 500 || bigFontSize(style.fontSize);
}

function holdsFormatting(element: Element): boolean {
  const tag = element.localName;
  if (element instanceof HTMLElement && styled(element.style)) return true;
  // Notion's copy of words wraps their inline code in this div; read from
  // the HTML it stays code, as Notion's paste of its own copy keeps it.
  if (element.classList.contains("notion-inline-code-container")) return true;
  // So does its copy of colored words, each naming its color; Notion's paste
  // of its own copy keeps the colors (seen 2026-11-14).
  if (holdsNotionHighlight(element)) return true;
  // A checkbox is read as "[ ] " and a Slack emoji picture as its name.
  if (tag === "input") return (element as HTMLInputElement).type !== "checkbox";
  if (tag === "img") return !/^:.+:$/u.test(element.getAttribute("data-stringify-emoji") ?? "");
  // A Google Docs '<b style="font-weight:normal">' holds words alone.
  const unbolded = (tag === "b" || tag === "strong") && element instanceof HTMLElement && element.style.fontWeight === "normal";
  // A line break counts but in a paragraph's own words, where it starts the
  // next paragraph (so does one wrapped in nothing but space).
  if (!WORD_HOLDERS.has(tag) && !unbolded) return true;
  return Array.from(element.children).some(child => !(tag === "p"
    && (child.localName === "br" || (!child.textContent?.trim() && child.querySelector("br")))) && holdsFormatting(child));
}

/**
 * Whether this paste is read from its text/plain over the HTML beside it:
 * outside HTML of nothing but words in paragraphs, divs and spans (not
 * Leafen's own blocks, nor HTML with no text beside it, nor a WordPress
 * copy, whose block comments both hold, nor Notion's copy of inline code).
 */
export function pastesTextOverHtml(data: DataTransfer) {
  const html = data.getData("text/html"), text = data.getData("text/plain");
  if (!html || !text || html.includes("data-native-") || data.types.includes(LEAFEN_BLOCKS_MIME)) return false;
  if (WORDPRESS.test(html) && WORDPRESS.test(text)) return false;
  try {
    const fragment = clipboardHtmlFragment(html, EDITOR_PASTE_BOUNDS.richtext.maxHtmlCharacters);
    return !Array.from(fragment.children).some(holdsFormatting);
  } catch (error) {
    // HTML past its bound is the HTML reading's to refuse.
    if (error instanceof RangeError) return false;
    throw error;
  }
}

/**
 * Whether the clipboard is Notion's own copy: it carries Notion's own kinds
 * beside its HTML (text/_notion-blocks-v3-production and
 * text/_notion-page-source-production for blocks, text/_notion-text-production
 * for words, seen 2026-11-16), which Notion's paste reads over the HTML —
 * with its blocks' words as that kind holds them (notionCopyTitles), false
 * when it isn't Notion's.
 */
export function notionCopy(data: DataTransfer): NotionCopy | false {
  if (!Array.from(data.types).some(type => /^text\/_notion-[\w-]+-production$/u.test(type))) return false;
  const blocks = data.types.includes(NOTION_BLOCKS) ? data.getData(NOTION_BLOCKS) : "";
  return { titles: blocks && blocks.length <= EDITOR_PASTE_BOUNDS.richtext.maxHtmlCharacters ? notionCopyTitles(blocks) : [] };
}

const NOTION_BLOCKS = "text/_notion-blocks-v3-production";
