import { useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { LinkIcon, Pencil, SmileIcon, Trash } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { blockItemText } from "./blockItemLabels";
import menuStyles from "@/components/RowMenu.module.css";
import tabMenuStyles from "./TabBlockMenu.module.css";

type TabAction = { key: string; label: string; icon: ReactNode; run(): void };

/**
 * A tab's menu, opened by pressing it with the other button or with the menu
 * key, as Notion's tabs offer (seen 2026-10-06): rename, edit its icon, copy
 * its link and delete it — the last tab cannot be deleted. It hangs from the
 * tab, not the pointer: its start at the tab's, 4px under it, wherever the tab
 * was pressed (Notion's, seen 2026-11-18 in Chrome at 1440: tabs at 558.9 and
 * 607.1, 429..461, pressed at 580,445 and 641,452, opened at 558.9 and 607.1, 465).
 */
export function TabBlockMenu({ opener, label, deletable, writable, onRename, onEditIcon, onCopyLink, onDelete, onClose }: {
  /** The tab pressed. */
  opener: HTMLElement;
  label: string;
  deletable: boolean;
  /** Without it the menu copies the link alone. */
  writable: boolean;
  onRename(): void;
  onEditIcon(): void;
  onCopyLink(): void;
  onDelete(): void;
  onClose(): void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(popoverRectOf(opener)!, { ...size, width: Math.max(size.width, 180) }, viewport,
    { gap: 4 }), [opener], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const actions: TabAction[] = [
    ...(writable ? [
      { key: "rename", label: blockItemText("tabs.menu.rename"), icon: <Pencil size="regular" aria-hidden="true" />, run: onRename },
      { key: "icon", label: blockItemText("tabs.menu.editIcon"), icon: <SmileIcon size="regular" aria-hidden="true" />, run: onEditIcon },
    ] : []),
    { key: "link", label: blockItemText("tabs.menu.copyLink"), icon: <LinkIcon size="regular" aria-hidden="true" />, run: onCopyLink },
    ...(deletable ? [{ key: "delete", label: blockItemText("tabs.menu.delete"), icon: <Trash size="regular" aria-hidden="true" />, run: onDelete }] : []),
  ];
  const menu = useNativeMenu({ items: actions, open: place !== null, menuRef, label: blockItemText("tabs.menu.label", { label }),
    keyOf: (action) => action.key, textOf: (action) => action.label,
    onAction: (action) => { onClose(); action.run(); } });
  return createPortal(
    <div {...menu.menuProps} ref={menuRef} className={`${menuStyles.menu} ${tabMenuStyles.menu}`} style={popoverPlacementStyle(place)} data-tab-block-menu>
      {actions.map((action) => (
        <NativeMenuItem key={action.key} state={menu.state} itemKey={action.key} className={menuStyles.item} data-tab-action={action.key}>
          {({ labelProps }) => <>{action.icon}<span {...labelProps}>{action.label}</span></>}
        </NativeMenuItem>
      ))}
    </div>,
    document.body,
  );
}
