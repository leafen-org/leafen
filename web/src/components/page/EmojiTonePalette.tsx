import { type RefObject, useRef } from "react";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import styles from "./EmojiTonePalette.module.css";

export const TONE_HANDS = ["✋", "✋🏻", "✋🏼", "✋🏽", "✋🏾", "✋🏿"];
const TONES = TONE_HANDS.map((hand, value) => ({ hand, value }));

export function EmojiTonePalette({ tone, triggerRef, label, nameOf, onPick, onClose }: {
  tone: number; triggerRef: RefObject<HTMLButtonElement | null>; label: string;
  nameOf(value: number): string; onPick(value: number): void; onClose(): void;
}) {
  const ref = useRef<HTMLDivElement>(null), layerRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(layerRef, (size, viewport) => placeAround(popoverRectOf(triggerRef.current)!, size, viewport,
    { gap: 4, align: "end", minHeight: 0 }), [triggerRef], undefined,
    { kind: "picker", onEscape: onClose, onOutsidePress: onClose, inside: () => [triggerRef.current], returnFocusRef: triggerRef });
  return <PopoverLayer><div ref={layerRef} style={popoverPlacementStyle(place)} className={styles.toneLayer}>
    <NativeChoiceGrid items={TONES} keyOf={item => String(item.value)} textOf={item => nameOf(item.value)}
      onAction={item => onPick(item.value)} checkedKey={String(tone)} listRef={ref} label={label}
      className={styles.tones} itemClassName={styles.tone} open={Boolean(place)}>{item => item.hand}</NativeChoiceGrid>
  </div></PopoverLayer>;
}
