import { Fragment, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import type { DbProperty } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { PhoneSheetChoices } from "./PhoneSheetChoices";
import { PhoneSheetNoResults } from "../shell/PhoneSheetNoResults";
import { chipStyle } from "./colors";
import { PropertyIcon } from "./PropertyIcon";
import { databaseViewLabels } from "./databaseViewLabels";
import { statusGroupColumns, statusGroupForOption, statusGroupLabelKey, statusOptionsByOption, statusOptionsInGroupOrder } from "./propertyStatusPolicy";
import { statusFilterFolded } from "./statusRowOptions";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import styles from "./DatabasePhonePickers.module.css";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

type PickerProperty = Pick<DbProperty, "id" | "name" | "type" | "config">;

/**
 * A phone's property pick on a screen of its own, as Notion's "속성" from a
 * sort's or a filter's property (seen 2026-10-21 at 375px): "취소" at the
 * head's right, the properties in flat rows of icon and name.
 */
export function PhonePropertyScreen({ properties, onPick, onClose }: {
  properties: readonly PickerProperty[];
  onPick(property: PickerProperty): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="screen" title={labels.sheetProperty} onClose={onClose} action={{ label: labels.sheetCancel, onPress: onClose }}
      data={{ "data-phone-property-screen": "" }}>
      <PhoneSheetGroup>
        {properties.map((property) => (
          <PhoneSheetRow key={property.id} icon={<PropertyIcon property={property} size="regular" />} label={property.name} more={false}
            onPress={() => onPick(property)} data={{ "data-picker-property": property.id }} />
        ))}
      </PhoneSheetGroup>
    </PhoneSheet>
  );
}

/**
 * The properties rising from the bottom with "속성 검색" over them, as
 * Notion's "정렬 추가" (seen 2026-10-21).
 */
export function PhonePropertyMenu({ label, properties, onPick, onClose }: {
  label: string;
  properties: readonly PickerProperty[];
  onPick(property: PickerProperty): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const [search, setSearch] = useState("");
  const query = search.trim().toLocaleLowerCase();
  return (
    <PhoneSheetMenu label={label} marks="properties" onClose={onClose}>
      <PhoneSheetSearch value={search} placeholder={labels.sheetSearchProperty} aria-label={labels.sheetSearchProperty} onValueChange={setSearch} />
      {properties.filter((property) => !query || property.name.toLocaleLowerCase().includes(query)).map((property) => (
        <PhoneSheetRow key={property.id} icon={<PropertyIcon property={property} size="regular" />} label={property.name} more={false}
          onPress={() => onPick(property)} data={{ "data-picker-property": property.id }} />
      ))}
    </PhoneSheetMenu>
  );
}

/**
 * A short choice in a half sheet with "완료", as Notion's sort direction,
 * filter condition and "연산자" (seen 2026-10-21): no mark on the current
 * one; picking one closes it.
 */
export function PhoneChoiceSheet<Key extends string>({ title, choices, onPick, onClose, data }: {
  title: ReactNode;
  choices: ReadonlyArray<{ key: Key; label: string; description?: string }>;
  onPick(key: Key): void;
  onClose(): void;
  data?: Record<`data-${string}`, string | undefined>;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="half" title={title} label={typeof title === "string" && title ? title : labels.filterCondition} onClose={onClose}
      action={{ label: labels.sheetDone, onPress: onClose }} data={data}>
      <PhoneSheetGroup>
        {choices.map((choice) => (
          <PhoneSheetRow key={choice.key} label={choice.label} description={choice.description} more={false}
            onPress={() => { onPick(choice.key); onClose(); }} data={{ "data-phone-choice": choice.key }} />
        ))}
      </PhoneSheetGroup>
    </PhoneSheet>
  );
}

/**
 * A setting's choices, as Notion's grouping "정렬" and a status option's
 * "그룹화" on a phone: the whole-sheet choice over the sheet it was opened
 * from (PhoneSheetChoices; seen 2026-11-18 on a Galaxy A90 at 411 in two tabs:
 * no head, its grip, the choices straight under it, the chosen one ticked);
 * picking one closes it.
 */
export function PhoneMenuChoice({ label, value, options, onPick, onClose }: {
  label: string;
  value: string;
  options: ReadonlyArray<{ value: string; label: string }>;
  onPick(value: string): void;
  onClose(): void;
}) {
  return <PhoneSheetChoices label={label} value={value} choices={options.map((option) => ({ key: option.value, label: option.label }))}
    onPick={onPick} onClose={onClose} data={{ "data-phone-menu-choice": "" }} />;
}

type Option = { id?: string; name?: string; color?: string };

/** The options a select, multi-select or status filter has chosen. */
function chosenFilterOptions(value: unknown): Set<string> {
  if (Array.isArray(value)) return new Set(value.filter((item): item is string => typeof item === "string" && item !== ""));
  return typeof value === "string" && value ? new Set([value]) : new Set();
}

/** A select's, multi-select's or status's options a filter has chosen. */
export function chosenFilterOptionList(property: Pick<DbProperty, "type" | "config">, value: unknown): Option[] {
  const chosen = chosenFilterOptions(value);
  const options: Option[] = property.type === "status" ? statusOptionsInGroupOrder(property.config)
    : Array.isArray(property.config?.options) ? property.config.options as Option[] : [];
  return [...chosen].map((id) => options.find((option) => option.id === id || option.name === id)).filter((option): option is Option => Boolean(option));
}

/** An option's coloured pill in a phone's sheet: 24px high, 16px words, 8px in, 4px round (Notion's, seen 2026-10-21 and 10-24). */
export function PhoneOptionPill({ option }: { option: Option }) {
  return <span className={styles.pill} style={chipStyle(option.color)}>{option.name}</span>;
}

/**
 * A select's, multi-select's or status's filter values in a phone's sheet,
 * as Notion's (seen 2026-10-21): a group whose field ("하나 이상의 옵션을
 * 선택하세요.") holds the chosen ones and finds the others, then the options
 * with a small box to tick and their coloured pill; a status's groups each
 * ticked on their own above their options.
 *
 * In the field each chosen one's pill has its "×" taking it out, the field
 * then holding the focus, and at the field's end "ⓧ" shows once one is
 * chosen or a word typed; its press is `onClear`, which empties the value
 * and closes the sheet — a quick filter's goes, leaving "필터 없음", and a
 * rule's screen goes back to its rule (Notion's phone, seen 2026-11-17 in
 * Android Chrome on a Galaxy A90 at 411: a select's field, a word typed or an
 * option chosen, in a quick filter's sheet and an advanced rule's screen).
 */
export function PhoneFilterOptions({ property, value, onChange, onClear }: {
  property: DbProperty;
  value: unknown;
  onChange(value: string[] | undefined): void;
  onClear(): void;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation(["propertyCell", "databaseView"]);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const chosen = chosenFilterOptions(value);
  const status = property.type === "status";
  // Each group stands on its own row over its options, none for a group itself (statusOptionsByOption), and
  // those folded as it opened under "기타 상태 옵션 N개" until it is pressed, as the desktop list (statusFilterFolded).
  const statusOptions = status ? statusOptionsByOption(property.config) : [];
  const [folded, setFolded] = useState(() => statusFilterFolded(status ? property.config : undefined, chosen));
  const options: Option[] = status ? statusOptions : Array.isArray(property.config?.options) ? property.config.options as Option[] : [];
  const has = (id: string, name?: string) => chosen.has(id) || (name ? chosen.has(name) : false);
  const toggle = (id: string, name?: string) => {
    const next = new Set(chosen);
    if (has(id, name)) { next.delete(id); if (name) next.delete(name); } else next.add(id);
    onChange(next.size ? [...next] : undefined);
  };
  const matches = (name = "") => name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
  const row = (id: string, label: ReactNode, name: string | undefined, nested = false) => (
    <button key={id} type="button" role="option" aria-selected={has(id, name)} className={`${sheetStyles.row} ${styles.option}`}
      data-nested={nested ? "true" : undefined} data-filter-option={id} onClick={() => toggle(id, name)}>
      <span className={styles.box} data-checked={has(id, name) ? "true" : undefined} aria-hidden="true">
      </span>
      {label}
    </button>
  );
  const pill = (option: Option) => <PhoneOptionPill option={option} />;
  const named = chosenFilterOptionList(property, value);
  // Words no option has leave "결과 없음" where the list was (Notion's, seen 2026-11-17 on iPhone Safari at 402 and Android Chrome at 411).
  const shown = options.filter((option) => matches(option.name));
  return (
    <>
      <PhoneSheetGroup>
        <div className={styles.field}>
          <label className={styles.picks}>
            {named.map((option) => (
              <span key={option.id ?? option.name} className={`${styles.pill} ${styles.fieldPill}`} style={chipStyle(option.color)}>
                <span className={styles.pillName}>{option.name}</span>
                <button type="button" aria-label={labels.sheetRemoveChoice(option.name ?? "")} data-filter-option-remove=""
                  onClick={(event) => {
                    event.preventDefault();
                    toggle(option.id ?? option.name ?? "", option.name);
                    inputRef.current?.focus({ preventScroll: true });
                  }}>
                  <X size="small" aria-hidden="true" />
                </button>
              </span>
            ))}
            <input ref={inputRef} value={query} placeholder={named.length ? "" : labels.filterOptionsPlaceholder}
              aria-label={labels.filterValueFor(property.name)} onChange={(event) => setQuery(event.target.value)} />
          </label>
          <SearchClearButton value={chosen.size > 0 || query !== ""} small phone label={labels.sheetClearValue} onClear={onClear} />
        </div>
      </PhoneSheetGroup>
      {!status && query.trim() && shown.length === 0 ? <PhoneSheetNoResults>{labels.noSearchResults}</PhoneSheetNoResults> : <PhoneSheetGroup>
        <div role="listbox" aria-multiselectable="true" aria-label={labels.filterValueFor(property.name)}>
          {status ? statusGroupColumns(property.config).map((group) => {
            const key = statusGroupLabelKey(group.name);
            const inGroup = statusOptions.filter((option) => statusGroupForOption(property.config, option)?.id === group.id && matches(option.name) && !folded.has(option.id));
            return (
              <Fragment key={group.id}>
                {row(group.id, <span className={styles.groupName}>{key ? t(key) : group.name}</span>, group.name)}
                {inGroup.map((option) => row(option.id, pill(option), option.name, true))}
              </Fragment>
            );
          }) : shown.map((option) => row(option.id ?? option.name ?? "", pill(option), option.name))}
        </div>
        {folded.size ? (
          <button type="button" className={`${sheetStyles.row} ${styles.option}`} data-filter-folded="" onClick={() => setFolded(new Set())}>
            <PropertyTypeIcon type="status" size="regular" />{t("databaseView:otherStatusOptions", { count: folded.size })}
          </button>
        ) : null}
      </PhoneSheetGroup>}
    </>
  );
}

/** A filter's options on a screen of their own, as Notion's from an advanced rule's "옵션 선택" (seen 2026-10-21). */
export function PhoneFilterOptionsScreen({ property, value, onChange, onClose }: {
  property: DbProperty;
  value: unknown;
  onChange(value: string[] | undefined): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="screen" title={property.name} onClose={onClose} action={{ label: labels.sheetDone, onPress: onClose }}
      data={{ "data-phone-filter-options": "" }}>
      <PhoneFilterOptions property={property} value={value} onChange={onChange} onClear={() => { onChange(undefined); onClose(); }} />
    </PhoneSheet>
  );
}
