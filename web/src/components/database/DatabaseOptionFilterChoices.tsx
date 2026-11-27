import { Fragment, useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown, XMarkIcon } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import type { DbProperty } from "@/lib/types";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { chipStyle } from "./colors";
import { NotionSelectCaret } from "./NotionSelect";
import { databaseViewLabels } from "./databaseViewLabels";
import { statusGroupColumns, statusGroupForOption, statusGroupLabelKey, statusOptionsByOption, statusStageOf } from "./propertyStatusPolicy";
import { StatusGroupLabel } from "./StatusGroupLabel";
import { statusFilterFolded } from "./statusRowOptions";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import selectStyles from "./NotionSelect.module.css";
import styles from "./DatabaseQuickFilterBar.module.css";
import ownStyles from "./DatabaseOptionFilterChoices.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

type Option = { id?: string; name?: string; color?: string };

/** The options a select's or status's filter value names, by ID or name. */
export function chosenFilterOptions(value: unknown): Set<string> {
  if (Array.isArray(value)) return new Set(value.filter((item): item is string => typeof item === "string" && item !== ""));
  return typeof value === "string" && value ? new Set([value]) : new Set();
}

/**
 * A select's, multi-select's or status's filter values to tick any of, as
 * Notion's pill editor and an advanced rule's list show them: a select's
 * options under a search, a status's groups and options.
 */
export function OptionFilterChoices({ property, value, onChoose, onCleared }: {
  property: DbProperty;
  value: unknown;
  /** The values chosen, or nothing when cleared. */
  onChoose(next: string[] | undefined): void;
  /** After "ⓧ" emptied it: a pill's editor closes (SelectFilterChoices). */
  onCleared?(): void;
}) {
  return property.type === "status"
    ? <StatusFilterChoices property={property} chosen={chosenFilterOptions(value)} onChoose={(next) => onChoose(next.length ? next : undefined)} />
    : <SelectFilterChoices property={property} chosen={chosenFilterOptions(value)} onChoose={onChoose} onCleared={onCleared} />;
}

/**
 * An advanced filter rule's options, as Notion's (seen 2026-11-11): a button
 * holding each chosen option as its chip, a line of them showing, and under
 * it, 260 wide from its start, the options' list to tick any of — a rule the
 * API made with several ("a", "c") shows and edits them all.
 */
export function OptionFilterRuleValue({ property, value, onChange }: {
  property: DbProperty;
  value: unknown;
  onChange(next: string[] | undefined): void;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("propertyCell");
  // Its ⌄ its rule's, as the rule's other choices (NotionSelectCaret).
  const placeCaret = useContext(NotionSelectCaret);
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), panelRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(panelRef, open
    ? (size, viewport) => placeAround(popoverRectOf(buttonRef.current)!, size, viewport, { gap: 0, align: "start", alignFallback: true })
    : null, [open], undefined, { kind: "menu", restoreFocus: false, onEscape: () => { setOpen(false); buttonRef.current?.focus({ preventScroll: true }); },
    onOutsidePress: () => setOpen(false), inside: () => [buttonRef.current] });
  const chips = optionChips(property, chosenFilterOptions(value), t);
  // Its search takes the keys as it opens, as Notion's (seen 2026-11-11). Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
  // an advanced rule's "옵션 선택" pressed, "z" went into its search (Leafen the same).
  useEffect(() => { if (open) panelRef.current?.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true }); }, [open]);
  return (
    <span className={selectStyles.notionSelect} data-notion-select="">
      <button ref={buttonRef} type="button" className={`${selectStyles.notionSelectButton} ${styles.ruleOptionsButton}`} data-notion-select-button=""
        aria-label={labels.filterValueFor(property.name)} aria-haspopup="listbox" aria-expanded={open} data-filter-rule-options=""
        onClick={() => setOpen(!open)}>
        {chips.length ? (
          <span className={styles.ruleOptionChips}>
            {chips.map((chip) => (
              <span key={chip.key} className={styles.editorChip} style={chipStyle(chip.color)}>
                <span className={styles.editorChipText}>{chip.name}</span>
              </span>
            ))}
          </span>
        ) : <span className={selectStyles.notionSelectLabel} data-placeholder="true">{labels.chooseOption}</span>}
        {placeCaret ?? <ChevronDown size="tiny" aria-hidden="true" />}
      </button>
      {open && createPortal(
        <div ref={panelRef} className={`${styles.popover} ${styles.rulePopover}`} data-filter-rule-options-list=""
          style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
          {/* A select's "ⓧ" clearing them all closes it, the button reading "옵션 선택" again; a status's
              "선택 취소" leaves its list open, unticked (Notion's, seen 2026-11-11). */}
          <OptionFilterChoices property={property} value={value}
            onChoose={(next) => { onChange(next); if (!next && property.type !== "status") setOpen(false); }} />
        </div>, document.body,
      )}
    </span>
  );
}

