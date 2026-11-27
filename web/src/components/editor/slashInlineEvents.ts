import type { InlineSlashKind } from "./blockDefinitions";

/** Starts an inline item in the new line created by a block's plus menu. */
export const SLASH_INLINE_REQUEST = "leafen:slash-inline";
export type SlashInlineRequest = { blockId: string; kind: InlineSlashKind };
