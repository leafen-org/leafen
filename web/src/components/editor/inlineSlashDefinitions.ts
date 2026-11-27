import type { BlockDef, InlineSlashKind } from "./blockDefinitions";

// Notion's slash menu ends its block groups with "Inline" (seen 2026-09-30):
// mention a person, mention a page, a date or reminder, an emoji and an
// inline equation. Each goes into the line where "/" was typed instead of
// making a block: the mentions open the @ menu for their kind, the emoji
// its picker and the equation its field.
const INLINE_ITEMS: ReadonlyArray<{
  inline: InlineSlashKind;
  label: string;
  description: string;
  keywords: string[];
  koKeywords: string[];
}> = [
  {
    inline: "person",
    label: "Mention a person",
    description: "Ping someone so they get a notification",
    keywords: ["mention", "person"],
    koKeywords: ["멘션", "사용자"],
  },
  {
    inline: "page",
    label: "Mention a page",
    description: "Mention a page or data source, and place in text",
    keywords: ["mention", "data source"],
    koKeywords: ["멘션", "페이지"],
  },
  {
    inline: "date",
    label: "Date or reminder",
    description: "Insert a date or reminder in text",
    keywords: ["date", "reminder", "remind"],
    koKeywords: ["날짜", "리마인더"],
  },
  {
    inline: "emoji",
    label: "Emoji",
    description: "Search for an emoji to place in text",
    keywords: ["emoji"],
    koKeywords: ["이모지"],
  },
  {
    inline: "equation",
    label: "Inline equation",
    description: "Insert mathematical symbols in text",
    keywords: ["inline", "equation", "math", "latex"],
    koKeywords: ["인라인", "수학", "공식", "라텍스"],
  },
];

export const INLINE_SLASH_DEFS: BlockDef[] = INLINE_ITEMS.map(({ inline, ...item }) => ({
  ...item,
  id: `inline_${inline}`,
  type: "paragraph",
  glyph: "@",
  group: "Inline",
  inline,
}));
