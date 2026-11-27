import { useLayoutEffect, useRef } from "react";
import type { GraphProperty } from "@/contentGraph";
import { CheckSquareOutlineIcon, SelectChoiceIcon, type LeafenIconSize } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { CheckIcon } from "../icons";
import { statusAfterCheckboxClick, statusCheckboxState } from "./propertyStatusPolicy";
import styles from "./databaseSheet";

/**
 * A status column's cell shown as a checkbox (Notion's "다음과 같이 표시:
 * 체크박스", seen 2026-10-11), and a page's value so (rowPageStatusDisplay.ts):
 * checked when finished, a dash while in progress; a click finishes it, or
 * sets it back.
 */
export function TableStatusCheckbox({ property, value, readOnly, label, onChange }: {
  property: Pick<GraphProperty, "config"> | { config?: unknown };
  value: unknown;
  readOnly: boolean;
  label: string;
  onChange(optionId: string): void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  const state = statusCheckboxState(property.config, value);
  useLayoutEffect(() => { if (ref.current) ref.current.indeterminate = state === "mixed"; }, [state]);
  return (
    <span className={styles.cellCheck} data-status-checkbox={state}>
      <input ref={ref} type="checkbox" aria-label={label} checked={state === "checked"} disabled={readOnly}
        onChange={() => { const next = statusAfterCheckboxClick(property.config, value); if (next) onChange(next); }} />
    </span>
  );
}

/**
 * A "다음과 같이 표시" choice's mark, as Notion's in a column's menu, a
 * property's settings and the phone's sheet alike (seen 2026-11-16): "체크박스"
 * an outlined box with a ✓, not its type's filled one; "선택" a lighter ring
 * than its type's (SelectChoiceIcon).
 */
export function StatusShowAsIcon({ choice, size }: { choice: "checkbox" | "select"; size: LeafenIconSize }) {
  return choice === "checkbox" ? <CheckSquareOutlineIcon size={size} aria-hidden="true" /> : <SelectChoiceIcon size={size} aria-hidden="true" />;
}

/** "다음과 같이 표시:" under its row in a status column's menu: "체크박스" or "선택", each with its mark (StatusShowAsIcon), as Notion's (seen 2026-11-02). */
export function TableStatusDisplayMenu({ label, focusOnOpen, checkbox, choices, onPick }: {
  label: string; focusOnOpen: boolean; checkbox: boolean; choices: { checkbox: string; select: string }; onPick(checkbox: boolean): void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const items = [{ key: "checkbox", label: choices.checkbox }, { key: "select", label: choices.select }];
  // Lit on its first row from the pointer too, as the column menu's other sides (TablePropertyMenu open).
  const menu = useNativeMenu({ items, open: true, menuRef, label, focusOnOpen, highlightOnOpen: true,
    keyOf: (item) => item.key, textOf: (item) => item.label, onAction: (item) => onPick(item.key === "checkbox") });
  return (
    <div {...menu.menuProps} ref={menuRef}>
      {items.map((item) => (
        <NativeMenuItem key={item.key} state={menu.state} itemKey={item.key} className={styles.propertyHeaderItem}>
          {({ labelProps }) => <>
            <StatusShowAsIcon choice={item.key === "checkbox" ? "checkbox" : "select"} size="regular" />
            <span {...labelProps} className={styles.propertyHeaderItemText}>{item.label}</span>
            {(item.key === "checkbox") === checkbox ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </>}
        </NativeMenuItem>
      ))}
    </div>
  );
}
