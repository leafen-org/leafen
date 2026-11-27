"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { triggerBrowserLinkDownload } from "@/lib/browserFileDownload";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { CollapseIcon, DownloadCircleIcon, type GlyphFrame } from "@/icons/leafen";
import { DividerIcon, Plus } from "./icons";
import { keepRightPress } from "@/lib/desktopTextMenu";
import styles from "./ImagePreviewDialog.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { CoverCredit } from "./page/CoverCredit";

// Notion's picture view zooms 50% a press, from 50% to 200%, the press past
// either end greyed (measured 2026-11-15: + 100 → 150 → 200, − 200 → 150 →
// 100 → 50).
const minimumZoom = 50;
const maximumZoom = 200;
const zoomStep = 50;

// Where Notion paints its bar's marks in their 20px boxes (its dashLong,
// arrowInCircleDown and arrowCollapse, measured 2026-11-15 at 1440): a dash
// 10.6 by 1, a circle 15.3 across, the way out 13.5 across. Its "+" is
// Leafen's already (12.8).
const ZOOM_OUT_MARK: GlyphFrame = { drawn: [2.5, 9.375, 15, 1.25], notion: [4.71, 9.5, 10.59, 1] };
const DOWNLOAD_MARK: GlyphFrame = { drawn: [1.875, 1.875, 16.25, 16.25], notion: [2.37, 2.37, 15.26, 15.26] };
const CLOSE_MARK: GlyphFrame = { drawn: [3.125, 3.125, 13.75, 13.75], notion: [3.24, 3.24, 13.53, 13.53] };

