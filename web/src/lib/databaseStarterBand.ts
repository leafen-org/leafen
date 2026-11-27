import type { GraphBlock } from "@/contentGraph";

/**
 * A database page a template made keeps Notion's "시작하기" band under its
 * views until someone closes it (seen 2026-11-06, task tracker): the
 * template's example rows, which "샘플 페이지 삭제하기" sends to the trash, and
 * the tour Leafen knows for it.
 */
export type DatabaseStarterBand = { samples: string[]; tour?: string };

/** The band a database block holds, or null when it has none (closed, or not a template's). */
export function databaseStarterBand(block: GraphBlock): DatabaseStarterBand | null {
  const band = block.content.starterBand;
  if (!band || typeof band !== "object" || Array.isArray(band)) return null;
  const { samples, tour } = band as { samples?: unknown; tour?: unknown };
  return {
    samples: Array.isArray(samples) ? samples.filter((id): id is string => typeof id === "string" && id !== "") : [],
    ...(typeof tour === "string" && tour ? { tour } : {}),
  };
}
