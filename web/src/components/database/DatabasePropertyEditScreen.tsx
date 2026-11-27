import { SwitchInput } from "@/components/Switch";
import { useEffect, useRef, useState, type ReactNode, useContext } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { ArrowLeft, ArrowsExpandHorizontal, ChevronRight, Copy, EyeIcon, LockIcon, Trash, WrapIcon } from "@/icons/leafen";
import { sprintManagedProperty } from "./databaseSprintContext";
import { DatabaseFixedPropertiesContext, DatabaseTaskRolePropertiesContext } from "./untitledRowName";
import { useInSettingsPanel, useSettingsScreenHead } from "./settingsScreenHead";
import { organizationModule } from "@/lib/organizationModule";
import { propertyAccessRestrictable, requestPropertyAccessDialog } from "@/lib/propertyAccess";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import { ConvertIcon } from "../icons";
import { usePropertyTypeChangeConfirm } from "./PropertyTypeChangeConfirm";
import { propertyTypeHasConfig } from "./PropertyTypeConfig";
import { PropertyAppEditorChoice } from "./PropertyAppEditorChoice";
import { PropertyTypeMenu } from "./PropertyTypeChoices";
import { configWithDescription, propertyDescription } from "./propertyDescription";
import { duplicatedProperty, propertyAfterTypeChange } from "./propertyEditModel";
import { localizedPropertyTypeLabel, propertyTypeLabel } from "./propertyTypes";
import { canDuplicateProperty } from "./rowPropertiesModel";
import { TablePropertyMenuHead, type TablePropertyHeadChange } from "./TablePropertyMenuHead";
import { NotionSelect } from "./NotionSelect";
import { StatusShowAsIcon } from "./TableStatusDisplay";
import { pageShowsStatusCheckbox, propertyAfterPageStatusDisplay } from "./rowPageStatusDisplay";
import {
  propertyAfterFullUrl, propertyShowsFullUrl, tableColumnWrapped, tableConfigAfterStatusDisplay, tableConfigAfterWrap, tableStatusCheckboxIds,
} from "./tablePropertyMenuModel";
import styles from "./databaseSheet";

/**
 * One property's settings in the view settings, as Notion's "속성 편집"
 * (seen 2026-09-30): its icon, name and description; "유형 >", which lists
 * the types it can turn into; its type's own settings; "콘텐츠 줄바꿈하기" for
 * a table's column, and for a status column "다음과 같이 표시:" (seen
 * 2026-10-11); and "속성 복제" and "속성 삭제". "속성 사용 권한" follows "유형"
 * for whoever has full access to the database (seen 2026-10-11), and a table's
 * URL column's "전체 URL 표시" after it (seen 2026-11-11).
 */