export function ImagePreviewDialog({
  src,
  alt,
  label,
  closeLabel,
  downloadLabel,
  downloadText,
  toolbarLabel,
  zoomInLabel,
  zoomOutLabel,
  zoomLevelLabel,
  restoreFocusOnClose = true,
  onClose,
  onDownload,
}: {
  src: string;
  alt: string;
  label: string;
  closeLabel: string;
  /** Notion's view has no way to open the original (seen 2026-11-05); kept for callers. */
  openLabel?: string;
  openText?: string;
  downloadLabel: string;
  downloadText: string;
  toolbarLabel: string;
  zoomInLabel: string;
  zoomOutLabel: string;
  zoomLevelLabel: string;
  restoreFocusOnClose?: boolean;
  onClose: () => void;
  onOpen?: () => void;
  onDownload?: () => void;
}) {
  const backdropRef = useRef<HTMLDivElement>(null);
  const [zoom, setZoom] = useState(100);
  // The zoom typed in its field while the field is open, null while shut;
  // kept in a ref as well for the field's own events of the same moment.
  const [typedZoom, setTypedZoom] = useState<string | null>(null);
  const typed = useRef<string | null>(null);
  const zoomField = useRef<HTMLInputElement>(null);
  // Its controls show while the pointer moves and go after it rests, as
  // Notion's (seen 2026-11-05); a key's focus in them keeps them.
  const [controlsShown, setControlsShown] = useState(false);
  const controlsTimer = useRef<number | null>(null);
  const showControls = () => {
    setControlsShown(true);
    if (controlsTimer.current !== null) window.clearTimeout(controlsTimer.current);
    controlsTimer.current = window.setTimeout(() => setControlsShown(false), 2000);
  };
  useEffect(() => () => { if (controlsTimer.current !== null) window.clearTimeout(controlsTimer.current); }, []);

  function changeZoom(delta: number) {
    setZoom((current) => Math.max(minimumZoom, Math.min(maximumZoom, current + delta)));
  }

  // A zoomed picture moves while it is dragged, as Notion's (seen 2026-11-15
  // at 1440, by mouse and by a drag in two ways): at zoom z its shift is the
  // pointer's place over z less the window's middle, each way held within
  // (z − 1) times that middle — at 150% a drag pressed at the middle jumps
  // it 240 left and 120 up, one ending at (620, 361) leaves it at −306.67,
  // −120.33. A new zoom sets it back in the middle.
  const [pan, setPan] = useState({ x: 0, y: 0 });
  useEffect(() => { setPan({ x: 0, y: 0 }); }, [zoom]);
  function dragImage(x: number, y: number) {
    const from = pressedAt.current;
    if (!from || zoom <= 100 || Math.hypot(x - from.x, y - from.y) <= 4) return;
    const scale = zoom / 100;
    // The shift counts from the middle's whole pixel, the hold from the middle itself (723 high: 361 and 361.5).
    const middleX = window.innerWidth / 2, middleY = window.innerHeight / 2;
    const held = (value: number, middle: number) => Math.max(-(scale - 1) * middle, Math.min((scale - 1) * middle, value));
    setPan({ x: held(x / scale - Math.floor(middleX), middleX), y: held(y / scale - Math.floor(middleY), middleY) });
  }

  // A press on the picture zooms it in a step from 100%, and back to 100%
  // from any other zoom, as Notion's (seen 2026-11-15 in two tabs: 100 →
  // 150, 150, 200 and 50 → 100); a drag over it keeps the zoom (150 stayed).
  const pressedAt = useRef<{ x: number; y: number } | null>(null);
  function pressImage(x: number, y: number) {
    const from = pressedAt.current;
    pressedAt.current = null;
    if (from && Math.hypot(x - from.x, y - from.y) > 4) return;
    setZoom((current) => current === 100 ? 100 + zoomStep : 100);
  }

  /**
   * Its zoom pressed opens a field with the number chosen whole, as Notion's
   * (seen 2026-11-15 in two tabs, at 100% too): Enter or leaving the field
   * takes a number typed there, held between 50% and 200% (500 is 200, 10 is
   * 50), and keeps the zoom for anything else; Escape keeps it and leaves the
   * view open.
   */
  function typeZoom(text: string | null) {
    typed.current = text;
    setTypedZoom(text);
  }
  function closeZoomField(take: boolean) {
    const text = typed.current?.trim();
    if (text === undefined) return;
    typeZoom(null);
    const asked = Number(text);
    if (take && text && Number.isFinite(asked)) setZoom(Math.max(minimumZoom, Math.min(maximumZoom, Math.round(asked))));
  }
  const zoomFieldOpen = typedZoom !== null;
  useEffect(() => {
    if (!zoomFieldOpen) return;
    zoomField.current?.focus();
    zoomField.current?.select();
  }, [zoomFieldOpen]);

  // A modal layer: the page stays still, Escape closes it while nothing
  // opened over it takes the key, and the focus goes back where asked. It
  // takes no focus as it opens, and Tab moves none while it is open — the
  // focus stays off it, on the page's body, and its controls are no Tab
  // stops — as Notion's (seen 2026-11-26 in Chrome 1440, two tabs, no script
  // before the keys: the focus on the body as it opened and after Tab).
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: backdropRef, modalOwned: () => [backdropRef.current], restoreFocus: restoreFocusOnClose, onEscape: () => onClose(),
    initialFocus: "opener",
  });
  useEffect(() => {
    // Ahead of the modal's Tab ring (overlayLayer), which leaves a prevented key.
    const stay = (event: KeyboardEvent) => { if (event.key === "Tab" && !event.altKey && !event.ctrlKey && !event.metaKey) event.preventDefault(); };
    document.addEventListener("keydown", stay, true);
    return () => document.removeEventListener("keydown", stay, true);
  }, []);

  const dialog = (
    // The backdrop click is a pointer shortcut; the close button and Escape
    // provide equivalent keyboard paths.
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      ref={backdropRef}
      className={styles.backdrop}
      style={{ zIndex }}
      role="dialog"
      aria-modal="true"
      aria-label={label}
      data-attachment-image-preview
      data-image-preview-zoom={zoom}
      tabIndex={-1}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      onPointerMove={showControls}
      onAuxClick={(event) => event.stopPropagation()}
      onContextMenu={keepRightPress}
    >
      <div
        className={styles.toolbar}
        role="group"
        aria-label={toolbarLabel}
        data-attachment-image-preview-actions
        data-image-preview-toolbar
        data-shown={controlsShown ? "true" : undefined}
      >
        <button
          type="button"
          className={styles.control}
          aria-label={zoomOutLabel}
          data-image-preview-control="zoom-out"
          disabled={zoom === minimumZoom}
          onClick={() => changeZoom(-zoomStep)}
        >
          <DividerIcon size="regular" frame={ZOOM_OUT_MARK} aria-hidden="true" />
        </button>
        {typedZoom === null ? (
          <button type="button" className={`${styles.control} ${styles.zoom}`} aria-label={zoomLevelLabel}
            data-image-preview-control="zoom-level" onClick={() => typeZoom(String(zoom))}>
            {zoom}%
          </button>
        ) : (
          <span className={styles.zoomField} data-image-preview-control="zoom-level">
            <input ref={zoomField} className={styles.zoomInput} value={typedZoom} aria-label={zoomLevelLabel} spellCheck={false}
              onChange={(event) => typeZoom(event.currentTarget.value)}
              onBlur={() => closeZoomField(true)}
              onKeyDown={(event) => {
                if (imeComposing(event) || (!fieldEnter(event) && event.key !== "Escape")) return;
                // Escape shuts the field alone, the view staying open (the overlay leaves a prevented key).
                event.preventDefault();
                closeZoomField(fieldEnter(event));
                // The focus goes to the view itself, so a key typed next lands nowhere, as Notion's (its page's body).
                backdropRef.current?.focus();
              }} />
            <span aria-hidden="true">%</span>
          </span>
        )}
        <button
          type="button"
          className={styles.control}
          aria-label={zoomInLabel}
          data-image-preview-control="zoom-in"
          disabled={zoom === maximumZoom}
          onClick={() => changeZoom(zoomStep)}
        >
          <Plus size="regular" aria-hidden="true" />
        </button>
        <span className={styles.separator} aria-hidden="true" />
        {/* Saved the one way downloads go (lib/browserFileDownload): an app's
            window, which a bare download link leaves doing nothing, saves it too. */}
        <button type="button" className={styles.control} aria-label={downloadLabel} title={downloadText}
          data-image-preview-control="download" onClick={onDownload ?? (() => { void triggerBrowserLinkDownload(src, alt); })}>
          <DownloadCircleIcon size="regular" frame={DOWNLOAD_MARK} aria-hidden="true" /></button>
        <span className={styles.separator} aria-hidden="true" />
        {/* Notion's way out is the last control; it has no × (seen 2026-11-05). */}
        <button type="button" className={styles.control} aria-label={closeLabel} title={closeLabel}
          data-image-preview-control="close" onClick={onClose}><CollapseIcon size="regular" frame={CLOSE_MARK} aria-hidden="true" /></button>
      </div>
      {/* A photo from Unsplash names its photographer and Unsplash in the window's top end corner while the
          controls show (CoverCredit). */}
      <CoverCredit cover={src} place="top" shown={controlsShown} />
      {/* A pointer shortcut: the zoom buttons are the keyboard's way. */}
      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/click-events-have-key-events */}
      <img
        className={styles.image}
        src={src}
        alt={alt}
        data-image-preview-image
        style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom / 100})` }}
        draggable={false}
        onPointerDown={(event) => {
          pressedAt.current = { x: event.clientX, y: event.clientY };
          event.currentTarget.setPointerCapture?.(event.pointerId);
        }}
        onPointerMove={(event) => { if (event.buttons & 1) dragImage(event.clientX, event.clientY); }}
        onClick={(event) => pressImage(event.clientX, event.clientY)}
      />
    </div>
  );

  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
