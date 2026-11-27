import { NUMBER_DECIMAL_PLACES, normalizeNumberFormat, type NumberFormat } from "./numberFormat";

/**
 * A chart view's settings beyond its type, axes and color theme, as Notion's
 * chart settings set them (seen 2026-10-05) and the official API names them:
 * how its groups are ordered, shown and stacked, its range, reference lines,
 * grid lines, axis names, labels, caption, line and donut styles, and a
 * number chart's title, format and color. A setting left unset reads as
 * Notion's default.
 */

const CHART_SORTS = ["manual", "x_ascending", "x_descending", "y_ascending", "y_descending"] as const;
export type ChartSort = (typeof CHART_SORTS)[number];
const CHART_SERIES_SORTS = ["manual", "ascending", "descending"] as const;
export type ChartSeriesSort = (typeof CHART_SERIES_SORTS)[number];
export const CHART_GRID_LINES = ["none", "horizontal", "vertical", "both"] as const;
export type ChartGridLines = (typeof CHART_GRID_LINES)[number];
export const CHART_AXIS_LABELS = ["none", "x_axis", "y_axis", "both"] as const;
export type ChartAxisLabels = (typeof CHART_AXIS_LABELS)[number];
export const CHART_GROUP_STYLES = ["normal", "percent", "side_by_side"] as const;
export type ChartGroupStyle = (typeof CHART_GROUP_STYLES)[number];
export const CHART_LEGEND_POSITIONS = ["off", "bottom", "side"] as const;
export type ChartLegendPosition = (typeof CHART_LEGEND_POSITIONS)[number];
export const CHART_DONUT_LABELS = ["none", "value", "name", "name_and_value"] as const;
export type ChartDonutLabels = (typeof CHART_DONUT_LABELS)[number];
/** A reference line's colors, in the order Notion's grid shows them; "default" is its 검정색, "gray" its 옅은 회색 (the API's gray and lightgray). */
export const CHART_LINE_COLORS = ["default", "gray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"] as const;
export type ChartLineColor = (typeof CHART_LINE_COLORS)[number];
export const CHART_LINE_DASHES = ["dash", "solid"] as const;
/** How many reference lines the screen adds to a chart, as Notion's (seen 2026-11-12); its API takes 100. */
export const CHART_LINE_LIMIT = 10;
/** A number chart's value colors, in the order Notion's letters show them (seen 2026-11-18): its yellow before its orange, as the reference lines'. */
export const CHART_TEXT_COLORS = ["default", "gray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"] as const;
export const CHART_RULE_OPERATORS = [">", ">=", "<", "<=", "=", "!="] as const;
export type ChartRuleOperator = (typeof CHART_RULE_OPERATORS)[number];
export const CHART_DECIMALS = NUMBER_DECIMAL_PLACES;

export type ChartReferenceLine = { id?: string; value: number; label: string; color: ChartLineColor; dashStyle: (typeof CHART_LINE_DASHES)[number] };
export type ChartColorRule = { operator: ChartRuleOperator; value: number; color: string };

/** How a date X axis groups its days, as Notion's menu lists them (seen 2026-11-06). */
export const CHART_DATE_BYS = ["relative", "day", "week", "month", "year"] as const;
/** The X axis properties whose days group by one of those units. */
export const CHART_DATE_TYPES: ReadonlySet<string> = new Set(["date", "created_time", "last_edited_time"]);
export type ChartDateBy = (typeof CHART_DATE_BYS)[number];

