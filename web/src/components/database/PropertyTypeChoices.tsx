import { useRef, useState, type MutableRefObject, type ReactNode } from "react";
import type { GraphProperty } from "@/contentGraph";
import { CheckIcon } from "@/icons/leafen";
import type { PropertyType } from "@/lib/types";
import { NativeMenuItem, useNativeMenu, useOpenedFromKeyboard } from "@/lib/nativeMenu";
import { editablePropertyTypes } from "./propertyTypes";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { PropertyTypeHeadedList, type TypeListNameKeys } from "./PropertyTypeHeadedList";
import styles from "./databaseSheet";

/**
 * The types a property can turn into, with the row page's names and icons:
 * the column menu's "유형 변경" and the property settings' "유형" list them,
 * the current type marked at its line's end, as Notion's (seen 2026-11-06).
 * The column menu's lights its first type as it opens, from the pointer or
 * the keys, its first ↓ the second; the property settings' lights none when
 * pressed open, its first ↓ its first row (`litOnOpen`, seen 2026-11-14).
 * A list under a heading — the property settings' "유형" and the view
 * settings' "새 속성" — stands under it and its 🔍, which opens a search over
 * the types, with the one-column type lists' keys (PropertyTypeHeadedList).
 */
export function PropertyTypeMenu(props: {
  /** The property whose type changes, its type marked; none for a property not made yet. */
  property?: GraphProperty;
  /** The types listed in place of those the property can change to (a new property's). */
  choices?: readonly { type: PropertyType; label: string; disabled?: boolean }[];
  label: string; onChoose(type: PropertyType): void;
  /** A leading row back to where the list was opened from. */
  back?: { label: string; icon: ReactNode; className: string; iconClassName: string; run(): void };
  /** The list's heading, with its search. */
  heading?: string;
  className?: string; focusOnOpen?: boolean; attributes?: Record<string, string>;
  /** Whether its first type is lit as it opens from the pointer; opened from the keys it is lit either way. */
  litOnOpen?: boolean;
  /** A heading's list: the name over it, whose arrows move its mark. */
  nameKeysRef?: MutableRefObject<TypeListNameKeys | null>;
}) {
  const { property, choices, label, onChoose, heading, className, focusOnOpen, litOnOpen, attributes, nameKeysRef } = props;
  return heading ? <PropertyTypeHeadedList property={property} choices={choices} label={label} onChoose={onChoose} heading={heading}
    className={className} focusOnOpen={focusOnOpen} litOnOpen={litOnOpen} attributes={attributes} nameKeysRef={nameKeysRef} />
    : <PropertyTypeChoiceMenu {...props} />;
}

function PropertyTypeChoiceMenu({ property, choices, label, onChoose, back, className, focusOnOpen = true, litOnOpen = true, attributes }: {
  property?: GraphProperty;
  choices?: readonly { type: PropertyType; label: string; disabled?: boolean }[];
  label: string; onChoose(type: PropertyType): void;
  back?: { label: string; icon: ReactNode; className: string; iconClassName: string; run(): void };
  className?: string; focusOnOpen?: boolean; attributes?: Record<string, string>; litOnOpen?: boolean;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  type Row = { key: string; label: string; type?: PropertyType; disabled?: boolean; run(): void };
  const rows: Row[] = [
    ...(back ? [{ key: "back", label: back.label, run: back.run }] : []),
    ...(choices ?? editablePropertyTypes((property?.type ?? "rich_text") as PropertyType))
      .map(({ type, label: name, disabled }) => ({ key: type, label: name, type, disabled, run: () => onChoose(type) })),
  ];
  const firstType = rows.find((row) => row.type && !row.disabled)?.key;
  // Opened from the keys, its first type is lit either way.
  const keyboardNow = useOpenedFromKeyboard(), [lightFirst] = useState(() => litOnOpen || keyboardNow);
  const menu = useNativeMenu({ items: rows, open: true, menuRef, label, focusOnOpen, firstKey: lightFirst ? firstType : undefined,
    highlightOnOpen: lightFirst, quietOnPointer: !lightFirst,
    disabledKeys: rows.filter((row) => row.disabled).map((row) => row.key),
    keyOf: (row) => row.key, textOf: (row) => row.label, onAction: (row) => { if (!row.disabled) row.run(); } });
  const lit = menu.state.selectionManager.focusedKey;
  return (
    <div {...menu.menuProps} ref={menuRef} className={className} {...attributes}>
      {rows.map((row) => row.type ? (
        <NativeMenuItem key={row.key} state={menu.state} itemKey={row.key} role="menuitemradio" aria-checked={property?.type === row.type}
          disabled={row.disabled} className={styles.propertyHeaderTypeOption} data-property-type={row.type}
          data-active={lit === row.key ? "true" : undefined}>
          {({ labelProps }) => <>
            <PropertyTypeIcon type={row.type!} size="regular" /> <span {...labelProps}>{row.label}</span>
            {property?.type === row.type ? <CheckIcon size="small" aria-hidden="true" className={styles.propertyHeaderTypeCheck} /> : null}
          </>}
        </NativeMenuItem>
      ) : (
        <NativeMenuItem key={row.key} state={menu.state} itemKey={row.key} className={back!.className}>
          {({ labelProps }) => <><span className={back!.iconClassName} aria-hidden="true">{back!.icon}</span><span {...labelProps}>{row.label}</span></>}
        </NativeMenuItem>
      ))}
    </div>
  );
}
