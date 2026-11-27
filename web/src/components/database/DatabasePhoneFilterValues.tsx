import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { cloneElement, isValidElement, useEffect, useRef, useState, type ReactNode } from "react";
import { CheckIcon, ChevronDown, X, XCircleIcon, XMarkBoldSmallIcon } from "@/icons/leafen";
import { afterNextPaint } from "@/lib/afterPress";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import type { DbProperty, FilterOperator, RelativeDateFilterUnit, RelativeDateFilterValue, ViewFilter } from "@/lib/types";
import { dateRangeFilterValue } from "@/shared/database/queryFilterPolicy";
import { isRelativeFilterDay, RELATIVE_FILTER_DAYS, relativeDateFilterDays, relativeFilterDay } from "@/shared/database/queryRelativeDate.mjs";
import { SearchClearButton } from "@/components/PageActionSearch";
import { PageIconGlyph } from "../page/PageIcon";
import { PersonAvatar } from "../people/PersonAvatar";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { PhoneSheetNoResults } from "../shell/PhoneSheetNoResults";
import { dateKey } from "./dateUtils";
import { DateFilterCalendar, useRangePick } from "./DateFilterEditor";
import { relativeFilterDayText } from "./DateFilterDayChoice";
import { DateTextInput } from "./DateTextInput";
import { filterPeople, filterPickId, filterPickValue, pagePicks, relationFilterTarget, type FilterPick } from "./DatabaseFilterValueInput";
import noteStyles from "./DatabaseFilterPicks.module.css";
import { useFilterThisPage } from "./filterThisPage";
import { useFilterPageMaker, type FilterPageMaker } from "./filterPageMaker";
import { PhoneFilterCreatePage } from "./PhoneFilterCreatePage";
import { relationPagesFound, useRelationRowsInOrder } from "./relationPageOrder";
import { NO_VALUE_FILTERS, dateFilterLabels, relativeDateUnitLabels } from "./databaseFilterLabels";
import { dateFilterRelativeValue, effectiveFilterOperator, filterOperatorSign } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { databaseViewLabels } from "./databaseViewLabels";
import { NumberTextInput } from "./NumberTextInput";
import { currentUserFilterValue } from "./query";
import styles from "./DatabasePhoneFilterValues.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// A filter's value in a phone's sheet, as Notion's (seen 2026-10-23 at
// 375px): a number's field after its grey sign on a band edge to edge, a
// date's relative parts each in a box of its own over its calendar, and
// people or pages found by a search field and chosen as many as wanted.

/** A number's value: the band, its sign grey 8px in, the field after it and its "ⓧ" while it holds one. */
export function PhoneNumberFilterValue({ property, operator, value, onChange }: {
  property: DbProperty;
  operator: FilterOperator;
  value: unknown;
  onChange(value: number | null): void;
}) {
  const labels = databaseViewLabels();
  return (
    <div className={styles.band} data-filter-number-value="">
      <span className={styles.sign} aria-hidden="true">{filterOperatorSign(operator, property)}</span>
      <NumberTextInput className={styles.field} ariaLabel={labels.filterValueFor(property.name)} value={value} placeholder={labels.value} live onChange={onChange} />
      {typeof value === "number" ? <PhoneFilterValueClear onClear={() => onChange(null)} /> : null}
    </div>
  );
}

/**
 * A typed value's "ⓧ" at its field's end, emptying it: a 20px circle in a
 * 24px box running 4px past the field's row, and the focus goes with it, so
 * the keyboard folds away (Notion's phone, seen 2026-11-15 at 402pt, a word's
 * and a number's field; as the desktop's, DatabaseQuickFilterEditor).
 */
export function PhoneFilterValueClear({ onClear }: { onClear(): void }) {
  const labels = databaseViewLabels();
  return (
    <button type="button" className={`${styles.clear} ${styles.typedClear}`} tabIndex={-1} aria-label={labels.sheetClearValue}
      data-filter-value-clear="" onClick={onClear}>
      <XCircleIcon size="regular" aria-hidden="true" />
    </button>
  );
}

function today() {
  return dateKey(new Date());
}

/**
 * A filter value's short choices rising from the bottom, the chosen one
 * ticked at its end; a pick closes it (Notion's phone, seen 2026-11-11 at
 * 402pt: a day field's "⌄" and a relative date's boxes).
 */