// The chips a rule's button shows, in the order it names them, as Notion's
// (seen 2026-11-11: ["Done", "To-do"] reads "Done", "Not started"): an
// option as itself, a status group as its options (a group standing alone
// by its name in the reader's language), and what the property no longer
// has as what it names.
function optionChips(property: DbProperty, chosen: ReadonlySet<string>, t: (key: string) => string) {
  const options = (Array.isArray(property.config?.options) ? property.config.options : []) as Option[];
  const groups = property.type === "status" ? statusGroupColumns(property.config) : [];
  const listed = property.type === "status" ? statusOptionsByOption(property.config) : [];
  const chips: { key: string; name: string; color?: string }[] = [];
  const add = (key: string, name: string, color?: string) => {
    if (!chips.some((chip) => chip.key === key)) chips.push({ key, name, color });
  };
  for (const value of chosen) {
    const group = groups.find((candidate) => candidate.id === value || candidate.name === value);
    const members = group ? listed.filter((option) => statusGroupForOption(property.config, option)?.id === group.id) : [];
    const option = options.find((candidate) => candidate.id === value || candidate.name === value);
    if (members.length) for (const member of members) add(member.id, member.name, member.color);
    else if (group) {
      const key = statusGroupLabelKey(group.name);
      add(group.id, key ? t(key) : group.name, group.color);
    } else if (option) add(option.id ?? value, option.name ?? value, option.color);
    else add(value, value);
  }
  return chips;
}

