import { newId } from "@/lib/ids";

type RelationConfig = Record<string, unknown>;

/**
 * A two-way relation names its paired property in relatedPropertyId; the
 * server creates, renames and removes that property on the related database
 * and keeps both sides' values in step. reciprocalName asks it to name the
 * paired property. A relation to its own database pairs on that database,
 * and without a pair it relates both rows by itself (relationSymmetric), as
 * Notion's left when its related property is turned off (seen 2026-11-12).
 */
export function relationTwoWayConfig<T extends object>(
  config: T, sourceDataSourceId: string, enabled: boolean, reciprocalName?: string,
): T {
  const current = { ...config } as RelationConfig;
  const next: RelationConfig = { ...current };
  delete next.relationTwoWay;
  delete next.relationSymmetric;
  if (!enabled) {
    delete next.relatedPropertyId;
    delete next.reciprocalName;
    return withSymmetry(next, sourceDataSourceId) as T;
  }
  const pairId = typeof current.relatedPropertyId === "string" ? current.relatedPropertyId : "";
  next.relatedPropertyId = pairId || newId();
  const name = reciprocalName?.trim();
  if (name) next.reciprocalName = name;
  return next as T;
}

/** Pointing a relation at another database pairs it anew there, if paired. */
export function relationTargetConfig<T extends object>(config: T, sourceDataSourceId: string, targetId: string): T {
  const current = { ...config } as RelationConfig;
  const next: RelationConfig = { ...current, relationDatabaseId: targetId };
  delete next.relationTwoWay;
  delete next.relationSymmetric;
  if (current.relationDatabaseId !== targetId && next.relatedPropertyId) {
    delete next.reciprocalName;
    next.relatedPropertyId = newId();
  }
  return withSymmetry(next, sourceDataSourceId) as T;
}

/**
 * A relation to its own database with no paired property relates both rows,
 * as one Notion's screen makes does (seen 2026-11-12); the server pairs it
 * with itself.
 */
function withSymmetry(config: RelationConfig, sourceDataSourceId: string): RelationConfig {
  if (config.relationDatabaseId === sourceDataSourceId && !config.relatedPropertyId) config.relationSymmetric = true;
  return config;
}

/** Whether a relation is two-way: it names its paired property. */
export function relationIsTwoWay(config: object): boolean {
  const pairId = (config as RelationConfig).relatedPropertyId;
  return typeof pairId === "string" && pairId !== "";
}
