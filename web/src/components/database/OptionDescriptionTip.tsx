import { type MouseEvent, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { placeBeside, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { useTipPress } from "@/lib/tipTiming";
import styles from "./PropertyDescriptionTip.module.css";

type Shown = { name: string; description: string; at: PopoverRect };

/**
 * A described option's name and description in a tip while the pointer is on
 * its row in a cell's picker, as Notion's (seen 2026-11-06): 2px left of the
 * picker, level with the row's middle, its name over its description. It
 * goes as the app's other tips go (lib/tipTiming.ts): off its row, or with a
 * press anywhere.
 */
export function useOptionDescriptionTip() {
  const [shown, setShown] = useState<Shown | null>(null);
  const tipRef = useRef<HTMLDivElement>(null), control = useRef<HTMLElement | null>(null);
  const place = usePopoverPlacement(tipRef, shown ? (size, viewport) => placeBeside(shown.at, size, viewport,
    { prefer: "left", gap: 2, offsetTop: (shown.at.bottom - shown.at.top - size.height) / 2 }) : null, [shown], undefined, { kind: "tooltip" });
  function show(event: MouseEvent<HTMLElement>, option: { name: string; description?: string | null }) {
    if (!option.description) return;
    control.current = event.currentTarget;
    const row = event.currentTarget.getBoundingClientRect();
    const picker = event.currentTarget.closest<HTMLElement>("[role=dialog]")?.getBoundingClientRect() ?? row;
    setShown({ name: option.name, description: option.description, at: { top: row.top, bottom: row.bottom, left: picker.left, right: picker.right } });
  }
  const hide = () => setShown(null);
  useTipPress(shown !== null, hide, () => control.current);
  const tip = shown && typeof document !== "undefined" ? createPortal(
    <div ref={tipRef} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-option-description-tip>
      <strong>{shown.name}</strong>
      <span>{shown.description}</span>
    </div>,
    document.body,
  ) : null;
  return { show, hide, tip };
}
