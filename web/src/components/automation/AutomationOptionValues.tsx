import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, X, XCircleIcon } from "@/icons/leafen";
import type { AutomationAction, DbProperty, EditPropertyAutomationAction } from "@/lib/types";
import { chipStyle } from "@/components/database/colors";
import { statusGroupColumns, statusOptionsInGroupOrder } from "@/components/database/propertyStatusPolicy";
import { PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { ListedItems, PickBox, ValueModeButton, automationLiteralInMode, automationValueModeOf } from "./AutomationPersonValue";
import styles from "./AutomationEditProperty.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

export type AutomationOption = { id: string; name: string; color?: string };

export function automationPropertyOptions(property: DbProperty): AutomationOption[] {
  const options = property.type === "status" ? statusOptionsInGroupOrder(property.config) : property.config?.options ?? [];
  return options.filter((option) => typeof option?.id === "string").map(({ id, name, color }) => ({ id, name: name ?? "", color }));
}

export function automationChosenOptionIds(value: unknown): string[] {
  if (Array.isArray(value)) return value.filter((item): item is string => typeof item === "string");
  return typeof value === "string" && value ? [value] : [];
}

/** The options a value names, as the property has them; one it no longer has left out. */
export function automationChosenOptions(property: DbProperty, value: unknown): AutomationOption[] {
  const options = automationPropertyOptions(property);
  return automationChosenOptionIds(value).map((id) => options.find((option) => option.id === id || option.name === id))
    .filter((option): option is AutomationOption => Boolean(option));
}

/**
 * A multi-select's options in a card's sentence, as Notion's (seen
 * 2026-11-11): their pills in the language's list, "[빨강] 및 [파랑]".
 */
export function OptionValueChips({ options }: { options: readonly AutomationOption[] }) {
  return <ListedItems items={options.map((option) => <span key={option.id} className={styles.pill} style={chipStyle(option.color)}>{option.name}</span>)} />;
}

/**
 * The options a select, status or multi-select is set to, as Notion's (seen
 * 2026-11-11): a field with the chosen ones' pills, each with "×", "⊗" at its
 * end taking them all back, and a search; a multi-select's "다음으로 교체… ⌄"
 * under it (not on a page line); then the options — a multi-select's each
 * by a box ticked once chosen, a select's or a status's the one chosen ✓ at
 * its end, picking one closing the window. The first row is lit: arrows move
 * it, Enter picks it, Backspace in an empty search takes back the last.
 */
export function AutomationOptionValues({ action, property, phone, line, onChange, onDone }: {
  action: EditPropertyAutomationAction; property: DbProperty; phone: boolean;
  /** A page line's window: no mode, which the line asks beside its field (AutomationLineChoiceField). */
  line: boolean;
  onChange(action: AutomationAction): void;
  onDone(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [query, setQuery] = useState("");
  const [lit, setLit] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const options = automationPropertyOptions(property);
  const multi = property.type === "multi_select";
  const chosen = action.value.kind === "literal" ? automationChosenOptionIds(action.value.value) : [];
  const mode = multi ? automationValueModeOf(action.value) : "replace";
  const latest = useRef({ action, chosen, mode });
  latest.current = { action, chosen, mode };
  const set = (ids: string[], nextMode = latest.current.mode) => onChange({ ...latest.current.action,
    value: multi ? automationLiteralInMode(ids, nextMode) : { kind: "literal", value: ids.at(-1) ?? null } });
  const toggle = (id: string) => {
    const current = latest.current.chosen;
    set(current.includes(id) ? current.filter((item) => item !== id) : multi ? [...current, id] : [id]);
    setQuery("");
    setLit(0);
    // A select or a status keeps one: picking it closes the window (Notion's, seen 2026-11-11; measured once
    // 2026-11-15 in Chrome 1440 with no script before the press, a select's option picked in "속성 편집").
    if (!multi && !phone) onDone();
    else input.current?.focus({ preventScroll: true });
  };
  const words = query.trim().toLocaleLowerCase();
  const shown = options.filter((option) => option.name.toLocaleLowerCase().includes(words));
  useEffect(() => { setLit(0); }, [words]);
  // A status's options under its groups' names as they are named, each its dotted pill (Notion's, seen 2026-11-11).
  const status = property.type === "status";
  const columns = status && !phone ? statusGroupColumns(property.config) : [];
  const groups = columns.length ? columns : null;
  const ordered = groups ? [...groups.flatMap((group) => shown.filter((option) => group.optionIds.includes(option.id))),
    ...shown.filter((option) => !groups.some((group) => group.optionIds.includes(option.id)))] : shown;
  const shownLit = Math.min(lit, Math.max(0, ordered.length - 1));
  const keyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (ordered.length) setLit((shownLit + (event.key === "ArrowDown" ? 1 : ordered.length - 1)) % ordered.length);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (ordered[shownLit]) toggle(ordered[shownLit].id);
    } else if (event.key === "Backspace" && !query && chosen.length) {
      event.preventDefault();
      set(chosen.slice(0, -1));
    }
  };
  const search = <input ref={input} value={query} placeholder={chosen.length ? "" : t("databaseView:editPropertyOptions")}
    aria-label={t("databaseView:editPropertyOptions")} autoFocus={fieldFocusOnOpen()} onChange={(event) => setQuery(event.currentTarget.value)}
    onKeyDown={phone ? undefined : keyDown} />;
  const chosenOptions = chosen.map((id) => options.find((option) => option.id === id)).filter((option): option is AutomationOption => Boolean(option));
  // On a phone flat white rows, as Notion's (seen 2026-10-24 at 375px): the field, then each option's 16px pill.
  if (phone) {
    return <>
      <PhoneSheetGroup>
        <label className={styles.phoneField}>
          {chosenOptions.map((option) => <span key={option.id} className={styles.phonePill} style={chipStyle(option.color)}>{option.name}</span>)}
          {search}
        </label>
      </PhoneSheetGroup>
      <PhoneSheetGroup>
        {shown.map((option) => <PhoneSheetRow key={option.id} more={false} onPress={() => toggle(option.id)}
          label={<span className={styles.phonePill} style={chipStyle(option.color)}>{option.name}</span>}
          value={chosen.includes(option.id) ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
          data={{ "data-automation-option": option.id, "data-chosen": chosen.includes(option.id) ? "true" : undefined }} />)}
      </PhoneSheetGroup>
    </>;
  }
  const row = (option: AutomationOption) => {
    const picked = chosen.includes(option.id);
    const index = ordered.indexOf(option);
    return <button key={option.id} type="button" role="option" aria-selected={picked} className={styles.optionRow}
      data-automation-option={option.id} data-lit={index === shownLit || undefined}
      onPointerMove={() => { if (index !== shownLit) setLit(index); }} onPointerDown={(event) => event.preventDefault()}
      onClick={() => toggle(option.id)}>
      {multi ? <PickBox checked={picked} /> : null}
      <span className={styles.pill} style={chipStyle(option.color)} data-status-pill={status ? "" : undefined}>{option.name}</span>
      {!multi && picked ? <CheckIcon size="small" aria-hidden="true" /> : null}
    </button>;
  };
  // A value set: its pills, each with "×", and "⊗" at the field's end taking them all back (Notion's, seen 2026-11-11).
  return <div className={styles.options}>
    <label className={styles.optionField}>
      <span className={styles.optionTokens}>
        {chosenOptions.map((option) => (
          <span key={option.id} className={styles.pill} style={chipStyle(option.color)} data-status-pill={status ? "" : undefined}>
            {option.name}
            <button type="button" aria-label={t("databaseView:removeRecipient", { name: option.name })} onClick={() => toggle(option.id)}>
              <X size="micro" aria-hidden="true" />
            </button>
          </span>
        ))}
        {search}
      </span>
      {chosen.length ? <button type="button" className={styles.optionClear} aria-label={t("common:actions.clearAll")} onClick={() => set([])}>
        <XCircleIcon size="small" aria-hidden="true" />
      </button> : null}
    </label>
    {multi && !line ? <div className={styles.optionModes}>
      <ValueModeButton mode={mode} onPick={(next) => set(latest.current.chosen, next)} />
    </div> : null}
    <div role="listbox" aria-multiselectable={multi} aria-label={property.name} className={styles.optionList} data-status={groups ? "" : undefined}>
      {groups ? groups.map((group) => {
        const rows = shown.filter((option) => group.optionIds.includes(option.id));
        return rows.length ? <div key={group.id} role="group" aria-label={group.name} className={styles.statusGroup}>
          <span className={styles.statusGroupName} aria-hidden="true">{group.name}</span>
          {rows.map(row)}
        </div> : null;
      }).concat(ordered.filter((option) => !groups.some((group) => group.optionIds.includes(option.id))).map(row)) : shown.map(row)}
    </div>
  </div>;
}
