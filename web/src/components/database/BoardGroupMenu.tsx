"use client";

import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { createFocusManager } from "@react-aria/focus";
import { FocusScope, useDialog } from "react-aria";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useTranslation } from "react-i18next";
import { placeAround, type PopoverRect, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { COLOR_NAMES, chipStyle } from "./colors";
import type { BoardOption } from "./boardViewModel";
import groupStyles from "./databaseSheet";

export function BoardGroupMenu({
  option,
  position,
  onChange,
  onClose,
  onDelete,
  onHide,
  returnTo,
}: {
  returnTo: HTMLElement;
  option: BoardOption;
  /** The group's "…" it opens under. */
  position: PopoverRect;
  onChange: (patch: Partial<BoardOption>) => void;
  onClose: () => void;
  onDelete: () => void;
  onHide?: () => void;
}) {
  const { t } = useTranslation(["boardView", "tableView"]);
  const menuRef = useRef<HTMLDivElement>(null), colorsRef = useRef<HTMLDivElement>(null), actionsRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef(returnTo), entered = useRef(false);
  triggerRef.current = returnTo;
  // Under the group's "…", or over it where only that side has room.
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(position, size, viewport, { gap: 4 }), [position], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose, returnFocusRef: triggerRef, inside: () => [triggerRef.current] });
  const { dialogProps } = useDialog({ "aria-label": t("boardView:groupOptions", { groupName: option.name }) }, menuRef);
  const actions = [...(onHide ? [{ id: "hide", label: t("tableView:hide"), act: onHide }] : []),
    { id: "delete", label: t("boardView:deleteGroup"), act: onDelete }];
  const actionMenu = useNativeMenu({ items: actions, open: Boolean(place), menuRef: actionsRef, label: t("boardView:groupOptions", { groupName: option.name }),
    keyOf: action => action.id, textOf: action => action.label, onAction: action => action.act(),
    focusOnOpen: false });
  useEffect(() => {
    if (!place || entered.current) return;
    entered.current = true; createFocusManager(menuRef).focusFirst({ tabbable: true });
  }, [Boolean(place)]);

  return createPortal(
    <div
      ref={menuRef}
      className={groupStyles.boardGroupMenu}
      {...dialogProps}
      style={{ position: "fixed", ...popoverPlacementStyle(place) }}
    >
      <FocusScope contain>
      <span className={groupStyles.boardGroupLabel}>{t("boardView:groupColor")}</span>
      <NativeChoiceGrid items={COLOR_NAMES} keyOf={color => color} textOf={color => color}
        listRef={colorsRef} checkedKey={option.color ?? "default"} open={Boolean(place)} autoFocus={false}
        label={t("boardView:groupColor")}
        className={groupStyles.boardColorGrid} marker={color => ({ "data-board-color": color })}
        onAction={color => onChange({ color })}>
        {color => <span style={chipStyle(color)} aria-hidden="true" />}
      </NativeChoiceGrid>
      <div {...actionMenu.menuProps} ref={actionsRef}>
        {actions.map(action => <NativeMenuItem key={action.id} itemKey={action.id} state={actionMenu.state}
          className={groupStyles.boardGroupItem}>
          {() => <span>{action.label}</span>}
        </NativeMenuItem>)}
      </div>
      </FocusScope>
    </div>,
    document.body,
  );
}
