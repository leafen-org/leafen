import { i18next } from "@/i18n";
import type { ProductStatusSink } from "@/lib/productStatus";
import { nativeBoardQueryBounds } from "@/components/database/boardViewModel";
import { queryConfigSize } from "@/components/database/queryBudget";

/**
 * Whether a view change asks more of a query than its bounds take (filter
 * rules, filter groups and their depth, sorts) and more than the view did:
 * the change is refused with a notice, so a view is not saved past the
 * bounds; one saved past them before still shows (`boundedQueryConfig`).
 */
export function refuseViewQueryGrowth(previous: unknown, next: unknown, setMessage: ProductStatusSink): boolean {
  const bounds = nativeBoardQueryBounds, before = queryConfigSize(previous), after = queryConfigSize(next);
  const grows = (key: keyof typeof after, max: number) => after[key] > max && after[key] > before[key];
  if (grows("sorts", bounds.maxSorts)) {
    setMessage(i18next.t("databaseView:status.tooManySorts", { max: bounds.maxSorts }), "error");
    return true;
  }
  if (grows("filters", bounds.maxFilters)) {
    setMessage(i18next.t("databaseView:status.tooManyFilters", { max: bounds.maxFilters }), "error");
    return true;
  }
  if (grows("groups", bounds.maxFilterGroups) || grows("depth", bounds.maxFilterDepth)) {
    setMessage(i18next.t("databaseView:status.tooManyFilterGroups"), "error");
    return true;
  }
  return false;
}
