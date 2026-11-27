import type { BlockType } from "./types";

export type BlockTurnAction =
  | "change_type"
  | "create_child_page"
  | "create_page_link"
  | "create_database"
  | "create_inline_database"
  | "create_columns"
  | "create_simple_table"
  | "create_equation"
  | "create_synced_block"
  | "create_button"
  | "create_tab";

export interface BlockSpec {
  definitionType: BlockType;
  editableText: boolean;
  toggle: boolean;
  structural: boolean;
  canContainChildren: boolean;
  emptyEnterExitsToParagraph: boolean;
  continuesOnEnter: boolean;
  typeChangeContent: "preserve_text" | "clear";
  newContent: "rich" | "javascript_code";
  clone: "preserve";
  clipboard: "preserve_internal" | "reject";
  canonicalProjection: boolean;
  canonicalCreatorTarget: boolean;
  turnIntoOrder: number | null;
  blockSelectionTurnIntoOrder: number | null;
  textSelectionTurnIntoOrder: number | null;
  turnAction: BlockTurnAction;
}

type BlockBehaviorProfile = Pick<
  BlockSpec,
  | "editableText"
  | "toggle"
  | "structural"
  | "canContainChildren"
  | "emptyEnterExitsToParagraph"
  | "continuesOnEnter"
  | "typeChangeContent"
  | "newContent"
  | "clone"
  | "clipboard"
  | "canonicalProjection"
  | "canonicalCreatorTarget"
>;

const COMMON = {
  clone: "preserve",
  clipboard: "preserve_internal",
  canonicalProjection: false,
  canonicalCreatorTarget: false,
} as const;

const PROFILES = {
  paragraph: {
    ...COMMON,
    editableText: true,
    toggle: false,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  emptyExitText: {
    ...COMMON,
    editableText: true,
    toggle: false,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: true,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  // A heading takes no blocks in it, as Notion's: Tab under one does nothing
  // (2026-10-16: the paragraph went in and took its size). A toggle heading does.
  heading: {
    ...COMMON,
    editableText: true,
    toggle: false,
    structural: false,
    canContainChildren: false,
    emptyEnterExitsToParagraph: true,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  toggleHeading: {
    ...COMMON,
    editableText: true,
    toggle: true,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  list: {
    ...COMMON,
    editableText: true,
    toggle: false,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: true,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  toggle: {
    ...COMMON,
    editableText: true,
    toggle: true,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  code: {
    ...COMMON,
    editableText: true,
    toggle: false,
    structural: false,
    canContainChildren: false,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "javascript_code",
  },
  atomicClear: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: false,
    canContainChildren: false,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "clear",
    newContent: "rich",
  },
  atomicPreserve: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: false,
    canContainChildren: false,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
  },
  tab: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "clear",
    newContent: "rich",
  },
  legacyTemplate: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: false,
    canContainChildren: true,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "clear",
    newContent: "rich",
  },
  structural: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: true,
    canContainChildren: false,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "clear",
    newContent: "rich",
  },
  canonicalChild: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: false,
    canContainChildren: false,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "preserve_text",
    newContent: "rich",
    canonicalProjection: true,
    canonicalCreatorTarget: true,
  },
  canonicalDatabaseTarget: {
    ...COMMON,
    editableText: false,
    toggle: false,
    structural: false,
    canContainChildren: false,
    emptyEnterExitsToParagraph: false,
    continuesOnEnter: false,
    typeChangeContent: "clear",
    newContent: "rich",
    canonicalCreatorTarget: true,
  },
} as const satisfies Record<string, BlockBehaviorProfile>;

interface BlockSpecAssignment {
  profile: keyof typeof PROFILES;
  definitionType: BlockType;
  turnIntoOrder: number | null;
  blockSelectionTurnIntoOrder: number | null;
  // The text selection bar's turn menu, in the order of Notion's (seen
  // 2026-10-31): text, headings, lists, to-do, toggle, code, quote, callout.
  textSelectionTurnIntoOrder: number | null;
  turnAction: BlockTurnAction;
}

