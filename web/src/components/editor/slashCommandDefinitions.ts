import type { BlockType } from "@/lib/types";
import type { BlockDef, SlashAction } from "./blockDefinitions";
import { BLOCK_HANDLE_BACKGROUND_COLORS, BLOCK_HANDLE_COLORS } from "./blockHandleModel";
import { blockTurnChoices } from "./blockTurnInto";

// Notion's slash menu ends with what acts on the line where "/" was typed
// (seen 2026-09-30): "Turn into" the block types the block menu turns a
// block into, "Actions" (copy link to block, duplicate, move to, delete),
// then the text colors and the background colors. Named, a "Turn into"
// item reads "Heading 1 · Turn into" beside "Heading 1", which makes a block.
// Its "Turn into" has "페이지 위치:" after "페이지", with a ">" — chosen, the
// pages to put the block in open beside the menu, as from the block menu
// (seen 2026-10-25; on 10-01 it wasn't there).

const TURN_INTO_DEFS: BlockDef[] = blockTurnChoices(1).map(({ key, type, columns, placeIn }) => ({
  id: `turn_${key}`,
  type: type as BlockType,
  ...(columns ? { columnCount: columns } : {}),
  ...(placeIn ? { pageIn: true as const } : {}),
  // The label and keywords are the block type's own (blockDefLabel).
  label: "",
  description: "",
  glyph: "↪",
  group: "TurnInto",
  keywords: ["turn into", "turn"],
  koKeywords: ["전환"],
  action: "turn_into",
}));

const ACTION_ITEMS: ReadonlyArray<{ action: SlashAction; label: string; description: string; keywords: string[]; koKeywords: string[] }> = [
  {
    action: "copy_link",
    label: "Copy link to block",
    description: "",
    keywords: ["copy link", "link", "block link"],
    koKeywords: ["블록 링크 복사", "링크", "복사"],
  },
  {
    action: "duplicate",
    label: "Duplicate",
    description: "",
    keywords: ["duplicate"],
    koKeywords: ["복제"],
  },
  {
    action: "move_to",
    label: "Move to",
    description: "",
    keywords: ["move", "move to"],
    koKeywords: ["옮기기"],
  },
  {
    action: "delete",
    label: "Delete",
    description: "",
    keywords: ["delete"],
    koKeywords: ["삭제"],
  },
  // "여기에서 발표 [베타] ⌘⌥P", as the block menu's (Notion "/발표", "/표", seen 2026-10-31).
  {
    action: "present",
    label: "Present here",
    description: "",
    keywords: ["present", "present from here"],
    koKeywords: ["발표"],
  },
];

const ACTION_DEFS: BlockDef[] = ACTION_ITEMS.map(({ action, ...item }) => ({
  ...item,
  ...(action === "present" ? { betaMark: true as const } : {}),
  id: `action_${action}`,
  type: "paragraph",
  glyph: "⋯",
  group: "Actions",
  action,
}));

const COLOR_NAMES: Record<string, string> = {
  default: "Default", gray: "Gray", brown: "Brown", orange: "Orange", yellow: "Yellow",
  green: "Green", blue: "Blue", purple: "Purple", pink: "Pink", red: "Red",
};
const COLOR_KO_NAMES: Record<string, string> = {
  default: "기본", gray: "회색", brown: "갈색", orange: "주황색", yellow: "노란색",
  green: "초록색", blue: "파란색", purple: "보라색", pink: "분홍색", red: "빨간색",
};

// A block has one color: a text color or a background, so "Default text"
// and "Default background" both clear it.
function colorDef(name: string, background: boolean): BlockDef {
  const token = background ? `${name}_background` : name;
  const english = COLOR_NAMES[name];
  const korean = COLOR_KO_NAMES[name];
  // The default holds "text" and "background" only inside a word: Notion's
  // "/te" and "/ba" list the other colors there and the default past 25,
  // "/ext" and "/tb" find it (seen 2026-11-16).
  const plain = name === "default";
  return {
    id: `color_${token}`,
    type: "paragraph",
    label: plain ? english : `${english} ${background ? "background" : "text"}`,
    // Notion's color rows show no tip (seen 2026-11-01).
    description: "",
    glyph: "A",
    group: background ? "BackgroundColor" : "TextColor",
    // Green is also "teal": Notion's "/teal" lists 초록색 텍스트 and 초록색 배경 (seen 2026-11-11).
    // A background is found by its name and "background" run together
    // ("/db" 빨간색 배경, "/nb" 초록색·갈색 배경), the default by "black" and
    // "white", gray by "grey" — and none by "colour" or "highlight" (Notion's
    // two-letter queries, seen 2026-11-15).
    keywords: ["color", english.toLowerCase(), ...(name === "green" ? ["teal"] : []), ...(name === "gray" ? ["grey"] : []),
      ...(plain ? ["black", "white", background ? "defaultbackground" : "default-text"] : [background ? "background" : "text color"]),
      ...(background && !plain ? [`${name}background`, ...(name === "green" ? ["tealbackground"] : [])] : [])],
    koKeywords: background ? ["색", "색상", korean, "배경"] : ["색", "색상", korean],
    action: "set_color",
    colorToken: name === "default" ? "default" : token,
  };
}

const TEXT_COLOR_DEFS = BLOCK_HANDLE_COLORS.map((color) => colorDef(color, false));
const BACKGROUND_COLOR_DEFS = ["default", ...BLOCK_HANDLE_BACKGROUND_COLORS.map((color) => color.slice(0, -"_background".length))]
  .map((color) => colorDef(color, true));

// "가져오기", as Notion's (seen 2026-10-01, searched 2026-10-12): the file
// formats Leafen reads, in Notion's order; the services Notion imports from an
// account are left out. Each opens the import for this page.
const IMPORT_ITEMS: ReadonlyArray<{ format: NonNullable<BlockDef["importFormat"]>; label: string; keywords: string[]; koKeywords: string[] }> = [
  { format: "csv", label: "CSV", keywords: ["import", "csv"], koKeywords: ["가져오기", "csv"] },
  { format: "markdown", label: "Text & Markdown", keywords: ["import", "text", "markdown"], koKeywords: ["가져오기", "텍스트"] },
  { format: "word", label: "Word", keywords: ["import", "word"], koKeywords: ["가져오기"] },
  { format: "zip", label: "ZIP", keywords: ["import", "zip", "universal", "generic"], koKeywords: ["가져오기"] },
  { format: "pdf", label: "PDF", keywords: ["import", "pdf"], koKeywords: ["가져오기", "pdf"] },
];

const IMPORT_DEFS: BlockDef[] = IMPORT_ITEMS.map(({ format, ...item }) => ({
  ...item,
  id: `import_${format}`,
  type: "paragraph",
  description: "Import a file into this page.",
  glyph: "↓",
  group: "Import",
  action: "import",
  importFormat: format,
}));

export const SLASH_COMMAND_DEFS: BlockDef[] = [
  ...IMPORT_DEFS,
  ...TURN_INTO_DEFS,
  ...ACTION_DEFS,
  ...TEXT_COLOR_DEFS,
  ...BACKGROUND_COLOR_DEFS,
];
