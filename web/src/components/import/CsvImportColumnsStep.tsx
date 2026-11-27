import { ArrowRight, CheckIcon, ChevronDown, ChevronRight, EyeSlashIcon, Plus } from "@/icons/leafen";
import { CSV_MERGE_NEW_TYPES, chooseCSVMergeProperty, csvMergeMenuProperties, csvMergeNewType, type CSVMergeChoice } from "@/lib/csvMergeImportModel";
import { CSV_DATE_FORMATS, CSV_NEW_DATABASE_TYPES, csvChoiceType, type CSVNewDatabaseChoice } from "@/lib/csvImportModel";
import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import type { WorkspaceCSVMergeTarget } from "../../workspaceImportWire.generated";
import { PropertyTypeIcon } from "../database/PropertyTypeIcon";
import { localizedPropertyTypeLabel } from "../database/propertyTypes";
import type { PropertyType } from "@/lib/types";
import { SwitchInput } from "@/components/Switch";
import { SearchClearButton } from "@/components/PageActionSearch";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu } from "@/lib/nativeMenu";
import { placeBeside, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { placeBesideField } from "./CsvImportPlacePicker";
import { touchScreen } from "@/lib/screenKind";
import styles from "./CsvImportDialog.module.css";
import pickerStyles from "./CsvImportPlacePicker.module.css";

type Choices =
  | { mode: "new"; choices: readonly CSVNewDatabaseChoice[]; onChoices(next: CSVNewDatabaseChoice[]): void }
  | { mode: "merge"; target: WorkspaceCSVMergeTarget; choices: readonly CSVMergeChoice[]; onChoices(next: CSVMergeChoice[]): void };

/**
 * "CSV 열 설정" in the CSV window, as Notion's (seen 2026-10-01): whether the
 * first row is the header, then each column with the property type it
 * takes, chosen from a 240px menu, and "CSV 가져오기" and "취소하기" below,
 * which stay at the window's foot while many columns scroll (seen 2026-11-12).
 */
export function CsvImportColumnsStep({ names, header, onHeader, columns, busy, error, onImport, onCancel }: {
  names: readonly string[];
  header: boolean;
  onHeader(next: boolean): void;
  columns: Choices;
  busy: boolean;
  error: string;
  onImport(): void;
  onCancel(): void;
}) {
  const { t } = useTranslation(["importDialog", "propertyCell"]);
  const [menu, setMenu] = useState<{ column: number; anchor: PopoverRect } | null>(null);
  const ready = columns.mode === "new"
    ? columns.choices.includes("title")
    : columns.choices.some(Boolean);
  const label = (index: number) => {
    if (columns.mode === "new") {
      const choice = columns.choices[index] ?? "";
      return choice ? typeLabel(choice) : t("importDialog:csvMerge.skipColumn");
    }
    const choice = columns.choices[index] ?? "";
    if (choice.startsWith("property:")) {
      return columns.target.properties.find((property) => `property:${property.id}` === choice)?.name ?? "";
    }
    const made = csvMergeNewType(choice);
    if (!made) return t("importDialog:csvMerge.skipColumn");
    const name = localizedPropertyTypeLabel(made.type as PropertyType);
    return made.dateFormat ? `${name} • ${t(`propertyCell:dateFormat.${made.dateFormat}`)}` : name;
  };
  // The kind a column takes, for its mark; a skipped column has none.
  const typeOf = (index: number): PropertyType | undefined => {
    if (columns.mode === "new") {
      const choice = columns.choices[index];
      return choice ? csvChoiceType(choice).type as PropertyType : undefined;
    }
    const choice = columns.choices[index] ?? "";
    if (choice.startsWith("property:")) return columns.target.properties.find((property) => `property:${property.id}` === choice)?.type as PropertyType | undefined;
    return csvMergeNewType(choice)?.type as PropertyType | undefined;
  };
  // A date names its format after a dot, as Notion's "날짜 • 전체 날짜".
  const typeLabel = (choice: CSVNewDatabaseChoice) => {
    const { type, dateFormat } = csvChoiceType(choice);
    if (type === "title") return t("importDialog:csvWindow.titleType");
    const name = localizedPropertyTypeLabel(type as PropertyType);
    return dateFormat ? `${name} • ${t(`propertyCell:dateFormat.${dateFormat}`)}` : name;
  };

  return (
    <>
      <h2 className={styles.title}>{t("importDialog:csvMerge.mapping")}</h2>
      <p className={styles.hint} data-mapping="true">{t("importDialog:csvMerge.mappingHint")}</p>
      <label className={styles.switchRow}>
        <SwitchInput checked={header} disabled={busy}
          data-csv-header-row="true" onChange={(event) => onHeader(event.currentTarget.checked)} />
        {t("importDialog:csvWindow.firstRowHeader")}
      </label>
      {/* Only the columns scroll, their heads with them; the buttons stay at the foot (Notion's, seen 2026-11-12). */}
      <div className={styles.columnsScroll} data-csv-columns-scroll="true">
        <div className={styles.columns} role="group" aria-label={t("importDialog:csvMerge.mapping")}>
          <span className={styles.columnsHead}>{t("importDialog:csvMerge.csvColumn")}</span>
          <span aria-hidden="true" />
          <span className={styles.columnsHead}>{t("importDialog:csvMerge.propertyType")}</span>
          {names.map((name, index) => (
            <ColumnRow key={index} name={name} label={label(index)} type={typeOf(index)} skipped={!(columns.choices[index] ?? "")}
              disabled={busy} column={index} expanded={menu?.column === index}
              onOpen={(anchor) => setMenu({ column: index, anchor })} />
          ))}
        </div>
      </div>
      <div className={styles.foot}>
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
        <button type="button" className={styles.primary} disabled={busy || !ready} data-csv-import="true" onClick={onImport}>
          {busy ? t("importDialog:csvWindow.importing") : t("importDialog:csvWindow.import")}
        </button>
        <button type="button" className={styles.secondary} onClick={onCancel}>{t("importDialog:csvWindow.cancel")}</button>
      </div>
      {menu ? (
        <TypeMenu anchor={menu.anchor} columns={columns} column={menu.column} typeLabel={typeLabel}
          onClose={() => setMenu(null)} />
      ) : null}
    </>
  );
}

function ColumnRow({ name, label, type, skipped, disabled, column, expanded, onOpen }: {
  name: string;
  label: string;
  type?: PropertyType;
  skipped: boolean;
  disabled: boolean;
  column: number;
  /** Its type menu is open: Escape closes it with the focus left here (routeOverlayKeyDown). */
  expanded: boolean;
  onOpen(anchor: PopoverRect): void;
}) {
  return (
    <>
      <span className={styles.columnName} title={name}>{name}</span>
      <ArrowRight size="small" aria-hidden="true" className={styles.columnArrow} />
      <button type="button" className={styles.typeButton} disabled={disabled} aria-haspopup="menu" aria-expanded={expanded}
        aria-label={`${name}: ${label}`} data-csv-column={column} data-skipped={skipped ? "true" : undefined}
        onClick={(event) => onOpen(popoverAnchorOf(event.currentTarget))}>
        {type ? <PropertyTypeIcon type={type} /> : <EyeSlashIcon size="small" aria-hidden="true" />}
        <span>{label}</span>
        <ChevronDown size="micro" aria-hidden="true" />
      </button>
    </>
  );
}

/**
 * A column's types: for a new database, "가져오지 않음" and the types a new
 * database takes; for a database's rows, "가져오지 않음" and its properties
 * under "기존 속성", its title property last with a gray "제목" tag
 * after its name, then, under a line, the new property types under "새 속성
 * 추가" (seen 2026-11-13).
 */
type TypeAction = {
  key: string; text: string; checked: boolean; icon?: ReactNode; tag?: string;
  choose?: () => void; submenu?: "date" | "new";
};

/**
 * A row's mark: every kind's own, in gray, and "가져오지 않음"'s crossed eye,
 * as Notion's type menu and its "Add new property" draw them; "새 속성 추가"
 * a plus in the words' color (seen 2026-11-13). A date's formats have none.
 */
const typeMark = (type: PropertyType) => <span aria-hidden="true" className={styles.typeMenuMark}>
  <PropertyTypeIcon type={type} size="regular" /></span>;
const skipMark = <span aria-hidden="true" className={styles.typeMenuMark}><EyeSlashIcon size="regular" /></span>;
const newPropertyMark = <span aria-hidden="true"><Plus size="regular" /></span>;

function TypeMenu({ anchor, columns, column, typeLabel, onClose }: {
  anchor: PopoverRect;
  columns: Choices;
  column: number;
  typeLabel(type: CSVNewDatabaseChoice): string;
  onClose(): void;
}) {
  const { t } = useTranslation(["importDialog", "propertyCell"]);
  const ref = useRef<HTMLDivElement>(null);
  const dateTrigger = useRef<HTMLButtonElement>(null), newTrigger = useRef<HTMLButtonElement>(null);
  const dateRef = useRef<HTMLDivElement>(null), newRef = useRef<HTMLDivElement>(null);
  const current = columns.choices[column] ?? "";
  const skip = () => {
    if (columns.mode === "new") columns.onChoices(columns.choices.map((choice, index) => index === column ? "" : choice));
    else columns.onChoices(columns.choices.map((choice, index) => index === column ? "" : choice));
  };
  const items: TypeAction[] = [{ key: "skip", text: t("importDialog:csvMerge.skipColumn"), checked: current === "",
    icon: skipMark, choose: skip }];
  if (columns.mode === "new") {
    for (const type of CSV_NEW_DATABASE_TYPES) items.push(type === "date"
      ? { key: type, text: localizedPropertyTypeLabel("date"), checked: false, submenu: "date", icon: typeMark(type) }
      : { key: type, text: typeLabel(type), checked: current === type, icon: typeMark(type), choose: () => {
        // Choosing another title demotes the old title to text, as before.
        columns.onChoices(columns.choices.map((choice, index) => index === column ? type
          : type === "title" && choice === "title" ? "rich_text" : choice));
      } });
  } else {
    for (const property of csvMergeMenuProperties(columns.target.properties)) {
      const value = `property:${property.id}` as const;
      // A property another column fills is not greyed out: taken here, that
      // column fills a new one instead (chooseCSVMergeProperty).
      items.push({ key: value, text: property.name, checked: current === value, icon: typeMark(property.type as PropertyType),
        tag: property.type === "title" ? t("importDialog:csvWindow.titleType") : undefined,
        choose: () => columns.onChoices(chooseCSVMergeProperty(columns.choices, column, value, columns.target.properties)) });
    }
    items.push({ key: "new", text: t("importDialog:csvMerge.newProperty"), checked: false, submenu: "new", icon: newPropertyMark });
  }
  // The types and a database's properties are searched from a field atop
  // them, as Notion's "속성 유형 검색…" (seen 2026-11-08, a database's
  // 2026-11-13: "기존 속성" and "Add new property" stay while the rest are
  // searched); the field keeps the menu's keys.
  const searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const folded = query.trim().toLocaleLowerCase();
  const shownItems = folded ? items.filter(item => item.key === "new" || item.text.toLocaleLowerCase().includes(folded)) : items;
  const picker = useNativeMenu({ items: shownItems, open: true, menuRef: ref, label: t("importDialog:csvMerge.propertyType"), onClose,
    keyOf: item => item.key, textOf: item => item.text,
    focusOnOpen: touchScreen(), focusOwnerRef: searchRef,
    onAction: item => { if (!item.submenu) { item.choose?.(); onClose(); } } });
  const dateMenu = useNativeSubmenu({ triggerKey: "date", rootTrigger: picker.rootTrigger, parentMenuRef: ref,
    submenuRef: dateRef, triggerRef: dateTrigger });
  const newMenu = useNativeSubmenu({ triggerKey: "new", rootTrigger: picker.rootTrigger, parentMenuRef: ref,
    submenuRef: newRef, triggerRef: newTrigger });
  const kind = dateMenu.state.isOpen ? "date" : newMenu.state.isOpen ? "new" : null;
  const submenu = kind === "date" ? dateMenu : newMenu;
  const placement = usePopoverPlacement(ref, (measured, viewport) => placeBesideField(anchor, measured, viewport),
    [anchor, shownItems.length], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const renderItem = (item: TypeAction) => <NativeMenuItem key={item.key} state={picker.state} itemKey={item.key}
    role={item.submenu ? "menuitem" : "menuitemradio"} aria-checked={item.submenu ? undefined : item.checked}
    buttonRef={item.submenu === "date" ? dateTrigger : item.submenu === "new" ? newTrigger : undefined}
    itemOptions={item.submenu === "date" ? dateMenu.submenuTriggerProps : item.submenu === "new" ? newMenu.submenuTriggerProps : undefined}
    className={pickerStyles.popoverItem} data-csv-type={item.key === "skip" ? "" : item.key}
    data-active={item.submenu && kind === item.submenu ? "true" : undefined}>
    {({ labelProps }) => <>{item.icon}<span className={item.tag ? pickerStyles.typeName : undefined}><span {...labelProps}>{item.text}</span>
      {item.tag ? <span className={pickerStyles.typeTag}>{item.tag}</span> : null}</span>
      {item.submenu ? <ChevronRight size="regular" aria-hidden="true" />
        : item.checked ? <CheckIcon size="small" aria-hidden="true" /> : null}</>}
  </NativeMenuItem>;
  const subItems: TypeAction[] = columns.mode === "new" && kind === "date"
    ? CSV_DATE_FORMATS.map(format => ({ key: `date:${format}`, text: t(`propertyCell:dateFormat.${format}`), checked: false,
      choose: () => { const value: CSVNewDatabaseChoice = `date:${format}`;
        columns.onChoices(columns.choices.map((choice, index) => index === column ? value : choice)); } }))
    : columns.mode === "merge" && kind === "new"
      ? CSV_MERGE_NEW_TYPES.map((type): TypeAction => type === "date"
        // "날짜" opens its display formats, as Notion's (seen 2026-10-05).
        ? { key: "new:date", text: localizedPropertyTypeLabel(type), checked: false, submenu: "date", icon: typeMark(type) }
        : { key: `new:${type}`, text: localizedPropertyTypeLabel(type), checked: current === `new:${type}`, icon: typeMark(type),
          choose: () => { const value: CSVMergeChoice = `new:${type}`;
            columns.onChoices(columns.choices.map((choice, index) => index === column ? value : choice)); } }) : [];
  // A new date's formats, under "새 속성 추가" › "날짜".
  const mergeDateItems: TypeAction[] = columns.mode === "merge"
    ? CSV_DATE_FORMATS.map(format => ({ key: `new:date:${format}`, text: t(`propertyCell:dateFormat.${format}`),
      checked: current === `new:date:${format}`,
      choose: () => { const value: CSVMergeChoice = `new:date:${format}`;
        columns.onChoices(columns.choices.map((choice, index) => index === column ? value : choice)); } }))
    : [];
  // Drawn over the window rather than inside its scroll, where its foot was cut (2026-11-08).
  return createPortal(<>
    <div {...picker.menuProps} ref={ref} className={`${pickerStyles.popover} ${pickerStyles.typeMenu}`}
      style={{ ...popoverPlacementStyle(placement), width: 240 }} data-csv-type-menu={column}>
      <label className={pickerStyles.search}>
        <input ref={searchRef} value={query} autoFocus={!touchScreen()} placeholder={t("importDialog:csvMerge.searchTypes")}
          aria-label={t("importDialog:csvMerge.searchTypes")} {...picker.ownerProps} onChange={(event) => setQuery(event.target.value)} />
        <SearchClearButton value={query} inputRef={searchRef} onClear={() => setQuery("")} />
      </label>
      {columns.mode === "merge" ? <div className={pickerStyles.popoverGroup}>{t("importDialog:csvMerge.existingProperties")}</div> : null}
      {shownItems.map(item => item.key === "new"
        ? [<div key="line" role="separator" className={pickerStyles.typeLine} />, renderItem(item)] : renderItem(item))}
    </div>
    {kind ? <TypeSubmenu key={kind} items={subItems} nestedItems={kind === "new" ? mergeDateItems : undefined}
      rootTrigger={picker.rootTrigger} parentRef={ref} column={column} kind={kind}
      menuRef={kind === "date" ? dateRef : newRef} menuOptions={submenu.submenuProps} focusOnOpen={submenu.state.focusStrategy !== null}
      label={kind === "date" ? localizedPropertyTypeLabel("date") : t("importDialog:csvMerge.newProperty")}
      onBack={submenu.state.close} onClose={onClose} /> : null}
  </>, document.body);
}

function TypeSubmenu({ items, nestedItems, rootTrigger, parentRef, column, kind, label, menuRef: ref, menuOptions, focusOnOpen, onBack, onClose }: {
  items: TypeAction[];
  /** The type menu's own trigger state, which every level of its submenus shares. */
  rootTrigger: ReturnType<typeof useNativeMenu>["rootTrigger"];
  /** The items an item's submenu opens, as a new date's formats. */
  nestedItems?: TypeAction[];
  parentRef: React.RefObject<HTMLDivElement | null>;
  menuRef: React.RefObject<HTMLDivElement | null>; menuOptions: NonNullable<Parameters<typeof useNativeMenu>[0]["menuOptions"]>;
  focusOnOpen: boolean;
  column: number; kind: "date" | "new"; label: string; onBack(): void; onClose(): void;
}) {
  const picker = useNativeMenu({ items, open: true, menuRef: ref, label, menuOptions, focusOnOpen,
    keyOf: item => item.key, textOf: item => item.text, onAction: item => { if (item.submenu) return; item.choose?.(); onClose(); } });
  const nestedTrigger = useRef<HTMLButtonElement>(null), nestedRef = useRef<HTMLDivElement>(null);
  const nestedKey = items.find(item => item.submenu)?.key ?? "";
  const nested = useNativeSubmenu({ triggerKey: nestedKey, rootTrigger, parentMenuRef: ref,
    submenuRef: nestedRef, triggerRef: nestedTrigger });
  const placement = usePopoverPlacement(ref, (measured, viewport) => placeBeside(popoverAnchorOf(parentRef.current!), measured, viewport, { gap: 4 }),
    [parentRef, items.length], undefined, { kind: "menu", inside: () => [parentRef.current], onEscape: onBack, onOutsidePress: onClose });
  return <>
    <div {...picker.menuProps} ref={ref} className={`${pickerStyles.popover} ${pickerStyles.typeMenu}`}
      style={{ ...popoverPlacementStyle(placement), width: 240 }} data-csv-date-format-menu={kind === "date" ? column : undefined} data-csv-new-type-menu={kind === "new" ? column : undefined}>
      {items.map(item => <NativeMenuItem key={item.key} state={picker.state} itemKey={item.key}
        role={item.submenu ? "menuitem" : "menuitemradio"} aria-checked={item.submenu ? undefined : item.checked}
        buttonRef={item.submenu ? nestedTrigger : undefined} itemOptions={item.submenu ? nested.submenuTriggerProps : undefined}
        data-active={item.submenu && nested.state.isOpen ? "true" : undefined}
        className={pickerStyles.popoverItem} data-csv-type={item.key}>
        {({ labelProps }) => <>{item.icon}<span><span {...labelProps}>{item.text}</span></span>
          {item.submenu ? <ChevronRight size="regular" aria-hidden="true" />
            : item.checked ? <CheckIcon size="small" aria-hidden="true" /> : null}</>}
      </NativeMenuItem>)}
    </div>
    {nested.state.isOpen && nestedItems?.length ? <TypeSubmenu items={nestedItems} rootTrigger={rootTrigger} parentRef={ref} column={column} kind="date"
      menuRef={nestedRef} menuOptions={nested.submenuProps} focusOnOpen={nested.state.focusStrategy !== null}
      label={items.find(item => item.submenu)?.text ?? ""} onBack={nested.state.close} onClose={onClose} /> : null}
  </>;
}
