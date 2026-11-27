import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, CheckboxValueIcon, CheckIcon, ChevronDown } from "@/icons/leafen";
import { useProductPages } from "@/components/product/productPagesContext";
import type { WorkspacePerson } from "@/lib/workspacePeopleChoices";
import type { DbProperty } from "@/lib/types";
import { personLabel } from "@/lib/peopleDirectory";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetHeadButton, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { chipStyle } from "./colors";
import { PhoneChoiceSheet } from "./DatabasePhonePickers";
import {
  triggerChosen,
  triggerConditionKind,
  triggerHoldsKey,
  triggerOperatorChoices,
  triggerOperatorLabel,
  triggerPropertyOptions,
  triggerSentence,
  triggerSentenceText,
  triggerSetKey,
  type TriggerOperator,
  type TriggerOperatorChoice,
} from "./automationTriggerConditions";
import { statusConditionChoices, statusConditionItems } from "./automationStatusCondition";
import { statusGroupLabelKey } from "./propertyStatusPolicy";
import { StatusGroupLabel } from "./StatusGroupLabel";
import styles from "./PhoneAutomationCondition.module.css";
import { fieldFocusOnOpen, phoneScreen } from "@/lib/screenKind";
import { ConditionPersonRow, ConditionPicksField, ConditionSentencePeople } from "./AutomationConditionPicks";
import { PhoneRelationConditionPages, RelationConditionPage, relationPageName, useTriggerRelationPages } from "./AutomationRelationCondition";
import relationStyles from "./AutomationRelationCondition.module.css";

export type TriggerConditionValue = { operator?: TriggerOperator; value?: unknown };
const NAME = "\u0001", VALUES = "\u0002";

/** The workspace's people, for a person property's condition. */
function useWorkspacePeople(wanted: boolean) {
  const load = useProductPages()?.workspacePeople;
  const [people, setPeople] = useState<WorkspacePerson[]>([]);
  useEffect(() => {
    if (!wanted || !load) return;
    const controller = new AbortController();
    load(controller.signal).then((result) => { if (!controller.signal.aborted) setPeople(result); }, () => {});
    return () => controller.abort();
  }, [load, wanted]);
  return people;
}

/**
 * A property trigger's card words, as Notion's (seen 2026-10-24), in the
 * sentences of databaseView's automationTrigger* keys: the property's name in
 * 500 and its options as their pills (Stage set to Lead, Won or Lost), a
 * number's or words' value in a grey one ("Amount > [15]"), a cleared
 * property (Owner), "Done 속성 편집"; a status's group ticked whole as a pill
 * of its own name and colour (Status set to Complete, seen 2026-11-11); a
 * relation's one page in the line, two or more under it one to a line with
 * the line's place for them left empty (seen 2026-11-17 in Chrome at 1440 and
 * on a Galaxy A90 at 411); people picked by their faces and names, one in
 * the line, two or more side by side on a line of their own between the
 * sentence's words before and after them (seen 2026-11-18 in Chrome at 1440);
 * a checkbox's state a pill with its box (Done set to a ticked "선택됨", seen
 * 2026-11-18 in Chrome at 1440). The words keep their own
 * spaces and wrap piece by piece, the lines touching (`.sentence`; Notion's,
 * seen 2026-11-18 in Chrome at 1440 in two tabs and on a Galaxy A90 at 411).
 */
