import type { PersonValueBounds } from "@/lib/peopleDirectory";
import type { GraphDataSource } from "@/contentGraph";
import type { DbProperty, Page, ViewFilter } from "@/lib/types";
import { BoardGroupPreview, type BoardGroupPreviewFilters } from "./BoardGroupPreview";
import { chartBuckets, type ChartAxisGrouping, type ChartValueContext } from "./chartViewBuckets";
import type { ChartDateRange } from "./chartDateBucketRange";
import type { ChartCategory } from "./chartViewSeries";
import { useDatabaseIcon } from "./databaseOpenIcon";
import { useDatabaseOpenHref } from "./databaseOpenHref";
import type { PropertyValueReadContext } from "./PropValue";

/** What a chart's group opens its rows with: the database, its rows' table and its view's filters. */
export type ChartRowsSource = {
  dataSource: GraphDataSource;
  properties: readonly DbProperty[];
  context: PropertyValueReadContext;
  /** The view's filters, rows and bounds the window starts from; none where its filter bar cannot show. */
  filters?: Omit<BoardGroupPreviewFilters, "rule" | "ruleAsPill">;
  onOpenRow(rowId: string): void;
};

/**
 * The rule that keeps a chart's group's rows, as Notion's window shows it
 * in its filter bar (seen 2026-11-11: "상태: 완료", "완료함: 체크 표시됨",
 * "… 체크 표시되지 않음"; a status's group by its name, "Progress: To-do",
 * 2026-11-12): an option or a status's group, a person, a page or a check,
 * no value for the group of none, and a date's group its days, "When: 10월
 * 4일 → 10월 10일" (2026-11-12, chartDateBucketRange). Numbers, text and
 * computed values have none; their window holds the group's rows as they are.
 */
export function chartGroupRule(property: Pick<DbProperty, "id" | "type">, key: string, range?: ChartDateRange): ViewFilter | null {
  if (key === "__empty") return property.type === "checkbox" ? null : { propertyId: property.id, operator: "is_empty" };
  if (range) return { propertyId: property.id, operator: "is_between", value: range };
  switch (property.type) {
    case "select":
    case "status":
      return { propertyId: property.id, operator: "equals", value: [key] };
    case "multi_select":
    case "person":
    case "created_by":
    case "last_edited_by":
    case "relation":
      return { propertyId: property.id, operator: "contains", value: [key] };
    case "checkbox":
      return { propertyId: property.id, operator: "equals", value: key === "checked" };
    default:
      return null;
  }
}

/**
 * A chart's group's rows in Notion's "표 데이터 미리보기" window (seen
 * 2026-11-11): the group's name in bold, its rule first among the view's
 * filters, and, when the chart stacks, its rows by what it stacks by.
 */
export function ChartRowsWindow({ source, category, groupProp, stackProp, stackGrouping, personBounds, valueContext, onClose }: {
  source: ChartRowsSource;
  category: ChartCategory;
  groupProp: DbProperty;
  stackProp?: DbProperty;
  /** How its stacked groups group, as the chart draws them (chartStackGrouping). */
  stackGrouping?: ChartAxisGrouping;
  personBounds: PersonValueBounds;
  valueContext: ChartValueContext;
  onClose(): void;
}) {
  const databaseHref = useDatabaseOpenHref(source.dataSource.id), databaseIcon = useDatabaseIcon(source.dataSource);
  const filters: BoardGroupPreviewFilters | undefined = source.filters
    ? { ...source.filters, rule: (key) => chartGroupRule(groupProp, key, key === category.key ? category.range : undefined), ruleAsPill: true } : undefined;
  const sections = stackProp ? (rows: readonly Page[]) => chartBuckets(rows, stackProp, personBounds, valueContext, stackGrouping)
    .map((bucket) => ({ key: bucket.key, label: bucket.label, color: bucket.color, rows: bucket.rows })) : undefined;
  return (
    <BoardGroupPreview group={{ key: category.key, label: category.label, rows: category.rows }} properties={source.properties} groupPropertyId={groupProp.id}
      context={source.context} databaseTitle={source.dataSource.title} databaseSourceId={source.dataSource.id} databaseIcon={databaseIcon} databaseHref={databaseHref}
      filters={filters} plainTitle sections={sections} onOpenRow={source.onOpenRow} onClose={onClose} />
  );
}
