"use client";

import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DbProperty, PropertyType } from "@/lib/types";
import { ConfirmCard, confirmAction } from "@/components/ConfirmActionDialog";
import { localizedPropertyTypeLabel } from "./propertyTypes";
import { focusedOpener } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";

// A stored cell value a person would miss: `false` and empty containers count
// as empty.
export function isNonEmptyStoredValue(value: unknown): boolean {
  if (value == null || value === "" || value === false) return false;
  if (Array.isArray(value)) return value.some((item) => item != null && item !== "");
  if (typeof value === "object") {
    return Object.values(value as Record<string, unknown>).some(
      (item) => item != null && item !== ""
    );
  }
  return true;
}

type PendingTypeChange = {
  kind: "twoWay";
  propName: string;
  nextType: PropertyType;
  apply: () => void;
} | {
  kind: "unsupported";
  propName: string;
  nextType: PropertyType;
};

// The server converts existing cells for every type change except title and
// place, which keep their own lifecycles. Changes to relation go to the
// relation conversion dialog.
const SERVER_UNCONVERTIBLE_TYPES = new Set<PropertyType>(["title", "place"]);

function typeChangeIsServerConvertible(
  currentType: PropertyType,
  nextType: PropertyType,
) {
  if (currentType === nextType || nextType === "relation") return true;
  return !SERVER_UNCONVERTIBLE_TYPES.has(currentType)
    && !SERVER_UNCONVERTIBLE_TYPES.has(nextType);
}

/**
 * Guard for property type changes: blocks transitions the server does not
 * convert, asks before ending a two-way relation, and applies every other
 * change at once, values or not, as Notion's (seen 2026-10-14 and
 * 2026-11-11: text "abc" made a number is empty, unasked). The change's
 * "Property type changed." notice offers Undo, which puts the original cells
 * back, and ⌘Z / ⌘⇧Z take it back and make it again.
 *
 * Shared by every type-change path (table column header, row property panel,
 * view-settings property detail).
 */
export function usePropertyTypeChangeConfirm() {
  const { t } = useTranslation(["propertyTypeChangeConfirm", "common"]);
  const [pending, setPending] = useState<PendingTypeChange | null>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);

  // closeAsker: the menu that asks, closed as the two-way card shows (the table's column menu).
  function confirmPropertyTypeChange(prop: DbProperty, nextType: PropertyType, apply: () => void, closeAsker?: () => void) {
    if (nextType === prop.type) {
      apply();
      return;
    }
    restoreFocusRef.current = focusedOpener();
    if (!typeChangeIsServerConvertible(prop.type, nextType)) {
      setPending({ kind: "unsupported", propName: prop.name, nextType });
      return;
    }
    // Ending a two-way relation is asked first, values or not, as Notion's (seen 2026-11-04). From a table's
    // column menu the menu closes as the card shows, and "취소" leaves nothing open, the page's body focused and
    // a typed letter landing nowhere, as Notion's (유형 변경 › 텍스트 pressed, measured twice on 2026-11-15 in
    // Chrome 1440 in different tabs, no script before the action, the screen and one typed letter after it).
    if (prop.type === "relation" && prop.config?.relatedPropertyId && nextType !== "relation") {
      if (closeAsker) {
        closeAsker();
        void confirmAction({ message: t("propertyTypeChangeConfirm:twoWayEnds"), confirmLabel: t("propertyTypeChangeConfirm:twoWayContinue"),
          cancelLabel: t("common:actions.cancel"), danger: "filled", markers: { dialog: { "data-property-type-change-confirm": "twoWay" } },
        }).then(go => { if (go) apply(); });
        return;
      }
      setPending({ kind: "twoWay", propName: prop.name, nextType, apply });
      return;
    }
    restoreFocusRef.current = null;
    apply();
  }

  // Cancelled, the card gives the keys back to what asked, the menu or panel
  // that asked still open under it (a row page's or the view settings'
  // property; Notion's not measured there).
  function closeDialog(restoreFocus: boolean) {
    setPending(null);
    if (restoreFocus) giveFocusBack({ to: restoreFocusRef.current });
    restoreFocusRef.current = null;
  }

  function applyPending() {
    if (!pending || pending.kind === "unsupported") return;
    const { apply } = pending;
    closeDialog(false);
    apply();
  }

  // The app's one confirmation card, over the menu that asked; cancel, the
  // safe answer, takes focus (2026-10-17: it was drawn apart, its buttons
  // side by side).
  const typeChangeConfirmDialog = pending
    ? createPortal(
        <ConfirmCard
          message={pending.kind === "twoWay"
            ? t("propertyTypeChangeConfirm:twoWayEnds")
            : t("propertyTypeChangeConfirm:unsupportedTitle")}
          detail={pending.kind === "twoWay" ? undefined : t("propertyTypeChangeConfirm:unsupportedMessage",
            { propName: pending.propName, typeLabel: localizedPropertyTypeLabel(pending.nextType) },
          )}
          confirmLabel={pending.kind === "unsupported" ? t("common:actions.close") : t("propertyTypeChangeConfirm:twoWayContinue")}
          cancelLabel={pending.kind === "unsupported" ? null : t("common:actions.cancel")}
          danger={pending.kind === "twoWay" ? "filled" : false}
          markers={{ dialog: { "data-property-type-change-confirm": pending.kind } }}
          onAnswer={(answer) => (answer === null || pending.kind === "unsupported" ? closeDialog(true) : applyPending())}
        />,
        document.body
      )
    : null;

  return { confirmPropertyTypeChange, typeChangeConfirmDialog };
}
