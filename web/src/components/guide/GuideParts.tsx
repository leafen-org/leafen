import { useLayoutEffect, useRef, type CSSProperties } from "react";
import { detectKeyboardShortcutPlatform } from "@/lib/keyboardShortcuts";
import styles from "./GuideParts.module.css";

// The parts every moving screen guide draws with: a scene places them in its
// picture and says where they go and when; how they look and move is here.

/**
 * Where the cursor's tip is `at` milliseconds into the scene: `x`/`y` pixels
 * from the centre of the element marked `data-guide-target={on}` in the same
 * screen, or, without `on`, of the element the cursor is placed in.
 */
export type GuideCursorStop = { at: number; on?: string; x?: number; y?: number };

// The arrow's tip sits about 3px right of and below its box's corner.
const TIP = 3;
// A hand moving a mouse: quick at first, settling on the target.
const GLIDE = "cubic-bezier(0.3, 0.9, 0.35, 1)";

function centre(element: Element): [number, number] {
  const box = element.getBoundingClientRect();
  return [box.left + box.width / 2, box.top + box.height / 2];
}

/**
 * The system's pointer — black edged in white, as macOS and Linux draw it;
 * white edged in black on Windows. Place it inside the element it ends on
 * (one that is positioned); it moves through `path`, whose last stop is
 * usually `{ at, x: 0, y: 0 }`, and with `clickAt` presses there with a
 * ripple.
 * Resting somewhere is two stops at one place. Between stops it glides, or
 * moves at an even pace with `easing="linear"`. Stops on marked elements are
 * measured when the scene starts, so the path follows the screen's layout at
 * any frame width. Hidden, and still, when reduced motion is asked for.
 */
export function GuideCursor({ path, clickAt, easing = GLIDE }: {
  path: readonly GuideCursorStop[];
  clickAt?: number;
  easing?: string;
}) {
  const cursor = useRef<HTMLSpanElement>(null);
  const windowsLook = detectKeyboardShortcutPlatform() === "windows";
  const moves = JSON.stringify(path);
  useLayoutEffect(() => {
    const node = cursor.current;
    const stops = JSON.parse(moves) as GuideCursorStop[];
    const last = stops.at(-1);
    if (!node || !last || window.getComputedStyle(node).display === "none") return undefined;
    node.style.transform = "";
    // The player's frame, where the marked elements are looked up.
    const frame = node.closest<HTMLElement>("[data-guide-screen]");
    const scale = frame && frame.offsetWidth > 0 ? frame.getBoundingClientRect().width / frame.offsetWidth : 1;
    const home = node.getBoundingClientRect();
    const place = (stop: GuideCursorStop) => {
      const target = stop.on ? frame?.querySelector(`[data-guide-target="${stop.on}"]`) : null;
      const [left, top] = target ? centre(target) : [home.left, home.top];
      const x = (left - home.left) / scale + (stop.x ?? 0) - TIP;
      const y = (top - home.top) / scale + (stop.y ?? 0) - TIP;
      return `translate(${x}px, ${y}px)`;
    };
    if (stops.length === 1 || typeof node.animate !== "function") {
      node.style.transform = place(last);
      return undefined;
    }
    const start = stops[0].at;
    const span = Math.max(1, last.at - start);
    const motion = node.animate(
      stops.map((stop) => ({ transform: place(stop), offset: (stop.at - start) / span, easing })),
      { delay: start, duration: span, fill: "both" },
    );
    return () => motion.cancel();
  }, [moves, easing]);

  return (
    <>
      {clickAt === undefined ? null : <GuideClick at={clickAt} />}
      <span
        ref={cursor}
        className={styles.cursor}
        style={clickAt === undefined ? undefined : ({ "--press-at": `${clickAt}ms` } as CSSProperties)}
        aria-hidden="true"
      >
        <svg className={styles.cursorArrow} viewBox="0 0 24 24" width="17" height="17">
          <path
            d="M5.2 2.7 17.9 14a.55.55 0 0 1-.37.96h-6.2l2.45 5.55a.6.6 0 0 1-.3.79l-1.83.8a.6.6 0 0 1-.79-.3l-2.45-5.57-4.06 4.42A.55.55 0 0 1 3.4 20.3V3.1a.55.55 0 0 1 .92-.4Z"
            fill={windowsLook ? "#fff" : "#111"}
            stroke={windowsLook ? "#111" : "#fff"}
            strokeWidth="1.4"
            strokeLinejoin="round"
          />
        </svg>
      </span>
    </>
  );
}

/** A click's ripple at the centre of the element it is placed in, `at` ms into the scene. */
export function GuideClick({ at }: { at: number }) {
  return (
    <span
      className={styles.clickRipple}
      style={{ "--click-at": `${at}ms` } as CSSProperties}
      aria-hidden="true"
    />
  );
}

/** The blinking caret of a field being typed in. */
export function GuideCaret() {
  return <i className={styles.caret} aria-hidden="true" />;
}

/**
 * A window's title bar as the system the guide is read on draws it: on a Mac
 * the three lights before the address or title; on Windows the minimize,
 * maximize and close buttons after it, on Linux the close button alone.
 * Scenes put it first in their picture and fill the rest of the frame below
 * it (the bar is 26px tall).
 */
export function GuideWindowBar({ title }: { title: string }) {
  const platform = detectKeyboardShortcutPlatform();
  return (
    <div className={styles.chrome} data-os={platform}>
      {platform === "mac" ? (
        <span className={styles.trafficLights} aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
      ) : null}
      <span className={styles.chromeUrl}>{title}</span>
      {platform === "mac" ? null : (
        <span className={styles.captionButtons} aria-hidden="true">
          {platform === "windows" ? (
            <>
              <svg viewBox="0 0 10 10"><path d="M1 5h8" /></svg>
              <svg viewBox="0 0 10 10"><rect x="1.5" y="1.5" width="7" height="7" /></svg>
            </>
          ) : null}
          <svg viewBox="0 0 10 10"><path d="m1.5 1.5 7 7m0-7-7 7" /></svg>
        </span>
      )}
    </div>
  );
}
