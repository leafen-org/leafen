import type { BlockDef } from "./blocks";
import { requestEditorBlockCommand } from "./editorCommands";

// The text blocks Notion's "/" makes while suggesting: "기본 블록" is 텍스트,
// 제목1–4, 글머리 기호 목록, 번호 매기기 목록 and 할 일 목록 alone, and "작업" is
// 블록 링크 복사, 삭제 and 여기에서 발표 (seen 2026-11-18 in Chrome at 1440, a
// page and a database page's side peek; no "전환", colors, media, databases,
// inline rows or imports). Its "추천" is the plain menu's rule over these rows
// (slashSuggestions): the two picked most of late. "삭제" suggests the line's block away — its
// words struck, "삭제: “…”", the block gone once accepted (seen 2026-11-18 in
// a page and a database page's side peek; page_suggestion_removals.go).
// "잠깐의 휴식 › 커피 내리기" is left out as in the plain "/" menu (SlashMenu).
const KINDS = new Set(["paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item", "numbered_list_item", "to_do"]);
const ACTIONS = { copy_link: "copyLink", present: "present", delete: "delete" } as const;

/** Whether "/" lists a row while suggesting. */
export function slashRowWhileSuggesting(def: BlockDef) {
  if (def.action) return def.action in ACTIONS;
  return def.group === "Basic" && !def.inline && KINDS.has(def.type);
}

/**
 * A "/" row picked while suggesting: the typed "/…" goes (`suggest`, the
 * editor's suggestions), then its action runs on the line's block — "삭제"
 * suggests the block away (`remove`) — or a block of its kind is suggested
 * (nativeTiptapSuggestions `slash`).
 */
export function pickSlashRowWhileSuggesting(
  def: BlockDef, range: { blockId: string; from: number; to: number },
  suggest: (from: number, to: number, blockType?: string) => boolean, remove: (blockIds: readonly string[]) => boolean,
) {
  if (!slashRowWhileSuggesting(def)) return;
  const action = def.action ? ACTIONS[def.action as keyof typeof ACTIONS] : undefined;
  if (!suggest(range.from, range.to, action ? undefined : def.type) || !action) return;
  if (action === "delete") remove([range.blockId]); else requestEditorBlockCommand(range.blockId, action);
}