function SelectFilterChoices({ property, chosen, onChoose, onCleared }: {
  property: DbProperty;
  chosen: ReadonlySet<string>;
  onChoose(next: string[] | undefined): void;
  onCleared?(): void;
}) {
  const labels = databaseViewLabels();
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  // A pill's "ⓧ" shows once an option is chosen or a word typed, and empties both and closes the editor, the
  // focus going back to what opened it — the pill, or the filter button that made it — as Notion's (seen
  // 2026-11-16 by w1383 in three tabs, a letter typed after lighting the ring there). A rule's list keeps
  // its "ⓧ" for chosen options alone (Notion's not seen).
  const clearable = chosen.size > 0 || (onCleared !== undefined && query !== "");
  // The option the list lights: its first as it opens and as the search changes, moved by ↑↓, chosen by Enter (Notion's, seen 2026-11-05).
  const [lit, setLit] = useState(0);
  const options = (Array.isArray(property.config?.options) ? property.config.options : []) as Option[];
  const matching = options.filter((option) => (option.name ?? "").toLowerCase().includes(query.trim().toLowerCase()));
  // A verification's three states tick plainly, with nothing to search, as Notion's (seen 2026-11-05).
  const statesOnly = (property.config as { filtersByStateOnly?: boolean } | undefined)?.filtersByStateOnly === true;
  const optionId = (option: Option) => option.id ?? option.name ?? "";
  const isOn = (option: Option) => chosen.has(optionId(option)) || (option.name ? chosen.has(option.name) : false);
  const toggle = (option: Option) => {
    const id = optionId(option), on = isOn(option), next = new Set(chosen);
    if (on) { next.delete(id); if (option.name) next.delete(option.name); } else next.add(id);
    onChoose([...next]);
  };
  const litIndex = Math.min(lit, matching.length - 1);
  return (
    <div className={styles.editorOptions}>
      {statesOnly ? null : <div className={styles.editorOptionSearch}>
        {/* A chosen option as a chip with its "×", and "ⓧ" clearing them all, as Notion's (seen 2026-11-05):
            the chips and what's typed wrap in their own place, the "ⓧ" keeping the first line's end (2026-11-07). */}
        <div className={styles.editorOptionPicks}>
        {[...chosen].map((id) => {
          const option = options.find((candidate) => candidate.id === id || candidate.name === id);
          return option ? <span key={id} className={`${styles.editorChip} ${styles.editorChipRemovable}`} style={chipStyle(option.color)}>
            <span className={styles.editorChipText}>{option.name}</span>
            <button type="button" tabIndex={-1} aria-label={labels.removeQuickFilter(option.name ?? "")} onClick={() => toggle(option)}>
              <XMarkIcon size="small" aria-hidden="true" />
            </button>
          </span> : null;
        })}
        <input ref={searchRef} value={query} placeholder={chosen.size ? "" : labels.filterOptionsPlaceholder}
          aria-label={labels.filterValueFor(property.name)} aria-activedescendant={matching.length ? `filter-option-${optionId(matching[litIndex]!)}` : undefined}
          onChange={(event) => { setQuery(event.target.value); setLit(0); }}
          onKeyDown={(event) => {
            if (imeComposing(event)) return;
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault();
              if (matching.length) setLit((litIndex + (event.key === "ArrowDown" ? 1 : matching.length - 1)) % matching.length);
            } else if (fieldEnter(event) && matching[litIndex]) {
              event.preventDefault(); toggle(matching[litIndex]!);
            }
          }} />
        </div>
        {/* Notion's 16px chips-field "ⓧ" (seen 2026-11-17 at 1440: a select pill's and the trash filters' alike). */}
        <SearchClearButton value={clearable} small className={styles.editorClearAll} tabIndex={-1} label={labels.sheetClearValue}
          inputRef={onCleared ? undefined : searchRef} onClear={() => { if (chosen.size) onChoose(undefined); onCleared?.(); }} />
      </div>}
      <div role="listbox" aria-multiselectable="true" aria-label={labels.filterValueFor(property.name)} className={styles.editorOptionList}>
        {!matching.length && query.trim() ? <div className={ownStyles.noResults} role="presentation">{labels.noSearchResults}</div> : null}
        {matching.map((option, index) => {
          const id = optionId(option);
          const on = isOn(option);
          return (
            <button key={id} id={`filter-option-${id}`} type="button" role="option" aria-selected={on} className={styles.editorOption}
              data-quick-filter-option={id} data-lit={!statesOnly && index === litIndex ? "true" : undefined}
              onPointerMove={() => { if (index !== litIndex) setLit(index); }}
              onClick={() => toggle(option)}>
              <span className={styles.editorOptionBox} data-checked={on ? "true" : undefined} aria-hidden="true">
              </span>
              {statesOnly ? <span>{option.name}</span> : <span className={styles.editorChip} style={chipStyle(option.color)}><span className={styles.editorChipText}>{option.name}</span></span>}
            </button>
          );
        })}
      </div>
      {statesOnly && chosen.size ? (
        <button type="button" className={styles.editorOption} data-quick-filter-clear
          onClick={() => onChoose(undefined)}>{labels.sheetClearValue}</button>
      ) : null}
    </div>
  );
}

/**
 * A status filter's choices as Notion's list reads them (seen 2026-11-11):
 * a group chosen ticks its options, and its options all chosen tick it;
 * ticking or unticking keeps a group whose options are all ticked as the
 * group ("Done" alone becomes "Complete"), the rest as their options.
 */
function statusChoices(property: DbProperty, chosen: ReadonlySet<string>) {
  const groups = statusGroupColumns(property.config);
  const listed = statusOptionsByOption(property.config);
  const has = (id: string, name?: string) => chosen.has(id) || (name ? chosen.has(name) : false);
  const membersOf = (groupId: string) => listed.filter((option) => statusGroupForOption(property.config, option)?.id === groupId);
  const groupOf = (option: { id: string }) => groups.find((group) => membersOf(group.id).some((member) => member.id === option.id));
  const groupChosen = (group: { id: string; name: string }) => has(group.id, group.name);
  const optionOn = (option: { id: string; name: string }) => {
    const group = groupOf(option);
    return has(option.id, option.name) || (group ? groupChosen(group) : false);
  };
  const groupOn = (group: { id: string; name: string }) => {
    const members = membersOf(group.id);
    return groupChosen(group) || (members.length > 0 && members.every(optionOn));
  };
  // What the list ticks after a change, kept as groups where whole.
  const keep = (ticked: Set<string>, groupsOn: Set<string>) => {
    const next: string[] = [];
    const known = new Set<string>();
    for (const group of groups) {
      known.add(group.id).add(group.name);
      const members = membersOf(group.id);
      for (const member of members) known.add(member.id).add(member.name);
      if (members.length ? members.every((member) => ticked.has(member.id)) : groupsOn.has(group.id)) next.push(group.id);
      else next.push(...members.filter((member) => ticked.has(member.id)).map((member) => member.id));
    }
    // A value the property no longer has stays as it was.
    return [...next, ...[...chosen].filter((value) => !known.has(value))];
  };
  const toggle = (target: { kind: "group" | "option"; id: string }) => {
    const ticked = new Set(listed.filter(optionOn).map((option) => option.id));
    const groupsOn = new Set(groups.filter(groupOn).map((group) => group.id));
    if (target.kind === "group") {
      const group = groups.find((candidate) => candidate.id === target.id)!;
      const on = groupOn(group);
      for (const member of membersOf(group.id)) if (on) ticked.delete(member.id); else ticked.add(member.id);
      if (on) groupsOn.delete(group.id); else groupsOn.add(group.id);
    } else if (ticked.has(target.id)) ticked.delete(target.id);
    else ticked.add(target.id);
    return keep(ticked, groupsOn);
  };
  return { groups, listed, membersOf, optionOn, groupOn, toggle };
}

