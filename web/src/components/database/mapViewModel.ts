/**
 * The map view's camera in Web Mercator: a world square of 256 × 2^zoom
 * pixels, a center in world units (0–1 on each axis) and a zoom level.
 * Place pins, the coordinate grid, pan and zoom all go through it.
 */

export const MAP_PIN_LIMIT = 100;
const MAP_MIN_ZOOM = 0;
export const MAP_MAX_ZOOM = 17;
const MAX_LATITUDE = 85.05112878;
const TILE_SIZE = 256;

export type MapCamera = { x: number; y: number; zoom: number };
export type MapViewport = { width: number; height: number };
export type MapWorldPoint = { x: number; y: number };

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

export function worldSize(zoom: number) {
  return TILE_SIZE * 2 ** zoom;
}

export function mercator(lat: number, lon: number): MapWorldPoint {
  const latitude = clamp(lat, -MAX_LATITUDE, MAX_LATITUDE) * (Math.PI / 180);
  return {
    x: (clamp(lon, -180, 180) + 180) / 360,
    y: (1 - Math.log(Math.tan(latitude) + 1 / Math.cos(latitude)) / Math.PI) / 2,
  };
}

export function inverseMercator(point: MapWorldPoint) {
  const lon = point.x * 360 - 180;
  const lat = (Math.atan(Math.sinh(Math.PI * (1 - 2 * point.y))) * 180) / Math.PI;
  return { lat, lon };
}

export function projectToScreen(point: MapWorldPoint, camera: MapCamera, viewport: MapViewport) {
  const size = worldSize(camera.zoom);
  return {
    left: (point.x - camera.x) * size + viewport.width / 2,
    top: (point.y - camera.y) * size + viewport.height / 2,
  };
}

export function screenToWorld(left: number, top: number, camera: MapCamera, viewport: MapViewport): MapWorldPoint {
  const size = worldSize(camera.zoom);
  return {
    x: camera.x + (left - viewport.width / 2) / size,
    y: camera.y + (top - viewport.height / 2) / size,
  };
}

function clampCamera(camera: MapCamera): MapCamera {
  return {
    x: clamp(camera.x, 0, 1),
    y: clamp(camera.y, 0, 1),
    zoom: clamp(camera.zoom, MAP_MIN_ZOOM, MAP_MAX_ZOOM),
  };
}

/**
 * A map with nothing on it looks at the United States from where Notion's
 * new map does, whatever its size (seen 2026-11-09 at 684×400 and 939×400:
 * MapLibre's center 40°N 100°W at zoom 3.5, its tiles 512px — 4.5 here).
 */
const EMPTY_MAP_CAMERA: MapCamera = { ...mercator(40, -100), zoom: 4.5 };

/** The United States mainland filling a small map, as a place's own map starts with no point (Leafen's). */
export function unitedStatesCamera(viewport: MapViewport): MapCamera {
  return fitCamera([mercator(49.4, -124.8), mercator(24.5, -66.9)], viewport, 0);
}

/**
 * The camera that shows every pin, as Notion's map opens (seen 2026-11-03,
 * MapLibre at 684×400): the pins' bounds in the middle with 25px around
 * them, no closer than the map goes — so one place, or several at one spot,
 * opens at the closest zoom, its streets and buildings in sight.
 */
export function fitCamera(points: readonly MapWorldPoint[], viewport: MapViewport, padding = 25): MapCamera {
  const width = Math.max(1, viewport.width - padding * 2);
  const height = Math.max(1, viewport.height - padding * 2);
  if (points.length === 0) return EMPTY_MAP_CAMERA;
  const xs = points.map((point) => point.x);
  const ys = points.map((point) => point.y);
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const minY = Math.min(...ys);
  const maxY = Math.max(...ys);
  const spanX = maxX - minX;
  const spanY = maxY - minY;
  const zoom = Math.min(
    spanX > 0 ? Math.log2(width / (TILE_SIZE * spanX)) : MAP_MAX_ZOOM,
    spanY > 0 ? Math.log2(height / (TILE_SIZE * spanY)) : MAP_MAX_ZOOM,
    MAP_MAX_ZOOM,
  );
  return clampCamera({ x: (minX + maxX) / 2, y: (minY + maxY) / 2, zoom });
}

