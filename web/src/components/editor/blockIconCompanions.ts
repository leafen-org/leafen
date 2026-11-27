/**
 * Several chosen blocks' "아이콘 편집" opens the first one's own picker, and
 * the icon picked there each of them takes, as Notion's (seen 2026-11-26 in
 * Chrome, two tabs at different times: two sub-pages, two callouts; two
 * full-page databases' lines offer it too). The picker asks once, as it opens,
 * which other blocks go with it (blockMenuSeveral).
 */
const companions = new Map<string, readonly string[]>();

/** The blocks that take the icon picked in this block's picker as well, the next time it opens from its menu. */
export function setIconCompanions(blockId: string, others: readonly string[]) {
  companions.set(blockId, others);
}

/** The blocks that take the icon picked in this block's picker too; none when it opened for the block alone. */
export function takeIconCompanions(blockId: string): readonly string[] {
  const others = companions.get(blockId) ?? [];
  companions.delete(blockId);
  return others;
}
