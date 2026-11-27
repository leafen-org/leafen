import type { SelectOption } from "@/lib/types";

// A select's "정렬", as Notion's (seen 2026-10-05): options in the order set
// by hand, alphabetical or reverse alphabetical. The options are stored in
// the chosen order, so every list of them reads the same; a new or renamed
// option takes its place, and dragging one sets the order by hand again.
export const OPTION_SORTS = ["manual", "alphabetical", "reverse_alphabetical"] as const;
export type OptionSort = (typeof OPTION_SORTS)[number];

export function optionSortOf(config: unknown): OptionSort {
  const sort = (config as { optionSort?: unknown } | undefined)?.optionSort;
  return OPTION_SORTS.includes(sort as OptionSort) ? sort as OptionSort : "manual";
}

export function sortedOptions(options: readonly SelectOption[], sort: OptionSort) {
  if (sort === "manual") return [...options];
  const sorted = [...options].sort((left, right) => left.name.localeCompare(right.name));
  return sort === "reverse_alphabetical" ? sorted.reverse() : sorted;
}
