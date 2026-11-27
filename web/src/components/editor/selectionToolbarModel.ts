import { TEXT_SELECTION_TURN_INTO_BLOCK_TYPES, type TextSelectionTurnIntoBlockType } from "@/lib/blockSemantics";

export type SelectionToolbarMark =
  | "bold"
  | "italic"
  | "underline"
  | "strikethrough"
  | "code"
  | "link";

export type SelectionToolbarActiveMarks = Record<SelectionToolbarMark, boolean>;

export type SelectionToolbarActiveColor = string | "mixed";

export const INACTIVE_MARKS: SelectionToolbarActiveMarks = {
  bold: false,
  italic: false,
  underline: false,
  strikethrough: false,
  code: false,
  link: false,
};

const TURN_OPTION_PRESENTATION = {
  paragraph: { label: "selectionToolbar:turnInto.options.text", glyph: "¶" },
  heading_1: { label: "selectionToolbar:turnInto.options.heading1", glyph: "H1" },
  heading_2: { label: "selectionToolbar:turnInto.options.heading2", glyph: "H2" },
  heading_3: { label: "selectionToolbar:turnInto.options.heading3", glyph: "H3" },
  heading_4: { label: "selectionToolbar:turnInto.options.heading4", glyph: "H4" },
  to_do: { label: "selectionToolbar:turnInto.options.toDoList", glyph: "☑" },
  bulleted_list_item: { label: "selectionToolbar:turnInto.options.bulletedList", glyph: "•" },
  numbered_list_item: { label: "selectionToolbar:turnInto.options.numberedList", glyph: "1." },
  toggle: { label: "selectionToolbar:turnInto.options.toggleList", glyph: "▸" },
  quote: { label: "selectionToolbar:turnInto.options.quote", glyph: "❝" },
  callout: { label: "selectionToolbar:turnInto.options.callout", glyph: "💡" },
  code: { label: "selectionToolbar:turnInto.options.code", glyph: "</>" },
} as const satisfies Record<TextSelectionTurnIntoBlockType, { label: string; glyph: string }>;

export const TURN_OPTIONS = TEXT_SELECTION_TURN_INTO_BLOCK_TYPES.map((type) => ({
  type,
  ...TURN_OPTION_PRESENTATION[type],
}));

export const TEXT_COLORS = [
  { token: "default", label: "selectionToolbar:colors.text.default" },
  { token: "gray", label: "selectionToolbar:colors.text.gray" },
  { token: "brown", label: "selectionToolbar:colors.text.brown" },
  { token: "orange", label: "selectionToolbar:colors.text.orange" },
  { token: "yellow", label: "selectionToolbar:colors.text.yellow" },
  { token: "green", label: "selectionToolbar:colors.text.green" },
  { token: "blue", label: "selectionToolbar:colors.text.blue" },
  { token: "purple", label: "selectionToolbar:colors.text.purple" },
  { token: "pink", label: "selectionToolbar:colors.text.pink" },
  { token: "red", label: "selectionToolbar:colors.text.red" },
] as const;

// The background colors begin with taking the background off, as Notion's
// selection menu does (seen 2026-10-08).
export const BACKGROUND_COLORS = [
  { token: "default_background", label: "selectionToolbar:colors.background.default" },
  { token: "gray_background", label: "selectionToolbar:colors.background.gray" },
  { token: "brown_background", label: "selectionToolbar:colors.background.brown" },
  { token: "orange_background", label: "selectionToolbar:colors.background.orange" },
  { token: "yellow_background", label: "selectionToolbar:colors.background.yellow" },
  { token: "green_background", label: "selectionToolbar:colors.background.green" },
  { token: "blue_background", label: "selectionToolbar:colors.background.blue" },
  { token: "purple_background", label: "selectionToolbar:colors.background.purple" },
  { token: "pink_background", label: "selectionToolbar:colors.background.pink" },
  { token: "red_background", label: "selectionToolbar:colors.background.red" },
] as const;

export type SelectionColorChoice = { token: string; label: string };

/**
 * What choosing a color does to the selected text, which holds a text color
 * and a background together, as Notion's (seen 2026-10-08): a color sets its
 * kind; "default" takes the text color off and "default_background" the
 * background (a null value), each leaving the other kind alone.
 */
export function selectionColorChange(token: string): { kind: "color" | "background"; value: string | null } {
  const kind = token.endsWith("_background") ? "background" : "color";
  return { kind, value: token === "default" || token === "default_background" ? null : token };
}
