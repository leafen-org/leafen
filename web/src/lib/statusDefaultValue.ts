type StatusConfig = {
  defaultOptionId?: unknown;
  options?: Array<{ id?: unknown }>;
  statusGroups?: Array<{ optionIds?: unknown }>;
};

// A view reads a status's default for each row that sets none, and an option
// property's options for each row: worked out once a config.
const defaults = new WeakMap<object, string | null>();
const optionIds = new WeakMap<object, Set<string>>();

/**
 * The option a status reads as while a row sets none, as Notion's (seen
 * 2026-10-06, 2026-11-08): the one its config names, else the first of its
 * first group's ("To-do"), else its first option. A config naming none ("")
 * has no default, as a status made from a select has in Notion
 * (default_option null): its rows that set none read empty. The server reads
 * the same (databaseStatusDefaultOptionID).
 */
export function statusDefaultOptionId(config: Record<string, unknown>): string | null {
  const kept = defaults.get(config);
  if (kept !== undefined) return kept;
  const found = defaultOf(config as StatusConfig);
  defaults.set(config, found);
  return found;
}

function defaultOf(status: StatusConfig): string | null {
  const known = new Set((Array.isArray(status.options) ? status.options : []).flatMap(({ id }) => typeof id === "string" ? [id] : []));
  if (!known.size || status.defaultOptionId === "") return null;
  if (typeof status.defaultOptionId === "string" && known.has(status.defaultOptionId)) return status.defaultOptionId;
  const first = Array.isArray(status.statusGroups) ? status.statusGroups[0]?.optionIds : undefined;
  const grouped = Array.isArray(first) ? first.find((id): id is string => typeof id === "string" && known.has(id)) : undefined;
  return grouped ?? [...known][0];
}

/**
 * The options a select's, multi-select's or status's config lists; null for
 * one that lists none, whose values are taken as they are.
 */
function configOptionIds(config: unknown): Set<string> | null {
  if (!config || typeof config !== "object" || !Array.isArray((config as StatusConfig).options)) return null;
  const kept = optionIds.get(config);
  if (kept) return kept;
  const found = new Set(((config as StatusConfig).options ?? []).flatMap(({ id }) => typeof id === "string" ? [id] : []));
  optionIds.set(config, found);
  return found;
}

/**
 * A status's value as it reads: the option a row sets, else its default.
 * Notion keeps no default in a row (seen 2026-11-08): rows that set none, made
 * before the status or after it, show the default and switch with it, a filter
 * or a sort reads it, and a type change leaves them empty. A row whose option
 * the status no longer has sets none, as Notion's reads it (seen 2026-11-08).
 * The server reads the same (databaseStatusValueOrDefault).
 */
export function statusValueOrDefault(config: unknown, value: unknown): unknown {
  if (typeof value === "string" ? value !== "" && (configOptionIds(config)?.has(value) ?? true) : value != null) return value;
  const fallback = statusDefaultOptionId(config && typeof config === "object" ? config as Record<string, unknown> : {});
  return fallback ?? (typeof value === "string" && value !== "" ? null : value);
}

/**
 * A select's or multi-select's value without the options it no longer has,
 * as Notion's rows read once the option is taken away (seen 2026-11-08). The
 * server reads the same (databaseOptionValueKnown).
 */
export function optionValueKnown(config: unknown, value: unknown): unknown {
  const known = configOptionIds(config);
  if (!known) return value;
  if (typeof value === "string") return value === "" || known.has(value) ? value : null;
  if (!Array.isArray(value) || value.length === 0) return value;
  const kept = value.filter((id) => typeof id !== "string" || known.has(id));
  return kept.length === value.length ? value : kept;
}
