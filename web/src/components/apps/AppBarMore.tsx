import { useRef } from "react";
import { createPortal } from "react-dom";
import { useButton, useMenuTrigger } from "react-aria";
import { useMenuTriggerState } from "react-stately";
import { DotsHorizontal } from "@/icons/leafen";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import rowMenuStyles from "@/components/RowMenu.module.css";

/** The app bar's links in a "⋯" menu, for a bar too narrow to show them beside
 * the app's maker (a phone, or a narrow window in English, 2026-11-06), so the
 * maker's name and the full-window button stay in view. */
export function AppBarMore({ label, className, links }: {
  label: string;
  className: string;
  links: { key: string; label: string; onSelect(): void }[];
}) {
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const state = useMenuTriggerState({});
  const { menuTriggerProps, menuProps } = useMenuTrigger({ type: "menu" }, state, trigger);
  const { buttonProps } = useButton({ ...menuTriggerProps, preventFocusOnPress: false, onPressStart: undefined,
    onPress: (event) => { if (event.pointerType !== "keyboard") state.toggle(); } }, trigger);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 4 },
  ) : null, [state.isOpen], undefined, { kind: "menu", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current] });
  return <>
    <button {...buttonProps} ref={trigger} className={className} aria-label={label} title={label} data-app-bar-more>
      <DotsHorizontal size="small" aria-hidden="true" />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={`${rowMenuStyles.menu} ${rowMenuStyles.contextMenu}`}
      style={popoverPlacementStyle(placement)}>
      <NativeActionMenu {...menuProps} aria-label={label} autoFocus={state.focusStrategy ?? true}>
        {links.map((link) => <NativeActionMenuItem key={link.key} id={link.key} className={rowMenuStyles.item} data-app-bar-link={link.key}
          onClick={() => { state.close(); link.onSelect(); }}>
          <span>{link.label}</span>
        </NativeActionMenuItem>)}
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}
