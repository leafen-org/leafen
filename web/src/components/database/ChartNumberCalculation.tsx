import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { DbProperty } from "@/lib/types";
import { notionChartAggregateSupportsProperty, type ChartAggregate } from "./chartViewModel";
import { CalculationMenu } from "./TableCalculationMenu";
import { tableCalculationGroups } from "./tableCalculationModel";

/**
 * A number chart's "계산", as Notion's (seen 2026-11-18 in Chrome at 1440,
 * dark, twice): the calculate groups a table column of the shown property's
 * type offers — "수 ›" (모두 세기 · 값 세기 · 중복 제외 모두 세기 · 빈 값
 * 세기 · 비어 있지 않은 값 세기, or a checkbox's checked counts), "비율(%) ›"
 * and, by type, "추가 옵션 ›" (합계 · 평균 · 중앙값 · 최소 · 최대 · 범위) or
 * "날짜 ›" — with nothing chosen on the spot ahead of them and no 99+ switch,
 * each group 200 wide beside the 250 menu, its first row lit as it opens
 * wherever the current calculation is. "모두 세기" counts the rows and keeps
 * the property shown.
 */
export function ChartNumberCalculationMenu({ property, aggregate, onPick }: {
  property: DbProperty; aggregate: ChartAggregate; onPick(aggregate: ChartAggregate): void;
}) {
  const { t } = useTranslation("chartView");
  return <CalculationMenu label={t("calculate")} leading={[]} groups={tableCalculationGroups(property.type)}
    picked={aggregate === "count" ? "count_all" : aggregate} focusOnOpen litFirst groupWidth={200}
    onPick={(key) => onPick(key === "count_all" ? "count" : key as ChartAggregate)} />;
}

/**
 * The calculation a number chart takes as a property is shown, as Notion's
 * (seen 2026-11-18 in Chrome, from none and from each kind of property): the
 * one it had where the property allows it — 빈 값 세기 from a text to a
 * select, a number or a checkbox — unless that was 모두 세기; otherwise a
 * number's 합계 and any other property's 모두 세기 (합계 to a date: 모두 세기).
 */
export function chartNumberAggregateFor(property: DbProperty, current: ChartAggregate): ChartAggregate {
  if (current !== "count" && notionChartAggregateSupportsProperty(current, property.type)) return current;
  return notionChartAggregateSupportsProperty("sum", property.type) ? "sum" : "count";
}

/**
 * A number chart's title until one is typed, as Notion's: "모두 세기" with
 * no property shown, and with one its own words for "{calculation} of
 * {property}", the calculation named as its menu names it — "점수장 중
 * 합계장", "단계장 중 모두 세기장" (seen 2026-11-18, Notion's Korean as it
 * draws it).
 */
export function chartNumberTitle(t: TFunction, aggregate: ChartAggregate, propertyName?: string) {
  const calculation = aggregate === "count" ? t("chartView:countAll") : t(`chartView:aggregates.${aggregate}`);
  return propertyName === undefined ? calculation : t("chartView:numberTitle", { aggregate: calculation, property: propertyName });
}