export function TriggerConditionSentence({ property, condition }: { property: DbProperty; condition: TriggerConditionValue }) {
  const { t } = useTranslation("databaseView");
  const people = useWorkspacePeople(property.type === "person" && Boolean(condition.operator));
  const options = triggerPropertyOptions(property);
  const pages = useTriggerRelationPages(property);
  const pageOf = (id: string) => pages.find((page) => page.id === id);
  const pageName = (id: string) => { const page = pageOf(id); return page ? relationPageName(page) : t("untitled"); };
  // A person by the workspace's list, else by the people the app knows while that list comes (no ID shown meanwhile).
  const nameOf = (id: string) => property.type === "relation" ? pageName(id)
    : options.find((option) => option.id === id)?.name ?? people.find((person) => person.id === id)?.name ?? (personLabel(id, undefined, false) || id);
  const sentence = triggerSentence(t, property, condition.operator, condition.value, nameOf);
  // A status's names come in its items' order (triggerSentence).
  const statusItems = property.type === "status" && Array.isArray(condition.value) ? statusConditionItems(property, condition.value) : null;
  // "[Lead], [Won] 또는 [Lost]": a comma held to its pill with the space after it (the two wrap as one, as Notion's
  // "[알파], " box), " 또는 " before the last as words of its own.
  // A checkbox's states are pills of their own, each with its box (CheckboxValueIcon).
  const states = sentence.form === "set" ? sentence.states : undefined;
  const chips = (values: readonly string[]) => values.map((name, index) => {
    const option = statusItems ? statusItems[index] : options.find((candidate) => candidate.name === name);
    const chip = states ? <span key={`chip-${index}`} className={styles.checkboxChip} data-checkbox-value={String(states[index])}>
      <CheckboxValueIcon checked={states[index]} size="tiny" aria-hidden="true" />{name}</span>
      : <span key={`chip-${index}`} className={option ? styles.pill : styles.valueChip} style={option ? chipStyle(option.color) : undefined}
        data-status-pill={option && property.type === "status" ? "" : undefined}>{name}</span>;
    if (index < values.length - 2) return [<span key={`joint-${index}`}>{chip}{", "}</span>];
    return index === values.length - 2 ? [chip, <span key={`joint-${index}`}>{t("automationTriggerOr")}</span>] : [chip];
  }).flat();
  const name = <strong>{sentence.name}</strong>;
  if (sentence.form === "compare") {
    return <span className={styles.sentence} data-valued={sentence.values.length ? "compare" : undefined}>
      {name}<span>{` ${sentence.operator} `}</span>{chips(sentence.values)}</span>;
  }
  const template = sentence.form === "edited" ? t("automationSummary.propertyEdited", { name: NAME })
    : sentence.form === "cleared" ? t("automationTriggerCleared", { name: NAME })
      : t(sentence.form === "set" ? triggerSetKey(sentence) : triggerHoldsKey(sentence), { name: NAME, values: VALUES });
  // A relation's pages are its pages' icons and names, people their faces and names, not pills: two or more pages go
  // under the line, people on a line of their own within it (ConditionSentencePeople).
  const pageIds = (sentence.form === "holds" || sentence.form === "lacks") && sentence.many !== undefined
    ? (triggerChosen(condition.value) ?? []).map(String) : null;
  const person = property.type === "person";
  const mention = (id: string, index: number) => <RelationConditionPage key={`${id}-${index}`} page={pageOf(id)} name={pageName(id)} />;
  const peopleLine = person && pageIds !== null && pageIds.length > 1;
  // Pills are the line's own pieces, wrapping one by one; two or more people keep a line of their own (`values`).
  const pills = pageIds ? [] : chips("values" in sentence ? sentence.values : []);
  const values = pageIds && person ? <ConditionSentencePeople people={pageIds.map((id) => ({ id, name: nameOf(id) }))} />
    : pageIds ? pageIds.length === 1 ? mention(pageIds[0], 0) : null : pills;
  // Its tip is the sentence's words, the names joined with "및" in the line (Notion's, seen
  // 2026-11-18 in Chrome at 1440 for people and pages), not the card's words in the order they are laid out.
  const words = pageIds ? triggerSentenceText(t, sentence) : undefined;
  const line = <span className={styles.sentence} data-people-line={peopleLine ? "" : undefined} data-valued={pills.length && !states ? "set" : undefined}
    data-checkbox={states ? "" : undefined}
    data-card-words={!person && pageIds && pageIds.length > 1 ? undefined : words}>
    {template.split(/(\u0001|\u0002)/u).filter(Boolean).map((part, index) => part === NAME ? <strong key={index}>{sentence.name}</strong>
      : part === VALUES ? peopleLine ? <span key={index} className={styles.values}>{values}</span> : <Fragment key={index}>{values}</Fragment>
        : <span key={index}>{part}</span>)}
  </span>;
  return pageIds && !person && pageIds.length > 1
    ? <span className={relationStyles.pagesSentence} data-card-words={words}>{line}<span className={relationStyles.pageList}>{pageIds.map(mention)}</span></span>
    : line;
}

/**
 * What a property trigger's value picker shows and does, the same on a phone's
 * screen and a desktop's window (AutomationConditionValuePopover): the
 * operator, an option list's or people's items with "모든 옵션" (none chosen
 * is all of them), a checkbox's two states, or a number's or words' value.
 *
 * The chosen ones (`picked`, in the order chosen) are chips in the search
 * (ConditionPicksField). An option list's every option ticked is "모든
 * 옵션" again, its chips gone; people all picked stay picked, "모든 옵션"
 * ticked over their chips, and "모든 옵션" pressed then takes them all
 * back. Notion's, seen 2026-11-17 in Chrome at 1440 and on a Galaxy A90 at
 * 411 (people's last press not seen).
 */
