"use client";

import { personValue, storePersonValue } from "@/lib/browserStorage";
import { type KeyboardEvent, type PointerEvent, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MapPinPlusIcon, X } from "@/icons/leafen";
import type { DbProperty, DbView, Page, PlacePropertyValue } from "@/lib/types";
import type { CurrentDatabaseViewsReadSnapshot } from "./CurrentDatabaseViews";
import { NotionSelect } from "./NotionSelect";
import { PropertyIcon } from "./PropertyIcon";
import { databaseViewLabels } from "./databaseViewLabels";
import { MapBaseLayer, useMapTileConfig } from "./DatabaseMapBase";
import {
  fitCamera,
  inverseMercator,
  MAP_PIN_LIMIT,
  mercator,
  panCamera,
  projectToScreen,
  screenToWorld,
  zoomCamera,
  type MapCamera,
  type MapViewport,
} from "./mapViewModel";
import { cellValue } from "./query";
import { conditionalColorResolver } from "./conditionalColorModel";
import styles from "./DatabaseMapView.module.css";
import { viewDisplayName } from "./databaseViewShared";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { DatabaseViewControls } from "./DatabaseViewControls";
import { UndatedRowsMenu } from "./UndatedRowsMenu";
import { useRowDisplayTitle } from "./untitledRowName";
import { placePropertySummary } from "./structuredPropertyValues";

function isPlaceValue(value: unknown): value is PlacePropertyValue {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return Number.isFinite(record.lat) && Number.isFinite(record.lon);
}

/**
 * Notion's map view: rows with a place sit as pins on a map that zooms and
 * pans; a pin opens its page. At most 100 pins show at once. Under the pins
 * lies the server's tile map, or the bundled outline and a coordinate grid
 * when external tiles are off or fail (`MapBaseLayer`). A conditional color
 * fills a pin with that color's accent, Notion's blue pin staying for the
 * rest and for "default" (seen 2026-10-12). Its top right holds "핀 추가 모드
 * 활성화": on, the map takes a press as a new page placed there, which
 * opens, and the mode goes off (Notion's, seen 2026-11-02).
 */
const MAP_CAMERA = "database.map-camera";

function savedMapCamera(viewId: string): MapCamera | null {
  try {
    const saved = JSON.parse(personValue(MAP_CAMERA, { part: viewId }) ?? "null") as MapCamera | null;
    return saved && Number.isFinite(saved.zoom) ? saved : null;
  } catch {
    return null;
  }
}

