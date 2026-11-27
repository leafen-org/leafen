"use client";

import type { PlaceSearchResponse } from "@/administratorWire.generated";
import { PlaceSearchControls } from "./PlaceSearchControls";
import { MinusIcon, Plus, XMarkIcon } from "@/icons/leafen";
import { type KeyboardEvent, type PointerEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { PlacePropertyValue } from "@/lib/types";
import { MapBaseLayer, useRuntimeMapConfig } from "./DatabaseMapBase";
import {
  inverseMercator,
  mercator,
  panCamera,
  projectToScreen,
  screenToWorld,
  zoomCamera,
  type MapCamera,
  type MapViewport,
  unitedStatesCamera,
} from "./mapViewModel";
import mapStyles from "./DatabaseMapView.module.css";
import styles from "./PlacePicker.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// Notion's map, 4px in from the window's sides and 168 high: 232 wide in a
// table cell's 240 window, 543 in a row page's 551 (seen 2026-11-12 and
// 2026-11-25). The map takes the window's width; this is the first draw's.
const VIEWPORT: MapViewport = { width: 232, height: 168 };
const PLACED_ZOOM = 12;
// A press that moves less than this picks the point; more pans the map.
const CLICK_SLOP = 4;

function startCamera(value: PlacePropertyValue | null): MapCamera {
  return value ? { ...mercator(value.lat, value.lon), zoom: PLACED_ZOOM } : unitedStatesCamera(VIEWPORT);
}

/**
 * A place's name, address and point, as decided for Leafen: the
 * point is picked on a small map — pressed, or the map's center with Enter —
 * with optional administrator-configured explicit search. The tile server's map or the bundled
 * outline lies under it, as in the map view. A new point keeps the name and
 * address typed.
 *
 * Laid out as Notion's place window (seen 2026-11-12, 1440 dark), 240 wide
 * over a table cell and as wide as the value on a row page: across its top
 * the field, here the name, with the "×" that clears the place; the map the
 * window's width less 8 and 168 high, 4px in; with no place yet, "현재
 * 위치" as a menu row. Leafen's own: the address under the name, and a line telling
 * how the map takes the point.
 */
export function PlacePicker({ value, onChange, onRemove, search, isCurrent = () => true }: {
 search?: (query:string,signal:AbortSignal)=>Promise<PlaceSearchResponse>;
 isCurrent?: ()=>boolean;
  value: PlacePropertyValue | null;
  onChange: (value: PlacePropertyValue) => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation(["propertyCell", "mapView"]);
  const config = useRuntimeMapConfig();
  const tileConfig=config.map;
  const [name, setName] = useState(value?.name ?? "");
  const [address, setAddress] = useState(value?.address ?? "");
  const [camera, setCamera] = useState(() => startCamera(value));
  const [viewport, setViewport] = useState(VIEWPORT);
  const mapRef = useRef<HTMLDivElement>(null);
  const press = useRef<{ pointerId: number; left: number; top: number; moved: boolean } | null>(null);

  useEffect(() => {
    const element = mapRef.current;
    if (!element) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = element.getBoundingClientRect();
      const delta = Math.max(-1, Math.min(1, -event.deltaY / 240));
      setCamera((previous) => zoomCamera(previous, delta, { left: event.clientX - rect.left, top: event.clientY - rect.top }, { width: rect.width, height: rect.height }));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    // The map is as wide as its window less 8, the camera keeping its middle.
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) setViewport((previous) => previous.width === width && previous.height === height ? previous : { width, height });
    });
    observer?.observe(element);
    return () => {
      element.removeEventListener("wheel", onWheel);
      observer?.disconnect();
    };
  }, []);

  const text = (field: string) => field.trim() || null;

  function pick(left: number, top: number) {
    const { lat, lon } = inverseMercator(screenToWorld(left, top, camera, viewport));
    onChange({ lat: Number(lat.toFixed(6)), lon: Number(lon.toFixed(6)), name: text(name), address: text(address) });
  }

  // The name and address are kept with a point once there is one.
  function keepText() {
    if (!value || (text(name) === (value.name ?? null) && text(address) === (value.address ?? null))) return;
    onChange({ ...value, name: text(name), address: text(address) });
  }

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || (event.target as HTMLElement).closest("button")) return;
    press.current = { pointerId: event.pointerId, left: event.clientX, top: event.clientY, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const active = press.current;
    if (!active || active.pointerId !== event.pointerId) return;
    const dx = event.clientX - active.left;
    const dy = event.clientY - active.top;
    if (!active.moved && Math.hypot(dx, dy) < CLICK_SLOP) return;
    press.current = { ...active, left: event.clientX, top: event.clientY, moved: true };
    setCamera((previous) => panCamera(previous, dx, dy));
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    const active = press.current;
    if (!active || active.pointerId !== event.pointerId) return;
    press.current = null;
    if (active.moved) return;
    const rect = event.currentTarget.getBoundingClientRect();
    pick(event.clientX - rect.left, event.clientY - rect.top);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.target !== event.currentTarget) return;
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [60, 0], ArrowRight: [-60, 0], ArrowUp: [0, 60], ArrowDown: [0, -60],
    };
    const center = { left: viewport.width / 2, top: viewport.height / 2 };
    if (moves[event.key]) setCamera(panCamera(camera, ...moves[event.key]));
    else if (event.key === "+" || event.key === "=") setCamera(zoomCamera(camera, 1, center, viewport));
    else if (event.key === "-" || event.key === "_") setCamera(zoomCamera(camera, -1, center, viewport));
    else if (fieldEnter(event) || event.key === " ") pick(center.left, center.top);
    else return;
    event.preventDefault();
  }

  const pin = value ? projectToScreen(mercator(value.lat, value.lon), camera, viewport) : null;
  const center = { left: viewport.width / 2, top: viewport.height / 2 };
  return (
    <div className={styles.picker} data-place-picker>
      <PlaceSearchControls search={search} enabled={config.placeSearchEnabled} isCurrent={isCurrent} located={Boolean(value)}
        onPick={(next)=>{setName(next.name??"");setAddress(next.address??"");setCamera(startCamera(next));onChange(next);}}>
        <div className={styles.band}>
          <input className={styles.bandInput} value={name} placeholder={t("propertyCell:placeName")} aria-label={t("propertyCell:placeName")}
            onChange={(event) => setName(event.currentTarget.value)} onBlur={keepText}
            onKeyDown={(event) => { if (fieldEnter(event)) keepText(); }} />
          {value ? (
            <button type="button" className={styles.clear} aria-label={t("propertyCell:placeRemove")} onClick={onRemove}>
              <XMarkIcon size="small" aria-hidden="true" />
            </button>
          ) : null}
        </div>
        <div className={styles.band}>
          <input className={styles.bandInput} value={address} placeholder={t("propertyCell:placeAddress")} aria-label={t("propertyCell:placeAddress")}
            onChange={(event) => setAddress(event.currentTarget.value)} onBlur={keepText}
            onKeyDown={(event) => { if (fieldEnter(event)) keepText(); }} />
        </div>
        <div className={styles.mapRoom}>
          <div
            ref={mapRef}
            className={styles.map}
            role="application"
            aria-roledescription={t("mapView:roleDescription")}
            aria-label={t("propertyCell:placeMap")}
            tabIndex={0}
            data-place-map
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={() => { press.current = null; }}
            onKeyDown={onKeyDown}
          >
            <MapBaseLayer camera={camera} viewport={viewport} config={tileConfig} />
            {pin ? <span className={mapStyles.pin} style={{ left: `${pin.left}px`, top: `${pin.top}px` }} aria-hidden="true" data-place-pin><span /></span> : null}
            <span className={styles.crosshair} style={{ left: `${center.left}px`, top: `${center.top}px` }} aria-hidden="true" />
            <div className={styles.zoom} data-place-zoom>
              <button type="button" aria-label={t("mapView:zoomIn")} onClick={() => setCamera(zoomCamera(camera, 1, center, viewport))}><Plus size="small" aria-hidden="true" /></button>
              <button type="button" aria-label={t("mapView:zoomOut")} onClick={() => setCamera(zoomCamera(camera, -1, center, viewport))}><MinusIcon size="small" aria-hidden="true" /></button>
            </div>
          </div>
        </div>
        <p className={styles.hint}>{value ? t("propertyCell:placeMoveHint") : t("propertyCell:placePickHint")}</p>
      </PlaceSearchControls>
    </div>
  );
}
