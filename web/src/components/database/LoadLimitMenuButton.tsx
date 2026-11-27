import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, DotsHorizontal } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { TABLE_INITIAL_LOAD_OPTIONS } from "./queryProjection";
import styles from "./LoadLimitMenuButton.module.css";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";

/**
 * The "⋯" on a "더 불러오기" line, as Notion's (seen 2026-10-01): it chooses
 * how many rows or cards the view first shows ("처음 불러오기에서 보기": 10,
 * 25, 50 or 100 pages).
 */
export function LoadLimitMenuButton({ limit, onLimit }: { limit: number; onLimit(limit: number): void }) {
  const { t } = useTranslation("tableView");
  const [menu, setMenu] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // Under the "⋯", or over it where only that side has room.
  const place = usePopoverPlacement(menuRef, menu ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 },
  ) : null, [menu], undefined, { kind: "menu", onEscape: () => setMenu(false), onOutsidePress: () => setMenu(false),
    inside: () => [trigger.current] });

  const collection = useNativeMenu({ items: [...TABLE_INITIAL_LOAD_OPTIONS], open: menu, menuRef,
    label: t("loadLimitHeading"), keyOf: item => String(item), textOf: item => t("loadLimitOption", { limit: item }),
    firstKey: String(limit), onAction: option => { setMenu(false); if (option !== limit) onLimit(option); } });

  function openMenu() {
    setMenu(true);
  }

  return (
    <>
      <button ref={trigger} type="button" className={styles.limitButton} data-load-limit
        aria-label={t("loadLimitMenu")} aria-haspopup="menu" aria-expanded={Boolean(menu)}
        onClick={() => (menu ? setMenu(false) : openMenu())}>
        <DotsHorizontal size="regular" aria-hidden="true" />
      </button>
      {menu && createPortal(
        <div {...collection.menuProps} ref={menuRef} className={styles.menu} data-load-limit-menu
          style={popoverPlacementStyle(place)}>
          <div className={styles.heading} role="presentation">{t("loadLimitHeading")}</div>
          {TABLE_INITIAL_LOAD_OPTIONS.map((option) => (
            <NativeMenuItem key={option} itemKey={String(option)} state={collection.state} role="menuitemradio"
              className={styles.item} aria-checked={option === limit} data-load-limit-option={option}>{({ labelProps }) => <>
              <span {...labelProps}>{t("loadLimitOption", { limit: option })}</span>
              {option === limit && <CheckIcon size="small" aria-hidden="true" />}
            </>}</NativeMenuItem>
          ))}
        </div>,
        document.body,
      )}
    </>
  );
}
