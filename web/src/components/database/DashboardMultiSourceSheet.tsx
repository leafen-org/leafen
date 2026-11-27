import { i18next } from "@/i18n";
import type { DbProperty, ViewFilter } from "@/lib/types";
import { PhoneSheet } from "../shell/PhoneSheet";
import { MultiSourceBody } from "./DashboardMultiSourceWindow";
import type { DashboardFilterSource } from "./dashboardFilterSources";
import { multiSourceProperties } from "./dashboardMultiSourceFilter";

/**
 * A dashboard filter's "다중 출처 필터링" on a phone: a sheet over the
 * filters' sheet headed by its name, holding what its window holds on a
 * computer (MultiSourceBody) — its lists sheets over it. Its closing with no
 * property left takes the filter away, as the window's does. Notion's phone
 * not yet seen.
 */
export function MultiSourceSheet({ filter, properties, sources, onChange, onRemove, onDone, onClose }: {
  filter: ViewFilter;
  properties: DbProperty[];
  sources: readonly DashboardFilterSource[];
  onChange(filter: ViewFilter): void;
  onRemove(): void;
  /** "완료하기": the filter's condition in its place. */
  onDone(): void;
  onClose(): void;
}) {
  const empty = multiSourceProperties(filter, properties).length === 0;
  return (
    <PhoneSheet title={i18next.t("databaseView:multiSourceFiltering")} onClose={empty ? onRemove : onClose}
      data={{ "data-multi-source-window": "" }}>
      <MultiSourceBody sheet filter={filter} properties={properties} sources={sources} exclude={[]}
        onChange={onChange} onRemove={onRemove} onDone={onDone} />
    </PhoneSheet>
  );
}
