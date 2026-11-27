import { type RefObject, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DataSourceIcon } from "@/lib/nativeDatabaseViewProductAdapter";
import type { WorkspaceFileTarget } from "@/lib/storage";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { PageIconGlyph, type PageIconSource } from "../page/PageIcon";
import { BUILT_IN_ICON_SHARE, builtInIcon } from "../page/nativeIcons";
import styles from "./DatabaseTitleIcon.module.css";

/**
 * An inline database's own title icon over its views, as Notion's (seen
 * 2026-11-20 in Chrome at 1440, dpr 2, a built-in checkmark and an emoji):
 * a 24px box at the title's start, the name 4px after it; in it the icon's
 * 22px button 1px before its start and 1px down, the one pressed (the
 * 22px button the picker opens under, seen 2026-11-18), a built-in icon 0.96
 * of it (BUILT_IN_ICON_SHARE: 21.12), an emoji 0.9 (19.8), centred; a
 * linked view's title draws its source's in the same slot
 * (InlineDatabaseLinkedTitle, Notion's the same, seen 2026-11-20). A
 * database's title over a page or a side
 * peek is its own 36px box, the icon filling it (Notion's 36 there).
 */
export const INLINE_TITLE_ICON = {
  className: styles.inlineTitle,
  size: (icon: PageIconSource) => builtInIcon(icon) ? 22 * BUILT_IN_ICON_SHARE : 22,
};

/**
 * A database's icon by its name — over its views on a page, as its own
 * page's title, as a side peek's title — which, pressed, opens its icon
 * picker as a page's icon does: the picker's middle under the icon's, 4px
 * down, over the column (usePageIconPickerAnchor). Notion's are all so (seen
 * 2026-11-18 in Chrome at 1440, two tabs: an inline database's 22px icon,
 * its own page's 36px and its side peek's 36px each opened the 408px picker
 * centred under it, 4px down; a pick closed it and changed the icon there
 * and on the page; its random kept it open). Read only, or with nothing to
 * change it, the icon is drawn alone.
 */
export function DatabaseTitleIcon({ icon, size, box, className, buttonRef, open: givenOpen, onOpenChange, onIconChange, uploadTarget, dataAttributes }: {
  icon: PageIconSource;
  size: number;
  /** The box a picture icon is drawn against (PageIconGlyph). */
  box?: number;
  className?: string;
  /** The button, for a place that also opens the picker another way ("아이콘 추가"). */
  buttonRef?: RefObject<HTMLButtonElement | null>;
  /** Whether its picker is open, where the place holds that; else the icon holds it. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  onIconChange?: (icon: DataSourceIcon) => void;
  uploadTarget?: WorkspaceFileTarget;
  dataAttributes?: Record<`data-${string}`, string>;
}) {
  const { t } = useTranslation("pageHeader");
  const ownRef = useRef<HTMLButtonElement>(null);
  const ref = buttonRef ?? ownRef;
  const [ownOpen, setOwnOpen] = useState(false);
  const open = givenOpen ?? ownOpen;
  const setOpen = onOpenChange ?? setOwnOpen;
  const anchor = usePageIconPickerAnchor(open && Boolean(onIconChange), ref);
  const glyph = <PageIconGlyph page={icon} size={size} box={box} fallback="none" />;
  if (!onIconChange) return <span className={className} aria-hidden="true" {...dataAttributes}>{glyph}</span>;
  const pick = (next: DataSourceIcon) => {
    onIconChange(next);
    setOpen(false);
  };
  return (
    <>
      <button ref={ref} type="button" className={className ? `${styles.button} ${className}` : styles.button}
        aria-label={t("changeIcon")} aria-haspopup="dialog" aria-expanded={open} {...dataAttributes}
        onClick={() => setOpen(true)}>
        {glyph}
      </button>
      {open && anchor ? (
        <AnchoredIconPicker anchor={anchor} onClose={() => setOpen(false)} picker={{
          returnFocusRef: ref,
          returnFocusFromKeysOnly: true,
          pageIcon: true, current: icon, uploadTarget,
          onPick: (emoji) => pick({ icon: emoji, iconType: "emoji" }),
          onShuffle: (value, iconType) => onIconChange({ icon: value, iconType }),
          onPickImage: (url) => pick({ icon: url, iconType: "image" }),
          onPickCustomEmoji: (emoji) => pick({ icon: emoji.id, iconType: "custom_emoji" }),
          onPickBuiltInIcon: (value) => pick({ icon: value, iconType: "icon" }),
          onRemove: () => pick({ icon: "", iconType: "none" }),
        }} />
      ) : null}
    </>
  );
}