export function useTriggerConditionChoices(property: DbProperty, condition: TriggerConditionValue, onChange: (next: TriggerConditionValue) => void) {
  const kind = triggerConditionKind(property);
  const personList = property.type === "person";
  const people = useWorkspacePeople(personList);
  const operator: TriggerOperatorChoice = condition.operator ?? "edited";
  // People by their names, the reader's own too (Notion lists the reader by name, not as "나").
  const items = kind === "options" || property.type === "multi_select"
    ? triggerPropertyOptions(property).map((option) => ({ id: option.id, name: option.name, color: option.color as string | undefined }))
    : people.map((person) => ({ id: person.id, name: person.name, color: undefined }));
  const chosen = triggerChosen(condition.value);
  const all = chosen === null;
  const chosenIds = (chosen ?? []).filter((item): item is string => typeof item === "string");
  const picked = chosenIds.flatMap((id) => items.filter((item) => item.id === id));
  const allTicked = all || (personList && items.length > 0 && items.every((item) => chosenIds.includes(item.id)));
  // "모든 옵션" with some but not all of an option list ticked: "−" on its box, on a phone's screen and a desktop's window
  // alike (Notion's, seen 2026-11-17 in Chrome at 1440 in two tabs and on a Galaxy A90 at 411; all ticked again from it, as
  // Leafen's toggleAll). People picked leave it empty.
  const someTicked = !personList && !all && chosenIds.length > 0;
  const allBox = (box: HTMLInputElement | null) => { if (box) box.indeterminate = someTicked; };
  const toggle = (id: string) => {
    const current = chosen ? chosenIds : items.map((item) => item.id);
    const next = current.includes(id) ? current.filter((item) => item !== id) : [...current, id];
    onChange({ ...condition, value: !personList && next.length === items.length ? undefined : next });
  };
  const toggleAll = () => onChange({ ...condition, value: allTicked ? [] : undefined });
  // A person's Enter (useConditionListKeys): under "모든 옵션", where the first of the dimmed list is lit, that one alone
  // (Notion's, seen 2026-11-18 in Chrome at 1440 in two tabs); else as a press.
  const pick = (id: string) => { if (personList && all) onChange({ ...condition, value: [id] }); else toggle(id); };
  // The search's "ⓧ": the chosen ones taken back, none chosen (its words stay).
  const clearPicked = () => { if (picked.length) onChange({ ...condition, value: [] }); };
  const checkbox = kind === "checkbox";
  const states = checkbox ? (chosen ?? [false, true]) : [];
  const toggleState = (state: boolean) => {
    const next = states.includes(state) ? states.filter((item) => item !== state) : [...states, state];
    onChange(next.length === 1 ? { operator: "is", value: next } : {});
  };
  const listed = operator === "is" || operator === "is_not" || operator === "contains" || operator === "does_not_contain";
  const choosing = listed && (kind === "options" || kind === "list" || kind === "pages");
  const typing = !checkbox && !choosing && (listed || (kind === "number" || kind === "text") && operator !== "edited" && operator !== "cleared");
  const pills = kind === "options" || property.type === "multi_select";
  const pickOperator = (key: TriggerOperatorChoice) => {
    const next = key === "edited" ? undefined : key;
    const keepsList = next && ["is", "is_not", "contains", "does_not_contain"].includes(next) && (kind === "options" || kind === "list" || kind === "pages");
    onChange({ operator: next, value: keepsList ? condition.value : undefined });
  };
  const typed = condition.value === undefined || condition.value === null ? "" : String(condition.value);
  const type = (text: string) => {
    const number = Number(text);
    onChange({ ...condition, value: kind === "number" ? text.trim() === "" || !Number.isFinite(number) ? "" : number : text });
  };
  return { kind, operator, items, chosen, all, allTicked, allBox, toggle, pick, toggleAll, picked, personList, clearPicked, checkbox, states, toggleState,
    choosing, typing, pills, pickOperator, typed, type };
}

export type TriggerConditionChoices = ReturnType<typeof useTriggerConditionChoices>;

