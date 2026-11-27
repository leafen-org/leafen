import { useContext, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty, GraphView } from "@/contentGraph";
import {
  ArrowsUpDownStackedIcon, ArrowTurnDownRightIcon, ArrowTurnLeftUpIcon, ChartPieCutoutIcon, ChartViewIcon, DecimalsIcon, DottedLineIcon,
  EyeSlashIcon, FlagIcon, NumberChartIcon, NumbersIcon, PaintBrushIcon, PaletteIcon, RisingBarsIcon, SlidersIcon, SortIcon, SplitRectangleIcon, TitlePropertyIcon,
} from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { popoverAnchorOf, type PopoverRect } from "@/lib/popoverPlacement";
import type { DbProperty } from "@/lib/types";
import { ChartGroupsMenu } from "./ChartGroupsMenu";
import { ChartNumberCalculationMenu, chartNumberAggregateFor, chartNumberTitle } from "./ChartNumberCalculation";
import { ChartChoiceList, ChartChoiceRow, ChartPopover, ChartRow, ChartSwitchRow, ChartTextField, type ChartChoice } from "./ChartPanelMenus";
import { ChartRangeMenu, ChartReferenceLinesMenu } from "./ChartReferenceLines";
import { ChartNumberColorMenu, ChartStyleOptions, ChartThemeMenu, chartTextColorName } from "./ChartStyleOptions";
import { chartAxisSorts, chartOfferedSort, chartSortFamily, chartStackSorts, type ChartSortFamily } from "./chartSortChoices";
import { ChartTypeIcon } from "./chartTypeIcons";
import { isChartGroupableProperty } from "./chartViewBuckets";
import {
  chartAxisKind, chartDateAxis, chartFirstAxis, chartFormulaValueType, chartGroupChangePatch, chartStackChangePatch, chartStartedAxisPatch,
} from "./chartGroupingConfiguration";
import {
  CHART_AGGREGATES, CHART_HEIGHTS, CHART_TYPES, chartHeightOf, normalizedChartAggregate, normalizedChartType, notionChartAggregateRequiresProperty,
  notionChartAggregateFamily, notionChartAggregateSupportsProperty, type ChartType,
} from "./chartViewModel";
import { chartInkColor } from "./chartViewPalette";
import { useChartPublication } from "./chartViewRegistry";
import { CHART_DATE_BYS, CHART_DECIMALS, chartCumulativeOffered, chartSettings, chartThemePatch, chartTypeChangePatch } from "./chartViewSettings";
import { numberFormatCode, numberFormatMatchRank, numberFormatsLastCurrencyFirst, type NumberFormat } from "./numberFormat";
import { PropertyIcon } from "./PropertyIcon";
import { rollupGivesNumber } from "./rollupNumberDisplay";
import { disambiguatePropertyLabels } from "./propertyOptionLabels";
import menuStyles from "./ChartPanelMenus.module.css";
import styles from "./ChartSettingsPanel.module.css";
import { DatabaseTaskRowsContext } from "./untitledRowName";

type Menu = "x" | "weekStart" | "sort" | "y" | "decimals" | "stack" | "stackWeekStart" | "stackSort" | "range" | "lines" | "color" | "style"
  | "value" | "calculate" | "format" | "height";
/**
 * How wide a row's menu is where it isn't the chart menus' own 250 (ChartPopover), as Notion's
 * (read 2026-11-18 in Chrome at 1440, two tabs): the range's two fields as wide as their row, the
 * reference lines and "스타일 옵션 더 보기" 280. A number chart's colors are as wide
 * as their ten letters (186), or 290 with their rules (read 2026-11-18).
 */
function chartMenuWidth(menu: Menu, type: ChartType, row: PopoverRect, dynamicColor: boolean): number | "content" | undefined {
  if (menu === "range") return row.right - row.left;
  if (menu === "color" && type === "number") return dynamicColor ? 290 : "content";
  return menu === "style" || menu === "lines" ? 280 : undefined;
}

/** The calculations a number on the Y axis offers, in Notion's order (seen 2026-11-11). */
const Y_NUMBER_AGGREGATES = ["sum", "average", "min", "max", "median", "unique"] as const;

