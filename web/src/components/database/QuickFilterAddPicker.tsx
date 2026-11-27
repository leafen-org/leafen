import type { ComponentProps } from "react";
import type { DbProperty } from "@/lib/types";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { DashboardFilterPropertyPicker } from "./DashboardFilterPropertyPicker";
import { dashboardFilterProperties, groupedSources, useDashboardFilterSources } from "./dashboardFilterSources";
import { databaseViewLabels } from "./databaseViewLabels";

// A bar's pill, which on a dashboard of several widget sources may name a property of each (DashboardMultiSourcePill).
export { FilterBarPill } from "./DashboardMultiSourcePill";

/**
 * The filter bar's "+ 필터" list, the toolbar's "필터" picker: the properties
 * not yet filtered and "고급 필터 추가". A dashboard's has no advanced filter,
 * and one whose widgets show more than one source lists each source's
 * properties apart under its name, ending in "⚙ 다중 출처 필터링", as its
 * toolbar's does (DatabaseFilterMenu).
 */
export function QuickFilterAddPicker({ properties, footer, onPick }: {
  /** The properties it offers, those already filtered left out. */
  properties: DbProperty[];
  footer?: ComponentProps<typeof DatabasePropertyPicker>["footer"];
  onPick(property: DbProperty): void;
}) {
  const labels = databaseViewLabels();
  const dashboard = useDashboardFilterSources(), grouped = groupedSources(dashboard);
  if (grouped) {
    const offered = new Set(properties.map(({ id }) => id));
    return <DashboardFilterPropertyPicker sources={grouped} multiSource exclude={dashboardFilterProperties(grouped).flatMap(({ id }) => offered.has(id) ? [] : [id])}
      onPick={(id) => { const property = properties.find((candidate) => candidate.id === id); if (property) onPick(property); }} />;
  }
  return <DatabasePropertyPicker placeholder={labels.filterBy} properties={properties} footer={dashboard ? undefined : footer} onPick={onPick} />;
}
