"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DISABLED_MAP_TILES, mapTileSource } from "@/lib/mapTileConfig";
import { fetchRuntimeConfig, subscribeRuntimeConfig } from "@/lib/runtimeConfigService";
import type { MapConfig } from "@/runtimeConfigWire.generated";
import {
  formatCoordinate,
  graticule,
  mercator,
  projectToScreen,
  visibleTiles,
  worldSize,
  type MapCamera,
  type MapViewport,
} from "./mapViewModel";
import styles from "./DatabaseMapView.module.css";

/** The tile settings the server gives every browser, off until they load. */
export function useMapTileConfig(): MapConfig { return useRuntimeMapConfig().map; }
export function useRuntimeMapConfig() {
  const [config, setConfig] = useState({map:DISABLED_MAP_TILES,placeSearchEnabled:false});
  useEffect(() => {
    let live = true, generation = 0;
    const update = (runtime: Awaited<ReturnType<typeof fetchRuntimeConfig>>["config"]) => {
      if (live) setConfig({map:runtime.map,placeSearchEnabled:runtime.placeSearchEnabled});
    };
    const unsubscribe = subscribeRuntimeConfig(runtime => { generation++; update(runtime); });
    void fetchRuntimeConfig(true).then(({ config: runtime }) => {
      if (generation === 0) update(runtime);
    });
    return () => { live = false; unsubscribe(); };
  }, []);
  return config;
}

const OUTLINE_UNITS = 4096;

type MapOutline = typeof import("./mapOutline.generated").mapOutline;

let outlineLoad: Promise<MapOutline> | null = null;

/** The bundled outline, fetched once the first time a map draws it. */
function useMapOutline(needed: boolean): MapOutline | null {
  const [outline, setOutline] = useState<MapOutline | null>(null);
  useEffect(() => {
    if (!needed || outline) return;
    let live = true;
    outlineLoad ??= import("./mapOutline.generated").then((module) => module.mapOutline);
    outlineLoad.then((loaded) => { if (live) setOutline(loaded); }, () => { outlineLoad = null; });
    return () => { live = false; };
  }, [needed, outline]);
  return outline;
}

function outlinePath(rings: readonly (readonly number[])[], closed: boolean) {
  let path = "";
  for (const ring of rings) {
    for (let index = 0; index + 1 < ring.length; index += 2) {
      const point = mercator(ring[index + 1], ring[index]);
      path += `${index === 0 ? "M" : "L"}${(point.x * OUTLINE_UNITS).toFixed(1)} ${(point.y * OUTLINE_UNITS).toFixed(1)}`;
    }
    if (closed) path += "Z";
  }
  return path;
}

/**
 * What lies under the pins: the tile server's map when external tiles are
 * on, each tile requested only while it is in view and sent with no more
 * than this site's origin as referrer. When tiles are off or a tile fails,
 * the bundled Natural Earth outline, fetched only then, and the coordinate
 * grid show instead.
 * The source of what is drawn is always credited.
 */
export function MapBaseLayer({
  camera,
  viewport,
  config,
}: {
  camera: MapCamera;
  viewport: MapViewport;
  config: MapConfig;
}) {
  const { t } = useTranslation("mapView");
  const [failed, setFailed] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => { setFailed(new Set()); }, [config.tileUrl]);
  const tiles = config.tilesEnabled ? visibleTiles(camera, viewport) : [];
  const fallback = !config.tilesEnabled || tiles.some((tile) => failed.has(tile.key));
  const outline = useMapOutline(fallback);
  const paths = useMemo(() => outline ? {
    land: outlinePath(outline.land, true),
    borders: outlinePath(outline.borders, false),
  } : null, [outline]);
  const size = worldSize(camera.zoom);
  const transform = `translate(${viewport.width / 2 - camera.x * size} ${viewport.height / 2 - camera.y * size}) `
    + `scale(${size / OUTLINE_UNITS})`;
  const outlineDrawn = fallback && outline !== null && outline.source !== "";
  // No label is drawn over another or under the credit in its corner.
  const credit = useRef<HTMLParagraphElement>(null);
  const [creditBox, setCreditBox] = useState({ width: 0, height: 0 });
  const credited = config.tilesEnabled || outlineDrawn;
  useLayoutEffect(() => {
    const box = credited ? credit.current : null;
    setCreditBox({ width: box?.offsetWidth ?? 0, height: box?.offsetHeight ?? 0 });
  }, [credited, config.tilesEnabled, config.attribution, outlineDrawn]);
  const taken: LabelBox[] = [{ left: viewport.width - creditBox.width, right: viewport.width,
    top: viewport.height - creditBox.height, bottom: viewport.height }];
  const grid = fallback ? gridLabels(camera, viewport, taken) : null;
  // Cities go by rank, the larger first, each only where its whole name is clear.
  const places = fallback && outline
    ? outline.places.flatMap((place) => {
      if (place.rank > camera.zoom + 1) return [];
      const { left, top } = projectToScreen(mercator(place.lat, place.lon), camera, viewport);
      if (left < 0 || top < 0 || left > viewport.width || top > viewport.height) return [];
      const box = { left: left - 3, right: left + 5 + place.name.length * PLACE_CHAR_WIDTH, top: top - 8, bottom: top + 6 };
      if (box.right > viewport.width || box.top < 0 || box.bottom > viewport.height || taken.some((other) => overlaps(box, other))) return [];
      taken.push(box);
      return [{ ...place, left, top }];
    }).slice(0, 60)
    : [];
  return (
    <>
      {fallback ? (
        <svg className={styles.outline} width={viewport.width} height={viewport.height} aria-hidden="true" data-map-outline>
          {paths ? (
            <g transform={transform}>
              <path className={styles.land} d={paths.land} />
              <path className={styles.border} d={paths.borders} vectorEffect="non-scaling-stroke" />
            </g>
          ) : null}
          {places.map((place) => (
            <g key={`${place.name}:${place.lon}:${place.lat}`} className={styles.place}>
              <circle cx={place.left} cy={place.top} r={2} />
              <text x={place.left + 5} y={place.top + 3}>{place.name}</text>
            </g>
          ))}
        </svg>
      ) : null}
      {grid ? <MapGrid grid={grid} viewport={viewport} /> : null}
      {tiles.length > 0 ? (
        <div className={styles.tiles} aria-hidden="true" data-map-tiles data-osm-tiles={osmTiles(config.tileUrl) ? "true" : undefined}>
          {tiles.filter((tile) => !failed.has(tile.key)).map((tile) => (
            <img
              key={tile.key}
              src={mapTileSource(config.tileUrl, tile)}
              alt=""
              width={tile.size}
              height={tile.size}
              style={{ left: `${tile.left}px`, top: `${tile.top}px`, width: `${tile.size}px`, height: `${tile.size}px` }}
              referrerPolicy="strict-origin-when-cross-origin"
              decoding="async"
              draggable={false}
              data-map-tile={tile.key}
              onError={() => setFailed((current) => new Set(current).add(tile.key))}
            />
          ))}
        </div>
      ) : null}
      {credited ? (
        <p ref={credit} className={styles.attribution} data-map-attribution>
          {config.tilesEnabled ? (
            config.attributionUrl
              ? <a href={config.attributionUrl} target="_blank" rel="noopener noreferrer">{config.attribution}</a>
              : <span>{config.attribution}</span>
          ) : null}
          {config.tilesEnabled && outlineDrawn ? <span aria-hidden="true"> · </span> : null}
          {outlineDrawn ? <span>{t("naturalEarth")}</span> : null}
        </p>
      ) : null}
    </>
  );
}