/**
 * A chart view's own settings in its view's panel, as Notion's (seen
 * 2026-10-05), between its layout and its data source: the chart type's
 * five buttons; for columns, bars and lines "X축" (what it shows, its sort
 * and groups, leaving out zeros) and "Y축" (what it shows, the property it
 * stacks by and that sort, its range, reference lines); for a donut "데이터"
 * (what it shows, what splits it, its sort); for a number its title and
 * "데이터" (the property, its calculation and format); then "스타일" (color
 * and more options, or a number's color and height). With sub-items on,
 * "하위 항목 ›" follows a bar chart's range, a donut's sort and a number's
 * format (seen 2026-10-11). Each of these is a group of its own under its
 * name, the type's first under a line (ChartSettingsPanel.module.css), and
 * each row's mark is the one Notion draws there (seen 2026-11-18): an arrow
 * turning down to the end for what the categories show and one turning up
 * for the values, the sort's two arrows on their side for the categories and
 * a donut's (seen 2026-11-18, manual and by value alike) and upright for a
 * stack's, a split box for the grouping, an arrow
 * up over one down for the range — on its side where the values run across —
 * and a number's height, a dotted line for the reference lines, a palette
 * and a brush for the style; a donut's own are the chart view's ring and a
 * pie with a piece out, a number's "Aa", "#", "123" and two sliders.
 */
