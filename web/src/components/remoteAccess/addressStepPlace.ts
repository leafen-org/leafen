import { type RefObject, useCallback, useEffect, useLayoutEffect, useRef } from "react";
import { usePersonMoved } from "@/lib/personMoved";

/**
 * Where the outside-address step (RemoteAddressChoice) shows from as its
 * views replace each other, in the first run's window and Settings' dialog
 * alike. The first run's screen scrolls in its panel's <main>
 * (FirstRunScreen), the dialog in itself.
 *
 * Only "나중에" can be pressed while the network is checked — at the foot,
 * under the example — so the focus lands there. When the choices come, Enter
 * must take the recommended one, not skip, and the step shows from its top,
 * where the question is, unless the person moved it meanwhile: they keep
 * their place (2026-11-19, personMoved.ts).
 *
 * When the check ends on the screen of a network where Tailscale can't be
 * used (RemoteAddressOwnWay), its layout is another and shorter: "나중에" and
 * the line under it would jump up from under a pointer on its way to them
 * (2026-11-19). Seen when the check ended, "나중에" stays where it stood on
 * the screen: the own way's room grows above it, and "다른 방법" opens into
 * that room; out of sight, the shorter screen shows as it is. The returned
 * function notes where it stood, just before the step becomes "own".
 */
export function useAddressStepPlace(step: string, screen: boolean, dialog: RefObject<HTMLElement | null>,
  firstChoice: RefObject<HTMLButtonElement | null>): () => void {
  const personMoved = usePersonMoved(step === "checking");
  useEffect(() => {
    if (step !== "checking" && step !== "choose") return undefined;
    if (step === "choose") firstChoice.current?.focus({ preventScroll: true });
    if (step === "choose" && personMoved()) return undefined;
    const top = () => (screen ? firstChoice.current?.closest("main") : dialog.current)?.scrollTo({ top: 0 });
    top();
    // The entry focus lands a frame later (useViewEntryFocus) and scrolls to "나중에".
    let second = 0;
    const first = window.requestAnimationFrame(() => { second = window.requestAnimationFrame(top); });
    return () => { window.cancelAnimationFrame(first); window.cancelAnimationFrame(second); };
  }, [dialog, firstChoice, personMoved, screen, step]);

  const held = useRef<{ region: HTMLElement; top: number | null } | null>(null);
  const hold = useCallback(() => {
    const region = screen ? firstChoice.current?.closest("main") : dialog.current;
    const later = region?.querySelector<HTMLElement>(LATER);
    if (!region || !later) return;
    const place = later.getBoundingClientRect();
    const view = region.getBoundingClientRect();
    const seen = place.bottom > Math.max(view.top, 0) && place.top < Math.min(view.bottom, window.innerHeight);
    held.current = { region, top: seen ? place.top : null };
  }, [dialog, firstChoice, screen]);
  useLayoutEffect(() => {
    const was = held.current;
    held.current = null;
    if (step !== "own" || !was?.region.isConnected) return;
    const later = was.region.querySelector<HTMLElement>(LATER);
    const room = later?.closest<HTMLElement>("[data-remote-own-way]");
    if (!later || !room) return;
    if (was.top !== null) {
      // The region's scroll has already come back within the shorter content.
      const up = was.top - later.getBoundingClientRect().top;
      if (up > 0) room.style.minBlockSize = `${room.getBoundingClientRect().height + up}px`;
      const off = later.getBoundingClientRect().top - was.top;
      if (Math.abs(off) >= 1) was.region.scrollTop += off;
    }
    // The own way's first choice takes the focus without scrolling, before the entry focus would.
    if (!room.contains(document.activeElement)) room.querySelector<HTMLElement>("button")?.focus({ preventScroll: true });
  }, [step]);
  return hold;
}

const LATER = '[data-remote-choice="later"]';
