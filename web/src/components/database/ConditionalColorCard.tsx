import { SwitchInput } from "@/components/Switch";
import { useEffect, useId, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import type { GraphView } from "@/contentGraph";
import { i18next } from "@/i18n";
import { DragHandleIcon, SidebarChevronDown, Trash } from "@/icons/leafen";
import type { DragReorder } from "@/lib/useDragReorder";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { DbProperty, FilterOperator, ViewFilter } from "@/lib/types";
import { chipStyle, COLOR_NAMES } from "./colors";
import { conditionalColorHasOptionColors, type ConditionalColor, type ConditionalColorSetting } from "./conditionalColorModel";
import { NO_VALUE_FILTERS } from "./databaseFilterLabels";
import { effectiveFilterOperator, filterOperatorLabel, filterOperatorsFor, filterPropertyForComparison } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { filterValueText } from "./databaseFilterValueText";
import { databaseViewLabels } from "./databaseViewLabels";
import { filterOperatorChange } from "./DatabaseFilterEditors";
import { QuickFilterValue } from "./DatabaseQuickFilterEditor";
import { isDateFilterProperty } from "./DateFilterEditor";
import { NotionSelect } from "./NotionSelect";
import { PropertySettingButton, PropertySettingCaret } from "./PropertySettingRow";
import styles from "./ConditionalColorCard.module.css";
import valueStyles from "./DatabaseQuickFilterBar.module.css";
import { fieldEscape } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/** Views whose pages are cards: their color is the card's background, as Notion's "카드 배경". */
const CARD_VIEWS = new Set(["board", "gallery"]);
/** A value picked from a list opens it from a box under the head; one typed is typed there. */
const LISTED_VALUES = new Set(["select", "multi_select", "status", "checkbox"]);
// A card's condition and its listed value draw Notion's filled ⌄ (arrowChevronSingleDownFillSmall) in the tertiary icon
// colour, the condition's at 12 and the value's at 16, 12 from the box's end (seen 2026-11-22 at 1440 dark, dpr 2).
const OPERATOR_CARET = <SidebarChevronDown size="micro" aria-hidden="true" />;
// Its setting rows' "›" is that filled ⌄ at 16 turned to point right, pointing down while its row is open
// (Notion's, ink 5.84 by 10.14, seen 2026-11-22 at 1440 dark, dpr 1 and 2; .settingRow > svg).
const ROW_CARET = <SidebarChevronDown size="small" aria-hidden="true" />;

/** A color's name, as Notion's options name theirs ("초록색"); "auto" is "일치 옵션". */
export function conditionalColorName(color: ConditionalColor) {
  return color === "auto" ? databaseViewLabels().conditionalColorAuto : i18next.t(`propertyTypeConfig:optionColors.${color}`);
}

/** A swatch of a color as the page takes it: "default" is none; a phone sheet's row has a 22px one. */
export function Swatch({ color, className, phone }: { color: Exclude<ConditionalColor, "auto">; className?: string; phone?: boolean }) {
  return <span className={`${styles.swatch} ${className ?? ""}`} data-color={color} data-phone={phone ? "true" : undefined} aria-hidden="true" />;
}

/** A popover under its button — at the button's end, or its start — its field or itself focused, closed by Escape and a press outside it. */
function CardPopover({ open, anchor, label, align = "end", className, valuePanel = false, onClose, children }: {
  open: boolean; anchor: RefObject<HTMLElement | null>; label: string; align?: "start" | "end"; className?: string;
  /** It holds a filter's value, laid out as in a pill's editor with no head over it (`data-filter-value-panel`). */
  valuePanel?: boolean;
  onClose(restore?: boolean): void; children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null), id = useId();
  const place = usePopoverPlacement(ref, open ? (size, viewport) => placeAround(popoverRectOf(anchor.current)!, size, viewport,
    { gap: align === "end" ? 1 : 4, align, alignFallback: true }) : null, [open], undefined,
  { kind: "picker", restoreFocus: false, onEscape: () => onClose(true), onOutsidePress: () => onClose(), inside: () => [anchor.current] });
  // What it holds may take the keys as it opens — a checkbox's two states are a menu that lights its first row and
  // takes them, as Notion's (seen 2026-11-22 at 1440 dark, dpr 1, in two tabs with no script in the page before the
  // press: "선택..." pressed open lit "체크 표시되지 않음", ↓ then lit "체크 표시됨", the menu focused); only when
  // nothing in it did is its field, or itself, given them.
  useEffect(() => {
    if (!open || ref.current?.contains(document.activeElement)) return;
    (ref.current?.querySelector<HTMLInputElement>("input:not(:disabled):not([role=switch])") ?? ref.current)?.focus({ preventScroll: true });
  }, [open]);
  if (!open || typeof document === "undefined") return null;
  return createPortal(<div ref={ref} id={id} role="dialog" tabIndex={-1} aria-label={label} className={`${styles.popover} ${className ?? ""}`}
    data-filter-value-panel={valuePanel ? "" : undefined}
    style={{ position: "fixed", ...popoverPlacementStyle(place) }} onKeyDown={(event) => {
      if (fieldEscape(event)) { event.preventDefault(); event.stopPropagation(); onClose(true); }
    }}>{children}</div>, document.body);
}

/**
 * The page background a setting gives, as Notion's (seen 2026-10-24): "일치
 * 옵션" and a switch for a property with option colors, then ten 30px
 * swatches in two rows — 기본 (none) to 빨간색 — faint while the switch is
 * on; choosing either closes it.
 */
function ColorChoice({ setting, property, label, onPick }: {
  setting: ConditionalColorSetting; property: DbProperty | undefined; label: string; onPick(color: ConditionalColor): void;
}) {
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const openedFromKeys = useRef(false);
  // Escape gives its row the keys back only when keys opened it; pressed open, it leaves them nowhere, the
  // settings still open, as Notion's (measured in Chrome with no script in the page before the action, a
  // letter typed after it: 2026-11-15 16:4x and 21:1x in separate tabs, the colors pressed open — no ring,
  // the letter went nowhere, the focus on the body; once at 01:1x the toolbar's 설정 button had it, which
  // Notion gives back when it held the keys as the colors opened). A pick still gives the row the keys.
  const close = (restore = false) => { setOpen(false); if (restore && openedFromKeys.current) buttonRef.current?.focus({ preventScroll: true }); };
  const pick = (color: ConditionalColor) => { onPick(color); setOpen(false); buttonRef.current?.focus({ preventScroll: true }); };
  const auto = setting.color === "auto";
  return <>
    <PropertySettingButton buttonRef={buttonRef} className={styles.settingRow} label={label} value={conditionalColorName(setting.color)}
      valueIcon={auto ? undefined : <Swatch color={setting.color as Exclude<ConditionalColor, "auto">} className={styles.rowSwatch} />}
      aria-haspopup="dialog" aria-expanded={open} data-conditional-color-color={setting.color}
      onClick={(event) => { openedFromKeys.current = event.detail === 0; setOpen(!open); }} />
    <CardPopover open={open} anchor={buttonRef} label={label} className={styles.colors} onClose={close}>
      {conditionalColorHasOptionColors(property) && (
        <label className={styles.colorAuto}>
          <span>{databaseViewLabels().conditionalColorAuto}</span>
          <SwitchInput checked={auto} data-conditional-color-auto
            onChange={(event) => pick(event.currentTarget.checked ? "auto" : "default")} />
        </label>
      )}
      <div className={styles.swatches} data-faint={auto ? "true" : undefined}>
        {COLOR_NAMES.map((color) => (
          <button key={color} type="button" className={styles.swatchButton} aria-label={conditionalColorName(color)} title={conditionalColorName(color)}
            aria-pressed={setting.color === color} data-conditional-color-swatch={color} onClick={() => pick(color)}>
            <Swatch color={color} />
          </button>
        ))}
      </div>
    </CardPopover>
  </>;
}

/** A value picked from a list, in a box under the head ("선택..." until one is), the list under it. */
function ListedValue({ filter, property, properties, snapshot, onChange }: {
  filter: ViewFilter; property: DbProperty; properties: DbProperty[]; snapshot: DatabaseFilterSnapshot; onChange(patch: Partial<ViewFilter>): void;
}) {
  const labels = databaseViewLabels();
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), anchorRef = useRef<HTMLDivElement>(null);
  const close = (restore = false) => { setOpen(false); if (restore) buttonRef.current?.focus({ preventScroll: true }); };
  const values = Array.isArray(filter.value) ? filter.value : filter.value === undefined || filter.value === "" ? [] : [filter.value];
  const options = Array.isArray(property.config?.options) ? property.config.options as Array<{ id?: string; name?: string; color?: string }> : [];
  const chips = LISTED_VALUES.has(property.type) && property.type !== "checkbox"
    ? values.map((value) => options.find((option) => option.id === value || option.name === value)).filter((option) => option !== undefined) : [];
  const text = chips.length ? "" : values.length ? filterValueText(filter.value, property) : "";
  // Its list opens from the box's 240 wide place (the box 4 in, 4 over the place's foot): at the place's start,
  // 4 under it, so 4 before the box and 8 under it, as Notion's (seen 2026-11-22 at 1440 dark, dpr 1).
  return <div className={styles.value}><div ref={anchorRef} className={styles.valuePlace}>
    <button ref={buttonRef} type="button" className={styles.valueBox} aria-haspopup="dialog" aria-expanded={open}
      aria-label={labels.filterValueFor(property.name)} data-conditional-color-value onClick={() => setOpen(!open)}>
      <span className={styles.valueChips}>
        {chips.map((option) => <span key={option.id ?? option.name} className={styles.valueChip} style={chipStyle(option.color)}>{option.name}</span>)}
        {text ? <span>{text}</span> : null}
        {!chips.length && !text ? <span className={styles.valuePlaceholder}>{labels.conditionalColorChoose}</span> : null}
      </span>
      <SidebarChevronDown size="small" aria-hidden="true" />
    </button></div>
    <CardPopover open={open} anchor={anchorRef} label={labels.filterValueFor(property.name)} align="start"
      className={`${styles.valueList} ${valueStyles.valuePanel}`} valuePanel onClose={close}>
      <QuickFilterValue filter={filter} property={property} properties={properties} snapshot={snapshot} onChange={onChange} onClose={() => close(true)} />
    </CardPopover>
  </div>;
}

