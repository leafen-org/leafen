import type { UsableApp } from "@/appPlatformWire.generated";
import type { ImportFormat } from "@/components/import/ImportDialog";
import { NEW_CALLOUT_COLOR, NEW_CALLOUT_ICON } from "@/lib/blockDefaults";
import type { BlockType, ViewType } from "@/lib/types";
import { DATABASE_SLASH_DEFS } from "./databaseSlashDefinitions";
import { INLINE_SLASH_DEFS } from "./inlineSlashDefinitions";
import { SLASH_COMMAND_DEFS } from "./slashCommandDefinitions";
import { EMBED_SERVICES } from "./embedServices";

/** What a slash item in the groups after "Inline" does to its line. */
export type SlashAction =
  | "copy_link"
  | "duplicate"
  | "move_to"
  | "delete"
  | "present"
  | "turn_into"
  | "set_color"
  | "import";

/** What an inline slash item puts in its line; see inlineSlashDefinitions. */
export type InlineSlashKind = "person" | "page" | "date" | "emoji" | "equation";

/**
 * The slash menu's groups, in Notion's order (seen 2026-09-30): the block
 * groups, "Inline", "Embed", then what acts on the line — turn into,
 * actions, text colors and background colors.
 */
export type SlashGroup =
  | "Basic"
  | "Media"
  | "Database"
  | "Advanced"
  | "Inline"
  | "Embed"
  | "Apps"
  | "Import"
  | "TurnInto"
  | "Actions"
  | "TextColor"
  | "BackgroundColor";

export interface BlockDef {
  id?: string;
  type: BlockType;
  /** English source label; user-facing display resolves via i18next (see
   * blockDefLabel). Kept for direct English reads and cross-language search. */
  label: string;
  /** English source description; see label. */
  description: string;
  /** sidebar/slash icon glyph */
  glyph: string;
  group: SlashGroup;
  keywords: string[];
  /**
   * Korean search words, matched with `keywords` by the block menu's search
   * on a screen in a language the search words don't stand for
   * (blockDefSearchKeywords); the "/" search reads the search words
   * (blockSearchWords).
   */
  koKeywords?: string[];
  placeholder?: string;
  columnCount?: number;
  /** "페이지 위치:" of "Turn into": the block goes in a page the person picks, as a page, leaving a link. */
  pageIn?: true;
  databaseView?: ViewType;
  /** The first view's settings, as a chart's kind. */
  viewConfig?: Record<string, unknown>;
  /** What the block starts with, as a Mermaid code block's language. */
  content?: Record<string, unknown>;
  /** Text the block is made with, chosen so typing replaces it. */
  sampleText?: string;
  action?: SlashAction;
  colorToken?: string;
  hiddenWhenEmpty?: boolean;
  /** A block "/" doesn't make at all, though its "Turn into" item is there. */
  slashHidden?: true;
  /** An item that goes into the line rather than making a block. */
  inline?: InlineSlashKind;
  /** A "가져오기" item's file format. */
  importFormat?: ImportFormat;
  /** An item only a build with the app platform offers. */
  appPlatform?: boolean;
  /** An installed app's block, named as the app names it and shown with Leafen's "앱" mark and the app's name (docs/app-platform.md 10-1); `keywords`, the words its "/" item declares (docs/apps.md). */
  app?: { name: string; title: string; keywords?: readonly string[] };
  /** An app's "/" item that runs one of its actions where it was chosen. */
  appAction?: { usable: UsableApp; action: string };
  /** Notion's blue "New" after the name, on the items Notion marks (seen 2026-10-27). */
  newMark?: true;
  /** Notion's grey "베타" after the name ("여기에서 발표", seen 2026-10-31). */
  betaMark?: true;
}

// Notion's service embeds that draw from a link alone, each making an embed
// block named for its service (seen 2026-10-01).
const EMBED_SERVICE_DEFS: BlockDef[] = EMBED_SERVICES.map((service) => ({
  id: `embed_${service.id}`,
  type: "embed",
  label: service.name,
  description: `Embed ${service.name} from a link.`,
  glyph: "▣",
  group: "Embed",
  keywords: [...service.keywords, "embed"],
  koKeywords: ["임베드"],
  content: { service: service.id },
}));