type LabelBox = { left: number; right: number; top: number; bottom: number };

function overlaps(a: LabelBox, b: LabelBox) {
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
}

// The longitudes' 10px labels run along the bottom and the latitudes' down
// the left; one that would fall on another, under the credit or off the map
// is left out (2026-11-02: two labels on one spot in the bottom left corner,
// the last one under "Natural Earth"), and the cities' 11px names keep clear
// of them all.
const GRID_CHAR_WIDTH = 6;
const PLACE_CHAR_WIDTH = 6.5;

/** The grid's lines and the labels that have room, each label's box added to `taken`. */
function gridLabels(camera: MapCamera, viewport: MapViewport, taken: LabelBox[]) {
  const grid = graticule(camera, viewport);
  const place = (label: string, box: LabelBox) => {
    if (box.top < 0 || box.right > viewport.width || taken.some((other) => overlaps(box, other))) return null;
    taken.push(box);
    return label;
  };
  const longitudes = grid.longitudes.map((lon) => {
    const { left } = projectToScreen(mercator(0, lon), camera, viewport);
    const label = formatCoordinate(lon, "E", "W");
    return { lon, left, label: place(label, { left: left + 4, right: left + 4 + label.length * GRID_CHAR_WIDTH,
      top: viewport.height - 16, bottom: viewport.height - 4 }) };
  });
  const latitudes = grid.latitudes.map((lat) => {
    const { top } = projectToScreen(mercator(lat, 0), camera, viewport);
    const label = formatCoordinate(lat, "N", "S");
    return { lat, top, label: place(label, { left: 6, right: 6 + label.length * GRID_CHAR_WIDTH, top: top - 14, bottom: top - 2 }) };
  });
  return { longitudes, latitudes };
}

/** Latitude and longitude lines with their degrees, drawn where no map is. */
function MapGrid({ grid, viewport }: { grid: ReturnType<typeof gridLabels>; viewport: MapViewport }) {
  return (
    <svg className={styles.grid} width={viewport.width} height={viewport.height} aria-hidden="true">
      {grid.longitudes.map(({ lon, left, label }) => (
        <g key={`lon:${lon}`}>
          <line x1={left} x2={left} y1={0} y2={viewport.height} data-meridian={lon === 0 || undefined} />
          {label ? <text x={left + 4} y={viewport.height - 6}>{label}</text> : null}
        </g>
      ))}
      {grid.latitudes.map(({ lat, top, label }) => (
        <g key={`lat:${lat}`}>
          <line x1={0} x2={viewport.width} y1={top} y2={top} data-meridian={lat === 0 || undefined} />
          {label ? <text x={6} y={top - 4}>{label}</text> : null}
        </g>
      ))}
    </svg>
  );
}

/** Whether the tiles are OpenStreetMap's own, drawn light whatever the theme. */
function osmTiles(tileUrl: string) {
  try { return /(^|\.)tile\.openstreetmap\.org$/u.test(new URL(tileUrl.replace(/\{[a-z]\}/gu, "0")).hostname); } catch { return false; }
}
