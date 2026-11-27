import { createGraphProperty, createInlineDatabase, type GraphBlock, type GraphOperation, type GraphProperty, type GraphRow, type GraphView, type GraphViewType, type InlineDatabaseNames } from "@/contentGraph";
import { newViewDefaults } from "@/components/database/viewLayoutDefaults";
import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { createDefaultFormViewConfig } from "@/lib/formViewProjection";
import { createDefaultStatusConfig } from "@/lib/statusPropertyDefaults";
import type { DbProperty, PropertyConfig } from "@/lib/types";

const label = (key: string, values?: Record<string, unknown>) => String(i18next.t(`common:generated.${key}`, values ?? {}));

function day(offset: number) {
  const now = new Date();
  const date = new Date(now.getFullYear(), now.getMonth(), now.getDate() + offset);
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

/**
 * The first view's own name when "/" makes a database as a board, a timeline
 * or a calendar: the menu's words for it, "보드 보기", "타임라인 보기", "캘린더
 * 보기", as Notion's (seen 2026-11-15 in Chrome, Korean: "/보드" in one tab,
 * "/타임라인" and "/캘린더" in another, each view so named; "/표" left its
 * table unnamed). Other layouts keep none of their own here (a dashboard's is
 * named in createStartedDatabase).
 */
export function slashViewName(type: GraphViewType) {
  return type === "board" || type === "timeline" || type === "calendar"
    ? String(i18next.t(`blocks:defs.inline_database_${type}.label`)) : "";
}

/**
 * A database made from "/" as a kind of view starts with what that view needs,
 * as Notion's (seen 2026-11-05): a board its status "상태" with "담당자" and
 * grouped by it; a timeline those and "날짜" with three cards around today; a
 * calendar "태그" and "날짜"; a map "장소"; a list or gallery "태그" and "생성일";
 * a chart "상태", four rows to draw and a second view "데이터"; a form "새 폼"
 * with its two questions, its response's person and time, and a second view
 * "응답". A table and a feed have the name alone.
 */
export function createStartedDatabase(
  blocks: GraphBlock[], title: string, pageId: string, names: InlineDatabaseNames,
  firstView?: { type: GraphViewType; name: string; config?: Record<string, unknown> },
) {
  const form = firstView?.type === "form";
  const formLabel = (key: string, count?: number) => String(i18next.t(`editor:starter.formDefaults.${key}`, { count }));
  // A dashboard is a view block, its tab first with no name over it: Notion's
  // starts with "데이터 소스 제목 표시" off (seen 2026-11-09; databaseViewBlock),
  // and is named "대시보드" as its own name — its settings' name field shows it
  // where a never-named table's is empty (seen 2026-11-09; viewOwnName).
  const dashboard = firstView?.type === "dashboard" ? { ...firstView, name: String(i18next.t("databaseView:viewTypes.dashboard")),
    config: { ...firstView.config, showDataSourceTitle: false } } : undefined;
  const created = createInlineDatabase(blocks, form ? formLabel("newForm") : title, pageId,
    form ? { name: formLabel("question", 1) } : names,
    form && firstView ? { ...firstView, name: formLabel("builder") } : dashboard ?? firstView);
  if (!firstView) return { ...created, rows: [] as GraphRow[] };
  const source = created.dataSource.id;
  const properties: GraphProperty[] = [...created.properties];
  const titleId = properties[0].id;
  const add = (type: GraphProperty["type"], name: string, config: Record<string, unknown> = {}) => {
    const { property } = createGraphProperty(properties, source, type, name, config);
    properties.push(property);
    return property;
  };
  const addStatus = () => add("status", label("propertyNames.status"), createDefaultStatusConfig(newId, i18next.language));
  const config: Record<string, unknown> = {};
  const rows: GraphRow[] = [];
  const addRow = (values: GraphRow["values"]) => rows.push({
    id: newId(), dataSourceId: source, subitemParentId: null, hasContent: false,
    position: graphPositionBetween(rows.at(-1)?.position, undefined), values,
  });
  const statusIds = (property: GraphProperty) => ((property.config as { options?: Array<{ id: string }> }).options ?? []).map(({ id }) => id);
  const extraViews: GraphView[] = [];
  switch (firstView.type) {
    case "board": {
      config.groupBy = addStatus().id;
      add("person", label("propertyNames.assignee"));
      break;
    }
    case "timeline": {
      const status = addStatus();
      add("person", label("propertyNames.assignee"));
      const date = add("date", label("propertyNames.date"));
      config.timelineBy = date.id;
      [[-5, -1], [-3, 1], [-1, 3]].forEach(([from, to], index) => addRow({
        [titleId]: { type: "title", value: label("sampleCard", { number: index + 1 }) },
        [status.id]: { type: "status", value: statusIds(status)[0] },
        [date.id]: { type: "date", value: `${day(from)}/${day(to)}` },
      }));
      break;
    }
    case "calendar":
      add("multi_select", label("propertyNames.tags"), { options: [] });
      config.calendarBy = add("date", label("propertyNames.date")).id;
      break;
    case "map":
      config.mapBy = add("place", label("propertyNames.place")).id;
      break;
    case "gallery":
    case "list":
      add("multi_select", label("propertyNames.tags"), { options: [] });
      add("created_time", label("propertyNames.createdTime"));
      break;
    case "chart": {
      if (firstView.config?.chartType === "line") {
        // A line chart counts six dates of the last few days, by day, oldest first, as Notion's new one (seen 2026-11-06).
        const date = add("date", label("propertyNames.date"));
        for (const offset of [-2, -2, -1, -2, -3, -4]) addRow({ [titleId]: { type: "title", value: "" }, [date.id]: { type: "date", value: day(offset) } });
        Object.assign(config, { chartGroupBy: date.id, chartDateBy: "day", chartSort: "x_ascending" });
      } else if (firstView.config?.chartType === "number") {
        // A number chart sums four prices in dollars, as Notion's new one (seen 2026-11-06: "US$1천").
        const price = add("number", label("propertyNames.price"), { numberFormat: "dollar" });
        for (const value of [300, 100, 400, 200]) addRow({ [titleId]: { type: "title", value: "" }, [price.id]: { type: "number", value } });
        Object.assign(config, { chartAggregate: "sum", chartAggregateBy: price.id });
      } else {
        const status = addStatus();
        const [todo, doing, done] = statusIds(status);
        for (const option of [todo, doing, doing, done]) {
          addRow({ [titleId]: { type: "title", value: "" }, [status.id]: { type: "status", value: option } });
        }
        // Its bars go by the status's options, "상태 오름차순" (Notion's, seen 2026-11-14 and 2026-11-15: 시작 전 · 진행 중 · 완료).
        config.chartSort = "x_ascending";
      }
      // Both views show their tabs, not the database's title, as Notion's new chart (seen 2026-11-06).
      config.showDataSourceTitle = false;
      // A new donut has its slices from the largest and no labels on them (Notion's, seen 2026-11-06).
      if (firstView.config?.chartType === "donut") Object.assign(config, { chartSort: "y_descending", chartDonutLabels: "none" });
      extraViews.push({
        id: newId(), dataSourceId: source, type: "table", name: label("viewNames.data"),
        position: graphPositionBetween(created.views[0].position, undefined), config: { showDataSourceTitle: false },
      });
      break;
    }
    case "form": {
      // Responses read question 1, respondent, submission time, then the
      // second question, with questions added later after them, as Notion's
      // "/폼" (seen 2026-11-08).
      add("created_by", formLabel("respondent"));
      add("created_time", formLabel("submittedAt"));
      add("multi_select", formLabel("question", 2), { options: ["blue", "yellow", "red"].map((color, index) => ({
        id: newId(), name: formLabel("option", index + 1), color,
      })) });
      const questions = createDefaultFormViewConfig(properties.map((property, position): DbProperty => ({
        id: property.id, databaseId: property.dataSourceId, type: property.type, name: property.name,
        config: property.config as PropertyConfig, position,
      })));
      for (const question of questions.questions) {
        if (properties.find(({ id }) => id === question.propertyId)?.type === "multi_select") question.maxSelections = 1;
      }
      config.leafenForm = questions;
      config.leafenFormAudience = "workspace";
      extraViews.push({
        id: newId(), dataSourceId: source, type: "table", name: formLabel("responses"),
        position: graphPositionBetween(created.views[0].position, undefined), config: {},
      });
      break;
    }
    default:
      return { ...created, rows };
  }
  const view: GraphView = {
    ...created.views[0],
    config: { ...created.views[0].config, ...newViewDefaults(firstView.type, properties), ...config, ...firstView.config,
      // Its first rows in the order they are made.
      ...(rows.length ? { rowOrder: rows.map(({ id }) => id) } : {}) },
  };
  const operations: GraphOperation[] = [
    ...created.operations.map((operation) => operation.kind === "upsertView" ? { kind: "upsertView" as const, view } : operation),
    ...properties.slice(1).map((property) => ({ kind: "upsertProperty" as const, property })),
    ...extraViews.map((extra) => ({ kind: "upsertView" as const, view: extra })),
    ...rows.map((row) => ({ kind: "upsertRow" as const, row })),
  ];
  return {
    ...created,
    container: { ...created.container, viewIds: [view.id, ...extraViews.map(({ id }) => id)] },
    views: [view, ...extraViews],
    properties,
    rows,
    operations,
  };
}
