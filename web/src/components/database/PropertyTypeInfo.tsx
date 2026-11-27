import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { i18next } from "@/i18n";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { placeBeside, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { useTipPress } from "@/lib/tipTiming";
import type { PropertyType } from "@/lib/types";
import tipStyles from "./PropertyDescriptionTip.module.css";
import styles from "./PropertyTypeInfo.module.css";

/** A type's words for its "ⓘ", or why it is dimmed where it is. */
export function propertyTypeTip(type: PropertyType, dimmed = false) {
  if (dimmed) return i18next.t(`tableView:dimmedTypeTips.${type}`);
  return i18next.exists(`tableView:typeTips.${type}`) ? i18next.t(`tableView:typeTips.${type}`) : null;
}

/**
 * A type's words in a dark tip beside its "ⓘ", as Notion's in every type list
 * (seen 2026-11-21 at 1440, dark, in a database page's new property window,
 * a table's "+" and the view settings): 6 to the ⓘ's right and centred on it,
 * on its left where the window has no room — a table's and the panel's at the
 * window's right edge — in the text's own color. Its words are Leafen's own.
 * It goes as the app's other tips go (lib/tipTiming.ts): off its "ⓘ", or with a press
 * anywhere.
 */
export function usePropertyTypeTip() {
  const [shown, setShown] = useState<{ text: string; at: PopoverRect } | null>(null);
  const tipRef = useRef<HTMLDivElement>(null), control = useRef<Element | null>(null);
  useTipPress(shown !== null, () => setShown(null), () => control.current);
  const place = usePopoverPlacement(tipRef, shown ? (size, viewport) => placeBeside(shown.at, size, viewport,
    { prefer: "right", gap: 6, offsetTop: (shown.at.bottom - shown.at.top - size.height) / 2 }) : null, [shown], undefined, { kind: "tooltip" });
  const tip = shown && typeof document !== "undefined" ? createPortal(
    <div ref={tipRef} className={tipStyles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-property-type-tip>
      <span>{shown.text}</span>
    </div>,
    document.body,
  ) : null;
  return { show: (text: string, at: PopoverRect, element?: Element) => { control.current = element ?? null; setShown({ text, at }); },
    hide: () => setShown(null), tip };
}

export type PropertyTypeTipState = ReturnType<typeof usePropertyTypeTip>;

/**
 * A type row's "ⓘ" (PropertyTypeInfo.module.css), shown by its list on the
 * row under the pointer only; moved onto, it shows the type's words in its
 * list's tip (usePropertyTypeTip).
 */
export function PropertyTypeInfo({ type, dimmed = false, tip }: { type: PropertyType; dimmed?: boolean; tip: PropertyTypeTipState }) {
  const text = propertyTypeTip(type, dimmed);
  if (!text) return null;
  return (
    <span className={styles.info} aria-hidden="true" data-property-type-info
      onMouseEnter={(event) => { const r = event.currentTarget.getBoundingClientRect(); tip.show(text, { top: r.top, bottom: r.bottom, left: r.left, right: r.right }, event.currentTarget); }}
      onMouseLeave={tip.hide}>
      <QuestionCircleIcon size="tiny" />
    </span>
  );
}
