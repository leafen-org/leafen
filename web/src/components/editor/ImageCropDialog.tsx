import { Fragment, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { AspectRatioIcon, CheckIcon, ChevronDown } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { FULL_IMAGE_CROP, cropToAspect, imageCrop, type ImageCrop } from "./imageCropModel";
import styles from "./ImageCropDialog.module.css";

const RATIOS = ["free", "circle", "square", "5:4", "4:3", "7:5", "3:2", "5:3", "16:9"] as const;
const HANDLES = ["nw", "n", "ne", "e", "se", "s", "sw", "w"] as const;
type Handle = (typeof HANDLES)[number] | "move";

export function ImageCropDialog({ src, initial, onClose, onSave, busy = false, error, status }: {
  src: string; initial: unknown; onClose(): void; onSave(crop: ImageCrop): void;
  busy?: boolean; error?: string; status?: string;
}) {
  const { t } = useTranslation(["blockHandle", "common"]);
  const root = useRef<HTMLDivElement>(null), stage = useRef<HTMLDivElement>(null);
  const [crop, setCrop] = useState(imageCrop(initial) ?? FULL_IMAGE_CROP);
  const [ratio, setRatio] = useState<(typeof RATIOS)[number]>(crop.circle ? "circle" : "free");
  const [ratioOpen, setRatioOpen] = useState(false), [naturalRatio, setNaturalRatio] = useState(0);
  const gesture = useRef<(() => void) | null>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: root, modalOwned: () => [root.current],
    restoreFocus: true, onEscape: () => ratioOpen ? setRatioOpen(false) : onClose(), onOutsidePress: onClose });
  useEffect(() => { root.current?.querySelector<HTMLButtonElement>("button")?.focus(); return () => gesture.current?.(); }, []);
  function aspect(next = ratio) {
    if (next === "free") return 0;
    if (next === "circle" || next === "square") return 1;
    const [width, height] = next.split(":").map(Number); return width / height;
  }
  function choose(next: (typeof RATIOS)[number]) {
    if (busy) return;
    setRatio(next); setRatioOpen(false);
    if (next === "free") setCrop(({ circle: _circle, ...rest }) => rest);
    else if (naturalRatio) setCrop(value => cropToAspect(value, aspect(next), naturalRatio, next === "circle"));
  }
  function changed(start: ImageCrop, handle: Handle, dx: number, dy: number): ImageCrop {
    if (handle === "move") return { ...start, x: Math.max(0, Math.min(1 - start.width, start.x + dx)),
      y: Math.max(0, Math.min(1 - start.height, start.y + dy)) };
    let x = start.x, y = start.y, right = x + start.width, bottom = y + start.height;
    if (handle.includes("w")) x = Math.max(0, Math.min(right - .01, x + dx));
    if (handle.includes("e")) right = Math.min(1, Math.max(x + .01, right + dx));
    if (handle.includes("n")) y = Math.max(0, Math.min(bottom - .01, y + dy));
    if (handle.includes("s")) bottom = Math.min(1, Math.max(y + .01, bottom + dy));
    let next = { x, y, width: right - x, height: bottom - y, ...(ratio === "circle" ? { circle: true } : {}) };
    const fixed = aspect() / naturalRatio;
    if (fixed) {
      // Keep the opposite edge while preserving the chosen aspect.
      let width = handle === "n" || handle === "s" ? next.height * fixed : next.width;
      width = Math.max(Math.max(.01, .01 * fixed), Math.min(width, 1, fixed));
      const height = width / fixed;
      x = handle.includes("w") ? right - width : x;
      y = handle.includes("n") ? bottom - height : y;
      next = { ...next, x: Math.max(0, Math.min(1 - width, x)), y: Math.max(0, Math.min(1 - height, y)), width, height };
    }
    return next;
  }
  function start(handle: Handle, event: PointerEvent<HTMLElement>) {
    if (busy) return;
    event.preventDefault(); event.stopPropagation(); gesture.current?.(); setRatioOpen(false);
    const rect = stage.current?.getBoundingClientRect(); if (!rect?.width || !rect.height) return;
    const x = event.clientX, y = event.clientY, before = crop;
    const move = (next: globalThis.PointerEvent) => setCrop(changed(before, handle, (next.clientX - x) / rect.width, (next.clientY - y) / rect.height));
    const end = () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", end); gesture.current = null; };
    gesture.current = end; window.addEventListener("pointermove", move); window.addEventListener("pointerup", end);
  }
  function keys(handle: Handle, event: KeyboardEvent<HTMLElement>) {
    if (busy) return;
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) return;
    event.preventDefault(); event.stopPropagation();
    const step = event.shiftKey ? .05 : .01;
    setCrop(before => changed(before, handle, event.key === "ArrowLeft" ? -step : event.key === "ArrowRight" ? step : 0,
      event.key === "ArrowUp" ? -step : event.key === "ArrowDown" ? step : 0));
  }
  const label = t("blockHandle:media.crop");
  return createPortal(<div className={styles.backdrop} style={{ zIndex }}>
    <div ref={root} role="dialog" aria-modal="true" aria-label={label} className={styles.dialog} data-image-crop-dialog>
      <header className={styles.header}>
        <div className={styles.ratios}><button type="button" aria-label={t("blockHandle:crop.ratio")} aria-haspopup="menu"
          aria-expanded={ratioOpen} onClick={() => setRatioOpen(open => !open)}><AspectRatioIcon size="regular" aria-hidden="true" /><ChevronDown size="small" aria-hidden="true" /></button>
          {/* Notion's (seen 2026-11-15): 28px rows, the chosen one ticked, a rule after "자유형", and "Circle" and "Square" untranslated. */}
          {ratioOpen ? <div role="menu" className={styles.ratioMenu}>{RATIOS.map(value => <Fragment key={value}><button type="button"
            role="menuitemradio" aria-checked={ratio === value} onClick={() => choose(value)}>
            <span>{value.includes(":") ? value : t(`blockHandle:crop.${value}`)}</span>{ratio === value ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </button>{value === "free" ? <hr className={styles.ratioRule} /> : null}</Fragment>)}</div> : null}
        </div>
        <strong>{label}</strong><div className={styles.buttons}>
          <button type="button" onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="button" data-image-crop-save aria-busy={busy || undefined} disabled={!naturalRatio || busy} onClick={() => { if (imageCrop(crop)) onSave(crop); }}>{t("common:actions.save")}</button>
        </div>
      </header>
      <div className={styles.body}><div ref={stage} className={styles.stage}>
        <img src={src} alt={t("blockHandle:crop.original")} draggable={false}
          onLoad={event => setNaturalRatio(event.currentTarget.naturalWidth / event.currentTarget.naturalHeight)} />
        <div className={styles.selection} role="group" tabIndex={0} aria-label={t("blockHandle:crop.move")}
          data-image-crop-selection data-circle={crop.circle || undefined}
          style={{ left: `${crop.x * 100}%`, top: `${crop.y * 100}%`, width: `${crop.width * 100}%`, height: `${crop.height * 100}%`,
            backgroundImage: `url(${JSON.stringify(src)})`, backgroundSize: `${100 / crop.width}% ${100 / crop.height}%`,
            backgroundPosition: `${crop.width === 1 ? 0 : crop.x / (1 - crop.width) * 100}% ${crop.height === 1 ? 0 : crop.y / (1 - crop.height) * 100}%` }}
          onPointerDown={event => start("move", event)} onKeyDown={event => keys("move", event)}>
          {HANDLES.filter(handle => ratio === "free" || handle.length === 2).map(handle => <button key={handle} type="button"
            data-crop-handle={handle} className={styles.handle} aria-label={t(`blockHandle:crop.handles.${handle}`)}
            onPointerDown={event => start(handle, event)} onKeyDown={event => keys(handle, event)} />)}
        </div>
      </div></div>
      {status ? <div className={styles.status} role="status">{status}</div> : null}
      {error ? <div className={styles.status} role="alert" data-error>{error}</div> : null}
    </div>
  </div>, document.body);
}