/**
 * Where a property trigger says what the edit leaves, on a phone (Notion's
 * desktop list page, seen 2026-10-24, as a screen): "←" and the property's
 * name with its operator ("Stage 다음과 동일함 ⌄", picked in a half sheet)
 * and "완료" at the head; then, as the operator asks, a search holding the
 * chosen ones as chips (ConditionPicksField) with "모든 옵션" and each option
 * ticked or each person to pick (ConditionPersonRow), a number's or words'
 * "값 입력" (a number's a 12px row, words' a box a level in), or
 * a checkbox's "체크 표시 해제됨"·"체크 표시됨" under "Done 속성 설정 값:",
 * each ✓ while ticked. A status's names no
 * operator and has no search: "모든 옵션", then its groups ticked whole and
 * their options under them, as its desktop window (automationStatusCondition.ts).
 * A relation's pages are its relation filter's list (PhoneRelationConditionPages).
 * Its groups 28 apart, lined on top only, and 45px rows: a search, then an
 * option list's "모든 옵션" in a group of its own and its options in another,
 * people's "모든 옵션" under their search and the people in another
 * (PhoneAutomationCondition.module.css).
 *
 * On a phone a select's or multi-select's option search takes the keys as
 * the screen opens, from "+ 새 조건" or the card, the keyboard up; people's
 * and a relation's search and a value's field wait for a tap. Judged after
 * the press only, no script in the page before it: Notion on a Galaxy A90 at
 * 411 (2026-11-17 in two tabs at different times: a select four times, once
 * from its card, a multi-select twice — "z" typed went into "모든 옵션"; a
 * relation and a title twice, people and a number once — "z" went nowhere,
 * no keyboard). A touch screen wider than a phone's keeps its fields waiting
 * (fieldFocusOnOpen).
 */
