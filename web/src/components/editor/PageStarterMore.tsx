import { useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useButton, useMenuTrigger } from "react-aria";
import { useMenuTriggerState } from "react-stately";
import type { GraphViewType } from "@/contentGraph";
import { DotsHorizontal, ImportIcon } from "@/icons/leafen";
import { ViewTypeIcon } from "@/components/database/databaseViewShared";
import { NativeActionMenu, NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import rowMenuStyles from "@/components/RowMenu.module.css";
import styles from "./editorStyles.generated.module.css";

const VIEWS = ["table", "board", "list", "timeline", "calendar", "gallery"] as const satisfies readonly GraphViewType[];

/** The starter's last pill, "⋯": the page as a database in each view, then
 * an import, in a menu 200px wide over the pill's left edge, no row lit
 * when a click opens it (Notion's, seen 2026-10-31). */
export function PageStarterMore({ disabled, onView, onImport }: {
  disabled: boolean;
  onView(view: GraphViewType): void;
  onImport?: () => void;
}) {
  const { t } = useTranslation(["editor", "databaseView"]);
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const state = useMenuTriggerState({});
  const { menuTriggerProps, menuProps } = useMenuTrigger({ type: "menu", isDisabled: disabled }, state, trigger);
  const { buttonProps } = useButton({ ...menuTriggerProps, isDisabled: disabled, preventFocusOnPress: false, onPressStart: undefined,
    onPress: (event) => { if (event.pointerType !== "keyboard") state.toggle(); } }, trigger);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { prefer: "above", gap: 4 },
  ) : null, [state.isOpen], undefined, { kind: "picker", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current] });
  const choose = (action: () => void) => { state.close(); action(); };
  return <>
    <button {...buttonProps} ref={trigger} className={`${styles.pageStarterButton} ${styles.pageStarterMore}`} data-page-starter-action
      aria-label={t("starter.moreOptions")}>
      <DotsHorizontal size="regular" aria-hidden="true" />
    </button>
    {/* Its rows are drawn before it is placed, so it is placed at its full
        height: placed short, it would grow down over the pill. */}
    {state.isOpen ? createPortal(<div ref={menu} className={`${rowMenuStyles.menu} ${rowMenuStyles.contextMenu} ${styles.pageStarterMenu}`}
      style={popoverPlacementStyle(placement)}>
      <NativeActionMenu {...menuProps} aria-label={t("starter.moreOptions")} autoFocus={state.focusStrategy ?? true}>
        {VIEWS.map((view) => <NativeActionMenuItem key={view} id={view} className={rowMenuStyles.item} data-page-starter-view={view}
          onClick={() => choose(() => onView(view))}>
          <ViewTypeIcon type={view} size="regular" /><span>{t(`databaseView:viewTypes.${view}`)}</span>
        </NativeActionMenuItem>)}
        {onImport ? <NativeMenuSeparator className={rowMenuStyles.divider} /> : null}
        {onImport ? <NativeActionMenuItem id="import" className={rowMenuStyles.item} data-page-starter-import
          onClick={() => choose(onImport)}>
          <ImportIcon size="regular" aria-hidden="true" /><span>{t("starter.import")}</span>
        </NativeActionMenuItem> : null}
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}