export function MapView({
  view,
  rows,
  onOpenRow,
  snapshot,
  onSelectMapBy,
  onAddPin,
}: {
  db: Page;
  view: DbView;
  rows: Page[];
  onOpenRow: (pageId: string, mode?: "new_tab" | "side") => void;
  snapshot: CurrentDatabaseViewsReadSnapshot;
  onSelectMapBy?: (propertyId: string) => void;
  /** A new page with the place pressed in pin mode, in that place property. */
  onAddPin?: (propertyId: string, place: { lat: number; lon: number }) => void;
}) {
  const { t } = useTranslation("mapView");
  const rowTitle = useRowDisplayTitle();
  const labels = databaseViewLabels();
  const tileConfig = useMapTileConfig();
  const properties = snapshot.properties as DbProperty[];
  const placeProperties = properties.filter((property) => property.type === "place");
  const placeProperty = placeProperties.find((property) => property.id === view.config?.mapBy)
    ?? placeProperties[0];
  const allPoints = useMemo(() => placeProperty
    ? rows.flatMap((row) => {
      const value = cellValue(row, placeProperty);
      return isPlaceValue(value) ? [{ row, value, world: mercator(value.lat, value.lon) }] : [];
    })
    : [], [placeProperty, rows]);
  const points = allPoints.slice(0, MAP_PIN_LIMIT);
  // The rows with no place, listed from the toolbar as Notion's "위치 없음(N개)" (seen 2026-11-02).
  const placeless = useMemo(() => placeProperty
    ? rows.filter((row) => !isPlaceValue(cellValue(row, placeProperty)))
    : [], [placeProperty, rows]);
  const pinColor = useMemo(() => conditionalColorResolver(view.config as Record<string, unknown>, properties, {
    pagesById: snapshot.propertyValueContext.pagesById as Record<string, Page>, currentUserId: snapshot.propertyValueContext.userId,
  }, snapshot.queryBounds), [properties, snapshot.propertyValueContext, snapshot.queryBounds, view.config]);
  const canvasRef = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState<MapViewport>({ width: 720, height: 420 });
  // Moved, a view comes back to where its reader left it, as Notion's (seen 2026-11-03).
  const [camera, setCameraNow] = useState<MapCamera | null>(() => savedMapCamera(view.id));
  const moved = useRef(camera !== null);
  const setCamera = (next: MapCamera | ((previous: MapCamera | null) => MapCamera)) => setCameraNow((previous) => {
    const camera = typeof next === "function" ? next(previous) : next;
    moved.current = true;
    storePersonValue(MAP_CAMERA, JSON.stringify(camera), { part: view.id });
    return camera;
  });
  const drag = useRef<{ pointerId: number; left: number; top: number; moved: number } | null>(null);
  const [addingPin, setAddingPin] = useState(false);
  const fitKey = `${placeProperty?.id ?? ""}:${points.map(({ row }) => row.id).join(",")}`;

  useEffect(() => {
    const element = canvasRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) setViewport({ width, height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [placeProperty?.id]);

  useEffect(() => {
    if (!moved.current) setCameraNow(null);
  }, [fitKey]);

  const current = camera ?? fitCamera(points.map(({ world }) => world), viewport);

  useEffect(() => {
    const element = canvasRef.current;
    if (!element) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = element.getBoundingClientRect();
      const delta = Math.max(-1, Math.min(1, -event.deltaY / 240));
      setCamera((previous) => zoomCamera(previous ?? current, delta, {
        left: event.clientX - rect.left, top: event.clientY - rect.top,
      }, viewport));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  });

  const center = { left: viewport.width / 2, top: viewport.height / 2 };
  const zoomBy = (delta: number) => setCamera(zoomCamera(current, delta, center, viewport));

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || (event.target as HTMLElement).closest("button, a")) return;
    drag.current = { pointerId: event.pointerId, left: event.clientX, top: event.clientY, moved: 0 };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    const dx = event.clientX - active.left;
    const dy = event.clientY - active.top;
    drag.current = { ...active, left: event.clientX, top: event.clientY, moved: active.moved + Math.abs(dx) + Math.abs(dy) };
    setCamera((previous) => panCamera(previous ?? current, dx, dy));
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    const active = drag.current;
    if (active?.pointerId !== event.pointerId) return;
    drag.current = null;
    // In pin mode a press that didn't pan places a page where it was.
    if (!addingPin || !placeProperty || !onAddPin || active.moved > 4 || event.type !== "pointerup") return;
    const rect = event.currentTarget.getBoundingClientRect();
    const { lat, lon } = inverseMercator(screenToWorld(event.clientX - rect.left, event.clientY - rect.top, current, viewport));
    setAddingPin(false);
    onAddPin(placeProperty.id, { lat, lon });
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [80, 0], ArrowRight: [-80, 0], ArrowUp: [0, 80], ArrowDown: [0, -80],
    };
    if (event.target !== event.currentTarget) return;
    if (event.key === "Escape" && addingPin) {
      event.preventDefault();
      setAddingPin(false);
    } else if (moves[event.key]) {
      event.preventDefault();
      setCamera(panCamera(current, ...moves[event.key]));
    } else if (event.key === "+" || event.key === "=") {
      event.preventDefault();
      zoomBy(1);
    } else if (event.key === "-" || event.key === "_") {
      event.preventDefault();
      zoomBy(-1);
    }
  }

  return (
    <section
      className={styles.mapView}
      aria-label={labels.mapViewAria(viewDisplayName(view))}
      data-map-view
      data-map-property-id={placeProperty?.id}
    >
      {placeProperty ? (
        <DatabaseViewControls><UndatedRowsMenu rows={placeless} readOnly place onAssignToday={() => {}} onOpen={(rowId) => onOpenRow(rowId)} /></DatabaseViewControls>
      ) : null}
      {placeProperties.length > 1 && placeProperty ? (
        <div className={styles.toolbar}>
          <NotionSelect
            ariaLabel={labels.mapBy}
            value={placeProperty.id}
            options={placeProperties.map((property) => ({
              value: property.id,
              label: property.name,
              icon: <PropertyIcon property={property} size="small" />,
            }))}
            disabled={!onSelectMapBy}
            onChange={(propertyId) => onSelectMapBy?.(propertyId)}
          />
        </div>
      ) : null}
      {/* A map with no row placed yet shows the map, as Notion's does (seen
          2026-10-06); only a screen reader hears that no row is on it. */}
      {!placeProperty ? (
        <div className={styles.empty}>{labels.mapNeedsPlace}</div>
      ) : (
        <div
          ref={canvasRef}
          className={styles.canvas}
          role="application"
          aria-roledescription={t("roleDescription")}
          aria-label={labels.mapPlotAria(viewDisplayName(view))}
          tabIndex={0}
          data-map-zoom={current.zoom.toFixed(2)}
          data-height={view.config?.mapHeight ?? "medium"}
          data-adding-pin={addingPin ? "true" : undefined}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
          onDoubleClick={(event) => {
            if ((event.target as HTMLElement).closest("button")) return;
            const rect = event.currentTarget.getBoundingClientRect();
            setCamera(zoomCamera(current, 1, { left: event.clientX - rect.left, top: event.clientY - rect.top }, viewport));
          }}
          onKeyDown={onKeyDown}
        >
          <MapBaseLayer camera={current} viewport={viewport} config={tileConfig} />
          {onAddPin ? (
            <div className={styles.addPin}>
              <button type="button" aria-label={t(addingPin ? "cancelAddPin" : "addPin")} title={t(addingPin ? "cancelAddPin" : "addPin")}
                aria-pressed={addingPin} data-map-add-pin onClick={() => setAddingPin((on) => !on)}>
                {addingPin ? <X size="small" aria-hidden="true" /> : <MapPinPlusIcon size="small" aria-hidden="true" />}
              </button>
            </div>
          ) : null}
          {points.map(({ row, value, world }) => {
            const title = rowTitle(row);
            const place = placePropertySummary(value);
            const { left, top } = projectToScreen(world, current, viewport);
            if (left < -24 || top < -24 || left > viewport.width + 24 || top > viewport.height + 24) return null;
            const color = pinColor(row)?.color;
            const tinted = color && color !== "default" ? color : undefined;
            return (
              <button
                key={row.id}
                type="button"
                className={styles.pin}
                style={{ left: `${left}px`, top: `${top}px`, ...(tinted ? { "--pin-color": `var(--c-accent-${tinted})` } : {}) }}
                aria-label={labels.openMapRow(title, place)}
                title={`${title} · ${place}`}
                data-map-pin={row.id}
                data-conditional-color={tinted}
                onClick={(event) => { const mode = rowOpenModeOfClick(event); onOpenRow(row.id, mode === "default" ? undefined : mode); }}
              >
                <span aria-hidden="true" />
              </button>
            );
          })}
          {allPoints.length === 0 ? <p className={styles.quietStatus} role="status">{labels.mapEmpty}</p> : null}
          {allPoints.length > MAP_PIN_LIMIT ? (
            <p className={styles.notice} role="status">
              {t("pinLimit", { shown: MAP_PIN_LIMIT, count: allPoints.length })}
            </p>
          ) : null}
        </div>
      )}
    </section>
  );
}
