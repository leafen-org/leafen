import { Fragment, useRef } from "react";
import { CheckIcon } from "@/icons/leafen";
import { useNativeList } from "@/lib/nativeList";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import type { DateEditorChoice } from "./PropertyDateEditorOptions";
import type { TimeZoneGroup, TimeZoneRow } from "./dateTimeZones";
import styles from "./PropertyDateEditor.module.css";

export function DateEditorChoices({ choices, selected, label, open, lightFirst = false, onChoose }: {
  choices: readonly DateEditorChoice[]; selected: string; label: string; open: boolean;
  /**
   * Opens at its top with its first row lit, the pointer's ground on it, and
   * the keys going on from there, the chosen row only ticked, as Notion's
   * schedule time list (seen 2026-11-19 in Chrome at 1440, two tabs, no
   * script before the press: reopened with 오후 1:00, then 오후 7:30 chosen,
   * it stood at its top with 자정 전후 lit, and ↓ lit 오전 3:15 there).
   * Otherwise the keys start at the chosen row.
   */
  lightFirst?: boolean;
  onChoose(value: string): void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const list = useNativeList({ items: choices, listRef: ref, label, open, firstKey: lightFirst ? undefined : selected,
    keyOf: choice => choice.value, textOf: choice => choice.label, onAction: choice => onChoose(choice.value) });
  return <div {...list.listBoxProps} ref={ref} className={styles.choiceList} data-lights-row={lightFirst || undefined}>
    {choices.map(choice => <Fragment key={choice.value}>
      <NativePickerOption itemKey={choice.value} state={list.state}
        className={choice.description ? `${styles.choiceOption} ${styles.choiceTwoLine}` : styles.choiceOption} aria-selected={choice.value === selected}>
        {({ labelProps, descriptionProps }) => <>
          {choice.description
            ? <span><span {...labelProps}>{choice.label}</span><small {...descriptionProps}>{choice.description}</small></span>
            : <span {...labelProps}>{choice.label}</span>}
          {choice.value === selected && <CheckIcon size="small" />}
        </>}
      </NativePickerOption>
      {choice.ruleAfter ? <div className={styles.choiceRule} role="separator" /> : null}
    </Fragment>)}
  </div>;
}

export function DateZoneChoices({ groups, zone, offset, query, onQuery, searchLabel, label, groupLabel, noneLabel, open, onChoose }: {
  groups: TimeZoneGroup[]; zone: string; offset: string; query: string; onQuery(value: string): void;
  searchLabel: string; label: string; groupLabel(key: TimeZoneGroup["key"]): string; noneLabel: string;
  open: boolean; onChoose(zone: string): void;
}) {
  const ref = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null);
  const entries = groups.flatMap(group => group.rows.map(row => ({ ...row, key: `${group.key}:${row.zone}` })));
  // No zone is lit as the list opens or as the search is typed; the arrow
  // keys light one, from the first, with the keyboard's ring (`keyed`),
  // until the search changes or the pointer moves into another zone and
  // takes the highlight, as Notion's (seen 2026-11-14).
  const list = useNativeAutocomplete({ items: entries, query, onQuery, keyOf: row => row.key,
    highlightFirst: false,
    textOf: row => `${row.city} ${row.offset}`, onPick: row => onChoose(row.zone), label: searchLabel,
    listRef: ref, inputRef, open, commitTab: false });
  // Every zone's group (some 420) draws only the zones around what the list
  // shows, the highlighted one always (usePickerWindow): drawn whole, its
  // opening press drew and laid out every row before its paint (2026-11-14:
  // 128–208 ms, CPU ×2). A zone's row is 45 high, 1 apart (.zoneList .choiceOption).
  const all = groups.find(group => group.key === "all")?.rows ?? [];
  const lit = list.activeKey === null ? "" : String(list.activeKey);
  const lines = usePickerWindow({ scrollRef: ref, open: true, count: all.length, keyAt: index => `all:${all[index].zone}`, sizeAt: () => 45, gap: 1,
    focused: lit.startsWith("all:") ? all.findIndex(row => `all:${row.zone}` === lit) : -1 });
  // The chosen zone has a check at its row's end, by its city, as Notion's
  // (seen 2026-11-14: 16px, 3px under the row's top, 8px after the text).
  const drawRow = (groupKey: TimeZoneGroup["key"], row: TimeZoneRow, place?: { position: number; of: number }) => {
    const chosen = !offset && row.zone === zone;
    return <NativePickerOption key={`${groupKey}:${row.zone}`} itemKey={`${groupKey}:${row.zone}`} state={list.state}
      className={styles.choiceOption} aria-selected={chosen} data-time-zone-choice={row.zone}
      data-focus-visible={list.keyed && lit === `${groupKey}:${row.zone}` ? "true" : undefined}
      aria-posinset={place?.position} aria-setsize={place?.of}>
      {({ labelProps, descriptionProps }) => <>
        <span className={styles.zoneText}><span {...labelProps}>{row.city}</span><span {...descriptionProps} className={styles.zoneOffset}>{row.offset}</span></span>
        {chosen ? <span className={styles.zoneCheck}><CheckIcon size="small" aria-hidden="true" /></span> : null}
      </>}
    </NativePickerOption>;
  };
  return <>
    <div className={styles.zoneHead}>
      <input {...list.inputProps} ref={inputRef} className={styles.zoneSearch} placeholder={searchLabel} />
    </div>
    <div {...list.listBoxProps} ref={ref} aria-label={label} className={styles.zoneList}>
      {groups.map(group => <div key={group.key} role="group" aria-label={groupLabel(group.key)}>
        <div className={styles.zoneGroupLabel} aria-hidden="true">{groupLabel(group.key)}</div>
        {group.key === "all" && lines.windowed
          ? <div ref={lines.linesRef} role="presentation" style={pickerWindowBox(lines.height)}>
            {lines.lines.map(line => <div key={line.key} {...pickerWindowLine(line, lines.measure)}>
              {drawRow(group.key, all[line.index], { position: line.index + 1, of: all.length })}
            </div>)}
          </div>
          : group.rows.map(row => drawRow(group.key, row))}
      </div>)}
      {/* A search nothing answers keeps the zones' head over its words, as
          Notion's (seen 2026-11-14: "시간대 선택" over "결과 없음"). */}
      {groups.length === 0 && <div role="group" aria-label={groupLabel("all")}>
        <div className={styles.zoneGroupLabel} aria-hidden="true">{groupLabel("all")}</div>
        <div className={styles.zoneEmpty}><span>{noneLabel}</span></div>
      </div>}
    </div>
  </>;
}