export function ChartSettingsPanel({ view, properties, disabled, subitemsRow, onUpdateView }: {
  view: GraphView; properties: readonly GraphProperty[]; disabled: boolean;
  /** The database's "하위 항목 ›", when its sub-items are on. */
  subitemsRow?: ReactNode;
  onUpdateView(view: GraphView): void;
}) {
  const { t } = useTranslation(["chartView", "propertyTypeConfig", "editor", "databaseView"]);
  const [open, setOpen] = useState<{ menu: Menu; anchor: PopoverRect } | null>(null);
  const [yProperty, setYProperty] = useState<{ id: string; anchor: PopoverRect } | null>(null);
  // The X or stacked property whose units (a date's) or kind (a status's options or groups) open beside its row.
  const [unitsOf, setUnitsOf] = useState<{ id: string; anchor: PopoverRect } | null>(null);
  const publication = useChartPublication(view.id);
  const config = view.config as Record<string, unknown>;
  const props = properties as unknown as DbProperty[];
  const type: ChartType = normalizedChartType(config.chartType) ?? "bar";
  const groupable = props.filter(isChartGroupableProperty);
  const groupProp = groupable.find((prop) => prop.id === config.chartGroupBy)
    ?? chartFirstAxis(groupable)?.property ?? groupable[0];
  const settings = chartSettings(config, chartAxisKind(groupProp, props));
  const stackProp = groupable.find((prop) => prop.id === settings.stackBy);
  const aggregate = normalizedChartAggregate(config.chartAggregate) ?? "count";
  const aggregateBy = props.find((prop) => prop.id === config.chartAggregateBy);
  const labels = disambiguatePropertyLabels(props, t("untitledProperty"));
  const nameOf = (prop?: DbProperty) => prop ? labels[props.indexOf(prop)] : t("none");
  // "분할 기준 · 작업 수", "정렬 기준 · 작업 수 높음 → 낮음" in a task database (Notion's, seen 2026-11-03).
  const countName = t(useContext(DatabaseTaskRowsContext) ? "aggregates.countTasks" : "aggregates.count");
  const aggregateName = aggregate === "count" ? countName : t(`aggregates.${aggregate}`);
  // What the values are by their calculation's own name, the property left
  // out: "합계", "평균", "개별", "수" — the Y axis's "표시 대상" puts it after
  // the property, "점수 (합계)", "분류 (개별)" (Notion's, seen 2026-11-11), and the
  // sorts by value before their way, "합계 낮음 → 높음" (Notion's, seen
  // 2026-11-18 in Chrome at 1440: 수, 합계, 평균, 중앙값 and 개별 of a number,
  // the X axis's sorts and a donut's alike).
  const valuesName = aggregateBy && aggregate === "unique" ? t("distinct") : aggregateName;
  const yName = aggregateBy && aggregate !== "count" ? t("propertyValue", { property: nameOf(aggregateBy), aggregate: valuesName }) : aggregateName;
  const valueProps = props.filter((prop) => CHART_AGGREGATES.some((item) => item !== "count" && notionChartAggregateSupportsProperty(item, prop.type)));

  const update = (patch: Record<string, unknown>) => onUpdateView({ ...view, config: { ...view.config, ...patch } });
  const close = () => { setOpen(null); setYProperty(null); setUnitsOf(null); };
  const opener = (menu: Menu) => (anchor: PopoverRect) => setOpen({ menu, anchor });
  const row = (menu: Menu, icon: ReactNode, label: string, meta?: string, rowDisabled = false) => (
    <ChartRow icon={icon} label={label} meta={meta} marker={menu} disabled={disabled || rowDisabled} closed={rowDisabled}
      expanded={open?.menu === menu} onOpen={opener(menu)} />
  );
  const head = (name: string) => <div className={menuStyles.section}>{name}</div>;
  // Properties by name, each with its mark at 20 as Notion's chart menus list them (seen 2026-10-05, 2026-11-18).
  const propertyChoices = (list: readonly DbProperty[]): ChartChoice[] => list.map((prop) => ({
    value: prop.id, label: nameOf(prop), icon: <PropertyIcon property={prop} size="regular" />,
  })).sort((left, right) => left.label.localeCompare(right.label));
  const datedX = chartDateAxis(groupProp, props), datedStack = chartDateAxis(stackProp, props);
  // Stacked dates group by month until picked, a rollup's by how near today (chartBuckets).
  const stackDateBy = settings.stackDateBy ?? (stackProp?.type === "rollup" ? "relative" : "month");
  // "마감일 (주별)", "상태 (옵션)" (Notion's, seen 2026-11-12).
  const unitName = (prop: DbProperty | undefined, dateBy: string, statusBy: string) => prop && chartDateAxis(prop, props)
    ? t("propertyByDate", { property: nameOf(prop), unit: t(`dateBy.${dateBy}`) })
    : prop?.type === "status" ? t("propertyByDate", { property: nameOf(prop),
      unit: t(statusBy === "group" ? "databaseView:groupStatusByGroup" : "databaseView:groupStatusByOption") }) : nameOf(prop);
  // Each sort menu offers and names a property's own order by its kind (chartSortChoices.ts).
  const xFamily = chartSortFamily(groupProp, props), stackFamily = chartSortFamily(stackProp, props);
  const ownName = (family: ChartSortFamily, ascending: boolean, prop?: DbProperty) => family === "date" ? t(ascending ? "dateAscending" : "dateDescending")
    : t(family === "number" ? ascending ? "valueAscending" : "valueDescending" : family === "text" ? ascending ? "alphaAscending" : "alphaDescending"
      : ascending ? "ascending" : "descending", { name: nameOf(prop) });
  const xSorts = chartAxisSorts(xFamily, datedX), xOwn = { manual: "manual", ascending: "x_ascending", descending: "x_descending" };
  // The sort shown and ✓ is the chart's own where the views API gave one; its groups drag while drawn by hand
  // (chartShownSort; Notion's, seen 2026-11-18: an axis by hand under a chart descending lists them with handles).
  const xSort = chartOfferedSort<string>(settings.shownSort, xSorts, xOwn);
  const xDrawnByHand = chartOfferedSort<string>(settings.sort, xSorts, xOwn) === "manual";
  const sortName = (sort: string) => sort === "x_ascending" || sort === "x_descending" ? ownName(xFamily, sort === "x_ascending", groupProp)
    : sort === "y_ascending" ? t("valueAscending", { name: valuesName })
      : sort === "y_descending" ? t("valueDescending", { name: valuesName }) : t("manual");
  const stackSorts = chartStackSorts(stackFamily);
  const stackSort = chartOfferedSort<string>(settings.stackSort, stackSorts, { manual: "manual", ascending: "ascending", descending: "descending" });
  const stackSortName = (sort: string) => sort === "ascending" || sort === "descending" ? ownName(stackFamily, sort === "ascending", stackProp) : t("manual");
  // "일요일", "월요일" in the reader's language.
  const weekday = (day: number) => new Intl.DateTimeFormat(activeDateLocale(), { weekday: "long" }).format(new Date(2024, 0, 7 + day));
  const theme = typeof config.chartColorTheme === "string" ? config.chartColorTheme : "auto";
  const height = chartHeightOf(config.chartHeight);
  const axes = type !== "donut" && type !== "number";

  function pickAggregate(next: string, prop?: DbProperty) {
    const value = normalizedChartAggregate(next);
    if (!value) return;
    if (!notionChartAggregateRequiresProperty(value)) update({ chartAggregate: value, chartAggregateBy: undefined });
    else if (prop) update({ chartAggregate: value, chartAggregateBy: prop.id });
    close();
  }
  // "표시 대상" of the X axis, and the Y axis's "그룹화" with "없음" first, under "속성 검색" from seven choices: a property, a date
  // one then how its days group, "상대적 날짜 · 일별 · 주별 · 월별 · 연도별", a status one then "그룹 · 옵션",
  // in a menu beside its row (Notion's, seen 2026-11-06 and 2026-11-12, stacked too), the days' 200 wide (2026-11-18, both axes).
  const unitsMenu = (axis: "x" | "stack") => {
    const stacked = axis === "stack", current = stacked ? stackProp : groupProp;
    const keys = stacked ? { formula: "chartStackFormulaGroup", date: "chartStackDateBy", status: "chartStackStatusBy" }
      : { formula: "chartFormulaGroup", date: "chartDateBy", status: "chartStatusBy" };
    const changed = (property?: DbProperty): Record<string, unknown> => stacked ? chartStackChangePatch(property, props, config.chartGroupStyle)
      : property ? chartGroupChangePatch(property, props, type) : {};
    const nested = (property?: DbProperty) => chartDateAxis(property, props) || property?.type === "status";
    const prop = groupable.find((item) => item.id === unitsOf?.id);
    const choices = propertyChoices(groupable).map((choice) => nested(groupable.find((item) => item.id === choice.value)) ? { ...choice, next: true } : choice);
    return <>
      <ChartChoiceList label={t(stacked ? "groupBy" : "showAs")} marker={axis} value={stacked ? current?.id ?? "" : current?.id} search={t("searchProperties")}
        options={stacked ? [{ value: "", label: t("none") }, ...choices] : choices}
        onPick={(id) => {
          const property = groupable.find((item) => item.id === id);
          const item = document.querySelector(`[data-chart-choices="${axis}"] [data-chart-choice="${CSS.escape(id)}"]`);
          if (nested(property) && item) { setUnitsOf({ id, anchor: popoverAnchorOf(item) }); return; }
          if (stacked || property) update(changed(property));
          close();
        }} />
      {prop?.type === "status" && unitsOf && (
        <ChartPopover anchor={unitsOf.anchor} beside label={nameOf(prop)} width={180} marker={`${axis}-status-by`} onClose={() => setUnitsOf(null)}>
          <ChartChoiceList label={nameOf(prop)} marker={`${axis}-status-by`}
            value={current?.id === prop.id ? stacked ? settings.stackStatusBy : settings.statusBy : undefined}
            options={(["group", "option"] as const).map((by) => ({ value: by, label: t(by === "group" ? "databaseView:groupStatusByGroup" : "databaseView:groupStatusByOption"),
              icon: <PropertyIcon property={prop} size="regular" /> }))}
            onPick={(by) => { update({ ...changed(prop), [keys.status]: by === "group" ? "group" : undefined }); close(); }} />
        </ChartPopover>
      )}
      {prop && prop.type !== "status" && unitsOf && (
        <ChartPopover anchor={unitsOf.anchor} beside label={nameOf(prop)} width={200} marker={`${axis}-date-by`} onClose={() => setUnitsOf(null)}>
          <ChartChoiceList label={nameOf(prop)} marker={`${axis}-date-by`} value={current?.id === prop.id ? stacked ? stackDateBy : settings.dateBy : undefined}
            options={CHART_DATE_BYS.map((unit) => ({ value: unit, label: t(`dateBy.${unit}`), icon: <PropertyIcon property={prop} size="regular" /> }))}
            onPick={(unit) => {
              // A formula's dates keep their unit in its group's setting too (notion_view_formula_group.go).
              const patch = changed(prop), formula = patch[keys.formula] as { type?: string } | undefined;
              update({ ...patch, [keys.formula]: formula?.type === "date" ? { ...formula, group_by: unit } : formula, [keys.date]: unit });
              close();
            }} />
        </ChartPopover>
      )}
    </>;
  };
  // "표시 대상" of the Y axis, as Notion's (seen 2026-11-11): 수; then the properties holding
  // numbers, each with its calculation beside it — 합계 · 평균 · 최소 · 최대 · 중앙값 · 고유 값;
  // then the others, which count their distinct values, "분류 (개별)". The X axis's property is not listed.
  const yMenu = () => {
    const numeric = (prop: DbProperty) => prop.type === "number" || prop.type === "formula" && chartFormulaValueType(prop, props) === "number"
      || prop.type === "rollup" && rollupGivesNumber(prop.config?.rollupFunction, { type: "number" }, prop.config);
    const listed = props.filter((prop) => prop.id !== groupProp?.id && notionChartAggregateSupportsProperty("unique", prop.type));
    const numbers = propertyChoices(listed.filter(numeric)), others = propertyChoices(listed.filter((prop) => !numeric(prop)));
    const prop = props.find((item) => item.id === yProperty?.id);
    return <>
      <ChartChoiceList label={t("showAs")} marker="y" value={aggregate === "count" ? "count" : aggregate === "unique" ? aggregateBy?.id : undefined}
        search={t("searchProperties")}
        options={[{ value: "count", label: countName, icon: <NumbersIcon size="regular" /> },
          ...numbers.map((choice, index) => ({ ...choice, next: true, dividerBefore: index === 0 })),
          ...others.map((choice, index) => ({ ...choice, dividerBefore: index === 0 }))]}
        onPick={(value) => {
          const picked = props.find((item) => item.id === value);
          const item = document.querySelector(`[data-chart-choices="y"] [data-chart-choice="${CSS.escape(value)}"]`);
          if (value === "count") pickAggregate("count");
          else if (picked && numeric(picked) && item) setYProperty({ id: value, anchor: popoverAnchorOf(item) });
          else if (picked) pickAggregate("unique", picked);
        }} />
      {prop && yProperty && (
        <ChartPopover anchor={yProperty.anchor} beside label={nameOf(prop)} width={200} marker="y-calculation" onClose={() => setYProperty(null)}>
          <ChartChoiceList label={nameOf(prop)} marker="y-calculation" value={aggregateBy?.id === prop.id ? aggregate : undefined}
            options={Y_NUMBER_AGGREGATES.map((item) => ({ value: item, label: item === "unique" ? t("uniqueValues") : t(`aggregates.${item}`) }))}
            onPick={(item) => pickAggregate(item, prop)} />
        </ChartPopover>
      )}
    </>;
  };

  const menuContent = (menu: Menu): ReactNode => {
    switch (menu) {
      case "x":
      case "stack":
        return unitsMenu(menu);
      case "weekStart":
        return <ChartChoiceList label={t("databaseView:groupWeekStart")} marker="week-start" value={String(settings.weekStart)}
          options={[0, 1].map((day) => ({ value: String(day), label: weekday(day) }))}
          onPick={(day) => { update({ chartStartDayOfWeek: day === "1" ? 1 : undefined }); close(); }} />;
      case "stackWeekStart":
        return <ChartChoiceList label={t("databaseView:groupWeekStart")} marker="stack-week-start" value={String(settings.stackWeekStart)}
          options={[0, 1].map((day) => ({ value: String(day), label: weekday(day) }))}
          onPick={(day) => { update({ chartStackStartDayOfWeek: day === "1" ? 1 : undefined }); close(); }} />;
      case "sort":
        return <ChartGroupsMenu label={t("sortBy")} marker="sort" sort={xSort} manual={xDrawnByHand}
          sorts={xSorts.map((sort) => ({ value: sort, label: sortName(sort) }))}
          groups={publication?.groups ?? []}
          onSort={(sort) => update({ chartSort: sort === "manual" ? undefined : sort, chartGroupSort: undefined })}
          onHidden={(hidden) => update({ chartHiddenGroups: hidden.length ? hidden : undefined })}
          onOrder={(order) => update({ chartGroupOrder: order, chartSort: undefined, chartGroupSort: undefined })} />;
      case "stackSort":
        return <ChartGroupsMenu label={t("sortBy")} marker="stack-sort" sort={stackSort} manual={stackSort === "manual"}
          sorts={stackSorts.map((sort) => ({ value: sort, label: stackSortName(sort) }))}
          groups={publication?.stackGroups ?? []}
          onSort={(sort) => update({ chartStackSort: sort === "ascending" ? undefined : sort })}
          onHidden={(hidden) => update({ chartHiddenStackGroups: hidden.length ? hidden : undefined })}
          onOrder={(order) => update({ chartStackOrder: order, chartStackSort: "manual" })} />;
      case "y":
        return yMenu();
      case "decimals":
        return <ChartChoiceList label={t("decimals")} marker="y-decimals" value={settings.decimals === undefined ? "" : String(settings.decimals)}
          options={[{ value: "", label: t("defaultSetting") }, ...CHART_DECIMALS.map((places) => ({ value: String(places), label: String(places) }))]}
          onPick={(places) => { update({ chartDecimals: places === "" ? undefined : Number(places) }); close(); }} />;
      case "range":
        return <ChartRangeMenu min={settings.yMin} max={settings.yMax} onChange={(min, max) => update({ chartYMin: min, chartYMax: max })} />;
      case "lines":
        return <ChartReferenceLinesMenu lines={settings.referenceLines}
          newValue={publication?.lineValue ?? 0}
          onChange={(lines) => update({ chartReferenceLines: lines.length ? lines : undefined })} />;
      case "color":
        return type === "number"
          ? <ChartNumberColorMenu settings={settings} disabled={disabled} onChange={update} onPicked={close} />
          : <ChartThemeMenu onPick={(next) => { update(chartThemePatch(next)); close(); }} />;
      case "style":
        return <ChartStyleOptions type={type} stacked={Boolean(stackProp) && axes} config={config} settings={settings} disabled={disabled}
          onChange={update} />;
      case "value":
        // The properties alone, "없음" being where a new number chart starts and not a choice; "속성 검색" over them from
        // seven (Notion's, seen 2026-11-18 in Chrome, two tabs: no "없음" with a property shown or none, a field at seven).
        return <ChartChoiceList label={t("showAs")} marker="number-value" value={aggregateBy?.id} search={t("searchProperties")}
          options={propertyChoices(valueProps)}
          onPick={(id) => {
            const prop = props.find((item) => item.id === id);
            if (prop) update({ chartAggregate: chartNumberAggregateFor(prop, aggregate), chartAggregateBy: prop.id });
            close();
          }} />;
      case "calculate":
        // Its groups as a table column's for the property shown, which "모두 세기" keeps (ChartNumberCalculation.tsx).
        return aggregateBy && <ChartNumberCalculationMenu property={aggregateBy} aggregate={aggregate}
          onPick={(next) => { update({ chartAggregate: next, chartAggregateBy: aggregateBy.id }); close(); }} />;
      case "format":
        return <div className={styles.format}>
          {/* The number formats as a number property's list them, "원 (KRW)" and the last currency first, under their own
              "숫자 형식 필터링..." — "상속" kept whatever is typed — in a side menu 250 wide (Notion's, seen 2026-11-18 in
              Chrome at 1440, two tabs). */}
          <ChartChoiceRow label={t("numberFormat")} marker="number-format" disabled={disabled} value={settings.numberFormat ?? ""} width={250}
            filter={{ words: t("numberFormatFilter"), rank: (format, query) => format === "" ? -1 : numberFormatMatchRank(format as NumberFormat, query) }}
            options={[{ value: "", label: t("inherit") }, ...numberFormatsLastCurrencyFirst().map((format) => {
              const name = t(`propertyTypeConfig:numberFormats.${format}`), code = numberFormatCode(format);
              return { value: format, label: code ? t("propertyTypeConfig:numberFormatWithCode", { name, code }) : name, short: name };
            })]}
            onPick={(format) => update({ chartNumberFormat: format || undefined })} />
          <ChartChoiceRow label={t("decimals")} marker="number-decimals" disabled={disabled}
            value={settings.decimals === undefined ? "" : String(settings.decimals)}
            options={[{ value: "", label: t("inherit") }, ...CHART_DECIMALS.map((places) => ({ value: String(places), label: String(places) }))]}
            onPick={(places) => update({ chartDecimals: places === "" ? undefined : Number(places) })} />
        </div>;
      case "height":
        return <ChartChoiceList label={t("height")} marker="number-height" value={height}
          options={CHART_HEIGHTS.map((value) => ({ value, label: t(`heights.${value}`) }))}
          onPick={(value) => { update({ chartHeight: value }); close(); }} />;
    }
  };

  const yRowsDisabled = !groupProp;
  return <div className={styles.settings} data-chart-settings>
    <div className={styles.group}>
      <div className={menuStyles.sectionLine} role="separator" />
      {head(t("chartType"))}
      <div className={styles.types} role="radiogroup" aria-label={t("chartType")}>
        {CHART_TYPES.map((chartType) => (
          <button key={chartType} type="button" role="radio" aria-checked={type === chartType} aria-label={t(`chartTypes.${chartType}`)}
            title={t(`chartTypes.${chartType}`)} className={styles.type} disabled={disabled} data-chart-type-choice={chartType}
            onClick={() => update({ chartType, ...chartTypeChangePatch(chartType, type, config, chartStartedAxisPatch(groupable, props)) })}>
            <ChartTypeIcon type={chartType} size="wide" />
          </button>
        ))}
      </div>
      {type === "number" && <div className={styles.title}>
        <ChartSwitchRow icon={<TitlePropertyIcon size="regular" />} label={t("chartTitle")} marker="numberTitle" disabled={disabled}
          checked={!settings.hideTitle} onChange={(on) => update({ chartHideTitle: !on })} />
        {!settings.hideTitle && <ChartTextField value={settings.title} label={t("chartTitle")} marker="numberTitle" disabled={disabled}
          placeholder={chartNumberTitle(t, aggregate, aggregateBy && nameOf(aggregateBy))} onCommit={(title) => update({ chartTitle: title || undefined })} />}
      </div>}
    </div>
    {type === "number" && <>
      <div className={styles.group}>
        {head(t("data"))}
        {row("value", <NumberChartIcon size="regular" />, t("showAs"), aggregateBy ? nameOf(aggregateBy) : t("none"))}
        {row("calculate", <NumbersIcon size="regular" />, t("calculate"), aggregate === "count" ? t("countAll") : aggregateName, !aggregateBy)}
        {/* What "형식" shows is its number format's name alone, a currency's without its code, "상속" until one is
            picked, whatever its decimal places (Notion's, seen 2026-11-18 in Chrome at 1440 dpr 1: 상속, 숫자 and 숫자
            with 2 places, 상속 with 2 places, 원 with 2 places). */}
        {row("format", <SlidersIcon size="regular" />, t("format"),
          settings.numberFormat ? t(`propertyTypeConfig:numberFormats.${settings.numberFormat}`) : t("inherit"))}
        {subitemsRow}
      </div>
      <div className={styles.group}>
        {head(t("style"))}
        {/* A color picked shows its swatch before its name, "■ 초록색"; "기본" and "동적 색상" none (Notion's, seen 2026-11-18). */}
        <ChartRow icon={<PaletteIcon size="regular" />} label={t("color")} marker="color" disabled={disabled} drop
          meta={settings.dynamicColor ? t("dynamicColor") : chartTextColorName(t, settings.textColor)}
          swatch={settings.dynamicColor || settings.textColor === "default" ? undefined : chartInkColor(settings.textColor)}
          expanded={open?.menu === "color"} onOpen={opener("color")} />
        {row("height", <ArrowsUpDownStackedIcon size="regular" />, t("height"), t(`heights.${height}`))}
      </div>
    </>}
    {type === "donut" && <div className={styles.group}>
      {head(t("data"))}
      {row("x", <ChartViewIcon size="regular" />, t("showAs"), nameOf(groupProp))}
      {row("y", <ChartPieCutoutIcon size="regular" />, t("splitBy"), yName, yRowsDisabled)}
      {row("sort", <SortIcon size="regular" className={styles.sideways} />, t("sortBy"), sortName(xSort))}
      {subitemsRow}
    </div>}
    {axes && (() => {
      // The X axis is the screen's: a horizontal bar chart's is its values',
      // so Notion lists those first under "X축" (seen 2026-10-10).
      const horizontal = type === "horizontal_bar";
      const rangeMark = <ArrowsUpDownStackedIcon size="regular" className={horizontal ? styles.sideways : undefined} />;
      const categoryRows = <>
        {/* "마감일 (주별)", "상태 (옵션)" (Notion's, seen 2026-11-12). */}
        {row("x", <ArrowTurnDownRightIcon size="regular" />, t("showAs"), unitName(groupProp, settings.dateBy, settings.statusBy))}
        {/* By week, the day a week starts on, "한 주를 시작할 요일 › 일요일" (Notion's, seen 2026-11-12). */}
        {datedX && settings.dateBy === "week" && row("weekStart", <FlagIcon size="regular" />, t("databaseView:groupWeekStart"), weekday(settings.weekStart))}
        {row("sort", <SortIcon size="regular" className={styles.sideways} />, t("sortBy"), sortName(xSort))}
        {/* "0 값 생략" writes the chart's own and its X axis's, a chart or an axis that said nothing too (Notion's, seen 2026-11-18). */}
        <ChartSwitchRow icon={<EyeSlashIcon size="regular" weight="regular" />} label={t("omitZero")} marker="omitZero" disabled={disabled}
          checked={settings.hideEmptyGroups} onChange={(on) => update({ chartHideEmptyGroups: on || undefined, chartAxisHideEmptyGroups: on, chartEmptyGroupsUnsaid: undefined })} />
      </>;
      const valueRows = <>
        {row("y", <ArrowTurnLeftUpIcon size="regular" />, t("showAs"), yName, yRowsDisabled)}
        {/* A calculation of numbers rounds to "소수점 이하 자리 › 기본 설정 · 0–5" (Notion's, seen 2026-11-12). */}
        {notionChartAggregateFamily(aggregate) === "number" && row("decimals", <DecimalsIcon size="regular" />, t("decimals"),
          settings.decimals === undefined ? t("defaultSetting") : String(settings.decimals))}
        {/* "When (월별)", then by week "한 주를 시작할 요일 › 일요일", "Progress (그룹)" (Notion's, seen 2026-11-12). */}
        {row("stack", <SplitRectangleIcon size="regular" />, t("groupBy"), stackProp ? unitName(stackProp, stackDateBy, settings.stackStatusBy) : t("none"))}
        {datedStack && stackDateBy === "week" && row("stackWeekStart", <FlagIcon size="regular" />, t("databaseView:groupWeekStart"), weekday(settings.stackWeekStart))}
        {stackProp && row("stackSort", <SortIcon size="regular" />, t("sortBy"), stackSortName(stackSort))}
        {/* "누적": each value adds those before it, offered while the groups go the X axis's
            ascending way and the chart counts or sums (Notion's, seen 2026-11-12). */}
        {chartCumulativeOffered(settings.ownSort, aggregate) && (
          <ChartSwitchRow icon={<RisingBarsIcon size="regular" />} label={t("cumulative")} marker="cumulative" disabled={disabled}
            checked={settings.cumulative} onChange={(on) => update({ chartCumulative: on })} />
        )}
        {row("range", rangeMark, t("range"), settings.yMin !== undefined || settings.yMax !== undefined ? t("rangeCustom") : t("rangeAuto"))}
        {subitemsRow}
        {row("lines", <DottedLineIcon size="regular" />, t("referenceLines"), t("lineCount", { count: settings.referenceLines.length }))}
      </>;
      return <>
        <div className={styles.group}>{head(t("xAxis"))}{horizontal ? valueRows : categoryRows}</div>
        <div className={styles.group}>{head(t("yAxis"))}{horizontal ? categoryRows : valueRows}</div>
      </>;
    })()}
    {type !== "number" && <div className={styles.group}>
      {head(t("style"))}
      {row("color", <PaletteIcon size="regular" />, t("color"), t(`themes.${theme}`, { defaultValue: t(`editor:colors.${theme}`) }))}
      {row("style", <PaintBrushIcon size="regular" />, t("moreStyle"))}
    </div>}
    {open && (
      <ChartPopover anchor={open.anchor} label={t("chartSettings")} marker={open.menu} onClose={close}
        tall={open.menu === "sort" || open.menu === "stackSort" || open.menu === "color" || open.menu === "height"}
        whole={open.menu === "sort" || open.menu === "stackSort" || open.menu === "color" || open.menu === "height" || open.menu === "lines"}
        width={chartMenuWidth(open.menu, type, open.anchor, settings.dynamicColor)}>
        {menuContent(open.menu)}
      </ChartPopover>
    )}
  </div>;
}
