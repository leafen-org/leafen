"use client";

import { useContext, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DbProperty, DbView, Page } from "@/lib/types";
import { applyView, type QueryBounds } from "./query";
import { dateKey, formatDate } from "./dateUtils";
import { compactNumberValue, formatNumberValue, numberDisplayForProperty } from "./numberFormat";
import { percentPresentationProperty } from "./rollupNumberDisplay";
import type { SummaryValueBounds } from "./summaryValue";
import { chartStackGrouping, isChartGroupableProperty } from "./chartViewBuckets";
import { chartAxisKind, chartFirstAxis } from "./chartGroupingConfiguration";
import {
  aggregateChartValue,
  chartHeightPixels,
  normalizedChartAggregate,
  normalizedChartType,
  notionChartAggregateFamily,
  notionChartAggregateRequiresProperty,
  notionChartAggregateSupportsProperty,
  recoveredNotionChartConfig,
  type ChartAggregate,
} from "./chartViewModel";
import { chartData } from "./chartViewSeries";
import { chartCumulativeApplies, chartRuleColor, chartSettings } from "./chartViewSettings";
import { chartValueAxis } from "./chartGeometry";
import { publishChart, type ChartPublication } from "./chartViewRegistry";
import { ChartCartesian } from "./ChartCartesian";
import { chartNumberTitle } from "./ChartNumberCalculation";
import { DonutChart, NumberChart } from "./ChartViewRenderers";
import { ChartRowsWindow, type ChartRowsSource } from "./ChartRowsWindow";
import styles from "./databaseSheet";
import chartStyles from "./chartView.module.css";
import { DashboardWidgetHeightContext } from "./dashboardWidgetHeight";
import { DatabaseTaskRowsContext } from "./untitledRowName";

const CHART_NS = "chartView";

export interface ChartViewSnapshot {
  snapshotIdentity: string;
  properties: readonly DbProperty[];
  rows: readonly Page[];
  pagesById: Readonly<Record<string, Page>>;
  currentUserId?: string;
  queryBounds: QueryBounds;
  summaryValueBounds: SummaryValueBounds;
  propertiesByDatabaseId?: Record<string, DbProperty[]>;
}

