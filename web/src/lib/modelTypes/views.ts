import type { ButtonTemplateBlock } from "./content";
import type { NotionChartAggregate, Timestamped } from "./core";
import type { PropertyConfig, PropertyType } from "./database";
import type { FormSubmitterAccess } from "@/formWire.generated";

export type ViewType =
  | "table"
  | "board"
  | "list"
  | "gallery"
  | "calendar"
  | "timeline"
  | "form"
  | "chart"
  | "dashboard"
  | "feed"
  | "map";

export type FormAudience = "none" | "workspace" | "web";
// "blue" is the default, Notion's dark button; "azure" is its blue.
export type FormButtonColor = "blue" | "gray" | "brown" | "yellow" | "orange" | "green" | "azure" | "purple" | "pink" | "red";

export interface FormViewQuestion {
  id: string;
  propertyId: string;
  label: string;
  description: string;
  required: boolean;
  hidden: boolean;
  syncWithPropertyName: boolean;
  /** In step with its property yet named on its own, as an API form's "Title" (formQuestionLabel). */
  ownLabel?: boolean;
  longAnswer?: boolean;
  optionsDisplay?: "list" | "dropdown";
  maxSelections?: number;
  /** A date question's answer has an end, or a time, as Notion's (seen 2026-11-05). */
  includeEndDate?: boolean;
  includeTime?: boolean;
  /** A checkbox question's words beside its box (Notion's "체크박스 라벨(선택 사항)", seen 2026-11-17). */
  checkboxLabel?: string;
  /** The condition group the question sits in: it is asked only while that group's condition holds. */
  conditionGroupId?: string;
  position: number;
}

/**
 * Questions asked only after a given answer, as Notion's "조건부 로직 추가"
 * (seen 2026-11-05): of a choice question, any of the options; of a checkbox,
 * checked or not. With nothing chosen its questions are never asked.
 */
export interface FormConditionGroup {
  id: string;
  sourceQuestionId: string;
  optionIds?: string[];
  /** Several options must all be chosen, not any of them. */
  matchAll?: boolean;
  checked?: boolean;
}

export interface FormViewSubmitConfig {
  buttonLabel: string;
  confirmationTitle: string;
  confirmationBody: string;
  buttonColor: FormButtonColor;
  /** The thanks' "내 응답 보기", "이메일로 사본 받기" and "다른 응답 제출하기"; unset shows each. A new form keeps the last off, as Notion's (seen 2026-11-08). */
  showViewResponse?: boolean;
  showResponseCopy?: boolean;
  showSubmitAnother?: boolean;
}

export interface FormViewConfig {
  version: 1;
  title: string;
  description: string;
  /** An emoji, a built-in icon's "name:color" or a picture's address, as a template's bare icon (storedIconType). */
  icon: string;
  cover: string;
  /** The cover's place from its top, 0 to 100, as a page's; unset is the middle. */
  coverPosition?: number;
  questions: FormViewQuestion[];
  conditionGroups?: FormConditionGroup[];
  /** "익명의 응답": a member's answer does not say whose it is (Notion's, seen 2026-11-05). */
  anonymous?: boolean;
  submit: FormViewSubmitConfig;
}

export interface FormPublicProperty {
  id: string;
  name: string;
  description?: string;
  type: PropertyType;
  config?: Pick<PropertyConfig, "options">;
}

export interface FormReferenceOption {
  id: string;
  name: string;
  avatar?: string | null;
  /** A page's own icon, and whether its body holds anything, which an empty icon shows. */
  icon?: string;
  iconType?: string;
  hasContent?: boolean;
}

export interface FormOptionsResult {
  propertyId: string;
  type: "person" | "relation";
  options: FormReferenceOption[];
  hasMore: boolean;
  after: string | null;
  /** The pages are a task database's, an untitled one "새 작업". */
  taskDatabase?: boolean;
  /** The database a relation's list reads, which it names. */
  targetTitle?: string;
  /** Its source's own icon, drawn before the name; none where it has none. */
  targetIcon?: string;
  targetIconType?: string;
}

export interface FormDefinitionResult {
  form: FormViewConfig;
  properties: FormPublicProperty[];
  revision: string;
  /** The submit screen offers "이메일로 사본 받기": signed in, and the server can send email. */
  responseCopy?: boolean;
  /** Where it lives, to a member who can read its database: for the bar of its page. */
  place?: { pageId: string; databaseId?: string; dataSourceId: string; viewId: string; title: string; canEdit: boolean };
  /** Whom it is open to: "web" is answered as the web answers it. */
  audience?: "none" | "workspace" | "web";
}

export interface FormSubmitResult {
  rowId: string;
  replayed: boolean;
  confirmation: FormViewSubmitConfig;
}

