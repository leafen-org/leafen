import { directoryPeople, personName, withPersonDistinction } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import type { ReactNode } from "react";
import { i18next } from "@/i18n";
import { pageIconSource, pageNamesNow, usePageNames, type PageNameReader } from "@/lib/pageNames";
import type {
  DbProperty,
  FilterOperator,
  OrganizationProfile,
  Page,
  WorkspaceMember,
} from "@/lib/types";
import { PageIconGlyph } from "../page/PageIcon";
import { DateDayFilterField, DateRangeFilterField } from "./DateRangeFilterField";
import { DateFilterDayChoice } from "./DateFilterDayChoice";
import { isRelativeFilterDay } from "@/shared/database/queryRelativeDate.mjs";
import { RelativeDateFilterValueInput } from "./RelativeDateFilterValueInput";
import { NumberTextInput } from "./NumberTextInput";
import { NotionSelect, type NotionSelectOption } from "./NotionSelect";
import { OptionFilterRuleValue } from "./DatabaseOptionFilterChoices";
import { NUMERIC_ROLLUP_FUNCTIONS } from "./databaseFilterLabels";
import { RELATIVE_SPRINTS, relativeSprintOf, sprintFilterValue, type RelativeSprint } from "@/shared/database/queryFilterPolicy";
import { PersonAvatar } from "../people/PersonAvatar";
import { relativeSprintIds } from "./databaseSprintContext";
import { databaseViewLabels } from "./databaseViewLabels";
import { filterPropertyForComparison } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { personLabel } from "./people";
import { FILTER_PAGE_CHIP_ICON, useFilterThisPage, type FilterThisPage } from "./filterThisPage";
import { relationSearchRank, useRelationRowsInOrder } from "./relationPageOrder";
import {
  currentPageFilterValue,
  currentUserFilterValue,
  isCurrentPageFilterValue,
  isCurrentUserFilterValue,
} from "./query";
import styles from "./databaseSheet";

export { filterOperatorsFor } from "./databaseFilterPolicy";

export function DatabaseFilterValueInput({
  prop: sourceProperty,
  operator,
  value,
  databaseProperties,
  snapshot,
  onChange,
  placeholder = databaseViewLabels().value,
  inputClassName,
}: {
  prop: DbProperty;
  operator: FilterOperator;
  value: unknown;
  databaseProperties: readonly DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onChange: (value: unknown) => void;
  /** A typed value's empty words: "값 입력" in a pill's editor, "값" in an advanced filter's row (Notion's, seen 2026-11-03). */
  placeholder?: string;
  /** A typed value's field's look where its holder draws it bare (a phone's sheet row). */
  inputClassName?: string;
}) {
  const prop = filterPropertyForComparison(sourceProperty, databaseProperties);
  if (prop.type === "checkbox") return <CheckboxFilterValue label={prop.name} value={value} onChange={onChange} />;

  if (prop.type === "select" || prop.type === "status" || prop.type === "multi_select") {
    // As many options as wanted, a rule the API made with several too (Notion's, seen 2026-11-11).
    return <OptionFilterRuleValue property={prop} value={value} onChange={onChange} />;
  }

  if (prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by") {
    return (
      <PersonFilterValue
        value={value}
        onChange={onChange}
        label={prop.name}
        snapshot={snapshot}
      />
    );
  }

  if (prop.type === "relation") {
    return <RelationFilterValue prop={prop} value={value} onChange={onChange} snapshot={snapshot} />;
  }

  if (prop.type === "rollup") {
    return (
      <RollupFilterValue
        prop={prop}
        placeholder={placeholder}
        inputClassName={inputClassName}
        value={value}
        onChange={onChange}
        databaseProperties={databaseProperties}
        snapshot={snapshot}
      />
    );
  }

  if (prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time") {
    if (operator === "relative_to_today") {
      return <RelativeDateFilterValueInput label={prop.name} value={value} onChange={onChange} />;
    }
    // One "범위 선택 ⌄" field opening the days and a calendar, as Notion's (seen 2026-11-11).
    if (operator === "is_between") return <DateRangeFilterField label={prop.name} value={value} onChange={onChange} />;
    // A day of its own or one named from today, as Notion's "사용자 지정 날짜 ⌄", the day its own field (seen 2026-11-11, 2026-11-12).
    return (
      <span className={styles.dateDayValue} data-date-day-value="">
        <DateFilterDayChoice label={prop.name} value={value} onChange={onChange} />
        {isRelativeFilterDay(value) ? null : <DateDayFilterField label={prop.name} value={value} onChange={onChange} />}
      </span>
    );
  }

  if (prop.type === "number" || prop.type === "unique_id") {
    return (
      <NumberTextInput
        className={inputClassName}
        ariaLabel={databaseViewLabels().filterValueFor(prop.name)}
        value={value}
        placeholder={placeholder}
        live
        onChange={onChange}
      />
    );
  }

  return (
    <input
      className={inputClassName}
      aria-label={databaseViewLabels().filterValueFor(prop.name)}
      type="text"
      value={value == null ? "" : String(value)}
      onChange={(event) => onChange(event.target.value)}
      placeholder={placeholder}
    />
  );
}