export function ChartView({
  db,
  view,
  snapshot,
  rows: rowsProp,
  rowsViewApplied = false,
  search,
  contextPageId,
  rowsSource,
}: {
  db: Page;
  view: DbView;
  snapshot: ChartViewSnapshot;
  rows?: readonly Page[];
  rowsViewApplied?: boolean;
  search?: string;
  contextPageId?: string;
  /** Where a group's rows open from a click on it; none where they don't. */
  rowsSource?: ChartRowsSource;
}) {
  void db;
  const { t } = useTranslation([CHART_NS, "common"]);
  const widgetHeight = useContext(DashboardWidgetHeightContext);
  const [openedKey, setOpenedKey] = useState<string | null>(null);
  const props = useMemo(() => Array.from(snapshot.properties), [snapshot.properties]);
  const snapshotRows = useMemo(() => Array.from(snapshot.rows), [snapshot.rows]);
  const rows = useMemo(
    () => rowsProp ? Array.from(rowsProp) : snapshotRows,
    [rowsProp, snapshotRows]
  );
  const pagesById = snapshot.pagesById as Record<string, Page>;
  const groupableProps = props.filter(isChartGroupableProperty);
  const recovered = recoveredNotionChartConfig(view, props);
  const chartType = normalizedChartType(view.config?.chartType) ?? recovered.chartType ?? "bar";
  const groupProp =
    groupableProps.find((prop) => prop.id === view.config?.chartGroupBy) ??
    groupableProps.find((prop) => prop.id === recovered.groupById) ??
    chartFirstAxis(groupableProps)?.property ??
    groupableProps[0];
  // A formula's or rollup's dates sort and group as a date property's.
  const settings = chartSettings((view.config ?? {}) as Record<string, unknown>, chartAxisKind(groupProp, props));
  // The Y axis's "그룹화", which column, bar and line charts stack by.
  const stackProp = chartType === "bar" || chartType === "horizontal_bar" || chartType === "line"
    ? groupableProps.find((prop) => prop.id === settings.stackBy) : undefined;
  const aggregate: ChartAggregate =
    normalizedChartAggregate(view.config?.chartAggregate) ?? recovered.aggregate ?? "count";
  const aggregateByRef = view.config?.chartAggregateBy ?? recovered.aggregateById;
  const configuredAggregateBy = props.find((prop) => prop.id === aggregateByRef);
  const aggregateBy = configuredAggregateBy &&
    notionChartAggregateSupportsProperty(aggregate, configuredAggregateBy.type)
    ? configuredAggregateBy
    : undefined;
  const aggregateSupported = !notionChartAggregateRequiresProperty(aggregate) || !!aggregateBy;
  const aggregateFamily = notionChartAggregateFamily(aggregate);

  const shown = useMemo(
    () => rowsViewApplied
      ? rows
      : applyView(rows, props, view, pagesById, {
          search,
          currentPageId: contextPageId,
          currentUserId: snapshot.currentUserId,
        }, snapshot.queryBounds),
    [
      rows,
      rowsViewApplied,
      props,
      view,
      pagesById,
      search,
      contextPageId,
      snapshot.currentUserId,
      snapshot.queryBounds,
    ]
  );
  const numberProperty = aggregateBy ? percentPresentationProperty(aggregateBy, props, snapshot.propertiesByDatabaseId) : undefined;
  const propertyNumber = aggregateFamily === "number" && numberProperty ? numberDisplayForProperty({ config: { ...numberProperty.config } }) : null;
  const numberFormat = propertyNumber?.format ?? "number";
  const formatValue = (value: number, format = numberFormat, decimals?: number) => {
    if (aggregateFamily === "percent" || aggregateFamily === "percent_checkbox") {
      // These aggregates already calculate a percentage in the 0–100 unit.
      return formatNumberValue(value / 100, "percent", decimals);
    }
    if (aggregateFamily === "date") {
      return formatDate(dateKey(new Date(value)));
    }
    if (aggregateFamily === "date_range") {
      return t(`${CHART_NS}:dateRangeDays`, { count: value });
    }
    return formatNumberValue(value, format, decimals);
  };
  const includeZero = aggregateFamily !== "date";
  // A calculation of numbers keeps its "소수점 이하 자리" on bars, points, slices, tips and the axis (Notion's, seen 2026-11-12: 2 → 3.50, 0.00).
  const barDecimals = aggregateFamily === "number" ? settings.decimals : undefined;
  const valueOf = (bucketRows: Page[]) =>
    aggregateChartValue(bucketRows, aggregate, aggregateBy, pagesById, props, snapshot.summaryValueBounds);
  const data = groupProp && aggregateSupported && chartType !== "number" ? chartData({
    rows: shown, groupProp, stackProp, settings, theme: view.config?.chartColorTheme,
    colorByValue: view.config?.chartColorByValue === true, slices: chartType === "donut", accumulate: chartCumulativeApplies(settings, aggregate),
    personBounds: snapshot.queryBounds.personValue, valueContext: { properties: props, pagesById, bounds: snapshot.summaryValueBounds, propertiesByDatabaseId: snapshot.propertiesByDatabaseId },
    aggregate: valueOf, format: (value) => formatValue(value, numberFormat, barDecimals),
  }) : null;
  // In a dashboard widget it fills the card, less its own 4px top, 12px foot
  // and the 4px under its axis labels.
  const chartHeight = widgetHeight !== null ? widgetHeight - 20 : chartHeightPixels(view.config?.chartHeight);
  const total = chartType === "number" && aggregateSupported ? valueOf(shown) : null;
  const ariaLabel = t(`${CHART_NS}:chartAria`, {
    name: view.name || t(`${CHART_NS}:untitledProperty`),
  });
  // A task database counts its "작업 수" (Notion's, seen 2026-11-03).
  const taskRows = useContext(DatabaseTaskRowsContext);
  const aggregateName = t(`${CHART_NS}:aggregates.${aggregate === "count" && taskRows ? "countTasks" : aggregate}`);
  const valueName = aggregateBy ? t(`${CHART_NS}:valueOfProperty`, { aggregate: aggregateName, property: aggregateBy.name }) : aggregateName;

  // The view's settings list this chart's groups and save its picture.
  const groups = data?.groups, stackGroups = data?.stackGroups;
  // A count of rows or of unique values never steps its set range by a fraction; a percent number format may (Notion's).
  const valueAxis = {
    includeZero, counts: aggregateFamily === "count" || aggregateFamily === "presence",
    wholeSteps: aggregate === "count" || aggregate === "unique", percentFormat: numberFormat === "percent",
  };
  // A new reference line starts at the middle of the value axis drawn (Notion's, seen 2026-11-12: 0–4 → 2, 0–12 → 6, 4–10 → 7).
  const axisEnds = data && (chartType === "bar" || chartType === "horizontal_bar" || chartType === "line")
    ? chartValueAxis(data.categories, data.stacked, chartType, settings, valueAxis) : { min: 0, max: 0 };
  const lineValue = (axisEnds.min + axisEnds.max) / 2;
  const publishedGroups = JSON.stringify([groups, stackGroups, lineValue]);
  // The chart drawn for its picture reads this render's data, whenever the save menu asks.
  const pictureRef = useRef<ChartPublication["drawPicture"] | null>(null);
  pictureRef.current = null;
  useEffect(() => {
    publishChart(view.id, {
      groups: groups ?? [], stackGroups: stackGroups ?? [], lineValue,
      drawPicture: (size) => pictureRef.current?.(size) ?? null,
    });
    return () => publishChart(view.id, null);
    // The groups as they read, not each render's new arrays.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.id, publishedGroups]);

  if (!groupProp) {
    return (
      <div className={chartStyles.wrap} data-chart-view>
        <div className={styles.viewEmpty}>
          <div className={styles.viewEmptyTitle}>{t(`${CHART_NS}:noGroupProperty`)}</div>
          <div className={styles.viewEmptyDesc}>{t(`${CHART_NS}:noGroupPropertyDesc`)}</div>
        </div>
      </div>
    );
  }

  const categories = data?.categories ?? [];
  const opened = openedKey === null ? undefined : categories.find((category) => category.key === openedKey);
  const onOpen = rowsSource ? (category: { key: string }) => setOpenedKey(category.key) : undefined;
  const rowsWindow = opened && rowsSource && groupProp ? <ChartRowsWindow source={rowsSource} category={opened} groupProp={groupProp} stackProp={stackProp}
    stackGrouping={chartStackGrouping(settings)}
    personBounds={snapshot.queryBounds.personValue} valueContext={{ properties: props, pagesById, bounds: snapshot.summaryValueBounds, propertiesByDatabaseId: snapshot.propertiesByDatabaseId }}
    onClose={() => setOpenedKey(null)} /> : null;
  const positiveTotal = categories.reduce((sum, entry) => sum + Math.max(0, entry.value), 0);
  const donutCenterText = aggregate === "count" || aggregate === "count_values" || aggregate === "sum"
    ? formatValue(positiveTotal)
    : undefined;
  // A number chart's title, format and color, as its settings set them.
  // "모두 세기" keeps the property it counts the rows of (ChartNumberCalculation.tsx).
  const numberTitle = settings.hideTitle ? undefined
    : settings.title || chartNumberTitle(t, aggregate, (aggregate === "count" ? configuredAggregateBy : aggregateBy)?.name);
  const numberText = total === null ? "—" : aggregateFamily === "number" || aggregateFamily === "count"
    ? compactNumberValue(total, settings.numberFormat ?? numberFormat, settings.decimals ?? propertyNumber?.decimals)
    : formatValue(total, settings.numberFormat ?? numberFormat, settings.decimals ?? propertyNumber?.decimals);
  const numberColor = total !== null && settings.dynamicColor ? chartRuleColor(total, settings.colorRules) ?? settings.textColor : settings.textColor;

  // A donut or a chart along axes, on the screen as wide as its box, or for its picture at that picture's size and with nothing to press.
  const drawChart = (height: number, width: number | undefined, opens: typeof onOpen) => chartType === "donut"
    ? <DonutChart categories={categories} settings={settings} height={height} width={width} centerValue={donutCenterText} ariaLabel={ariaLabel} onOpen={opens} />
    : data && chartType !== "number" ? <ChartCartesian data={data} kind={chartType} settings={settings} height={height} width={width} formatTick={(value) => formatValue(value, numberFormat, barDecimals)}
      formatCategoryTick={(value) => { const property = percentPresentationProperty(groupProp, props, snapshot.propertiesByDatabaseId); const number = numberDisplayForProperty({ config: { ...property.config } }); return formatNumberValue(value, number.format, number.decimals); }}
      {...valueAxis} axisNames={{ x: groupProp.name || t(`${CHART_NS}:untitledProperty`), y: valueName }}
      ariaLabel={ariaLabel} lineTheme={typeof view.config?.chartColorTheme === "string" ? view.config.chartColorTheme : "auto"} onOpen={opens} />
    : null;
  if (aggregateSupported && chartType !== "number") pictureRef.current = (size) => drawChart(size.height, size.width, undefined);

  return (
    <div
      className={chartStyles.wrap}
      data-chart-view
      data-chart-type={chartType}
      data-chart-aggregate={aggregate}
      data-chart-aggregate-property={aggregateBy?.id ?? ""}
      data-chart-unsupported={aggregateSupported ? undefined : "true"}
    >
      {!aggregateSupported ? (
        <div className={styles.viewEmpty} data-chart-unsupported-state>
          <div className={styles.viewEmptyTitle}>{t(`${CHART_NS}:unsupportedAggregateTitle`)}</div>
          <div className={styles.viewEmptyDesc}>
            {t(`${CHART_NS}:unsupportedAggregateDesc`, {
              aggregate: aggregateName,
              property: configuredAggregateBy?.name ?? t(`${CHART_NS}:missingProperty`),
            })}
          </div>
        </div>
      ) : chartType === "number" ? (
        <NumberChart value={numberText} title={numberTitle} color={numberColor} height={chartHeight} ariaLabel={ariaLabel} />
      ) : drawChart(chartHeight, undefined, onOpen)}
      {rowsWindow}
    </div>
  );
}
