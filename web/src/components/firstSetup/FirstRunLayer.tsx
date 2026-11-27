import { type ReactNode, useRef } from "react";
import { createPortal } from "react-dom";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import css from "./FirstSetup.module.css";

/**
 * A first run's screen over the whole window as a modal layer of its own
 * (2026-10-28: the app it covered still took Tab, a screen reader and a
 * desktop's accessibility tree, and nothing in it had the focus): the app
 * behind is inert, Tab goes round inside, and focus enters as it opens and
 * again when what it shows changes (`view`) and the focus isn't in it — its
 * first control, which is the field where a view asks for one.
 */
export function FirstRunLayer({ view, data, children }: {
  /** What the screen shows: a new one takes the focus in again. */
  view: string;
  /** Its data-* attributes, as the step's screen names itself. */
  data: Record<`data-${string}`, string>;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useOverlayLayer(true, { kind: "dialog", ref });
  useViewEntryFocus(ref, view);
  const layer = (
    <div ref={ref} className={css.screenLayer} role="dialog" aria-modal="true" {...data}>
      {children}
    </div>
  );
  return typeof document === "undefined" ? layer : createPortal(layer, document.body);
}
