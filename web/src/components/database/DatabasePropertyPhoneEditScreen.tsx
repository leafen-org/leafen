import { useContext, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { ArrowsExpandHorizontal, Copy, EyeIcon, LockIcon, Trash, WrapIcon } from "@/icons/leafen";
import { organizationModule } from "@/lib/organizationModule";
import { propertyAccessRestrictable, requestPropertyAccessDialog } from "@/lib/propertyAccess";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import { PhoneSheetGroup, PhoneSheetRow, PhoneSheetSwitch, phoneSheetStyles } from "../shell/PhoneSheet";
import { ConvertIcon } from "../icons";
import type { DatabasePropertyEditScreen } from "./DatabasePropertyEditScreen";
import { PhonePropertyTypeGroup } from "./PhonePropertySettings";
import { PhonePropertyTypeScreen } from "./PhonePropertyTypeScreen";
import phoneStyles from "./PhonePropertySettings.module.css";
import { sprintManagedProperty } from "./databaseSprintContext";
import { usePropertyTypeChangeConfirm } from "./PropertyTypeChangeConfirm";
import { propertyTypeHasConfig } from "./PropertyTypeConfig";
import { PropertyAppEditorChoice } from "./PropertyAppEditorChoice";
import { configWithDescription, propertyDescription } from "./propertyDescription";
import { duplicatedProperty, propertyAfterTypeChange } from "./propertyEditModel";
import { localizedPropertyTypeLabel, propertyTypeLabel } from "./propertyTypes";
import { canDuplicateProperty } from "./rowPropertiesModel";
import { pageShowsStatusCheckbox, propertyAfterPageStatusDisplay } from "./rowPageStatusDisplay";
import { TablePropertyMenuHead, type TablePropertyHeadChange } from "./TablePropertyMenuHead";
import { StatusShowAsIcon } from "./TableStatusDisplay";
import { PhoneSheetChoices } from "./PhoneSheetChoices";
import {
  propertyAfterFullUrl, propertyShowsFullUrl, tableColumnWrapped, tableConfigAfterStatusDisplay, tableConfigAfterWrap, tableStatusCheckboxIds,
} from "./tablePropertyMenuModel";
import { DatabaseTaskRolePropertiesContext } from "./untitledRowName";

/**
 * A property's settings on a phone, as Notion's sheet "속성 편집" (seen
 * 2026-11-11 at 402pt in iOS Safari, from a database page's property menu):
 * its icon, name and "i" in a white group of their own; "유형 ›" with its
 * type, "속성 사용 권한" and the type's own settings in the next, what
 * those open ("옵션 +", "표시 옵션") hanging under it (PhonePropertyTypeGroup); then
 * a table's "콘텐츠 줄바꿈하기" and a status's "다음과 같이 표시: ›" over
 * "속성 복제" and "속성 삭제" in a last group, none red. "유형 ›" turns the
 * sheet into the types it can turn into (PhonePropertyTypeScreen).
 * The desktop's settings are DatabasePropertyEditScreen; this takes its props.
 */
export function DatabasePropertyPhoneEditScreen({
  property, properties, view, dataSourceId, writable, manageAccess = false, typeSettings, popover = false, onUpdate, onUpdateView, onDuplicate,
  onDelete,
}: Parameters<typeof DatabasePropertyEditScreen>[0]) {
  const { t } = useTranslation(["tableView", "databaseView"]);
  const nameRef = useRef<HTMLInputElement>(null);
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const [sheet, setSheet] = useState<"type" | "statusDisplay" | null>(null);
  const { confirmPropertyTypeChange, typeChangeConfirmDialog } = usePropertyTypeChangeConfirm();
  const presentation = { ...property, databaseId: property.dataSourceId, position: 0, config: property.config as PropertyConfig } as DbProperty;
  const fixedType = property.type === "title";
  const taskRole = useContext(DatabaseTaskRolePropertiesContext).has(property.id);
  const typeLocked = taskRole || sprintManagedProperty(property);
  // The name and description go in one change, so neither undoes the other.
  const sent = useRef({ name: property.name, description: propertyDescription(property) });
  function changeHead(change: TablePropertyHeadChange) {
    let next: GraphProperty = property;
    const name = change.name?.trim();
    if (name && name !== property.name && name !== sent.current.name) {
      sent.current.name = name;
      next = { ...next, name };
    }
    const description = change.description?.trim();
    if (description !== undefined && description !== propertyDescription(property) && description !== sent.current.description) {
      sent.current.description = description;
      next = { ...next, config: configWithDescription(next.config, description) };
    }
    if (next !== property) onUpdate(next);
  }
  // A title's "유형" shows too, grey with a lock, as Notion's (seen 2026-11-11).
  const typeRows = property.config.rollupLocked !== true;
  const typeFixed = fixedType || typeLocked;
  // The type's own settings (its sort, format, limit...) go on in the same group, as Notion's.
  const typeSettingsShown = propertyTypeHasConfig(property.type) || property.type === "button";
  const access = writable && manageAccess && organizationModule()?.PropertyAccessDialog && propertyAccessRestrictable(property);
  const duplicate = onDuplicate && !fixedType && canDuplicateProperty(presentation);
  const table = view?.type === "table" && onUpdateView ? view : undefined;
  // A table's own rows for it: its wrap and, a status's, how it shows; a page's property menu has
  // the status's too, its pages' own and not a view's (rowPageStatusDisplay.ts), as the desktop's.
  const wrap = table && property.type !== "checkbox" ? table : undefined;
  const statusDisplay = property.type === "status" && (Boolean(table) || popover);
  const statusCheckbox = table ? tableStatusCheckboxIds(table.config).has(property.id) : pageShowsStatusCheckbox(property);
  // A URL's "전체 URL 표시" goes on in the type's group, the property's own, as the desktop's.
  const fullUrl = property.type === "url";
  const row = (key: string, icon: ReactNode, label: string, run: () => void, extra: Partial<Parameters<typeof PhoneSheetRow>[0]> = {}) => (
    <PhoneSheetRow key={key} icon={icon} label={label} more={false} disabled={!writable} onPress={run} {...extra}
      data={{ "data-property-edit-action": key, "data-phone-setting-row": "", ...extra.data }} />
  );
  // "유형 ›" turns the sheet into its types, as Notion's (PhonePropertyTypeScreen); one chosen, the settings come back.
  if (sheet === "type") return (
    <div data-property-edit={property.id} data-phone-property-edit="">
      <PhonePropertyTypeScreen property={property} onBack={() => setSheet(null)}
        onChoose={(type) => confirmPropertyTypeChange(presentation, type, () => {
          onUpdate(propertyAfterTypeChange(property, type, dataSourceId));
          setSheet(null);
        })} />
      {typeChangeConfirmDialog}
    </div>
  );
  return (
    // Its groups 16px apart, in a sheet of its own or the view settings' list.
    <div data-property-edit={property.id} data-phone-property-edit="" className={phoneSheetStyles.stack}>
      <PhoneSheetGroup>
        <TablePropertyMenuHead phone property={property} readOnly={!writable} nameRef={nameRef} descriptionRef={descriptionRef}
          onChange={changeHead} onUpdate={onUpdate} />
      </PhoneSheetGroup>
      {typeRows || access || fullUrl || typeSettingsShown ? (
        <PhonePropertyTypeGroup settings={typeSettingsShown ? typeSettings : null} rows={<>
          {typeRows ? (
            <PhoneSheetRow icon={<ConvertIcon size="regular" />} label={t("type")} disabled={!writable || typeFixed} more={!typeFixed}
              value={<>{typeFixed ? <LockIcon size="small" className={phoneStyles.typeLock} aria-hidden="true" /> : null}{fixedType ? localizedPropertyTypeLabel("title") : propertyTypeLabel(presentation.type)}</>}
              onPress={() => setSheet("type")} data={{ "data-property-type-row": "", "data-phone-setting-row": "" }} />
          ) : null}
          {access ? row("access", <LockIcon size="regular" />, t("propertyAccess"),
            () => requestPropertyAccessDialog({ dataSourceId, propertyId: property.id }), { data: { "data-property-access-row": "" } }) : null}
          {fullUrl ? row("fullUrl", <ArrowsExpandHorizontal size="regular" />, t("databaseView:showFullUrl"),
            () => onUpdate(propertyAfterFullUrl(property, !propertyShowsFullUrl(property, table?.config))),
            { value: <PhoneSheetSwitch on={propertyShowsFullUrl(property, table?.config)} />, data: { "data-property-full-url": "" } }) : null}
        </>} />
      ) : null}
      <PropertyAppEditorChoice property={property} writable={writable} onUpdate={onUpdate} />
      {wrap || statusDisplay || duplicate || onDelete ? (
        <PhoneSheetGroup>
          {wrap ? row("wrap", <WrapIcon size="regular" />, t("wrapText"),
            () => onUpdateView?.({ ...wrap, config: tableConfigAfterWrap(wrap.config, property.id, properties) }),
            { value: <PhoneSheetSwitch on={tableColumnWrapped(wrap.config, property.id)} /> }) : null}
          {statusDisplay ? row("statusDisplay", <EyeIcon size="regular" weight="regular" />, t("statusShowAs"), () => setSheet("statusDisplay"),
            { more: true, value: statusCheckbox ? t("statusShowAsCheckbox") : t("statusShowAsSelect") }) : null}
          {duplicate ? row("duplicate", <Copy size="regular" />, t("duplicateProperty"),
            () => onDuplicate?.(duplicatedProperty(property, properties, t))) : null}
          {onDelete && !fixedType && !taskRole ? row("delete", <Trash size="regular" />, t("deleteProperty"), () => onDelete(property.id)) : null}
          {onDelete && taskRole ? row("delete", <LockIcon size="regular" />, t("deleteProperty"), () => {}, { disabled: true }) : null}
        </PhoneSheetGroup>
      ) : null}
      {/* As Notion's (seen 2026-11-16 at 402pt in iOS Safari, from a page's property menu): its choices over this
          sheet (PhoneSheetChoices), "체크박스" then "선택" by their marks (StatusShowAsIcon), the one shown ticked. */}
      {sheet === "statusDisplay" && statusDisplay ? (
        <PhoneSheetChoices label={t("statusShowAs")} value={statusCheckbox ? "checkbox" : "select"} onClose={() => setSheet(null)}
          data={{ "data-status-display-sheet": "" }}
          choices={(["checkbox", "select"] as const).map((key) => ({ key, icon: <StatusShowAsIcon choice={key} size="regular" />,
            label: t(key === "checkbox" ? "statusShowAsCheckbox" : "statusShowAsSelect") }))}
          onPick={(key) => {
            if (table) onUpdateView?.({ ...table, config: tableConfigAfterStatusDisplay(table.config, property.id, key === "checkbox", properties) });
            else onUpdate(propertyAfterPageStatusDisplay(property, key === "checkbox", properties));
          }} />
      ) : null}
      {typeChangeConfirmDialog}
    </div>
  );
}