export type FilterOperator =
  | "equals"
  | "does_not_equal"
  | "contains"
  | "does_not_contain"
  | "starts_with"
  | "ends_with"
  | "is_empty"
  | "is_not_empty"
  | "greater_than"
  | "greater_than_or_equal_to"
  | "less_than"
  | "less_than_or_equal_to"
  | "on_or_before"
  | "on_or_after"
  | "before"
  | "after"
  | "is_between"
  | "this_week"
  | "past_week"
  | "past_month"
  | "past_year"
  | "next_week"
  | "next_month"
  | "next_year"
  | "relative_to_today";

export interface ViewFilter {
  propertyId: string;
  operator: FilterOperator;
  value?: unknown;
  /** A date property's day the filter reads: "end" for its end (its start when it has none), else its start. */
  datePart?: "end";
  /** Which of a rollup's related values the condition is about: none of them, every one, else at least one (Notion's "최소 하나의 옵션 ⌄"). */
  rollupMatch?: "none" | "every";
  /** The kind of value a rollup filter made through Notion's API names where it isn't its related values' own: it matches them only by being empty or not. */
  rollupKind?: string;
  /** A rule the database keeps: its pill dimmed, opening nothing (the Sprint board's current sprint, Notion's, seen 2026-11-04). */
  locked?: boolean;
  /**
   * A dashboard's filter over one property of each of several widget sources
   * (Notion's "다중 출처 필터링", dashboardMultiSourceFilter.ts): the properties,
   * `propertyId` first, the name it was given and its icon ("name:color").
   */
  multiSource?: { propertyIds: string[]; name?: string; icon?: string };
}

/**
 * Recursive filter tree for nested AND/OR groups. `filters` are leaf conditions
 * combined by `conjunction`; `groups` are nested sub-groups combined the same way.
 */
export interface FilterGroup {
  conjunction: "and" | "or";
  filters: ViewFilter[];
  groups?: FilterGroup[];
  /**
   * The order its rules and groups show in (filterGroupTerms): the n-th
   * "filter" is `filters[n]`, the n-th "group" `groups[n]`; without it the
   * rules come first.
   */
  order?: FilterGroupTerm[];
}

export type FilterGroupTerm = "filter" | "group";

export interface ViewSort {
  propertyId: string;
  direction: "asc" | "desc";
}

export type TableCalculation =
  | "count_all"
  | "count_values"
  | "count_unique"
  | "count_empty"
  | "percent_empty"
  | "percent_not_empty"
  | "checked"
  | "unchecked"
  | "percent_checked"
  | "percent_unchecked"
  | "sum"
  | "average"
  | "median"
  | "min"
  | "max"
  | "range"
  | "earliest_date"
  | "latest_date"
  | "date_range";

export interface DatabaseSubtaskViewConfig {
  displayMode: "show" | "hidden" | "flattened" | "disabled";
  filterScope: "parents" | "parents_and_subitems" | "subitems";
  propertyId: string;
  toggleColumnId: string;
}

