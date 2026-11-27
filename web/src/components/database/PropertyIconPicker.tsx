import type { ComponentProps } from "react";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { AnchoredIconPicker, PICKER_WIDTH, type IconPickerAnchor } from "../page/AnchoredEmojiPicker";

/**
 * Where a property's icon picker opens, as Notion's (measured 2026-11-17 in
 * Chrome 1440 in two tabs, the column menu's head and a new property's): its
 * middle under the icon drawn in the button that opened it, 3px under that
 * icon's foot — not the button's box (the head's 28px button, its icon 16.8px
 * 5.6 in: the picker's top 2.4 over the button's foot). The place follows the
 * icon as the menu settles.
 */
export function propertyIconPickerAnchor(button: HTMLElement): IconPickerAnchor {
  const icon = () => button.firstElementChild ?? button;
  return {
    get top() { return icon().getBoundingClientRect().top; },
    get bottom() { return icon().getBoundingClientRect().bottom; },
    get left() {
      const box = icon().getBoundingClientRect();
      return box.left + (box.width - Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN)) / 2;
    },
    get contextElement() { return icon(); },
  };
}

/**
 * A property's icon picker, wherever a property's icon is chosen (a column
 * menu's head, "+ 속성 추가", a new property's menu and screen), as Notion's
 * (seen 2026-11-17 in Chrome 1440): Notion's icons alone, in gray with no
 * color button (builtInIconChoice), 356px high whether or not the property
 * has an icon of its own — the short picker's height — and placed under the
 * icon it changes (propertyIconPickerAnchor).
 */
export function PropertyIconPicker({ anchor, marker, onClose, picker }: {
  anchor: IconPickerAnchor;
  marker: string;
  onClose: () => void;
  picker: Omit<ComponentProps<typeof AnchoredIconPicker>["picker"], "iconColors" | "short">;
}) {
  return <AnchoredIconPicker anchor={anchor} marker={marker} gap={3} onClose={onClose}
    picker={{ ...picker, iconColors: false, short: true }} />;
}
