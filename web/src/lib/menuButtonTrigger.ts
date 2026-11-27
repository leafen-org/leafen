import type { RefObject } from "react";
import { useButton, useMenuTrigger } from "react-aria";
import { useMenuTriggerState, type MenuTriggerProps } from "react-stately";

/**
 * A menu button, as Notion's (seen 2026-11-07 in the share window: general
 * access, a role, agent access; 2026-11-12 in settings: a member's role, a
 * teamspace member's role, a teamspace's security): a click opens the menu
 * with no row lit, even one that shows its current choice, the menu holding
 * the focus so the arrows start at its first row; the keys open it on its
 * first row.
 */
export function useMenuButtonTrigger(trigger: RefObject<HTMLButtonElement | null>, disabled: boolean, options: MenuTriggerProps = {}) {
  const state = useMenuTriggerState(options);
  const { menuTriggerProps, menuProps } = useMenuTrigger({ type: "menu", isDisabled: disabled }, state, trigger);
  const { buttonProps } = useButton({ ...menuTriggerProps, isDisabled: disabled, preventFocusOnPress: false, onPressStart: undefined,
    onPress: event => { if (event.pointerType !== "keyboard") state.toggle(null); } }, trigger);
  return { state, menuProps, buttonProps, autoFocus: state.focusStrategy ?? false } as const;
}
