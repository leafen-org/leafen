import { SwitchMark } from "@/components/Switch";
import { type RefObject, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "react-aria-components";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { placeAround, type PopoverRect, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { ICON_SIZES } from "@/icons/leafen";
import { NATIVE_ICON_COLORS, type NativeIconColor } from "@/nativeIconCatalog.generated";
import type { useBuiltInIconChoice } from "./builtInIconChoice";
import { nativeIconCellLabel, nativeIconColorVars, nativeIconLabel } from "./nativeIcons";
import { BuiltInIconGlyph } from "./PageIcon";
import styles from "./PageIconColor.module.css";

type Choice = ReturnType<typeof useBuiltInIconChoice>;

/** Notion's filled circle at each size it draws a color in: its view box and the circle's radius there. */
const DOT_GLYPHS = { tiny: { box: 16, radius: 6.05 }, regular: { box: 20, radius: 7.625 } } as const;

/**
 * An icon color's dot, as Notion draws it (seen 2026-11-17 at 1440, light and
 * dark): its filled circle glyph at the icon size, outlined one unit in its
 * own color — circleFillSmall in the picker's color button (14px, radius 6.05
 * of 16, 11.46 across with the outline) and circleFill in the color menu
 * (20px, radius 7.625 of 20, 16.25 across). "Choose every time" (`color`
 * null) fills it with the icons' tertiary color, outlined in light gray.
 */
export function IconColorDot({ color, size }: { color: NativeIconColor | null; size: keyof typeof DOT_GLYPHS }) {
  const { box, radius } = DOT_GLYPHS[size];
  const fill = color ? nativeIconColorVars[color] : "var(--icon-tertiary)";
  return <svg className={styles.iconColorDot} width={ICON_SIZES[size]} height={ICON_SIZES[size]} viewBox={`0 0 ${box} ${box}`}
    style={{ fill, stroke: color ? fill : nativeIconColorVars.lightgray }} aria-hidden="true">
    <circle cx={box / 2} cy={box / 2} r={radius} strokeWidth={1} />
  </svg>;
}

/**
 * A color's swatch in the menu, as Notion's (seen 2026-11-17 at 1440, two
 * tabs, and on an iPhone): its dot with a ring of its own color round the
 * cell, drawn over it and never pressed — whole on the color chosen, half
 * on the one under the pointer or the keys (PageIconColor.module.css).
 */
function SwatchFace({ color }: { color: NativeIconColor }) {
  return <>
    <IconColorDot color={color} size="regular" />
    <span className={styles.swatchRing} style={{ color: nativeIconColorVars[color] }} aria-hidden="true" />
  </>;
}

export function IconColorPalette({ choice, triggerRef, onClose }: {
  choice: Choice; triggerRef: RefObject<HTMLButtonElement | null>; onClose(): void;
}) {
  const { t } = useTranslation("emojiPicker");
  const ref = useRef<HTMLDivElement>(null), listRef = useRef<HTMLDivElement>(null);
  // Under the button with no gap, from its left edge the way the screen
  // reads, as Notion's (seen 2026-11-17 at 1440, two tabs; a phone's kept
  // 12px in from the screen's edge).
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(popoverRectOf(triggerRef.current)!, size, viewport,
    { gap: 0, align: "start", minHeight: 0 }), [triggerRef], undefined,
    { kind: "picker", onEscape: onClose, onOutsidePress: onClose, returnFocusRef: triggerRef, inside: () => [triggerRef.current] });
  return <PopoverLayer><div ref={ref} className={styles.iconColorMenu} style={popoverPlacementStyle(place)}
    role="dialog" aria-label={t("icons.colorGroupLabel")}>
    <NativeChoiceGrid items={NATIVE_ICON_COLORS} keyOf={color => color}
      textOf={color => t("icons.setColorLabel", { color: t(`icons.colors.${color}`) })}
      listRef={listRef} label={t("icons.colorGroupLabel")} className={styles.iconColorSwatches}
      checkedKey={choice.eachTime ? undefined : choice.color} disabledKeys={choice.eachTime ? [...NATIVE_ICON_COLORS] : []}
      open={Boolean(place)} marker={color => ({ "data-native-icon-color": color })}
      onAction={color => { if (!choice.eachTime) { choice.changeColor(color); onClose(); } }}>
      {color => <SwatchFace color={color} />}
    </NativeChoiceGrid>
    <div className={styles.eachTimeRow}>
      <Switch className={styles.iconColorEachTime} isSelected={choice.eachTime} onChange={choice.changeEachTime} autoFocus={choice.eachTime}>
        <span className={styles.eachTimeName}>{t("icons.chooseEachTime")}</span><SwitchMark on={choice.eachTime} className={styles.eachTimeTrack} />
      </Switch>
    </div>
  </div></PopoverLayer>;
}

export function IconColorRow({ choice, name, anchor, triggerRef }: {
  choice: Choice; name: string; anchor: PopoverRect; triggerRef: RefObject<HTMLButtonElement | null>;
}) {
  const { t } = useTranslation("emojiPicker");
  const ref = useRef<HTMLDivElement>(null), listRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(anchor, size, viewport, { gap: 4, minHeight: 0 }),
    [anchor], undefined, { kind: "menu", onEscape: choice.cancel, onOutsidePress: choice.cancel,
      returnFocusRef: triggerRef, inside: () => [triggerRef.current] });
  return <PopoverLayer><div ref={ref} style={popoverPlacementStyle(place)} className={styles.iconColorRow}>
    <NativeChoiceGrid items={NATIVE_ICON_COLORS} keyOf={color => color}
      textOf={color => nativeIconCellLabel(name, color)}
      onAction={color => choice.pickColored(name, color)} listRef={listRef}
      label={t("icons.colorChoicesLabel", { name: nativeIconLabel(name) })} className={styles.iconColors}
      marker={color => ({ "data-native-icon-color": color })} open={Boolean(place)}>
      {color => <BuiltInIconGlyph icon={{ name, color }} size={20} />}
    </NativeChoiceGrid>
  </div></PopoverLayer>;
}
