import type { RefObject } from "react";
import { useTranslation } from "react-i18next";
import { XMarkBoldSmallIcon, XMarkIcon } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { PersonAvatar } from "../people/PersonAvatar";
import { chipStyle } from "./colors";
import type { TriggerConditionChoices } from "./PhoneAutomationCondition";
import phonePills from "./DatabasePhonePickers.module.css";
import styles from "./AutomationConditionPicks.module.css";

/**
 * An option list's or people's search in a property condition's desktop
 * window and phone screen, as Notion's (seen 2026-11-17 in Chrome at 1440 and
 * on a Galaxy A90 at 411): the chosen ones as chips before the words, in the
 * order chosen, each with its "×" taking it out and the keys going back to
 * the words; "ⓧ" at the field's end once one is chosen or a word typed,
 * taking every chosen one back (none chosen, "모든 옵션" unticked) and
 * leaving the words and the keys where they were. The words read "모든
 * 옵션", "하나 이상의 옵션을 선택하세요..." or "한 명 이상의 사람 검색"
 * while no chip shows (`placeholder`).
 *
 * An option's chip is its pill, 18px of 12px words on a desktop (6px in, its
 * 9px "×" grey in a 16px icon in an 18px box 2px from either side, 4px round)
 * and the phone filter's 24px pill there (the same "×" in a 24px box); a person's is their face (16px, 20 on a phone),
 * the name 6px after it in 12px (14) and a faint 12px "×" in an 18px (20)
 * box. Option chips sit 4px apart (8 on a phone), people's 6, the words
 * after them in what is left of the line, onto the next when under 60px.
 */
export function ConditionPicksField({ choices, query, onQuery, inputRef, label, placeholder, phone = false, autoFocus }: {
  choices: Pick<TriggerConditionChoices, "picked" | "personList" | "toggle" | "clearPicked">;
  query: string;
  onQuery(query: string): void;
  inputRef: RefObject<HTMLInputElement | null>;
  label: string;
  placeholder: string;
  phone?: boolean;
  autoFocus?: boolean;
}) {
  const { t } = useTranslation("databaseView");
  const { picked, personList } = choices;
  const remove = (id: string) => { choices.toggle(id); inputRef.current?.focus({ preventScroll: true }); };
  return (
    <div className={styles.field} data-phone={phone || undefined} data-automation-condition-picks="">
      <div className={styles.picks}>
        {picked.map((item) => {
          const removeLabel = t("sheetRemoveChoice", { name: item.name });
          const press = <button type="button" aria-label={removeLabel} onMouseDown={(event) => event.preventDefault()} onClick={() => remove(item.id)}>
            {personList ? <XMarkBoldSmallIcon size="micro" aria-hidden="true" /> : <XMarkIcon size="small" aria-hidden="true" />}
          </button>;
          return personList ? (
            <span key={item.id} className={styles.person} data-automation-condition-pick={item.id}>
              <PersonAvatar userId={item.id} name={item.name} size={phone ? 20 : 16} />
              <span className={styles.personName}>{item.name}</span>
              {press}
            </span>
          ) : (
            <span key={item.id} className={phone ? styles.phoneOption : styles.option} style={chipStyle(item.color)}
              data-automation-condition-pick={item.id}>
              <span className={phone ? phonePills.pillName : styles.optionName}>{item.name}</span>
              {press}
            </span>
          );
        })}
        <span className={styles.words}>
          <input ref={inputRef} value={query} autoFocus={autoFocus} placeholder={picked.length ? "" : placeholder} aria-label={label}
            onChange={(event) => onQuery(event.currentTarget.value)} />
        </span>
      </div>
      <SearchClearButton value={picked.length > 0 || query !== ""} small phone={phone} label={t("sheetClearValue")} inputRef={inputRef}
        onClear={choices.clearPicked} />
    </div>
  );
}

/**
 * The people a condition's card names, as Notion's (seen 2026-11-18 in
 * Chrome at 1440): each their 20px face, the name 6px after it in the
 * line's 14px words, 4px apart side by side.
 */
export function ConditionSentencePeople({ people }: { people: ReadonlyArray<{ id: string; name: string }> }) {
  return (
    <span className={styles.sentencePeople}>
      {people.map((person, index) => (
        <span key={`${person.id}-${index}`} className={styles.sentencePerson} data-automation-condition-person={person.id}>
          <PersonAvatar userId={person.id} name={person.name} size={20} />
          <span className={styles.sentencePersonName}>{person.name}</span>
        </span>
      ))}
    </span>
  );
}

/**
 * A person to pick in a condition's list, as Notion's (seen 2026-11-17 in
 * Chrome at 1440 and on a Galaxy A90 at 411): no box, their face (20px, 24
 * on a phone) and the name 6px after it; under "모든 옵션" the list is
 * everyone, dimmed and not to be pressed, else those not yet picked. On a
 * desktop the keys' row is `lit` (useConditionListKeys), the pointer moving
 * onto one lighting it (`onLight`).
 */
export function ConditionPersonRow({ id, name, dimmed, phone = false, lit = false, onLight, onPick }: {
  id: string;
  name: string;
  dimmed: boolean;
  phone?: boolean;
  lit?: boolean;
  onLight?(id: string): void;
  onPick(id: string): void;
}) {
  return (
    <button type="button" className={styles.personRow} disabled={dimmed} data-dimmed={dimmed || undefined}
      data-phone={phone || undefined} data-lit={lit || undefined} data-automation-condition-item={id}
      onPointerMove={onLight && !lit ? () => onLight(id) : undefined} onClick={() => onPick(id)}>
      <PersonAvatar userId={id} name={name} size={phone ? 24 : 20} />
      <span className={styles.personRowName}>{name}</span>
    </button>
  );
}
