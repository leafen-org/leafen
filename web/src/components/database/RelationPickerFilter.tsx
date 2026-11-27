import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown, FilterIcon } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import type { Page } from "@/lib/types";
import styles from "./databaseSheet";
import { verificationPropertyValue } from "./structuredPropertyValues";

const RELATION_PICKER_SCOPES = ["active", "all", "verified", "archived"] as const;
export type RelationPickerScope = typeof RELATION_PICKER_SCOPES[number];

/** Whether a row is among those a relation's picker offers now, as Notion's filter (seen 2026-11-04). */
export function relationPickerOffers(scope: RelationPickerScope, page: Page, verificationIds: readonly string[]) {
  if (scope === "all") return true;
  if (scope === "archived") return Boolean(page.archivedAt);
  if (page.archivedAt) return false;
  return scope === "active" || verificationIds.some((id) => verificationPropertyValue(page.properties?.[id])?.state === "verified");
}

/**
 * The picker's filter, as Notion's "≡ ⌄" (seen 2026-11-04): all pages without
 * the archived ones at first, or with them, only verified ones or only
 * archived ones. Its list floats over the picker, which would clip it.
 */
export function RelationPickerFilter({ scope, onScope }: { scope: RelationPickerScope; onScope(scope: RelationPickerScope): void }) {
  const { t } = useTranslation("propertyCell");
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  usePopoverAppear(open, menuRef);
  const { zIndex, onKeyDown } = useOverlayLayer(open, { kind: "picker", ref: menuRef, onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current] });
  const menu = useNativeMenu({ items: [...RELATION_PICKER_SCOPES], open, menuRef, id: menuId, label: t("relationFilter"),
    keyOf: (item) => item, textOf: (item) => t(`relationScope.${item}`), firstKey: scope,
    onAction: (item) => { setOpen(false); onScope(item); } });
  const place = open ? buttonRef.current?.getBoundingClientRect() : null;
  return (
    <>
      <button ref={buttonRef} type="button" className={styles.relationFilterButton} aria-label={t("relationFilter")} title={t("relationFilter")}
        aria-haspopup="menu" aria-expanded={open} aria-controls={open ? menuId : undefined} data-relation-filter
        onClick={() => setOpen((current) => !current)}>
        <FilterIcon size="small" aria-hidden="true" />
        <ChevronDown size="micro" aria-hidden="true" />
      </button>
      {open && place ? createPortal(
        <div {...menu.menuProps} ref={menuRef} className={styles.relationScopeMenu} style={{ zIndex, top: place.bottom + 4, left: place.left }}
          onKeyDown={(event) => { menu.menuProps.onKeyDown?.(event); onKeyDown(event); }}>
          {RELATION_PICKER_SCOPES.map((value) => (
            <NativeMenuItem key={value} itemKey={value} state={menu.state} role="menuitemradio" aria-checked={value === scope}
              data-relation-scope={value}>{({ labelProps }) => <>
              <span className={styles.relationScopeCheck}>{value === scope ? <CheckIcon size="small" aria-hidden="true" /> : null}</span>
              <span {...labelProps}>{t(`relationScope.${value}`)}</span>
            </>}</NativeMenuItem>
          ))}
        </div>, document.body) : null}
    </>
  );
}