function PhoneFilterChoiceMenu<Key extends string>({ label, value, options, onPick, onClose }: {
  label: string;
  value: Key;
  options: ReadonlyArray<{ value: Key; label: string }>;
  onPick(value: Key): void;
  onClose(): void;
}) {
  return (
    <PhoneSheetMenu label={label} onClose={onClose}>
      {options.map((option) => (
        <PhoneSheetRow key={option.value} label={option.label} small more={false}
          data={{ "data-phone-choice": option.value, "data-chosen": option.value === value ? "true" : undefined }}
          value={option.value === value ? <CheckIcon size="regular" className={styles.choiceCheck} aria-hidden="true" /> : undefined}
          onPress={() => {
            onClose();
            if (option.value !== value) onPick(option.value);
          }} />
      ))}
    </PhoneSheetMenu>
  );
}

/** A choice in an edged box of its own, "⌄" at its end, its choices rising from the bottom (PhoneFilterChoiceMenu). */
function PhoneSelectBox<Key extends string>({ label, value, options, onChange }: {
  label: string;
  value: Key;
  options: ReadonlyArray<{ value: Key; label: string }>;
  onChange(value: Key): void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" className={styles.choiceBox} aria-label={label} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>
        <span className={styles.choiceLabel}>{options.find((option) => option.value === value)?.label}</span>
        <ChevronDown size="tiny" className={styles.chevron} aria-hidden="true" />
      </button>
      {open ? <PhoneFilterChoiceMenu label={label} value={value} options={options} onPick={onChange} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

/** A relative date's count in its field, cleared by its "ⓧ" and kept on leaving it. */
function PhoneCountBox({ label, value, onChange }: { label: string; value: number; onChange(value: number): void }) {
  const labels = databaseViewLabels();
  const [draft, setDraft] = useState(String(value));
  useEffect(() => setDraft(String(value)), [value]);
  const commit = () => {
    const next = Number(draft);
    if (Number.isSafeInteger(next) && next > 0) {
      setDraft(String(next));
      if (next !== value) onChange(next);
    } else setDraft(String(value));
  };
  return (
    <label className={styles.countBox}>
      <input type="number" className={styles.field} min={1} step={1} inputMode="numeric" aria-label={label} value={draft}
        onChange={(event) => setDraft(event.target.value)} onBlur={commit}
        onKeyDown={(event) => {
          if (imeComposing(event)) return;
          if (fieldEnter(event)) event.currentTarget.blur();
        }} />
      {draft ? (
        <button type="button" className={styles.clear} aria-label={labels.sheetClearValue} onMouseDown={(event) => event.preventDefault()}
          onClick={(event) => { setDraft(""); (event.currentTarget.previousElementSibling as HTMLInputElement | null)?.focus(); }}>
          <XCircleIcon size="regular" aria-hidden="true" />
        </button>
      ) : null}
    </label>
  );
}

/**
 * A date's value: the relative date's "이번"/"지난"/"다음", its count and its
 * unit each in a box, a day or a range in fields, then the month in a card,
 * and under a relative date a note in a card that the filter follows today's
 * date.
 */
export function PhoneDateFilterValue({ filter, property, onChange }: {
  filter: ViewFilter;
  property: DbProperty;
  onChange(patch: Partial<ViewFilter>): void;
}) {
  const labels = databaseViewLabels();
  const dateLabels = dateFilterLabels();
  const operator = effectiveFilterOperator(property, filter.operator);
  if (NO_VALUE_FILTERS.has(operator) && operator !== "relative_to_today") return null;
  if (operator === "relative_to_today") {
    const value = dateFilterRelativeValue(filter);
    const days = relativeDateFilterDays("relative_to_today", today(), value);
    const write = (next: RelativeDateFilterValue) => onChange({ operator, value: next });
    const units = relativeDateUnitLabels(value);
    return (
      <div className={styles.stack} data-date-filter-value="relative">
        <PhoneSelectBox label={labels.relativeDateDirectionFor(property.name)} value={value.direction}
          options={(["past", "next", "this"] as const).map((direction) => ({ value: direction, label: labels.relativeDateDirections[direction] }))}
          onChange={(direction) => write(direction === "this" ? { direction, unit: value.unit }
            : { direction, count: value.direction === "this" ? 1 : value.count, unit: value.unit })} />
        {value.direction !== "this" ? (
          <PhoneCountBox label={labels.relativeDateCountFor(property.name)} value={value.count} onChange={(count) => write({ ...value, count })} />
        ) : null}
        <PhoneSelectBox label={labels.relativeDateUnitFor(property.name)} value={value.unit}
          options={(["day", "week", "month", "year"] as const).map((unit) => ({ value: unit, label: units[unit] }))}
          onChange={(unit) => write({ ...value, unit: unit as RelativeDateFilterUnit })} />
        <div className={styles.card}><DateFilterCalendar phone start={days?.start} end={days?.end} /></div>
        <p className={styles.note}>{dateLabels.relativeNote}</p>
      </div>
    );
  }
  if (operator === "is_between") return <PhoneDateRangeFilterValue filter={filter} property={property} onChange={onChange} />;
  // A day named from today shows its words dimmed in the field, its day in
  // the calendar and the note under it; the field's "ⓧ" empties it and its
  // "⌄" lists the days to name; an empty field shows no words (Notion's
  // phone, seen 2026-11-11 at 402pt).
  const named = isRelativeFilterDay(filter.value);
  const day = named ? relativeFilterDay(filter.value, today()) ?? "" : typeof filter.value === "string" ? dateKey(filter.value) : "";
  const write = (value: string | undefined) => onChange({ operator, value });
  return (
    <div className={styles.stack} data-date-filter-value="day">
      <div className={styles.dayField} data-named={named ? "true" : undefined}>
        {named ? <input className={styles.field} value={relativeFilterDayText(filter.value)} disabled aria-label={labels.filterValueFor(property.name)} />
          : <DateTextInput className={`${styles.field} ${styles.dayValue}`} value={day} placeholder="" ariaLabel={labels.filterValueFor(property.name)}
            onChange={(next) => write(next ?? undefined)} />}
        {filter.value ? (
          <button type="button" className={styles.clear} aria-label={labels.sheetClearValue} onClick={() => write(undefined)}>
            <XCircleIcon size="regular" aria-hidden="true" />
          </button>
        ) : null}
        <PhoneDayChoice label={property.name} value={filter.value} onChange={write} />
      </div>
      <div className={styles.card}>
        <DateFilterCalendar phone start={day || undefined} end={day || undefined} onPick={write} />
      </div>
      {named ? <p className={styles.note}>{dateLabels.relativeNote}</p> : null}
    </div>
  );
}

/** "범위 내": its two fields, each cleared by its "ⓧ", over the month, its ends picked in turn (useRangePick). */
function PhoneDateRangeFilterValue({ filter, property, onChange }: {
  filter: ViewFilter;
  property: DbProperty;
  onChange(patch: Partial<ViewFilter>): void;
}) {
  const labels = databaseViewLabels();
  const dateLabels = dateFilterLabels();
  const operator = effectiveFilterOperator(property, filter.operator);
  const range = dateRangeFilterValue(filter.value);
  const write = (next: { start?: string; end?: string }) => onChange({
    operator, value: { ...(next.start ? { start: next.start } : {}), ...(next.end ? { end: next.end } : {}) },
  });
  const { due, setDue, pick } = useRangePick(range, write);
  const ends = [
    { part: "start" as const, day: range.start, label: dateLabels.rangeStartFor(property.name) },
    { part: "end" as const, day: range.end, label: dateLabels.rangeEndFor(property.name) },
  ];
  return (
    <div className={styles.stack} data-date-filter-value="range">
      {ends.map(({ part, day, label }) => (
        <label key={part} className={styles.rangeField} data-range-part={part}>
          <DateTextInput className={`${styles.field} ${styles.dayValue}`} value={day ?? ""} placeholder="" ariaLabel={label}
            onFocus={() => setDue(part)} onChange={(next) => write({ ...range, [part]: next ?? undefined })} />
          {day ? (
            <button type="button" className={styles.clear} aria-label={labels.sheetClearValue}
              onClick={() => { setDue(part); write({ ...range, [part]: undefined }); }}>
              <XCircleIcon size="regular" aria-hidden="true" />
            </button>
          ) : null}
        </label>
      ))}
      <div className={styles.card}>
        <DateFilterCalendar phone start={range.start} end={range.end} due={due} onPick={pick} />
      </div>
    </div>
  );
}

const CUSTOM_DAY = "custom";

/**
 * A day field's "⌄": the days named from today and "사용자 지정 날짜" in a
 * short list rising from the bottom (PhoneFilterChoiceMenu); choosing
 * "사용자 지정 날짜" from a named day empties the value (Notion's phone, seen
 * 2026-11-11 at 402pt).
 */
function PhoneDayChoice({ label, value, onChange }: { label: string; value: unknown; onChange(value: string | undefined): void }) {
  const labels = dateFilterLabels();
  const [open, setOpen] = useState(false);
  const chosen = isRelativeFilterDay(value) ? value : CUSTOM_DAY;
  const options = [...RELATIVE_FILTER_DAYS.map((word) => ({ value: word as string, label: labels.relativeDays(word) })), { value: CUSTOM_DAY, label: labels.customDay }];
  return (
    <>
      <button type="button" className={styles.dayChoice} aria-label={labels.relativeDayFor(label)} aria-haspopup="dialog" aria-expanded={open}
        onClick={() => setOpen(true)}>
        <ChevronDown size="micro" aria-hidden="true" />
      </button>
      {open ? (
        <PhoneFilterChoiceMenu label={labels.relativeDayFor(label)} value={chosen} options={options} onClose={() => setOpen(false)}
          onPick={(next) => onChange(next === CUSTOM_DAY ? undefined : next)} />
      ) : null}
    </>
  );
}

const ME = filterPickId(currentUserFilterValue());

/** The people or pages a filter has chosen, by the ids the desktop's list gives them: "나", "이 페이지" and a sprint's places too (filterPickId). */
function chosenFilterIds(value: unknown): string[] {
  const items = Array.isArray(value) ? value : value == null || value === "" ? [] : [value];
  return items.map(filterPickId).filter(Boolean);
}

function chosenValue(ids: readonly string[]): unknown {
  return ids.length ? ids.map(filterPickValue) : undefined;
}

type Choice = { id: string; label: string; icon?: ReactNode;
  /** Its icon in its chip where that is drawn smaller than in its row (an automation condition's page). */
  chipIcon?: ReactNode;
  /** Its mark beside its name, listed and chosen: a template's own page's "이 템플릿" (pagePicks). */
  note?: string };
const choiceLabel = (choice: Choice) => choice.label;
const choiceNote = (choice: Choice) => choice.note ? <small className={noteStyles.note}>{choice.note}</small> : null;

// A page still being made here, its chip shown before it has an id.
const MAKING = "\u0000making:";

/** A page made here: the empty document, as Notion's new page (pageEmpty). */
function madeChoice(id: string, label: string): Choice {
  return { id, label, icon: <PageIconGlyph page={{ icon: "", iconType: "none", kind: "page", hasContent: false }} size={22} /> };
}

/**
 * People or pages to choose as many of as wanted: a field holding the
 * chosen ones as chips with "×", finding the others; then the list, the
 * chosen ones marked "✓". Its "ⓧ" shows once one is chosen or a word typed,
 * and its press is `onClear`, emptying the value and closing the sheet, as
 * the options' field (PhoneFilterOptions; Notion's phone, seen 2026-11-17 in
 * Android Chrome at 411: a person's and a page's field). A page's chip is a
 * link to it, and a search finding no page offers to make one there
 * (PhoneFilterCreatePage): pressed, its chip shows at once, the words go and
 * the keyboard folds, and the page is made past that frame and chosen, as
 * Notion's (seen 2026-11-17 on iPhone Safari at 402).
 */
export function PhoneChoiceList({ label, placeholder, leads = [], choices, value, pages, maker, onChange, onClear, clearStays, condition }: {
  label: string;
  placeholder: string;
  /** "ⓧ" empties the words too and the field takes the keys, the list staying (an automation condition's pages). */
  clearStays?: boolean;
  /** An automation condition's pages on its screen: Notion's smaller chips and wider row icons there. */
  condition?: boolean;
  /** Pages, named 17px as Notion's; people 16px. */
  pages?: boolean;
  /** Where pages are made when a search finds none (useFilterPageMaker); none, "결과 없음". */
  maker?: FilterPageMaker | null;
  /** Choices before the rest: people's "나", a relation's "이 페이지" or a sprint relation's places (pagePicks); bare where without an icon. */
  leads?: readonly Choice[];
  choices: readonly Choice[];
  value: unknown;
  onChange(value: unknown): void;
  onClear(): void;
}) {
  const labels = databaseViewLabels();
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  // Pages made here, named by their words until the database's rows name them.
  const [made, setMade] = useState<readonly Choice[]>([]);
  // Pages made here stay out of the list until the next choice made or taken
  // back, the list keeping the pages it showed (Notion's, seen 2026-11-17 on
  // iPhone Safari at 402 twice and a Galaxy A90 at 411: the new page's chip
  // only, then "✓" in the list once another was chosen).
  const [unlisted, setUnlisted] = useState<ReadonlySet<string>>(() => new Set());
  const madeCount = useRef(0);
  const chosen = chosenFilterIds(value);
  const latest = useRef({ chosen, onChange });
  latest.current = { chosen, onChange };
  const all = [...leads, ...choices];
  const named = [...chosen.map((id) => all.find((choice) => choice.id === id) ?? made.find((choice) => choice.id === id) ?? { id, label: id }),
    ...made.filter((choice) => choice.id.startsWith(MAKING))];
  const toggle = (id: string) => {
    if (unlisted.size) setUnlisted(new Set());
    onChange(chosenValue(chosen.includes(id) ? chosen.filter((item) => item !== id) : [...chosen, id]));
  };
  const found = query.trim().toLocaleLowerCase();
  const createPage = (words: string) => {
    if (!maker) return;
    const key = `${MAKING}${madeCount.current++}`;
    setQuery("");
    inputRef.current?.blur();
    setMade((list) => [...list, madeChoice(key, words)]);
    afterNextPaint(() => void maker.make(words).then((id) => {
      setMade((list) => list.flatMap((choice) => choice.id !== key ? [choice] : id ? [madeChoice(id, words)] : []));
      if (!id) return;
      setUnlisted((ids) => new Set(ids).add(id));
      latest.current.onChange(chosenValue([...latest.current.chosen.filter((item) => item !== id), id]));
    }, () => setMade((list) => list.filter((choice) => choice.id !== key))));
  };
  // Words nobody has leave "결과 없음" where the people were, as the options' field (Notion's, seen 2026-11-17 in Android Chrome at 411);
  // words no page has, the row making one, only that (seen 2026-11-17 on iPhone Safari at 402: "Z" listed "Zac" alone).
  const shownLeads = leads.filter((lead) => !found || lead.label.toLocaleLowerCase().includes(found));
  // Pages are found as Notion finds them, where words start (relationPagesFound); people anywhere in a name.
  const listed = choices.filter((choice) => !unlisted.has(choice.id));
  const shown = pages ? relationPagesFound(listed, query, choiceLabel) : listed.filter((choice) => !found || choice.label.toLocaleLowerCase().includes(found));
  const row = (choice: Choice, bare = false) => (
    <button key={choice.id} type="button" role="option" aria-selected={chosen.includes(choice.id)} className={`${sheetStyles.row} ${styles.choice}`}
      data-bare={bare ? "true" : undefined} data-filter-choice={choice.id} onClick={() => toggle(choice.id)}>
      {bare ? null : <span className={styles.choiceIcon} aria-hidden="true">{choice.icon}</span>}
      <span className={sheetStyles.rowLabel}>{choice.label}{choiceNote(choice)}</span>
      {chosen.includes(choice.id) ? <CheckIcon size="page" className={styles.check} aria-hidden="true" /> : null}
    </button>
  );
  return (
    <>
      <PhoneSheetGroup>
        <div className={styles.search} data-pages={pages ? "true" : undefined} data-condition={condition ? "true" : undefined}>
          <div className={styles.picks}>
            {named.map((choice) => {
              const parts = <>
                {choice.icon ? <span className={styles.chipIcon} aria-hidden="true">{choice.chipIcon ?? choice.icon}</span> : null}
                <span className={styles.chipLabel}>{choice.label}</span>{choiceNote(choice)}
              </>;
              // A lead names a page by where the filter sits, not one to go to.
              const making = choice.id.startsWith(MAKING), lead = leads.some(({ id }) => id === choice.id);
              return (
                <span key={choice.id} className={styles.chip}>
                  {pages && !making && !lead ? <a className={styles.chipLink} href={pageHref(choice.id)}>{parts}</a> : parts}
                  <button type="button" aria-label={labels.sheetRemoveChoice(choice.label)} disabled={making} onClick={() => toggle(choice.id)}>
                    {pages ? <XMarkBoldSmallIcon size="small" aria-hidden="true" /> : <X size="small" aria-hidden="true" />}
                  </button>
                </span>
              );
            })}
            <input ref={inputRef} value={query} placeholder={named.length ? "" : placeholder} aria-label={label} onChange={(event) => setQuery(event.target.value)} />
          </div>
          <SearchClearButton value={named.length > 0 || query !== ""} small phone label={labels.sheetClearValue}
            inputRef={clearStays ? inputRef : undefined} onClear={clearStays ? () => { setQuery(""); onClear(); } : onClear} />
        </div>
      </PhoneSheetGroup>
      {!shownLeads.length && shown.length === 0 && found ? (pages && maker ? <PhoneFilterCreatePage words={query.trim()} database={maker.database} onPress={() => createPage(query.trim())} />
        : <PhoneSheetNoResults>{labels.noSearchResults}</PhoneSheetNoResults>) : <PhoneSheetGroup>
        <div role="listbox" aria-multiselectable="true" aria-label={label} data-pages={pages ? "true" : undefined} data-condition={condition ? "true" : undefined}>
          {shownLeads.map((lead) => row(lead, !lead.icon))}
          {shown.map((choice) => row(choice))}
        </div>
      </PhoneSheetGroup>}
    </>
  );
}

/** A person filter's people: "나", then everyone else with their picture. */
export function PhonePeopleFilterValue({ property, value, snapshot, onChange, onClear }: {
  property: DbProperty;
  value: unknown;
  snapshot: DatabaseFilterSnapshot;
  onChange(value: unknown): void;
  /** Its field's "ⓧ": empties the value and closes the sheet (PhoneChoiceList). */
  onClear(): void;
}) {
  const labels = databaseViewLabels();
  usePeopleDirectoryRevision();
  const people = filterPeople(snapshot).map((person) => ({
    id: person.id, label: person.label, icon: <PersonAvatar userId={person.id} name={person.label} size={24} />,
  }));
  return <PhoneChoiceList label={labels.filterValueFor(property.name)} placeholder={labels.sheetSearchPeople}
    leads={[{ id: ME, label: labels.me }]} choices={people} value={value} onChange={onChange} onClear={onClear} />;
}

/**
 * A relation filter's pages as the desktop's list gives them (pagePicks):
 * "이 페이지" first where the view names one — a template's own page by its
 * name, "이 템플릿" beside it — or a sprint relation's places, then the
 * related database's pages in the relation picker's order
 * (useRelationRowsInOrder), as Notion's phone lists them (seen 2026-11-20 on
 * a Galaxy A90 at 411: a template's view filtered by "tpl" listed "tpl"
 * first, then "새 페이지" and "가", as its desktop does). Their icons are
 * the phone's 22px.
 */
export function PhonePagesFilterValue({ property, value, snapshot, onChange, onClear }: {
  property: DbProperty;
  value: unknown;
  snapshot: DatabaseFilterSnapshot;
  onChange(value: unknown): void;
  /** Its field's "ⓧ": empties the value and closes the sheet (PhoneChoiceList). */
  onClear(): void;
}) {
  const pageNames = usePageNames();
  const labels = databaseViewLabels();
  const target = relationFilterTarget(property);
  const rows = useRelationRowsInOrder(snapshot.rowsByDatabase[target], snapshot.workspaceId);
  // Its pages named as they are named everywhere now, followed (lib/pageNames.ts).
  const { leads, picks } = pagePicks(property, snapshot, useFilterThisPage(), value, rows, pageNames);
  const maker = useFilterPageMaker(target);
  return <PhoneChoiceList label={labels.filterValueFor(property.name)} placeholder={labels.sheetSearchPages}
    leads={leads.map(phoneChoice)} choices={picks.map(phoneChoice)} value={value} pages maker={maker} onChange={onChange} onClear={onClear} />;
}

/** A pick of the desktop's list as a phone's choice: a page's icon at the phone's 22px, in its row and its chip alike. */
function phoneChoice(pick: FilterPick): Choice {
  const icon = isValidElement<{ size?: number }>(pick.icon) && pick.icon.type === PageIconGlyph ? cloneElement(pick.icon, { size: 22 }) : pick.icon;
  return { id: pick.id, label: pick.label, icon, note: pick.note };
}

/**
 * A rule's value on a screen of its own, from an advanced filter's "값
 * 선택", "원하는 만큼 선택" or "페이지 선택" (Notion's, seen 2026-10-23): the
 * property's name and "완료" at its head.
 */
export function PhoneFilterValueScreen({ property, onClose, children }: {
  property: DbProperty;
  onClose(): void;
  children: ReactNode;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="screen" title={property.name} onClose={onClose} action={{ label: labels.sheetDone, onPress: onClose }}
      data={{ "data-phone-filter-value": "" }}>
      <div className={sheetStyles.stack}>{children}</div>
    </PhoneSheet>
  );
}
