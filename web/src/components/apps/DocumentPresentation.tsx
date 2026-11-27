import { type KeyboardEvent, useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PresentationBar, usePresentationBar } from "@/components/page/PresentationBar";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { DocumentPreviewFrame } from "./DocumentPreview";
import styles from "./DocumentPresentation.module.css";
import { imeComposing } from "@/lib/fieldKeyBoundary";

type PresentedFile = { name: string; read: () => Promise<ArrayBuffer> };
let presented: PresentedFile | null = null;
let version = 0;
const listeners = new Set<() => void>();
const publish = (file: PresentedFile | null) => {
  presented = file;
  version += 1;
  for (const listener of listeners) listener();
};

/** Presents a PowerPoint file over the window, from its first slide. */
export function openDocumentPresentation(file: PresentedFile) {
  publish(file);
}

/** Where a presentation is drawn, outside the page it was opened from, so its keys and presses stay its own. */
export function DocumentPresentationHost() {
  useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  }, () => version, () => 0);
  useEffect(() => () => publish(null), []);
  return presented ? <DocumentPresentation key={version} name={presented.name} read={presented.read} onClose={() => publish(null)} /> : null;
}

const NEXT_KEYS = new Set(["ArrowRight", " ", "PageDown"]);
const BACK_KEYS = new Set(["ArrowLeft", "PageUp"]);

/**
 * A PowerPoint file's "프레젠테이션 모드", as Notion's from its embed (seen
 * 2026-11-12, 1440 dark): over the whole window, black, one slide whole in its
 * middle, from the first, the window left as it was (a page's presentation
 * fills the screen as it starts; this one doesn't). → Space PageDown show
 * the next, ← PageUp the one before; a press on the slide turns nothing. The
 * page presentation's bar sits at the foot without its slide link, shown as
 * the pointer moves; Escape or × ends it, leaving full screen too.
 */
function DocumentPresentation({ name, read, onClose }: { name: string; read: () => Promise<ArrayBuffer>; onClose: () => void }) {
  const { t } = useTranslation("presentation");
  const root = useRef<HTMLDivElement>(null);
  const [total, setTotal] = useState(0), [index, setIndex] = useState(0);
  const bar = usePresentationBar();
  const end = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    onClose();
  }, [onClose]);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: root, modalOwned: () => [root.current], restoreFocus: true,
    initialFocus: root, onEscape: end });
  useEffect(() => () => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
  }, []);

  const go = useCallback((next: number) => setIndex(Math.max(0, Math.min(next, total - 1))), [total]);
  const turn = (key: string) => {
    if (NEXT_KEYS.has(key)) go(index + 1);
    else if (BACK_KEYS.has(key)) go(index - 1);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.altKey || event.ctrlKey || event.metaKey || imeComposing(event)) return;
    // Space on one of the bar's buttons presses that button.
    if (event.key === " " && event.target instanceof HTMLButtonElement) return;
    if (!NEXT_KEYS.has(event.key) && !BACK_KEYS.has(event.key)) return;
    event.preventDefault();
    turn(event.key);
  };

  return createPortal(
    <div ref={root} className={styles.presentation} style={{ zIndex }} role="dialog" aria-modal="true" aria-label={t("label", { title: name })}
      tabIndex={-1} data-document-presentation={index} onKeyDown={onKeyDown} onMouseMove={bar.show}>
      <DocumentPreviewFrame kind="pptx" title={name} read={read} className={styles.slides} failedClassName={styles.failed}
        onEscape={end} present={index} onFrameMessage={(message) => {
          // The frame draws the slides and hears the keys and moves made over them.
          if (message.type === "leafen:document-preview:slides" && typeof message.count === "number") setTotal(message.count);
          if (message.type === "leafen:document-preview:key" && typeof message.key === "string") turn(message.key);
          if (message.type === "leafen:document-preview:pointer") bar.show();
        }} />
      <PresentationBar shown={bar.shown} over={bar.over} onShow={bar.show} index={index} total={total} onGo={go} onExit={end} />
    </div>,
    document.body,
  );
}
