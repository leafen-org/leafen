import { type RefObject, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { keyCameLast } from "@/lib/lastPress";
import { useTypedName } from "@/lib/typedName";
import { PropertyIcon, propertyOwnIcon } from "./PropertyIcon";
import { propertyNameButtonClass } from "./PropertyTypeIcon";
import { configWithDescription, propertyDescription } from "./propertyDescription";
import menuStyles from "./TablePropertyMenu.module.css";
import type { IconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { menuIconButtonProps } from "./menuIconButton";
import { PropertyIconPicker, propertyIconPickerAnchor } from "./PropertyIconPicker";
import { DescribeButton } from "./describeButton";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/** What the head's fields hold as they are left: a name, a description. */
export type TablePropertyHeadChange = { name?: string; description?: string };

/**
 * A column menu's head, as Notion's (seen 2026-09-30): the property's own
 * icon, its name with an "i" at its end that opens a line for the
 * property's description under it, shown whenever there is one. Enter or
 * leaving a field keeps what it holds, and the menu stays; so does closing
 * what holds it (useTypedName): Notion's view settings › 속성 편집 keep a
 * name typed there after a press outside (seen 2026-11-18 in Chrome 1440:
 * "단계p" on the column as the panel closed). What is typed is kept on this
 * device at each key as the change keeping it makes.
 */
export function TablePropertyMenuHead({
  property,
  readOnly,
  phone = false,
  nameRef,
  descriptionRef,
  onChange,
  onUpdate,
}: {
  property: GraphProperty;
  readOnly: boolean;
  /** In a phone's sheet: the row of a white group, its name 17px (TablePropertyMenu.module.css). */
  phone?: boolean;
  nameRef: RefObject<HTMLInputElement | null>;
  descriptionRef: RefObject<HTMLTextAreaElement | null>;
  onChange(change: TablePropertyHeadChange): void;
  onUpdate(property: GraphProperty): void;
}) {
  const { t } = useTranslation("tableView");
  const iconButtonRef = useRef<HTMLButtonElement>(null);
  const [iconAnchor, setIconAnchor] = useState<IconPickerAnchor | null>(null);
  const description = propertyDescription(property);
  const namedKept = useTypedName(property.id, "name", (name) => onChange({ name }), (value) => {
    const name = value.trim();
    return name && name !== property.name ? [{ kind: "upsertProperty", property: { ...property, name } }] : null;
  });
  const describedKept = useTypedName(property.id, "description", (value) => onChange({ description: value }), (value) => {
    const written = value.trim();
    return written !== description ? [{ kind: "upsertProperty", property: { ...property, config: configWithDescription(property.config, written) } }] : null;
  });
  // The description's line shows once ⓘ is pressed, a written one too (seen
  // 2026-11-06: Notion's menu opens without it; the header's ⓘ says it is
  // there), and then stays while the menu is open, even as it is cleared.
  const [describing, setDescribing] = useState(false);
  const showDescription = describing;
  // Closed, the icon picker gives its button the keys back. Escape on one
  // pressed open gives them to the column menu instead, its lit row (the
  // last one pointed at) ringed and a typed letter landing nowhere, as
  // Notion's (measured twice on 2026-11-15 in Chrome 1440 in different tabs,
  // the screen and one typed letter after the key, no script before it; a
  // third read saw the letter land nowhere too). Opened from the keys, Escape
  // still gives its button the keys back (Notion's not measured there).
  const iconOpenedByKey = useRef(false);
  function closeIconPicker(restore = true) {
    setIconAnchor(null);
    const button = iconButtonRef.current;
    const menu = restore ? null : button?.closest<HTMLElement>("[data-table-property-menu]");
    window.requestAnimationFrame(() => (menu ?? button)?.focus({ preventScroll: true }));
  }
  const escapeIconPicker = () => closeIconPicker(iconOpenedByKey.current);
  return (
    <>
      <div className={menuStyles.head} data-phone={phone || undefined} data-property-edit-head>
        {/* The property's own icon, as Notion's "아이콘 바꾸기": an icon picker
            of Notion's icons alone, with "제거" back to the type's icon. */}
        <button ref={iconButtonRef} type="button" className={`${menuStyles.headIcon} ${propertyNameButtonClass}`} disabled={readOnly}
          {...menuIconButtonProps(t("changePropertyIcon"), iconAnchor !== null)} data-property-icon-button
          onClick={(event) => {
            iconOpenedByKey.current = keyCameLast();
            setIconAnchor(propertyIconPickerAnchor(event.currentTarget));
          }}>
          <PropertyIcon property={property} size="regular" />
        </button>
        <span className={menuStyles.nameBox}>
          <input ref={nameRef} key={property.id} className={menuStyles.name} defaultValue={property.name}
            aria-label={t("propertyName")} disabled={readOnly} onChange={(event) => namedKept.typed(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (!fieldEnter(event)) return;
              event.preventDefault();
              namedKept.saved(event.currentTarget.value);
            }}
            onBlur={(event) => namedKept.saved(event.target.value)} />
          <DescribeButton className={menuStyles.describe} disabled={readOnly}
            name={t("addPropertyDescription")} open={showDescription} data-property-description-button
            onClick={() => {
              setDescribing(true);
              focusOpenedField(() => descriptionRef.current);
            }} />
        </span>
      </div>
      {showDescription && (
        <textarea ref={descriptionRef} key={property.id} className={menuStyles.description} defaultValue={description} data-phone={phone || undefined}
          rows={1} maxLength={2000} disabled={readOnly} data-property-description
          aria-label={t("propertyDescription")} placeholder={t("propertyDescriptionPlaceholder")}
          onChange={(event) => describedKept.typed(event.currentTarget.value)}
          onKeyDown={(event) => {
            if (!fieldEnter(event) || event.shiftKey) return;
            event.preventDefault();
            describedKept.saved(event.currentTarget.value);
          }}
          onBlur={(event) => describedKept.saved(event.target.value)} />
      )}
      {iconAnchor ? (
        <PropertyIconPicker anchor={iconAnchor} marker="property-icon-picker" onClose={() => (keyCameLast() ? escapeIconPicker() : closeIconPicker())}
          picker={{
            label: t("changePropertyIcon"), returnFocusFromKeysOnly: true,
            onPickBuiltInIcon: (icon) => {
              onUpdate({ ...property, config: { ...property.config, icon } });
              closeIconPicker();
            },
            onRemove: () => {
              if (propertyOwnIcon(property)) {
                const { icon: _removed, ...config } = property.config;
                onUpdate({ ...property, config });
              }
              closeIconPicker();
            },
          }} />
      ) : null}
    </>
  );
}
