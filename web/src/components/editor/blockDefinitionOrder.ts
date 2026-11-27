import { blockSpec, isBlockType } from "@/lib/blockSemantics";
import type { BlockType } from "@/lib/types";
import { BLOCK_DEFS, blockDefKey, type BlockDef } from "./blockDefinitions";

// Notion's slash menu order (seen 2026-09-30), group by group. The items
// that act on the line (slashCommandDefinitions) keep their own order after
// these.
const SLASH_ORDER_KEYS = [
  // Basic blocks
  "paragraph",
  "heading_1",
  "heading_2",
  "heading_3",
  "heading_4",
  "bulleted_list_item",
  "numbered_list_item",
  "to_do",
  "toggle",
  "child_page",
  "callout",
  "quote",
  "simple_table",
  "divider",
  "link_to_page",
  // Media
  "image",
  "video",
  "audio",
  "code",
  "file",
  "bookmark",
  // Database
  "inline_database_table",
  "inline_database_board",
  "inline_database_gallery",
  "inline_database_list",
  "inline_database_feed",
  "inline_database_dashboard",
  "inline_database_calendar",
  "inline_database_timeline",
  "inline_database_map",
  "chart_bar",
  "chart_horizontal_bar",
  "chart_line",
  "chart_donut",
  "chart_number",
  "inline_database_form",
  "inline_database",
  "child_database",
  "inline_database_linked",
  // Advanced blocks
  "table_of_contents",
  "equation",
  "button",
  "breadcrumb",
  "tab",
  "synced_block",
  "toggle_heading_1",
  "toggle_heading_2",
  "toggle_heading_3",
  "toggle_heading_4",
  "column_list_2",
  "column_list_3",
  "column_list_4",
  "column_list_5",
  "code_mermaid",
  // Inline
  "inline_person",
  "inline_page",
  "inline_date",
  "inline_emoji",
  "inline_equation",
  // Embeds, with Notion's link-drawn services in its order (seen 2026-10-01)
  "embed",
  "embed_html",
  "embed_google_drive",
  "embed_google_maps",
  "embed_figma",
  "embed_miro",
  "embed_excalidraw",
  "pdf",
  "embed_loom",
  "embed_typeform",
  "embed_codepen",
  "embed_replit",
] as const;

const SLASH_ORDER = new Map<string, number>(
  SLASH_ORDER_KEYS.map((key, index) => [key, index])
);

export function slashOrder(def: BlockDef, fallback: number) {
  return SLASH_ORDER.get(blockDefKey(def)) ?? SLASH_ORDER_KEYS.length + fallback;
}

const DEF_MAP: Record<string, BlockDef> = {};
for (const def of BLOCK_DEFS) {
  DEF_MAP[def.type] ??= def;
}

export function getDef(type: BlockType): BlockDef {
  if (!isBlockType(type)) return DEF_MAP["paragraph"];
  const semantics = blockSpec(type);
  return DEF_MAP[semantics.definitionType] ?? DEF_MAP["paragraph"];
}
