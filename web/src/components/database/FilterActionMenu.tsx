import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { DotsHorizontal } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import styles from "./FilterActionMenu.module.css";

export type FilterAction = {
  id: string;
  label: string;
  /** A gray line under the label, as "상위 필터 그룹 생성". */
  description?: string;
  icon: ReactNode;
  danger?: boolean;
  run(): void;
};

/**
 * An advanced filter's rule or group "⋯", as Notion's (seen 2026-10-23 at
 * 1280px): its menu drops from the button's left edge, 10px round, 28px rows
 * of a 20px icon 8px in and 14px words — "제거" in red, then "복제",
 * "그룹으로 전환" or a group's "필터로 전환" and "상위 그룹 생성". It takes
 * the keys as every menu does: opened by Enter from the window's keys
 * (useFilterWindowKeys) it lights "제거" ringed, as Notion's (seen
 * 2026-11-24 at 1685 dark).
 */
export function FilterActionMenu({ label, actions, className, trigger, data }: {
  label: string;
  actions: FilterAction[];
  className?: string;
  /** What the button shows in place of "⋯", as "+ 필터 규칙 추가 ⌄" (its menu drops the same way). */
  trigger?: ReactNode;
  data?: Record<string, string>;
}) {
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menuRef, open
    ? (size, viewport) => placeAround(popoverRectOf(buttonRef.current)!, size, viewport, { gap: 0, align: "start", alignFallback: true })
    : null, [open], undefined, {
    kind: "menu", onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current],
  });
  const menu = useNativeMenu({ items: actions, open, menuRef, label, keyOf: (action) => action.id, textOf: (action) => action.label,
    onAction: (action) => { setOpen(false); action.run(); }, onClose: () => setOpen(false) });
  return (
    <>
      <button ref={buttonRef} type="button" className={className} aria-label={trigger ? undefined : label} aria-haspopup="menu" aria-expanded={open}
        data-filter-actions={trigger ? undefined : ""} {...data} onClick={() => setOpen((current) => !current)}>
        {trigger ?? <DotsHorizontal size="regular" aria-hidden="true" />}
      </button>
      {open && typeof document !== "undefined" ? createPortal(
        <div {...menu.menuProps} ref={menuRef} className={styles.menu} data-filter-action-menu style={popoverPlacementStyle(place)}>
          {actions.map((action) => (
            <NativeMenuItem key={action.id} state={menu.state} itemKey={action.id} className={styles.item}
              data-danger={action.danger ? "true" : undefined} data-described={action.description ? "true" : undefined}
              data-filter-action={action.id}>
              {({ labelProps, descriptionProps }) => <>
                <span className={styles.icon} aria-hidden="true">{action.icon}</span>
                <span className={styles.text}>
                  <span {...labelProps}>{action.label}</span>
                  {action.description ? <small {...descriptionProps}>{action.description}</small> : null}
                </span>
              </>}
            </NativeMenuItem>
          ))}
        </div>,
        document.body,
      ) : null}
    </>
  );
}