export function PhoneTriggerConditionScreen({ property, condition, onChange, onBack, onDone }: {
  property: DbProperty;
  condition: TriggerConditionValue;
  onChange(next: TriggerConditionValue): void;
  onBack(): void;
  onDone(): void;
}) {
  const { t } = useTranslation(["databaseView", "common", "propertyCell"]);
  const [operators, setOperators] = useState(false);
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const choices = useTriggerConditionChoices(property, condition, onChange);
  const { kind, operator, items, chosen, all, checkbox, states } = choices;
  const shown = items.filter((item) => item.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const status = property.type === "status" ? statusConditionChoices(property, condition) : null;
  const statusRow = (key: string, tick: "on" | "mixed" | "off", content: ReactNode, press: () => TriggerConditionValue,
    data: Record<string, string>, nested = false) => <label key={key} className={`${sheetStyles.row} ${styles.check}`} data-nested={nested || undefined}>
    <input type="checkbox" checked={tick === "on"} ref={(box) => { if (box) box.indeterminate = tick === "mixed"; }}
      onChange={() => onChange(press())} {...data} />
    {content}
  </label>;
  // A checkbox's head says what it is set to, as its desktop window ("Done 속성 설정 값:", Notion's, seen 2026-11-18 on a
  // Galaxy A90 at 411).
  const title = <span className={styles.title}>
    <span>{checkbox ? t("databaseView:automationTriggerCheckbox.head", { name: property.name }) : property.name}</span>
    {checkbox || status ? null : <button type="button" className={styles.operator} onClick={() => setOperators(true)} data-phone-trigger-operator={operator}
      aria-label={t("databaseView:automationTriggerOperatorFor", { name: property.name })}>
      {triggerOperatorLabel(t, property, operator)}<ChevronDown size="small" aria-hidden="true" />
    </button>}
  </span>;
  return (
    <PhoneSheet size="screen" title={title} titleFit="shares" label={property.name} onClose={onBack}
      start={<PhoneSheetHeadButton label={t("common:actions.back")} onPress={onBack}><ArrowLeft size="regular" aria-hidden="true" /></PhoneSheetHeadButton>}
      action={{ label: t("common:actions.done"), onPress: onDone }} data={{ "data-phone-trigger-condition": property.id }}>
      <div className={`${sheetStyles.stack} ${styles.screen}`}>
        {status ? (
          <PhoneSheetGroup>
            {statusRow("all", status.all, <span className={sheetStyles.rowLabel}>{t("databaseView:automationTriggerAllOptions")}</span>,
              status.toggleAll, { "data-phone-trigger-all": "" })}
            {status.groups.flatMap((group) => {
              const key = statusGroupLabelKey(group.name);
              return [
                ...(group.id ? [statusRow(`group:${group.id}`, status.group(group),
                  <StatusGroupLabel stage={group.stage} color={group.color} label={key ? t(`propertyCell:${key}`) : group.name} />,
                  () => status.toggleGroup(group), { "data-phone-trigger-group": group.id })] : []),
                ...group.options.map((option) => statusRow(`option:${option.id}`, status.option(option.id) ? "on" : "off",
                  <span className={styles.pill} style={chipStyle(option.color)} data-status-pill="">{option.name}</span>,
                  () => status.toggleOption(option.id), { "data-phone-trigger-item": option.id }, Boolean(group.id))),
              ];
            })}
          </PhoneSheetGroup>
        ) : checkbox ? (
          <PhoneSheetGroup>
            {[false, true].map((state) => <PhoneSheetRow key={String(state)} more={false} onPress={() => choices.toggleState(state)}
              label={t(state ? "databaseView:automationTriggerCheckbox.checked" : "databaseView:automationTriggerCheckbox.unchecked")}
              value={states.includes(state) ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
              data={{ "data-phone-trigger-state": String(state), "data-chosen": states.includes(state) ? "true" : undefined }} />)}
          </PhoneSheetGroup>
        ) : kind === "pages" && choices.choosing ? (
          <PhoneRelationConditionPages property={property} value={condition.value} onChange={(value) => onChange({ ...condition, value })} />
        ) : choices.choosing ? (() => {
          const people = choices.personList;
          const nothing = !all && !(chosen ?? []).length;
          const search = <ConditionPicksField choices={choices} query={query} onQuery={setQuery} inputRef={searchRef} phone autoFocus={phoneScreen() && !people}
            label={t("databaseView:automationTriggerAllOptions")}
            placeholder={t(people ? "databaseView:automationTriggerSearchPeople"
              : nothing ? "databaseView:automationTriggerPickOptions" : "databaseView:automationTriggerAllOptions")} />;
          const allRow = <label className={`${sheetStyles.row} ${styles.check}`}>
            <input type="checkbox" checked={choices.allTicked} ref={choices.allBox} data-phone-trigger-all onChange={choices.toggleAll} />
            <span className={sheetStyles.rowLabel}>{t("databaseView:automationTriggerAllOptions")}</span>
          </label>;
          // People picked leave the list for the search's chips; under "모든 옵션" all are listed, dimmed (ConditionPersonRow).
          const listed = people ? shown.filter((item) => all || !(chosen ?? []).includes(item.id)) : shown;
          // People's "모든 옵션" is under their search in its group, an option list's in a group of its own (Notion's).
          return <>
            {people ? <PhoneSheetGroup className={styles.searchGroup}>{search}{allRow}</PhoneSheetGroup> : <>
              <PhoneSheetGroup className={styles.searchGroup}>{search}</PhoneSheetGroup>
              <PhoneSheetGroup>{allRow}</PhoneSheetGroup>
            </>}
            {listed.length ? <PhoneSheetGroup>
              {listed.map((item) => people
                ? <ConditionPersonRow key={item.id} id={item.id} name={item.name} dimmed={all} phone onPick={choices.toggle} />
                : <label key={item.id} className={`${sheetStyles.row} ${styles.check}`}>
                  <input type="checkbox" checked={all || (chosen ?? []).includes(item.id)} data-phone-trigger-item={item.id} onChange={() => choices.toggle(item.id)} />
                  {item.color !== undefined || choices.pills
                    ? <span className={styles.pill} style={chipStyle(item.color)} data-status-pill={property.type === "status" ? "" : undefined}>{item.name}</span>
                    : <span className={sheetStyles.rowLabel}>{item.name}</span>}
                </label>)}
            </PhoneSheetGroup> : null}
          </>;
        })() : choices.typing ? (() => {
          const value = <input className={kind === "text" ? styles.words : `${sheetStyles.menuSearch} ${styles.value}`} inputMode={kind === "number" ? "decimal" : undefined}
            autoFocus={fieldFocusOnOpen()} value={choices.typed} placeholder={t("databaseView:automationTriggerValue")}
            aria-label={t("databaseView:automationTriggerValue")} data-phone-trigger-value onChange={(event) => choices.type(event.currentTarget.value)} />;
          // A number's value is the group's row; words' a box a level further in, as a relation's field (Notion's).
          return kind === "text" ? <>
            <div className={relationStyles.phoneLevel} aria-hidden="true" />
            <PhoneSheetGroup><div className={styles.wordsRow}>{value}</div></PhoneSheetGroup>
          </> : <PhoneSheetGroup>{value}</PhoneSheetGroup>;
        })() : null}
      </div>
      {operators ? (
        <PhoneChoiceSheet title="" onClose={() => setOperators(false)} data={{ "data-phone-trigger-operators": "" }}
          choices={triggerOperatorChoices(property).map((key) => ({ key, label: triggerOperatorLabel(t, property, key) }))}
          onPick={(key) => choices.pickOperator(key as TriggerOperatorChoice)} />
      ) : null}
    </PhoneSheet>
  );
}