const ASSIGNMENTS = {
  paragraph: { profile: "paragraph", definitionType: "paragraph", turnIntoOrder: 0, blockSelectionTurnIntoOrder: 0, textSelectionTurnIntoOrder: 0, turnAction: "change_type" },
  heading_1: { profile: "heading", definitionType: "heading_1", turnIntoOrder: 3, blockSelectionTurnIntoOrder: 1, textSelectionTurnIntoOrder: 1, turnAction: "change_type" },
  heading_2: { profile: "heading", definitionType: "heading_2", turnIntoOrder: 5, blockSelectionTurnIntoOrder: 3, textSelectionTurnIntoOrder: 2, turnAction: "change_type" },
  heading_3: { profile: "heading", definitionType: "heading_3", turnIntoOrder: 7, blockSelectionTurnIntoOrder: 5, textSelectionTurnIntoOrder: 3, turnAction: "change_type" },
  heading_4: { profile: "heading", definitionType: "heading_4", turnIntoOrder: 9, blockSelectionTurnIntoOrder: 7, textSelectionTurnIntoOrder: 4, turnAction: "change_type" },
  toggle_heading_1: { profile: "toggleHeading", definitionType: "toggle_heading_1", turnIntoOrder: 4, blockSelectionTurnIntoOrder: 2, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  toggle_heading_2: { profile: "toggleHeading", definitionType: "toggle_heading_2", turnIntoOrder: 6, blockSelectionTurnIntoOrder: 4, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  toggle_heading_3: { profile: "toggleHeading", definitionType: "toggle_heading_3", turnIntoOrder: 8, blockSelectionTurnIntoOrder: 6, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  toggle_heading_4: { profile: "toggleHeading", definitionType: "toggle_heading_4", turnIntoOrder: 10, blockSelectionTurnIntoOrder: 8, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  bulleted_list_item: { profile: "list", definitionType: "bulleted_list_item", turnIntoOrder: 12, blockSelectionTurnIntoOrder: 10, textSelectionTurnIntoOrder: 5, turnAction: "change_type" },
  numbered_list_item: { profile: "list", definitionType: "numbered_list_item", turnIntoOrder: 13, blockSelectionTurnIntoOrder: 11, textSelectionTurnIntoOrder: 6, turnAction: "change_type" },
  to_do: { profile: "list", definitionType: "to_do", turnIntoOrder: 11, blockSelectionTurnIntoOrder: 9, textSelectionTurnIntoOrder: 7, turnAction: "change_type" },
  toggle: { profile: "toggle", definitionType: "toggle", turnIntoOrder: 14, blockSelectionTurnIntoOrder: 12, textSelectionTurnIntoOrder: 8, turnAction: "change_type" },
  quote: { profile: "emptyExitText", definitionType: "quote", turnIntoOrder: 15, blockSelectionTurnIntoOrder: 13, textSelectionTurnIntoOrder: 10, turnAction: "change_type" },
  callout: { profile: "emptyExitText", definitionType: "callout", turnIntoOrder: 16, blockSelectionTurnIntoOrder: 14, textSelectionTurnIntoOrder: 11, turnAction: "change_type" },
  divider: { profile: "atomicClear", definitionType: "divider", turnIntoOrder: null, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  code: { profile: "code", definitionType: "code", turnIntoOrder: 17, blockSelectionTurnIntoOrder: 15, textSelectionTurnIntoOrder: 9, turnAction: "change_type" },
  equation: { profile: "atomicClear", definitionType: "equation", turnIntoOrder: 18, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_equation" },
  table: { profile: "tab", definitionType: "table", turnIntoOrder: 23, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_simple_table" },
  table_row: { profile: "structural", definitionType: "table", turnIntoOrder: null, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  image: { profile: "atomicPreserve", definitionType: "image", turnIntoOrder: 26, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  video: { profile: "atomicClear", definitionType: "video", turnIntoOrder: 27, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  audio: { profile: "atomicClear", definitionType: "audio", turnIntoOrder: 28, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  bookmark: { profile: "atomicPreserve", definitionType: "bookmark", turnIntoOrder: 29, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  link_preview: { profile: "atomicPreserve", definitionType: "bookmark", turnIntoOrder: null, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  embed: { profile: "atomicClear", definitionType: "embed", turnIntoOrder: 30, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  pdf: { profile: "atomicClear", definitionType: "pdf", turnIntoOrder: 31, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  file: { profile: "atomicClear", definitionType: "file", turnIntoOrder: 32, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  breadcrumb: { profile: "atomicClear", definitionType: "breadcrumb", turnIntoOrder: 19, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  table_of_contents: { profile: "atomicClear", definitionType: "table_of_contents", turnIntoOrder: 24, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  synced_block: { profile: "atomicClear", definitionType: "synced_block", turnIntoOrder: 20, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_synced_block" },
  button: { profile: "atomicClear", definitionType: "button", turnIntoOrder: 21, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_button" },
  template: { profile: "legacyTemplate", definitionType: "button", turnIntoOrder: null, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  tab: { profile: "tab", definitionType: "tab", turnIntoOrder: 22, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_tab" },
  inline_database: { profile: "canonicalDatabaseTarget", definitionType: "inline_database", turnIntoOrder: 34, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_inline_database" },
  column_list: { profile: "structural", definitionType: "column_list", turnIntoOrder: 25, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_columns" },
  column: { profile: "structural", definitionType: "column_list", turnIntoOrder: null, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "change_type" },
  child_page: { profile: "canonicalChild", definitionType: "child_page", turnIntoOrder: 1, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_child_page" },
  link_to_page: { profile: "atomicClear", definitionType: "link_to_page", turnIntoOrder: 2, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_page_link" },
  child_database: { profile: "canonicalChild", definitionType: "child_database", turnIntoOrder: 33, blockSelectionTurnIntoOrder: null, textSelectionTurnIntoOrder: null, turnAction: "create_database" },
} as const satisfies Record<BlockType, BlockSpecAssignment>;

const BLOCK_TYPES = Object.freeze(Object.keys(ASSIGNMENTS) as BlockType[]);

const BLOCK_SPECS = Object.freeze(Object.fromEntries(
  BLOCK_TYPES.map((type) => {
    const assignment = ASSIGNMENTS[type];
    return [type, {
      ...PROFILES[assignment.profile],
      definitionType: assignment.definitionType,
      turnIntoOrder: assignment.turnIntoOrder,
      blockSelectionTurnIntoOrder: assignment.blockSelectionTurnIntoOrder,
      textSelectionTurnIntoOrder: assignment.textSelectionTurnIntoOrder,
      turnAction: assignment.turnAction,
    } satisfies BlockSpec];
  }),
)) as Readonly<Record<BlockType, BlockSpec>>;

export function blockSpec(type: BlockType): BlockSpec {
  return BLOCK_SPECS[type];
}

export function isBlockType(value: unknown): value is BlockType {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(BLOCK_SPECS, value);
}

function orderedTypes(
  order: "turnIntoOrder" | "blockSelectionTurnIntoOrder" | "textSelectionTurnIntoOrder",
) {
  return Object.freeze(BLOCK_TYPES
    .filter((type) => BLOCK_SPECS[type][order] !== null)
    .sort((left, right) => (
      (BLOCK_SPECS[left][order] ?? Number.MAX_SAFE_INTEGER) -
      (BLOCK_SPECS[right][order] ?? Number.MAX_SAFE_INTEGER)
    )));
}

export type TextSelectionTurnIntoBlockType = {
  [Type in BlockType]: typeof ASSIGNMENTS[Type]["textSelectionTurnIntoOrder"] extends number
    ? Type
    : never;
}[BlockType];
export const TEXT_SELECTION_TURN_INTO_BLOCK_TYPES = orderedTypes(
  "textSelectionTurnIntoOrder",
) as readonly TextSelectionTurnIntoBlockType[];

export const TEXT_BLOCK_TYPES: ReadonlySet<BlockType> = new Set(
  BLOCK_TYPES.filter((type) => BLOCK_SPECS[type].editableText),
);
export const TOGGLE_BLOCK_TYPES: ReadonlySet<BlockType> = new Set(
  BLOCK_TYPES.filter((type) => BLOCK_SPECS[type].toggle),
);
export const STRUCTURAL_BLOCK_TYPES: ReadonlySet<BlockType> = new Set(
  BLOCK_TYPES.filter((type) => BLOCK_SPECS[type].structural),
);
export const CONTAINER_BLOCK_TYPES: ReadonlySet<BlockType> = new Set(
  BLOCK_TYPES.filter((type) => BLOCK_SPECS[type].canContainChildren),
);
/**
 * Blocks Tab puts the block after them into, as their last child: any that
 * holds blocks but a table, whose children are its rows. Tab after a table
 * does nothing, a line's or a chosen block's, as Notion's (seen 2026-11-02
 * and 2026-11-14: a chosen picture after one sent a change the server
 * refused, with a red notice).
 */
export const TAB_INTO_BLOCK_TYPES: ReadonlySet<BlockType> = new Set(
  BLOCK_TYPES.filter((type) => BLOCK_SPECS[type].canContainChildren && type !== "table"),
);
/**
 * Blocks that take blocks inside even before anything is in them: a list
 * item, to-do, toggle (an open one), quote or callout, as Notion's do for a
 * drop (seen 2026-11-07) and for Cmd/Ctrl+Shift+Arrow (seen 2026-11-08). A
 * line of text takes blocks only once it holds some.
 */
export const TAKES_FIRST_CHILD_BLOCK_TYPES: ReadonlySet<BlockType> = new Set<BlockType>([
  "bulleted_list_item", "numbered_list_item", "to_do", "quote", "callout", ...TOGGLE_BLOCK_TYPES,
]);

/**
 * Whether a block of this type is kept out of synced blocks where a person
 * places it, as Notion's is (seen 2026-11-08): dragged, a synced block finds
 * no place anywhere inside one, ⌘⇧↑ leaves it beside one, and inside one "/"
 * and "전환" offer none. Notion's ⌘⇧↓, Tab and paste still put a synced block
 * in one, as its API does, so one can stand in another: the rule is how a
 * person places it, not what a synced block holds. A block holding a synced
 * block goes in by any way (syncedBlockKeepingOut).
 */
export function keptOutOfSyncedBlocks(type: string) {
  return type === "synced_block";
}
