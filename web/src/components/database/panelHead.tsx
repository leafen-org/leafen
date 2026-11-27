import { createContext, type ReactNode, useContext, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal, flushSync } from "react-dom";
import styles from "./DatabaseViewSettingsPanel.module.css";
import footStyles from "./panelFoot.module.css";
import { useEdgeFadeAsSized } from "@/lib/edgeFade";

/**
 * The docked view settings panel's 42px head, over a body that scrolls on
 * its own with its scrollbar under the head, as Notion's every screen (seen
 * 2026-11-06, 11-07: head 166–208, the body from 208).
 */
export const PanelHeadContext = createContext<HTMLElement | null>(null);
/** The popover panel's foot under its body, which stays while the body scrolls (PanelFoot). */
const PanelFootContext = createContext<HTMLElement | null>(null);

/**
 * The docked panel's head and what goes under it (its body), the head kept
 * here: given as it is first drawn, it draws again the headings shown in it,
 * not the panel holding them — held by the panel, it drew the view settings'
 * 126 parts once more as they opened (2026-11-14, NAS condition, CPU ×2).
 */
export function PanelHeadBox({ children }: { children: ReactNode }) {
  const [head, setHead] = useState<HTMLDivElement | null>(null), [foot, setFoot] = useState<HTMLDivElement | null>(null);
  // The body under it (`[data-edge-fade]`) fades at each end it scrolls on
  // past, 1.7px a pixel still to scroll there, 34px at most, sharp where
  // nothing is past it, as Notion's every screen (seen 2026-11-17 at 1440,
  // its mask read at each place).
  const body = useMemo(() => ({ current: head?.parentElement?.querySelector<HTMLElement>(":scope > [data-panel-body]") ?? null }), [head]);
  useEdgeFadeAsSized(body, 1.7);
  return (
    <PanelHeadContext.Provider value={head}>
      <PanelFootContext.Provider value={foot}>
        <div ref={setHead} className={styles.panelHead} />
        {children}
        <div ref={setFoot} className={footStyles.panelFoot} data-panel-foot />
      </PanelFootContext.Provider>
    </PanelHeadContext.Provider>
  );
}

type Place = { top: number; left: number; width: number };

/**
 * A settings screen's heading. In the docked panel it shows in the head,
 * where it would sit with the body unscrolled, and keeps that place in the
 * body unseen, so what follows sits as it did; elsewhere it shows in place.
 *
 * Its place is read by one observer, once the frame has laid the panel out
 * and again as the heading's box changes, and drawn before that frame
 * paints. Read as each draw ended, it made the browser work out the newly
 * drawn panel's styles early, 184 elements at the view settings press, and
 * again at every draw of the panel (2026-11-14, NAS condition, CPU ×2).
 */
export function PanelHeading({ children }: { children: ReactNode }) {
  const head = useContext(PanelHeadContext);
  const keptRef = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<Place | null>(null);
  useLayoutEffect(() => {
    const kept = keptRef.current;
    const body = kept?.closest<HTMLElement>("[data-panel-body]");
    if (!head || !kept || !body) return undefined;
    const observer = new ResizeObserver(() => {
      const box = kept.getBoundingClientRect(), origin = head.getBoundingClientRect();
      const next = { top: box.top - origin.top + body.scrollTop, left: box.left - origin.left, width: box.width };
      flushSync(() => setPlace((current) => current && current.top === next.top && current.left === next.left && current.width === next.width ? current : next));
    });
    observer.observe(kept);
    return () => observer.disconnect();
  }, [head]);
  if (!head) return <>{children}</>;
  return (
    <>
      <div ref={keptRef} className={styles.headingKept} aria-hidden="true" inert>{children}</div>
      {createPortal(
        <div className={styles.headingShown} style={place ?? { visibility: "hidden" }} data-panel-heading>{children}</div>,
        head,
      )}
    </>
  );
}

/** Whether a screen's PanelFoot shows in the popover's foot (true) or in place (docked, or a phone's sheet). */
export function usePanelFootShown() {
  return Boolean(useContext(PanelFootContext)?.closest('[data-popover="true"]'));
}

/**
 * What a screen keeps under its body: in a popover, in its foot, which stays while the body scrolls over it, as
 * Notion's sprints' "스프린트 켜기" (seen 2026-11-22 at 1440 dark, dpr 1: in a 480 window at 431..463 of a panel at
 * 83..467, its body scrolling above it); docked or on a phone, in place.
 */
export function PanelFoot({ children }: { children: ReactNode }) {
  const foot = useContext(PanelFootContext);
  return foot?.closest('[data-popover="true"]') ? createPortal(children, foot) : <>{children}</>;
}
