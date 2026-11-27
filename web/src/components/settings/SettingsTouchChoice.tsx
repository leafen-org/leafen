import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import type { NotionSelectOption, NotionSelectSearch } from "@/components/database/NotionSelect";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "@/components/shell/PhoneSheet";
import { pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import selectStyles from "@/components/database/NotionSelect.module.css";
import { SettingsChevron } from "./SettingsChevron";
import styles from "./SettingsTouchChoice.module.css";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

type Group = { key: string; label?: string; options: NotionSelectOption[] };

/** The options in the runs of their headings, as the list shows them. */
function groupsOf(options: NotionSelectOption[]): Group[] {
  const groups: Group[] = [];
  for (const option of options) {
    const last = groups.at(-1);
    if (last && last.label === option.group) last.options.push(option);
    else groups.push({ key: `${groups.length}:${option.group ?? ""}`, label: option.group, options: [option] });
  }
  return groups;
}

/**
 * A settings row's choice on a touch screen, as Notion's on a phone and an
 * iPad (seen 2026-11-12 in iPhone Safari 402 and 2026-11-13 in iPad Safari
 * 834 and 1210): its button as on a desktop, and its choices on a sheet
 * rising from the screen's foot — on a wider screen in its middle, 642 wide
 * (PhoneSheet) — headed by the row's name with a blue "완료", the choices
 * in a white group, the chosen one ticked ("테마": 시스템 설정 사용 ✓ ·
 * 라이트 모드 · 다크 모드). A long list (a time zone's 419) finds by its
 * search field, which takes no focus, so no keyboard rises over the sheet,
 * and draws only the rows around what it shows (usePickerWindow).
 */
export function SettingsTouchChoice({ ariaLabel, value, options, disabled, placeholder, search, buttonClassName, describedBy, onChange }: {
  ariaLabel: string;
  value: string;
  options: NotionSelectOption[];
  disabled?: boolean;
  placeholder?: string;
  search?: NotionSelectSearch;
  buttonClassName?: string;
  describedBy?: string;
  onChange(value: string): void;
}) {
  const [open, setOpen] = useState(false);
  const selected = options.find((option) => option.value === value) ?? (placeholder === undefined ? options[0] : undefined);
  const shown = selected?.triggerLabel || selected?.label || "";
  const placeheld = !shown && placeholder !== undefined;
  const none = disabled || options.length === 0;
  return (
    <span className={selectStyles.notionSelect} data-notion-select="">
      <button type="button" className={`${selectStyles.notionSelectButton} ${buttonClassName ?? ""}`} data-notion-select-button=""
        aria-label={ariaLabel} aria-describedby={describedBy} aria-haspopup="dialog" aria-expanded={open} disabled={none}
        onClick={() => setOpen(true)}>
        {selected?.icon ? <span className={selectStyles.notionSelectIcon} aria-hidden="true">{selected.icon}</span> : null}
        <span className={selectStyles.notionSelectLabel} data-placeholder={placeheld ? "true" : undefined}>{placeheld ? placeholder : shown}</span>
        <SettingsChevron />
      </button>
      {open ? <SettingsChoiceSheet title={ariaLabel} value={value} options={options} search={search}
        onPick={onChange} onClose={() => setOpen(false)} /> : null}
    </span>
  );
}

/** The sheet a settings choice rises on (above): a settings filter's too (SettingsFilterChip). */
export function SettingsChoiceSheet({ title, value, options, search, onPick, onClose }: {
  title: string; value: string; options: NotionSelectOption[]; search?: NotionSelectSearch;
  onPick(value: string): void; onClose(): void;
}) {
  const { t } = useTranslation("common");
  const [query, setQuery] = useState("");
  const queryChanged = useRef(search?.onQueryChange);
  queryChanged.current = search?.onQueryChange;
  useLayoutEffect(() => {
    queryChanged.current?.(query);
  }, [query]);
  useLayoutEffect(() => () => queryChanged.current?.(""), []);
  const groups = useMemo(() => {
    const words = query.trim().toLocaleLowerCase();
    const shown = options.filter((option) => !(search?.hideDisabledOptions && option.disabled))
      .filter((option) => !words || (search?.match ? search.match(option, words) : option.label.toLocaleLowerCase().includes(words)));
    return groupsOf(shown);
  }, [options, query, search]);
  // The longest run draws only its rows around what the sheet shows, as a
  // schedule's time zones (PhoneAutomationSchedule): the sheet's body, which
  // holds the groups, scrolls them; a row with a line under its name is 66
  // high, a plain one 45, each group's last a pixel less (PhoneSheet).
  const stackRef = useRef<HTMLDivElement>(null), scrollRef = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => { scrollRef.current = stackRef.current?.parentElement ?? null; });
  // The chosen one shows as it opens, in the middle where it is far down a
  // long list, as a language's 40 (2026-11-12: a phone's list opened at its
  // top, the chosen "한국어" out of sight).
  useLayoutEffect(() => {
    const scroller = stackRef.current?.parentElement, chosen = stackRef.current?.querySelector<HTMLElement>('[data-chosen="true"]');
    if (!scroller || !chosen) return;
    const top = chosen.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
    if (top + chosen.offsetHeight > scroller.clientHeight) scroller.scrollTop += top - (scroller.clientHeight - chosen.offsetHeight) / 2;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as it opens
  }, []);
  const longest = groups.reduce((at, group, index) => group.options.length > (groups[at]?.options.length ?? 0) ? index : at, 0);
  const long = groups[longest]?.options ?? [];
  const chosenAt = long.findIndex((option) => option.value === value);
  const lines = usePickerWindow({ scrollRef, open: true, count: long.length, keyAt: (index) => long[index].value,
    sizeAt: (index) => (long[index].description ? 66 : 45) - (index === long.length - 1 ? 1 : 0), focused: chosenAt });
  const row = (option: NotionSelectOption) => (
    <PhoneSheetRow key={option.value} label={option.label} description={option.description || undefined} icon={option.icon} more={false}
      disabled={option.disabled} value={option.value === value ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
      data={{ "data-settings-choice": option.value, "data-chosen": option.value === value ? "true" : undefined }}
      onPress={() => { if (option.value !== value) onPick(option.value); onClose(); }} />
  );
  return (
    <PhoneSheet title={title} tone="settings" onClose={onClose} action={{ label: t("actions.done"), onPress: onClose }}
      data={{ "data-settings-choice-sheet": "" }}>
      <div ref={stackRef} className={sheetStyles.stack}>
        {search ? (
          <PhoneSheetGroup>
            <PhoneSheetSearch value={query} placeholder={search.placeholder} aria-label={search.ariaLabel} onValueChange={setQuery} />
          </PhoneSheetGroup>
        ) : null}
        {groups.map((group, index) => (
          <PhoneSheetGroup key={group.key} label={group.label}>
            {index === longest && lines.windowed ? (
              <div ref={lines.linesRef} role="presentation" style={pickerWindowBox(lines.height)}>
                {lines.lines.map((line) => (
                  <div key={line.key} {...pickerWindowLine(line, lines.measure)} data-window-line-last={line.index === long.length - 1 ? "" : undefined}>
                    {row(long[line.index])}
                  </div>
                ))}
              </div>
            ) : group.options.map(row)}
          </PhoneSheetGroup>
        ))}
        {groups.length === 0 && search?.noResultsLabel ? <p className={styles.none}>{search.noResultsLabel}</p> : null}
      </div>
    </PhoneSheet>
  );
}
