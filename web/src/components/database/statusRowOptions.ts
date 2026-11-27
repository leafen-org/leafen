import type { PropertyConfig, SelectOption } from "@/lib/types";
import { statusGroupColumns, statusGroupForOption, statusOptionsByOption } from "./propertyStatusPolicy";

/**
 * A status over the rows of several databases ("내 작업"'s) lists each row's
 * own database's options apart (PropertyConfig.rowOptionIds): what a row's
 * status may be set to is decided here, for its pick and a card moved on a
 * board, and what its filter's list opens. A status that lists none (every
 * database's own) takes any option and folds none.
 */
function ownOptionIds(config: unknown, rowId: string): readonly string[] | undefined {
  return config && typeof config === "object" ? (config as PropertyConfig).rowOptionIds?.[rowId] : undefined;
}

/**
 * The options a row's status is picked from: its own database's, in that
 * database's order within each group, where its status lists them apart, as
 * Notion's card and cell pick on My tasks (seen 2026-11-24 in two tabs: an
 * "AB03c" task's pick read 대기, 시작 전 | 진행 중 | Done — its database's own
 * order, while My tasks' columns read 시작 전, 대기 — and none of the other
 * source's 완료 or 보관됨); else all a view by option lists (statusOptionsByOption).
 */
export function statusOptionsForRow(config: unknown, rowId: string): SelectOption[] {
  const all = statusOptionsByOption(config);
  const own = ownOptionIds(config, rowId);
  if (!own) return all;
  const byId = new Map(all.map((option) => [option.id, option]));
  const groupOf = new Map(statusGroupColumns(config).flatMap((group, index) => group.optionIds.map((id) => [id, index] as const)));
  return own.flatMap((id) => byId.get(id) ?? [])
    .map((option, index) => ({ option, index, group: groupOf.get(option.id) ?? 0 }))
    .sort((left, right) => left.group - right.group || left.index - right.index)
    .map(({ option }) => option);
}

/**
 * Whether a row's status takes a value: any where it lists no options apart;
 * else one of its own database's options, or a group's own (a check, a card
 * moved to a group's column) where its database has an option in that group.
 * A card dragged on My tasks' board over a column of an option its database
 * lacks shows no drop line there and stays where it was (boardCellTakesCard),
 * as Notion's (seen 2026-11-24 in two tabs), rather than standing there until
 * the server wrote that database's first option of the group.
 */
export function statusTakesValue(config: unknown, rowId: string, value: unknown): boolean {
  const own = ownOptionIds(config, rowId);
  if (!own || typeof value !== "string") return true;
  if (own.includes(value)) return true;
  const options = (config as PropertyConfig).options ?? [];
  if (!options.some((option) => option.id === value && option.standsForGroup)) return false;
  const group = statusGroupForOption(config, value)?.id;
  return own.some((id) => statusGroupForOption(config, id)?.id === group);
}

const NONE: ReadonlySet<string> = new Set();

/**
 * The options a status filter's list folds as it opens, under "기타 상태
 * 옵션 N개", as Notion's My tasks (seen 2026-11-24 in two tabs): it opens
 * the options of the databases a task the view shows is in
 * (PropertyConfig.shownOptionIds) — a task "진행 중" of one database opened
 * all three of its options, the other database's two folded, and with no
 * task shown every option folded under the groups — and an option chosen by
 * itself, a group chosen leaving its options folded. A status that lists no
 * shown options (a database's own) folds none.
 */
export function statusFilterFolded(config: unknown, chosen: ReadonlySet<string>): ReadonlySet<string> {
  const shown = config && typeof config === "object" ? (config as PropertyConfig).shownOptionIds : undefined;
  if (!shown) return NONE;
  const opened = new Set(shown);
  return new Set(statusOptionsByOption(config)
    .filter((option) => !opened.has(option.id) && !chosen.has(option.id) && !chosen.has(option.name))
    .map((option) => option.id));
}