const CLEAR_CHECKBOX = "clear";
const CHECKBOX_MENU = { width: 220, maxHeight: 270, gap: 0 };

/**
 * A checkbox rule's value, as Notion's (seen 2026-11-12): a button showing
 * the 16px box as a cell does, ticked blue or ringed, or "값 선택" in gray
 * with none; its 220px list right under it, its first row lit, "체크 표시되지
 * 않음" and "체크 표시됨" with the chosen one ticked, and under a line
 * "지우기" while one is chosen.
 */
function CheckboxFilterValue({ label, value, onChange }: { label: string; value: unknown; onChange: (value: unknown) => void }) {
  const labels = databaseViewLabels();
  const state = value === true || value === "true" ? true : value === false || value === "false" ? false : undefined;
  return (
    <NotionSelect
      ariaLabel={labels.filterValueFor(label)}
      value={state === undefined ? "" : String(state)}
      placeholder={labels.sheetPickValue}
      buttonContent={state === undefined ? undefined : (
        <span className={styles.checkboxFilterValue}>
          <span className={styles.checkboxFilterBox} data-checked={state ? "true" : undefined} role="img"
            aria-label={state ? labels.checked : labels.unchecked}>
          </span>
        </span>
      )}
      options={[
        { value: "false", label: labels.unchecked },
        { value: "true", label: labels.checked },
        ...(state === undefined ? [] : [{ value: CLEAR_CHECKBOX, label: labels.clearValue, divided: true }]),
      ]}
      firstLit menuSize={CHECKBOX_MENU} menuClassName={styles.relativeChoiceMenu}
      optionClassName={`${styles.relativeChoiceOption} ${styles.checkboxFilterOption}`}
      onChange={(next) => onChange(next === "true" ? true : next === "false" ? false : undefined)}
    />
  );
}

/**
 * A person or relation rule's values in an advanced filter: as many as
 * wanted from a list that stays open, the relative ones first, as Notion's
 * (seen 2026-11-04).
 */
function PicksFilterValue({ label, searchLabel, leads, picks, value, pages, onChange }: {
  label: string; searchLabel: string; leads: readonly FilterPick[]; picks: readonly FilterPick[];
  /** Pages, found where words start (relationSearchRank); people anywhere in a name. */
  pages?: boolean;
  value: unknown; onChange: (value: unknown) => void;
}) {
  const labels = databaseViewLabels();
  const chosen = (Array.isArray(value) ? value : value == null || value === "" ? [] : [value]).map(filterPickId).filter(Boolean);
  return (
    <NotionSelect
      ariaLabel={labels.filterValueFor(label)}
      value=""
      placeholder={searchLabel}
      search={{ ariaLabel: searchLabel, placeholder: searchLabel, noResultsLabel: labels.noMatchingPages, hideDisabledOptions: true,
        rank: pages ? (option, query) => relationSearchRank(option.label, query) : undefined }}
      options={[...leads, ...picks].map((pick) => ({ value: pick.id, label: pick.label, icon: pick.icon, description: pick.note }))}
      multiple={{ values: chosen, onChange: (next) => onChange(next.length ? next.map(filterPickValue) : undefined) }}
      onChange={() => undefined}
    />
  );
}

function PersonFilterValue({ value, onChange, label, snapshot }: {
  value: unknown; onChange: (value: unknown) => void; label: string; snapshot: DatabaseFilterSnapshot;
}) {
  usePeopleDirectoryRevision();
  const { leads, picks } = personPicks(snapshot);
  return <PicksFilterValue label={label} searchLabel={databaseViewLabels().sheetSearchPeople} leads={leads} picks={picks} value={value} onChange={onChange} />;
}

function RelationFilterValue({ prop, value, onChange, snapshot }: {
  prop: DbProperty; value: unknown; onChange: (value: unknown) => void; snapshot: DatabaseFilterSnapshot;
}) {
  const rows = useRelationRowsInOrder(snapshot.rowsByDatabase[relationFilterTarget(prop)], snapshot.workspaceId);
  // Its pages named as they are named everywhere now, followed (lib/pageNames.ts).
  const { leads, picks } = pagePicks(prop, snapshot, useFilterThisPage(), value, rows, usePageNames());
  return <PicksFilterValue label={prop.name} searchLabel={databaseViewLabels().sheetSearchPages} leads={leads} picks={picks} value={value} pages
    onChange={onChange} />;
}

