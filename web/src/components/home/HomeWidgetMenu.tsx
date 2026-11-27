import { createContext, useContext, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useButton, useMenuTrigger } from "react-aria";
import { useMenuTriggerState } from "react-stately";
import { DotsHorizontal, EyeSlashIcon } from "@/icons/leafen";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import rowMenuStyles from "@/components/RowMenu.module.css";
import type { HomeWidget } from "./HomeMenu";
import styles from "./HomeWidgetMenu.module.css";

/** Hides a widget from Home, as its "⋯" › "홈에서 숨기기" and page "⋯" › "위젯 표시/숨기기" do. */
const HideWidget = createContext<((widget: HomeWidget) => void) | null>(null);
export const HomeWidgetHideProvider = HideWidget.Provider;

/**
 * A widget's head with its "⋯" at the end, shown while the pointer is on the
 * widget, as Notion's Home widgets (seen 2026-11-16 at 1440: a 24px button at
 * the head's end, 3px over its 18px line, a 16px "⋯"; pressed, a 280px menu
 * under it at its end). Its one row, "홈에서 숨기기", hides the widget as
 * page "⋯" › "위젯 표시/숨기기" does. Notion's "최근 방문" has none, nor does
 * a widget's other rows Leafen has nothing for (a calendar's settings, help).
 */
export function HomeWidgetHead({ widget, children }: { widget?: HomeWidget; children: ReactNode }) {
  const hide = useContext(HideWidget);
  if (!widget || !hide) return <>{children}</>;
  return <div className={styles.head}>{children}<HomeWidgetMenu onHide={() => hide(widget)} /></div>;
}

function HomeWidgetMenu({ onHide }: { onHide(): void }) {
  const { t } = useTranslation("homeView");
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const state = useMenuTriggerState({});
  const { menuTriggerProps, menuProps } = useMenuTrigger({ type: "menu" }, state, trigger);
  const { buttonProps } = useButton({ ...menuTriggerProps, preventFocusOnPress: false, onPressStart: undefined,
    onPress: (event) => { if (event.pointerType !== "keyboard") state.toggle(); } }, trigger);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 0 },
  ) : null, [state.isOpen], undefined, { kind: "menu", onEscape: state.close, onOutsidePress: state.close, inside: () => [trigger.current] });
  return <>
    <button {...buttonProps} ref={trigger} className={styles.button} data-home-widget-menu aria-label={t("widgetOptions")}>
      <DotsHorizontal size="small" aria-hidden="true" />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={`${rowMenuStyles.menu} ${rowMenuStyles.contextMenu}`} data-row-menu-variant="home"
      style={{ ...popoverPlacementStyle(placement), width: 280 }}>
      <NativeActionMenu {...menuProps} aria-label={t("widgetOptions")} autoFocus={state.focusStrategy ?? true}>
        <NativeActionMenuItem id="hide" className={rowMenuStyles.item} data-home-widget-hide onClick={() => { state.close(); onHide(); }}>
          <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" /><span>{t("hideWidget")}</span>
        </NativeActionMenuItem>
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}