export function DatabasePropertyEditScreen({
  property, properties, view, dataSourceId, writable: writableHere, manageAccess = false, typeSettings, focusName = false, popover = false, onUpdate, onUpdateView, onDuplicate,
  onDelete,
}: {
  property: GraphProperty;
  properties: readonly GraphProperty[];
  /** The view it is edited from, whose table can wrap its column; none on a database page opened by its address. */
  view?: GraphView;
  dataSourceId: string;
  writable: boolean;
  /** Whether the person may change the database's property access. */
  manageAccess?: boolean;
  /** The property's type settings (options, number format, formula...). */
  typeSettings: ReactNode;
  /** Puts the caret at the end of the name as it opens, or "select" all of a new one's, as Notion's (seen 2026-11-01). */
  focusName?: boolean | "select";
  /** Shown as a row page property's popover (its menu's "속성 편집"). */
  popover?: boolean;
  onUpdate(property: GraphProperty): void;
  onUpdateView?(view: GraphView): void;
  onDuplicate?(property: GraphProperty): void;
  onDelete?(propertyId: string): void;
}) {
  const { t } = useTranslation(["tableView", "databaseView"]);
  const nameRef = useRef<HTMLInputElement>(null);
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  const [choosingType, setChoosingType] = useState(false);
  // In the view settings, the types take the panel's heading, "← 속성 유형 변경",
  // in place of the name's row, and stand under "유형 🔍" (Notion's, seen 2026-11-11).
  // A row page's popover is the types alone under "유형 🔍", Escape going back
  // to the property's menu (Notion's, seen 2026-11-14 at 1440, after its
  // "AI 자동 채우기" rows, which Leafen has none of); its focus stays on the
  // page there, so the row the keys light shows no ring.
  const inPanel = useInSettingsPanel(), typeHead = inPanel || popover;
  useSettingsScreenHead(choosingType && inPanel ? { title: t("changePropertyType"), onBack: () => setChoosingType(false) } : null);
  // A new property's name chosen whole; a rename's with the caret at its end. Measured 2026-11-15 after the
  // press only (no script in the page before it), Notion in Chrome 1440, a side peek: "+ 속성 추가" › 텍스트, the
  // name chosen whole and "z" typed over it (Leafen the same); a name's 이름 바꾸기, Notion's name chosen whole
  // too and "z" typed over it, where Leafen's "z" went in after it (seen once, left until a second way agrees).
  useEffect(() => {
    const input = nameRef.current;
    if (!focusName || !input) return;
    input.focus();
    if (focusName === "select") input.select();
    else input.setSelectionRange(input.value.length, input.value.length);
  }, [focusName, property.id]);
  const { confirmPropertyTypeChange, typeChangeConfirmDialog } = usePropertyTypeChangeConfirm();
  const presentation = { ...property, databaseId: property.dataSourceId, position: 0, config: property.config as PropertyConfig } as DbProperty;
  // A source's own property (People's) is kept as it is: its settings only show it.
  const fixed = useContext(DatabaseFixedPropertiesContext).has(property.id);
  const writable = writableHere && !fixed;
  const fixedType = property.type === "title" || fixed;
  // A task database's own property keeps its type and stays, as Notion's (seen 2026-11-02).
  const taskRole = useContext(DatabaseTaskRolePropertiesContext).has(property.id);
  // A property its sprints manage keeps its type too; their relations may go (Notion's, seen 2026-11-07).
  const typeLocked = taskRole || sprintManagedProperty(property);
  // The name and description go in one change, so neither undoes the other.
  const sent = useRef({ name: property.name, description: propertyDescription(property) });
  function changeHead(change: TablePropertyHeadChange) {
    let next = property;
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
  const action = (marker: string, icon: ReactNode, label: string, onClick: () => void) => (
    <button type="button" className={styles.layoutAction} data-property-edit-action={marker} disabled={!writable} onClick={onClick}>
      <span className={styles.layoutActionIcon} aria-hidden="true">{icon}</span>
      <span>{label}</span>
    </button>
  );
  // A title's "유형" shows too, faded with a lock, as Notion's (seen 2026-11-11).
  const typeRow = property.config.rollupLocked !== true;
  const typeFixed = fixedType || typeLocked;
  const access = writable && manageAccess && organizationModule()?.PropertyAccessDialog && propertyAccessRestrictable(property);
  const typeSettingsShown = propertyTypeHasConfig(property.type) || property.type === "button";
  const table = view?.type === "table" && onUpdateView ? view : undefined;
  // A table's own rows for it: its wrap and, a status's, how it shows.
  const wrap = table && property.type !== "checkbox" ? table : undefined;
  const statusDisplay = table && property.type === "status" ? table : undefined;
  // A page's property menu has it too, its pages' own and not a view's (rowPageStatusDisplay.ts).
  const pageStatusDisplay = !table && popover && property.type === "status";
  // A URL's "전체 URL 표시" stands where a type's settings do (Notion's, seen 2026-11-11), the
  // property's own wherever its settings open, a page's too (seen 2026-11-14).
  const fullUrl = property.type === "url";
  const duplicate = Boolean(onDuplicate && !fixedType && canDuplicateProperty(presentation));
  const lastGroup = Boolean(wrap || statusDisplay || pageStatusDisplay || duplicate || (onDelete && (taskRole || !fixedType)));
  return (
    <div className={styles.sourcePropertiesMenu} data-property-edit={property.id}>
      {choosingType && typeHead ? null : (
        <TablePropertyMenuHead property={property} readOnly={!writable} nameRef={nameRef} descriptionRef={descriptionRef}
          onChange={changeHead} onUpdate={onUpdate} />
      )}
      {choosingType ? (
        <PropertyTypeMenu property={property} label={t(inPanel ? "changePropertyType" : "changeType")} className={styles.layoutSettings} litOnOpen={false}
          attributes={{ "data-property-type-choices": "", ...(popover ? { "data-ringless": "true" } : {}) }} heading={typeHead ? t("type") : undefined}
          back={typeHead ? undefined : { label: t("changeType"), icon: <ArrowLeft size="regular" />, className: styles.layoutAction,
            iconClassName: styles.layoutActionIcon, run: () => setChoosingType(false) }}
          onChoose={(type) => confirmPropertyTypeChange(presentation, type, () => {
            onUpdate(propertyAfterTypeChange(property, type, dataSourceId));
            setChoosingType(false);
          })} />
      ) : (
        // As Notion's (seen 2026-11-11): the type, its access and the type's own settings, then over a
        // line the table's wrap and status display, duplicate and delete, every row 28px a pixel apart.
        <div className={styles.propertyEditRows}>
          {typeRow ? (
            <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} data-property-type-row
              data-locked={fixedType ? "title" : typeLocked || undefined}
              title={typeLocked ? t("databaseView:taskPropertyTypeLocked", { type: propertyTypeLabel(presentation.type) }) : undefined}
              disabled={!writable || typeFixed} onClick={() => setChoosingType(true)}>
              <span className={styles.layoutRowLabel}><ConvertIcon size="regular" aria-hidden="true" />{t("type")}</span>
              <span className={styles.layoutChoiceValue}>{typeFixed ? <LockIcon size="small" aria-hidden="true" /> : null}
                <span data-sheet-value>{fixedType ? localizedPropertyTypeLabel("title") : propertyTypeLabel(presentation.type)}</span>{typeFixed ? null : <ChevronRight size="regular" aria-hidden="true" />}</span>
            </button>
          ) : null}
          {access ? (
            <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} data-property-access-row
              onClick={() => requestPropertyAccessDialog({ dataSourceId, propertyId: property.id })}>
              <span className={styles.layoutRowLabel}><LockIcon size="regular" aria-hidden="true" />{t("propertyAccess")}</span>
            </button>
          ) : null}
          {fullUrl ? (
            <label className={`${styles.layoutRow} ${styles.layoutSwitchRow}`}>
              <span className={styles.layoutRowLabel}><ArrowsExpandHorizontal size="regular" aria-hidden="true" />{t("databaseView:showFullUrl")}</span>
              <SwitchInput data-property-full-url checked={propertyShowsFullUrl(property, table?.config)} disabled={!writable}
                onChange={() => onUpdate(propertyAfterFullUrl(property, !propertyShowsFullUrl(property, table?.config)))} />
            </label>
          ) : null}
          {typeSettingsShown ? <div data-property-type-settings="">{typeSettings}</div> : null}
          <PropertyAppEditorChoice property={property} writable={writable} onUpdate={onUpdate} />
          {lastGroup && (typeRow || access || fullUrl || typeSettingsShown) ? <div className={styles.layoutDivider} role="separator" /> : null}
          {/* "↩ 콘텐츠 줄바꿈하기", its arrow as the column menu's, on or off (Notion's, seen 2026-11-11). */}
          {wrap ? (
            <label className={`${styles.layoutRow} ${styles.layoutSwitchRow}`}>
              <span className={styles.layoutRowLabel}><WrapIcon size="regular" aria-hidden="true" />{t("wrapText")}</span>
              <SwitchInput data-property-wrap checked={tableColumnWrapped(wrap.config, property.id)} disabled={!writable}
                onChange={() => onUpdateView?.({ ...wrap, config: tableConfigAfterWrap(wrap.config, property.id, properties) })} />
            </label>
          ) : null}
          {/* After the wrap, as Notion's "콘텐츠 줄바꿈하기 → 다음과 같이 표시:" (seen 2026-11-02). */}
          {statusDisplay || pageStatusDisplay ? (
            // Each by its mark (StatusShowAsIcon), the row's value its words alone (Notion's, seen 2026-11-16, a page's and a column's list alike).
            <NotionSelect ariaLabel={t("statusShowAs")} settingLabel={t("statusShowAs")} settingIcon={<EyeIcon size="small" />} disabled={!writable}
              settingValueIcon={false} choiceIcons
              value={(statusDisplay ? tableStatusCheckboxIds(statusDisplay.config).has(property.id) : pageShowsStatusCheckbox(property)) ? "checkbox" : "select"}
              options={[{ value: "checkbox", label: t("statusShowAsCheckbox"), icon: <StatusShowAsIcon choice="checkbox" size="regular" /> },
                { value: "select", label: t("statusShowAsSelect"), icon: <StatusShowAsIcon choice="select" size="regular" /> }]}
              onChange={(value) => statusDisplay
                ? onUpdateView?.({ ...statusDisplay, config: tableConfigAfterStatusDisplay(statusDisplay.config, property.id, value === "checkbox", properties) })
                : onUpdate(propertyAfterPageStatusDisplay(property, value === "checkbox", properties))} />
          ) : null}
          {duplicate && onDuplicate ? action("duplicate", <Copy size="regular" />, t("duplicateProperty"), () => onDuplicate(duplicatedProperty(property, properties, t))) : null}
          {onDelete && !fixedType && !taskRole && action("delete", <Trash size="regular" />, t("deleteProperty"), () => onDelete(property.id))}
          {onDelete && taskRole ? (
            <button type="button" className={styles.layoutAction} data-property-edit-action="delete" disabled title={t("databaseView:taskPropertyRequired")}>
              <span className={styles.layoutActionIcon} aria-hidden="true"><LockIcon size="regular" /></span>
              <span>{t("deleteProperty")}</span>
            </button>
          ) : null}
        </div>
      )}
      {typeChangeConfirmDialog}
    </div>
  );
}
