import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useDataSourceHasUniqueId, type UniqueIdSource } from "@/lib/databaseUniqueIdState";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { PropertyType } from "@/lib/types";
import { BuiltInIconGlyph } from "../page/PageIcon";
import { parseNativeIconValue } from "../page/nativeIcons";
import { propertyNameButtonClass, PropertyTypeIcon } from "./PropertyTypeIcon";
import { PropertyTypeMenu } from "./PropertyTypeChoices";
import type { TypeListNameKeys } from "./PropertyTypeHeadedList";
import { propertyTypeLabel } from "./propertyTypes";
import { ROW_PAGE_ADD_PROPERTY_TYPES } from "./tableAddPropertyModel";
import styles from "./databaseSheet";
import menuStyles from "./TablePropertyMenu.module.css";
import own from "./DatabaseNewPropertyScreen.module.css";
import type { IconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { menuIconButtonProps } from "./menuIconButton";
import { PropertyIconPicker, propertyIconPickerAnchor } from "./PropertyIconPicker";
import { markNewPropertyOpened } from "./newPropertyOpening";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/** A new property's name, icon and type, as its screen hands them on. */
export type NewPropertyChoice = { type: PropertyType; name: string; icon?: string };

/**
 * The view settings' "+ 새 속성", as Notion's (seen 2026-11-17 at 1440 in
 * Chrome, the panel's own screen under "← 〈database〉의 새 속성", its "←"
 * back to the properties): the icon button ("아이콘 바꾸기", the text type's
 * mark until an icon is picked) beside "속성 이름", which takes the keys,
 * then "유형" with its 🔍 over the types in the order of a database page's
 * "속성 추가" — Notion's "AI 자동 채우기" rows between them Leafen has none of,
 * nor the line under them. A type picked makes the property named
 * as typed, else for its type; the name's arrows move the types' mark,
 * keeping the keys (seen 2026-11-21); Enter in it makes the type marked,
 * else a text property, as Notion's ("q" then Enter, 2026-11-17). Either way the property's own
 * settings follow, its name chosen whole, and their "←" comes back here.
 * Opened first since the document loaded, it takes the turn of a database
 * page's "속성 추가" whose keys would go to the types' search (newPropertyOpening).
 */
export function DatabaseNewPropertyScreen({ idSource, onMake }: {
  /** The data source, whose ID property dims "ID". */
  idSource: UniqueIdSource;
  onMake(choice: NewPropertyChoice): void;
}) {
  const { t } = useTranslation(["tableView", "rowProperties"]);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState<string | null>(null);
  const [iconAnchor, setIconAnchor] = useState<IconPickerAnchor | null>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const typeKeys = useRef<TypeListNameKeys | null>(null);
  useEffect(() => markNewPropertyOpened(), []);
  const idDimmed = useDataSourceHasUniqueId(idSource.dataSourceId, idSource.has, true);
  const choices = useMemo(() => ROW_PAGE_ADD_PROPERTY_TYPES.map((type) => ({
    type, label: propertyTypeLabel(type), ...(type === "unique_id" && idDimmed ? { disabled: true } : {}),
  })), [idDimmed]);
  const chosenIcon = icon ? parseNativeIconValue(icon) : undefined;
  const make = (type: PropertyType) => onMake({ type, name, ...(icon ? { icon } : {}) });
  function closeIconPicker() {
    setIconAnchor(null);
    focusOpenedField(() => nameRef.current, { scroll: false });
  }
  return (
    <>
      <div className={`${menuStyles.head} ${own.head}`} data-new-property-head>
        <button type="button" className={`${menuStyles.headIcon} ${propertyNameButtonClass}`}
          {...menuIconButtonProps(t("changePropertyIcon"), iconAnchor !== null)} data-new-property-icon-button
          onClick={(event) => {
            setIconAnchor(propertyIconPickerAnchor(event.currentTarget));
          }}>
          {chosenIcon ? <BuiltInIconGlyph icon={chosenIcon} size={18} /> : <PropertyTypeIcon type="rich_text" size="regular" />}
        </button>
        <input ref={nameRef} autoFocus={fieldFocusOnOpen()} className={menuStyles.name} value={name} placeholder={t("propertyName")}
          aria-label={t("propertyName")} data-new-property-name onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (imeComposing(event) || typeKeys.current?.onNameKeyDown(event) || !fieldEnter(event)) return;
            event.preventDefault();
            const marked = typeKeys.current?.marked();
            make(marked && !(marked === "unique_id" && idDimmed) ? marked : "rich_text");
          }} />
      </div>
      <PropertyTypeMenu choices={choices} label={t("rowProperties:newPropertyType")} className={`${styles.layoutSettings} ${own.types}`}
        focusOnOpen={false} litOnOpen={false} heading={t("type")} attributes={{ "data-new-property-types": "" }} nameKeysRef={typeKeys}
        onChoose={(type) => { if (!(type === "unique_id" && idDimmed)) make(type); }} />
      {iconAnchor ? (
        <PropertyIconPicker anchor={iconAnchor} marker="new-property-icon-picker" onClose={closeIconPicker}
          picker={{
            label: t("changePropertyIcon"),
            onPickBuiltInIcon: (picked) => { setIcon(picked); closeIconPicker(); },
            onRemove: () => { setIcon(null); closeIconPicker(); },
          }} />
      ) : null}
    </>
  );
}
