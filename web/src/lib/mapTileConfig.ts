import type { MapConfig } from "@/runtimeConfigWire.generated";

/** Map tiles stay off unless the server names a usable tile address. */
export const DISABLED_MAP_TILES: MapConfig = {
  tilesEnabled: false,
  tileUrl: "",
  attribution: "",
  attributionUrl: "",
};

function webUrl(candidate: unknown): string | null {
  if (typeof candidate !== "string" || /\s/u.test(candidate)) return null;
  try {
    const url = new URL(candidate.replace(/\{[zxy]\}/gu, "0"));
    if ((url.protocol !== "https:" && url.protocol !== "http:") || url.username || url.password) return null;
    return candidate;
  } catch {
    return null;
  }
}

export function normalizeMapConfig(value: unknown): MapConfig {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const tileUrl = webUrl(record.tileUrl);
  const attribution = typeof record.attribution === "string" ? record.attribution.trim() : "";
  if (
    record.tilesEnabled !== true || !tileUrl
    || !["{z}", "{x}", "{y}"].every((placeholder) => tileUrl.includes(placeholder))
    || !attribution || attribution.length > 200
  ) {
    return DISABLED_MAP_TILES;
  }
  return {
    tilesEnabled: true,
    tileUrl,
    attribution,
    attributionUrl: webUrl(record.attributionUrl) ?? "",
  };
}

export function mapTileSource(template: string, tile: { z: number; x: number; y: number }) {
  return template
    .replaceAll("{z}", String(tile.z))
    .replaceAll("{x}", String(tile.x))
    .replaceAll("{y}", String(tile.y));
}