export function panCamera(camera: MapCamera, dx: number, dy: number): MapCamera {
  const size = worldSize(camera.zoom);
  return clampCamera({ ...camera, x: camera.x - dx / size, y: camera.y - dy / size });
}

/** Zooms by `delta` levels keeping the world point under `anchor` still. */
export function zoomCamera(
  camera: MapCamera,
  delta: number,
  anchor: { left: number; top: number },
  viewport: MapViewport,
): MapCamera {
  const zoom = clamp(camera.zoom + delta, MAP_MIN_ZOOM, MAP_MAX_ZOOM);
  const fixed = screenToWorld(anchor.left, anchor.top, camera, viewport);
  const size = worldSize(zoom);
  return clampCamera({
    x: fixed.x - (anchor.left - viewport.width / 2) / size,
    y: fixed.y - (anchor.top - viewport.height / 2) / size,
    zoom,
  });
}

export type MapTile = { key: string; z: number; x: number; y: number; left: number; top: number; size: number };

const MAP_TILE_LIMIT = 64;

/**
 * The tiles that cover the viewport at the zoom level nearest the camera,
 * scaled to the camera's fractional zoom. Nothing outside the viewport is
 * requested.
 */
export function visibleTiles(camera: MapCamera, viewport: MapViewport, maxZoom = 19): MapTile[] {
  const z = clamp(Math.round(camera.zoom), 0, maxZoom);
  const count = 2 ** z;
  const scale = worldSize(camera.zoom) / count;
  const topLeft = screenToWorld(0, 0, camera, viewport);
  const bottomRight = screenToWorld(viewport.width, viewport.height, camera, viewport);
  const minX = clamp(Math.floor(topLeft.x * count), 0, count - 1);
  const maxX = clamp(Math.floor(bottomRight.x * count), 0, count - 1);
  const minY = clamp(Math.floor(topLeft.y * count), 0, count - 1);
  const maxY = clamp(Math.floor(bottomRight.y * count), 0, count - 1);
  const tiles: MapTile[] = [];
  for (let y = minY; y <= maxY; y += 1) {
    for (let x = minX; x <= maxX && tiles.length < MAP_TILE_LIMIT; x += 1) {
      const origin = projectToScreen({ x: x / count, y: y / count }, camera, viewport);
      const left = Math.round(origin.left);
      const top = Math.round(origin.top);
      tiles.push({
        key: `${z}/${x}/${y}`, z, x, y, left, top,
        size: Math.round(origin.left + scale) - left,
      });
    }
  }
  return tiles;
}

const GRID_STEPS = [90, 45, 30, 15, 10, 5, 2, 1, 0.5, 0.2, 0.1, 0.05, 0.02, 0.01, 0.005, 0.002, 0.001];

/** Latitude and longitude lines about every 90 px at the current zoom. */
export function graticule(camera: MapCamera, viewport: MapViewport) {
  const degreesPerPixel = 360 / worldSize(camera.zoom);
  const step = GRID_STEPS.find((candidate) => candidate / degreesPerPixel < 90 * 2 && candidate / degreesPerPixel >= 90)
    ?? GRID_STEPS.find((candidate) => candidate / degreesPerPixel < 90)
    ?? GRID_STEPS[GRID_STEPS.length - 1];
  const topLeft = inverseMercator(screenToWorld(0, 0, camera, viewport));
  const bottomRight = inverseMercator(screenToWorld(viewport.width, viewport.height, camera, viewport));
  const lines = (from: number, to: number, min: number, max: number) => {
    const values: number[] = [];
    const start = Math.ceil(Math.max(min, from) / step) * step;
    for (let value = start; value <= Math.min(max, to) && values.length < 200; value += step) {
      values.push(Number(value.toFixed(6)));
    }
    return values;
  };
  return {
    step,
    longitudes: lines(topLeft.lon, bottomRight.lon, -180, 180),
    latitudes: lines(bottomRight.lat, topLeft.lat, -MAX_LATITUDE, MAX_LATITUDE),
  };
}

export function formatCoordinate(value: number, positive: string, negative: string) {
  const rounded = Number(Math.abs(value).toFixed(3));
  return `${rounded}°${value >= 0 ? positive : negative}`;
}
