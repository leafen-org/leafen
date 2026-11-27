import { useCallback, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LockIcon, Pencil, UnlockIcon } from "@/icons/leafen";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { useOpenedFromKeyboard } from "@/lib/nativeMenu";
import { personName } from "@/lib/peopleDirectory";
import { styles } from "./topBarStyles";
import { usePhoneLockBand } from "./phoneLockBandStore";

// Above a database page open over the page (its panel's layer is 60).
const MENU_LAYER = 130;

export type TopBarLockProps = {
  className?: string;
  /** The page it is the lock of: on a phone its band under the bar draws it (PhoneLockBand). */
  pageId?: string;
  /** Who locked the page, by ID. */
  lockedBy?: string;
  onLockAgain?: () => void;
  onUnlockForEveryone?: () => void;
  onUnlockForMe?: () => void;
  unlockedForMe: boolean;
};

/**
 * A locked page's "Locked" beside its name in the top bar, as Notion's (seen
 * 2026-09-29/30): it opens "Unlock", for the reader alone, and "Unlock for
 * everyone"; unlocked for the reader, it reads "Re-lock" and locks it again
 * for them. A reader who can change neither sees it as the page's state.
 * Pointed at, its tip names who locked it, over "click to unlock" in gray
 * for one who can (seen 2026-10-31). On a phone it is drawn in its band
 * under the bar instead (PhoneLockBand).
 */

export function TopBarLock({ className, pageId, ...lock }: TopBarLockProps) {
  const { lockedBy, onLockAgain, onUnlockForEveryone, onUnlockForMe, unlockedForMe } = lock;
  const banded = usePhoneLockBand(pageId, lock);
  const { t } = useTranslation("topBar");
  // Its first row starts highlighted only when opened from the keyboard, as Notion's.
  const fromKeyboard = useOpenedFromKeyboard();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // Right under "Locked", or over it where only that side has room (Notion, 2026-10-31).
  const place = usePopoverPlacement(menuRef, open ? (size, viewport) => placeAround(
    popoverRectOf(triggerRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 0 },
  ) : null, [open], undefined, { kind: "menu", onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false) });
  const close = useCallback(() => {
    setOpen(false);
  }, []);

  if (banded) return null;
  if (unlockedForMe) {
    return (
      <button type="button" className={className ?? styles.lockPill} data-page-lock-again disabled={!onLockAgain}
        onClick={onLockAgain}>
        <UnlockIcon size="small" weight="fill" aria-hidden="true" />
        <span>{t("lockAgain")}</span>
      </button>
    );
  }
  const choose = (action?: () => void) => () => {
    setOpen(false);
    action?.();
  };
  // A name is written into the tip's own markup, so its stars and breaks go.
  const locker = personName(lockedBy ?? "").replace(/[*\n]/gu, " ").trim();
  const unlockable = Boolean(onUnlockForMe || onUnlockForEveryone);
  const tip = locker ? t("lockedTip", { name: locker }) : unlockable ? t("lockedTipUnlock") : undefined;
  return (
    <>
      <button ref={triggerRef} type="button" className={className ?? styles.lockPill} data-page-lock-status
        aria-haspopup="menu" aria-expanded={open} disabled={!unlockable}
        data-icon-tip={open ? undefined : tip} data-tip-keys={locker && unlockable && !open ? t("lockedTipUnlock") : undefined}
        onClick={() => setOpen((value) => !value)}>
        <LockIcon size="small" weight="fill" aria-hidden="true" />
        <span>{t("lockStatus")}</span>
      </button>
      {open && typeof document !== "undefined" ? createPortal((
        <>
          <button type="button" className={styles.menuBackdrop} style={popoverBackdropStyle(place) ?? { zIndex: MENU_LAYER - 1 }} tabIndex={-1}
            aria-label={t("closePageMenu")} onClick={close} />
          {/* Each choice with its mark, as Notion's (seen 2026-10-01). */}
          <NativeActionMenu menuRef={menuRef} className={`${styles.crumbMenu} ${styles.lockMenu}`} autoFocus={fromKeyboard ? "first" : true}
            style={{ position: "fixed", zIndex: MENU_LAYER, ...popoverPlacementStyle(place) }}
            aria-label={t("lockStatus")}>
            {onUnlockForMe ? (
              <NativeActionMenuItem type="button" className={styles.menuItem} data-menu-item role="menuitem" data-page-unlock-for-me
                onClick={choose(onUnlockForMe)}>
                <Pencil size="regular" aria-hidden="true" /><span>{t("unlockForMe")}</span>
              </NativeActionMenuItem>
            ) : null}
            {onUnlockForEveryone ? (
              <NativeActionMenuItem type="button" className={styles.menuItem} data-menu-item role="menuitem" data-page-unlock-for-everyone
                onClick={choose(onUnlockForEveryone)}>
                <UnlockIcon size="regular" aria-hidden="true" /><span>{t("unlockForEveryone")}</span>
              </NativeActionMenuItem>
            ) : null}
          </NativeActionMenu>
        </>
      ), document.body) : null}
    </>
  );
}