export interface ViewConfig {
  tasksAcrossSources?: boolean;
  statusCheckboxPropertyId?: string;
  type?: ViewType;
  visibleProperties?: string[];
  hiddenProperties?: string[];
  propertyOrder?: string[];
  rowPagePropertyOrder?: string[]; // row peek/full-page property panel order, separate from table column order
  propertyWidths?: Record<string, number>;
  tableCalculations?: Record<string, TableCalculation>;
  subtasks?: DatabaseSubtaskViewConfig;
  search?: string;
  filters?: ViewFilter[];
  filterConjunction?: "and" | "or";
  filterGroup?: FilterGroup; // nested AND/OR filter tree (supersedes flat filters when set)
  quickFilters?: Array<ViewFilter | FilterGroup>; // legacy import compatibility; normalize into filterGroup
  filterBarHidden?: boolean; // kept by views that started their bar closed; every bar starts so now (databaseFilterBar.ts)
  sorts?: ViewSort[];
  wrappedColumns?: string[]; // property ids whose table cells wrap text
  statusCheckboxColumns?: string[]; // status property ids a table shows as checkboxes (Notion's status_show_as)
  groupBy?: string; // propertyId
  groupByMode?: "group" | "option"; // Status board column axis
  hiddenGroupCounts?: string[]; // group keys whose board aggregate is hidden
  groupCountsHidden?: boolean; // every group's board aggregate hidden
  showGroupCounts?: boolean; // table group heads show their counts (Notion's "집계 표시", off at first)
  hiddenGroups?: string[]; // board group keys hidden from the board
  pinnedGroups?: string[]; // board group keys listed under "pinned groups" beside the board
  noValueAt?: number; // where a board ordered by hand shows its group for no value among its options' groups, once a group was added after it
  calendarBy?: string; // date property id
  timelineBy?: string; // start date property id
  timelineEndBy?: string; // optional end date property id
  timelineZoom?: "hours" | "day" | "week" | "bi_week" | "month" | "quarter" | "year" | "5_years"; // timeline scale
  timelineTableProperties?: Array<{ id: string; visible: boolean; width?: number; wrap?: boolean }>; // table panel columns
  timelineShowTable?: boolean; // timeline left table/list rail
  /** The view draws no dependency arrows or link handles, though the database keeps its dependencies. */
  timelineHideDependencies?: boolean;
  dependencyProperty?: string; // relation property holding each row's predecessor rows (timeline arrows)
  wrap?: boolean;
  fitImage?: boolean; // card previews contain image instead of cropping
  calendarLayout?: "month" | "week";
  showWeekends?: boolean; // calendar shows Saturday and Sunday (default true)
  mapHeight?: "small" | "medium" | "large" | "extra_large"; // map view height
  cardSize?: "small" | "medium" | "large";
  cardLayout?: "list" | "compact"; // board and gallery card density
  frozenColumns?: number; // table columns frozen from the left
  showVerticalLines?: boolean; // table lines between columns (default true)
  coverProperty?: string;
  openPageIn?: "side" | "center" | "full"; // how a row opens (peek / modal / full page)
  rowHeight?: "short" | "medium" | "tall"; // table row density
  initialLoadLimit?: number; // table first row batch size
  madeLoadLimit?: number; // the first count a view added in "새 보기" keeps until one is chosen (firstLoadLimit)
  subGroupBy?: string; // second-level grouping (board)
  subGroup?: { // its settings, in the keys of the groups' own
    hiddenGroups?: string[]; groupOrder?: string[]; groupSort?: "ascending" | "descending"; hideEmptyGroups?: false;
    keptGroups?: string[]; collapsedGroups?: string[];
  };
  chartType?: "bar" | "horizontal_bar" | "line" | "donut" | "number"; // chart view layout
  chartColorTheme?: string; // auto, colorful or one option color
  chartHeight?: "small" | "medium" | "large" | "extra_large";
  chartShowDataLabels?: boolean;
  chartColorByValue?: boolean; // shade series by their values
  chartGroupBy?: string; // chart x-axis / grouping property id
  chartAggregate?: NotionChartAggregate; // chart y-axis aggregation
  chartAggregateBy?: string; // compatible property id aggregated for non-count chart aggregations
  mapBy?: string; // place property id used by map views
  showPageIcons?: boolean; // false hides pages' icons (table rows, cards, list rows, feed posts)
  directEditPageName?: boolean; // a list's name edited where pressed, not opened (Notion's "Directly edit page name")
  dashboardTitleIcons?: boolean; // a dashboard's widget names led by their views' icons
  boardColumnColors?: boolean; // false leaves board columns untinted
  calendarWrapTitles?: boolean; // calendar cards wrap their titles
  feedShowAuthor?: boolean; // false hides a feed post's author
  icon?: string; // the view's own built-in icon, "name:color"
  description?: string; // the view's description
  notionViewId?: string; // source Notion view id, when imported from Notion API
  notionType?: string; // original Notion view type, when imported from Notion API
  notionChromeCreatedTime?: string; // peer-created time used only for imported linked DB tab ordering
  viewTabOrderEditedAt?: string; // user reordered database view tabs; prefer saved positions over imported chrome order
  inlineDatabaseBlockId?: string; // block-scoped inline/linked database view owner
  inlineDatabaseSourceViewId?: string; // source view cloned when creating a block-scoped inline view
  unsupportedNotionViewType?: string; // original Notion view type that needs a fallback renderer
  notion?: Record<string, unknown>; // raw source Notion view metadata used for linked database scoping
  notionConfiguration?: Record<string, unknown>; // raw official configuration retained for recovery/parity
  notionFilter?: unknown;
  notionSorts?: unknown;
  notionVisibleProperties?: unknown;
  notionHiddenProperties?: unknown;
  notionPropertyOrder?: unknown;
  notionPropertySettings?: unknown;
  notionQuickFilters?: unknown;
  unresolvedPropertyReferences?: unknown[];
  templateLinkedView?: boolean;
  templateLinkedSourceDatabaseId?: string;
  templateLinkedRelationPropertyId?: string;
  leafenForm?: FormViewConfig;
  /** Display snapshot only; `form_links` remains the server authority. */
  leafenFormAudience?: FormAudience;
  /** Display snapshot only, like the audience. */
  leafenFormSubmitterAccess?: FormSubmitterAccess;
}

export interface DbView extends Timestamped {
  databaseId: string;
  /** Immutable request identity for exact metadata-create replay. */
  createMutationId?: string;
  /** Server-owned actor bound to the immutable create receipt. */
  createMutationActorId?: string;
  name: string;
  type: ViewType;
  config?: ViewConfig;
  position: number;
}

export interface DbTemplate extends Timestamped {
  databaseId: string;
  /** Immutable request identity for exact metadata-create replay. */
  createMutationId?: string;
  /** Server-owned actor bound to the immutable create receipt. */
  createMutationActorId?: string;
  name: string;
  icon?: string;
  title?: string;
  properties?: Record<string, unknown>;
  blocks?: ButtonTemplateBlock[];
  isDefault?: boolean;
  position: number;
}
