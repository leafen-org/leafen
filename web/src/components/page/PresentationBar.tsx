import { type ReactNode, type RefObject, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, PresentFullscreenExitIcon, PresentFullscreenIcon, X } from "@/icons/leafen";
import styles from "./PresentationBar.module.css";

const CONTROLS_IDLE_MS = 2000;

/**
 * Whether a presentation's bar shows: as it opens and while the pointer
 * moves, gone 2s after the pointer rests unless it rests on the bar, as
 * Notion's (seen 2026-10-12 for a page, 2026-11-12 for a PowerPoint file).
 */
export function usePresentationBar() {
  const [shown, setShown] = useState(true);
  const over = useRef(false), idle = useRef(0);
  const rest = useCallback(() => {
    window.clearTimeout(idle.current);
    idle.current = window.setTimeout(() => { if (!over.current) setShown(false); }, CONTROLS_IDLE_MS);
  }, []);
  const show = useCallback(() => {
    setShown(true);
    rest();
  }, [rest]);
  const hide = useCallback(() => setShown(false), []);
  useEffect(() => {
    rest();
    return () => window.clearTimeout(idle.current);
  }, [rest]);
  return { shown, show, hide, over };
}

/**
 * A presentation's bar at the window's foot, as Notion's (a page's and a
 * PowerPoint file's, seen 2026-11-09 and 2026-11-12): ‹, "N장 중 M번째
 * 슬라이드", ›, a line, what the presentation adds (a page's slide link),
 * full screen — the whole window's, "전체 화면 종료" while it is — and ×.
 */
export function PresentationBar({ shown, over, onShow, index, total, onGo, onExit, children }: {
  shown: boolean; over: RefObject<boolean>; onShow: () => void;
  /** The slide in view, from 0, of total; none yet while the slides are read. */
  index: number; total: number;
  onGo: (index: number) => void; onExit: () => void;
  children?: ReactNode;
}) {
  const { t } = useTranslation("presentation");
  const [fullscreen, setFullscreen] = useState(() => Boolean(document.fullscreenElement));
  useEffect(() => {
    const follow = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", follow);
    return () => document.removeEventListener("fullscreenchange", follow);
  }, []);
  function toggleFullscreen() {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    else void document.documentElement.requestFullscreen?.().catch(() => undefined);
  }
  return <div className={styles.controls} data-shown={shown ? "true" : undefined}
    onMouseEnter={() => { over.current = true; onShow(); }}
    onMouseLeave={() => { over.current = false; }}>
    <div className={styles.bar} data-presentation-controls>
      <button type="button" className={styles.button} aria-label={t("previous")} title={t("previous")} disabled={index === 0}
        onClick={() => onGo(index - 1)}><ChevronLeft size="small" aria-hidden="true" /></button>
      <span className={styles.count} data-presentation-count>{total ? t("count", { current: index + 1, total }) : ""}</span>
      <button type="button" className={styles.button} aria-label={t("next")} title={t("next")} disabled={index >= total - 1}
        onClick={() => onGo(index + 1)}><ChevronRight size="small" aria-hidden="true" /></button>
      <span className={styles.separator} aria-hidden="true" />
      {children}
      <button type="button" className={styles.button} aria-label={t(fullscreen ? "exitFullscreen" : "fullscreen")}
        title={t(fullscreen ? "exitFullscreen" : "fullscreen")} onClick={toggleFullscreen}>
        {fullscreen ? <PresentFullscreenExitIcon size="small" aria-hidden="true" /> : <PresentFullscreenIcon size="small" aria-hidden="true" />}
      </button>
      <button type="button" className={styles.button} aria-label={t("exit")} title={t("exitHint")}
        onClick={onExit}><X size="small" aria-hidden="true" /></button>
    </div>
  </div>;
}

/** The class a presentation's own extra bar button takes (a page's slide link). */
export const presentationBarButton = styles.button;
