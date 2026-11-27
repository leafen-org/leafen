import { useRef, useState } from "react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import type { PropertyType } from "@/lib/types";
import { SearchClearButton } from "../PageActionSearch";
import { Search } from "../icons";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles, usePhoneSheetHead } from "../shell/PhoneSheet";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { editablePropertyTypes, propertyTypeFound } from "./propertyTypes";
import { PhoneSheetTick } from "./TablePropertyPhoneSheet";
import styles from "./PhonePropertyTypeScreen.module.css";

/**
 * A property's "유형 ›" on a phone, as Notion's (seen 2026-11-17 at 402pt in iOS
 * Safari, from a database page's property menu and from the view settings'
 * "속성 편집"): a screen inside the same sheet, not a sheet over it — its head
 * "‹ 속성 유형 변경", "‹" back to the settings, the sheet as high as it was, a
 * drag closing the whole sheet. "유형" in grey stands over the types with its
 * 🔍, which opens "유형 검색 중…" in a white group under it, the caret and the
 * keyboard there, its "ⓧ" once it holds words; pressed again it shuts the
 * field and lists every type (Notion's kept the hidden words' filter and
 * listed none). The types are one white group, the current one ticked; none
 * found, nothing is listed. Notion's "AI 자동 채우기" group over them Leafen
 * has none of. Outside a sheet it is a sheet of its own.
 */
export function PhonePropertyTypeScreen({ property, onBack, onChoose }: {
  property: GraphProperty;
  onBack(): void;
  onChoose(type: PropertyType): void;
}) {
  const { t } = useTranslation(["tableView", "rowProperties", "pageLayout"]);
  const title = t("tableView:changePropertyType");
  const inSheet = usePhoneSheetHead({ title, onBack });
  const searchRef = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState<string | null>(null);
  const types = editablePropertyTypes(property.type as PropertyType).filter(({ type }) => !search || propertyTypeFound(type, search));
  function toggleSearch() {
    // Drawn and focused within the press, so a phone's keyboard comes up with the caret; the browser then brings
    // the field over the keyboard as it does a field tapped in a sheet (holdPhoneWindow leaves a layer's field to it).
    flushSync(() => setSearch((current) => current === null ? "" : null));
    searchRef.current?.focus();
  }
  const screen = (
    <div className={phoneSheetStyles.stack} data-property-type-choices="" data-phone-type-screen="">
      <div className={styles.head}>
        <h3 className={styles.label}>
          {t("tableView:type")}
          <button type="button" className={styles.searchButton} aria-label={t("rowProperties:searchPropertyTypes")}
            aria-pressed={search !== null} data-property-type-search-button onClick={toggleSearch}>
            <Search size="tiny" aria-hidden="true" />
          </button>
        </h3>
        {search === null ? null : (
          <PhoneSheetGroup>
            <label className={styles.field}>
              <input ref={searchRef} className={styles.search} value={search} placeholder={t("pageLayout:searchingTypes")}
                aria-label={t("rowProperties:searchPropertyTypes")} data-property-type-search
                onChange={(event) => setSearch(event.target.value)} />
              <SearchClearButton value={search} inputRef={searchRef} onClear={() => setSearch("")} />
            </label>
          </PhoneSheetGroup>
        )}
      </div>
      {types.length ? (
        <PhoneSheetGroup>
          {types.map(({ type, label, disabled }) => (
            <PhoneSheetRow key={type} icon={<PropertyTypeIcon type={type} size="regular" />} label={label} disabled={disabled} more={false}
              value={property.type === type ? <PhoneSheetTick /> : undefined} data={{ "data-property-type": type }}
              onPress={() => onChoose(type)} />
          ))}
        </PhoneSheetGroup>
      ) : null}
    </div>
  );
  return inSheet ? screen : (
    <PhoneSheet title={title} onClose={onBack} onBack={onBack} data={{ "data-submenu": "type" }}>{screen}</PhoneSheet>
  );
}
