import { useTranslation } from "react-i18next";
import { DatabasePropertyPicker } from "@/components/database/DatabasePropertyPicker";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "@/components/shell/PhoneSheet";
import { CheckIcon, PersonCircleIcon, ServerUsersIcon, Trash } from "@/icons/leafen";
import type { PropertyAccessDialogProps } from "@/lib/organizationModule";
import type { PropertyAccessLevel } from "@/lib/propertyAccess";
import { levelText, PRINCIPAL_SECTIONS, propertyAccessLevels, usePrincipalChoices, type PrincipalPickerProps } from "./PropertyAccessParts";
import type { PropertyAccessPrincipalType } from "./propertyAccessApi";
import { LevelPicture } from "./PropertyAccessLevelPicture";
import css from "./PropertyAccessPhone.module.css";

/**
 * A phone's level menu, as Notion's (read 2026-11-18 on a Galaxy A90's
 * Chrome at 411 in a Business database, w1949 and w1954): a whole sheet under
 * an empty head, its levels in one white group — each its 37×25 picture 16px
 * in and 10 before two lines, the name 17px on 22 and what it lets one do
 * 15px on 20 in grey, 10px over and under them, a ✓ 16 from the end at the
 * one chosen; an exception's "예외 제거" under a bin in a group of its own.
 */
export function PhoneLevelSheet({ kind, value, onChoose, onRemove, onClose }: {
  kind: "default" | PropertyAccessPrincipalType;
  value: PropertyAccessLevel;
  onChoose: (level: PropertyAccessLevel) => void;
  onRemove?: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("propertyAccess");
  return (
    <PhoneSheet title={null} label={t(kind === "default" ? "defaultTitle" : "exceptionsTitle")} onClose={onClose}
      data={{ "data-access-level-menu": "" }}>
      <PhoneSheetGroup>
        <div role="menu" className={css.levels}>
          {propertyAccessLevels(kind).map((level) => {
            const text = levelText(t, level, kind);
            return (
              <button key={level} type="button" role="menuitemradio" aria-checked={level === value} className={css.level} data-access-level={level}
                onClick={() => { onChoose(level); onClose(); }}>
                <span className={css.levelPicture}><LevelPicture level={level} valuesOnly={kind === "person_property"} /></span>
                <span className={css.levelText}><span>{text.label}</span><span className={css.levelDescription}>{text.description}</span></span>
                {level === value ? <CheckIcon size="page" className={css.levelCheck} aria-hidden="true" /> : null}
              </button>
            );
          })}
        </div>
      </PhoneSheetGroup>
      {onRemove ? <PhoneSheetGroup>
        <PhoneSheetRow icon={<Trash size="regular" />} label={t("removeException")} danger more={false} data={{ "data-access-remove": "" }}
          onPress={() => { onRemove(); onClose(); }} />
      </PhoneSheetGroup> : null}
    </PhoneSheet>
  );
}

/**
 * A phone's "+ 사용자, 그룹 또는 사용자 속성 추가", as Notion's (read 2026-11-18
 * on a Galaxy A90's Chrome at 411, w1954): a whole sheet under an empty head,
 * the search in a group of its own (its words 8px in), then each section under
 * its grey heading in a group of 45px rows 12px in — a person's 28px face 10
 * before a 16px name, a property's or "페이지 생성자"'s 22px mark in a 24px box
 * 10 before a 17px name.
 */
export function PhonePrincipalSheet({ onPick, onClose, ...rest }: PrincipalPickerProps) {
  const { t } = useTranslation("propertyAccess");
  const { query, setQuery, shown } = usePrincipalChoices(rest);
  return (
    <PhoneSheet title={null} label={t("addException")} onClose={onClose} data={{ "data-access-principal-picker": "" }}>
      <PhoneSheetGroup>
        <label className={`${sheetStyles.row} ${css.search}`}>
          <input className={css.searchField} value={query} placeholder={t("searchPrincipals")} aria-label={t("searchPrincipals")}
            onChange={(event) => setQuery(event.target.value)} />
        </label>
      </PhoneSheetGroup>
      {PRINCIPAL_SECTIONS.map((section) => {
        const rows = shown.filter((choice) => choice.section === section);
        return rows.length ? <PhoneSheetGroup key={section} label={t(`section.${section}`)}>
          {rows.map((choice) => {
            const face = choice.type === "user" || choice.type === "integration";
            return <button key={`${choice.type}:${choice.id}`} type="button" className={`${sheetStyles.row} ${css.principal}`} data-face={face ? "true" : undefined}
              data-access-principal={`${choice.type}:${choice.id}`} onClick={() => { onPick(choice); onClose(); }}>
              <span className={css.principalMark} aria-hidden="true">{face ? <PersonAvatar userId={choice.id} name={choice.label} size={28} />
                : choice.type === "group" ? <ServerUsersIcon size="wide" /> : choice.id === "page_creator" ? <PersonCircleIcon size="wide" />
                  : <PropertyTypeIcon type="person" size="wide" />}</span>
              <span className={sheetStyles.rowLabel}>{choice.label}</span>
            </button>;
          })}
        </PhoneSheetGroup> : null;
      })}
    </PhoneSheet>
  );
}

/** A phone's choice of the rule's property: the property picker's sheet form under an empty head, as the share's own (PropertyAccessShareRules). */
export function PhonePropertySheet({ properties, currentId, onChoose, onClose }: {
  properties: PropertyAccessDialogProps["properties"];
  currentId: string;
  onChoose: (id: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("propertyAccess");
  return (
    <PhoneSheet title={null} label={t("title")} onClose={onClose} data={{ "data-access-property-picker": "" }}>
      <DatabasePropertyPicker placeholder={t("searchProperties")} properties={properties} keepOrder checked={currentId} sheet sheetOpensMore={false}
        onPick={(property) => { onChoose(property.id); onClose(); }} />
    </PhoneSheet>
  );
}
