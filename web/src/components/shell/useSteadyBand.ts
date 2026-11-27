import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { layerOpenOver } from "@/lib/overlayLayer";

/** Marks the band across the top of the app (AppShell). */
const APP_BAND_ATTRIBUTE = "data-app-band";
// How long after a press in the band its answer may still change the band.
const BAND_PRESS_MS = 3000;

/**
 * A band that only advises (a backup off or failing, a security release, an
 * address to move to) comes in and goes with a page: a page once drawn keeps
 * its place, as Notion's does, and such a band found after it was drawn
 * waits for the next page, or for the page to be out of sight (2026-11-06:
 * the backup band came a moment after the page and pushed it 40px down, and
 * a press on the page menu landed on nothing; 2026-11-15: it still did on a
 * reload a person only read, with no press to hold it back). It follows at
 * once when the press was in the band itself (its close, its button), and
 * while a window is open over the page (a save in settings takes it away), as
 * nothing on the page is being pressed or read then. A band saying changes
 * aren't kept is not one of these: it comes at once.
 */
export function useSteadyBand(notice: ReactNode, pathname: string): ReactNode {
  const wanted = Boolean(notice);
  const [shown, setShown] = useState(wanted);
  const wantedRef = useRef(wanted);
  wantedRef.current = wanted;
  // What the band last said, for a band whose going waits.
  const last = useRef<ReactNode>(notice);
  if (notice) last.current = notice;
  // When the person last pressed in the band: what that press does (its close
  // taking the band away) follows at once, and only that.
  const pressedBandAt = useRef(0);

  useEffect(() => {
    const press = (event: Event) => {
      const band = document.querySelector(`[${APP_BAND_ATTRIBUTE}]`);
      pressedBandAt.current = event.target instanceof Node && band?.contains(event.target) ? Date.now() : 0;
    };
    const hidden = () => { if (document.visibilityState === "hidden") setShown(wantedRef.current); };
    const kinds = ["pointerdown", "keydown"] as const;
    for (const kind of kinds) window.addEventListener(kind, press, { capture: true, passive: true });
    document.addEventListener("visibilitychange", hidden);
    return () => {
      for (const kind of kinds) window.removeEventListener(kind, press, { capture: true });
      document.removeEventListener("visibilitychange", hidden);
    };
  }, []);

  useEffect(() => {
    if (wanted === shown) return;
    const page = document.getElementById("app-main-content");
    const fromBand = Date.now() - pressedBandAt.current < BAND_PRESS_MS;
    if (fromBand || document.visibilityState === "hidden" || Boolean(page && layerOpenOver(page))) {
      pressedBandAt.current = 0;
      setShown(wanted);
    }
  }, [wanted, shown]);

  // A new page is drawn anew, band and all, before it is painted: the band
  // doesn't come a frame after the page it moves down.
  useLayoutEffect(() => {
    setShown(wantedRef.current);
  }, [pathname]);

  if (!shown) return null;
  return notice ?? last.current;
}
