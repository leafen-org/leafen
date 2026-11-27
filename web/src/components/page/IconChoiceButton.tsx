import { useRef, useState } from "react";
import { EmojiFaceFillIcon } from "@/icons/leafen";
import type { IconType } from "@/lib/types";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "./AnchoredEmojiPicker";
import { PageIconGlyph } from "./PageIcon";
import { ICON_FIRST_TABS } from "./pageIconPickerModel";
import styles from "./IconChoiceButton.module.css";

/** What an icon choice button holds: an emoji, a built-in icon, or a picture or workspace emoji kept from before. */
export type ChosenIcon = { icon: string; iconType: IconType };

/**
 * Notion's "아이콘 추가" button, one for a page button's head and a "페이지 아이콘 설정" step (seen 2026-11-21 in
 * Chrome at 1440, dark, in both, two tabs at different times): 28 square with 6 corners, a 1px border-coloured ring
 * over a 2px-down shadow; Notion's filled face in the tertiary grey until an icon is chosen, then the icon in a 24
 * place at its middle — a built-in icon 23.04 across (0.96 of it), an emoji 16.8 (0.7 of it). Its picker, 356 high,
 * opens centred under it, 4 below, over it where the window ends, on "아이콘" then "이모지" with no upload, and offers
 * "제거" once an icon is chosen.
 */
export function IconChoiceButton({ icon, label, workspaceId, disabled = false, marker, onChoose }: {
  icon: ChosenIcon | null;
  label: string;
  workspaceId: string;
  disabled?: boolean;
  /** Names the picker's layer for the place that opened it, as data-{marker}. */
  marker?: string;
  /** The icon picked, or null for "제거". */
  onChoose: (icon: { icon: string; iconType: "emoji" | "icon" } | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const anchor = usePageIconPickerAnchor(open, button);
  const choose = (next: { icon: string; iconType: "emoji" | "icon" } | null) => {
    onChoose(next);
    setOpen(false);
  };
  return <>
    <button ref={button} type="button" className={styles.button} aria-label={label} aria-haspopup="dialog" aria-expanded={open}
      disabled={disabled} data-icon-choice={icon?.iconType ?? "none"} onClick={() => setOpen(true)}>
      {icon ? <ChosenGlyph icon={icon} workspaceId={workspaceId} />
        : <EmojiFaceFillIcon size="small" aria-hidden="true" className={styles.empty} />}
    </button>
    {open && anchor ? <AnchoredIconPicker anchor={anchor} marker={marker} onClose={() => setOpen(false)}
      picker={{
        tabOrder: ICON_FIRST_TABS, current: icon ?? undefined, returnFocusRef: button, short: true,
        onPick: (emoji) => choose({ icon: emoji, iconType: "emoji" }),
        onPickBuiltInIcon: (value) => choose({ icon: value, iconType: "icon" }),
        onRemove: icon ? () => choose(null) : undefined,
      }} /> : null}
  </>;
}

// A picture or a workspace emoji kept from before fills its place as a page's picture its box (0.888, not measured here).
function ChosenGlyph({ icon, workspaceId }: { icon: ChosenIcon; workspaceId: string }) {
  const page = { ...icon, workspaceId };
  if (icon.iconType === "emoji") return <span className={styles.emoji}><PageIconGlyph page={page} fallback="" /></span>;
  if (icon.iconType === "icon") return <PageIconGlyph page={page} size={23.04} fallback="" />;
  return <PageIconGlyph page={page} size={21.31} box={24} fallback="" />;
}