// The relative values by id: "나", "이 페이지", a sprint relation's places.
const PICK_ME = "__leafen_current_user__", THIS_PAGE = "__leafen_current_page__", SPRINT = "__leafen_sprint:";

export function filterPickId(item: unknown) {
  if (isCurrentUserFilterValue(item)) return PICK_ME;
  if (isCurrentPageFilterValue(item)) return THIS_PAGE;
  const sprint = relativeSprintOf(item);
  if (sprint) return SPRINT + sprint;
  return typeof item === "string" ? item.trim() : "";
}

export function filterPickValue(id: string): unknown {
  if (id === PICK_ME) return currentUserFilterValue();
  if (id === THIS_PAGE) return currentPageFilterValue();
  return id.startsWith(SPRINT) ? sprintFilterValue(id.slice(SPRINT.length) as RelativeSprint) : id;
}

export type FilterPick = {
  id: string; label: string; icon?: ReactNode;
  /** Its 16px picture in its chip once chosen. */
  chipIcon?: ReactNode;
  /** A page's chip, its name in 500 3px after its icon, as Notion's page chips (seen 2026-11-12); a person's keeps 6px. */
  page?: boolean;
  /** Its mark beside its name, listed and chosen: a template's own page's "이 템플릿" (FilterThisPage `template`). */
  note?: string;
};

/** A person filter's people: "나", then everyone with their picture. */
export function personPicks(snapshot: DatabaseFilterSnapshot) {
  const people = filterPeople(snapshot, true).map((person) => ({
    id: person.id, label: person.label, icon: <PersonAvatar userId={person.id} name={person.label} size={20} />,
    chipIcon: <PersonAvatar userId={person.id} name={person.label} size={16} />,
  }));
  return { leads: [{ id: PICK_ME, label: databaseViewLabels().me }], picks: people };
}

/**
 * A relation filter's pages with their icons, after a sprint relation's
 * "현재", "다음" and "마지막" (Notion's, seen 2026-11-04) or "this page"
 * where they are pages of its database (FilterThisPage: a layout tab's
 * "이 페이지", a template by its name marked "이 템플릿"); elsewhere the pages alone (Notion's,
 * seen 2026-11-12: a page's or a row page's inline database), "이 페이지"
 * kept only where a filter already holds it. The pages come as `rows`
 * gives them (useRelationRowsInOrder), else as the database's rows stand.
 */
export function pagePicks(property: DbProperty, snapshot: DatabaseFilterSnapshot, thisPage: FilterThisPage | null, value?: unknown,
  rows?: readonly Page[], names: PageNameReader = pageNamesNow) {
  const target = relationFilterTarget(property);
  const sprints = relativeSprintIds(target, "current") !== undefined;
  const held = (Array.isArray(value) ? value : [value]).some((item) => filterPickId(item) === THIS_PAGE);
  const own = thisPage?.dataSourceId === target ? thisPage : null;
  const leads: FilterPick[] = sprints
    ? RELATIVE_SPRINTS.map((place) => ({ id: SPRINT + place, label: i18next.t(`databaseView:relativeSprints.${place}`) }))
    : own || held ? [{ id: THIS_PAGE, label: own?.name || databaseViewLabels().layoutTabThisPage, icon: own?.icon, chipIcon: own?.chipIcon,
      page: Boolean(own?.chipIcon), note: own?.template ? i18next.t("databaseView:filterThisTemplate") : undefined }] : [];
  // A page chosen is a chip of its icon and name, as Notion's (seen 2026-11-12).
  const picks = (rows ?? snapshot.rowsByDatabase[target] ?? []).map((page) => ({
    id: page.id, label: names.title(page), icon: <PageIconGlyph page={pageIconSource(page)} size={20} />,
    chipIcon: <PageIconGlyph page={pageIconSource(page)} size={FILTER_PAGE_CHIP_ICON} />, page: true,
  }));
  return { leads, picks };
}

/** The database a relation filter's pages are rows of. */
export function relationFilterTarget(property: DbProperty) {
  return property.config?.relationDatabaseId ?? property.databaseId;
}

export { pageIconSource };

