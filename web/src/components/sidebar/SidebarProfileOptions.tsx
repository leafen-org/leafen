import { type ReactNode, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { useOpenedFromKeyboard } from "@/lib/nativeMenu";
import { placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { styles } from "./sidebarStyles";

export type ProfileMenuOptions = { id: string; anchor: HTMLElement };
export type ProfileAction = { label: string; icon?: ReactNode; run(): void };

/** Account actions keep their exact server/profile target. The child owns
 * its overlay lease and actual engine collection separately from the parent. */
export function SidebarProfileOptions({ options, actions, onClose }: { options: ProfileMenuOptions; actions: ProfileAction[]; onClose(): void }) {
  const { t } = useTranslation("sidebar"), menu = useRef<HTMLDivElement>(null);
  // Its first row starts highlighted only when opened from the keyboard, as Notion's.
  const fromKeyboard = useOpenedFromKeyboard();
  const place = usePopoverPlacement(menu, (size, viewport) => placeBeside(
    popoverRectOf(options.anchor) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 8 },
  ), [options.anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose,
    inside: () => [options.anchor] });
  const content = <div ref={menu} className={styles.profileOptions} style={{ ...popoverPlacementStyle(place) }}>
    <NativeActionMenu aria-label={t("profiles.accountOptions")} autoFocus={fromKeyboard ? "first" : true}>
      {actions.map(action => <NativeActionMenuItem key={action.label} className={`${styles.workspaceMenuItem} ${styles.profileOptionsItem}`}
        data-workspace-menu-item onClick={() => { onClose(); action.run(); }}>{action.icon}<span>{action.label}</span></NativeActionMenuItem>)}
    </NativeActionMenu>
  </div>;
  return typeof document === "undefined" ? content : createPortal(content, document.body);
}
