import { type MouseEvent, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { placeAround, placeBeside, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useTipPress } from "@/lib/tipTiming";
import { propertyDescription } from "./propertyDescription";
import styles from "./PropertyDescriptionTip.module.css";

const TIP_GAP = 6;

/**
 * A described property's name and description in a dark tip while the
 * pointer is over where it is named, as Notion shows it (seen 2026-09-30):
 * above a table column's head, left of a database page's property. A
 * database page's names every property so, its name alone where it has no
 * description, half a second after the pointer comes (Notion's, seen
 * 2026-11-14: "식" in a 26×27 tip 6px left of its line, about 540ms on).
 * It goes, and its wait with it, as the app's other tips go
 * (lib/tipTiming.ts): off its name or with a press anywhere.
 */
export function usePropertyDescriptionTip(
  property: { name: string; config?: unknown },
  placement: "above" | "left",
  { nameAlone = false, delay = 0 }: { nameAlone?: boolean; delay?: number } = {},
) {
  const description = propertyDescription(property);
  const tipped = Boolean(description) || nameAlone;
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const [pointed, setPointed] = useState(false);
  const timer = useRef(0), control = useRef<HTMLElement | null>(null);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const show = (event: MouseEvent<HTMLElement>) => {
    const target = event.currentTarget;
    window.clearTimeout(timer.current);
    control.current = target;
    setPointed(true);
    if (!delay) setAnchor(target.getBoundingClientRect());
    else timer.current = window.setTimeout(() => { if (target.isConnected) setAnchor(target.getBoundingClientRect()); }, delay);
  };
  const hide = () => { window.clearTimeout(timer.current); setPointed(false); setAnchor(null); };
  useTipPress(pointed, hide, () => control.current);
  const handlers = tipped ? { onMouseEnter: show, onMouseLeave: hide } : {};
  const tipRef = useRef<HTMLDivElement>(null);
  // Above, or left and level with its middle, where the window has room;
  // else on the other side.
  const place = usePopoverPlacement(tipRef, tipped && anchor ? (size, viewport) => placement === "above"
    ? placeAround(anchor, size, viewport, { prefer: "above", gap: TIP_GAP })
    : placeBeside(anchor, size, viewport, { prefer: "left", gap: TIP_GAP, offsetTop: (anchor.height - size.height) / 2 })
  : null, [anchor, placement], undefined, { kind: "tooltip" });
  const tip = tipped && anchor && typeof document !== "undefined" ? createPortal(
    <div ref={tipRef} className={styles.tip} data-placement={placement} style={popoverPlacementStyle(place)} role="tooltip"
      data-property-description-tip={description ? "" : undefined} data-property-name-tip={description ? undefined : ""}>
      <strong>{property.name}</strong>
      {description ? <span>{description}</span> : null}
    </div>,
    document.body,
  ) : null;
  return { description, handlers, tip };
}