function RollupFilterValue({
  prop,
  placeholder,
  inputClassName,
  value,
  onChange,
  databaseProperties,
  snapshot,
}: {
  prop: DbProperty;
  placeholder: string;
  inputClassName?: string;
  value: unknown;
  onChange: (value: unknown) => void;
  databaseProperties: readonly DbProperty[];
  snapshot: DatabaseFilterSnapshot;
}) {
  const relationProp = databaseProperties.find(
    (candidate) => candidate.type === "relation"
      && candidate.id === prop.config?.rollupRelationPropertyId,
  );
  const firstHopDbId = relationProp?.config?.relationDatabaseId ?? relationProp?.databaseId;
  const firstHopProps = firstHopDbId ? snapshot.propertiesByDatabase[firstHopDbId] ?? [] : [];
  const targetProp = firstHopProps.find(
    (candidate) => candidate.id === prop.config?.rollupTargetPropertyId,
  );
  const rollupFunction = prop.config?.rollupFunction ?? "show_original";

  if (NUMERIC_ROLLUP_FUNCTIONS.has(rollupFunction)) {
    return (
      <NumberTextInput
        className={inputClassName}
        ariaLabel={databaseViewLabels().filterValueFor(prop.name)}
        value={value}
        placeholder={placeholder}
        onChange={onChange}
      />
    );
  }
  const relationTargetDbId = !targetProp
    ? firstHopDbId
    : targetProp.type === "relation"
      ? targetProp.config?.relationDatabaseId ?? targetProp.databaseId
      : undefined;
  if (relationTargetDbId) {
    const relationLikeProp: DbProperty = {
      ...prop,
      type: "relation",
      config: { ...(prop.config ?? {}), relationDatabaseId: relationTargetDbId },
    };
    return (
      <RelationFilterValue
        prop={relationLikeProp}
        value={value}
        onChange={onChange}
        snapshot={snapshot}
      />
    );
  }

  return (
    <input
      className={inputClassName}
      aria-label={databaseViewLabels().filterValueFor(prop.name)}
      type="text"
      value={value == null ? "" : String(value)}
      onChange={(event) => onChange(event.target.value)}
      placeholder={placeholder}
    />
  );
}

/**
 * The people a person filter offers after "나": everyone else, as Notion's
 * phone list (seen 2026-10-23); its pill editor lists the reader too (seen
 * 2026-11-04).
 */
export function filterPeople(snapshot: DatabaseFilterSnapshot, withReader = false): { id: string; label: string }[] {
  const currentUserId = snapshot.currentUserId || "local-user";
  return personFilterOptions({
    currentUserId, currentUserOption: "", organizationProfiles: snapshot.organizationProfiles,
    selectedValue: "", workspaceMembers: snapshot.workspaceMembers,
  }).filter((option) => option.value && (withReader || option.value !== currentUserId) && !option.disabled)
    .map((option) => ({ id: option.value, label: String(option.label) }));
}

function personFilterOptions({
  currentUserId,
  currentUserOption,
  organizationProfiles,
  selectedValue,
  workspaceMembers,
}: {
  currentUserId: string;
  currentUserOption: string;
  organizationProfiles: readonly OrganizationProfile[];
  selectedValue: string;
  workspaceMembers: readonly WorkspaceMember[];
}): NotionSelectOption[] {
  const options = new Map<string, NotionSelectOption>();
  const add = (id: string | null | undefined, label?: string | null, email?: string | null) => {
    const optionValue = id?.trim();
    if (!optionValue || options.has(optionValue)) return;
    const display = label?.trim() || email?.trim() || personLabel(optionValue, currentUserId);
    options.set(optionValue, { value: optionValue, label: withPersonDistinction(optionValue, display) });
  };
  const currentMember = workspaceMembers.find((member) => member.userId === currentUserId);
  const currentProfile = organizationProfiles.find((profile) => profile.userId === currentUserId);
  add(
    currentUserId,
    // The reader by their name alone, with no "(나)" after it, as Notion's list (seen 2026-11-06).
    currentMember?.displayName ?? currentProfile?.displayName ?? personName(currentUserId),
    currentMember?.email ?? currentProfile?.email,
  );
  for (const member of workspaceMembers) add(member.userId, member.displayName, member.email);
  for (const profile of organizationProfiles) add(profile.userId, profile.displayName, profile.email);
  // Everyone the workspace's people directory knows, as its person property's
  // picker offers them (2026-11-06: the filter offered only "나").
  for (const person of directoryPeople()) add(person.userId, person.displayName);
  if (selectedValue && selectedValue !== currentUserOption && !options.has(selectedValue)) {
    options.set(selectedValue, {
      value: selectedValue,
      label: personLabel(selectedValue, currentUserId),
    });
  }
  return [
    { value: "", label: databaseViewLabels().choosePerson, disabled: true },
    { value: currentUserOption, label: databaseViewLabels().me },
    ...options.values(),
  ];
}
