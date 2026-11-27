/** The name Notion sorts its group for no value by, among the options' names. */
const NO_VALUE_GROUP_NAME = "uncategorized";

/**
 * The order a view's groups by an option property start in, new or grouped
 * by it from the settings, as Notion's (seen 2026-11-07, both ways): the
 * options and the group for no value by name, character codes compared, so
 * capitals before small letters and both before Hangul, and no value sorted
 * as "uncategorized" — 10, 2, Alpha, Mid, beta, no value, zeta, 가나 — a
 * table's, a board's and a board's sub-groups alike (seen 2026-11-08), a
 * view grouped on the screen or made by Notion's API sorting them by hand
 * alike (seen 2026-11-24 in two tabs: tags x, y, z with "M 없음" first; a
 * select's a, b with "S 없음" last). The view keeps the order as its own from
 * then on; groups added later follow. The server gives an agent's views the
 * same (mcpBoardGroupOrder). `none` is the key of the group for no value, a
 * board's or a table's (NO_TABLE_GROUP); undefined for another property.
 */
export function boardOptionGroupOrder(property: { type: string; config?: unknown } | undefined, none = "__none"): string[] | undefined {
  if (property?.type !== "select" && property?.type !== "multi_select") return undefined;
  const raw = (property.config as { options?: unknown } | undefined)?.options;
  const options = Array.isArray(raw) ? raw.filter((option): option is { id: string; name: string } =>
    typeof option?.id === "string" && typeof option?.name === "string") : [];
  const named = [...options.map((option) => [option.name, option.id] as const), [NO_VALUE_GROUP_NAME, none] as const];
  return named.sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0)).map(([, id]) => id);
}

/**
 * The order of groups by an option property (`groups`: each option's id and
 * name, the group for no value as `none`) with no order of their own kept
 * yet, as a new grouping by it starts them (boardOptionGroupOrder): a
 * board's columns and sub-groups as a table's groups (orderTableGroups),
 * Notion's alike (seen 2026-11-24 in two tabs: boards by tags y, x, z and by
 * a select c, a, b, and their sub-groups, made on the screen or by its views
 * API — "M 없음", x, y, z and a, b, c, "S 없음"). Values no option holds any
 * more ("unknown:") are left out, to follow in their own order.
 */
export function optionGroupsStartOrder(type: string, groups: readonly { key: string; label: string }[], none = "__none"): string[] | undefined {
  const options = groups.filter(({ key }) => key !== none && !key.startsWith("unknown:")).map(({ key, label }) => ({ id: key, name: label }));
  return boardOptionGroupOrder({ type, config: { options } }, none);
}
