import { type ComponentProps, type RefObject, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { POPOVER_MARGIN, placeAround, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { WorkspaceFileTarget } from "@/lib/storage";
import { PageIconPicker } from "./PageIconPicker";
import { PhoneEmojiSheet } from "./PhoneEmojiSheet";
import styles from "./AnchoredEmojiPicker.module.css";

const PICKER_GAP = 4;
export const PICKER_WIDTH = 408;

/** With `right`, the box it opens from, its start on the box's start (the
 * box's right where the screen reads from the right); without, `left` is the
 * picker's own left (a picker centred under an icon). */
export type IconPickerAnchor = { top: number; bottom: number; left: number; right?: number; contextElement?: Element };
type Anchor = IconPickerAnchor;

/**
 * Where a page's icon picker opens, as Notion's (measured 2026-11-02), and a
 * teamspace's in its new and settings windows (2026-11-16): its middle under
 * the icon's, 4px down, over the page's column or the window rather than in
 * it, so the sidebar's edge or the window's doesn't cut it. The place follows the icon once
 * it is there ("아이콘 추가" puts it in, then opens the picker), as the page
 * settles around it (a scrollbar coming or going moved it 7px).
 */
export function usePageIconPickerAnchor(open: boolean, iconRef: RefObject<HTMLElement | null>) {
  const [anchor, setAnchor] = useState<Anchor | null>(null);
  useLayoutEffect(() => {
    const icon = iconRef.current;
    if (!open || !icon) {
      setAnchor(null);
      return;
    }
    setAnchor({
      get top() { return icon.getBoundingClientRect().top; },
      get bottom() { return icon.getBoundingClientRect().bottom; },
      get left() {
        const box = icon.getBoundingClientRect();
        return box.left + (box.width - Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN)) / 2;
      },
      contextElement: icon,
    });
  }, [open, iconRef]);
  return anchor;
}

/**
 * An icon picker by a place on the screen: under the place, or over it where
 * the window ends, and kept inside the window. It lies in a layer of its own,
 * so a menu that scrolls doesn't clip it.
 */
export function AnchoredIconPicker({ anchor, marker, rise = false, gap = PICKER_GAP, onClose, picker }: {
  anchor: Anchor;
  /** How far under its place it opens: 4px, or a property's 3 (PropertyIconPicker). */
  gap?: number;
  /** Too tall for the room under its place, it keeps its height and moves up over it (a callout's, as Notion's). */
  rise?: boolean;
  /** Names the layer for the place that opened it, as data-{marker}. */
  marker?: string;
  onClose: () => void;
  picker: Omit<ComponentProps<typeof PageIconPicker>, "placement" | "onClose">;
}) {
  const layerRef = useRef<HTMLDivElement>(null);
  const [zIndex, setZIndex] = useState<number>();
  // The layer is a point; the picker in it is what is placed, over the menu
  // or dialog it opened from.
  const place = usePopoverPlacement(layerRef, (size, viewport) => anchor.right === undefined
    ? { ...placeAround(popoverRectAt(anchor), size, viewport, { gap, rise, appear: "fade" }), corner: true }
    : placeAround({ top: anchor.top, bottom: anchor.bottom, left: anchor.left, right: anchor.right, contextElement: anchor.contextElement }, size, viewport, { gap, rise, appear: "fade" }),
  [anchor, rise, gap], (layer) => layer.querySelector<HTMLElement>("[role='dialog']"), false);
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={layerRef} className={styles.layer}
      style={place
        ? { top: place.top, left: place.left, zIndex }
        : { top: anchor.bottom + gap, left: anchor.left, opacity: 0, zIndex }}
      {...(marker ? { [`data-${marker}`]: "true" } : {})}>
      <PageIconPicker {...picker} placement="anchored" onClose={onClose} onLayerZIndexChange={setZIndex} />
    </div>,
    document.body,
  );
}

/**
 * A reaction's emoji picker by a place on the screen, as a comment's
 * reactions, a page's and chosen words' open it; given a workspace place, it
 * offers the workspace's own emoji and adding one. On a phone it is Notion's
 * whole-screen "리액션" sheet instead, wherever it was opened from
 * (PhoneEmojiSheet). With `leavesFocus` it takes no focus, a field opened
 * with it keeping the caret (PageIconPicker).
 */
export function AnchoredEmojiPicker({ anchor, label, marker, leavesFocus, onClose, onPick, custom }: {
  anchor: Anchor;
  label: string;
  marker?: string;
  leavesFocus?: boolean;
  onClose: () => void;
  onPick: (emoji: string) => void;
  custom?: { target: WorkspaceFileTarget; onPick: (emoji: WorkspaceCustomEmoji) => void; pickAdded?: boolean };
}) {
  const { phone } = useSidebarLayout(true);
  if (phone) {
    return <PhoneEmojiSheet marker={marker} onClose={onClose} onPick={onPick}
      custom={custom?.target.workspaceId ? { workspaceId: custom.target.workspaceId, onPick: custom.onPick } : undefined} />;
  }
  return (
    <AnchoredIconPicker anchor={anchor} marker={marker} onClose={onClose}
      picker={{
        label, offerRandom: false, onPick, leavesFocus,
        ...(custom ? { uploadTarget: custom.target, onPickCustomEmoji: custom.onPick, pickAddedEmoji: custom.pickAdded } : {}),
      }} />
  );
}