export const BLOCK_DEFS: BlockDef[] = [
  {
    type: "paragraph",
    label: "Text",
    description: "Just start writing with plain text",
    glyph: "¶",
    group: "Basic",
    keywords: ["text"],
    koKeywords: ["텍스트"],
    placeholder: "Press ‘/’ for commands…",
  },
  {
    type: "child_page",
    label: "Page",
    description: "Embed a sub-page inside this page",
    glyph: "▣",
    group: "Basic",
    keywords: ["page"],
    koKeywords: ["페이지"],
  },
  {
    type: "link_to_page",
    label: "Link to page",
    description: "Link to an existing page",
    glyph: "↗",
    group: "Basic",
    keywords: ["page", "link", "ltp"],
    koKeywords: ["페이지", "링크", "ltp"],
  },
  {
    type: "heading_1",
    label: "Heading 1",
    description: "Big section heading",
    glyph: "H₁",
    group: "Basic",
    keywords: ["h1", "heading"],
    koKeywords: ["제목", "제목1"],
    placeholder: "Heading 1",
  },
  {
    type: "toggle_heading_1",
    label: "Toggle heading 1",
    description: "Hide content inside a large heading",
    glyph: "▸H₁",
    group: "Advanced",
    keywords: ["toggle", "heading", "h1", "th1", "expand"],
    koKeywords: ["토글", "제목"],
    placeholder: "Heading 1",
  },
  {
    type: "heading_2",
    label: "Heading 2",
    description: "Medium section heading",
    glyph: "H₂",
    group: "Basic",
    keywords: ["h2", "heading", "two", "sub heading"],
    koKeywords: ["제목", "제목2"],
    placeholder: "Heading 2",
  },
  {
    type: "toggle_heading_2",
    label: "Toggle heading 2",
    description: "Hide content inside a medium heading",
    glyph: "▸H₂",
    group: "Advanced",
    keywords: ["toggle", "heading", "h2", "th2", "expand", "two", "sub heading"],
    koKeywords: ["토글", "제목"],
    placeholder: "Heading 2",
  },
  {
    type: "heading_3",
    label: "Heading 3",
    description: "Small section heading",
    glyph: "H₃",
    group: "Basic",
    keywords: ["h3", "heading", "three", "subsub heading"],
    koKeywords: ["제목", "제목3"],
    placeholder: "Heading 3",
  },
  {
    type: "toggle_heading_3",
    label: "Toggle heading 3",
    description: "Hide content inside a small heading",
    glyph: "▸H₃",
    group: "Advanced",
    keywords: ["toggle", "heading", "h3", "th3", "expand", "three", "subsub heading"],
    koKeywords: ["토글", "제목"],
    placeholder: "Heading 3",
  },
  {
    type: "heading_4",
    label: "Heading 4",
    description: "Smaller section heading",
    glyph: "H₄",
    group: "Basic",
    keywords: ["h4", "heading", "four", "subsubsub heading"],
    koKeywords: ["제목", "제목4"],
    placeholder: "Heading 4",
  },
  {
    type: "toggle_heading_4",
    label: "Toggle heading 4",
    description: "Hide content inside a smaller heading",
    glyph: "▸H₄",
    group: "Advanced",
    keywords: ["toggle", "heading", "h4", "th4", "expand", "four", "subsubsub heading"],
    koKeywords: ["토글", "제목"],
    placeholder: "Heading 4",
    // Notion's "/" makes toggle headings 1-3 only; the fourth is a "Turn into"
    // item alone (seen 2026-10-31: "/toggle", "/heading", "/제목").
    hiddenWhenEmpty: true,
    slashHidden: true,
  },
  {
    type: "to_do",
    label: "To-do list",
    description: "Track tasks with a to-do list",
    glyph: "☑",
    group: "Basic",
    keywords: ["todo", "checkbox", "check"],
    koKeywords: ["할 일", "체크", "체크박스", "작업"],
    placeholder: "To-do",
  },
  {
    type: "bulleted_list_item",
    label: "Bulleted list",
    description: "Create a simple bulleted list",
    glyph: "•",
    group: "Basic",
    keywords: ["bullet", "list"],
    koKeywords: ["글머리", "글머리 기호", "목록"],
    placeholder: "List",
  },
  {
    type: "numbered_list_item",
    label: "Numbered list",
    description: "Create a list with numbering",
    glyph: "1.",
    group: "Basic",
    keywords: ["number", "list"],
    koKeywords: ["번호", "번호 목록", "목록"],
    placeholder: "List",
  },
  {
    type: "toggle",
    label: "Toggle list",
    description: "Toggles can hide and show content inside",
    glyph: "▸",
    group: "Basic",
    keywords: ["toggle", "collapse", "expand", "details"],
    koKeywords: ["토글", "정보"],
    placeholder: "Toggle",
  },
  {
    type: "quote",
    label: "Quote",
    description: "Capture a quote",
    glyph: "❝",
    group: "Basic",
    keywords: ["quote"],
    koKeywords: ["인용"],
    placeholder: "Empty quote",
  },
  {
    type: "callout",
    label: "Callout",
    description: "Make writing stand out",
    glyph: "💡",
    group: "Basic",
    // Not "note": Notion's "/note" lists AI 노트 and two services, no callout (seen 2026-11-11).
    keywords: ["callout"],
    koKeywords: ["콜아웃"],
    placeholder: "Type something…",
    content: { color: NEW_CALLOUT_COLOR, ...NEW_CALLOUT_ICON },
  },
  {
    type: "divider",
    label: "Divider",
    description: "Visually divide blocks",
    glyph: "—",
    group: "Basic",
    keywords: ["divider", "separator", "line", "hr", "section", "horizontal rule"],
    koKeywords: ["구분선", "선", "섹션"],
  },
  {
    type: "code",
    label: "Code",
    description: "Capture a code snippet",
    glyph: "</>",
    group: "Media",
    keywords: ["code"],
    koKeywords: ["코드", "미디어"],
  },
  {
    type: "equation",
    label: "Block equation",
    description: "Display a standalone math equation",
    glyph: "∑",
    group: "Advanced",
    keywords: ["equation", "block equation", "math", "latex", "formula"],
    koKeywords: ["수식", "수학", "공식"],
  },
  {
    type: "table_of_contents",
    label: "Table of contents",
    description: "Show an outline of your page",
    glyph: "☰",
    group: "Advanced",
    // Not "headings": Notion's "/heading" doesn't list it (seen 2026-10-31).
    keywords: ["toc", "contents", "table"],
    koKeywords: ["목차"],
  },
  {
    type: "synced_block",
    label: "Synced block",
    description: "Sync content across pages",
    glyph: "↔",
    group: "Advanced",
    // Notion's "/embed" lists it first (seen 2026-10-31), "/reuse" nothing (2026-11-11).
    keywords: ["synced", "sync", "embed", "reference", "portal", "group"],
    koKeywords: ["동기화", "임베드"],
  },
  {
    type: "button",
    label: "Button",
    description: "Run custom automations with a click",
    glyph: "▣",
    group: "Advanced",
    // Notion's "/duplicate" and "/lica" list it before 복제 (seen 2026-11-11).
    keywords: ["button", "automation", "template", "duplicate", "btn"],
    koKeywords: ["버튼", "템플릿", "복제"],
  },
  {
    type: "tab",
    label: "Tabs",
    description: "Organize content in tabs",
    glyph: "▤",
    group: "Advanced",
    // Notion's "/toggle" and "/토글" list it, "/sec", "/orga" and "/cont" don't (seen 2026-10-31, 2026-11-15).
    keywords: ["tab", "tabs", "tabbed", "toggle", "switch"],
    koKeywords: ["탭", "토글", "위치"],
    placeholder: "Tabs",
    newMark: true,
  },
  {
    type: "breadcrumb",
    label: "Breadcrumb",
    description: "Show the current page location",
    glyph: "›",
    group: "Advanced",
    keywords: ["breadcrumb"],
    koKeywords: ["이동 경로", "경로"],
  },
  {
    type: "table",
    label: "Table",
    description: "Add a structured table to this page.",
    glyph: "▤",
    group: "Basic",
    keywords: ["table"],
    koKeywords: ["표"],
  },
  {
    id: "column_list_2",
    type: "column_list",
    label: "2 columns",
    description: "Create 2 columns of blocks",
    glyph: "▥▥",
    group: "Advanced",
    keywords: ["column", "columns", "vertical", "two"],
    koKeywords: ["열", "세로", "변환"],
    columnCount: 2,
  },
  {
    id: "column_list_3",
    type: "column_list",
    label: "3 columns",
    description: "Create 3 columns of blocks",
    glyph: "▥▥▥",
    group: "Advanced",
    keywords: ["column", "columns", "three"],
    koKeywords: ["열", "세로", "변환"],
    columnCount: 3,
  },
  {
    id: "column_list_4",
    type: "column_list",
    label: "4 columns",
    description: "Create 4 columns of blocks",
    glyph: "▥▥▥▥",
    group: "Advanced",
    keywords: ["column", "columns", "four"],
    koKeywords: ["열", "세로", "변환"],
    columnCount: 4,
  },
  {
    id: "column_list_5",
    type: "column_list",
    label: "5 columns",
    description: "Create 5 columns of blocks",
    glyph: "▥▥▥▥▥",
    group: "Advanced",
    keywords: ["column", "columns", "five"],
    koKeywords: ["열", "세로", "변환"],
    columnCount: 5,
  },
  {
    id: "code_mermaid",
    type: "code",
    label: "Code - Mermaid",
    description: "Create a diagram by writing code",
    glyph: "</>",
    group: "Advanced",
    keywords: ["mermaid", "diagram", "flowchart", "code", "graphviz"],
    koKeywords: ["머메이드", "mermaid", "다이어그램", "graphviz", "그래프", "문서", "순서", "코드"],
    content: { language: "mermaid" },
    // Made with a diagram to change, the code chosen, as Notion's (seen 2026-11-03).
    sampleText: "graph TD\n  Mermaid --> Diagram",
  },
  {
    type: "image",
    label: "Image",
    description: "Upload or embed with a link",
    glyph: "▧",
    group: "Media",
    // Not "embed": Notion's "/embed" doesn't list it (seen 2026-10-31).
    keywords: ["image", "img", "photo", "picture", "media", "gif", "giphy", "jpg"],
    koKeywords: ["이미지", "사진", "gif", "giphy", "jpg", "미디어"],
  },
  {
    type: "video",
    label: "Video",
    description: "Embed from YouTube, Vimeo…",
    glyph: "▶",
    group: "Media",
    keywords: ["video", "media"],
    koKeywords: ["동영상", "영상", "미디어"],
  },
  {
    type: "audio",
    label: "Audio",
    description: "Embed from SoundCloud, Spotify…",
    glyph: "♫",
    group: "Media",
    keywords: ["audio", "sound", "music", "media"],
    koKeywords: ["오디오", "음악", "미디어"],
  },
  {
    type: "bookmark",
    label: "Web bookmark",
    description: "Save a link as a visual bookmark",
    glyph: "🔖",
    group: "Media",
    keywords: ["bookmark", "link", "web"],
    koKeywords: ["북마크", "링크", "웹", "미디어"],
  },
  {
    type: "embed",
    label: "Embed",
    description: "For PDFs, Google Maps, and more",
    glyph: "▣",
    group: "Embed",
    // Not "website": Notion's "/website" lists HTML alone (seen 2026-11-11).
    keywords: ["embed", "iframe"],
    koKeywords: ["임베드"],
  },
  // Notion's "HTML" after "임베드" (seen 2026-10-07): an uploaded HTML file
  // in a frame with no origin; its AI generation is outside "no AI in the
  // editor". A build without the app platform has no item.
  {
    id: "embed_html",
    type: "embed",
    label: "HTML",
    description: "Upload or link an HTML file",
    glyph: "▣",
    group: "Embed",
    // Notion lists it for "/website", "/web page", "/웹 페이지", "/web pa",
    // "/page", "/age" and "/페이지" (last of 7) but not "/webpage" or "/웹페"
    // (seen 2026-11-11; "/페이지" didn't list it 2026-11-05 and 11-07).
    keywords: ["html", "artifact", "embed", "website", "web page", "htm"],
    koKeywords: ["HTML", "임베드", "웹사이트", "웹 페이지"],
    content: { service: "html" },
    appPlatform: true,
    newMark: true,
  },
  ...EMBED_SERVICE_DEFS,
  {
    type: "pdf",
    label: "PDF",
    description: "Embed a PDF",
    glyph: "▤",
    group: "Embed",
    keywords: ["pdf", "embed", "preview"],
    koKeywords: ["PDF", "임베드"],
  },
  {
    type: "file",
    label: "File",
    description: "Upload or embed with a link",
    glyph: "▭",
    group: "Media",
    keywords: ["file", "upload", "attach"],
    koKeywords: ["파일", "첨부", "업로드", "비디오", "미디어"],
  },
  ...DATABASE_SLASH_DEFS,
  ...INLINE_SLASH_DEFS,
  ...SLASH_COMMAND_DEFS,
];

export function blockDefKey(def: Pick<BlockDef, "id" | "type">) {
  // Keep the existing translation key while the persisted graph authority moves
  // from the legacy atomic simple_table type to the native table/table_row pair.
  if (!def.id && def.type === "table") return "simple_table";
  return def.id ?? def.type;
}