export interface ChartSettings {
  /** How a date X axis groups its days. */
  dateBy: ChartDateBy;
  /** The day a week starts on by week, 0 Sunday until set to 1 Monday (Notion's "한 주를 시작할 요일", seen 2026-11-12). */
  weekStart: 0 | 1;
  /** A status axis's groups: its options until set to its status groups (Notion's "그룹 · 옵션", seen 2026-11-12). */
  statusBy: "option" | "group";
  /**
   * What a formula X axis was given to group by: another type than the
   * property's (true), or the result family its group names; its group for
   * no value shows only with rows unless that is the formula's own family
   * (Notion's, seen 2026-11-12).
   */
  axisGivenAs?: true | string;
  /** How its groups are drawn (chartSort). */
  sort: ChartSort;
  /**
   * The chart's own sort as kept (unset reading by hand), with its type's
   * defaults, which "누적" goes by (chartShownSort).
   */
  ownSort: ChartSort;
  /** The sort its settings show and ✓ (chartShownSort). */
  shownSort: ChartSort;
  /** Its X axis's own order, by hand until the views API sorts it (chartSort), which groups of a value keep when the chart sorts by value. */
  axisSort: ChartSeriesSort;
  groupOrder: string[];
  hiddenGroups: ReadonlySet<string>;
  /** Its own "0 값 생략", as its switch shows it. */
  hideEmptyGroups: boolean;
  /**
   * Its X axis leaves out its groups without rows apart from its own switch,
   * as Notion's API keeps an x_axis hide_empty_groups given beside the
   * chart's own another way: the chart leaves them out when either does
   * (seen 2026-11-12).
   */
  axisHidesEmptyGroups: boolean;
  stackBy?: string;
  stackSort: ChartSeriesSort;
  stackOrder: string[];
  hiddenStackGroups: ReadonlySet<string>;
  /** What it stacks by leaves out its groups without rows (Notion's API stack_by hide_empty_groups, seen 2026-11-12). */
  hideEmptyStackGroups: boolean;
  /**
   * How stacked dates group their days, as picked beside the property in its "그룹화" menu, and the day their
   * weeks start on; a stacked status's groups or options (Notion's, seen 2026-11-12). Until picked, its
   * property's own (chartStackGrouping).
   */
  stackDateBy?: ChartDateBy;
  stackWeekStart: 0 | 1;
  stackStatusBy: "option" | "group";
  groupStyle: ChartGroupStyle;
  legend: ChartLegendPosition;
  yMin?: number;
  yMax?: number;
  referenceLines: ChartReferenceLine[];
  gridLines: ChartGridLines;
  axisLabels: ChartAxisLabels;
  showDataLabels: boolean;
  showCaption: boolean;
  caption: string;
  smoothLine: boolean;
  hideLineFill: boolean;
  cumulative: boolean;
  donutLabels: ChartDonutLabels;
  hideCenterValue: boolean;
  hideCenterLabel: boolean;
  centerLabel: string;
  hideNegative: boolean;
  hideTitle: boolean;
  title: string;
  numberFormat?: NumberFormat;
  decimals?: number;
  textColor: string;
  dynamicColor: boolean;
  colorRules: ChartColorRule[];
}

type Config = Readonly<Record<string, unknown>>;

function oneOf<T extends string>(values: readonly T[], value: unknown, fallback: T): T {
  return values.includes(value as T) ? value as T : fallback;
}