/**
 * A status filter's values, as Notion's (seen 2026-10-08, 2026-11-08): each
 * group ticked, its mark and name in its color (a "완료" group reads apart
 * from a "완료" option), its options under it with their dots, 8px further
 * in, and "선택 취소" under them. An option standing for its whole group
 * ("내 작업"'s own) is not listed: the group is its tick. Options folded as
 * it opens (statusFilterFolded) wait under "기타 상태 옵션 N개", the status
 * mark before it, a line over it as over "선택 취소"; its press opens them
 * in their groups' places and it leaves (Notion's My tasks, seen 2026-11-24
 * in two tabs). What it folded stays so while it is open, ticks and all.
 */
function StatusFilterChoices({ property, chosen, onChoose }: {
  property: DbProperty;
  chosen: ReadonlySet<string>;
  onChoose(next: string[]): void;
}) {
  const { t } = useTranslation(["propertyCell", "databaseView"]);
  const labels = databaseViewLabels();
  const { groups, membersOf, optionOn, groupOn, toggle } = statusChoices(property, chosen);
  const [folded, setFolded] = useState(() => statusFilterFolded(property.config, chosen));
  const row = (key: string, on: boolean, content: React.ReactNode, onClick: () => void, data: Record<string, string>, nested = false) => (
    <button key={key} type="button" role="option" aria-selected={on} className={styles.editorOption}
      data-nested={nested ? "true" : undefined} onClick={onClick} {...data}>
      <span className={styles.editorOptionBox} data-checked={on ? "true" : undefined} aria-hidden="true">
      </span>
      {content}
    </button>
  );
  return (
    <div className={styles.editorOptions}>
      <div role="listbox" aria-multiselectable="true" aria-label={labels.filterValueFor(property.name)} className={styles.editorStatusList}>
        {groups.map((group, index) => {
          const key = statusGroupLabelKey(group.name);
          return (
            <Fragment key={group.id}>
              {row(group.id, groupOn(group),
                <StatusGroupLabel stage={statusStageOf(index, groups.length)} color={group.color} label={key ? t(`propertyCell:${key}`) : group.name} />,
                () => onChoose(toggle({ kind: "group", id: group.id })), { "data-quick-filter-status-group": group.id })}
              {membersOf(group.id).filter((option) => !folded.has(option.id)).map((option) => row(
                option.id, optionOn(option),
                <span className={styles.editorChip} style={chipStyle(option.color)} data-status-chip=""><span className={styles.editorChipText}>{option.name}</span></span>,
                () => onChoose(toggle({ kind: "option", id: option.id })), { "data-quick-filter-option": option.id }, true,
              ))}
            </Fragment>
          );
        })}
      </div>
      {folded.size ? (
        <button type="button" className={`${styles.editorClear} ${ownStyles.folded}`} onClick={() => setFolded(new Set())} data-quick-filter-folded="">
          <PropertyTypeIcon type="status" size="regular" />{t("databaseView:otherStatusOptions", { count: folded.size })}
        </button>
      ) : null}
      <button type="button" className={styles.editorClear} onClick={() => onChoose([])} data-quick-filter-clear="">
        {t("databaseView:clearSelection")}
      </button>
    </div>
  );
}