/**
 * A conditional color setting as Notion's card (seen 2026-10-24 at 1280px):
 * a grey 10px card — its head the property in faint 12px, its condition as
 * a pill ("비어 있지 않음 ⌄", the operators a number's as signs) and a
 * trash; the value under it when the condition takes one; then "페이지 배경"
 * ("카드 배경" on cards) with the color, and in a table "적용 대상:" with
 * "전체 행" or "이 속성". A drag handle at its left moves it: the first
 * setting a page matches colors it. Conditions Notion's card can't show —
 * several, or on another property — show in `conditions` instead.
 */
export function ConditionalColorCard({ view, setting, index, dbProperties, filterSnapshot, reorder, handle, conditions, onChange, onDelete }: {
  view: GraphView;
  setting: ConditionalColorSetting;
  index: number;
  dbProperties: DbProperty[];
  filterSnapshot: DatabaseFilterSnapshot;
  reorder: DragReorder;
  handle: { above?: string; below?: string };
  conditions?: ReactNode;
  onChange(patch: (setting: ConditionalColorSetting) => ConditionalColorSetting): void;
  onDelete(): void;
}) {
  const labels = databaseViewLabels();
  const source = dbProperties.find(({ id }) => id === setting.propertyId);
  const filter = conditions ? undefined : setting.filterGroup.filters[0];
  const property = source ? filterPropertyForComparison(source, dbProperties) : undefined;
  const operator = property && filter ? effectiveFilterOperator(property, filter.operator) : undefined;
  const setFilter = (patch: Partial<ViewFilter>) => onChange((current) => ({
    ...current, filterGroup: { ...current.filterGroup, filters: current.filterGroup.filters.map((item, at) => at === 0 ? { ...item, ...patch } as ViewFilter : item) },
  }));
  const listed = property && (LISTED_VALUES.has(property.type) || isDateFilterProperty(property));
  // A condition that takes a typed value puts the caret in its field, as
  // Notion's (measured 2026-11-15 in Chrome at 1440 with no script in the
  // page: "비어 있지 않음" changed to "값을 포함하는 데이터", a typed "z" went
  // into the value field).
  const valueRef = useRef<HTMLDivElement>(null);
  const setOperator = (next: FilterOperator) => {
    if (!filter || !property) return;
    setFilter(filterOperatorChange(filter, property, next));
    if (!listed && !NO_VALUE_FILTERS.has(next)) focusOpenedField(() => valueRef.current?.querySelector<HTMLInputElement>("input"));
  };
  return (
    <div className={styles.card} role="group" aria-label={labels.conditionalColorSetting(index + 1)} data-conditional-color={setting.id}
      {...reorder.row(setting.id)}>
      <button type="button" className={styles.handle} aria-label={labels.conditionalColorMove(index + 1)}
        {...reorder.handle(setting.id, true, handle)}>
        <DragHandleIcon size="small" aria-hidden="true" />
      </button>
      <div className={styles.head}>
        <span className={styles.property}>{source?.name ?? labels.untitled}</span>
        {property && filter && operator ? (
          <span className={styles.contents} data-conditional-color-condition={operator}>
            {/* Its conditions as a filter's: a list 190 wide right under it, the chosen one lit and unticked (Notion's,
                seen 2026-11-22 at 1440 dark, dpr 1). */}
            <NotionSelect ariaLabel={labels.filterCondition} className={styles.operator} buttonClassName={styles.operatorButton}
              caret={OPERATOR_CARET} value={operator} plain menuMinWidth={190}
              options={filterOperatorsFor(property).map((candidate) => ({ value: candidate, label: filterOperatorLabel(candidate, property) }))}
              onChange={(next) => setOperator(next as FilterOperator)} />
          </span>
        ) : null}
        <button type="button" className={styles.delete} aria-label={labels.conditionalColorDeleteRule} data-conditional-color-delete onClick={onDelete}>
          <Trash size="small" aria-hidden="true" />
        </button>
      </div>
      {conditions ? <div className={styles.conditions}>{conditions}</div> : null}
      {property && filter && operator && !NO_VALUE_FILTERS.has(operator) ? (
        listed
          ? <ListedValue filter={filter} property={property} properties={dbProperties} snapshot={filterSnapshot} onChange={setFilter} />
          : <div ref={valueRef} className={styles.value}><div className={styles.valueField}>
            <QuickFilterValue filter={filter} property={property} properties={dbProperties} snapshot={filterSnapshot} onChange={setFilter} onClose={() => undefined} />
          </div></div>
      ) : null}
      <div className={styles.rows}><PropertySettingCaret.Provider value={ROW_CARET}>
        <ColorChoice setting={setting} property={source}
          label={CARD_VIEWS.has(view.type) ? labels.conditionalColorCardBackground : labels.conditionalColorBackground}
          onPick={(color) => onChange((current) => ({ ...current, color }))} />
        {view.type === "table" && (
          <span className={styles.contents} data-conditional-color-apply={setting.applyTo ?? "row"}>
            <NotionSelect ariaLabel={labels.conditionalColorApplyTo} settingLabel={labels.conditionalColorApplyTo} settingPlace="below"
              settingMenuWidth={120} buttonClassName={styles.settingRow} value={setting.applyTo ?? "row"}
              options={[{ value: "row", label: labels.conditionalColorApplyRow }, { value: "property", label: labels.conditionalColorApplyProperty }]}
              onChange={(value) => onChange((current) => value === "property" ? { ...current, applyTo: "property" } : { ...current, applyTo: undefined })} />
          </span>
        )}
      </PropertySettingCaret.Provider></div>
    </div>
  );
}