function strings(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function finite(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function text(value: unknown) {
  return typeof value === "string" ? value : "";
}

export function chartReferenceLines(value: unknown): ChartReferenceLine[] {
  return (Array.isArray(value) ? value : []).flatMap((raw) => {
    const line = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
    const at = finite(line.value);
    return at === undefined ? [] : [{
      ...(typeof line.id === "string" ? { id: line.id } : {}),
      value: at, label: text(line.label), color: oneOf(CHART_LINE_COLORS, line.color, "default"),
      dashStyle: oneOf(CHART_LINE_DASHES, line.dashStyle, "dash"),
    }];
  });
}

function colorRules(value: unknown): ChartColorRule[] {
  return (Array.isArray(value) ? value : []).flatMap((raw) => {
    const rule = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
    const at = finite(rule.value);
    return at === undefined || !CHART_RULE_OPERATORS.includes(rule.operator as ChartRuleOperator) ? [] : [{
      operator: rule.operator as ChartRuleOperator, value: at, color: oneOf(CHART_TEXT_COLORS, rule.color, "green"),
    }];
  });
}

/**
 * Whether a chart offers "누적": only while its own sort goes the X axis's
 * ascending way and it counts or sums — by bars, sideways bars or line, on
 * any X axis (Notion's, seen 2026-11-12: no row by hand, descending or by
 * value, nor for an average, minimum, maximum, median or distinct count).
 */
export function chartCumulativeOffered(sort: ChartSort, aggregate: string) {
  return sort === "x_ascending" && (aggregate === "count" || aggregate === "sum");
}

/** "누적" adds each value to those before it, in the order drawn, where it is on and offered. */
export function chartCumulativeApplies(settings: Pick<ChartSettings, "cumulative" | "ownSort">, aggregate: string) {
  return settings.cumulative && chartCumulativeOffered(settings.ownSort, aggregate);
}

const AXIS_SORTS = { manual: "manual", ascending: "x_ascending", descending: "x_descending" } as const;

/**
 * How a chart orders its groups: by its X axis's own sort, which Notion's
 * API keeps apart from the chart's (chartGroupSort, which this screen's sort
 * menu clears), unless the chart sorts by value; a rollup's axis, and which
 * way a number's runs, by the chart's sort alone (Notion's, seen 2026-11-12:
 * a number axis given descending with the chart's sort unset runs up).
 */
function chartSort(config: Config, groupType?: string): ChartSort {
  const chosen = oneOf(CHART_SORTS, config.chartSort, "manual");
  const own = chartAxisOwnSort(config, groupType);
  return own && groupType !== "rollup" && groupType !== "number" && !chosen.startsWith("y_") ? AXIS_SORTS[own] : chosen;
}

// The X axes given as another type than their property's that go up whatever sort they were given; the others go by
// hand (Notion's, seen 2026-11-12: a text's, a formula's of words, a URL's "글 없음 · _x · 9 · 10 · 나무 · apple"
// ascending or descending, a relation's "관계 없음 · Banana · zeta · 가나다", a status's by its options; a select's
// "B · C · a · 선택 없음 · 가" and people by their IDs).
const GIVEN_AXIS_ASCENDING: ReadonlySet<string> = new Set(["title", "rich_text", "url", "email", "phone", "formula", "relation", "status"]);

/**
 * Its X axis's own sort as the views API keeps it (none once this screen sorts the chart): for an axis given as
 * another type than its property's, the one Notion's chart goes by whatever it was given.
 */
function chartAxisOwnSort(config: Config, groupType?: string): ChartSeriesSort | undefined {
  const own = CHART_SERIES_SORTS.find((sort) => sort === config.chartGroupSort);
  if (!own || chartAxisGivenAs(config) !== true) return own;
  return groupType && GIVEN_AXIS_ASCENDING.has(groupType) ? "ascending" : "manual";
}

// A group given as another type than its property's, or the family a formula axis was given (as the views API keeps them).
function chartAxisGivenAs(config: Config): true | string | undefined {
  const given = config.chartGivenAxis as { property?: unknown } | undefined;
  if (given && typeof given === "object" && given.property === config.chartGroupBy) return true;
  const family = (config.chartFormulaGroup as { type?: unknown } | undefined)?.type;
  return typeof family === "string" ? family : undefined;
}

/**
 * A sort as its X axis's type takes it: a number's by hand goes up, and
 * dates by when (Notion's, seen 2026-11-06), or by value where the views
 * API says so (seen 2026-11-12).
 */
function typedChartSort(sort: ChartSort, groupType?: string): ChartSort {
  if (groupType === "number" && sort === "manual") return "x_ascending";
  const dated = groupType && (CHART_DATE_TYPES.has(groupType) || groupType === "rollup_date");
  return dated && sort !== "x_descending" && !sort.startsWith("y_") ? "x_ascending" : sort;
}

/**
 * The sort a chart's settings show and ✓, and the one "누적" goes by: the
 * views API keeps the chart's own sort apart from its X axis's, which it
 * draws by (chartSort). Notion's settings show the chart's own wherever the
 * API gave one, "수동" too, and the axis's where it gave none; "누적" goes
 * by the chart's own alone, unset reading by hand (seen 2026-11-18 in Chrome
 * at 1440, a select axis given each way through the API: axis by hand and
 * chart descending showed 단계 내림차순 over bars drawn by hand; axis
 * descending and chart ascending showed 단계 오름차순 and offered 누적, which
 * added up the bars in their descending order; axis ascending and no chart
 * sort showed 단계 오름차순 without 누적, and given 누적 drew none; axis
 * descending and chart "manual" showed 수동 over descending bars).
 */
function chartShownSort(config: Config, drawn: ChartSort, groupType?: string) {
  const given = CHART_SORTS.find((sort) => sort === config.chartSort);
  const own = typedChartSort(given ?? "manual", groupType);
  return { ownSort: own, shownSort: given ? own : drawn };
}

/** A chart view's settings, each as set or as Notion's default. */
export function chartSettings(config: Config, groupType?: string): ChartSettings {
  const decimals = finite(config.chartDecimals);
  const sort = typedChartSort(chartSort(config, groupType), groupType);
  return {
    sort,
    ...chartShownSort(config, sort, groupType),
    axisSort: chartAxisOwnSort(config, groupType) ?? "manual",
    groupOrder: strings(config.chartGroupOrder),
    hiddenGroups: new Set(strings(config.chartHiddenGroups)),
    hideEmptyGroups: config.chartHideEmptyGroups === true,
    axisHidesEmptyGroups: config.chartAxisHideEmptyGroups === true,
    stackBy: typeof config.chartStackBy === "string" && config.chartStackBy ? config.chartStackBy : undefined,
    stackSort: oneOf(CHART_SERIES_SORTS, config.chartStackSort, "ascending"),
    stackOrder: strings(config.chartStackOrder),
    hiddenStackGroups: new Set(strings(config.chartHiddenStackGroups)),
    hideEmptyStackGroups: config.chartHideEmptyStackGroups === true,
    stackDateBy: CHART_DATE_BYS.find((unit) => unit === config.chartStackDateBy),
    stackWeekStart: config.chartStackStartDayOfWeek === 1 ? 1 : 0,
    stackStatusBy: config.chartStackStatusBy === "group" ? "group" : "option",
    groupStyle: oneOf(CHART_GROUP_STYLES, config.chartGroupStyle, "normal"),
    // The legend switch of bars and lines hides it apart from the donut's choice, the views API's legend_position, and a
    // donut shows it 꺼짐 too until a place is picked (Notion's, seen 2026-11-18 in Chrome: a line's switch off left no
    // legend_position and its donut listed 꺼짐 and drew none; 하단 picked wrote "bottom" and drew it).
    legend: config.chartLegendHidden === true ? "off" : oneOf(CHART_LEGEND_POSITIONS, config.chartLegendPosition, "bottom"),
    yMin: finite(config.chartYMin),
    yMax: finite(config.chartYMax),
    referenceLines: chartReferenceLines(config.chartReferenceLines),
    gridLines: oneOf(CHART_GRID_LINES, config.chartGridLines, "horizontal"),
    axisLabels: oneOf(CHART_AXIS_LABELS, config.chartAxisLabels, "none"),
    // Data labels show unless turned off, as a new Notion chart's do.
    showDataLabels: config.chartShowDataLabels !== false,
    showCaption: config.chartShowCaption === true,
    caption: text(config.chartCaption),
    smoothLine: config.chartSmoothLine === true,
    hideLineFill: config.chartHideLineFill === true,
    cumulative: config.chartCumulative === true,
    donutLabels: chartDonutLabels(config),
    hideCenterValue: config.chartHideCenterValue === true,
    hideCenterLabel: config.chartHideCenterLabel === true,
    centerLabel: text(config.chartCenterLabel),
    hideNegative: config.chartHideNegative === true,
    // A rollup's dates by how near today until a unit is picked (chartAxisKind).
    dateBy: oneOf(CHART_DATE_BYS, config.chartDateBy, groupType === "rollup_date" || groupType === "rollup" ? "relative" : "month"),
    weekStart: config.chartStartDayOfWeek === 1 ? 1 : 0,
    statusBy: config.chartStatusBy === "group" ? "group" : "option",
    axisGivenAs: chartAxisGivenAs(config),
    hideTitle: config.chartHideTitle === true,
    title: text(config.chartTitle),
    numberFormat: normalizeNumberFormat(config.chartNumberFormat),
    decimals: decimals !== undefined && (CHART_DECIMALS as readonly number[]).includes(decimals) ? decimals : undefined,
    textColor: oneOf(CHART_TEXT_COLORS, config.chartTextColor, "default"),
    dynamicColor: config.chartDynamicColor === true,
    colorRules: colorRules(config.chartColorRules),
  };
}

/** The value color a number chart's rules give, the first that holds, or none. */
export function chartRuleColor(value: number, rules: readonly ChartColorRule[]) {
  const holds = (rule: ChartColorRule) => rule.operator === ">" ? value > rule.value : rule.operator === ">=" ? value >= rule.value
    : rule.operator === "<" ? value < rule.value : rule.operator === "<=" ? value <= rule.value
      : rule.operator === "=" ? value === rule.value : value !== rule.value;
  return rules.find(holds)?.color;
}

/**
 * A color theme picked from "색상", as Notion's screen writes it (seen
 * 2026-11-18 in Chrome, its views API read after each pick): the theme
 * itself, "auto" too; one of many colors (자동, 다양한 색상) turns value
 * colors off, color_by_value false, where one color keeps them.
 */
export function chartThemePatch(theme: string): Record<string, unknown> {
  return theme === "auto" || theme === "colorful" ? { chartColorTheme: theme, chartColorByValue: false } : { chartColorTheme: theme };
}

/**
 * What a donut's slices are labeled with: none until picked, as Notion's
 * donut draws one its settings never labeled (seen 2026-11-18 in Chrome at
 * 1440, "데이터 레이블 없음": a number chart made a donut, and a donut its
 * views API made with no donut_labels). Its type change labels a donut made
 * from bars or a line (chartTypeChangePatch).
 */
export function chartDonutLabels(config: Config): ChartDonutLabels {
  return oneOf(CHART_DONUT_LABELS, config.chartDonutLabels, "none");
}

/**
 * The settings a chart takes when its type changes, as Notion's screen writes
 * them (seen 2026-10-05; 2026-11-18 in Chrome at 1440, every change among
 * column, bar, line, donut and number in two tabs and databases, its views
 * API read after each, and the labels' ways once more in a third):
 * - among column, bar and line, nothing but the type;
 * - from one of those to a donut, its slices go from the largest and its X
 *   axis's own sort ascending, and its slices are labeled with their values
 *   if the bars showed theirs — the labels it had kept where they show
 *   something, none where the bars showed none; what it stacks by goes and
 *   does not come back;
 * - from a donut back to one of those, the bars show their values if the
 *   donut's slices showed labels;
 * - a number chart has no X axis: made a chart of any other type again, it
 *   starts the axis a new chart starts with (`axis`, chartFirstAxis) by
 *   hand, keeping the chart's own sort, labels and "0 값 생략"; that axis
 *   leaves out its empty groups in a donut and shows them in the others
 *   (bar → number → donut "수동", 단계 → number → column 우선순위).
 */
export function chartTypeChangePatch(
  next: string, current: string, config: Config = {}, axis: Readonly<Record<string, unknown>> = {},
): Record<string, unknown> {
  if (next === current) return {};
  const patch: Record<string, unknown> = next === "donut" || next === "number" ? { chartStackBy: undefined } : {};
  if (current === "number") {
    // Its "0 값 생략" says it for the new axis too where it is on (notionChartAxisHidesEmptyGroups).
    const own = config.chartHideEmptyGroups === true;
    return { ...patch, ...axis, chartAxisHideEmptyGroups: next === "donut" ? own ? undefined : true : own ? false : undefined };
  }
  if (next === "donut") {
    const shown = config.chartShowDataLabels !== false;
    const labels = chartDonutLabels(config);
    return {
      ...patch, chartSort: "y_descending", chartGroupSort: "ascending",
      ...(!shown ? { chartDonutLabels: "none" } : labels === "none" ? { chartDonutLabels: "value" } : {}),
    };
  }
  if (current === "donut" && next !== "number") return { ...patch, chartShowDataLabels: chartDonutLabels(config) !== "none" };
  return patch;
}
