import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { placeAround, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useTipPress } from "@/lib/tipTiming";
import styles from "./PropertyDescriptionTip.module.css";

/**
 * A card's value, the property's name in a tip under it while the pointer is
 * on it, as Notion's cards do (seen 2026-11-03). It takes no room of its own.
 * The value's editor open over it (CardValuePress), it shows none: the
 * pointer on the editor isn't on the value. It goes as the app's other tips
 * go (lib/tipTiming.ts): off the value or with a press anywhere.
 */
export function CardValueTip({ name, children }: { name: string; children: ReactNode }) {
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const tipRef = useRef<HTMLDivElement>(null), own = useRef<HTMLSpanElement>(null);
  useTipPress(anchor !== null, () => setAnchor(null), () => own.current);
  const place = usePopoverPlacement(tipRef, anchor ? (size, viewport) => placeAround(anchor, size, viewport, { gap: 4 }) : null, [anchor],
    undefined, { kind: "tooltip" });
  return (
    <span ref={own} style={{ display: "contents" }} onMouseEnter={(event) => {
      if (!event.currentTarget.contains(event.target as Node)) return;
      const value = event.currentTarget.firstElementChild;
      if (value && name.trim()) setAnchor(value.getBoundingClientRect());
    }} onMouseLeave={() => setAnchor(null)}>
      {children}
      {anchor ? createPortal(
        <div ref={tipRef} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-card-value-tip><strong>{name}</strong></div>,
        document.body,
      ) : null}
    </span>
  );
}
