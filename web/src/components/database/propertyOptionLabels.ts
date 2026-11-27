import type { DbProperty } from "@/lib/types";

// Labels that tell apart properties or options sharing a name, however many
// a database holds.
export function disambiguateOptionLabels(baseLabels: readonly string[]) {
  const totals = new Map<string, number>();
  for (const label of baseLabels) {
    totals.set(label, (totals.get(label) ?? 0) + 1);
  }

  const occurrences = new Map<string, number>();
  return baseLabels.map((label) => {
    if ((totals.get(label) ?? 0) < 2) return label;
    const occurrence = (occurrences.get(label) ?? 0) + 1;
    occurrences.set(label, occurrence);
    return `${label} (${occurrence})`;
  });
}

export function disambiguatePropertyLabels(
  properties: readonly DbProperty[],
  untitledLabel: string
) {
  return disambiguateOptionLabels(
    properties.map((property) => property.name || untitledLabel)
  );
}
