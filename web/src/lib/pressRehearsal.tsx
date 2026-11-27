import { createContext, startTransition, useContext, useLayoutEffect, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterStart } from "./startSettled";

// A part a press draws in the press's own frame (the block menu) is, the first
// time, code the page has never run and rows its fonts have never laid out:
// on a slow device the block menu's first click took 65 ms, 51 of its script
// and 19 of layout, where every later one took 22–27 (2026-11-13, Chrome's
// CPU ×2), the gap all first-time work. Once the page's start has settled
// (startSettled.ts) such a part is drawn once out of sight and taken away, so
// a person's first press is like their next. The rehearsal is its own React
// root in a box off the screen that takes no pointer and is read by no screen
// reader; it is drawn as a transition, in slices a press goes before; a part
// drawn in it takes no layer, focus or listener of its own (usePressRehearsal)
// and draws into the box instead of the page's body.
const RehearsalHost = createContext<HTMLElement | null>(null);

/** The box a rehearsal draws into, or null when the part is drawn for a person. */
export function usePressRehearsal(): HTMLElement | null {
  return useContext(RehearsalHost);
}

const rehearsed = new Set<string>();

function Drawn({ host, children, onDrawn }: { host: HTMLElement; children: ReactNode; onDrawn: () => void }) {
  // Laid out as it is committed, so its rows' words are shaped now.
  useLayoutEffect(() => {
    void host.offsetWidth;
    onDrawn();
  }, [host, onDrawn]);
  return children;
}

/** Draws `draw()` once out of sight after the start settles, once per `key` for the page's life. */
export function rehearseAfterStart(key: string, draw: () => ReactNode) {
  if (typeof document === "undefined" || rehearsed.has(key)) return;
  rehearsed.add(key);
  afterStart(() => {
    const host = document.createElement("div");
    host.setAttribute("aria-hidden", "true");
    host.inert = true;
    host.setAttribute("data-press-rehearsal", key);
    host.style.cssText = "position:fixed;inset:0;transform:translateX(-300vw);opacity:0;pointer-events:none;contain:strict";
    document.body.append(host);
    const root = createRoot(host);
    const done = () => window.setTimeout(() => {
      root.unmount();
      host.remove();
    }, 0);
    startTransition(() => root.render(
      <RehearsalHost.Provider value={host}><Drawn host={host} onDrawn={done}>{draw()}</Drawn></RehearsalHost.Provider>,
    ));
  });
}
